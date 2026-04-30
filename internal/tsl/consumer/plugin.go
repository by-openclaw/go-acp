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

	session    *udpSession // set for v3.1, v4.0, or v5.0-UDP
	tcpSession *tcpSession // set for v5.0-TCP
}

// Connect binds a UDP listener on (ip, port). ip may be empty for
// bind-all. For v5.0 this opens the UDP mode; TCP mode is available via
// ConnectV50TCP. For TSL there is no handshake — the listener is ready
// immediately after Connect returns.
func (p *Plugin) Connect(ctx context.Context, ip string, port int) error {
	if p.session != nil || p.tcpSession != nil {
		return fmt.Errorf("tsl %s: already connected", p.version.name())
	}
	addr := fmt.Sprintf("%s:%d", ip, port)
	s := newUDPSession()
	var decode func(*net.UDPAddr, []byte, *udpSession)
	switch p.version {
	case V31:
		decode = decodeV31Payload
	case V40:
		decode = decodeV40Payload
	case V50:
		decode = decodeV50Payload
	default:
		return fmt.Errorf("tsl consumer: unknown version %v", p.version)
	}
	if err := s.listen(ctx, addr, decode); err != nil {
		return err
	}
	p.session = s
	return nil
}

// ConnectV50TCP binds a TCP listener on (ip, port) that accepts v5.0
// streams wrapped in DLE/STX per spec §Phy. Each accepted connection
// gets a DLE-stream decoder; frames are dispatched via SubscribeV50.
func (p *Plugin) ConnectV50TCP(ctx context.Context, ip string, port int) error {
	if p.version != V50 {
		return fmt.Errorf("tsl %s: ConnectV50TCP only valid for v5.0 plugin", p.version.name())
	}
	if p.session != nil || p.tcpSession != nil {
		return fmt.Errorf("tsl %s: already connected", p.version.name())
	}
	addr := fmt.Sprintf("%s:%d", ip, port)
	ts := newTCPSession()
	if err := ts.listen(ctx, addr); err != nil {
		return err
	}
	p.tcpSession = ts
	return nil
}

// Disconnect closes the active listener and stops the read loop.
func (p *Plugin) Disconnect() error {
	var err error
	if p.session != nil {
		err = p.session.close()
		p.session = nil
	}
	if p.tcpSession != nil {
		if e := p.tcpSession.close(); e != nil && err == nil {
			err = e
		}
		p.tcpSession = nil
	}
	return err
}

// BoundAddr returns the actual UDP listen address (v3.1/v4.0/v5.0-UDP).
// For TCP mode see BoundTCPAddr.
func (p *Plugin) BoundAddr() *net.UDPAddr {
	if p.session == nil {
		return nil
	}
	return p.session.boundAddr()
}

// BoundTCPAddr returns the TCP listen address when v5.0 is in TCP mode.
func (p *Plugin) BoundTCPAddr() *net.TCPAddr {
	if p.tcpSession == nil {
		return nil
	}
	return p.tcpSession.boundAddr()
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

// SubscribeV40 registers a handler for v4.0 frames.
func (p *Plugin) SubscribeV40(h V40Handler) error {
	if p.session == nil {
		return fmt.Errorf("tsl %s: not connected", p.version.name())
	}
	if p.version != V40 {
		return fmt.Errorf("tsl %s: SubscribeV40 only valid for v4.0 plugin", p.version.name())
	}
	p.session.subscribeV40(h)
	return nil
}

// SubscribeV50 registers a handler for v5.0 packets. Works for both UDP
// and TCP sessions — the handler fires on frames received via whichever
// transport is active.
func (p *Plugin) SubscribeV50(h V50Handler) error {
	if p.version != V50 {
		return fmt.Errorf("tsl %s: SubscribeV50 only valid for v5.0 plugin", p.version.name())
	}
	switch {
	case p.session != nil:
		p.session.subscribeV50(h)
	case p.tcpSession != nil:
		p.tcpSession.subscribeV50(h)
	default:
		return fmt.Errorf("tsl %s: not connected", p.version.name())
	}
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
