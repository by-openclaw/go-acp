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
	"sync"

	"dhs/internal/export/canonical"
	"dhs/internal/osc/codec"
	"dhs/internal/provider"
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

	// mu guards the lazy-init pointers below. It is held only to read or
	// create a pointer; it is NEVER held across a blocking call (serveBlock)
	// or any network I/O. Callers capture the pointer to a local under the
	// lock, unlock, then invoke the method on the local. This mirrors the
	// tsl provider fix (ensureSender/senderRef) that closed a -race failure
	// where Serve/bind lazily created a sender read concurrently by
	// BoundAddr/Send/Stop.
	mu     sync.Mutex
	sender *udpSender
	tcp    *tcpDialer
}

// ensureSender returns the lazily-created UDP sender, creating it under mu
// on first use. The returned pointer is safe to use after the lock is
// released (the pointer itself is immutable once set).
func (s *Server) ensureSender() *udpSender {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sender == nil {
		s.sender = newUDPSender()
	}
	return s.sender
}

// senderRef returns the current UDP sender (possibly nil) under mu, without
// creating one. Used by read paths (BoundAddr / Send / Stop) that must
// preserve the not-bound behaviour when no sender exists yet.
func (s *Server) senderRef() *udpSender {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sender
}

// ensureTCPDialer returns the lazily-created TCP dialer, creating it under
// mu on first use.
func (s *Server) ensureTCPDialer() *tcpDialer {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tcp == nil {
		s.tcp = newTCPDialer(s.framerForVersion())
	}
	return s.tcp
}

// tcpDialerRef returns the current TCP dialer (possibly nil) under mu,
// without creating one. Used by Stop.
func (s *Server) tcpDialerRef() *tcpDialer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tcp
}

// framerForVersion returns the TCP framing kind for this Server's version.
func (s *Server) framerForVersion() framerKind {
	switch s.version {
	case V11:
		return framerSLIP
	default:
		return framerLenPrefix
	}
}

var errNotImplemented = errors.New("osc: provider operation not implemented in this phase")

// Serve binds the UDP egress socket and blocks on ctx. addr is the
// local bind (empty / ":0" → ephemeral). Destinations are added via
// AddDestination before or after Serve starts.
func (s *Server) Serve(ctx context.Context, addr string) error {
	// Capture the sender pointer under mu, then release mu BEFORE the
	// blocking serveBlock call — the lock never spans network I/O.
	sender := s.ensureSender()
	return sender.serveBlock(ctx, addr)
}

// Stop closes the UDP socket and any pending TCP connections.
func (s *Server) Stop() error {
	var first error
	// Capture pointers under mu, then close on the locals (close may do
	// network I/O / block, so it must not run under Server.mu).
	sender := s.senderRef()
	if sender != nil {
		if err := sender.close(); err != nil {
			first = err
		}
	}
	dialer := s.tcpDialerRef()
	if dialer != nil {
		if err := dialer.close(); err != nil && first == nil {
			first = err
		}
	}
	return first
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
	sender := s.ensureSender()
	return sender.bind(addr)
}

// BoundAddr returns the local UDP address (ephemeral-port resolution).
func (s *Server) BoundAddr() *net.UDPAddr {
	sender := s.senderRef()
	if sender == nil {
		return nil
	}
	return sender.boundAddr()
}

// AddDestination registers a remote peer to push OSC packets to.
// Broadcast addresses like 255.255.255.255 or subnet broadcasts are
// accepted — SO_BROADCAST is set on the socket.
func (s *Server) AddDestination(host string, port int) error {
	sender := s.ensureSender()
	return sender.addDest(host, port)
}

// SendMessage encodes + fans out a single OSC Message to all configured
// destinations.
func (s *Server) SendMessage(m codec.Message) error {
	sender := s.senderRef()
	if sender == nil {
		return fmt.Errorf("osc %s: not bound (call Bind or Serve first)", s.version.name())
	}
	return sender.sendMessage(m)
}

// SendBundle encodes + fans out an OSC Bundle (grouped messages under
// one timetag).
func (s *Server) SendBundle(b codec.Bundle) error {
	sender := s.senderRef()
	if sender == nil {
		return fmt.Errorf("osc %s: not bound (call Bind or Serve first)", s.version.name())
	}
	return sender.sendBundle(b)
}

// SendMessageTCP dials (or reuses) a TCP connection to (host, port) and
// writes the Message framed per this version (length-prefix for v1.0,
// SLIP double-END for v1.1).
func (s *Server) SendMessageTCP(host string, port int, m codec.Message) error {
	dialer := s.ensureTCPDialer()
	return dialer.sendMessage(host, port, m)
}

// SendBundleTCP dials (or reuses) a TCP connection and writes a framed
// Bundle.
func (s *Server) SendBundleTCP(host string, port int, b codec.Bundle) error {
	dialer := s.ensureTCPDialer()
	return dialer.sendBundle(host, port, b)
}
