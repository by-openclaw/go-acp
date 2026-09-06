// Package cerebrumnb is the consumer plugin for the EVS Cerebrum
// Northbound API v0.13 (a.k.a. "Neuron Bridge"). It speaks XML over
// WebSocket on port 40007 by default.
//
// Registry name: cerebrum-nb
package cerebrumnb

import (
	"context"
	"dhs/internal/plugin"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"dhs/internal/cerebrum-nb/codec"
	"dhs/internal/consumer"
	"dhs/internal/metrics"
	"dhs/internal/transport"
)

func init() {
	consumer.Register(&Factory{})
}

// DefaultPort is the Cerebrum NB WebSocket default port. Configurable
// in the Cerebrum app; the CLI exposes --port to override.
const DefaultPort = 40007

// Factory builds Plugin instances. Registered at init().
type Factory struct{}

func (f *Factory) Meta() consumer.ProtocolMeta {
	return consumer.ProtocolMeta{
		Name:        "cerebrum-nb",
		DefaultPort: DefaultPort,
		Description: "EVS Cerebrum Northbound API (XML over WebSocket; a.k.a. Neuron Bridge)",
	}
}

func (f *Factory) New(deps plugin.Deps) consumer.Protocol {
	deps = deps.WithDefaults()
	p := NewPlugin(deps.Logger)
	p.met = deps.Metrics
	p.Configure(deps.Net, defaultKeepAliveTimeout)
	return p
}

// Plugin is the consumer-side handle. It wraps a single WebSocket
// session (one connection per Plugin) and routes RX events to
// subscribers.
type Plugin struct {
	// Health supplies SessionHealth. Inherited, not reimplemented: what is
	// Cerebrum's is the stale window — the keep-alive timeout it already
	// judges a dead link by — and the Session as the time source.
	consumer.Health

	logger *slog.Logger

	// met counts every XML document in and out. Supplied rather than
	// created, so the process scrapes every connector from one place.
	met *metrics.Connector

	// Username / Password come from CLI flags or the
	// DHS_CEREBRUM_USER / DHS_CEREBRUM_PASS env vars. Optional —
	// LOGIN is not required to open a Cerebrum NB session (verified
	// with EVS support 2026-04-30). Set them only if you need to
	// call Login() explicitly after Connect.
	Username string
	Password string

	// UseTLS opts the dial URL into wss:// instead of ws://.
	UseTLS bool

	// InsecureSkipVerify disables TLS cert validation. Only honoured
	// when UseTLS is true.
	InsecureSkipVerify bool

	// Capture, when set, records every TX/RX XML document (the ws text
	// payload) to a JSONL wire-trace at this path — the same --capture
	// contract every other connector honours (#242). Set before Connect.
	Capture string

	// CaptureMeta, when non-nil, is written as LINE ONE of the capture
	// (ADR-0028 self-description: CLI, binary identity, wire context).
	// Typically a wiretrace.MetaRecord built by the CLI; opaque here.
	// Set before Connect.
	CaptureMeta any

	mu      sync.Mutex
	session *Session

	// devSubs tracks canonical DEVICE VALUE subscriptions by dotted
	// path (D2 — device_canonical.go) so Unsubscribe can cancel both
	// the local dispatch and the wire row.
	devSubMu sync.Mutex
	devSubs  map[string]*deviceSub
}

// NewPlugin constructs a Plugin with the given logger. Credentials
// must be set on the returned Plugin before Connect.
func NewPlugin(logger *slog.Logger) *Plugin {
	if logger == nil {
		logger = slog.Default()
	}
	return &Plugin{logger: logger.With(slog.String("plugin", "cerebrum-nb"))}
}

// Connect dials the Cerebrum WebSocket endpoint. Cerebrum has no path
// on the URL — host:port only.
//
// LOGIN is performed automatically when both Username and Password are
// set on the Plugin: actions (DEVICE_CHANGE, CATEGORY_CHANGE, ROUTING
// ACTION, …) require an authenticated session and the server will
// otherwise NACK with `NOT_LOGGED_IN` (§6 ID 6). When credentials are
// not provided, Connect skips LOGIN and dials only — useful for
// connection diagnostics, but every action will fail until Login() is
// called.
//
// Persistence at the transport layer is guaranteed by TCP keep-alive
// in both directions; no application-level keep-alive is required or
// emitted (verified 2026-04-30, see CLAUDE.md "Session persistence").
func (p *Plugin) Connect(ctx context.Context, host string, port int) error {
	if port == 0 {
		port = DefaultPort
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

	var rec *transport.Recorder
	if p.Capture != "" {
		r, rerr := transport.NewRecorder(p.Capture)
		if rerr != nil {
			return fmt.Errorf("cerebrum-nb: --capture: %w", rerr)
		}
		rec = r
		rec.WriteMeta(p.CaptureMeta) // nil-safe; ADR-0028 line one
	}

	sess, err := newSession(ctx, p.logger, url, transport.TLSOptions{
		Enable:   p.UseTLS,
		Insecure: p.InsecureSkipVerify,
	}, rec, p.met)
	if err != nil {
		_ = rec.Close() // nil-safe; don't leak the file on dial failure
		return err
	}

	if p.Username != "" && p.Password != "" {
		if err := sess.login(ctx, p.Username, p.Password); err != nil {
			_ = sess.close()
			return err
		}
	} else {
		p.logger.Info("connected without LOGIN; actions will NACK with NOT_LOGGED_IN until Login() is called",
			slog.String("host", host),
		)
	}
	p.session = sess
	p.Opened("tcp", host, port, sessionTimes{sess})
	return nil
}

// Login sends a LOGIN frame using the credentials set on the Plugin
// and blocks for the LOGIN_REPLY. Optional — Cerebrum servers accept
// most NB verbs without an authenticated session. Returns an error if
// Username or Password are empty, or if the server NACKs the login.
func (p *Plugin) Login(ctx context.Context) error {
	sess := p.Session()
	if sess == nil {
		return fmt.Errorf("cerebrum-nb: not connected (call Connect first)")
	}
	if p.Username == "" || p.Password == "" {
		return fmt.Errorf("cerebrum-nb: Login: Username and Password must be set on the Plugin")
	}
	return sess.login(ctx, p.Username, p.Password)
}

// Disconnect tears down the WebSocket. Idempotent.
func (p *Plugin) Disconnect() error {
	p.mu.Lock()
	sess := p.session
	p.session = nil
	p.Closed()
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

// ----- consumer.Protocol shim methods -----
//
// Cerebrum is a control-plane (router-of-routers), not a card-frame
// device. The slot/walk/getValue/setValue interface bends; we map the
// most useful subset and return clear errors for the rest. CLI verbs
// for Cerebrum-specific shapes (listen, list-devices, list-routers,
// walk) live in cmd/dhs/cerebrum_nb*.go and call Session() methods
// directly.

// GetDeviceInfo polls Cerebrum and returns the redundancy snapshot as
// DeviceInfo. ProtocolVersion is parsed from the login_reply api_ver.
func (p *Plugin) GetDeviceInfo(ctx context.Context) (consumer.DeviceInfo, error) {
	sess := p.Session()
	if sess == nil {
		return consumer.DeviceInfo{}, fmt.Errorf("cerebrum-nb: not connected")
	}
	if _, err := sess.Poll(ctx); err != nil {
		return consumer.DeviceInfo{}, err
	}
	host, port := sess.RemoteHostPort()
	return consumer.DeviceInfo{
		IP:              host,
		Port:            port,
		ProtocolVersion: sess.APIVersionMajor(),
	}, nil
}

// GetSlotInfo / Walk / GetValue / SetValue do not map cleanly onto
// Cerebrum's control-plane shape — the richer Session methods are the
// supported entry points.

func (p *Plugin) GetSlotInfo(_ context.Context, _ int) (consumer.SlotInfo, error) {
	return consumer.SlotInfo{}, fmt.Errorf("cerebrum-nb: slot-info not applicable; use Session.ListDevices / ListRouters")
}

func (p *Plugin) Walk(_ context.Context, _ int) ([]consumer.Object, error) {
	return nil, fmt.Errorf("cerebrum-nb: walk not applicable; use Session.WalkAll")
}

// GetValue is implemented in device_canonical.go (D2 — the DEVICE
// domain on the canonical Tree/DM contract).

func (p *Plugin) SetValue(ctx context.Context, req consumer.ValueRequest, val consumer.Value) (consumer.Value, error) {
	sess := p.Session()
	if sess == nil {
		return consumer.Value{}, fmt.Errorf("cerebrum-nb: not connected")
	}
	if req.Path == "" {
		return consumer.Value{}, fmt.Errorf("cerebrum-nb: set-value requires Path of form 'device.sub_device.object'")
	}
	if val.Kind != consumer.KindString {
		return consumer.Value{}, fmt.Errorf("cerebrum-nb: set-value: only string values mapped to <device SET_VALUE> (got %s)", val.Kind)
	}
	devName, sub, obj, err := splitDevicePath(req.Path)
	if err != nil {
		return consumer.Value{}, err
	}
	body := &codec.DeviceAction{
		Type:       "SET_VALUE",
		DeviceName: devName,
		SubDevice:  sub,
		Object:     obj,
		Value:      val.Str,
	}
	if err := sess.Action(ctx, body); err != nil {
		return consumer.Value{}, err
	}
	return val, nil
}

// Subscribe / Unsubscribe are implemented in device_canonical.go (D2):
// canonical DEVICE.SUB.OBJECT… paths onto §5.4 VALUE subscriptions.
// Routing/category/salvo subscriptions keep their precise §5 addressing
// through Session.Subscribe<X> (the Matrix-template half).

// Metrics returns the connector's counter set — XML documents and bytes in
// and out, plus errors. Satisfies the optional interface the CLI
// type-asserts for --metrics-addr.
func (p *Plugin) Metrics() *metrics.Connector {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.met
}

// sessionTimes adapts a Session to consumer.RxTxTimes.
//
// The session stamps rx only (noteRX, on every inbound frame), which is what
// Live is derived from. There is no single write path to stamp for tx, so
// LastTx is reported as unknown rather than invented.
type sessionTimes struct{ s *Session }

func (t sessionTimes) LastRx() time.Time { return t.s.LastRx() }
func (t sessionTimes) LastTx() time.Time { return time.Time{} }
