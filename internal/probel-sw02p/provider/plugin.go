// Package probelsw02p is the Probel SW-P-02 provider plugin — it serves
// a canonical tree as an SW-P-02 matrix controller (tx side) over TCP.
//
// Symmetric to the consumer plugin at
// internal/probel-sw02p/consumer/. Reuses internal/probel-sw02p/codec/
// for the byte framer + per-command codecs. Per-command request
// handlers (CrosspointConnect / Interrogate / TallyDump / ...) are
// added by follow-up per-command commits; this scaffold only wires the
// listener + session I/O with a no-op dispatcher.
//
// Layering:
//
//	plugin.go  Factory + registration                (this file)
//	tree.go    canonical.Export → matrix state
//	server.go  TCP accept + session lifecycle
//	session.go per-connection dispatch (no-op for now)
package probelsw02p

import (
	"log/slog"

	"acp/internal/export/canonical"
	"acp/internal/probel-sw02p/codec"
	"acp/internal/provider"
)

// DefaultPort mirrors the consumer's default SW-P-02 TCP port.
const DefaultPort = codec.DefaultPort

func init() {
	provider.Register(&Factory{})
}

// Factory registers the SW-P-02 provider plugin with the compile-time
// provider registry.
type Factory struct{}

// Meta publishes the static descriptor used by cmd/dhs.
func (f *Factory) Meta() provider.Meta {
	return provider.Meta{
		Name:        "probel-sw02p",
		DefaultPort: DefaultPort,
		Description: "Probel SW-P-02 provider — serves a canonical tree as an SW-P-02 matrix over TCP:2002",
	}
}

// New constructs a fresh provider bound to the supplied tree.
func (f *Factory) New(logger *slog.Logger, tree *canonical.Export) provider.Provider {
	return newServer(logger, tree)
}
