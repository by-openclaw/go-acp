package provider

// IS-12 over the socket: the vendor workers, the block accessors, the
// BCP-008 counters, and the refusals — a controller driving MS-05-02
// over NCP has to be told which of its assumptions was wrong.

import (
	"encoding/json"
	stdhttp "net/http"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is12"
	"dhs/internal/amwa/codec/ms05"
	httpsession "dhs/internal/amwa/session/http"
)

// ncpCommand sends one command and returns the single result it
// carries.
func ncpCommand(t *testing.T, ws *httpsession.WebSocket, oid int,
	method is12.MethodID, args any) is12.MethodResult {
	t.Helper()
	raw := json.RawMessage(nil)
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		raw = b
	}
	resp := ncpRoundTrip(t, ws, is12.CommandMessage{
		Commands: []is12.Command{{
			Handle: 1, OID: oid, MethodID: method, Arguments: raw,
		}},
	})
	rm, ok := resp.(is12.CommandResponseMessage)
	if !ok {
		t.Fatalf("response was %T, want a command response", resp)
	}
	if len(rm.Responses) != 1 {
		t.Fatalf("responses = %+v, want one", rm.Responses)
	}
	return rm.Responses[0].Result
}

// ncpOID finds the oid of the first object of a class.
func ncpOID(t *testing.T, s *IS14ConfigurationServer, id ms05.NcClassId) int {
	t.Helper()
	return int(objectOfClass(t, s, id).oid)
}

// A message type a Node never receives is refused with the type named:
// the other four IS-12 message types travel Node→Controller only, and
// a controller sending one has misread the protocol.
func TestNCPRefusesAMessageTowardsANode(t *testing.T) {
	addr := serveNCPNode(t)
	ws := ncpDial(t, addr)

	resp := ncpRoundTrip(t, ws, is12.NotificationMessage{
		Notifications: []is12.Notification{},
	})
	em, ok := resp.(is12.ErrorMessage)
	if !ok {
		t.Fatalf("response was %T, want an error", resp)
	}
	if em.Status != int(ms05.NcMethodStatusInvalidRequest) {
		t.Errorf("status = %d, want InvalidRequest", em.Status)
	}
}

// The block accessors: members of the root block, and the refusal for
// an oid that is not a block at all.
func TestNCPBlockAccessors(t *testing.T) {
	addr := serveNCPNode(t)
	ws := ncpDial(t, addr)

	members := ncpCommand(t, ws, 1, is12.MethodID{Level: 2, Index: 1}, map[string]any{})
	if members.Status != int(ms05.NcMethodStatusOk) {
		t.Errorf("GetMemberDescriptors = %+v", members)
	}

	notABlock := ncpCommand(t, ws, 2, is12.MethodID{Level: 2, Index: 1}, map[string]any{})
	if notABlock.Status == int(ms05.NcMethodStatusOk) {
		t.Errorf("an oid that is not a block answered %+v", notABlock)
	}

	byPath := ncpCommand(t, ws, 1, is12.MethodID{Level: 2, Index: 2},
		map[string]any{"path": []string{"DeviceManager"}})
	if byPath.Status != int(ms05.NcMethodStatusOk) {
		t.Errorf("FindMembersByPath = %+v", byPath)
	}

	// A path nobody published matches nothing — an empty list, not a
	// refusal: FindMembersByPath is a search, and finding none is an
	// answer.
	noSuchPath := ncpCommand(t, ws, 1, is12.MethodID{Level: 2, Index: 2},
		map[string]any{"path": []string{"NotAnObject"}})
	if noSuchPath.Status != int(ms05.NcMethodStatusOk) {
		t.Errorf("a search that found nothing = %+v", noSuchPath)
	}
	if string(noSuchPath.Value) != "[]" {
		t.Errorf("value = %s, want an empty list", noSuchPath.Value)
	}
}

// Get on a property the object does not carry names it, rather than
// answering with a null a controller would cache as the value.
func TestNCPGetRefusals(t *testing.T) {
	addr := serveNCPNode(t)
	ws := ncpDial(t, addr)

	res := ncpCommand(t, ws, 1, is12.MethodID{Level: 1, Index: 1},
		map[string]any{"id": map[string]any{"level": 9, "index": 9}})
	if res.Status != int(ms05.NcMethodStatusPropertyNotImplemented) {
		t.Errorf("= %+v, want PropertyNotImplemented", res)
	}

	bad := ncpCommand(t, ws, 1, is12.MethodID{Level: 1, Index: 1}, "not an object")
	if bad.Status != int(ms05.NcMethodStatusParameterError) {
		t.Errorf("arguments of the wrong shape = %+v", bad)
	}
}

// A method the class does not declare is MethodNotImplemented — the
// status that tells a controller to stop asking rather than to retry.
func TestNCPUnknownMethod(t *testing.T) {
	addr := serveNCPNode(t)
	ws := ncpDial(t, addr)

	res := ncpCommand(t, ws, 1, is12.MethodID{Level: 9, Index: 9}, map[string]any{})
	if res.Status != int(ms05.NcMethodStatusMethodNotImplemented) {
		t.Errorf("= %+v, want MethodNotImplemented", res)
	}
}

// The vendor gain worker answers over IS-12 as well as IS-14 REST, and
// through the same setProperty gate: one implementation, two faces.
func TestNCPGainControl(t *testing.T) {
	addr := serveNCPBundleNode(t, audioBundle())
	ws := ncpDial(t, addr)
	cfg := configFixture(t)
	oid := ncpOID(t, cfg, vendorClassID)

	if res := ncpCommand(t, ws, oid, is12.MethodID{Level: 4, Index: 1},
		map[string]any{}); res.Status == int(ms05.NcMethodStatusOk) {
		t.Errorf("SetGainDb with no argument answered %+v", res)
	}
	if res := ncpCommand(t, ws, oid, is12.MethodID{Level: 4, Index: 1},
		"not an object"); res.Status == int(ms05.NcMethodStatusOk) {
		t.Errorf("arguments of the wrong shape answered %+v", res)
	}
	if res := ncpCommand(t, ws, oid, is12.MethodID{Level: 4, Index: 1},
		map[string]any{"gainDb": 9999}); res.Status == int(ms05.NcMethodStatusOk) {
		t.Errorf("a value outside the constraint answered %+v", res)
	}
	if res := ncpCommand(t, ws, oid, is12.MethodID{Level: 4, Index: 1},
		map[string]any{"gainDb": -6}); res.Status != int(ms05.NcMethodStatusOk) {
		t.Errorf("a value in range = %+v", res)
	}
}

// The fault-injection worker is reachable over IS-12 too, sharing one
// implementation with the REST face so the two cannot drift.
func TestNCPFaultControl(t *testing.T) {
	addr := serveNCPBundleNode(t, audioBundle())
	ws := ncpDial(t, addr)
	cfg := configFixture(t)
	oid := ncpOID(t, cfg, faultClassID)

	res := ncpCommand(t, ws, oid, is12.MethodID{Level: 4, Index: 1}, map[string]any{})
	if res.Status == int(ms05.NcMethodStatusOk) {
		t.Errorf("a fault injection with no target answered %+v", res)
	}
}

// BCP-008 counters read back over the socket, per monitor and per
// kind, and a monitor nobody has counted for answers with an empty
// list rather than null.
func TestNCPMonitorCounters(t *testing.T) {
	addr := serveNCPBundleNode(t, audioBundle())
	ws := ncpDial(t, addr)

	for _, tc := range []struct {
		oid   int
		index int
	}{
		{5, 1}, // receiver: lost
		{5, 2}, // receiver: late
		{6, 1}, // sender: transmission errors
	} {
		res := ncpCommand(t, ws, tc.oid, is12.MethodID{Level: 4, Index: tc.index}, map[string]any{})
		if res.Status != int(ms05.NcMethodStatusOk) {
			t.Errorf("oid %d 4m%d = %+v", tc.oid, tc.index, res)
		}
	}
}

// The version path IS the socket, so a plain GET is not a request the
// NCP face can answer — it is reported and dropped rather than
// answered with something a controller would try to parse.
func TestNCPRefusesARequestThatIsNotAnUpgrade(t *testing.T) {
	addr := serveNCPNode(t)

	code, _ := get(t, addr, "/x-nmos/ncp/v1.0")
	if code == stdhttp.StatusSwitchingProtocols {
		t.Errorf("a plain GET = %d, want no protocol switch", code)
	}
}

// A socket that has gone away is reported, not retried into silence:
// the subscriber is gone and the notification with it, and the
// operator should be able to see that happened.
func TestNCPSendReportsWhatItCannotDeliver(t *testing.T) {
	tap := newLogTap()
	addr := serveNCPNode(t)
	cfg := configFixture(t)
	s := NewIS12NCPServer(tap.logger(), cfg)

	ws := ncpDial(t, addr)
	_ = ws.Close()
	s.send(&ncpConn{ws: ws}, is12.CommandResponseMessage{
		Responses: []is12.CommandResponseEntry{{Handle: 1, Result: is12.MethodResult{Status: 200}}},
	})
	if !tap.has("send") {
		t.Errorf("a send to a dead socket must be reported; saw %v", tap.snapshot())
	}

	// A message the codec will not encode is reported at the encoder
	// rather than put on the wire half-formed.
	s.send(&ncpConn{ws: ws}, is12.NotificationMessage{})
	if !tap.has("encode") {
		t.Errorf("an unencodable message must be reported; saw %v", tap.snapshot())
	}
}

// A property value the model cannot render is not notified: a
// notification carrying nothing tells a subscriber the property
// changed to null, which is a different fact from "it changed".
func TestNCPDoesNotNotifyAValueItCannotRender(t *testing.T) {
	cfg := configFixture(t)
	s := NewIS12NCPServer(newLogTap().logger(), cfg)

	refuseMarshal(t)
	s.notifyPropertyChanged(1, ms05.NcPropertyId{Level: 1, Index: 6}, "anything")
}

// SetGainDb names one property, so an object without it is
// PropertyNotImplemented rather than a silent success.
func TestNCPGainOnAnObjectWithoutGain(t *testing.T) {
	cfg := configFixture(t)
	s := NewIS12NCPServer(newLogTap().logger(), cfg)

	res := s.methodSetGainDb(cfg.objectByOid(1), json.RawMessage(`{"gainDb":-6}`))
	if res.Status != int(ms05.NcMethodStatusPropertyNotImplemented) {
		t.Fatalf("= %+v, want PropertyNotImplemented", res)
	}
}

// FindMembersByPath compares the whole path, not a prefix: a search
// for a two-segment path must not match a one-segment member.
func TestNCPFindMembersByPathComparesTheWholePath(t *testing.T) {
	addr := serveNCPNode(t)
	ws := ncpDial(t, addr)

	res := ncpCommand(t, ws, 1, is12.MethodID{Level: 2, Index: 2},
		map[string]any{"path": []string{"DeviceManager", "Deeper"}})
	if string(res.Value) != "[]" {
		t.Errorf("value = %s, want no match", res.Value)
	}
}

// With the Configuration API opted out there is no device model to
// control, so IS-12 is not mounted and no ws control href is
// advertised for a face that answers nothing.
func TestNCPIsNotMountedWithoutTheDeviceModel(t *testing.T) {
	s := nodeFor(t, validBundle(), func(c *IS04NodeConfig) { c.NoConfigurationAPI = true })
	srv := httpsession.NewServer(newLogTap().logger())

	s.attachNCPAPI(srv)

	if s.ncp != nil {
		t.Error("IS-12 must not be mounted without a device model")
	}
}

// A method id in the vendor range that the fault worker does not
// declare falls through to MethodNotImplemented, rather than matching
// the first fault method that happens to share a level.
func TestNCPFaultClassWithAnUndeclaredMethod(t *testing.T) {
	addr := serveNCPBundleNode(t, audioBundle())
	ws := ncpDial(t, addr)
	cfg := configFixture(t)
	oid := ncpOID(t, cfg, faultClassID)

	res := ncpCommand(t, ws, oid, is12.MethodID{Level: 4, Index: 9}, map[string]any{})
	if res.Status != int(ms05.NcMethodStatusMethodNotImplemented) {
		t.Fatalf("= %+v, want MethodNotImplemented", res)
	}
}

// Counters that have actually been recorded come back through the
// socket — the empty list is the interesting case only until a stream
// has run.
func TestNCPMonitorCountersCarryWhatWasCounted(t *testing.T) {
	addr := serveNCPBundleNode(t, audioBundle())
	ws := ncpDial(t, addr)

	// The fault worker is the seam that puts counters on a monitor.
	cfg := configFixture(t)
	fault := ncpOID(t, cfg, faultClassID)
	rx := cfg.objectByOid(5)
	for id, key := range cfg.monitorByResource {
		if key == strings.Join(rx.path, ".") {
			cfg.SetMonitorActive(id, true)
		}
	}
	_ = fault

	res := ncpCommand(t, ws, 8, is12.MethodID{Level: 4, Index: 4}, map[string]any{
		"monitorRole": rx.role,
		"counter":     "lost",
		"name":        "leg-0",
		"increment":   3,
	})
	if res.Status != int(ms05.NcMethodStatusOk) {
		t.Fatalf("adding a counter = %+v", res)
	}

	got := ncpCommand(t, ws, 5, is12.MethodID{Level: 4, Index: 1}, map[string]any{})
	if got.Status != int(ms05.NcMethodStatusOk) {
		t.Fatalf("reading the counters = %+v", got)
	}
	if string(got.Value) == "[]" {
		t.Errorf("value = %s, want the counter that was added", got.Value)
	}
}
