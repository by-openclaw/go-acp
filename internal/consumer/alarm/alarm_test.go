package alarm

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/consumer"
)

// The template is the operator's contract: what an object's value
// means. These tests pin the contract on the shapes a plant really
// has — an SFP temperature with the module's own thresholds, a packet
// counter that must keep rising, a PTP lock enum, a multicast address
// that must stay inside the plant's range.

const fusionTemplate = `{
 "model": "FusioN6@0x68cd783f",
 "protocol": "mnset",
 "rows": [
  {"match": "port.*.sfp_ddm_info.temperature.current", "kind": "number",
   "high": [{"severity":"minor","raise":75,"clear":72},{"severity":"major","raise":80,"clear":77},{"severity":"critical","raise":85,"clear":82}],
   "low":  [{"severity":"minor","raise":-15,"clear":-12},{"severity":"critical","raise":-20,"clear":-17}],
   "hold": "10s", "text": "SFP temperature", "source": "module DDM thresholds"},
  {"match": "**.network.pkt_cnt", "kind": "counter", "stalled_for": "10s", "severity": "major",
   "text": "stream stopped", "source": "site rule"},
  {"match": "refclk.status", "kind": "enum", "normal": "3",
   "values": {"2":"minor","0":"major"}, "hold": "30s", "text": "PTP lock", "source": "Riedel manual"},
  {"match": "flows.*.network.dst_ip_addr", "kind": "text", "normal": "~^239\\.13[12]\\.",
   "severity": "major", "text": "multicast outside the plant range", "source": "site rule"}
 ]}`

func mustLoad(t *testing.T, s string) *Template {
	t.Helper()
	tpl, err := Load([]byte(s))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return tpl
}

func num(path string, n float64) consumer.Event {
	return consumer.Event{Path: path, Value: consumer.Value{Kind: consumer.KindFloat, Float: n}}
}

func str(path, s string) consumer.Event {
	return consumer.Event{Path: path, Value: consumer.Value{Kind: consumer.KindString, Str: s}}
}

func TestSeverityLadder(t *testing.T) {
	for _, c := range []struct {
		s      Severity
		name   string
		syslog int
		alarm  bool
	}{
		{Info, "info", 6, false},
		{Normal, "normal", 5, false},
		{Minor, "minor", 4, true},
		{Major, "major", 3, true},
		{Critical, "critical", 2, true},
		{Error, "error", 3, true},
	} {
		if c.s.String() != c.name || c.s.Syslog() != c.syslog || c.s.IsAlarm() != c.alarm {
			t.Errorf("%d: %q/%d/%v, want %q/%d/%v", c.s, c.s.String(), c.s.Syslog(), c.s.IsAlarm(), c.name, c.syslog, c.alarm)
		}
		if c.s > Normal {
			got, err := ParseSeverity(c.name)
			if c.s == Error {
				if err == nil {
					t.Error("error is produced, never authored")
				}
				continue
			}
			if err != nil || got != c.s {
				t.Errorf("ParseSeverity(%q) = %v, %v", c.name, got, err)
			}
		}
	}
	if s := Severity(9); s.String() != "severity(9)" {
		t.Errorf("out-of-range name = %q", s.String())
	}
	if _, err := ParseSeverity("info"); err == nil {
		t.Error("info is not authorable either")
	}
	if got, err := ParseSeverity("normal"); err != nil || got != Normal {
		t.Errorf("normal = %v, %v", got, err)
	}
}

func TestTemplateRoundTripsAndPicksTheFirstMatchingRow(t *testing.T) {
	tpl := mustLoad(t, fusionTemplate)
	if tpl.Model != "FusioN6@0x68cd783f" || tpl.Protocol != "mnset" || len(tpl.Rows) != 4 {
		t.Fatalf("template = %+v", tpl)
	}
	// export → import → export is a no-op (idempotent operator file).
	var buf, buf2 bytes.Buffer
	if err := tpl.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	again, err := Load(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err := again.Encode(&buf2); err != nil {
		t.Fatal(err)
	}
	if buf.String() != buf2.String() {
		t.Errorf("round trip is not byte-stable:\n%s\n---\n%s", buf.String(), buf2.String())
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Error("the file must end with a newline")
	}
	// row selection
	if r := tpl.RowFor("port.3.sfp_ddm_info.temperature.current"); r == nil || r.Kind != KindNumber {
		t.Errorf("temperature row = %+v", r)
	}
	if r := tpl.RowFor("flows.abc.network.pkt_cnt"); r == nil || r.Kind != KindCounter {
		t.Errorf("counter row = %+v", r)
	}
	if r := tpl.RowFor("self.system.fan_speed"); r != nil {
		t.Errorf("an object no row covers must have no row: %+v", r)
	}
}

func TestMatchPath(t *testing.T) {
	for _, c := range []struct {
		pat, path string
		want      bool
	}{
		{"a.b.c", "a.b.c", true},
		{"a.b.c", "a.b", false},
		{"a.*.c", "a.x.c", true},
		{"a.*.c", "a.x.y.c", false},
		{"**.pkt_cnt", "flows.x.network.pkt_cnt", true},
		{"**.pkt_cnt", "pkt_cnt", true},
		{"**.network.pkt_cnt", "pkt_cnt", false},
		{"telemetry.node.**", "telemetry.node.health.core_temp", true},
		{"telemetry.node.**", "telemetry.node", true},
		{"telemetry.node.**", "telemetry", false},
		{"**", "anything.at.all", true},
	} {
		if got := MatchPath(c.pat, strings.Split(c.path, ".")); got != c.want {
			t.Errorf("MatchPath(%q, %q) = %v", c.pat, c.path, got)
		}
	}
}

func TestTemplateValidationRefusesAuthoringMistakes(t *testing.T) {
	cases := map[string]string{
		"has no rows":                       `{"model":"m","rows":[]}`,
		"row 0 has no match":                `{"model":"m","rows":[{"kind":"counter"}]}`,
		"both match":                        `{"model":"m","rows":[{"match":"a","kind":"counter"},{"match":"a","kind":"counter"}]}`,
		"unknown kind":                      `{"model":"m","rows":[{"match":"a","kind":"weather"}]}`,
		"number rule with no band":          `{"model":"m","rows":[{"match":"a","kind":"number"}]}`,
		"neither values nor a normal value": `{"model":"m","rows":[{"match":"a","kind":"enum"}]}`,
		"text rule with no expected value":  `{"model":"m","rows":[{"match":"a","kind":"text"}]}`,
		"high band 0":                       `{"model":"m","rows":[{"match":"a","high":[{"severity":"oops","raise":1}]}]}`,
		"low band 0":                        `{"model":"m","rows":[{"match":"a","low":[{"severity":"nope","raise":1}]}]}`,
		"clear 3 is above raise 1":          `{"model":"m","rows":[{"match":"a","high":[{"severity":"minor","raise":1,"clear":3}]}]}`,
		"clear 0 is below raise 1":          `{"model":"m","rows":[{"match":"a","low":[{"severity":"minor","raise":1,"clear":0}]}]}`,
		"value \"x\"":                       `{"model":"m","rows":[{"match":"a","kind":"enum","values":{"x":"oops"}}]}`,
		"row 0 (a): \"oops\"":               `{"model":"m","rows":[{"match":"a","kind":"counter","severity":"oops"}]}`,
		"error parsing regexp":              `{"model":"m","rows":[{"match":"a","kind":"text","normal":"~("}]}`,
		"decode template":                   `{"model":"m","rows":[{"match":"a","nope":1}]}`,
		"bad duration":                      `{"model":"m","rows":[{"match":"a","kind":"counter","hold":"soon"}]}`,
		"hold must be a string":             `{"model":"m","rows":[{"match":"a","kind":"counter","hold":true}]}`,
	}
	for want, body := range cases {
		_, err := Load([]byte(body))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Load(%s): err = %v, want it to name %q", body, err, want)
		}
	}
	// A hold given as nanoseconds is accepted (the monitor profile does the same).
	tpl, err := Load([]byte(`{"model":"m","rows":[{"match":"a","kind":"counter","hold":1500000000}]}`))
	if err != nil || tpl.Rows[0].Hold != Duration(1500*time.Millisecond) {
		t.Errorf("numeric hold = %v, %v", tpl, err)
	}
	if _, err := Load([]byte(`{`)); err == nil {
		t.Error("broken JSON must fail")
	}
	// A clear point on the right side is accepted on both sides.
	if _, err := Load([]byte(`{"model":"m","rows":[{"match":"a","high":[{"severity":"minor","raise":10,"clear":8}],"low":[{"severity":"minor","raise":1,"clear":3}]}]}`)); err != nil {
		t.Errorf("valid hysteresis refused: %v", err)
	}
}

func TestNumberBandsWithHysteresisAndHold(t *testing.T) {
	clk := clock.NewFake(time.Unix(1700000000, 0))
	e := New(mustLoad(t, fusionTemplate), clk)
	const p = "port.3.sfp_ddm_info.temperature.current"

	// Starting normal says nothing.
	if tr := e.Eval("fusion-53", num(p, 36.5)); tr != nil {
		t.Fatalf("starting normal must be silent: %v", tr)
	}
	// Crossing 80 needs the 10s hold before it is adopted.
	if tr := e.Eval("fusion-53", num(p, 81)); tr != nil {
		t.Fatalf("hold not respected: %v", tr)
	}
	clk.Advance(9 * time.Second)
	if tr := e.Eval("fusion-53", num(p, 81)); tr != nil {
		t.Fatalf("hold not respected: %v", tr)
	}
	clk.Advance(2 * time.Second)
	tr := e.Eval("fusion-53", num(p, 81))
	if tr == nil || tr.Severity != Major || tr.Prior != Normal || tr.Band != "high major" || tr.Text != "SFP temperature" {
		t.Fatalf("raise = %+v", tr)
	}
	if !strings.Contains(tr.String(), "major raised") {
		t.Errorf("String() = %q", tr.String())
	}
	// 78 is below the major raise point but above its clear point: the
	// alarm holds, and nothing is reported.
	clk.Advance(time.Minute)
	if tr := e.Eval("fusion-53", num(p, 78)); tr != nil {
		t.Fatalf("hysteresis broken, eased too early: %v", tr)
	}
	if got := e.Severity("fusion-53", 0, p); got != Major {
		t.Errorf("severity = %v", got)
	}
	// 76 passes the clear point: it eases to minor (76 >= 75) after the hold.
	e.Eval("fusion-53", num(p, 76))
	clk.Advance(11 * time.Second)
	tr = e.Eval("fusion-53", num(p, 76))
	if tr == nil || tr.Severity != Minor || tr.Prior != Major || !strings.Contains(tr.String(), "eased") {
		t.Fatalf("ease = %+v", tr)
	}
	// Below the minor clear point it clears.
	e.Eval("fusion-53", num(p, 70))
	clk.Advance(11 * time.Second)
	tr = e.Eval("fusion-53", num(p, 70))
	if tr == nil || tr.Severity != Normal || !strings.Contains(tr.String(), "cleared") {
		t.Fatalf("clear = %+v", tr)
	}
	// The low side works the same way.
	e.Eval("fusion-53", num(p, -21))
	clk.Advance(11 * time.Second)
	tr = e.Eval("fusion-53", num(p, -21))
	if tr == nil || tr.Severity != Critical || tr.Band != "low critical" {
		t.Fatalf("low band = %+v", tr)
	}
	// A value that cannot be read is an error, not a guess.
	e.Eval("fusion-53", str(p, "n/a"))
	clk.Advance(11 * time.Second)
	tr = e.Eval("fusion-53", str(p, "n/a"))
	if tr == nil || tr.Severity != Error || tr.Band != "unreadable" {
		t.Fatalf("unreadable = %+v", tr)
	}
	active := e.Active()
	if len(active) != 1 || active[0].Severity != Error || active[0].Device != "fusion-53" || active[0].Path != p {
		t.Errorf("Active = %+v", active)
	}
}

func TestCounterStallAndWrap(t *testing.T) {
	clk := clock.NewFake(time.Unix(1700000000, 0))
	e := New(mustLoad(t, fusionTemplate), clk)
	const p = "flows.abc.network.pkt_cnt"

	e.Eval("fusion-53", num(p, 1000))
	clk.Advance(5 * time.Second)
	if tr := e.Eval("fusion-53", num(p, 2000)); tr != nil {
		t.Fatalf("a rising counter is normal: %v", tr)
	}
	// It stops. Under the stall window nothing is said…
	clk.Advance(5 * time.Second)
	if tr := e.Eval("fusion-53", num(p, 2000)); tr != nil {
		t.Fatalf("too early: %v", tr)
	}
	// …past it, the stream is reported stopped.
	clk.Advance(6 * time.Second)
	tr := e.Eval("fusion-53", num(p, 2000))
	if tr == nil || tr.Severity != Major || tr.Band != "stalled" || tr.Text != "stream stopped" {
		t.Fatalf("stall = %+v", tr)
	}
	// A 32-bit wrap is movement, not a stall: the counter went down.
	clk.Advance(5 * time.Second)
	tr = e.Eval("fusion-53", num(p, 12))
	if tr == nil || tr.Severity != Normal {
		t.Fatalf("wrap must clear the stall: %+v", tr)
	}
	// A counter that answers with something unreadable is an error.
	clk.Advance(5 * time.Second)
	tr = e.Eval("fusion-53", str(p, "?"))
	if tr == nil || tr.Severity != Error {
		t.Fatalf("unreadable counter = %+v", tr)
	}
}

func TestEnumAndTextDrift(t *testing.T) {
	clk := clock.NewFake(time.Unix(1700000000, 0))
	e := New(mustLoad(t, fusionTemplate), clk)

	// PTP: 3 is normal, 2 is minor, 0 is major, anything else unlisted
	// but not the normal value takes the row's verdict.
	if tr := e.Eval("fusion-53", str("refclk.status", "3")); tr != nil {
		t.Fatalf("locked is normal: %v", tr)
	}
	e.Eval("fusion-53", str("refclk.status", "0"))
	clk.Advance(31 * time.Second)
	tr := e.Eval("fusion-53", str("refclk.status", "0"))
	if tr == nil || tr.Severity != Major || tr.Band != "value" {
		t.Fatalf("free-run = %+v", tr)
	}
	// A different value with the SAME verdict is not a transition: the
	// value change is already reported by the watch, the alarm is not.
	if tr := e.Eval("fusion-53", str("refclk.status", "7")); tr != nil {
		t.Fatalf("same verdict must not re-raise: %+v", tr)
	}
	// Back to locked, then an unlisted non-normal value raises again.
	e.Eval("fusion-53", str("refclk.status", "3"))
	clk.Advance(31 * time.Second)
	e.Eval("fusion-53", str("refclk.status", "3"))
	e.Eval("fusion-53", str("refclk.status", "7"))
	clk.Advance(31 * time.Second)
	tr = e.Eval("fusion-53", str("refclk.status", "7"))
	if tr == nil || tr.Severity != Major {
		t.Fatalf("unlisted non-normal value = %+v", tr)
	}
	e.Eval("fusion-53", str("refclk.status", "2"))
	clk.Advance(31 * time.Second)
	tr = e.Eval("fusion-53", str("refclk.status", "2"))
	if tr == nil || tr.Severity != Minor {
		t.Fatalf("holdover = %+v", tr)
	}

	// Drift: the multicast must stay in the plant's RED/BLUE range.
	const m = "flows.abc.network.dst_ip_addr"
	if tr := e.Eval("fusion-53", str(m, "239.131.3.134")); tr != nil {
		t.Fatalf("in range is normal: %v", tr)
	}
	tr = e.Eval("fusion-53", str(m, "239.0.1.2"))
	if tr == nil || tr.Severity != Major || tr.Band != "drift" || tr.Prev != "239.131.3.134" {
		t.Fatalf("drift = %+v", tr)
	}
	// A literal expectation works the same way.
	lit := New(mustLoad(t, `{"model":"m","rows":[{"match":"self.syslog.config.server","kind":"text","normal":"10.6.250.101","severity":"minor","source":"site"}]}`), clk)
	if tr := lit.Eval("d", str("self.syslog.config.server", "10.6.250.101")); tr != nil {
		t.Fatalf("expected value is normal: %v", tr)
	}
	tr = lit.Eval("d", str("self.syslog.config.server", "10.0.0.9"))
	if tr == nil || tr.Severity != Minor {
		t.Fatalf("literal drift = %+v", tr)
	}
}

func TestFlapCapSilencesAndReports(t *testing.T) {
	clk := clock.NewFake(time.Unix(1700000000, 0))
	e := New(mustLoad(t, `{"model":"m","rows":[{"match":"x","kind":"text","normal":"ok","severity":"minor","flap_cap":2,"source":"site"}]}`), clk)
	flip := func(v string) *Transition {
		clk.Advance(time.Second)
		return e.Eval("d", str("x", v))
	}
	flip("ok")
	var flapping *Transition
	for i := 0; i < 4 && flapping == nil; i++ {
		if tr := flip("bad"); tr != nil && tr.Flapping {
			flapping = tr
		}
		if tr := flip("ok"); tr != nil && tr.Flapping {
			flapping = tr
		}
	}
	if flapping == nil {
		t.Fatal("crossing the flap cap must be reported once")
	}
	if !strings.Contains(flapping.String(), "flapping") {
		t.Errorf("String() = %q", flapping.String())
	}
	// Silenced while it keeps bouncing, but the verdict still tracks.
	quiet := 0
	for i := 0; i < 6; i++ {
		if tr := flip("bad"); tr == nil {
			quiet++
		}
		if tr := flip("ok"); tr == nil {
			quiet++
		}
	}
	if quiet == 0 {
		t.Error("a flapping object must be silenced until it settles")
	}
	if got := e.Severity("d", 0, "x"); got != Normal && got != Minor {
		t.Errorf("verdict must keep tracking while silenced, got %v", got)
	}
	// It settles: after the quiet window a real change speaks again.
	clk.Advance(5 * time.Minute)
	if tr := e.Eval("d", str("x", "bad")); tr == nil {
		t.Error("after settling, a change must be reported again")
	}
}

func TestEvalIgnoresUncoveredObjectsAndReadsEveryValueKind(t *testing.T) {
	clk := clock.NewFake(time.Unix(1700000000, 0))
	e := New(mustLoad(t, `{"model":"m","rows":[{"match":"n","high":[{"severity":"major","raise":10}]},{"match":"t","kind":"text","normal":"x","source":"s"}]}`), clk)
	if tr := e.Eval("d", num("not.covered", 99)); tr != nil {
		t.Errorf("an uncovered object is info: %v", tr)
	}
	if e.Severity("d", 0, "not.covered") != Info {
		t.Error("uncovered severity is info")
	}
	if e.Template().Model != "m" {
		t.Error("Template()")
	}
	// int, uint, bool, float, string-number and IP all read.
	for _, v := range []consumer.Value{
		{Kind: consumer.KindInt, Int: 11},
		{Kind: consumer.KindUint, Uint: 12},
		{Kind: consumer.KindFloat, Float: 13},
		{Kind: consumer.KindString, Str: "14"},
	} {
		if tr := e.Eval("d", consumer.Event{Path: "n", Value: v}); tr == nil && e.Severity("d", 0, "n") != Major {
			t.Errorf("value %+v was not read as a number", v)
		}
	}
	// A band with no clear point is left as soon as its raise point is not met.
	if tr := e.Eval("d", num("n", 9)); tr == nil || tr.Severity != Normal {
		t.Errorf("no-hysteresis clear = %+v", tr)
	}
	// bool and IP render for the text rule.
	e.Eval("d", consumer.Event{Path: "t", Value: consumer.Value{Kind: consumer.KindBool, Bool: true}})
	if got := e.Severity("d", 0, "t"); got != Major {
		t.Errorf("bool drift = %v", got)
	}
	e.Eval("d", consumer.Event{Path: "t", Value: consumer.Value{Kind: consumer.KindIPAddr, IPAddr: [4]byte{10, 6, 40, 53}}})
	if tr := e.Eval("d", consumer.Event{Path: "t", Value: consumer.Value{Kind: consumer.KindIPAddr, IPAddr: [4]byte{10, 6, 40, 53}}}); tr != nil {
		t.Errorf("same value twice must not repeat: %v", tr)
	}
	// bool read as a number by a numeric row.
	b := New(mustLoad(t, `{"model":"m","rows":[{"match":"b","high":[{"severity":"minor","raise":1}]}]}`), clk)
	if tr := b.Eval("d", consumer.Event{Path: "b", Value: consumer.Value{Kind: consumer.KindBool, Bool: true}}); tr == nil || tr.Severity != Minor {
		t.Errorf("bool as number = %+v", tr)
	}
	if tr := b.Eval("d", consumer.Event{Path: "b", Value: consumer.Value{Kind: consumer.KindBool, Bool: false}}); tr == nil || tr.Severity != Normal {
		t.Errorf("bool false = %+v", tr)
	}
}

func TestActiveIsOrderedWorstFirst(t *testing.T) {
	clk := clock.NewFake(time.Unix(1700000000, 0))
	e := New(mustLoad(t, `{"model":"m","rows":[
	  {"match":"a","kind":"text","normal":"ok","severity":"minor","source":"s"},
	  {"match":"b","kind":"text","normal":"ok","severity":"critical","source":"s"},
	  {"match":"c","kind":"text","normal":"ok","severity":"major","source":"s"}]}`), clk)
	for _, p := range []string{"a", "b", "c"} {
		e.Eval("dev-2", str(p, "bad"))
		e.Eval("dev-1", str(p, "bad"))
	}
	act := e.Active()
	if len(act) != 6 {
		t.Fatalf("Active = %d entries", len(act))
	}
	if act[0].Severity != Critical || act[0].Device != "dev-1" || act[1].Device != "dev-2" {
		t.Errorf("order = %+v", act[:2])
	}
	if act[len(act)-1].Severity != Minor {
		t.Errorf("worst must come first: %+v", act)
	}
	// A cleared object leaves the list.
	e.Eval("dev-1", str("b", "ok"))
	if got := len(e.Active()); got != 5 {
		t.Errorf("after a clear: %d", got)
	}
}
