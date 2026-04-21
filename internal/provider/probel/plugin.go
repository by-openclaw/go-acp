// Package probel is the Probel SW-P-08 provider plugin — it serves a
// canonical tree as an SW-P-08 matrix controller (tx side) over TCP.
//
// Symmetric to the consumer plugin at internal/protocol/probel. Reuses
// internal/probel for the byte framer + per-command codecs. Per-command
// request handlers (CrosspointConnect / Interrogate / TallyDump / …) are
// added by follow-up per-command PRs; this scaffold only wires the
// listener + session I/O.
//
// Layering:
//
//	plugin.go  Factory + registration                (this file)
//	tree.go    canonical.Export -> matrix state tree (per-matrix per-level map[dst]src)
//	server.go  TCP accept + session lifecycle
//	session.go per-connection dispatch (stub: NAK every unknown CMD for now)
package probel

import (
	"log/slog"

	iprobel "acp/internal/probel"
	"acp/internal/export/canonical"
	"acp/internal/provider"
)

// DefaultPort mirrors the consumer's default Probel TCP port.
const DefaultPort = iprobel.DefaultPort

func init() {
	provider.Register(&Factory{})
}

// Factory registers the Probel provider plugin with the compile-time
// provider registry.
type Factory struct{}

// Meta publishes the static descriptor used by cmd/acp-provider.
func (f *Factory) Meta() provider.Meta {
	return provider.Meta{
		Name:        "probel",
		DefaultPort: DefaultPort,
		Description: "Probel SW-P-08 provider — serves a canonical tree as an SW-P-88 matrix over TCP:2008",
	}
}

// New constructs a fresh provider bound to the supplied tree.
func (f *Factory) New(logger *slog.Logger, tree *canonical.Export) provider.Provider {
	return newServer(logger, tree)
}
