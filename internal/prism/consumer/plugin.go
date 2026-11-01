// Package prism is the consumer of the Telestream PRISM waveform monitor.
//
// The API is the page the instrument serves at /api/help: one REST call
// per setting or measurement, compiled into internal/prism/codec. The
// page documents no push channel, so watch polls. This connector has
// never met a real PRISM: internal/prism/CLAUDE.md lists what is open.
package prism

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	dhsc "dhs/internal/consumer"
	"dhs/internal/consumer/pollwatch"
	"dhs/internal/plugin"
	"dhs/internal/prism/codec"
	transporthttp "dhs/internal/transport/http"
)

// Registry name and port. The page's text says 9000; its row links, the
// page having been served from them, use port 80.
const (
	Name            = "prism"
	DefaultPort     = 9000
	staleAfter      = 90 * time.Second
	defaultInterval = 2 * time.Second
)

// Environment the plugin reads at Connect.
const (
	EnvPass         = "DHS_PRISM_PASS"          // sent as "Authorization: Basic <password>"
	EnvScalar       = "DHS_PRISM_SCALAR"        // array (default) or bare
	EnvAnswerWait   = "DHS_PRISM_ANSWER_WAIT"   // one answer's wait where no --timeout applies (walk, watch)
	EnvAllowActions = "DHS_PRISM_ALLOW_ACTIONS" // "1" opens the guarded calls to set
)

func init() {
	dhsc.Register(&Factory{})
}

// Factory builds Plugin instances.
type Factory struct{}

// Meta describes the plugin to the registry.
func (f *Factory) Meta() dhsc.ProtocolMeta {
	return dhsc.ProtocolMeta{
		Name:        Name,
		DefaultPort: DefaultPort,
		Description: "Telestream PRISM waveform monitor (REST/JSON, the instrument's /api/help page)",
	}
}

// New builds a plugin from the injected dependency set.
func (f *Factory) New(deps plugin.Deps) dhsc.Protocol {
	deps = deps.WithDefaults()
	p := &Plugin{deps: deps, interval: defaultInterval}
	p.cat, p.catErr = codec.Load()
	p.Init(deps, staleAfter)
	p.poller = pollwatch.New(p, p.pollProfileFor, deps.Logger, deps.Clock)
	return p
}

// Options is the session configuration; Connect fills it from the
// environment when the caller did not set it (tests set it directly).
type Options struct {
	Password     string
	Scalar       codec.ScalarForm
	AnswerWait   time.Duration
	AllowActions bool
}

// Plugin is one instrument: slot 0.
type Plugin struct {
	dhsc.Base

	deps   plugin.Deps
	opts   *Options
	cat    *codec.Catalogue
	catErr error
	poller *pollwatch.Poller

	mu       sync.Mutex
	rest     *rest
	host     string
	port     int
	scalar   codec.ScalarForm
	allow    bool
	interval time.Duration
}

// SetOptions fixes the session configuration instead of the environment.
func (p *Plugin) SetOptions(o Options) { p.opts = &o }

// PathNative: a --path names a call of the catalogue, no walk needed.
func (p *Plugin) PathNative() bool { return true }

// SetPollInterval sets how often a watch re-reads each call.
func (p *Plugin) SetPollInterval(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.interval = d
}

func (p *Plugin) options() (Options, error) {
	if p.opts != nil {
		return *p.opts, nil
	}
	o := Options{Password: os.Getenv(EnvPass), AllowActions: os.Getenv(EnvAllowActions) == "1"}
	var err error
	if o.Scalar, err = codec.ParseScalarForm(os.Getenv(EnvScalar)); err != nil {
		return o, err
	}
	if s := os.Getenv(EnvAnswerWait); s != "" {
		if o.AnswerWait, err = time.ParseDuration(s); err != nil {
			return o, fmt.Errorf("prism: %s: %w", EnvAnswerWait, err)
		}
	}
	return o, nil
}

// Connect opens no socket of its own: one small read proves an API
// answers at this address.
func (p *Plugin) Connect(ctx context.Context, ip string, port int) error {
	if p.catErr != nil {
		return p.catErr
	}
	if port <= 0 {
		port = DefaultPort
	}
	o, err := p.options()
	if err != nil {
		return err
	}
	client := transporthttp.NewClient()
	client.Metrics = p.Metrics()
	client.Recorder = p.Recorder()
	r := newREST(fmt.Sprintf("http://%s:%d", ip, port), client, o.Password, o.AnswerWait)

	p.mu.Lock()
	p.rest, p.host, p.port, p.scalar, p.allow = r, ip, port, o.Scalar, o.AllowActions
	p.mu.Unlock()

	e, _ := p.cat.Lookup("software_version")
	if _, err := p.read(ctx, e, codec.Target{Name: e.Name}); err != nil {
		p.mu.Lock()
		p.rest = nil
		p.mu.Unlock()
		var te *dhsc.TransportError
		if errors.As(err, &te) {
			return fmt.Errorf("prism: %s:%d: %w (the page names port 9000 and links port 80: try <host>:80)", ip, port, err)
		}
		return fmt.Errorf("prism: %s:%d: %w", ip, port, err)
	}
	p.Opened("tcp", ip, port, dhsc.MetricsTimes{C: p.Metrics()})
	return nil
}

// Disconnect stops the polling and reports what the session absorbed.
func (p *Plugin) Disconnect() error {
	p.poller.Close()
	p.mu.Lock()
	p.rest = nil
	p.mu.Unlock()
	if prof := p.ComplianceProfile(); prof.Classification() != "strict" {
		p.deps.Logger.Warn("prism: deviations from the page", "events", prof.SummaryLine())
	}
	p.Closed()
	return nil
}

func (p *Plugin) session() (*rest, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.rest == nil {
		return nil, fmt.Errorf("%w: prism", dhsc.ErrNotConnected)
	}
	return p.rest, nil
}

// GetDeviceInfo: an instrument is one box, one slot.
func (p *Plugin) GetDeviceInfo(ctx context.Context) (dhsc.DeviceInfo, error) {
	if _, err := p.session(); err != nil {
		return dhsc.DeviceInfo{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return dhsc.DeviceInfo{IP: p.host, Port: p.port, NumSlots: 1}, nil
}

// GetSlotInfo reads the identity calls. A call that fails lands under
// "<call>.error", never as a value of the call.
func (p *Plugin) GetSlotInfo(ctx context.Context, slot int) (dhsc.SlotInfo, error) {
	if slot != 0 {
		return dhsc.SlotInfo{}, fmt.Errorf("prism: slot %d: an instrument has slot 0 only", slot)
	}
	if _, err := p.session(); err != nil {
		return dhsc.SlotInfo{}, err
	}
	id := map[string]string{}
	next := answers(ctx)
	for _, call := range identityCalls() {
		if errors.Is(ctx.Err(), context.Canceled) {
			return dhsc.SlotInfo{}, ctx.Err()
		}
		e, t, _ := p.resolve(dhsc.ValueRequest{Path: call})
		c, cancel := next()
		reply, err := p.read(c, e, t)
		cancel()
		if err != nil {
			id[call+".error"] = err.Error()
			continue
		}
		id[call] = reply.Text
	}
	return dhsc.SlotInfo{Slot: 0, Status: dhsc.SlotPresent, State: dhsc.SlotStatePresent, IsOnline: true, Identity: id}, nil
}

func identityCalls() []string {
	return []string{
		"software_version", "nomenclature", "serial_number", "hardware_version_info/slot0",
		"system_status", "input_status", "ptp_status", "ip_signal_status/IP1", "ip_signal_status/IP2",
		"nmos_api_version",
	}
}

// answers gives each exchange of a verb the wait the verb was given. The
// CLI hands a verb one --timeout; a verb of several exchanges inside it
// would turn one answer's wait into a whole-command limit. Ctrl-C still
// stops the verb.
func answers(ctx context.Context) func() (context.Context, context.CancelFunc) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return func() (context.Context, context.CancelFunc) { return context.WithCancel(ctx) }
	}
	wait := time.Until(deadline)
	base, stop := context.WithCancel(context.WithoutCancel(ctx))
	context.AfterFunc(ctx, func() {
		if errors.Is(ctx.Err(), context.Canceled) {
			stop()
		}
	})
	return func() (context.Context, context.CancelFunc) { return context.WithTimeout(base, wait) }
}
