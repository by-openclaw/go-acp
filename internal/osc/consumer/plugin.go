// Package osc implements the consumer side of the OSC plugin for
// versions 1.0 and 1.1. OSC has no client/server model; the "consumer"
// role here is a UDP listener (or TCP acceptor in 1.1/SLIP or 1.0/length-
// prefix mode) that dispatches received messages + bundles to registered
// address-pattern handlers.
//
// Registered names:
//
//	osc-v10  — UDP + TCP length-prefix (OSC 1.0)
//	osc-v11  — UDP + TCP SLIP (OSC 1.1, adds T/F/N/I + arrays)
//
// Each version is a distinct registry entry per
// memory/feedback_protocol_versioning.md (Pattern A — wire framing
// differs materially on TCP).
package osc

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"acp/internal/protocol"
)

func init() {
	protocol.Register(&Factory{version: V10})
	protocol.Register(&Factory{version: V11})
}

// Version selects the OSC wire version a Plugin speaks.
type Version int

const (
	V10 Version = iota
	V11
)

func (v Version) name() string {
	switch v {
	case V10:
		return "osc-v10"
	case V11:
		return "osc-v11"
	}
	return "osc-unknown"
}

func (v Version) defaultPort() int {
	// No port is officially mandated. 8000 is the most common OSC
	// convention across QLab, TouchOSC, Companion, and the audio-
	// console ecosystem. User-configurable per session.
	return 8000
}

func (v Version) description() string {
	switch v {
	case V10:
		return "Open Sound Control 1.0 — UDP + TCP/length-prefix; core types i/f/s/b"
	case V11:
		return "Open Sound Control 1.1 — UDP + TCP/SLIP; adds T/F/N/I + arrays"
	}
	return ""
}

// Factory creates a Plugin bound to one OSC version.
type Factory struct {
	version Version
}

func (f *Factory) Meta() protocol.ProtocolMeta {
	return protocol.ProtocolMeta{
		Name:        f.version.name(),
		DefaultPort: f.version.defaultPort(),
		Description: f.version.description(),
	}
}

func (f *Factory) New(logger *slog.Logger) protocol.Protocol {
	return &Plugin{version: f.version, logger: logger}
}

// NewPluginV10 / NewPluginV11 construct version-bound Plugins directly
// (used by tests and callers that want the concrete type).
func NewPluginV10(logger *slog.Logger) *Plugin {
	return &Plugin{version: V10, logger: logger}
}
func NewPluginV11(logger *slog.Logger) *Plugin {
	return &Plugin{version: V11, logger: logger}
}

// Plugin implements protocol.Protocol for one OSC version. Connect
// opens a UDP listener on (ip, port); TCP transports (length-prefix for
// v1.0, SLIP for v1.1) are wired via separate ConnectTCP* methods.
type Plugin struct {
	version Version
	logger  *slog.Logger

	udp *udpSession
	tcp *tcpSession
}

// Connect binds a UDP listener on (ip, port). For TCP, use ConnectTCP
// (both v1.0 and v1.1). OSC has no handshake — the listener is ready
// immediately after Connect returns.
func (p *Plugin) Connect(ctx context.Context, ip string, port int) error {
	if p.udp != nil || p.tcp != nil {
		return fmt.Errorf("osc %s: already connected", p.version.name())
	}
	addr := fmt.Sprintf("%s:%d", ip, port)
	s := newUDPSession()
	if err := s.listen(ctx, addr); err != nil {
		return err
	}
	p.udp = s
	return nil
}

// ConnectTCP binds a TCP listener on (ip, port) using the framing
// dictated by this Plugin's version: length-prefix for v1.0, SLIP
// double-END for v1.1. Each accepted connection runs a per-conn
// packet-read loop that dispatches via SubscribePattern.
func (p *Plugin) ConnectTCP(ctx context.Context, ip string, port int) error {
	if p.udp != nil || p.tcp != nil {
		return fmt.Errorf("osc %s: already connected", p.version.name())
	}
	var framer framerKind
	switch p.version {
	case V10:
		framer = framerLenPrefix
	case V11:
		framer = framerSLIP
	}
	addr := fmt.Sprintf("%s:%d", ip, port)
	s := newTCPSession(framer)
	if err := s.listen(ctx, addr); err != nil {
		return err
	}
	p.tcp = s
	return nil
}

// Disconnect closes the active listener.
func (p *Plugin) Disconnect() error {
	var err error
	if p.udp != nil {
		err = p.udp.close()
		p.udp = nil
	}
	if p.tcp != nil {
		if e := p.tcp.close(); e != nil && err == nil {
			err = e
		}
		p.tcp = nil
	}
	return err
}

// BoundAddr returns the UDP listen address (nil if in TCP mode).
func (p *Plugin) BoundAddr() *net.UDPAddr {
	if p.udp == nil {
		return nil
	}
	return p.udp.boundAddr()
}

// BoundTCPAddr returns the TCP listen address (nil if not in TCP mode).
func (p *Plugin) BoundTCPAddr() *net.TCPAddr {
	if p.tcp == nil {
		return nil
	}
	return p.tcp.boundAddr()
}

// SubscribePattern registers a handler for messages whose address
// matches the pattern. Works for both UDP and TCP sessions.
func (p *Plugin) SubscribePattern(pattern string, h Handler) error {
	switch {
	case p.udp != nil:
		p.udp.subscribe(pattern, h)
	case p.tcp != nil:
		p.tcp.subscribe(pattern, h)
	default:
		return fmt.Errorf("osc %s: not connected", p.version.name())
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
