package provider

import (
	"errors"

	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/usm"
)

// The usmStats counters an agent answers with (RFC 3414 §5). Their
// VALUES are immaterial — nothing in the protocol reads them as
// numbers — but WHICH one is named is the whole diagnosis a manager
// gets, and usmStatsNotInTimeWindows is how one recovers from this
// agent rebooting.
var (
	usmUnknownEngineIDs = codec.MustParseOID("1.3.6.1.6.3.15.1.1.4.0")
	usmUnsupportedLevel = codec.MustParseOID("1.3.6.1.6.3.15.1.1.1.0")
	usmNotInTimeWindows = codec.MustParseOID("1.3.6.1.6.3.15.1.1.2.0")
	usmUnknownUserNames = codec.MustParseOID("1.3.6.1.6.3.15.1.1.3.0")
	usmWrongDigests     = codec.MustParseOID("1.3.6.1.6.3.15.1.1.5.0")
	usmDecryptionErrors = codec.MustParseOID("1.3.6.1.6.3.15.1.1.6.0")
)

// counterFor maps an Open failure onto the counter RFC 3414 §3.2 says
// to answer with. A failure with no counter of its own is reported as
// an unknown engine ID, which is what invites the manager to
// re-discover — the most useful thing it can do about a message this
// agent could not place at all.
func counterFor(err error) codec.OID {
	var ae *usm.AuthError
	if !errors.As(err, &ae) {
		return usmUnknownEngineIDs
	}
	switch ae.Why {
	case usm.FailureUnknownUser:
		return usmUnknownUserNames
	case usm.FailureWrongDigest:
		return usmWrongDigests
	case usm.FailureNotInTimeWindow:
		return usmNotInTimeWindows
	case usm.FailureUnsupportedLevel:
		return usmUnsupportedLevel
	case usm.FailureDecryption:
		return usmDecryptionErrors
	case usm.FailureUnknownEngineID, usm.FailureOther:
		return usmUnknownEngineIDs
	}
	return usmUnknownEngineIDs
}

// SetEngine gives the agent a USM engine so it answers SNMPv3. Nil (the
// default) keeps the agent v1/v2c only. Call it before Serve.
func (s *Server) SetEngine(e *usm.Engine) { s.engine = e }

// handleV3 answers one v3 datagram. A discovery probe (no authoritative
// engine ID yet) gets a Report carrying this engine's identity; an
// authenticated request is opened, answered by the ordinary agent, and the
// reply is sealed back as the same user.
func (s *Server) handleV3(raw []byte, req codec.Message) ([]byte, bool) {
	if s.engine == nil {
		return nil, false // not configured for v3
	}
	if isDiscovery(req) {
		return s.report(req, usmUnknownEngineIDs)
	}

	opened, user, err := s.engine.Open(raw)
	if err != nil {
		// Answer with a Report naming WHAT failed, so the manager can
		// act: re-discover for a stale clock, stop for a bad password.
		// Silence here is a timeout that looks like a dead agent, and
		// the same counter for every cause is a manager that can never
		// recover from this agent rebooting.
		s.logger.Debug("snmp agent: v3 open failed, sending report",
			"err", err.Error(), "counter", counterFor(err).String())
		return s.report(req, counterFor(err))
	}

	// Mark the opened message v3 so the handler takes the USM-authenticated
	// path (no community check) rather than the v1/v2c one, and build the
	// response PDU without framing — v3 seals it below rather than encoding
	// it with a community.
	opened.Version = codec.Version3
	out, ok := s.agent.respondPDU(opened)
	if !ok {
		return nil, false
	}

	msg := codec.Message{
		Version: codec.Version3,
		V3: &codec.V3{
			ID:              v3ID(req),
			MaxSize:         codec.MaxMessageSize,
			ContextEngineID: s.engine.ID(),
		},
		PDU: &out,
	}
	// Sealing after a successful Open cannot fail (the user is known and
	// the PDU is one the agent built), so an empty result — never expected
	// — simply means nothing is sent.
	sealed, _ := s.engine.Seal(msg, user.Name) //nolint:errcheck // see comment above
	return sealed, len(sealed) > 0
}

// isDiscovery reports whether a v3 message is an engine-discovery probe:
// it names no authoritative engine yet.
func isDiscovery(req codec.Message) bool {
	if req.V3 == nil {
		return false
	}
	p, _, err := codec.DecodeUSMParameters(req.V3.SecurityParameters)
	if err != nil {
		return false
	}
	return len(p.AuthoritativeEngineID) == 0
}

// report builds an unauthenticated Report that carries this engine's ID,
// boots and time — the answer a manager discovers the agent from — and
// names the counter that says why.
//
// Unauthenticated on purpose: a Report answers a message this engine
// could NOT agree with the sender on, so it has nothing to sign it
// with that the sender would accept. RFC 3414 §3.2 allows this, and it
// is the only thing that can work for discovery, where the manager has
// no keys yet.
func (s *Server) report(req codec.Message, counter codec.OID) ([]byte, bool) {
	params, _ := codec.EncodeUSMParameters(codec.USMParameters{
		AuthoritativeEngineID:    s.engine.ID(),
		AuthoritativeEngineBoots: s.engine.Boots(),
		AuthoritativeEngineTime:  s.engine.Time(),
	})
	msg := codec.Message{
		Version: codec.Version3,
		V3: &codec.V3{
			ID:                 v3ID(req),
			MaxSize:            codec.MaxMessageSize,
			SecurityModel:      codec.SecurityModelUSM,
			SecurityParameters: params,
			ContextEngineID:    s.engine.ID(),
		},
		PDU: &codec.PDU{
			Type:      codec.PDUTypeReport,
			RequestID: reportRequestID(req),
			VarBinds:  []codec.VarBind{{Name: counter, Value: codec.Counter32(1)}},
		},
	}
	// A report is a message the agent fully builds from its own valid
	// engine identity, so it always encodes; an empty result — never
	// expected — means nothing is sent.
	raw, _ := codec.Encode(msg) //nolint:errcheck // see comment above
	return raw, len(raw) > 0
}

func v3ID(req codec.Message) int32 {
	if req.V3 != nil {
		return req.V3.ID
	}
	return 0
}

func reportRequestID(req codec.Message) int32 {
	if req.PDU != nil {
		return req.PDU.RequestID
	}
	return 0
}

// DefaultV3User is the USM user this agent answers as when nobody
// named one.
//
// v3 is ON by default, and that is a deliberate change of posture: v1
// and v2c put their password in clear in every datagram, so an agent
// that only spoke them would oblige every manager in the plant to do
// the same. An agent that also speaks v3 lets a manager that can
// authenticate do so, while the devices that predate v3 keep working.
//
// With no --v3-auth the user is noAuthNoPriv: it identifies the
// manager without protecting the exchange, which is no weaker than the
// community it sits beside and is the only level that can work before
// anybody has shared a passphrase. Adding --v3-auth makes it
// authNoPriv and --v3-priv makes it authPriv — and the agent then
// REFUSES anything less for that user, because a message claiming less
// protection than the user is configured for is how a downgrade works.
const DefaultV3User = "dhs"
