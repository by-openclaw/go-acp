// Package probelsw02p is the Probel SW-P-02 consumer plugin — it opens a
// TCP session to a matrix controller (rx side) and exposes matrix /
// crosspoint operations through the protocol.Protocol interface.
//
// Layering (mirror of acp1 / acp2 / emberplus / probel-sw08p consumer
// packages):
//
//	plugin.go                    Factory + Protocol interface stubs
//	types.go                     package-level constants (DefaultPort)
//	metrics_helpers.go           raw-bytes → CMD id extractor
//	compliance_events.go         compliance event label catalogue
//	cmd_rxNNN_*.go               per-command consumer methods (future)
//	cmd_txNNN_*.go               per-command consumer methods (future)
//
// Wire codec lives at internal/probel-sw02p/codec/ (stdlib-only, shared
// with the provider plugin under internal/probel-sw02p/provider/).
package probelsw02p

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"acp/internal/metrics"
	"acp/internal/probel-sw02p/codec"
	"acp/internal/protocol"
	"acp/internal/protocol/compliance"
	"acp/internal/transport"
)

// DefaultOnlineStaleAfter is the default "no rx traffic observed for
// this long = peer is offline" threshold used by Plugin.IsOnline. Set
// to 90 s, matching the SW-P-08 consumer's default so the cross-
// protocol alive-bit contract stays consistent.
const DefaultOnlineStaleAfter = 90 * time.Second

// DefaultAppKeepaliveSpacing is the rotation spacing between rx 01
// keep-alive pings — one ping every this much wall-clock, advancing
// the dst cursor by 1 (modulo Plugin.Dsts). 2 s mirrors what live VSM
// does in the testbed (~721 × rx 01 sweep at ~2 s/dst). SW-P-02 has
// no in-protocol keep-alive command, so the rx 01 / tx 03 round-trip
// IS the keep-alive.
const DefaultAppKeepaliveSpacing = 2 * time.Second

// DefaultBootstrapSpacing is the spacing between rx 01 sweep frames
// at (re)connect. 10 ms keeps a 1024-dst sweep under ~10 s; tunable
// via MatrixConfig.BootstrapSpacing.
const DefaultBootstrapSpacing = 10 * time.Millisecond

func init() {
	protocol.Register(&Factory{})
}

// Factory registers the SW-P-02 consumer plugin with the compile-time
// protocol registry.
type Factory struct{}

// Meta publishes the static descriptor used by the CLI + API.
func (f *Factory) Meta() protocol.ProtocolMeta {
	return protocol.ProtocolMeta{
		Name:        "probel-sw02p",
		DefaultPort: DefaultPort,
		Description: "Probel SW-P-02 matrix controller (TCP)",
	}
}

// New constructs a fresh consumer plugin bound to the given logger.
func (f *Factory) New(logger *slog.Logger) protocol.Protocol {
	return &Plugin{logger: logger}
}

// MatrixConfig holds the externally-supplied matrix shape. SW-P-02
// has no wire-side discovery primitive that all controllers honour —
// VSM and Commie both configure size + mtxid + level per matrix in
// their UI. Our consumer follows that pattern: caller sets these via
// SetMatrixConfig before Connect.
type MatrixConfig struct {
	// MatrixID is the wire matrix identifier (0-15 in narrow §3.2.3,
	// 0-127 in extended). Default 0.
	MatrixID uint8
	// Level is the wire level identifier (0-15 narrow, 0-27 extended).
	// Default 0.
	Level uint8
	// Dsts is the destination count on this (matrix, level). Required
	// non-zero for InitialPoll / AppKeepalive to do anything.
	Dsts uint16
	// Srcs is the source count on this (matrix, level). Used for
	// validation + label config lookup; not required by the rx 01
	// poll itself.
	Srcs uint16

	// InitialPoll, when true, fires a one-shot rx 01 sweep at every
	// (re)Connect over dst=0..Dsts-1. Default true.
	InitialPoll bool
	// BootstrapSpacing is the wall-clock pacing between rx 01 frames
	// during the bootstrap sweep. Zero = DefaultBootstrapSpacing.
	BootstrapSpacing time.Duration
	// AppKeepaliveSpacing controls the rotating rx 01 keep-alive ping
	// after the bootstrap sweep finishes. Zero =
	// DefaultAppKeepaliveSpacing; negative = disable. SW-P-02 has no
	// keep-alive command in spec, so this is how we mirror the
	// VSM-style continuous-poll heartbeat.
	AppKeepaliveSpacing time.Duration
}

// Plugin is the SW-P-02 Protocol implementation. One instance talks to
// one matrix (host:port). Per-command state (name caches, tally cache,
// etc.) is added as subsequent commits land their PRs.
type Plugin struct {
	logger *slog.Logger

	mu       sync.Mutex
	host     string
	port     int
	client   *codec.Client
	recorder *transport.Recorder

	// matrixCfg holds caller-supplied matrix shape + bootstrap/keep-
	// alive knobs. Set via SetMatrixConfig before Connect; defaults
	// applied at Connect time.
	matrixCfg MatrixConfig

	// keepaliveCancel stops the per-session bootstrap + keep-alive
	// goroutine. Nil unless one is running.
	keepaliveCancel context.CancelFunc

	// profile aggregates wire-tolerance events observed during this
	// session. See compliance_events.go for the catalog. Nil until
	// Connect fires; callers read via ComplianceProfile().
	profile *compliance.Profile

	// metricsConn carries rx/tx counters + error counters. Nil until
	// Connect fires; callers read via Metrics(). Preserved after
	// Disconnect so post-mortem summaries are still available.
	metricsConn *metrics.Connector
}

// SetMatrixConfig records the caller-supplied matrix shape + poll
// knobs. Call before Connect. After Connect, changes take effect at
// the next Connect / Disconnect cycle.
func (p *Plugin) SetMatrixConfig(cfg MatrixConfig) {
	p.mu.Lock()
	p.matrixCfg = cfg
	p.mu.Unlock()
}

// MatrixConfig returns the currently-set matrix config (zero value if
// SetMatrixConfig was never called).
func (p *Plugin) MatrixConfig() MatrixConfig {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.matrixCfg
}

// Metrics returns the session-scoped connector metrics. Nil before
// Connect, non-nil after (preserved across Disconnect). Safe from any
// goroutine — metrics.Connector is internally synchronised.
func (p *Plugin) Metrics() *metrics.Connector {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.metricsConn
}

// IsOnline reports whether the plugin considers the matrix alive —
// true if we have seen any rx frame within DefaultOnlineStaleAfter.
// Mirrors the canonical.Header.IsOnline flag surfaced by the Ember+
// mirror: one "alive" bool, same semantics across every protocol.
// False before Connect and after Disconnect once the staleness
// threshold elapses.
func (p *Plugin) IsOnline() bool {
	return p.IsOnlineWithin(DefaultOnlineStaleAfter)
}

// IsOnlineWithin is IsOnline with an explicit staleness threshold.
func (p *Plugin) IsOnlineWithin(stale time.Duration) bool {
	p.mu.Lock()
	met := p.metricsConn
	p.mu.Unlock()
	if met == nil {
		return false
	}
	snap := met.Snapshot()
	if snap.LastRxAt.IsZero() {
		return false
	}
	return time.Since(snap.LastRxAt) < stale
}

// ComplianceProfile returns the session-scoped compliance profile.
// Nil before Connect, non-nil after.
func (p *Plugin) ComplianceProfile() *compliance.Profile {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.profile
}

// SetRecorder attaches a JSONL traffic recorder to this plugin. Call
// before Connect.
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
		return fmt.Errorf("probel-sw02p: already connected to %s:%d", p.host, p.port)
	}
	if port == 0 {
		port = DefaultPort
	}

	addr := fmt.Sprintf("%s:%d", ip, port)
	prof := &compliance.Profile{}
	met := metrics.NewConnector()
	// Register every known command byte so the metrics snapshot can
	// pretty-print names alongside raw ids. Empty in the scaffold;
	// per-command commits populate the catalogue.
	for _, id := range codec.CommandIDs() {
		met.RegisterCmd(uint8(id), codec.CommandName(id))
	}
	cfg := codec.ClientConfig{
		OnTx: func(b []byte) {
			if id, ok := probelCmdFromBytes(b); ok {
				met.ObserveCmdTx(id, len(b), 0)
			} else {
				met.ObserveTx(len(b), 0)
			}
		},
		OnRx: func(b []byte) {
			if id, ok := probelCmdFromBytes(b); ok {
				met.ObserveCmdRx(id, len(b))
			} else {
				met.ObserveRx(len(b))
			}
		},
	}
	if p.recorder != nil {
		rec := p.recorder
		wrappedTx := cfg.OnTx
		wrappedRx := cfg.OnRx
		cfg.OnTx = func(b []byte) {
			wrappedTx(b)
			rec.Record("probel-sw02p", "tx", b)
		}
		cfg.OnRx = func(b []byte) {
			wrappedRx(b)
			rec.Record("probel-sw02p", "rx", b)
		}
	}
	cli, err := codec.Dial(ctx, addr, p.logger, cfg)
	if err != nil {
		return &protocol.TransportError{Op: "connect", Err: err}
	}
	p.client = cli
	p.host = ip
	p.port = port
	p.profile = prof
	p.metricsConn = met
	p.logger.Info("probel-sw02p connected",
		slog.String("host", ip),
		slog.Int("port", port),
	)
	// Bootstrap sweep + keep-alive ping — fired in the background.
	// Lifetime bound to a fresh context so Disconnect can cancel
	// independently of the caller's connect ctx.
	kaCtx, kaCancel := context.WithCancel(context.Background())
	p.keepaliveCancel = kaCancel
	p.startKeepalive(kaCtx, cli)
	return nil
}

// Disconnect closes the TCP session. Safe to call on an unconnected
// plugin. Compliance profile + metrics connector are preserved after
// Disconnect so callers can still inspect them post-mortem.
func (p *Plugin) Disconnect() error {
	p.mu.Lock()
	cli := p.client
	met := p.metricsConn
	cancel := p.keepaliveCancel
	p.client = nil
	p.host = ""
	p.port = 0
	p.keepaliveCancel = nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if cli == nil {
		return nil
	}
	if met != nil {
		p.logger.Info("probel-sw02p session metrics",
			slog.String("summary", met.Summary()),
		)
	}
	return cli.Close()
}

// getClient returns the in-flight TCP client, or ErrNotConnected.
// Helper for per-command methods added by follow-up commits.
func (p *Plugin) getClient() (*codec.Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client == nil {
		return nil, protocol.ErrNotConnected
	}
	return p.client, nil
}

// ExposeClient returns the underlying codec.Client for callers that
// need direct Subscribe / raw frame access.
func (p *Plugin) ExposeClient() (*codec.Client, error) {
	return p.getClient()
}

// GetDeviceInfo is not applicable to SW-P-02 in the scaffold. Per-
// command commits may populate a synthetic DeviceInfo derived from
// Device-Name / Status replies.
func (p *Plugin) GetDeviceInfo(ctx context.Context) (protocol.DeviceInfo, error) {
	if _, err := p.getClient(); err != nil {
		return protocol.DeviceInfo{}, err
	}
	return protocol.DeviceInfo{IP: p.host, Port: p.port}, nil
}

// GetSlotInfo — slots are not an SW-P-02 concept.
func (p *Plugin) GetSlotInfo(ctx context.Context, slot int) (protocol.SlotInfo, error) {
	return protocol.SlotInfo{}, protocol.ErrNotImplemented
}

// Walk enumerates matrices / levels / destinations / sources. Lands in
// the "source + destination names + sizes" per-command commit.
func (p *Plugin) Walk(ctx context.Context, slot int) ([]protocol.Object, error) {
	return nil, protocol.ErrNotImplemented
}

// GetValue — for SW-P-02, GetValue on a crosspoint returns its current
// source. Lands in the CrosspointInterrogate per-command commit.
func (p *Plugin) GetValue(ctx context.Context, req protocol.ValueRequest) (protocol.Value, error) {
	return protocol.Value{}, protocol.ErrNotImplemented
}

// SetValue — for SW-P-02, SetValue on a crosspoint connects the named
// source. Lands in the CrosspointConnect per-command commit.
func (p *Plugin) SetValue(ctx context.Context, req protocol.ValueRequest, val protocol.Value) (protocol.Value, error) {
	return protocol.Value{}, protocol.ErrNotImplemented
}

// Subscribe attaches a callback for async tallies. Wiring lands in the
// CrosspointTally per-command commit.
func (p *Plugin) Subscribe(req protocol.ValueRequest, fn protocol.EventFunc) error {
	return protocol.ErrNotImplemented
}

// Unsubscribe removes a tally callback.
func (p *Plugin) Unsubscribe(req protocol.ValueRequest) error {
	return protocol.ErrNotImplemented
}
