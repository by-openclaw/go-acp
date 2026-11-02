// Package virtuoso is the consumer of the Nevion Virtuoso MI REST API
// (Nevion Virtuoso MI User Guide, "Guide to the REST API").
//
// A chassis is an Element Manager and up to eight extension cards, each
// with the same API: slot 0 here is the manager, slots 1..8 the cards. The
// object list of a slot comes from that module's own schema.json joined
// with its data document, never from a list kept here.
//
// Proven on a fake only. See ../docs/runbook.md before pointing it at a
// real chassis.
package virtuoso

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	dhsc "dhs/internal/consumer"
	"dhs/internal/consumer/pollwatch"
	"dhs/internal/plugin"
	"dhs/internal/transport"
	transporthttp "dhs/internal/transport/http"
	"dhs/internal/virtuoso/codec"
)

// Registry name, ports (guide, "Network Configuration") and pacing.
const (
	Name        = "virtuoso"
	DefaultPort = 80  // HTTP
	SecurePort  = 443 // HTTPS

	numSlots   = codec.MaxSlot + 1
	staleAfter = 90 * time.Second

	// answerWait is the only time limit: how long one answer may take.
	// 20 s is what the operator's capture scripts used on this device; it
	// is not measured beyond that.
	answerWait = 20 * time.Second

	// defaultInterval stays inside the guide's 3-minute idle session limit.
	defaultInterval = 30 * time.Second

	// pollGap is the least time between two watch reads. A chosen
	// conservative value, not a device limit: the guide gives none.
	pollGap = 200 * time.Millisecond
)

// Environment read at Connect. Secrets never travel as flags.
const (
	EnvUser     = "DHS_VIRTUOSO_USER"
	EnvPass     = "DHS_VIRTUOSO_PASS"
	EnvTLS      = "DHS_VIRTUOSO_TLS"      // "1": HTTPS (implied by port 443)
	EnvInsecure = "DHS_VIRTUOSO_INSECURE" // "1": accept a self-signed certificate
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
		Description: "Nevion Virtuoso MI chassis and cards (REST/JSON) — the model is the device's own schema",
	}
}

// New builds a plugin from the injected dependency set.
func (f *Factory) New(deps plugin.Deps) dhsc.Protocol {
	deps = deps.WithDefaults()
	p := &Plugin{deps: deps, interval: defaultInterval, gap: pollGap, wait: answerWait}
	p.Init(deps, staleAfter)
	p.poller = pollwatch.New(paced{p}, p.pollProfileFor, deps.Logger, deps.Clock)
	return p
}

// Options is the session configuration; Connect fills it from the
// environment when the caller did not set it (tests set it directly).
type Options struct {
	User, Pass string
	TLS        bool
	Insecure   bool
}

// Plugin is one chassis.
type Plugin struct {
	dhsc.Base

	deps     plugin.Deps
	opts     *Options
	wait     time.Duration
	gap      time.Duration
	poller   *pollwatch.Poller
	interval time.Duration

	// walkMu keeps walks one at a time: the slots of a chassis are read
	// sequentially, and a watch and its startup walk share one result.
	walkMu sync.Mutex

	mu      sync.Mutex
	rest    *rest
	host    string
	port    int
	swver   string
	schemas map[int]*codec.Schema
	trees   map[int][]dhsc.Object
}

// SetOptions fixes the session configuration instead of the environment.
func (p *Plugin) SetOptions(o Options) { p.opts = &o }

// PathNative: a --path is a URL path, answered with no walk.
func (p *Plugin) PathNative() bool { return true }

// MinOpTimeout keeps the CLI's per-operation deadline from ever being the
// limit: info reads nine modules one after the other, each with its own
// answer wait.
func (p *Plugin) MinOpTimeout() time.Duration { return (numSlots + 1) * p.wait }

func (p *Plugin) options(port int) Options {
	if p.opts != nil {
		return *p.opts
	}
	return Options{
		User:     os.Getenv(EnvUser),
		Pass:     os.Getenv(EnvPass),
		TLS:      os.Getenv(EnvTLS) == "1" || port == SecurePort,
		Insecure: os.Getenv(EnvInsecure) == "1",
	}
}

// Connect logs in when credentials are set, then reads the manager's
// identity, which proves the session.
func (p *Plugin) Connect(ctx context.Context, ip string, port int) error {
	if port <= 0 {
		port = DefaultPort
	}
	o := p.options(port)
	scheme := "http"
	client := transporthttp.NewClient()
	if o.TLS {
		scheme = "https"
		c, err := transporthttp.NewTLSClient(transport.TLSOptions{Enable: true, Insecure: o.Insecure}, p.wait)
		if err != nil {
			return fmt.Errorf("virtuoso: tls: %w", err)
		}
		client = c
	}
	client.HTTP.Timeout = p.wait
	client.Metrics = p.Metrics()
	client.Recorder = p.Recorder()
	r := newREST(fmt.Sprintf("%s://%s:%d", scheme, ip, port), client, o, p.deps.Logger, p.ComplianceProfile())
	if o.User != "" {
		if err := r.login(ctx); err != nil {
			return fmt.Errorf("virtuoso: %s:%d: %w", ip, port, err)
		}
	}
	info, err := p.product(ctx, r, 0)
	if err != nil {
		r.logout()
		return fmt.Errorf("virtuoso: %s:%d: %w", ip, port, err)
	}

	p.mu.Lock()
	old := p.rest
	p.rest, p.host, p.port, p.swver = r, ip, port, string(info.SwVer)
	p.schemas, p.trees = map[int]*codec.Schema{}, map[int][]dhsc.Object{}
	p.mu.Unlock()
	if old != nil {
		old.logout()
	}
	p.Opened("tcp", ip, port, dhsc.MetricsTimes{C: p.Metrics()})
	return nil
}

// Disconnect stops every watch and ends the session.
func (p *Plugin) Disconnect() error {
	p.poller.Close()
	p.mu.Lock()
	r := p.rest
	p.rest, p.schemas, p.trees = nil, nil, nil
	p.mu.Unlock()
	if r != nil {
		r.logout()
	}
	if line := p.ComplianceProfile().SummaryLine(); line != "" {
		p.deps.Logger.Warn("virtuoso compliance", "summary", line)
	}
	p.Closed()
	return nil
}

func (p *Plugin) session() (*rest, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.rest == nil {
		return nil, fmt.Errorf("%w: virtuoso", dhsc.ErrNotConnected)
	}
	return p.rest, nil
}

func (p *Plugin) product(ctx context.Context, r *rest, slot int) (codec.ProductInfo, error) {
	env, err := r.do(ctx, "GET", codec.ProductInfoTarget(slot))
	if err != nil {
		return codec.ProductInfo{}, err
	}
	return codec.DecodeProductInfo(env)
}

// GetDeviceInfo: the manager plus eight card slots. The API publishes no
// version of its own; the manager's software version stands in for it.
func (p *Plugin) GetDeviceInfo(ctx context.Context) (dhsc.DeviceInfo, error) {
	if _, err := p.session(); err != nil {
		return dhsc.DeviceInfo{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return dhsc.DeviceInfo{IP: p.host, Port: p.port, NumSlots: numSlots, DtdVersion: p.swver}, nil
}

// GetSlotInfo reads one module's identity. A card that does not answer is
// reported absent, with the reason in its identity. A module that answers
// with a status other than ok is an error, and so is a session the device
// refuses: neither says the slot is empty.
func (p *Plugin) GetSlotInfo(ctx context.Context, slot int) (dhsc.SlotInfo, error) {
	r, err := p.session()
	if err != nil {
		return dhsc.SlotInfo{}, err
	}
	if !codec.ValidSlot(slot) {
		return dhsc.SlotInfo{}, errSlotRange(slot)
	}
	info, err := p.product(ctx, r, slot)
	if err != nil {
		if slot > 0 && noAnswer(err) {
			return dhsc.SlotInfo{
				Slot: slot, Status: dhsc.SlotNoCard, State: dhsc.SlotStateNoCard,
				Identity: map[string]string{"reason": absentReason(err)},
			}, nil
		}
		return dhsc.SlotInfo{}, err
	}
	return dhsc.SlotInfo{
		Slot: slot, Status: dhsc.SlotPresent, State: dhsc.SlotStatePresent, IsOnline: true,
		LiveAt: p.Clock().Now(), Identity: info.Fields(),
	}, nil
}

// IdentityProbe is the ADR-0022 key: Model@SwRev, the product name and the
// software version of the module in the slot.
func (p *Plugin) IdentityProbe(ctx context.Context, slot int) (string, error) {
	r, err := p.session()
	if err != nil {
		return "", err
	}
	if !codec.ValidSlot(slot) {
		return "", errSlotRange(slot)
	}
	info, err := p.product(ctx, r, slot)
	if err != nil {
		return "", err
	}
	if info.Prodname == "" || info.SwVer == "" {
		return "", fmt.Errorf("virtuoso: slot %d product_info names %q@%q", slot, info.Prodname, info.SwVer)
	}
	return strings.ReplaceAll(string(info.Prodname), "/", "-") + "@" + string(info.SwVer), nil
}

func errSlotRange(slot int) error {
	return fmt.Errorf("virtuoso: slot %d: 0 is the manager, 1..%d the cards", slot, codec.MaxSlot)
}
