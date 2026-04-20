// Package acp1 is the ACP1 provider plugin — it serves a canonical
// tree.json as an AxonNet ACP1 device over UDP (Mode A per CLAUDE.md).
//
// Symmetric to the consumer plugin at internal/protocol/acp1. Reuses
// that package's Message codec, value codec, and type constants; adds
// a property encoder (reverse of property.go's DecodeObject) and a
// session dispatcher.
//
// Layering:
//
//	tree.go      canonical.Export -> (slot, group, id)-indexed snapshot
//	encoder.go   DecodedObject + type -> wire bytes for getObject reply
//	value.go     typed canonical.Parameter value -> bytes for getValue / setX reply
//	session.go   per-datagram dispatch (getValue / setValue / setInc /
//	             setDec / setDef / getObject), error-reply emission
//	server.go    UDP accept loop, broadcast announcement fan-out
//	plugin.go    (this file) Factory + registration
package acp1

import (
	"log/slog"

	"acp/internal/export/canonical"
	"acp/internal/provider"
	iacp1 "acp/internal/protocol/acp1"
)

// DefaultPort is the IANA-assigned ACP port for both UDP and TCP direct.
// Matches the consumer plugin's default.
const DefaultPort = iacp1.DefaultPort

func init() {
	provider.Register(&Factory{})
}

// Factory registers the ACP1 provider plugin with the compile-time registry.
type Factory struct{}

// Meta publishes the static descriptor used by the CLI + API.
func (f *Factory) Meta() provider.Meta {
	return provider.Meta{
		Name:        "acp1",
		DefaultPort: DefaultPort,
		Description: "ACP1 (AxonNet) provider — serves a canonical tree to consumers over UDP:2071",
	}
}

// New constructs a fresh provider around the supplied tree.
func (f *Factory) New(logger *slog.Logger, tree *canonical.Export) provider.Provider {
	return newServer(logger, tree)
}
