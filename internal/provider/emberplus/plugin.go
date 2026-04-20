// Package emberplus is the Ember+ provider plugin — it serves a Glow tree
// over S101/TCP to any number of consumers.
//
// Layering (mirror of the consumer):
//
//	tree.go      canonical.Export -> in-memory Node/Param/Matrix snapshot
//	encoder.go   tree walk -> Glow BER bytes (QualifiedParameter etc.)
//	session.go   per-connection consumer state (subscriptions, out queue)
//	server.go    TCP accept loop, fan-out on SetValue
//	plugin.go    (this file) Factory + registration
//
// Reuses consumer-side packages for symmetric work:
//
//	internal/protocol/emberplus/s101    framing (symmetric)
//	internal/protocol/emberplus/ber     TLV codec
//	internal/protocol/emberplus/glow    decoder + tag constants (decoder is symmetric)
package emberplus

import (
	"log/slog"

	"acp/internal/export/canonical"
	"acp/internal/provider"
)

// DefaultPort matches the scope of issue #66 — non-overlapping with the
// conventional Ember+ consumer-side ports (9000/9090/9092).
const DefaultPort = 9010

func init() {
	provider.Register(&Factory{})
}

// Factory registers the Ember+ provider plugin with the compile-time registry.
type Factory struct{}

// Meta publishes the static descriptor used by the CLI + API.
func (f *Factory) Meta() provider.Meta {
	return provider.Meta{
		Name:        "emberplus",
		DefaultPort: DefaultPort,
		Description: "Ember+ (Glow/S101/TCP) provider — serves a canonical tree to consumers",
	}
}

// New constructs a fresh provider around the supplied tree.
func (f *Factory) New(logger *slog.Logger, tree *canonical.Export) provider.Provider {
	return newServer(logger, tree)
}
