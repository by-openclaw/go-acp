package consumer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"

	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/usm"
)

// SNMPv3 polling, from the manager's side.
//
// The asymmetry is the whole design. An agent is authoritative for its
// own engine: it knows its engine ID, its boot count and its clock, and
// every message it accepts is timestamped against them. A manager is
// authoritative for NOTHING. Before it can authenticate a single GET it
// has to ask the agent who it is — RFC 3414 §4's discovery exchange —
// and key the user's HMAC and cipher keys on the answer.
//
// So a v3 session is two exchanges, not one:
//
//	probe    an unauthenticated request naming no engine, msgFlags
//	         reportable, so the agent must answer
//	Report   the agent's engine ID, boots and time, unauthenticated
//	         (there is nothing yet to authenticate it WITH)
//	request  sealed as the user, keyed on that engine ID, stamped with
//	         that engine's boots and current time
//
// And it can need doing again mid-session: an agent that reboots gets a
// new boot count, and every message keyed on the old one falls outside
// its RFC 3414 §2.2.3 time window. The agent says so with a Report, and
// the manager re-discovers and retries rather than reporting a live
// device as unreachable — which is what a manager that ignored Reports
// would do, for exactly as long as the agent stayed up.

// V3 is the user a v3 session authenticates as.
//
// It is a whole credential rather than a community string: a name, what
// it signs with and what it encrypts with. An empty Auth and Priv is
// noAuthNoPriv — a legitimate level that identifies a user without
// protecting the exchange, and the only one an agent can offer before
// anybody has shared a passphrase.
type V3 struct {
	User     string
	Auth     usm.AuthProtocol
	AuthPass string
	Priv     usm.PrivProtocol
	PrivPass string
	// Context scopes the PDU inside the agent. Empty is the agent's
	// default context, which is what every device in this plant serves.
	Context string
}

// user renders the credential the way usm takes it.
func (v V3) user() usm.User {
	return usm.User{
		Name: v.User, Auth: v.Auth, AuthPass: v.AuthPass,
		Priv: v.Priv, PrivPass: v.PrivPass,
	}
}

// ErrReport is a Report PDU where a Response was expected: the agent
// refused the message at the security layer rather than answering it.
// Discovery and the time-window resync are built on it.
var ErrReport = errors.New("snmp: the agent answered with a Report")

// Compliance events this file can raise.
const (
	// DiscoveryNotAReport is an agent answering a discovery probe with
	// something other than a Report. RFC 3414 §4 says Report; several
	// agents answer with a Response carrying the same engine identity,
	// which is usable, so it is absorbed and counted.
	DiscoveryNotAReport = "snmp_discovery_not_a_report"
	// EngineChanged is an agent whose engine ID is not the one this
	// session discovered — a different box answering the same address,
	// or a proxy in between.
	EngineChanged = "snmp_engine_id_changed"
	// EngineResynced is a session that had to re-run discovery because
	// the agent refused a message on time-window grounds. Normal after
	// an agent reboots; frequent means something is resetting, or two
	// engines are answering one address.
	EngineResynced = "snmp_engine_resynced"
)

// usmStats are the counters a Report names (RFC 3414 §5). Only the two
// that a manager can DO something about are distinguished here: the
// rest are the agent saying the credential is wrong, and retrying with
// the same credential would be a loop.
var (
	usmStatsUnknownEngineIDs  = codec.MustParseOID("1.3.6.1.6.3.15.1.1.4.0")
	usmStatsNotInTimeWindows  = codec.MustParseOID("1.3.6.1.6.3.15.1.1.2.0")
	usmStatsUnsupportedSecLvl = codec.MustParseOID("1.3.6.1.6.3.15.1.1.1.0")
	usmStatsUnknownUserNames  = codec.MustParseOID("1.3.6.1.6.3.15.1.1.3.0")
	usmStatsWrongDigests      = codec.MustParseOID("1.3.6.1.6.3.15.1.1.5.0")
	usmStatsDecryptionErrors  = codec.MustParseOID("1.3.6.1.6.3.15.1.1.6.0")
)

// discover performs RFC 3414 §4 and leaves the session able to seal.
//
// It is called once from Dial, and again whenever an agent reports that
// this manager's idea of its clock is stale.
func (s *Session) discover(ctx context.Context) error {
	params, _ := codec.EncodeUSMParameters(codec.USMParameters{})
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	conn := s.conn
	s.mu.Unlock()

	probe := codec.Message{
		Version: codec.Version3,
		V3: &codec.V3{
			ID:      s.nextMsgID(),
			MaxSize: codec.DefaultMaxSize,
			// Reportable, or a conforming agent is entitled to say
			// nothing at all and discovery becomes a timeout.
			Flags:              codec.FlagReportable,
			SecurityModel:      codec.SecurityModelUSM,
			SecurityParameters: params,
		},
		PDU: &codec.PDU{Type: codec.PDUTypeGet, RequestID: id},
	}
	// The probe is built here from constants and cannot fail to encode;
	// an empty result — never expected — simply sends nothing and times
	// out as an unanswered discovery would.
	raw, _ := codec.Encode(probe) //nolint:errcheck // see comment above

	reply, err := s.exchange(ctx, conn, raw)
	if err != nil {
		return fmt.Errorf("snmp: v3 discovery: %w", err)
	}
	m, err := codec.Decode(reply)
	if err != nil {
		return fmt.Errorf("snmp: v3 discovery reply: %w", err)
	}
	if m.V3 == nil {
		return fmt.Errorf("snmp: v3 discovery answered in %s", m.Version)
	}
	if m.PDU == nil || m.PDU.Type != codec.PDUTypeReport {
		// Usable as long as it carries the engine identity, so it is
		// taken and counted rather than refused.
		s.prof.Note(DiscoveryNotAReport)
	}
	p, _, err := codec.DecodeUSMParameters(m.V3.SecurityParameters)
	if err != nil {
		return fmt.Errorf("snmp: v3 discovery parameters: %w", err)
	}
	if len(p.AuthoritativeEngineID) == 0 {
		return errors.New("snmp: the agent named no engine ID to authenticate against")
	}

	engine, err := usm.NewRemoteEngine(p.AuthoritativeEngineID,
		p.AuthoritativeEngineBoots, p.AuthoritativeEngineTime, s.clk)
	if err != nil {
		return fmt.Errorf("snmp: v3 engine: %w", err)
	}
	if err := engine.AddUser(s.opts.V3.user()); err != nil {
		return fmt.Errorf("snmp: v3 user: %w", err)
	}

	s.mu.Lock()
	prev := s.engine
	s.engine = engine
	s.mu.Unlock()

	if prev != nil && !engineIDsEqual(prev.ID(), engine.ID()) {
		s.prof.Note(EngineChanged)
	}
	s.logger.Debug("snmp: v3 engine discovered",
		slog.String("engine_id", fmt.Sprintf("%x", p.AuthoritativeEngineID)),
		slog.Int64("boots", int64(p.AuthoritativeEngineBoots)),
		slog.Int64("time", int64(p.AuthoritativeEngineTime)),
		slog.String("level", s.opts.V3.user().SecurityLevel()))
	return nil
}

// exchange is one send and one read on an already-dialled socket, with
// the session's retries. Discovery uses it because it cannot go through
// roundTrip: there is no engine yet to seal with, and the reply it
// wants is the one roundTrip is written to reject.
func (s *Session) exchange(ctx context.Context, conn net.Conn, raw []byte) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= s.opts.Retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		deadline := s.clk.Now().Add(s.opts.Timeout)
		if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
			deadline = d
		}
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, fmt.Errorf("snmp: set deadline: %w", err)
		}
		if _, err := conn.Write(raw); err != nil {
			return nil, fmt.Errorf("snmp: send: %w", err)
		}
		buf := make([]byte, codec.MaxMessageSize)
		n, err := conn.Read(buf)
		if err == nil {
			return buf[:n], nil
		}
		var nerr net.Error
		if errors.As(err, &nerr) && nerr.Timeout() {
			lastErr = ErrTimeout
			continue
		}
		return nil, fmt.Errorf("snmp: receive: %w", err)
	}
	return nil, fmt.Errorf("%w after %d attempt(s)", lastErr, s.opts.Retries+1)
}

// sealV3 renders one request as the configured user.
//
// It is called per ATTEMPT rather than once per request, because the
// engine time it stamps is only valid for the RFC 3414 §2.2.3 window
// around now — a retry sent two seconds later carries a fresh stamp,
// not a stale one the agent would refuse.
func (s *Session) sealV3(p *codec.PDU) ([]byte, error) {
	s.mu.Lock()
	engine := s.engine
	s.mu.Unlock()
	if engine == nil {
		return nil, errors.New("snmp: v3 session has no discovered engine")
	}
	msg := codec.Message{
		Version: codec.Version3,
		V3: &codec.V3{
			ID:      s.nextMsgID(),
			MaxSize: codec.DefaultMaxSize,
			// Reportable: this manager wants to be TOLD when a message
			// is refused. The alternative is a timeout that looks like
			// a dead device.
			Flags:           codec.FlagReportable,
			SecurityModel:   codec.SecurityModelUSM,
			ContextEngineID: engine.ID(),
			ContextName:     s.opts.V3.Context,
		},
		PDU: p,
	}
	return engine.Seal(msg, s.opts.V3.User)
}

// openV3 verifies one reply and hands back its PDU.
//
// A Report comes back as ErrReport wrapped with what the agent's
// counter said, because the counter is the diagnosis: a wrong password
// and a rebooted agent are the same silence otherwise.
func (s *Session) openV3(raw []byte) (*codec.PDU, error) {
	s.mu.Lock()
	engine := s.engine
	s.mu.Unlock()
	if engine == nil {
		return nil, errors.New("snmp: v3 session has no discovered engine")
	}
	m, _, err := engine.Open(raw)
	if err != nil {
		// A Report about a message we could not open is sent
		// unauthenticated by definition — the agent is saying it could
		// not agree with us on keys or time, so it cannot sign the
		// complaint either. Decode it plainly and read the counter.
		if plain, derr := codec.Decode(raw); derr == nil && plain.PDU != nil &&
			plain.PDU.Type == codec.PDUTypeReport {
			return nil, reportError(plain.PDU)
		}
		return nil, err
	}
	// m.PDU is non-nil here by Open's contract — it decodes the scoped
	// PDU or it fails — and usm asserts that contract in its own tests.
	if m.PDU.Type == codec.PDUTypeReport {
		return nil, reportError(m.PDU)
	}
	return m.PDU, nil
}

// ReportError is a Report PDU, carrying which counter the agent named.
//
// It is a TYPE and not a message, because what a caller does next
// depends on which counter it was: a stale clock is worth rediscovering
// and retrying, a wrong password is worth stopping on. Deciding that by
// matching the text of an error is how a typo becomes a retry loop.
type ReportError struct {
	// Counter is the usmStats object the agent incremented (RFC 3414 §5).
	Counter codec.OID
	// Why is what an operator does about it.
	Why string
	// Retry says whether re-running discovery could clear it.
	Retry bool
}

func (e *ReportError) Error() string {
	if e.Why == "" {
		return fmt.Sprintf("snmp: the agent answered with a Report (%s)", e.Counter)
	}
	return fmt.Sprintf("snmp: the agent refused the message: %s", e.Why)
}

// Is makes errors.Is(err, ErrReport) true for every Report.
func (e *ReportError) Is(target error) bool { return target == ErrReport }

// acceptableV3 is [Session.acceptable] for a sealed session: open the
// datagram as the user, then apply the same correlation rules.
func (s *Session) acceptableV3(raw []byte, id int32) (*codec.PDU, error) {
	pdu, err := s.openV3(raw)
	if err != nil {
		var re *ReportError
		if errors.As(err, &re) {
			// A refusal the agent named. It belongs to this request.
			return nil, err
		}
		// Anything else is a datagram this session cannot verify —
		// somebody else's, or one that was tampered with. Counted, and
		// waited past, exactly as an unverifiable v2c reply would be.
		s.prof.Note(UnsolicitedResponse)
		s.logger.Debug("snmp: unopenable v3 reply", slog.String("err", err.Error()))
		return nil, nil
	}
	if pdu.Type != codec.PDUTypeResponse {
		s.prof.Note(UnsolicitedResponse)
		return nil, nil
	}
	if pdu.RequestID != id {
		s.prof.Note(UnsolicitedResponse)
		s.logger.Debug("snmp: v3 reply for another request",
			slog.Int64("got", int64(pdu.RequestID)), slog.Int64("want", int64(id)))
		return nil, nil
	}
	return pdu, nil
}

// reportError names what the Report's counter means, in the words an
// operator would use to fix it.
func reportError(p *codec.PDU) error {
	if len(p.VarBinds) == 0 {
		return &ReportError{}
	}
	name := p.VarBinds[0].Name
	switch {
	case name.Compare(usmStatsNotInTimeWindows) == 0:
		return &ReportError{Counter: name, Retry: true,
			Why: "this manager's clock for the agent is stale (usmStatsNotInTimeWindows) " +
				"— the agent rebooted, or the session outlived its time window"}
	case name.Compare(usmStatsUnknownEngineIDs) == 0:
		return &ReportError{Counter: name, Retry: true,
			Why: "the agent does not know the engine ID we used (usmStatsUnknownEngineIDs)"}
	case name.Compare(usmStatsUnknownUserNames) == 0:
		return &ReportError{Counter: name,
			Why: "the agent has no such user (usmStatsUnknownUserNames) — check --user " +
				"against the agent's own USM table"}
	case name.Compare(usmStatsWrongDigests) == 0:
		return &ReportError{Counter: name,
			Why: "the authentication password is wrong, or the protocol is (usmStatsWrongDigests)"}
	case name.Compare(usmStatsDecryptionErrors) == 0:
		return &ReportError{Counter: name,
			Why: "the privacy password is wrong, or the cipher is (usmStatsDecryptionErrors)"}
	case name.Compare(usmStatsUnsupportedSecLvl) == 0:
		return &ReportError{Counter: name,
			Why: "the agent does not offer this user the security level asked for " +
				"(usmStatsUnsupportedSecurityLevels)"}
	}
	return &ReportError{Counter: name}
}

// recoverable reports whether re-discovering the agent's engine could
// make this Report go away. A stale clock or an engine ID the agent has
// forgotten is worth one more try; a wrong password is not, and
// retrying it only locks somebody out more slowly.
func recoverable(err error) bool {
	var re *ReportError
	if !errors.As(err, &re) {
		return false
	}
	return re.Retry
}

func engineIDsEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// nextMsgID hands out the v3 message ID, which is numbered apart from
// the PDU's request-id: a Report about a message that could not be
// decrypted can still be matched to it.
func (s *Session) nextMsgID() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgID++
	return s.msgID
}
