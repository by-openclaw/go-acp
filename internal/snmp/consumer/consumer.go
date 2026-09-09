// Package consumer is the SNMP manager: the half of the connector that
// polls somebody else's agent and listens for what it sends unasked.
//
// It is built on internal/snmp/codec like the agent is, rather than on a
// library, for the reason internal/snmp/CLAUDE.md gives: a codec is what
// this repo makes, and there is no dependency worth taking to put bytes
// on a UDP socket. What it is NOT is a re-implementation of the agent's
// rules — the two roles share the codec and nothing else, because the
// only thing they agree on is the wire.
package consumer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"sync"
	"time"

	"dhs/internal/clock"
	"dhs/internal/consumer/compliance"
	"dhs/internal/plugin"
	"dhs/internal/snmp/codec"
)

// Compliance events this connector records. Each is something a device
// did that its own specification does not describe; the labels are
// constants because the label IS the aggregation key, and a typo in one
// call site silently splits a count in two.
const (
	// TruncatedWalk is an agent that answered a GETNEXT with a name that
	// did not advance. RFC 3416 requires strictly increasing names, and
	// an agent that repeats one walks a manager in a circle forever.
	TruncatedWalk = "snmp_walk_did_not_advance"
	// UnsolicitedResponse is a Response whose request-id matches nothing
	// outstanding: a late retry, or somebody else's traffic.
	UnsolicitedResponse = "snmp_unsolicited_response"
	// VersionMismatch is a reply in a different version from the request.
	VersionMismatch = "snmp_reply_version_mismatch"
	// CommunityMismatch is a reply carrying a community we did not send.
	CommunityMismatch = "snmp_reply_community_mismatch"
	// BulkOverrun is an agent that returned more repetitions than were
	// asked for, which a manager that trusted the count would misparse.
	BulkOverrun = "snmp_bulk_returned_too_much"
)

// ErrTimeout is a request that got no answer within its deadline. It is
// the ordinary case on UDP and callers branch on it.
var ErrTimeout = errors.New("snmp: no response")

// Options configure a session.
type Options struct {
	// Addr is host:port. A bare host takes the default port.
	Addr string
	// Version is v1 or v2c. v3 is not driven from here yet: a manager is
	// authoritative for nothing, so it must discover the agent's engine
	// before it can authenticate, and that is its own unit.
	Version codec.Version
	// Community is the v1/v2c password. Empty means "public".
	Community string
	// Timeout bounds one request. Zero means the default.
	Timeout time.Duration
	// Retries is how many times a request is repeated before it is a
	// timeout. UDP loses datagrams; a manager that did not retry would
	// report a device down because one packet was dropped.
	Retries int
	// MaxRepetitions is the GETBULK window. Zero means the default.
	MaxRepetitions int

	// Compliance records what an agent did that the RFCs do not
	// describe. Nil is fine — the profile answers every method.
	Compliance compliance.Recorder
}

// Defaults, chosen from what the devices in docs/testbed.md actually
// need rather than from taste: the IRDs answer in single-digit
// milliseconds on a quiet fabric and take seconds when they are busy
// walking their own 842 objects.
const (
	DefaultTimeout        = 2 * time.Second
	DefaultRetries        = 2
	DefaultMaxRepetitions = 25
	// DefaultPort is where an agent listens. Cerebrum's answers on 1161,
	// so this is a default and not an assumption.
	DefaultPort = 161
)

func (o Options) withDefaults() Options {
	if o.Version == 0 && o.Community == "" {
		// Version1 IS zero, so an unset Version cannot be told from an
		// explicit v1 — except that a caller who set neither wants the
		// modern default. v1 stays reachable by naming a community.
		o.Version = codec.Version2c
	}
	if o.Community == "" {
		o.Community = "public"
	}
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	if o.Retries < 0 {
		o.Retries = DefaultRetries
	}
	if o.MaxRepetitions <= 0 {
		o.MaxRepetitions = DefaultMaxRepetitions
	}
	return o
}

// Session is one manager talking to one agent.
//
// A session is NOT safe for concurrent requests, and does not pretend to
// be: SNMP correlates on a 32-bit request-id over a socket that may
// deliver a late retry from three requests ago, and serialising is how
// this stays a manager rather than becoming a dispatcher. A caller
// polling ten devices opens ten sessions.
type Session struct {
	opts   Options
	logger *slog.Logger
	clk    clock.Clock
	prof   compliance.Recorder

	mu     sync.Mutex
	conn   net.Conn
	nextID int32
	closed bool
}

// Dial opens a session. The socket is dialed through the injected
// transport so a plant's dial policy applies here as everywhere.
func Dial(ctx context.Context, opts Options, deps plugin.Deps) (*Session, error) {
	deps = deps.WithDefaults()
	opts = opts.withDefaults()

	switch opts.Version {
	case codec.Version1, codec.Version2c:
	default:
		return nil, fmt.Errorf("snmp: this manager speaks v1 and v2c, not %s", opts.Version)
	}

	addr, err := withDefaultPort(opts.Addr)
	if err != nil {
		return nil, err
	}
	conn, err := deps.Net.Dial(ctx, "udp", addr)
	if err != nil {
		return nil, fmt.Errorf("snmp: dial %s: %w", addr, err)
	}

	return &Session{
		opts:   opts,
		logger: plugin.LoggerOrDefault(deps.Logger),
		clk:    deps.Clock,
		prof:   opts.Compliance,
		conn:   conn,
		// A random starting request-id, so a restarted manager does not
		// accept a late reply to the previous run's request 1.
		nextID: rand.Int31(), //nolint:gosec // correlation, not secrecy
	}, nil
}

// withDefaultPort adds :161 to a bare host.
func withDefaultPort(addr string) (string, error) {
	if addr == "" {
		return "", fmt.Errorf("snmp: no address")
	}
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr, nil
	}
	return net.JoinHostPort(addr, fmt.Sprint(DefaultPort)), nil
}

// Close releases the socket. Idempotent.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.conn.Close()
}

// Get reads the named objects in one request.
//
// Every name comes back, in the order asked, so a caller can index the
// result against what it sent. Under v2c a missing object is an
// exception value in its own varbind and the rest of the request still
// answers; under v1 the whole request fails, which is the protocol and
// not something this hides.
func (s *Session) Get(ctx context.Context, names ...codec.OID) ([]codec.VarBind, error) {
	return s.request(ctx, codec.PDUTypeGet, names)
}

// GetNext reads whatever follows each name, which is one step of a walk.
func (s *Session) GetNext(ctx context.Context, names ...codec.OID) ([]codec.VarBind, error) {
	return s.request(ctx, codec.PDUTypeGetNext, names)
}

// Set writes. It is the only verb here that changes anything, and it is
// the reason a write community exists.
func (s *Session) Set(ctx context.Context, binds ...codec.VarBind) ([]codec.VarBind, error) {
	if len(binds) == 0 {
		return nil, fmt.Errorf("snmp: a SET needs something to write")
	}
	resp, err := s.roundTrip(ctx, &codec.PDU{Type: codec.PDUTypeSet, VarBinds: binds})
	if err != nil {
		return nil, err
	}
	return resp.VarBinds, statusError(resp)
}

// request is the shared shape of GET and GETNEXT: a list of names, each
// with a NULL where the value will go.
func (s *Session) request(ctx context.Context, t codec.PDUType,
	names []codec.OID) ([]codec.VarBind, error) {
	if len(names) == 0 {
		return nil, fmt.Errorf("snmp: %s needs at least one name", t)
	}
	binds := make([]codec.VarBind, 0, len(names))
	for _, n := range names {
		binds = append(binds, codec.VarBind{Name: n, Value: codec.Null()})
	}
	resp, err := s.roundTrip(ctx, &codec.PDU{Type: t, VarBinds: binds})
	if err != nil {
		return nil, err
	}
	return resp.VarBinds, statusError(resp)
}

// StatusError is a response that came back with an error-status.
//
// A typed error rather than a formatted one because callers BRANCH on
// the status: a walk ends when a v1 agent says noSuchName, a poller
// retries on genErr and gives up on noAccess, and a caller matching on
// rendered text is a caller that breaks when the text improves.
type StatusError struct {
	Status codec.ErrorStatus
	// Index is the 1-based error-index, pointing into the REQUEST.
	Index int
	// Name is the binding Index names, when the response echoed enough
	// of the request to say. Empty otherwise.
	Name codec.OID
}

func (e *StatusError) Error() string {
	if len(e.Name) > 0 {
		return fmt.Sprintf("snmp: %s at %s", e.Status, e.Name)
	}
	return fmt.Sprintf("snmp: %s", e.Status)
}

// Is lets errors.Is match on the status alone, so a caller can write
// errors.Is(err, &StatusError{Status: codec.NoSuchName}).
func (e *StatusError) Is(target error) bool {
	other, ok := target.(*StatusError)
	return ok && other.Status == e.Status
}

// statusError turns a response's error-status into an error, naming the
// binding it applies to.
//
// error-index is 1-based and points into the REQUEST, which is why the
// response echoes the request's bindings on failure: without that echo a
// manager knows something went wrong and not what.
func statusError(p *codec.PDU) error {
	if p.ErrorStatus == codec.NoError {
		return nil
	}
	e := &StatusError{Status: p.ErrorStatus, Index: p.ErrorIndex}
	if p.ErrorIndex > 0 && p.ErrorIndex <= len(p.VarBinds) {
		e.Name = p.VarBinds[p.ErrorIndex-1].Name
	}
	return e
}
