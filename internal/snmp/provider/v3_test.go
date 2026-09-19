package provider

import (
	"net"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/plugin"
	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/usm"
)

// v3User is the credential both ends of the loopback share.
func v3User() usm.User {
	return usm.User{
		Name:     "operator",
		Auth:     usm.HMACSHA256,
		AuthPass: "authpassword12345",
		Priv:     usm.AES128CFB,
		PrivPass: "privpassword12345",
	}
}

// v3Agent builds a server with a scalar and a USM engine, plus the
// manager-side engine that models it after discovery.
func v3Agent(t *testing.T) (*Server, *usm.Engine, codec.OID, *net.UDPAddr) {
	t.Helper()
	fk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	name := codec.MustParseOID("1.3.6.1.2.1.1.5.0")
	m := NewMIB()
	if err := m.Register(Scalar(name, codec.String("dhs-agent"))); err != nil {
		t.Fatal(err)
	}
	s := NewServer(m, Communities{}, plugin.Deps{Logger: quiet(), Clock: fk})

	id, err := usm.NewEngineID(usm.Enterprise, "agent")
	if err != nil {
		t.Fatal(err)
	}
	agentEng, err := usm.NewEngine(id, 1, fk)
	if err != nil {
		t.Fatal(err)
	}
	if err := agentEng.AddUser(v3User()); err != nil {
		t.Fatal(err)
	}
	s.SetEngine(agentEng)

	// The manager models the agent's engine (as discovery would yield).
	mgrEng, err := usm.NewRemoteEngine(id, agentEng.Boots(), agentEng.Time(), fk)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgrEng.AddUser(v3User()); err != nil {
		t.Fatal(err)
	}

	from := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40000}
	return s, mgrEng, name, from
}

func TestAgentV3Discovery(t *testing.T) {
	s, _, _, from := v3Agent(t)

	params, _ := codec.EncodeUSMParameters(codec.USMParameters{}) // empty = probe
	probe := codec.Message{
		Version: codec.Version3,
		V3: &codec.V3{
			ID: 2, MaxSize: codec.MaxMessageSize, Flags: codec.FlagReportable,
			SecurityModel: codec.SecurityModelUSM, SecurityParameters: params,
		},
		PDU: &codec.PDU{Type: codec.PDUTypeGet, RequestID: 2},
	}
	raw, err := codec.Encode(probe)
	if err != nil {
		t.Fatal(err)
	}
	reply, ok := s.handle(raw, from)
	if !ok {
		t.Fatal("agent did not answer the discovery probe")
	}
	m, err := codec.Decode(reply)
	if err != nil {
		t.Fatal(err)
	}
	if m.Type() != codec.PDUTypeReport {
		t.Errorf("discovery reply type = %s, want Report", m.Type())
	}
	up, _, err := codec.DecodeUSMParameters(m.V3.SecurityParameters)
	if err != nil {
		t.Fatal(err)
	}
	wantID, _ := usm.NewEngineID(usm.Enterprise, "agent")
	if string(up.AuthoritativeEngineID) != string(wantID) {
		t.Errorf("discovery engine ID = %x, want %x", up.AuthoritativeEngineID, wantID)
	}
}

func TestAgentV3AuthenticatedGet(t *testing.T) {
	s, mgr, name, from := v3Agent(t)

	req := codec.Message{
		Version: codec.Version3,
		V3:      &codec.V3{ID: 5, MaxSize: codec.MaxMessageSize, Flags: codec.FlagReportable},
		PDU: &codec.PDU{
			Type: codec.PDUTypeGet, RequestID: 5,
			VarBinds: []codec.VarBind{{Name: name, Value: codec.Null()}},
		},
	}
	raw, err := mgr.Seal(req, "operator")
	if err != nil {
		t.Fatal(err)
	}
	reply, ok := s.handle(raw, from)
	if !ok {
		t.Fatal("agent did not answer the authenticated get")
	}
	m, _, err := mgr.Open(reply)
	if err != nil {
		t.Fatalf("manager could not open the sealed reply: %v", err)
	}
	if m.PDU == nil || len(m.PDU.VarBinds) != 1 {
		t.Fatalf("reply PDU = %+v, want one varbind", m.PDU)
	}
	if got := string(m.PDU.VarBinds[0].Value.Bytes); got != "dhs-agent" {
		t.Errorf("value = %q, want dhs-agent", got)
	}
}

func TestAgentV3RefusedWhenNoEngine(t *testing.T) {
	m := NewMIB()
	_ = m.Register(Scalar(codec.MustParseOID("1.3.6.1.2.1.1.5.0"), codec.String("x")))
	s := NewServer(m, Communities{}, plugin.Deps{Logger: quiet()})
	// no SetEngine → v3 is refused
	from := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40000}
	params, _ := codec.EncodeUSMParameters(codec.USMParameters{})
	probe := codec.Message{
		Version: codec.Version3,
		V3:      &codec.V3{ID: 1, Flags: codec.FlagReportable, SecurityModel: codec.SecurityModelUSM, SecurityParameters: params},
		PDU:     &codec.PDU{Type: codec.PDUTypeGet, RequestID: 1},
	}
	raw, _ := codec.Encode(probe)
	if _, ok := s.handle(raw, from); ok {
		t.Error("an agent with no engine must not answer v3")
	}
}

func TestAgentV3OpenFailureSendsReport(t *testing.T) {
	s, _, name, from := v3Agent(t)

	// A manager with the right engine ID but the WRONG password: the
	// agent cannot open the message and answers with a Report so the
	// manager can rediscover, rather than time out.
	id, _ := usm.NewEngineID(usm.Enterprise, "agent")
	bad, err := usm.NewRemoteEngine(id, 1, 0, clock.NewFake(time.Time{}))
	if err != nil {
		t.Fatal(err)
	}
	wrong := v3User()
	wrong.AuthPass = "totally-different-pass"
	wrong.PrivPass = "totally-different-priv"
	if err := bad.AddUser(wrong); err != nil {
		t.Fatal(err)
	}

	req := codec.Message{
		Version: codec.Version3,
		V3:      &codec.V3{ID: 9, MaxSize: codec.MaxMessageSize, Flags: codec.FlagReportable},
		PDU: &codec.PDU{Type: codec.PDUTypeGet, RequestID: 9,
			VarBinds: []codec.VarBind{{Name: name, Value: codec.Null()}}},
	}
	raw, err := bad.Seal(req, "operator")
	if err != nil {
		t.Fatal(err)
	}
	reply, ok := s.handle(raw, from)
	if !ok {
		t.Fatal("agent should answer an unopenable message with a report")
	}
	m, err := codec.Decode(reply)
	if err != nil {
		t.Fatal(err)
	}
	if m.Type() != codec.PDUTypeReport {
		t.Errorf("reply type = %s, want Report", m.Type())
	}
}

func TestV3Helpers(t *testing.T) {
	// isDiscovery on a non-v3 message.
	if isDiscovery(codec.Message{Version: codec.Version2c}) {
		t.Error("a non-v3 message is not a discovery probe")
	}
	// isDiscovery on undecodable security parameters.
	if isDiscovery(codec.Message{Version: codec.Version3, V3: &codec.V3{SecurityParameters: []byte{0xff, 0x01}}}) {
		t.Error("garbage security parameters are not a discovery probe")
	}
	// v3ID with no envelope.
	if got := v3ID(codec.Message{}); got != 0 {
		t.Errorf("v3ID with no V3 = %d, want 0", got)
	}
	// reportRequestID with no PDU (as a priv request decodes).
	if got := reportRequestID(codec.Message{}); got != 0 {
		t.Errorf("reportRequestID with no PDU = %d, want 0", got)
	}
}

func TestAgentV3IgnoresNonAnswerablePDU(t *testing.T) {
	s, mgr, name, from := v3Agent(t)

	// An authenticated message whose PDU is a Response is not something
	// an agent answers; it opens but respondPDU declines it.
	req := codec.Message{
		Version: codec.Version3,
		V3:      &codec.V3{ID: 6, MaxSize: codec.MaxMessageSize, Flags: codec.FlagReportable},
		PDU: &codec.PDU{Type: codec.PDUTypeResponse, RequestID: 6,
			VarBinds: []codec.VarBind{{Name: name, Value: codec.Null()}}},
	}
	raw, err := mgr.Seal(req, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.handle(raw, from); ok {
		t.Error("agent must not answer a v3 Response-PDU")
	}
}
