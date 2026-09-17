package provider

// v1 and v2c disagree about almost every error case, and what they
// disagree about is exactly what a manager sees. So these tests run the
// same request under both versions and assert the two different answers,
// rather than asserting one and assuming the other.

import (
	"errors"
	"io"
	"log/slog"
	"testing"

	"dhs/internal/snmp/codec"
)

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// agentTree is the fixture: a system group, a writable object, and a
// structural node with an instance under it — enough to tell
// noSuchObject from noSuchInstance.
func agentTree(t *testing.T) (*MIB, *int64) {
	t.Helper()
	var stored int64 = 1
	m := newTree(t,
		Scalar(oid("1.3.6.1.2.1.1.1.0"), codec.String("dhs SNMP agent")),
		Scalar(oid("1.3.6.1.2.1.1.2.0"), codec.ObjectID(oid("1.3.6.1.4.1.1773"))),
		Object{OID: oid("1.3.6.1.2.1.2.2"), Access: NotAccessible},
		Scalar(oid("1.3.6.1.2.1.2.2.1.1.1"), codec.Int(1)),
		Writable(oid("1.3.6.1.4.1.1773.9.0"), codec.TypeInteger,
			func() codec.Value { return codec.Int(stored) },
			func(v codec.Value) error {
				if v.Int < 0 {
					return errors.New("negative")
				}
				stored = v.Int
				return nil
			}),
	)
	return m, &stored
}

func newTestAgent(t *testing.T) (*Agent, *int64) {
	t.Helper()
	m, stored := agentTree(t)
	return NewAgent(m, Communities{Read: "public", Write: "private"}, quiet()), stored
}

func request(v codec.Version, community string, t codec.PDUType, names ...string) codec.Message {
	vbs := make([]codec.VarBind, 0, len(names))
	for _, n := range names {
		vbs = append(vbs, codec.VarBind{Name: oid(n), Value: codec.Null()})
	}
	return codec.Message{Version: v, Community: community,
		PDU: &codec.PDU{Type: t, RequestID: 7, VarBinds: vbs}}
}

// respond runs one request and insists the agent answered.
func respond(t *testing.T, a *Agent, req codec.Message) codec.PDU {
	t.Helper()
	resp, raw, ok := a.Respond(req)
	if !ok {
		t.Fatal("the agent sent nothing")
	}
	if resp.PDU == nil {
		t.Fatal("the response has no PDU")
	}
	if resp.PDU.Type != codec.PDUTypeResponse {
		t.Fatalf("answered with a %s", resp.PDU.Type)
	}
	if resp.PDU.RequestID != req.PDU.RequestID {
		t.Fatalf("request-id = %d, want %d — a manager correlates on it",
			resp.PDU.RequestID, req.PDU.RequestID)
	}
	if resp.Version != req.Version || resp.Community != req.Community {
		t.Fatalf("answered as %s/%q", resp.Version, resp.Community)
	}
	// The bytes and the message must say the same thing, or a caller
	// that inspects one and sends the other is inspecting a fiction.
	back, err := codec.Decode(raw)
	if err != nil {
		t.Fatalf("the agent's own datagram does not decode: %v", err)
	}
	if back.PDU.ErrorStatus != resp.PDU.ErrorStatus ||
		len(back.PDU.VarBinds) != len(resp.PDU.VarBinds) {
		t.Fatalf("the datagram (%s/%d bindings) is not the response (%s/%d)",
			back.PDU.ErrorStatus, len(back.PDU.VarBinds),
			resp.PDU.ErrorStatus, len(resp.PDU.VarBinds))
	}
	return *resp.PDU
}

// ---------------------------------------------------------------------
// who is answered at all
// ---------------------------------------------------------------------

// A request the agent will not serve gets NO datagram, so that a scanner
// learns nothing from the difference between a wrong community and a
// closed port.
func TestTheAgentIsSilentRatherThanInformative(t *testing.T) {
	a, _ := newTestAgent(t)

	for _, tc := range []struct {
		name string
		req  codec.Message
	}{
		{"a wrong read community",
			request(codec.Version2c, "wrong", codec.PDUTypeGet, "1.3.6.1.2.1.1.1.0")},
		{"the read community on a SET",
			request(codec.Version2c, "public", codec.PDUTypeSet, "1.3.6.1.4.1.1773.9.0")},
		{"a wrong write community",
			request(codec.Version2c, "wrong", codec.PDUTypeSet, "1.3.6.1.4.1.1773.9.0")},
		{"a Response arriving at an agent",
			request(codec.Version2c, "public", codec.PDUTypeResponse)},
		{"somebody else's notification",
			request(codec.Version2c, "public", codec.PDUTypeTrapV2)},
		{"a v1 trap", codec.Message{Version: codec.Version1, Community: "public",
			TrapV1: &codec.TrapV1{Enterprise: oid("1.3.6.1.4.1.1773")}}},
		{"an empty message", codec.Message{Version: codec.Version2c}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, ok := a.Respond(tc.req); ok {
				t.Error("the agent answered something it must not")
			}
		})
	}
}

// An empty read community means "public", because that is what every
// manager tries first.
func TestAnEmptyReadCommunityMeansPublic(t *testing.T) {
	m, _ := agentTree(t)
	a := NewAgent(m, Communities{}, quiet())

	if _, _, ok := a.Respond(request(codec.Version2c, DefaultReadCommunity,
		codec.PDUTypeGet, "1.3.6.1.2.1.1.1.0")); !ok {
		t.Error("public must be admitted when no read community is set")
	}
	// And an empty write community refuses every SET, including one
	// carrying the read community.
	if _, _, ok := a.Respond(request(codec.Version2c, DefaultReadCommunity,
		codec.PDUTypeSet, "1.3.6.1.4.1.1773.9.0")); ok {
		t.Error("an unset write community must refuse every SET")
	}
	if _, _, ok := a.Respond(request(codec.Version2c, "",
		codec.PDUTypeSet, "1.3.6.1.4.1.1773.9.0")); ok {
		t.Error("an empty community must not match an empty write community")
	}
}

// ---------------------------------------------------------------------
// GET
// ---------------------------------------------------------------------

func TestGetAnswersWithTheValue(t *testing.T) {
	a, _ := newTestAgent(t)
	out := respond(t, a, request(codec.Version2c, "public", codec.PDUTypeGet,
		"1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.2.0"))

	if out.ErrorStatus != codec.NoError {
		t.Fatalf("error = %s", out.ErrorStatus)
	}
	if len(out.VarBinds) != 2 {
		t.Fatalf("%d varbinds, want 2", len(out.VarBinds))
	}
	if out.VarBinds[0].Value.String() != "dhs SNMP agent" {
		t.Errorf("sysDescr.0 = %s", out.VarBinds[0].Value)
	}
	if out.VarBinds[1].Value.String() != "1.3.6.1.4.1.1773" {
		t.Errorf("sysObjectID.0 = %s", out.VarBinds[1].Value)
	}
}

// v2c reports a missing object IN the varbind and answers the rest of
// the request; v1 has no exception values, so the whole request fails
// with the 1-based index of the name that was wrong.
func TestAMissingObjectUnderBothVersions(t *testing.T) {
	a, _ := newTestAgent(t)

	t.Run("v2c says which kind of missing", func(t *testing.T) {
		out := respond(t, a, request(codec.Version2c, "public", codec.PDUTypeGet,
			"1.3.6.1.2.1.1.1.0",      // exists
			"1.3.6.1.2.1.2.2.1.1.99", // the object exists, this instance does not
			"1.3.6.1.4.1.9999.1.0",   // nothing under this branch at all
			"1.3.6.1.2.1.2.2"))       // a structural node: no value here
		if out.ErrorStatus != codec.NoError {
			t.Fatalf("v2c must not fail the request: %s", out.ErrorStatus)
		}
		want := []codec.ValueType{codec.TypeOctetString, codec.TypeNoSuchInst,
			codec.TypeNoSuchObject, codec.TypeNoSuchInst}
		for i, w := range want {
			if out.VarBinds[i].Value.Type != w {
				t.Errorf("varbind %d = %s, want %s", i, out.VarBinds[i].Value.Type, w)
			}
		}
	})

	t.Run("v1 fails the whole request", func(t *testing.T) {
		out := respond(t, a, request(codec.Version1, "public", codec.PDUTypeGet,
			"1.3.6.1.2.1.1.1.0", "1.3.6.1.4.1.9999.1.0"))
		if out.ErrorStatus != codec.NoSuchName {
			t.Fatalf("= %s, want noSuchName", out.ErrorStatus)
		}
		if out.ErrorIndex != 2 {
			t.Errorf("error-index = %d, want 2 (1-based)", out.ErrorIndex)
		}
		// The request's bindings come back so the manager can line the
		// index up against the names it sent.
		if len(out.VarBinds) != 2 {
			t.Errorf("%d varbinds, want the request echoed", len(out.VarBinds))
		}
	})
}

// ---------------------------------------------------------------------
// GETNEXT
// ---------------------------------------------------------------------

// A GETNEXT walk is how a manager discovers a device it has no MIB for,
// and it ends when the agent says so — differently in each version.
func TestAWalkAndItsEnd(t *testing.T) {
	a, _ := newTestAgent(t)

	out := respond(t, a, request(codec.Version2c, "public", codec.PDUTypeGetNext,
		"1.3.6.1.2.1.1.1.0"))
	if got := out.VarBinds[0].Name.String(); got != "1.3.6.1.2.1.1.2.0" {
		t.Errorf("next after sysDescr.0 = %s", got)
	}

	// Past the last object.
	last := "1.3.6.1.4.1.1773.9.0"
	out = respond(t, a, request(codec.Version2c, "public", codec.PDUTypeGetNext, last))
	if out.VarBinds[0].Value.Type != codec.TypeEndOfMIBView {
		t.Errorf("= %s, want endOfMibView", out.VarBinds[0].Value.Type)
	}
	if out.VarBinds[0].Name.String() != last {
		t.Errorf("endOfMibView must keep the name asked for, got %s", out.VarBinds[0].Name)
	}

	out = respond(t, a, request(codec.Version1, "public", codec.PDUTypeGetNext, last))
	if out.ErrorStatus != codec.NoSuchName || out.ErrorIndex != 1 {
		t.Errorf("v1 end of walk = %s/%d, want noSuchName/1", out.ErrorStatus, out.ErrorIndex)
	}
}

// ---------------------------------------------------------------------
// GETBULK
// ---------------------------------------------------------------------

// The repetitions come back in ROUNDS — all the first steps, then all
// the seconds — because that is the order RFC 3416 §4.2.3 lays down and
// what a manager rebuilding a table by column depends on.
func TestGetBulkReturnsRounds(t *testing.T) {
	a, _ := newTestAgent(t)
	req := codec.Message{Version: codec.Version2c, Community: "public",
		PDU: &codec.PDU{Type: codec.PDUTypeGetBulk, RequestID: 1,
			NonRepeaters: 1, MaxRepetitions: 2,
			VarBinds: []codec.VarBind{
				{Name: oid("1.3.6.1.2.1.1.1.0")}, // non-repeater
				{Name: oid("1.3.6.1.2.1.1.1.0")}, // repeater A
				{Name: oid("1.3.6.1.2.1.2.2")},   // repeater B
			}}}

	out := respond(t, a, req)
	want := []string{
		"1.3.6.1.2.1.1.2.0",     // the non-repeater's one step
		"1.3.6.1.2.1.1.2.0",     // round 1, A
		"1.3.6.1.2.1.2.2.1.1.1", // round 1, B
		"1.3.6.1.2.1.2.2.1.1.1", // round 2, A
		"1.3.6.1.4.1.1773.9.0",  // round 2, B
	}
	if len(out.VarBinds) != len(want) {
		t.Fatalf("%d varbinds, want %d: %v", len(out.VarBinds), len(want), names(out.VarBinds))
	}
	for i, w := range want {
		if got := out.VarBinds[i].Name.String(); got != w {
			t.Errorf("varbind %d = %s, want %s", i, got, w)
		}
	}
}

// A bulk that walks off the end fills the rest with endOfMibView rather
// than stopping short, which is what tells the manager to stop asking.
func TestGetBulkRunsOffTheEnd(t *testing.T) {
	a, _ := newTestAgent(t)
	req := codec.Message{Version: codec.Version2c, Community: "public",
		PDU: &codec.PDU{Type: codec.PDUTypeGetBulk, RequestID: 1, MaxRepetitions: 20,
			VarBinds: []codec.VarBind{{Name: oid("1.3.6.1.2.1.1.1.0")}}}}

	out := respond(t, a, req)
	if len(out.VarBinds) != 20 {
		t.Fatalf("%d varbinds, want max-repetitions of them", len(out.VarBinds))
	}
	if out.VarBinds[len(out.VarBinds)-1].Value.Type != codec.TypeEndOfMIBView {
		t.Errorf("the tail must be endOfMibView, got %s", out.VarBinds[len(out.VarBinds)-1].Value)
	}
	if out.ErrorStatus != codec.NoError {
		t.Errorf("a bulk past the end is not an error: %s", out.ErrorStatus)
	}
}

// A manager's off-by-one still gets an answer it can use: RFC 3416
// §4.2.3 says to treat a negative count as zero rather than to refuse.
func TestGetBulkCountsThatMakeNoSense(t *testing.T) {
	a, _ := newTestAgent(t)
	for _, tc := range []struct {
		name             string
		nonRep, maxReps  int
		wantVarBindCount int
	}{
		{"negative non-repeaters", -1, 1, 1},
		{"negative max-repetitions", 0, -5, 0},
		{"more non-repeaters than names", 9, 1, 1},
		{"nothing repeated at all", 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := codec.Message{Version: codec.Version2c, Community: "public",
				PDU: &codec.PDU{Type: codec.PDUTypeGetBulk, RequestID: 1,
					NonRepeaters: tc.nonRep, MaxRepetitions: tc.maxReps,
					VarBinds: []codec.VarBind{{Name: oid("1.3.6.1.2.1.1.1.0")}}}}
			out := respond(t, a, req)
			if len(out.VarBinds) != tc.wantVarBindCount {
				t.Errorf("%d varbinds, want %d: %v",
					len(out.VarBinds), tc.wantVarBindCount, names(out.VarBinds))
			}
		})
	}
}

func names(vbs []codec.VarBind) []string {
	out := make([]string, 0, len(vbs))
	for _, vb := range vbs {
		out = append(out, vb.Name.String())
	}
	return out
}

// ---------------------------------------------------------------------
// SET
// ---------------------------------------------------------------------

func TestSetAppliesAndEchoes(t *testing.T) {
	a, stored := newTestAgent(t)
	req := codec.Message{Version: codec.Version2c, Community: "private",
		PDU: &codec.PDU{Type: codec.PDUTypeSet, RequestID: 1,
			VarBinds: []codec.VarBind{
				{Name: oid("1.3.6.1.4.1.1773.9.0"), Value: codec.Int(42)}}}}

	out := respond(t, a, req)
	if out.ErrorStatus != codec.NoError {
		t.Fatalf("= %s", out.ErrorStatus)
	}
	if *stored != 42 {
		t.Errorf("the object holds %d, want 42", *stored)
	}
	// The echo is how a manager confirms what the device took.
	if out.VarBinds[0].Value.Int != 42 {
		t.Errorf("echo = %s, want the value written", out.VarBinds[0].Value)
	}
}

// Each refusal has a different status in each version, and a manager
// branches on the status.
func TestSetRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		oid    string
		value  codec.Value
		wantV1 codec.ErrorStatus
		wantV2 codec.ErrorStatus
	}{
		{"an object that is not there", "1.3.6.1.4.1.9999.1.0", codec.Int(1),
			codec.NoSuchName, codec.NoCreation},
		{"a structural node", "1.3.6.1.2.1.2.2", codec.Int(1),
			codec.NoSuchName, codec.NoCreation},
		{"an object that is read-only", "1.3.6.1.2.1.1.1.0", codec.String("x"),
			codec.ReadOnly, codec.NotWritable},
		{"a value of the wrong syntax", "1.3.6.1.4.1.1773.9.0", codec.String("x"),
			codec.BadValue, codec.WrongType},
		{"a value the object refuses", "1.3.6.1.4.1.1773.9.0", codec.Int(-1),
			codec.BadValue, codec.WrongValue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, v := range []struct {
				version codec.Version
				want    codec.ErrorStatus
			}{{codec.Version1, tc.wantV1}, {codec.Version2c, tc.wantV2}} {
				a, stored := newTestAgent(t)
				before := *stored
				req := codec.Message{Version: v.version, Community: "private",
					PDU: &codec.PDU{Type: codec.PDUTypeSet, RequestID: 1,
						VarBinds: []codec.VarBind{{Name: oid(tc.oid), Value: tc.value}}}}

				out := respond(t, a, req)
				if out.ErrorStatus != v.want {
					t.Errorf("%s = %s, want %s", v.version, out.ErrorStatus, v.want)
				}
				if out.ErrorIndex != 1 {
					t.Errorf("%s error-index = %d, want 1", v.version, out.ErrorIndex)
				}
				if *stored != before {
					t.Errorf("%s: a refused set changed the device", v.version)
				}
			}
		})
	}
}

// A set is checked in full before anything is applied, so a request with
// one bad name leaves the device exactly where it was.
func TestSetIsCheckedBeforeItIsApplied(t *testing.T) {
	a, stored := newTestAgent(t)
	req := codec.Message{Version: codec.Version2c, Community: "private",
		PDU: &codec.PDU{Type: codec.PDUTypeSet, RequestID: 1,
			VarBinds: []codec.VarBind{
				{Name: oid("1.3.6.1.4.1.1773.9.0"), Value: codec.Int(42)},
				{Name: oid("1.3.6.1.2.1.1.1.0"), Value: codec.String("x")}, // read-only
			}}}

	out := respond(t, a, req)
	if out.ErrorStatus != codec.NotWritable || out.ErrorIndex != 2 {
		t.Fatalf("= %s/%d", out.ErrorStatus, out.ErrorIndex)
	}
	if *stored != 1 {
		t.Errorf("the first write was applied anyway: %d", *stored)
	}
}

// What the two-phase check cannot prevent is an object that fails AFTER
// its predecessor succeeded. commitFailed is the status that says the
// device is not where it was, and reporting wrongValue there would tell
// the manager the opposite.
func TestASetThatFailsPartWayThroughIsCommitFailed(t *testing.T) {
	var a, b int64
	m := newTree(t,
		Writable(oid("1.3.6.1.4.1.1773.1.0"), codec.TypeInteger,
			func() codec.Value { return codec.Int(a) },
			func(v codec.Value) error { a = v.Int; return nil }),
		Writable(oid("1.3.6.1.4.1.1773.2.0"), codec.TypeInteger,
			func() codec.Value { return codec.Int(b) },
			func(codec.Value) error { return errors.New("the hardware said no") }),
	)
	ag := NewAgent(m, Communities{Read: "public", Write: "private"}, quiet())

	req := codec.Message{Version: codec.Version2c, Community: "private",
		PDU: &codec.PDU{Type: codec.PDUTypeSet, RequestID: 1,
			VarBinds: []codec.VarBind{
				{Name: oid("1.3.6.1.4.1.1773.1.0"), Value: codec.Int(5)},
				{Name: oid("1.3.6.1.4.1.1773.2.0"), Value: codec.Int(6)},
			}}}

	out := respond(t, ag, req)
	if out.ErrorStatus != codec.CommitFailed || out.ErrorIndex != 2 {
		t.Fatalf("= %s/%d, want commitFailed/2", out.ErrorStatus, out.ErrorIndex)
	}
	if a != 5 {
		t.Errorf("the first write did not land: %d", a)
	}

	// Under v1 there is no commitFailed to report, so badValue is the
	// whole of what the manager can be told.
	req.Version = codec.Version1
	if out := respond(t, ag, req); out.ErrorStatus != codec.BadValue {
		t.Errorf("v1 = %s, want badValue", out.ErrorStatus)
	}
}

// ---------------------------------------------------------------------
// tooBig
// ---------------------------------------------------------------------

// A response that will not fit is REPLACED by tooBig, not truncated: a
// manager cannot tell a short table from the end of one.
func TestAResponseThatWillNotFitIsTooBig(t *testing.T) {
	a, _ := newTestAgent(t)
	a.maxSize = 64

	req := codec.Message{Version: codec.Version2c, Community: "public",
		PDU: &codec.PDU{Type: codec.PDUTypeGetBulk, RequestID: 1, MaxRepetitions: 20,
			VarBinds: []codec.VarBind{{Name: oid("1.3.6.1.2.1.1.1.0")}}}}

	out := respond(t, a, req)
	if out.ErrorStatus != codec.TooBig {
		t.Fatalf("= %s, want tooBig", out.ErrorStatus)
	}
	if len(out.VarBinds) != 0 {
		t.Errorf("%d varbinds, want none", len(out.VarBinds))
	}
	if out.ErrorIndex != 0 {
		t.Errorf("error-index = %d, want 0 — tooBig is about the whole response",
			out.ErrorIndex)
	}
}
