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
	"fmt"
	"log/slog"
	"net"

	"acp/internal/export/canonical"
	"acp/internal/osc/codec"
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

// Server implements provider.Provider for one OSC version. It owns an
// outbound UDP sender fanning messages + bundles to configured
// destinations.
type Server struct {
	version Version
	logger  *slog.Logger
	tree    *canonical.Export

	sender *udpSender
}

var errNotImplemented = errors.New("osc: provider operation not implemented in this phase")

// Serve binds the UDP egress socket and blocks on ctx. addr is the
// local bind (empty / ":0" → ephemeral). Destinations are added via
// AddDestination before or after Serve starts.
func (s *Server) Serve(ctx context.Context, addr string) error {
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

// SetValue is the canonical-tree write hook. Not wired for OSC in this
// phase — the provider currently drives via SendMessage / SendBundle
// direct API. Canonical-tree path-to-OSC address mapping follows once
// the schema side lands.
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

// BoundAddr returns the local UDP address (ephemeral-port resolution).
func (s *Server) BoundAddr() *net.UDPAddr {
	if s.sender == nil {
		return nil
	}
	return s.sender.boundAddr()
}

// AddDestination registers a remote peer to push OSC packets to.
// Broadcast addresses like 255.255.255.255 or subnet broadcasts are
// accepted — SO_BROADCAST is set on the socket.
func (s *Server) AddDestination(host string, port int) error {
	if s.sender == nil {
		s.sender = newUDPSender()
	}
	return s.sender.addDest(host, port)
}

// SendMessage encodes + fans out a single OSC Message to all configured
// destinations.
func (s *Server) SendMessage(m codec.Message) error {
	if s.sender == nil {
		return fmt.Errorf("osc %s: not bound (call Bind or Serve first)", s.version.name())
	}
	return s.sender.sendMessage(m)
}

// SendBundle encodes + fans out an OSC Bundle (grouped messages under
// one timetag).
func (s *Server) SendBundle(b codec.Bundle) error {
	if s.sender == nil {
		return fmt.Errorf("osc %s: not bound (call Bind or Serve first)", s.version.name())
	}
	return s.sender.sendBundle(b)
}
