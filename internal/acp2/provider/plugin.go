// Package acp2 is the ACP2 provider plugin — it serves a canonical
// tree.json as an ACP2 device over AN2/TCP port 2072.
//
// Symmetric to the consumer at internal/protocol/acp2. Reuses the
// consumer's AN2 framer, ACP2 codec, and property codec verbatim
// (all three are bidirectional by design). Adds:
//
//	tree.go      canonical.Export -> obj-id indexed snapshot
//	handlers.go  AN2 internal (proto=0) + ACP2 proto=2 request dispatch
//	session.go   per-connection state (AN2 read loop, mtid table, enabled protos)
//	server.go    TCP accept loop, per-conn session spawn, broadcast fan-out
//	plugin.go    (this file) Factory + registration
package acp2

import (
	"log/slog"

	"acp/internal/export/canonical"
	"acp/internal/provider"
	iacp2 "acp/internal/acp2/consumer"
)

// DefaultPort is the AN2 TCP port used for ACP2 traffic.
const DefaultPort = iacp2.DefaultPort

func init() {
	provider.Register(&Factory{})
}

// Factory registers the ACP2 provider plugin with the compile-time registry.
type Factory struct{}

// Meta publishes the static descriptor used by the CLI + API.
func (f *Factory) Meta() provider.Meta {
	return provider.Meta{
		Name:        "acp2",
		DefaultPort: DefaultPort,
		Description: "ACP2 (AN2/TCP) provider — serves a canonical tree to consumers on port 2072",
	}
}

// New constructs a fresh provider around the supplied tree.
func (f *Factory) New(logger *slog.Logger, tree *canonical.Export) provider.Provider {
	return newServer(logger, tree)
}
