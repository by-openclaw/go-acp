package provider

import (
	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/usm"
)

// usmUnknownEngineIDs.0 is the counter a discovery Report names (RFC 3414
// §5, usmStatsUnknownEngineIDs). Its value is immaterial to discovery; the
// engine identity travels in the message's security parameters.
var usmUnknownEngineIDs = codec.MustParseOID("1.3.6.1.6.3.15.1.1.4.0")

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
		return s.report(req)
	}

	opened, user, err := s.engine.Open(raw)
	if err != nil {
		// Unknown engine ID or a clock outside the RFC 3414 §3.2 window:
		// answer with a Report so the manager can (re)discover, rather
		// than drop the manager into a silent timeout.
		s.logger.Debug("snmp agent: v3 open failed, sending report", "err", err.Error())
		return s.report(req)
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
// boots and time — the answer a manager discovers the agent from.
func (s *Server) report(req codec.Message) ([]byte, bool) {
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
			VarBinds:  []codec.VarBind{{Name: usmUnknownEngineIDs, Value: codec.Counter32(1)}},
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
