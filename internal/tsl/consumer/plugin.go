// Package tsl implements the consumer (MV-receiver) side of the TSL UMD
// plugin for versions 3.1, 4.0, and 5.0. Scaffolding — wire logic lands
// per-version on the feature branch.
//
// Registered names:
//
//	tsl-v31  — v3.1 UDP
//	tsl-v40  — v4.0 UDP
//	tsl-v50  — v5.0 UDP + TCP (DLE/STX wrapped)
//
// Each version is a distinct registry entry because their wire formats,
// transports, and default ports differ. They share the compliance event
// vocabulary and codec package.
package tsl

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"acp/internal/protocol"
)

func init() {
	protocol.Register(&Factory{version: V31})
	protocol.Register(&Factory{version: V40})
	protocol.Register(&Factory{version: V50})
}

// Version identifies one of the three TSL UMD wire versions.
type Version int

const (
	V31 Version = iota
	V40
	V50
)

func (v Version) name() string {
	switch v {
	case V31:
		return "tsl-v31"
	case V40:
		return "tsl-v40"
	case V50:
		return "tsl-v50"
	}
	return "tsl-unknown"
}

func (v Version) defaultPort() int {
	switch v {
	case V31, V40:
		return 4000
	case V50:
		return 8901
	}
	return 0
}

func (v Version) description() string {
	switch v {
	case V31:
		return "TSL UMD v3.1 — 18-byte UDP frame (binary tallies, no colour)"
	case V40:
		return "TSL UMD v4.0 — v3.1 extended with XDATA + CHKSUM (2-bit colour per LH/Text/RH per display)"
	case V50:
		return "TSL UMD v5.0 — UDP (≤2048 B) or TCP with DLE/STX wrapper; multi-DMSG; UTF-16LE"
	}
	return ""
}

// Factory creates a Plugin bound to one TSL version.
type Factory struct {
	version Version
}

// Meta returns the registry metadata.
func (f *Factory) Meta() protocol.ProtocolMeta {
	return protocol.ProtocolMeta{
		Name:        f.version.name(),
		DefaultPort: f.version.defaultPort(),
		Description: f.version.description(),
	}
}

// New instantiates a Plugin for this version.
func (f *Factory) New(logger *slog.Logger) protocol.Protocol {
	return &Plugin{version: f.version, logger: logger}
}

// NewPluginV31 constructs a v3.1-bound Plugin directly (used by tests and
// by callers that want the concrete type rather than the interface).
func NewPluginV31(logger *slog.Logger) *Plugin {
	return &Plugin{version: V31, logger: logger}
}

// NewPluginV40 constructs a v4.0-bound Plugin directly.
func NewPluginV40(logger *slog.Logger) *Plugin {
	return &Plugin{version: V40, logger: logger}
}

// NewPluginV50 constructs a v5.0-bound Plugin directly.
func NewPluginV50(logger *slog.Logger) *Plugin {
	return &Plugin{version: V50, logger: logger}
}

// Plugin implements protocol.Protocol for one TSL version. For v3.1 and
// v4.0 it opens a UDP listener; v5.0 additionally supports TCP with
// DLE/STX wrapper (wired alongside v5 codec).
type Plugin struct {
	version Version
	logger  *slog.Logger

	session *udpSession
}

// Connect binds a UDP listener on (ip, port). ip may be empty for
// bind-all. For TSL there is no handshake — the listener is ready
// immediately after Connect returns.
func (p *Plugin) Connect(ctx context.Context, ip string, port int) error {
	if p.session != nil {
		return fmt.Errorf("tsl %s: already connected", p.version.name())
	}
	addr := fmt.Sprintf("%s:%d", ip, port)
	s := newUDPSession()
	decode := decodeV31Payload // v3.1/v4.0 share fixed-size frame boundary; v4.0 decoder switches later
	if p.version == V50 {
		return fmt.Errorf("tsl v5.0 consumer: not implemented in this phase (tracked by #121)")
	}
	if err := s.listen(ctx, addr, decode); err != nil {
		return err
	}
	p.session = s
	return nil
}

// Disconnect closes the UDP socket and stops the read loop.
func (p *Plugin) Disconnect() error {
	if p.session == nil {
		return nil
	}
	err := p.session.close()
	p.session = nil
	return err
}

// BoundAddr returns the actual listen address (useful when port 0 was
// requested for ephemeral).
func (p *Plugin) BoundAddr() *net.UDPAddr {
	if p.session == nil {
		return nil
	}
	return p.session.boundAddr()
}

// SubscribeV31 registers a handler for v3.1 frames. For the generic
// protocol.Protocol.Subscribe path, see Subscribe.
func (p *Plugin) SubscribeV31(h V31Handler) error {
	if p.session == nil {
		return fmt.Errorf("tsl %s: not connected", p.version.name())
	}
	if p.version != V31 {
		return fmt.Errorf("tsl %s: SubscribeV31 only valid for v3.1 plugin", p.version.name())
	}
	p.session.subscribeV31(h)
	return nil
}

func (p *Plugin) GetDeviceInfo(ctx context.Context) (protocol.DeviceInfo, error) {
	return protocol.DeviceInfo{}, protocol.ErrNotImplemented
}

func (p *Plugin) GetSlotInfo(ctx context.Context, slot int) (protocol.SlotInfo, error) {
	return protocol.SlotInfo{}, protocol.ErrNotImplemented
}

func (p *Plugin) Walk(ctx context.Context, slot int) ([]protocol.Object, error) {
	return nil, protocol.ErrNotImplemented
}

func (p *Plugin) GetValue(ctx context.Context, req protocol.ValueRequest) (protocol.Value, error) {
	return protocol.Value{}, protocol.ErrNotImplemented
}

func (p *Plugin) SetValue(ctx context.Context, req protocol.ValueRequest, val protocol.Value) (protocol.Value, error) {
	return protocol.Value{}, protocol.ErrNotImplemented
}

func (p *Plugin) Subscribe(req protocol.ValueRequest, fn protocol.EventFunc) error {
	return protocol.ErrNotImplemented
}

func (p *Plugin) Unsubscribe(req protocol.ValueRequest) error {
	return nil
}
