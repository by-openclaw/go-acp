package consumer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"dhs/internal/consumer/compliance"
	"dhs/internal/plugin"
	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/usm"
)

// TrapPort is where a manager listens for notifications.
const TrapPort = 162

// Trap is a notification as RECEIVED, normalised across the three wire
// formats so a handler is written once.
//
// The normalisation is the point. A v1 trap carries its enterprise,
// agent address and two trap numbers in the PDU; a v2c or v3
// notification carries an identity OID in its second varbind and no
// agent address at all. A handler that had to know which had arrived
// would be written for whichever the plant had on the day.
type Trap struct {
	// From is the sender's socket address, which for v2c and v3 is the
	// only thing that says which device this came from.
	From *net.UDPAddr
	// Version is what actually arrived, kept because an operator
	// migrating a plant needs to see which devices have moved.
	Version codec.Version
	// Community is the v1/v2c password the sender used. Empty for v3.
	Community string
	// User is the USM user a v3 notification authenticated as. Empty
	// otherwise.
	User string
	// SecurityLevel is the v3 protection the sender used, as an operator
	// names it. Empty for v1 and v2c, which have none.
	SecurityLevel string

	// Enterprise is the v1 PDU's enterprise. Derived for v2c and v3 by
	// stripping the RFC 3584 §3.1 mapping off TrapOID when it fits.
	Enterprise codec.OID
	// AgentAddr is the v1 PDU's agent-address. Nil for v2c and v3.
	AgentAddr net.IP
	// Generic and Specific are the v1 trap numbers, derived for v2c and
	// v3 from TrapOID.
	Generic  codec.GenericTrap
	Specific int
	// TrapOID is the v2c identity. Synthesised for v1 by the same
	// RFC 3584 mapping, so a handler can switch on one field.
	TrapOID codec.OID
	// Uptime is sysUpTime at the event, in centiseconds.
	Uptime uint32
	// VarBinds are the sender's own bindings, WITHOUT the two mandatory
	// ones a v2c notification opens with — those are Uptime and TrapOID
	// above, and leaving them in the list as well would have every
	// handler skip two entries it did not ask for.
	VarBinds []codec.VarBind
}

// Compliance events the listener records.
const (
	// TrapMissingBindings is a v2c or v3 notification that did not open
	// with sysUpTime.0 and snmpTrapOID.0. RFC 3416 §4.2.6 requires them
	// and a conforming receiver drops the notification; this one absorbs
	// it, because a dropped alarm is worse than a malformed one.
	TrapMissingBindings = "snmp_trap_missing_mandatory_bindings"
	// TrapUndecodable is a datagram on the trap port that is not SNMP.
	TrapUndecodable = "snmp_trap_undecodable"
	// TrapUnauthenticated is a v3 notification that failed USM.
	TrapUnauthenticated = "snmp_trap_failed_authentication"
	// TrapUnexpectedPDU is something other than a notification arriving
	// on the trap port — a Response, or a request from a confused peer.
	TrapUnexpectedPDU = "snmp_trap_unexpected_pdu"
)

// sysUpTimeInstance and snmpTrapOIDInstance are the two bindings a v2c
// notification must open with.
var (
	sysUpTimeInstance   = codec.MustParseOID("1.3.6.1.2.1.1.3.0")
	snmpTrapOIDInstance = codec.MustParseOID("1.3.6.1.6.3.1.1.4.1.0")
)

// genericTrapOIDs is RFC 3418 §2's identity for each generic trap, so a
// v1 trap can be given the same TrapOID a v2c one would carry.
var genericTrapOIDs = map[string]codec.GenericTrap{
	"1.3.6.1.6.3.1.1.5.1": codec.ColdStart,
	"1.3.6.1.6.3.1.1.5.2": codec.WarmStart,
	"1.3.6.1.6.3.1.1.5.3": codec.LinkDown,
	"1.3.6.1.6.3.1.1.5.4": codec.LinkUp,
	"1.3.6.1.6.3.1.1.5.5": codec.AuthenticationFailure,
	"1.3.6.1.6.3.1.1.5.6": codec.EGPNeighborLoss,
}

// ListenerOptions configure a trap receiver.
type ListenerOptions struct {
	// Addr is where to bind. Empty means every interface on [TrapPort].
	Addr string
	// Communities are the v1/v2c passwords accepted. Empty accepts any,
	// which is what a diagnostic listener wants and what a production
	// one must not have — so it is a decision the caller makes, not a
	// default that happens.
	Communities []string
	// Engine is the local USM engine, needed to open v3 notifications.
	// Without one, v3 notifications are counted and dropped.
	Engine *usm.Engine
	// Compliance records what senders did that the RFCs do not describe.
	Compliance compliance.Recorder
}

// Listener receives notifications.
//
// It answers nothing. A trap is unacknowledged by definition, and an
// InformRequest — which is acknowledged — is a separate verb this does
// not yet serve; a listener that half-answered informs would leave
// senders retrying forever, which is worse than not offering it.
type Listener struct {
	opts   ListenerOptions
	logger *slog.Logger
	prof   compliance.Recorder
	// listen binds through the injected transport rather than
	// net.ListenUDP, so a plant's socket policy applies to the trap port
	// as it does everywhere else. The architecture gate enforces it.
	listen func(ctx context.Context, network, addr string) (net.PacketConn, error)

	mu     sync.Mutex
	conn   net.PacketConn
	closed bool
}

// NewListener builds a receiver.
func NewListener(opts ListenerOptions, deps plugin.Deps) *Listener {
	deps = deps.WithDefaults()
	return &Listener{
		opts:   opts,
		logger: plugin.LoggerOrDefault(deps.Logger),
		prof:   opts.Compliance,
		listen: deps.Net.ListenPacket,
	}
}

// Addr is the bound address, or nil before Listen.
func (l *Listener) Addr() net.Addr {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn == nil {
		return nil
	}
	return l.conn.LocalAddr()
}

// Listen binds and delivers each notification to fn until ctx is done or
// Close is called.
//
// fn runs on the read loop, so a handler that blocks stops the listener
// hearing anything else. That is deliberate: a trap receiver that
// buffered would drop the oldest alarm under load, and an operator would
// rather the queue be visible in the kernel's socket buffer than
// invisible in ours.
func (l *Listener) Listen(ctx context.Context, fn func(Trap)) error {
	addr := l.opts.Addr
	if addr == "" {
		addr = fmt.Sprintf(":%d", TrapPort)
	}
	conn, err := l.listen(ctx, "udp4", addr)
	if err != nil {
		return fmt.Errorf("snmp: listen %q: %w", addr, err)
	}

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		_ = conn.Close()
		return net.ErrClosed
	}
	l.conn = conn
	l.mu.Unlock()

	l.logger.Info("snmp trap listener", slog.String("addr", conn.LocalAddr().String()))

	go func() {
		<-ctx.Done()
		_ = l.Close()
	}()

	buf := make([]byte, codec.MaxMessageSize)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		// A datagram from something that is not a UDP address cannot
		// have come from an agent, and the sender's address is the only
		// thing that identifies a v2c notification.
		udpFrom, ok := from.(*net.UDPAddr)
		if !ok {
			l.prof.Note(TrapUndecodable)
			continue
		}
		if t, ok := l.decode(buf[:n], udpFrom); ok {
			fn(t)
		}
	}
}

// Close stops the listener. Idempotent.
func (l *Listener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.conn == nil {
		return nil
	}
	return l.conn.Close()
}

// decode turns a datagram into a Trap, or drops it.
func (l *Listener) decode(raw []byte, from *net.UDPAddr) (Trap, bool) {
	m, err := codec.Decode(raw)
	if err != nil {
		l.prof.Note(TrapUndecodable)
		l.logger.Debug("snmp: undecodable datagram on the trap port",
			slog.String("from", from.String()), slog.String("err", err.Error()))
		return Trap{}, false
	}

	t := Trap{From: from, Version: m.Version, Community: m.Community}

	if m.Version == codec.Version3 {
		opened, user, err := l.openV3(raw)
		if err != nil {
			l.prof.Note(TrapUnauthenticated)
			l.logger.Warn("snmp: v3 notification refused",
				slog.String("from", from.String()), slog.String("err", err.Error()))
			return Trap{}, false
		}
		m = opened
		t.User, t.SecurityLevel = user.Name, user.SecurityLevel()
		t.Community = ""
	} else if !l.admits(m.Community) {
		l.prof.Note(TrapUnauthenticated)
		l.logger.Warn("snmp: notification with an unaccepted community",
			slog.String("from", from.String()))
		return Trap{}, false
	}

	if m.TrapV1 != nil {
		fillFromV1(&t, m.TrapV1)
		return t, true
	}
	if m.PDU == nil || m.PDU.Type != codec.PDUTypeTrapV2 {
		l.prof.Note(TrapUnexpectedPDU)
		l.logger.Debug("snmp: not a notification",
			slog.String("from", from.String()), slog.String("type", m.Type().String()))
		return Trap{}, false
	}
	l.fillFromV2(&t, m.PDU)
	return t, true
}

// openV3 verifies and decrypts, or says why not.
func (l *Listener) openV3(raw []byte) (codec.Message, usm.User, error) {
	if l.opts.Engine == nil {
		return codec.Message{}, usm.User{},
			fmt.Errorf("no USM engine is configured, so v3 cannot be read")
	}
	return l.opts.Engine.Open(raw)
}

// admits checks a v1/v2c community. An empty accept-list admits any,
// which is a diagnostic listener's job and a production one's hazard —
// the caller decides, and the log line above says when one was refused.
func (l *Listener) admits(community string) bool {
	if len(l.opts.Communities) == 0 {
		return true
	}
	for _, c := range l.opts.Communities {
		if c == community {
			return true
		}
	}
	return false
}

// fillFromV1 reads the fields a Trap-PDU carries directly, and
// synthesises the v2c identity from them so a handler can switch on one
// field whatever arrived.
func fillFromV1(t *Trap, v1 *codec.TrapV1) {
	t.Enterprise = v1.Enterprise
	t.AgentAddr = v1.AgentAddr
	t.Generic, t.Specific = v1.Generic, v1.Specific
	t.Uptime = v1.Timestamp
	t.VarBinds = v1.VarBinds
	t.TrapOID = trapOIDFor(v1.Enterprise, v1.Generic, v1.Specific)
}

// trapOIDFor is the RFC 3584 §3.1 mapping every v1-to-v2 proxy uses.
func trapOIDFor(enterprise codec.OID, generic codec.GenericTrap, specific int) codec.OID {
	for s, g := range genericTrapOIDs {
		if g == generic {
			return codec.MustParseOID(s)
		}
	}
	return enterprise.Append(0, uint32(specific))
}

// fillFromV2 reads the two mandatory bindings and derives the v1 fields
// from them, so the same handler sees the same shape.
func (l *Listener) fillFromV2(t *Trap, p *codec.PDU) {
	binds := p.VarBinds

	if len(binds) >= 2 &&
		binds[0].Name.Compare(sysUpTimeInstance) == 0 &&
		binds[1].Name.Compare(snmpTrapOIDInstance) == 0 {
		t.Uptime = uint32(binds[0].Value.Uint)
		t.TrapOID = binds[1].Value.OID
		t.VarBinds = binds[2:]
	} else {
		// RFC 3416 §4.2.6 requires them. A conforming receiver drops the
		// notification; this one keeps it, because a dropped alarm is
		// worse than a malformed one — and counts the deviation so the
		// device can be fixed.
		l.prof.Note(TrapMissingBindings)
		t.VarBinds = binds
	}

	// Give the handler the v1 view too, derived the other way.
	if g, ok := genericTrapOIDs[t.TrapOID.String()]; ok {
		t.Generic = g
		return
	}
	t.Generic = codec.EnterpriseSpecific
	if n := len(t.TrapOID); n >= 3 && t.TrapOID[n-2] == 0 {
		// enterprise.0.specific — the mapping run backwards.
		t.Enterprise = t.TrapOID[:n-2]
		t.Specific = int(t.TrapOID[n-1])
	}
}

// String renders a trap as one line, which is what a listener prints and
// what an operator greps.
func (t Trap) String() string {
	who := "?"
	if t.From != nil {
		who = t.From.IP.String()
	}
	level := t.Community
	if t.Version == codec.Version3 {
		level = t.User + "/" + t.SecurityLevel
	}
	return fmt.Sprintf("%s %s %s %s uptime=%s binds=%d",
		who, t.Version, level, t.TrapOID, centiseconds(t.Uptime), len(t.VarBinds))
}

// centiseconds renders sysUpTime as a duration. TimeTicks are hundredths
// of a second, and a receiver that read them as seconds would report an
// uptime a hundred times too long.
func centiseconds(v uint32) time.Duration {
	return time.Duration(v) * 10 * time.Millisecond
}
