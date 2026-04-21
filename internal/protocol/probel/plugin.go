// Package probel is the Probel SW-P-08 consumer plugin — it opens a TCP
// session to a matrix controller (rx side) and exposes matrix / crosspoint
// operations through the protocol.Protocol interface.
//
// Layering (mirror of acp1 / acp2 / emberplus consumer packages):
//
//	plugin.go                    Factory + Protocol interface stubs
//	types.go                     package-level constants (DefaultPort)
//	crosspoint_*.go              per-command consumer methods
//	protect_*.go                 per-command consumer methods
//	maintenance.go               per-command consumer method
//	dual_controller_status.go    per-command consumer method
//	master_protect_connect.go    per-command consumer method
//
// Wire codec lives at internal/probel/ (stdlib-only, shared with the
// provider plugin under internal/provider/probel/).
package probel

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	iprobel "acp/internal/probel"
	"acp/internal/protocol"
	"acp/internal/transport"
)

func init() {
	protocol.Register(&Factory{})
}

// Factory registers the Probel consumer plugin with the compile-time
// protocol registry.
type Factory struct{}

// Meta publishes the static descriptor used by the CLI + API.
func (f *Factory) Meta() protocol.ProtocolMeta {
	return protocol.ProtocolMeta{
		Name:        "probel",
		DefaultPort: DefaultPort,
		Description: "Probel SW-P-08 / SW-P-88 matrix controller (TCP)",
	}
}

// New constructs a fresh consumer plugin bound to the given logger.
func (f *Factory) New(logger *slog.Logger) protocol.Protocol {
	return &Plugin{logger: logger}
}

// Plugin is the Probel Protocol implementation. One instance talks to
// one matrix (host:port). Holds the TCP client and per-session state
// that individual commands populate (source/destination name caches,
// tie-line tally cache, etc.) as their PRs land.
type Plugin struct {
	logger *slog.Logger

	mu       sync.Mutex
	host     string
	port     int
	client   *iprobel.Client
	recorder *transport.Recorder
}

// SetRecorder attaches a JSONL traffic recorder to this plugin. Call
// before Connect — the recorder is wired into the iprobel.Client at
// Dial time and captures every TX and RX frame (including DLE ACK /
// DLE NAK control sequences) in the same format the ACP1/ACP2/Ember+
// plugins produce.
func (p *Plugin) SetRecorder(rec *transport.Recorder) {
	p.mu.Lock()
	p.recorder = rec
	p.mu.Unlock()
}

// Connect opens a TCP session to the matrix. Idempotent when called
// twice with the same endpoint. Port 0 resolves to DefaultPort.
func (p *Plugin) Connect(ctx context.Context, ip string, port int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.client != nil {
		if p.host == ip && (port == 0 || port == p.port) {
			return nil
		}
		return fmt.Errorf("probel: already connected to %s:%d", p.host, p.port)
	}
	if port == 0 {
		port = DefaultPort
	}

	addr := fmt.Sprintf("%s:%d", ip, port)
	cfg := iprobel.ClientConfig{}
	if p.recorder != nil {
		rec := p.recorder
		cfg.OnTx = func(b []byte) { rec.Record("probel", "tx", b) }
		cfg.OnRx = func(b []byte) { rec.Record("probel", "rx", b) }
	}
	cli, err := iprobel.Dial(ctx, addr, p.logger, cfg)
	if err != nil {
		return &protocol.TransportError{Op: "connect", Err: err}
	}
	p.client = cli
	p.host = ip
	p.port = port
	p.logger.Info("probel connected",
		slog.String("host", ip),
		slog.Int("port", port),
	)
	return nil
}

// Disconnect closes the TCP session. Safe to call on an unconnected plugin.
func (p *Plugin) Disconnect() error {
	p.mu.Lock()
	cli := p.client
	p.client = nil
	p.host = ""
	p.port = 0
	p.mu.Unlock()
	if cli == nil {
		return nil
	}
	return cli.Close()
}

// client returns the in-flight TCP client, or ErrNotConnected. Helper
// for per-command methods added by follow-up PRs.
func (p *Plugin) getClient() (*iprobel.Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client == nil {
		return nil, protocol.ErrNotConnected
	}
	return p.client, nil
}

// ExposeClient returns the underlying iprobel.Client for callers that
// need direct Subscribe / raw frame access (e.g. the CLI's watch
// subcommand, which just prints every async tally it sees). Returns
// ErrNotConnected before Connect fires.
func (p *Plugin) ExposeClient() (*iprobel.Client, error) {
	return p.getClient()
}

// GetDeviceInfo is not applicable to SW-P-08 (no rack-controller identity
// object). Per-command PRs may populate a synthetic DeviceInfo derived
// from Dual-Controller-Status / Device-Name-Request replies.
func (p *Plugin) GetDeviceInfo(ctx context.Context) (protocol.DeviceInfo, error) {
	if _, err := p.getClient(); err != nil {
		return protocol.DeviceInfo{}, err
	}
	return protocol.DeviceInfo{IP: p.host, Port: p.port}, nil
}

// GetSlotInfo — slots are not a Probel concept. Returns NotImplemented
// until we decide whether to synthesise matrix/level as slot pairs.
func (p *Plugin) GetSlotInfo(ctx context.Context, slot int) (protocol.SlotInfo, error) {
	return protocol.SlotInfo{}, protocol.ErrNotImplemented
}

// Walk enumerates matrices / levels / destinations / sources. Lands in the
// "source + destination names + sizes" per-command PR.
func (p *Plugin) Walk(ctx context.Context, slot int) ([]protocol.Object, error) {
	return nil, protocol.ErrNotImplemented
}

// GetValue — for Probel, GetValue on a crosspoint returns its current
// source. Lands in the CrosspointInterrogate per-command PR.
func (p *Plugin) GetValue(ctx context.Context, req protocol.ValueRequest) (protocol.Value, error) {
	return protocol.Value{}, protocol.ErrNotImplemented
}

// SetValue — for Probel, SetValue on a crosspoint connects the named
// source. Lands in the CrosspointConnect per-command PR.
func (p *Plugin) SetValue(ctx context.Context, req protocol.ValueRequest, val protocol.Value) (protocol.Value, error) {
	return protocol.Value{}, protocol.ErrNotImplemented
}

// Subscribe attaches a callback for async tallies. The wiring of
// iprobel.Client.Subscribe into protocol.Event lands in the
// CrosspointTally per-command PR.
func (p *Plugin) Subscribe(req protocol.ValueRequest, fn protocol.EventFunc) error {
	return protocol.ErrNotImplemented
}

// Unsubscribe removes a tally callback. Pairs with Subscribe.
func (p *Plugin) Unsubscribe(req protocol.ValueRequest) error {
	return protocol.ErrNotImplemented
}
