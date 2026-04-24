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
	"log/slog"

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

// Plugin implements protocol.Protocol for one OSC version. Scaffolding —
// wire implementations land per-phase on feat/osc-plugin.
type Plugin struct {
	version Version
	logger  *slog.Logger
}

func (p *Plugin) Connect(ctx context.Context, ip string, port int) error {
	return protocol.ErrNotImplemented
}

func (p *Plugin) Disconnect() error {
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
