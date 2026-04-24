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
	"log/slog"

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

// Plugin implements protocol.Protocol for one TSL version. Scaffolding —
// all methods return ErrNotImplemented until wire support lands.
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
