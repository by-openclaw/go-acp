package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"dhs/internal/rrcs/codec"
	rrcs "dhs/internal/rrcs/consumer"
)

// rrcsTestModel is the tree of the stand-in system, keys included.
func rrcsTestModel(t *testing.T) *rrcsModel {
	t.Helper()
	f := newRRCSFake(t, rrcsTreeAnswer)
	client, err := rrcs.NewClient(rrcs.Config{Addr: f.addr()})
	if err != nil {
		t.Fatal(err)
	}
	snap, _, err := rrcsCollect(context.Background(), client, rrcsCollectOpts{commands: true})
	if err != nil {
		t.Fatal(err)
	}
	m, err := rrcsModelOf(snap)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func rrcsEvent(method string, params ...codec.Value) rrcs.Event {
	all := append([]codec.Value{codec.String("R0000000001")}, params...)
	return rrcs.Event{Time: time.Date(2026, 10, 6, 15, 37, 28, 836e6, time.UTC), Method: method, TransKey: "R0000000001", Params: all}
}

func TestRRCSDecode(t *testing.T) {
	m := rrcsTestModel(t)
	st, mem, i, b := codec.Struct, rrcsMember, codec.Int, codec.Bool
	xp := st(mem("XP#1", codec.Array(i(1), i(61), i(7), i(1), i(61), i(1026), b(true))),
		mem("XP#2", codec.Array(i(1), i(61), i(1026), i(1), i(61), i(7), b(false))))
	addr := func(n, p int32, in bool) codec.Value {
		return st(mem("IsInput", b(in)), mem("Net", i(1)), mem("Node", i(n)), mem("Port", i(p)))
	}
	keyEvent := st(mem("KeyType", i(0)), mem("KeyAction", i(0)), mem("Node", i(61)), mem("Port", i(1026)),
		mem("SubPanel", i(0)), mem("Key", i(5)), mem("IsKeyLatched", b(true)))
	spyState := codec.Array(st(mem("Node", i(61)), mem("Port", i(1026)),
		mem("Key", st(mem("State", i(2)))),
		mem("Rotate", st(mem("State", i(3)), mem("ErrorCode", i(24)), mem("ErrorDescription", codec.String("Port is not online"))))))

	tests := []struct {
		name  string
		event rrcs.Event
		want  []string
	}{
		{"port active", rrcsEvent("PortActive", i(1), i(61), i(1026)),
			[]string{"15:37:28.836  oid=100 net.1.node.61.port.1026 Online = true  (PNL2)"}},
		{"port inactive, unknown port", rrcsEvent("PortInactive", i(1), i(70), i(3)),
			[]string{"15:37:28.836  net.1.node.70.port.3 Online = false"}},
		{"crosspoints", rrcsEvent("CrosspointChange", i(2), xp), []string{
			"15:37:28.836  oid=100 xp.net.1.node.61.port.7.in>net.1.node.61.port.1026 State = on  (I.-7 > PNL2)",
			"15:37:28.836  oid=101 xp.net.1.node.61.port.1026>net.1.node.61.port.7.out State = off  (PNL2 > O.-7)"}},
		{"volume", rrcsEvent("XpVolumeChange", codec.Array(
			st(mem("Source", addr(61, 7, true)), mem("Destination", addr(61, 1026, false)), mem("SingleVolume", i(218))),
			st(mem("Source", addr(61, 7, true)), mem("Destination", addr(61, 1026, false)), mem("ConferenceVolume", i(0))),
			st(mem("Source", addr(61, 7, true)), mem("Destination", addr(61, 1026, false)), mem("SingleVolume", codec.String("-1"))))), []string{
			"15:37:28.836  oid=100 xp.net.1.node.61.port.7.in>net.1.node.61.port.1026 SingleVolume = -6.0 dB [-114.5..12.5]  (I.-7 > PNL2)",
			"15:37:28.836  oid=100 xp.net.1.node.61.port.7.in>net.1.node.61.port.1026 ConferenceVolume = mute [-114.5..12.5]  (I.-7 > PNL2)",
			"15:37:28.836  oid=100 xp.net.1.node.61.port.7.in>net.1.node.61.port.1026 SingleVolume = unavailable [-114.5..12.5]  (I.-7 > PNL2)"}},
		{"logic source", rrcsEvent("LogicSourceChange", i(500), b(true)),
			[]string{"15:37:28.836  oid=500 logic.500 State = on  (Studio On-Air)"}},
		{"gp input", rrcsEvent("GpInputChange", i(1), i(61), i(1026), i(2), i(3), b(true)),
			[]string{"15:37:28.836  oid=100 net.1.node.61.port.1026.gpi.3 State = on  (PNL2)  slot 2"}},
		{"gp output", rrcsEvent("GpOutputChange", i(1), i(61), i(1026), i(2), i(4), b(false)),
			[]string{"15:37:28.836  oid=100 net.1.node.61.port.1026.gpo.4 State = off  (PNL2)  slot 2"}},
		{"configuration", rrcsEvent("ConfigurationChange"), []string{"15:37:28.836  gateway Configuration = changed"}},
		{"artist restored", rrcsEvent("ConnectArtistRestored", codec.String("Working")),
			[]string{"15:37:28.836  gateway ArtistConnection = connected  gateway state Working"}},
		{"gateway shutdown", rrcsEvent("GatewayShutdown", codec.String("Standby")),
			[]string{"15:37:28.836  gateway ArtistConnection = shutdown  gateway state Standby"}},
		{"upstream failed", rrcsEvent("UpstreamFailed", i(1), i(2)), []string{"15:37:28.836  net.1.node.2 UpstreamFailed = true"}},
		{"upstream cleared", rrcsEvent("UpstreamFailedCleared", i(1), i(2)), []string{"15:37:28.836  net.1.node.2 UpstreamFailed = false"}},
		{"node controller reboot", rrcsEvent("NodeControllerReboot", i(1), i(2)), []string{"15:37:28.836  net.1.node.2 NodeControllerReboot = true"}},
		{"client failed", rrcsEvent("ClientFailedCleared", i(1), i(60), i(4)), []string{"15:37:28.836  net.1.node.60.card.4 ClientFailed = false"}},
		{"key pressed", rrcsEvent("PanelSpyKeyEvent", keyEvent), []string{
			"15:37:28.836  oid=100 net.1.node.61.port.1026.keyevent.0.5 KeyAction = pressed  (PNL2)  key type 0 latched true; page 1: call-to-group group.200 (GROUP ALPHA)"}},
		{"function key", rrcsEvent("PanelSpyFuncKeyEvent", st(mem("FuncKeyAction", i(0)), mem("Node", i(61)), mem("Port", i(1026)),
			mem("FuncKeyNo", i(1)), mem("FuncKeyStates", st(mem("Shift", b(false)))))), []string{
			"15:37:28.836  oid=100 net.1.node.61.port.1026.funckey FuncKeyAction = 0  (PNL2)",
			"15:37:28.836  oid=100 net.1.node.61.port.1026.funckey FuncKeyNo = 1  (PNL2)"}},
		{"spy state", rrcsEvent("PanelSpyStateChange", spyState), []string{
			"15:37:28.836  oid=100 net.1.node.61.port.1026.spy Key = active  (PNL2)",
			"15:37:28.836  oid=100 net.1.node.61.port.1026.spy Rotate = error  (PNL2)  Port is not online"}},
		{"alive", rrcs.Event{Time: rrcsEvent("x").Time, Method: "GetAlive"}, []string{"15:37:28.836  gateway Alive = ping"}},
		{"unknown method", rrcsEvent("SendString", codec.String("hello")), []string{`15:37:28.836  event SendString = ["hello"]`}},
		{"crosspoints in another shape", rrcsEvent("CrosspointChange", i(1)), []string{"15:37:28.836  event CrosspointChange = [1]"}},
		{"key event without its struct", rrcsEvent("PanelSpyKeyEvent"), []string{"15:37:28.836  event PanelSpyKeyEvent = []"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lines := rrcsDecode(m, tc.event)
			got := make([]string, 0, len(lines))
			for _, l := range lines {
				got = append(got, l.text())
			}
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
	// Without a model the numbers are still printed.
	if l := rrcsDecode(nil, rrcsEvent("PortActive", i(1), i(61), i(1026))); l[0].text() != "15:37:28.836  net.1.node.61.port.1026 Online = true" {
		t.Errorf("no model: %s", l[0].text())
	}
}

func TestRRCSMetaFor(t *testing.T) {
	if m := rrcsMetaFor("PortAes67Output", "PayloadType"); m.Min != "96" || m.Max != "127" {
		t.Errorf("payload type: %+v", m)
	}
	if m := rrcsMetaFor("Media_2", "ServiceCodePoint"); m.Max != "63" {
		t.Errorf("dscp: %+v", m)
	}
	if m := rrcsMetaFor("Ptp", "PTP"); m.Min != "0" || m.Max != "127" {
		t.Errorf("ptp domain: %+v", m)
	}
	if m := rrcsMetaFor("", "LongName"); m != (rrcsMeta{}) {
		t.Errorf("a member without a range has one: %+v", m)
	}
}

// watch prints values by default, with the names read at start.
func TestRRCSWatchValues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		if call.Method == "RegisterForAllEvents" {
			url := "http://127.0.0.1:" + strconv.Itoa(int(call.Params[1].Int)) + call.Params[2].Str
			go func() {
				doc, _ := codec.EncodeCall("PortActive", codec.String("R0000000001"), codec.Int(1), codec.Int(61), codec.Int(1026))
				if resp, err := http.Post(url, "text/xml", bytes.NewReader(doc)); err == nil {
					_ = resp.Body.Close()
				}
				cancel()
			}()
			return call.Params[0], true
		}
		if call.Method == "UnregisterForAllEvents" {
			return call.Params[0], true
		}
		return rrcsTreeAnswer(call)
	})
	out, err := rrcsStdout(t, func() error {
		return runRRCS(ctx, []string{"watch", f.addr(), "--listen", "127.0.0.1:0", "--check", "0"})
	})
	if err != nil || !strings.Contains(out, "oid=100 net.1.node.61.port.1026 Online = true  (PNL2)") {
		t.Errorf("%v\n%s", err, out)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	g := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		if call.Method == "RegisterForAllEvents" {
			url := "http://127.0.0.1:" + strconv.Itoa(int(call.Params[1].Int)) + call.Params[2].Str
			go func() {
				doc, _ := codec.EncodeCall("LogicSourceChange", codec.String("R0000000001"), codec.Int(500), codec.Bool(true))
				if resp, err := http.Post(url, "text/xml", bytes.NewReader(doc)); err == nil {
					_ = resp.Body.Close()
				}
				cancel2()
			}()
		}
		// Nothing can be listed: the events still come out, with numbers.
		if strings.HasPrefix(call.Method, "GetAll") || call.Method == "GetVersion" || call.Method == "GetState" ||
			call.Method == "IsConnectedToArtist" || call.Method == "GetConfigurationID" {
			return codec.Value{}, false
		}
		return call.Params[0], true
	})
	js, err := rrcsStdout(t, func() error {
		return runRRCS(ctx2, []string{"watch", g.addr(), "--listen", "127.0.0.1:0", "--check", "0", "--output", "json"})
	})
	var line rrcsChangeLine
	if err != nil || json.Unmarshal([]byte(strings.TrimSpace(js)), &line) != nil ||
		line.Path != "logic.500" || line.Value != "on" || line.OID != 500 || line.Event != "LogicSourceChange" {
		t.Errorf("%v\n%s", err, js)
	}
	if err := runRRCS(context.Background(), []string{"watch", "h", "--events", "pretty"}); err == nil {
		t.Error("bad --events accepted")
	}
}

// The export carries the object ID in oid and the ranges the
// specification prints.
func TestRRCSExportMeta(t *testing.T) {
	f := newRRCSFake(t, rrcsTreeAnswer)
	file := filepath.Join(t.TempDir(), "x.csv")
	rrcsRun(t, "export", f.addr(), "--out", file)
	header, rows := rrcsReadExport(t, file)
	col := map[string]int{}
	for i, h := range header {
		col[h] = i
	}
	pt := rows["net.1.node.61.port.7.out.PortAes67Output.PayloadType"]
	if pt == nil || pt[col["oid"]] != "101" || pt[col["id"]] != "101" || pt[col["min"]] != "96" || pt[col["max"]] != "127" {
		t.Errorf("payload type row %v", pt)
	}
	ptime := rows["net.1.node.61.port.7.out.PortAes67Output.PacketTime"]
	if ptime == nil || ptime[col["unit"]] != "us" || ptime[col["enum_items"]] != "125|250|333|1000|1333" {
		t.Errorf("packet time row %v", ptime)
	}
}
