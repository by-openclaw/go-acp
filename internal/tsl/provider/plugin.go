// Package tsl implements the provider (tally-source) side of the TSL UMD
// plugin for versions 3.1, 4.0, and 5.0. Scaffolding — wire logic lands
// per-version on the feature branch.
//
// Registered names mirror the consumer side: tsl-v31, tsl-v40, tsl-v50.
package tsl

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"

	"acp/internal/export/canonical"
	"acp/internal/provider"
	"acp/internal/tsl/codec"
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

// NewServerV31 constructs a v3.1-bound Server directly (tests + direct callers).
func NewServerV31(logger *slog.Logger) *Server {
	return &Server{version: V31, logger: logger}
}

// NewServerV40 constructs a v4.0-bound Server directly.
func NewServerV40(logger *slog.Logger) *Server {
	return &Server{version: V40, logger: logger}
}

// NewServerV50 constructs a v5.0-bound Server directly.
func NewServerV50(logger *slog.Logger) *Server {
	return &Server{version: V50, logger: logger}
}

// Server implements provider.Provider for one TSL version. It owns an
// outbound UDP sender fanning frames out to a configurable set of
// destinations (the MVs to push to).
type Server struct {
	version Version
	logger  *slog.Logger
	tree    *canonical.Export

	sender *udpSender
}

// errNotImplemented is the scaffolding sentinel for operations that are
// not part of v3.1 (v5.0 TCP wrapper, canonical-tree SetValue).
var errNotImplemented = errors.New("tsl: provider operation not implemented in this phase")

// Serve binds a local UDP socket and blocks on ctx. addr is the local
// egress bind (empty / ":0" → ephemeral). Destinations are added via
// AddDestination before or after Serve starts.
func (s *Server) Serve(ctx context.Context, addr string) error {
	if s.version == V50 {
		return errNotImplemented
	}
	if s.sender == nil {
		s.sender = newUDPSender()
	}
	return s.sender.serveBlock(ctx, addr)
}

// Stop closes the UDP socket.
func (s *Server) Stop() error {
	if s.sender == nil {
		return nil
	}
	return s.sender.close()
}

// SetValue is the canonical-tree write hook. Not wired for TSL in this
// phase — producer drives frames via SendV31 (direct API). Tree-driven
// emission follows once the canonical schema for TSL displays lands.
func (s *Server) SetValue(ctx context.Context, path string, val any) (any, error) {
	return nil, errNotImplemented
}

// Bind binds the egress socket without blocking. Useful for tests that
// want to configure destinations before Serve starts.
func (s *Server) Bind(addr string) error {
	if s.sender == nil {
		s.sender = newUDPSender()
	}
	return s.sender.bind(addr)
}

// BoundAddr returns the local UDP address (ephemeral resolution).
func (s *Server) BoundAddr() *net.UDPAddr {
	if s.sender == nil {
		return nil
	}
	return s.sender.boundAddr()
}

// AddDestination registers an MV to push frames to. Multiple destinations
// fan out the same frame.
func (s *Server) AddDestination(host string, port int) error {
	if s.sender == nil {
		s.sender = newUDPSender()
	}
	return s.sender.addDest(host, port)
}

// SendV31 encodes and sends a v3.1 frame to all configured destinations.
func (s *Server) SendV31(frame codec.V31Frame) error {
	if s.version != V31 {
		return fmt.Errorf("tsl %s: SendV31 only valid for v3.1 plugin", s.version.name())
	}
	if s.sender == nil {
		return errors.New("tsl v3.1: not bound (call Bind or Serve first)")
	}
	return s.sender.encodeAndSendV31(frame)
}
