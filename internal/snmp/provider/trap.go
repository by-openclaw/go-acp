package provider

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"dhs/internal/plugin"
	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/usm"
)

// TrapPort is where a manager listens for notifications. Cerebrum's own
// receiver is on it; like [DefaultPort] it is a default, not a rule.
const TrapPort = 162

// Well-known OIDs a v2c notification is REQUIRED to carry (RFC 3416
// §4.2.6): the first varbind is sysUpTime.0 and the second is
// snmpTrapOID.0, in that order, before anything the sender wants to add.
// A notification missing them is dropped by a conforming receiver, which
// is the failure that looks like a network problem and is not.
var (
	SysUpTimeInstance = codec.MustParseOID("1.3.6.1.2.1.1.3.0")
	SNMPTrapOID       = codec.MustParseOID("1.3.6.1.6.3.1.1.4.1.0")
)

// Standard v2c notification identities (RFC 3418 §2), for the generic
// events a v1 sender expresses through the generic-trap field.
var (
	ColdStartOID             = codec.MustParseOID("1.3.6.1.6.3.1.1.5.1")
	WarmStartOID             = codec.MustParseOID("1.3.6.1.6.3.1.1.5.2")
	LinkDownOID              = codec.MustParseOID("1.3.6.1.6.3.1.1.5.3")
	LinkUpOID                = codec.MustParseOID("1.3.6.1.6.3.1.1.5.4")
	AuthenticationFailureOID = codec.MustParseOID("1.3.6.1.6.3.1.1.5.5")
	EGPNeighborLossOID       = codec.MustParseOID("1.3.6.1.6.3.1.1.5.6")
)

// Notification is one event, expressed the way the SENDER thinks about
// it rather than the way either wire format lays it out.
//
// The two formats disagree about almost everything — v1 puts the
// enterprise, the agent address and a pair of trap numbers in the PDU;
// v2c puts an identity OID in a varbind and has no agent address at all
// — so a caller that had to build both would build one and forget the
// other. This is the shape both are rendered FROM.
type Notification struct {
	// Enterprise is the sender's sysObjectID. It is the v1 PDU's
	// enterprise field, and the stem the v1 trap number hangs off when a
	// v2c identity has to be derived from it.
	Enterprise codec.OID
	// AgentAddr is the v1 PDU's agent-address. v2c has no such field; a
	// v2c receiver uses the datagram's source address.
	AgentAddr net.IP
	// Generic is the RFC 1157 generic trap. EnterpriseSpecific means
	// "look at Specific".
	Generic codec.GenericTrap
	// Specific is the enterprise-specific trap number, meaningful only
	// when Generic is EnterpriseSpecific.
	Specific int
	// Uptime is sysUpTime at the moment of the event, in centiseconds.
	Uptime uint32
	// VarBinds are the sender's own bindings. For v2c they follow the
	// two mandatory ones rather than replacing them.
	VarBinds []codec.VarBind
}

// TrapOID is the v2c identity for this notification.
//
// For a generic event it is the RFC 3418 well-known OID. For an
// enterprise-specific one, RFC 3584 §3.1 gives the mapping every v1-to-v2
// proxy uses: enterprise, then a zero arc, then the specific number —
// the zero is what keeps a vendor's trap 1 from colliding with the
// vendor's object 1.
func (n Notification) TrapOID() codec.OID {
	switch n.Generic {
	case codec.ColdStart:
		return ColdStartOID
	case codec.WarmStart:
		return WarmStartOID
	case codec.LinkDown:
		return LinkDownOID
	case codec.LinkUp:
		return LinkUpOID
	case codec.AuthenticationFailure:
		return AuthenticationFailureOID
	case codec.EGPNeighborLoss:
		return EGPNeighborLossOID
	default:
		return n.Enterprise.Append(0, uint32(n.Specific))
	}
}

// Message renders the notification for one version.
//
// v1 produces a Trap-PDU; v2c and v3 produce an SNMPv2-Trap-PDU whose
// first two varbinds are the mandatory sysUpTime.0 and snmpTrapOID.0.
// This is the one place all three shapes are built, so they cannot
// drift.
//
// A v3 message comes back UNSEALED: the security parameters, the digest
// and any encryption are the engine's to add, because they depend on
// keys this package does not hold. [TrapSender] does that step.
func (n Notification) Message(v codec.Version, community string, requestID int32) (codec.Message, error) {
	switch v {
	case codec.Version1:
		return codec.Message{
			Version:   codec.Version1,
			Community: community,
			TrapV1: &codec.TrapV1{
				Enterprise: n.Enterprise,
				AgentAddr:  n.AgentAddr,
				Generic:    n.Generic,
				Specific:   n.Specific,
				Timestamp:  n.Uptime,
				VarBinds:   n.VarBinds,
			},
		}, nil

	case codec.Version2c, codec.Version3:
		vbs := make([]codec.VarBind, 0, len(n.VarBinds)+2)
		vbs = append(vbs,
			codec.VarBind{Name: SysUpTimeInstance, Value: codec.TimeTicks(n.Uptime)},
			codec.VarBind{Name: SNMPTrapOID, Value: codec.ObjectID(n.TrapOID())})
		vbs = append(vbs, n.VarBinds...)

		m := codec.Message{
			Version:   v,
			Community: community,
			PDU: &codec.PDU{
				Type: codec.PDUTypeTrapV2, RequestID: requestID, VarBinds: vbs,
			},
		}
		if v == codec.Version3 {
			// A notification is not reportable: nobody is waiting to
			// answer it, so asking for a Report would only produce
			// traffic to a sender that has stopped listening.
			m.V3 = &codec.V3{ID: requestID}
			m.Community = ""
		}
		return m, nil

	default:
		return codec.Message{}, fmt.Errorf("snmp: cannot send a notification as %s", v)
	}
}

// TrapDestination is one manager to notify.
//
// Version is per destination rather than per sender because a plant
// mid-migration has both: the old NMS listening for v1 traps and the new
// one for v2c notifications, from the same device, at the same time.
type TrapDestination struct {
	Addr    string
	Version codec.Version
	// Community is the v1/v2c password. Ignored for v3.
	Community string
	// User names the USM user a v3 notification is sent as. Required for
	// v3 and ignored otherwise; the user itself, with its keys, lives on
	// the engine given to [NewTrapSenderV3].
	User string
}

// TrapSender emits notifications to a set of destinations.
//
// It dials one socket per destination on demand and keeps it: a trap
// burst during an alarm storm should not be a socket setup per packet,
// and a dialed UDP socket also pins the source address the receiver will
// see, which is what a manager's own access list is written against.
type TrapSender struct {
	logger *slog.Logger
	// dial goes through the injected transport rather than net.Dial, so
	// a plant's dial policy — source address, timeouts, whatever the
	// deployment needs — applies to traps as it does to everything else.
	// The architecture gate enforces it.
	dial func(ctx context.Context, network, addr string) (net.Conn, error)

	// engine is this process's authoritative USM engine, needed only for
	// v3 destinations. A sender with none refuses a v3 destination
	// rather than silently sending it unauthenticated.
	engine *usm.Engine

	mu    sync.Mutex
	dests []TrapDestination
	conns map[string]net.Conn
	// nextID numbers v2c notifications. v1 traps have no request-id.
	nextID int32
	closed bool
}

// NewTrapSender builds a sender for v1 and v2c destinations. deps
// carries the transport its sockets are dialed through.
func NewTrapSender(dests []TrapDestination, deps plugin.Deps) *TrapSender {
	return NewTrapSenderV3(dests, nil, deps)
}

// NewTrapSenderV3 is NewTrapSender with the local USM engine, which a v3
// destination needs: a v3 notification is authenticated as the SENDER's
// engine, so the keys are localised against ours.
func NewTrapSenderV3(dests []TrapDestination, engine *usm.Engine, deps plugin.Deps) *TrapSender {
	deps = deps.WithDefaults()
	return &TrapSender{
		logger: plugin.LoggerOrDefault(deps.Logger),
		dial:   deps.Net.Dial,
		engine: engine,
		dests:  append([]TrapDestination(nil), dests...),
		conns:  map[string]net.Conn{},
	}
}

// Destinations reports the configured managers, which is what an
// operator asking "who would hear about this" wants to see.
func (s *TrapSender) Destinations() []TrapDestination {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]TrapDestination(nil), s.dests...)
}

// Send emits the notification to every destination.
//
// Every destination is attempted even when an earlier one fails: a trap
// is the only notice a manager gets, so one unreachable NMS must not
// silence the others. The error returned names the FIRST failure, and
// the rest are logged — a caller usually only wants to know that
// something went wrong.
func (s *TrapSender) Send(ctx context.Context, n Notification) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return net.ErrClosed
	}
	dests := append([]TrapDestination(nil), s.dests...)
	s.nextID++
	id := s.nextID
	s.mu.Unlock()

	var firstErr error
	for _, d := range dests {
		if err := ctx.Err(); err != nil {
			// A cancelled context stops the fan-out rather than sending
			// to the destinations that happen to sort late.
			if firstErr == nil {
				firstErr = err
			}
			break
		}
		if err := s.sendOne(ctx, d, n, id); err != nil {
			s.logger.Warn("snmp: trap not delivered",
				slog.String("to", d.Addr), slog.String("err", err.Error()))
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (s *TrapSender) sendOne(ctx context.Context, d TrapDestination, n Notification, id int32) error {
	msg, err := n.Message(d.Version, d.Community, id)
	if err != nil {
		return err
	}

	var raw []byte
	if d.Version == codec.Version3 {
		if s.engine == nil {
			return fmt.Errorf(
				"snmp: %s is a v3 destination and this sender has no USM engine", d.Addr)
		}
		raw, err = s.engine.Seal(msg, d.User)
	} else {
		raw, err = codec.Encode(msg)
	}
	if err != nil {
		return err
	}
	conn, err := s.connFor(ctx, d.Addr)
	if err != nil {
		return err
	}
	if _, err := conn.Write(raw); err != nil {
		// A dialed UDP socket reports the previous datagram's ICMP
		// error, so a write failure usually means the destination said
		// "port unreachable" LAST time. Drop the socket so the next
		// trap redials rather than inheriting the error.
		s.drop(d.Addr)
		return err
	}
	return nil
}

// connFor returns the socket for a destination, dialing on first use.
func (s *TrapSender) connFor(ctx context.Context, addr string) (net.Conn, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, net.ErrClosed
	}
	if c, ok := s.conns[addr]; ok {
		s.mu.Unlock()
		return c, nil
	}
	dial := s.dial
	s.mu.Unlock()

	c, err := dial(ctx, "udp4", addr)
	if err != nil {
		return nil, fmt.Errorf("snmp: trap destination %q: %w", addr, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		// Close raced this dial. The socket belongs to nobody now.
		_ = c.Close()
		return nil, net.ErrClosed
	}
	if existing, ok := s.conns[addr]; ok {
		// Another Send dialed the same destination while this one was
		// in the syscall. Keep the first and discard this.
		_ = c.Close()
		return existing, nil
	}
	s.conns[addr] = c
	return c, nil
}

func (s *TrapSender) drop(addr string) {
	s.mu.Lock()
	c, ok := s.conns[addr]
	delete(s.conns, addr)
	s.mu.Unlock()
	if ok {
		_ = c.Close()
	}
}

// Close releases every socket. Idempotent.
func (s *TrapSender) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	conns := s.conns
	s.conns = map[string]net.Conn{}
	s.mu.Unlock()

	for _, c := range conns {
		_ = c.Close()
	}
	return nil
}
