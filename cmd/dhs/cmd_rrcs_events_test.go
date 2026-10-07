package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/consumer/alarm"
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
		{"spy state as a real RRCS sends it", rrcsEvent("PanelSpyStateChanged", codec.Array(st(mem("Node", i(61)), mem("Port", i(2)),
			mem("Key", st(mem("State", i(3)), mem("ErrorCode", i(99)), mem("ErrorDescription", codec.String("No client card acknowledge received (time-out=5000 msec)."))))))), []string{
			"15:37:28.836  oid=100 net.1.node.61.port.1026.spy Key = error  (PNL2)  No client card acknowledge received (time-out=5000 msec)."}},
		{"send string", rrcsEvent("SendString", codec.String("CUE 12")), []string{"15:37:28.836  gateway SendString = CUE 12"}},
		{"send string off", rrcsEvent("SendStringOff", codec.String("CUE 12")), []string{"15:37:28.836  gateway SendString = CUE 12  off"}},
		{"sic failed", rrcsEvent("SicFailed", st(mem("Bay", i(4)), mem("Description", codec.String("SIC failed in bay 4")), mem("Net", i(1)),
			mem("Node", i(60)), mem("Path", codec.String("Node/060/Bay/04")), mem("Severity", i(3)), mem("Status", b(true)), mem("Type", codec.String("SIC failed")))),
			[]string{"15:37:28.836  net.1.node.60.card.4 SicFailed = true  severity 3 SIC failed in bay 4"}},
		{"alive", rrcs.Event{Time: rrcsEvent("x").Time, Method: "GetAlive"}, []string{"15:37:28.836  gateway Alive = ping"}},
		{"unknown method", rrcsEvent("SomethingNew", codec.String("hello")), []string{`15:37:28.836  event SomethingNew = ["hello"]`}},
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
		return runRRCS(ctx, []string{"watch", f.addr(), "--spy", "none", "--volume", "no", "--listen", "127.0.0.1:0", "--check", "0"})
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
		return runRRCS(ctx2, []string{"watch", g.addr(), "--spy", "none", "--volume", "no", "--listen", "127.0.0.1:0", "--check", "0", "--output", "json"})
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

// The uniform logging contract: the events of watch reach the local file
// in the chosen format and the remote syslog server as RFC 5424, and the
// operational lines go with them.
func TestRRCSWatchLogsToSinks(t *testing.T) {
	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = udp.Close() }()
	datagrams := make(chan string, 16)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, _, err := udp.ReadFrom(buf)
			if err != nil {
				return
			}
			datagrams <- string(buf[:n])
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		if call.Method == "RegisterForAllEvents" {
			url := "http://127.0.0.1:" + strconv.Itoa(int(call.Params[1].Int)) + call.Params[2].Str
			go func() {
				doc, _ := codec.EncodeCall("PortInactive", codec.String("R0000000001"), codec.Int(1), codec.Int(61), codec.Int(1026))
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
	logFile := filepath.Join(t.TempDir(), "watch.log")
	if _, err := rrcsStdout(t, func() error {
		return runRRCS(ctx, []string{"watch", f.addr(), "--spy", "none", "--volume", "no", "--listen", "127.0.0.1:0", "--check", "0",
			"--log", logFile, "--log-format", "json", "--syslog-addr", udp.LocalAddr().String()})
	}); err != nil {
		t.Fatalf("watch: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(logFile), "watch*.log"))
	var logged string
	for _, name := range files {
		raw, _ := os.ReadFile(name)
		logged += string(raw)
	}
	for _, want := range []string{`"msg":"value_change"`, `"proto":"rrcs"`, `"event":"PortInactive"`,
		`"path":"net.1.node.61.port.1026"`, `"label":"Online"`, `"value":"false"`, `"oid":100`,
		`rrcs watch: registered at`, `rrcs watch: unregistered in`} {
		if !strings.Contains(logged, want) {
			t.Errorf("log file lacks %s:\n%s", want, logged)
		}
	}
	got := ""
	deadline := time.After(2 * time.Second)
collect:
	for !strings.Contains(got, "value_change") {
		select {
		case d := <-datagrams:
			got += d + "\n"
		case <-deadline:
			break collect
		}
	}
	if !strings.Contains(got, "value_change") || !strings.Contains(got, "path=net.1.node.61.port.1026") || !strings.HasPrefix(got, "<") {
		t.Errorf("syslog datagrams:\n%s", got)
	}
	// --log off writes no file; an unusable syslog address is refused.
	off := filepath.Join(t.TempDir(), "none")
	rrcsRun(t, "info", f.addr(), "--log", "off")
	if _, err := os.Stat(off); err == nil {
		t.Error("a log file exists with --log off")
	}
	if err := runRRCS(context.Background(), []string{"info", f.addr(), "--syslog-addr", "not an address"}); err == nil {
		t.Error("unusable --syslog-addr accepted")
	}
}

// The committed template loads, and every row says where it comes from
// (ADR-0033: a row without a source is a guess).
func TestRRCSAlarmTemplate(t *testing.T) {
	tpl, err := alarm.LoadFile(filepath.Join("..", "..", "internal", "rrcs", "alarm", "RRCS@9.0.json"))
	if err != nil {
		t.Fatalf("template: %v", err)
	}
	for _, row := range tpl.Rows {
		if len(row.Source) < 40 {
			t.Errorf("row %s has no real source: %q", row.Match, row.Source)
		}
	}
	if last := tpl.Rows[len(tpl.Rows)-1]; last.Match != "**" || last.Kind != alarm.KindInfo {
		t.Errorf("the template does not end with the catch-all info row: %+v", last)
	}
}

// watch --alarm raises when the Artist connection fails and clears when
// it is back; a port going off line waits out its hold and raises nothing
// at once.
func TestRRCSWatchAlarm(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		if call.Method == "RegisterForAllEvents" {
			url := "http://127.0.0.1:" + strconv.Itoa(int(call.Params[1].Int)) + call.Params[2].Str
			go func() {
				for _, doc := range [][]byte{
					must(codec.EncodeCall("ConnectArtistFailure", codec.String("R0000000001"), codec.String("Working"))),
					must(codec.EncodeCall("PortInactive", codec.String("R0000000002"), codec.Int(1), codec.Int(61), codec.Int(1026))),
					must(codec.EncodeCall("ConnectArtistRestored", codec.String("R0000000003"), codec.String("Working"))),
				} {
					if resp, err := http.Post(url, "text/xml", bytes.NewReader(doc)); err == nil {
						_ = resp.Body.Close()
					}
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
	logFile := filepath.Join(t.TempDir(), "watch.log")
	out, err := rrcsStdout(t, func() error {
		return runRRCS(ctx, []string{"watch", f.addr(), "--spy", "none", "--volume", "no", "--listen", "127.0.0.1:0", "--check", "0", "--log", logFile, "--log-format", "json",
			"--alarm", filepath.Join("..", "..", "internal", "rrcs", "alarm", "RRCS@9.0.json")})
	})
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	rrcsWant(t, out,
		"ALARM critical raised gateway.ArtistConnection (value): failed",
		"ALARM normal cleared gateway.ArtistConnection",
		"RRCS connection to the Artist system")
	if strings.Contains(out, "ALARM minor") {
		t.Errorf("a port off line raised before its hold:\n%s", out)
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(logFile), "watch*.log"))
	var logged string
	for _, name := range files {
		raw, _ := os.ReadFile(name)
		logged += string(raw)
	}
	if !strings.Contains(logged, `"msg":"alarm"`) || !strings.Contains(logged, `"severity":"critical"`) {
		t.Errorf("the alarm did not reach the log:\n%s", logged)
	}
	var val *consumer.ValidationError
	if err := runRRCS(context.Background(), []string{"watch", f.addr(), "--spy", "none", "--volume", "no", "--listen", "127.0.0.1:0", "--alarm", filepath.Join(t.TempDir(), "none.json")}); !errors.As(err, &val) {
		t.Errorf("missing template: %v", err)
	}
}

func must(doc []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return doc
}

// watch --volume yes registers for crosspoint levels, follows a
// crosspoint when it is made, and prints the level RRCS then sends — on
// whatever path the older registration uses.
func TestRRCSWatchVolume(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var seen []codec.Call
	base := ""
	post := func(path string, doc []byte) {
		if resp, err := http.Post(base+path, "text/xml", bytes.NewReader(doc)); err == nil {
			_ = resp.Body.Close()
		}
	}
	st, mem, i, b := codec.Struct, rrcsMember, codec.Int, codec.Bool
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		mu.Lock()
		seen = append(seen, call)
		mu.Unlock()
		k := call.Params[0]
		switch call.Method {
		case "RegisterForAllEvents":
			base = "http://127.0.0.1:" + strconv.Itoa(int(call.Params[1].Int))
			return k, true
		case "RegisterForEventsEx":
			// The crosspoint is made once the level registration exists.
			go post("/RPC2", must(codec.EncodeCall("CrosspointChange", codec.String("R0000000001"), i(1),
				st(mem("XP#1", codec.Array(i(1), i(61), i(7), i(1), i(61), i(1026), b(true)))))))
			return codec.Array(k, i(0)), true
		case "XpVolumeChangeRegistryAdd":
			addr := func(n, p int32, in bool) codec.Value {
				return st(mem("IsInput", b(in)), mem("Net", i(1)), mem("Node", i(n)), mem("Port", i(p)))
			}
			go func() {
				// No path: the older registration names address and port only.
				post("/", must(codec.EncodeCall("XpVolumeChange", codec.String("R0000000002"), codec.Array(
					st(mem("Source", addr(61, 7, true)), mem("Destination", addr(61, 1026, false)), mem("SingleVolume", i(218)))))))
				cancel()
			}()
			return codec.Array(k, i(0)), true
		case "UnregisterForAllEvents", "UnregisterForEventsEx":
			return codec.Array(k, i(0)), true
		}
		return rrcsTreeAnswer(call)
	})
	out, err := rrcsStdout(t, func() error {
		return runRRCS(ctx, []string{"watch", f.addr(), "--spy", "none", "--volume", "no", "--listen", "127.0.0.1:0", "--check", "0", "--volume", "yes"})
	})
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	rrcsWant(t, out,
		"xp.net.1.node.61.port.7.in>net.1.node.61.port.1026 State = on",
		"xp.net.1.node.61.port.7.in>net.1.node.61.port.1026 SingleVolume = -6.0 dB [-114.5..12.5]")
	mu.Lock()
	defer mu.Unlock()
	var ex, add, unex *codec.Call
	adds := 0
	for n := range seen {
		switch seen[n].Method {
		case "RegisterForEventsEx":
			ex = &seen[n]
		case "XpVolumeChangeRegistryAdd":
			adds++
			if add == nil { // the crosspoint as made; the second is its reverse
				add = &seen[n]
			}
		case "UnregisterForEventsEx":
			unex = &seen[n]
		}
	}
	// §8.15: TransKey, IP-address, TCP-port, {XpVolumeChange: true}.
	if ex == nil || len(ex.Params) != 4 || ex.Params[1].Str != "127.0.0.1" || len(ex.Params[3].Members) != 1 || ex.Params[3].Members[0].Name != "XpVolumeChange" {
		t.Fatalf("RegisterForEventsEx: %+v", ex)
	}
	// §8.15: TransKey, IP-address, TCP-port, [{Destination, Source}].
	if add == nil || adds != 2 || len(add.Params) != 4 || len(add.Params[3].Items) != 1 {
		t.Fatalf("XpVolumeChangeRegistryAdd: %+v", add)
	}
	xp := add.Params[3].Items[0]
	dst, _ := xp.Field("Destination")
	src, _ := xp.Field("Source")
	if rrcsMemberInt(dst, "Port") != 1026 || rrcsFieldBool(dst, "IsInput") || rrcsMemberInt(src, "Port") != 7 || !rrcsFieldBool(src, "IsInput") {
		t.Errorf("crosspoint sent: %+v", xp)
	}
	// §8.15: TransKey, IP-address — RRCS 9.0 refuses a third parameter.
	if unex == nil || len(unex.Params) != 2 || unex.Params[1].Str != "127.0.0.1" {
		t.Errorf("UnregisterForEventsEx: %+v", unex)
	}
	if err := runRRCS(context.Background(), []string{"watch", "h", "--volume", "maybe"}); err == nil {
		t.Error("bad --volume accepted")
	}
}

// TestRRCSWatchCSV: a value of watch in the columns of export.
func TestRRCSWatchCSV(t *testing.T) {
	h := rrcsWatchCSVHeader()
	if h[0] != "ts" || h[1] != "event" || h[len(h)-1] != "description" || len(h) != len(rrcsCSVHeader)+3 {
		t.Fatalf("header: %v", h)
	}
	col := func(rec []string, name string) string {
		for i, c := range h {
			if c == name {
				return rec[i]
			}
		}
		t.Fatalf("no column %s", name)
		return ""
	}
	level := rrcsChangeLine{Time: "t", Event: "XpVolumeChange", OID: 7, Path: "xp.a>b", Label: "SingleVolume", Value: "-14.5",
		Unit: "dB", Min: "-114.5", Max: "12.5", Enum: "mute|unavailable", Name: "A > B"}.record("h")
	if len(level) != len(h) {
		t.Fatalf("record has %d fields, header %d", len(level), len(h))
	}
	for name, want := range map[string]string{"ip": "h", "oid": "7", "path": "xp.a>b.SingleVolume", "kind": "float", "value": "-14.5",
		"unit": "dB", "min": "-114.5", "max": "12.5", "enum_items": "mute|unavailable", "description": "A > B", "event": "XpVolumeChange"} {
		if got := col(level, name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	state := rrcsChangeLine{Path: "xp.a>b", Label: "State", Value: "on"}.record("h")
	if col(state, "kind") != "enum" || col(state, "enum_items") != "off|on" {
		t.Errorf("state: %v", state)
	}
	if online := (rrcsChangeLine{Path: "p", Label: "Online", Value: "true"}).record("h"); col(online, "kind") != "bool" {
		t.Errorf("online: %v", online)
	}
}

// xp --level yes reads the level through the volume registration and
// removes what it registered, even where the state read is refused.
func TestRRCSXpLevel(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	base := ""
	st, mem, i, b := codec.Struct, rrcsMember, codec.Int, codec.Bool
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		mu.Lock()
		methods = append(methods, call.Method)
		mu.Unlock()
		k := call.Params[0]
		switch call.Method {
		case "GetXpStatus":
			return codec.Array(k, i(3)), true // Node address invalid, as GetXpVolume on an Artist-1024
		case "RegisterForAllEvents":
			base = "http://127.0.0.1:" + strconv.Itoa(int(call.Params[1].Int))
			return k, true
		case "XpVolumeChangeRegistryAdd":
			addr := func(n, p int32, in bool) codec.Value {
				return st(mem("IsInput", b(in)), mem("Node", i(n)), mem("Port", i(p)))
			}
			go func() {
				doc := must(codec.EncodeCall("XpVolumeChange", codec.String("R0000000000"), codec.Array(
					st(mem("ConferenceVolume", i(0)), mem("Destination", addr(61, 1024, false)), mem("SingleVolume", i(190)), mem("Source", addr(61, 1026, true))))))
				if resp, err := http.Post(base+"/", "text/xml", bytes.NewReader(doc)); err == nil {
					_ = resp.Body.Close()
				}
			}()
			return codec.Array(k, i(0)), true
		case "RegisterForEventsEx", "UnregisterForAllEvents", "UnregisterForEventsEx":
			return codec.Array(k, i(0)), true
		}
		return codec.Value{}, false
	})
	out := rrcsRun(t, "xp", f.addr(), "--src", "net.1.node.61.port.1026", "--dst", "net.1.node.61.port.1024", "--level", "yes", "--listen", "127.0.0.1:0")
	rrcsWant(t, out,
		"xp.net.1.node.61.port.1026>net.1.node.61.port.1024 SingleVolume = -20.0 dB [-114.5..12.5]  (raw 190)",
		"ConferenceVolume = mute")
	mu.Lock()
	defer mu.Unlock()
	if got := strings.Join(methods, ","); got != "GetXpStatus,RegisterForAllEvents,RegisterForEventsEx,XpVolumeChangeRegistryAdd,UnregisterForEventsEx,UnregisterForAllEvents" {
		t.Errorf("methods: %s", got)
	}
}
