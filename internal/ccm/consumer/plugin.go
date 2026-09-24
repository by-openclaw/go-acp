package consumer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"dhs/internal/ccm/codec"
	dhsc "dhs/internal/consumer"
	"dhs/internal/consumer/pollwatch"
	"dhs/internal/plugin"
)

// The client above is CCM's own shape — walk the streams, export the
// model. This file is the other face: the same client behind the
// neutral consumer.Protocol, so a Neuron answers `info`, `tree`,
// `get`, `export`, `watch` and `alarm` like every other device here,
// with no REST in the command line.
//
// Two things make the neutral face better informed than the CCM one:
//
//   - the DEVICE'S OWN api.yml decides what exists and what may be
//     written. A tree walk learns the shape and misses the rest: on
//     BRIDGE 7.0.3 `/misc` does not list `luts`, `/io/sdi` answers
//     with an array of objects so the walk never reaches the per-UUID
//     resources under it, and nothing in any GET says which resources
//     accept a PUT. The spec says all three — 75 GETs and 36 PUTs
//     where a walk finds 41 resources;
//   - so every object carries real access bits, and an operator (or
//     `alarm suggest`) can tell a setting from a reading.
//
// REST only, by decision — see ../CLAUDE.md. `watch` polls.

// Name is the registry name: dhs consumer ccm <verb>.
const Name = "ccm"

// DefaultPort is the REST API's port. The Neuron serves https on 443
// and the client builds https://<host>/api/v1 from it.
const DefaultPort = 443

// staleAfter is how long a polled value is treated as current.
const staleAfter = 30 * time.Second

// defaultInterval is how often a watch re-reads each object when the
// operator names no cadence. REST polling costs a round trip per
// resource, so this is deliberately slower than a push protocol's.
const defaultInterval = 15 * time.Second

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
		Description: "EVS Neuron CCM (REST/JSON over TLS) — the model is the device's own api.yml",
	}
}

// New builds a plugin from the injected dependency set.
func (f *Factory) New(deps plugin.Deps) dhsc.Protocol {
	deps = deps.WithDefaults()
	p := &Plugin{deps: deps, interval: defaultInterval}
	p.Init(deps, staleAfter)
	p.poller = pollwatch.New(p, p.pollProfileFor, deps.Logger, deps.Clock)
	return p
}

// Plugin is one Neuron. A bridge is a box, so everything lives on slot
// 0 — the shape every other single-box connector uses.
type Plugin struct {
	dhsc.Base

	deps plugin.Deps

	mu       sync.Mutex
	client   *Client
	host     string
	port     int
	dev      codec.Device
	spec     *codec.Spec
	tree     []dhsc.Object
	byPath   map[string]dhsc.Object
	interval time.Duration

	poller *pollwatch.Poller
}

// Connect reads the device's identity and its OpenAPI document.
//
// The spec is fetched at connect and not on demand: everything after
// this point — which paths exist, which accept a write, what a `--path`
// resolves to — is answered from it, and a device that cannot produce
// it is one this connector would be guessing about.
func (p *Plugin) Connect(ctx context.Context, ip string, port int) error {
	if port <= 0 {
		port = DefaultPort
	}
	host := ip
	if port != DefaultPort {
		host = fmt.Sprintf("%s:%d", ip, port)
	}
	client := dialClient(host)

	dev, _, err := client.Walk(ctx)
	if err != nil {
		return fmt.Errorf("ccm: %s: %w", host, err)
	}
	doc, err := client.FetchSpec(ctx)
	if err != nil {
		return fmt.Errorf("ccm: %s: api.yml: %w", host, err)
	}
	spec, err := codec.ParseSpec(doc)
	if err != nil {
		return fmt.Errorf("ccm: %s: %w", host, err)
	}

	p.mu.Lock()
	p.client, p.host, p.port = client, ip, port
	p.dev, p.spec = *dev, spec
	p.tree, p.byPath = nil, nil
	p.mu.Unlock()

	p.Opened("tcp", ip, port, dhsc.MetricsTimes{C: p.Metrics()})
	return nil
}

// Disconnect drops the client. HTTP keeps no session, so this is
// bookkeeping — but a connector that stayed "connected" after being
// told to stop would report a device as reachable that nobody is
// talking to.
func (p *Plugin) Disconnect() error {
	p.mu.Lock()
	p.client = nil
	p.tree, p.byPath = nil, nil
	p.mu.Unlock()
	p.Closed()
	return nil
}

// GetDeviceInfo answers from /self. A bridge is one box: one slot,
// always present.
func (p *Plugin) GetDeviceInfo(ctx context.Context) (dhsc.DeviceInfo, error) {
	p.mu.Lock()
	dev, host, port, connected := p.dev, p.host, p.port, p.client != nil
	p.mu.Unlock()
	if !connected {
		return dhsc.DeviceInfo{}, errNotConnected
	}
	return dhsc.DeviceInfo{
		IP: host, Port: port, NumSlots: 1,
		// The REST API's own version, which is what /api/v1 means and
		// what changes when EVS breaks compatibility.
		ProtocolVersion: 1,
		// The firmware, which is what a DM and an alarm template are
		// keyed by and what an operator reads in `info`.
		DtdVersion: dev.ProductVersion,
	}, nil
}

// GetSlotInfo answers for the one slot a bridge has.
func (p *Plugin) GetSlotInfo(ctx context.Context, slot int) (dhsc.SlotInfo, error) {
	p.mu.Lock()
	connected := p.client != nil
	p.mu.Unlock()
	if !connected {
		return dhsc.SlotInfo{}, errNotConnected
	}
	if slot != 0 {
		return dhsc.SlotInfo{Slot: slot, Status: dhsc.SlotNoCard}, nil
	}
	return dhsc.SlotInfo{
		Slot: 0, Status: dhsc.SlotPresent, State: dhsc.SlotStatePresent, IsOnline: true,
	}, nil
}

// IdentityProbe answers "which device am I talking to" the way
// ADR-0022 asks: Model@SwRev, so a per-model alarm template and a
// per-model DM find each other. /self carries both.
func (p *Plugin) IdentityProbe(ctx context.Context, slot int) (string, error) {
	p.mu.Lock()
	dev, connected := p.dev, p.client != nil
	p.mu.Unlock()
	if !connected {
		return "", errNotConnected
	}
	if dev.ProductName == "" || dev.ProductVersion == "" {
		return "", fmt.Errorf("ccm: /self named %q@%q", dev.ProductName, dev.ProductVersion)
	}
	return dev.ProductName + "@" + dev.ProductVersion, nil
}

// MinOpTimeout is the floor this connector needs for one operation.
//
// A walk here is hundreds of HTTPS round trips against a device that
// is also passing media; the CLI's default per-operation timeout is
// shorter than that, and a walk cut off mid-way looks like a device
// that stopped answering.
func (p *Plugin) MinOpTimeout() time.Duration { return 2 * time.Minute }

var errNotConnected = fmt.Errorf("ccm: not connected")

// dialClient builds the client a session talks through.
//
// A variable so a test can point the connector at an http test server:
// the client derives an https base from the host, and a fake device on
// loopback has no certificate. Production never reassigns it.
var dialClient = func(host string) *Client { return New(Options{Host: host}) }
