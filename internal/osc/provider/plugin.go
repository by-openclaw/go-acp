// Package osc implements the provider side of the OSC plugin for
// versions 1.0 and 1.1. The provider pushes messages/bundles to
// configured destinations via UDP (unicast + broadcast) or TCP
// (length-prefix for 1.0, SLIP for 1.1).
//
// Registered names mirror the consumer side: osc-v10, osc-v11.
package osc

import (
	"context"
	"errors"
	"log/slog"

	"acp/internal/export/canonical"
	"acp/internal/provider"
)

func init() {
	provider.Register(&Factory{version: V10})
	provider.Register(&Factory{version: V11})
}

// Version selects the OSC wire version a Server speaks.
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
	return 8000
}

func (v Version) description() string {
	switch v {
	case V10:
		return "OSC 1.0 producer — UDP + TCP/length-prefix"
	case V11:
		return "OSC 1.1 producer — UDP + TCP/SLIP (double-END)"
	}
	return ""
}

type Factory struct {
	version Version
}

func (f *Factory) Meta() provider.Meta {
	return provider.Meta{
		Name:        f.version.name(),
		DefaultPort: f.version.defaultPort(),
		Description: f.version.description(),
	}
}

func (f *Factory) New(logger *slog.Logger, tree *canonical.Export) provider.Provider {
	return &Server{version: f.version, logger: logger, tree: tree}
}

// NewServerV10 / NewServerV11 construct version-bound Servers directly.
func NewServerV10(logger *slog.Logger) *Server {
	return &Server{version: V10, logger: logger}
}
func NewServerV11(logger *slog.Logger) *Server {
	return &Server{version: V11, logger: logger}
}

// Server implements provider.Provider for one OSC version. Scaffolding —
// Serve/Stop/SetValue return not-implemented until per-phase wiring.
type Server struct {
	version Version
	logger  *slog.Logger
	tree    *canonical.Export
}

var errNotImplemented = errors.New("osc: provider operation not implemented in this phase")

func (s *Server) Serve(ctx context.Context, addr string) error {
	return errNotImplemented
}

func (s *Server) Stop() error {
	return nil
}

func (s *Server) SetValue(ctx context.Context, path string, val any) (any, error) {
	return nil, errNotImplemented
}
