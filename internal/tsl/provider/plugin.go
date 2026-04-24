// Package tsl implements the provider (tally-source) side of the TSL UMD
// plugin for versions 3.1, 4.0, and 5.0. Scaffolding — wire logic lands
// per-version on the feature branch.
//
// Registered names mirror the consumer side: tsl-v31, tsl-v40, tsl-v50.
package tsl

import (
	"context"
	"errors"
	"log/slog"

	"acp/internal/export/canonical"
	"acp/internal/provider"
)

func init() {
	provider.Register(&Factory{version: V31})
	provider.Register(&Factory{version: V40})
	provider.Register(&Factory{version: V50})
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
		return "TSL UMD v3.1 producer — 18-byte UDP"
	case V40:
		return "TSL UMD v4.0 producer — v3.1 + XDATA + CHKSUM"
	case V50:
		return "TSL UMD v5.0 producer — UDP or TCP (DLE/STX wrapped)"
	}
	return ""
}

// Factory creates a Server bound to one TSL version.
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

// Server implements provider.Provider for one TSL version. Scaffolding —
// Serve/Stop/SetValue return a not-implemented error until wire support
// lands.
type Server struct {
	version Version
	logger  *slog.Logger
	tree    *canonical.Export
}

// errNotImplemented is the scaffolding sentinel. Replaced with real
// wire implementations per-phase.
var errNotImplemented = errors.New("tsl: provider not implemented yet (scaffolding)")

func (s *Server) Serve(ctx context.Context, addr string) error {
	return errNotImplemented
}

func (s *Server) Stop() error {
	return nil
}

func (s *Server) SetValue(ctx context.Context, path string, val any) (any, error) {
	return nil, errNotImplemented
}
