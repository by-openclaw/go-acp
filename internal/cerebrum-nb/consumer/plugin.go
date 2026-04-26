// Package cerebrumnb is the consumer plugin for the EVS Cerebrum
// Northbound API v0.13 (a.k.a. "Neuron Bridge"). It speaks XML over
// WebSocket on port 40007 by default.
//
// Registry name: cerebrum-nb
package cerebrumnb

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"acp/internal/cerebrum-nb/codec"
	"acp/internal/protocol"
)

func init() {
	protocol.Register(&Factory{})
}

// DefaultPort is the Cerebrum NB WebSocket default port. Configurable
// in the Cerebrum app; the CLI exposes --port to override.
const DefaultPort = 40007

// Factory builds Plugin instances. Registered at init().
type Factory struct{}

func (f *Factory) Meta() protocol.ProtocolMeta {
	return protocol.ProtocolMeta{
		Name:        "cerebrum-nb",
		DefaultPort: DefaultPort,
		Description: "EVS Cerebrum Northbound API (XML over WebSocket; a.k.a. Neuron Bridge)",
	}
}

func (f *Factory) New(logger *slog.Logger) protocol.Protocol {
	return NewPlugin(logger)
}

// Plugin is the consumer-side handle. It wraps a single WebSocket
// session (one connection per Plugin) and routes RX events to
// subscribers.
type Plugin struct {
	logger *slog.Logger

	// Username / Password come from CLI flags or the
	// DHS_CEREBRUM_USER / DHS_CEREBRUM_PASS env vars. Set before
	// Connect.
	Username string
	Password string

	// UseTLS opts the dial URL into wss:// instead of ws://.
	UseTLS bool

	// InsecureSkipVerify disables TLS cert validation. Only honoured
	// when UseTLS is true.
	InsecureSkipVerify bool

	mu      sync.Mutex
	session *Session
}

// NewPlugin constructs a Plugin with the given logger. Credentials
// must be set on the returned Plugin before Connect.
func NewPlugin(logger *slog.Logger) *Plugin {
	if logger == nil {
		logger = slog.Default()
	}
	return &Plugin{logger: logger.With(slog.String("plugin", "cerebrum-nb"))}
}

// Connect dials the Cerebrum WebSocket endpoint and performs login.
// Cerebrum has no path on the URL — host:port only.
func (p *Plugin) Connect(ctx context.Context, host string, port int) error {
	if port == 0 {
		port = DefaultPort
	}
	if p.Username == "" || p.Password == "" {
		return fmt.Errorf("cerebrum-nb: username and password required (set Plugin.Username/Password or DHS_CEREBRUM_USER/DHS_CEREBRUM_PASS env)")
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.session != nil {
		return fmt.Errorf("cerebrum-nb: already connected (call Disconnect first)")
	}

	scheme := "ws"
	if p.UseTLS {
		scheme = "wss"
	}
	url := fmt.Sprintf("%s://%s:%d/", scheme, host, port)

	sess, err := newSession(ctx, p.logger, url, p.UseTLS, p.InsecureSkipVerify)
	if err != nil {
		return err
	}
	if err := sess.login(ctx, p.Username, p.Password); err != nil {
		_ = sess.close()
		return err
	}
	p.session = sess
	return nil
}

// Disconnect tears down the WebSocket. Idempotent.
func (p *Plugin) Disconnect() error {
	p.mu.Lock()
	sess := p.session
	p.session = nil
	p.mu.Unlock()
	if sess == nil {
		return nil
	}
	return sess.close()
}

// Session returns the live session, or nil if not connected. CLI verbs
// that need richer access (Listen, ListDevices, Walk) call this.
func (p *Plugin) Session() *Session {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.session
}

// Compliance returns the in-memory compliance profile so CLI / tests
// can read accumulated events.
func (p *Plugin) Compliance() *Profile {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.session == nil {
		return nil
	}
	return p.session.compliance
}

// ----- protocol.Protocol shim methods -----
//
// Cerebrum is a control-plane (router-of-routers), not a card-frame
// device. The slot/walk/getValue/setValue interface bends; we map the
// most useful subset and return clear errors for the rest. CLI verbs
// for Cerebrum-specific shapes (listen, list-devices, list-routers,
// walk) live in cmd/dhs/cerebrum_nb*.go and call Session() methods
// directly.

// GetDeviceInfo polls Cerebrum and returns the redundancy snapshot as
// DeviceInfo. ProtocolVersion is parsed from the login_reply api_ver.
func (p *Plugin) GetDeviceInfo(ctx context.Context) (protocol.DeviceInfo, error) {
	sess := p.Session()
	if sess == nil {
		return protocol.DeviceInfo{}, fmt.Errorf("cerebrum-nb: not connected")
	}
	if _, err := sess.Poll(ctx); err != nil {
		return protocol.DeviceInfo{}, err
	}
	host, port := sess.RemoteHostPort()
	return protocol.DeviceInfo{
		IP:              host,
		Port:            port,
		ProtocolVersion: sess.APIVersionMajor(),
	}, nil
}

// GetSlotInfo / Walk / GetValue / SetValue do not map cleanly onto
// Cerebrum's control-plane shape — the richer Session methods are the
// supported entry points.

func (p *Plugin) GetSlotInfo(_ context.Context, _ int) (protocol.SlotInfo, error) {
	return protocol.SlotInfo{}, fmt.Errorf("cerebrum-nb: slot-info not applicable; use Session.ListDevices / ListRouters")
}

func (p *Plugin) Walk(_ context.Context, _ int) ([]protocol.Object, error) {
	return nil, fmt.Errorf("cerebrum-nb: walk not applicable; use Session.WalkAll")
}

func (p *Plugin) GetValue(_ context.Context, _ protocol.ValueRequest) (protocol.Value, error) {
	return protocol.Value{}, fmt.Errorf("cerebrum-nb: get-value not applicable; use Session.Obtain")
}

func (p *Plugin) SetValue(ctx context.Context, req protocol.ValueRequest, val protocol.Value) (protocol.Value, error) {
	sess := p.Session()
	if sess == nil {
		return protocol.Value{}, fmt.Errorf("cerebrum-nb: not connected")
	}
	if req.Path == "" {
		return protocol.Value{}, fmt.Errorf("cerebrum-nb: set-value requires Path of form 'device.sub_device.object'")
	}
	if val.Kind != protocol.KindString {
		return protocol.Value{}, fmt.Errorf("cerebrum-nb: set-value: only string values mapped to <device SET_VALUE> (got %s)", val.Kind)
	}
	devName, sub, obj, err := splitDevicePath(req.Path)
	if err != nil {
		return protocol.Value{}, err
	}
	body := &codec.DeviceAction{
		Type:       "SET_VALUE",
		DeviceName: devName,
		SubDevice:  sub,
		Object:     obj,
		Value:      val.Str,
	}
	if err := sess.Action(ctx, body); err != nil {
		return protocol.Value{}, err
	}
	return val, nil
}

// Subscribe / Unsubscribe through the generic interface aren't wired —
// CLI verbs use Session.Subscribe<X> directly to express the §5
// addressing precisely.
func (p *Plugin) Subscribe(_ protocol.ValueRequest, _ protocol.EventFunc) error {
	return fmt.Errorf("cerebrum-nb: protocol.Subscribe not implemented; use Session.Subscribe<Routing|Category|Salvo|Device|Datastore>")
}

func (p *Plugin) Unsubscribe(_ protocol.ValueRequest) error {
	return fmt.Errorf("cerebrum-nb: protocol.Unsubscribe not implemented; use Session.UnsubscribeAll")
}
