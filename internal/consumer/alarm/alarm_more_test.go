package alarm

import (
	"strings"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/consumer"
)

// The corners: the defensive arms a hand-built template can reach
// (Load validates them away, code that builds a Template does not),
// the fallbacks a row relies on when it says little, and the side of
// the ladder the main test does not walk back down.

func TestTransitionStringCarriesUnitAndPlainBand(t *testing.T) {
	tr := Transition{Path: "port.3.sfp_ddm_info.temperature.current", Band: "high major",
		Value: "81", Prev: "36.5", Unit: "°C", Severity: Major, Prior: Normal}
	s := tr.String()
	if !strings.Contains(s, "81 °C") || !strings.Contains(s, "major raised") || strings.Contains(s, "—") {
		t.Errorf("String() = %q", s)
	}
}

func TestNewWithoutAClockUsesTheSystemOne(t *testing.T) {
	e := New(mustLoad(t, `{"model":"m","rows":[{"match":"x","kind":"text","normal":"ok","source":"s"}]}`), nil)
	e.Eval("d", str("x", "ok"))
	tr := e.Eval("d", str("x", "bad"))
	if tr == nil || tr.At.IsZero() {
		t.Fatalf("transition = %+v, want a real timestamp", tr)
	}
	if time.Since(tr.At) > time.Minute {
		t.Errorf("At = %v, want now", tr.At)
	}
}

func TestEnumValueMappedToNormalIsNormal(t *testing.T) {
	clk := clock.NewFake(time.Unix(1700000000, 0))
	e := New(mustLoad(t, `{"model":"m","rows":[{"match":"s","kind":"enum","values":{"3":"normal","0":"major"},"source":"s"}]}`), clk)
	e.Eval("d", str("s", "0"))
	if got := e.Severity("d", 0, "s"); got != Major {
		t.Fatalf("severity = %v", got)
	}
	tr := e.Eval("d", str("s", "3"))
	if tr == nil || tr.Severity != Normal || tr.Band != "normal" {
		t.Errorf("a value mapped to normal must clear: %+v", tr)
	}
}

func TestHandBuiltTemplateDefensiveArms(t *testing.T) {
	// Load() refuses these; a Template built in code can still carry
	// them, and the evaluator must not pretend it understood.
	clk := clock.NewFake(time.Unix(1700000000, 0))
	bogus := &Template{Model: "m", Rows: []Row{
		{Match: "e", Kind: KindEnum, Values: map[string]string{"1": "not-a-severity"}},
		{Match: "c", Kind: KindCounter, Severity: "not-a-severity"},
	}}
	e := New(bogus, clk)
	if tr := e.Eval("d", str("e", "1")); tr == nil || tr.Severity != Error || tr.Band != "unreadable" {
		t.Errorf("unreadable enum severity = %+v", tr)
	}
	// A row whose verdict cannot be parsed falls back to major.
	if got := bogus.Rows[1].verdict(); got != Major {
		t.Errorf("verdict fallback = %v", got)
	}
}

func TestBandLookupAndClearPoints(t *testing.T) {
	clear := 72.0
	row := &Row{Match: "x", High: []Band{{Severity: "minor", Raise: 75, Clear: &clear}}, Low: []Band{{Severity: "major", Raise: -5}}}
	if b, side := bandByName(row, "high minor"); b == nil || side != "high" || b.Raise != 75 {
		t.Errorf("high lookup = %+v %q", b, side)
	}
	if b, side := bandByName(row, "low major"); b == nil || side != "low" {
		t.Errorf("low lookup = %+v %q", b, side)
	}
	if b, _ := bandByName(row, "normal"); b != nil {
		t.Error("a band name with no side must not resolve")
	}
	if b, _ := bandByName(row, "high critical"); b != nil {
		t.Error("a band the row does not have must not resolve")
	}
	// A band with no clear point is left as soon as its raise point is not met.
	if cleared(row.Low[0], "low", -6) || !cleared(row.Low[0], "low", -4) {
		t.Error("low band without hysteresis")
	}
	if !cleared(row.High[0], "high", 71) || cleared(row.High[0], "high", 73) {
		t.Error("high band hysteresis")
	}
}

func TestLowSideHysteresisWalksBackUp(t *testing.T) {
	clk := clock.NewFake(time.Unix(1700000000, 0))
	e := New(mustLoad(t, fusionTemplate), clk)
	const p = "port.3.sfp_ddm_info.temperature.current"
	e.Eval("fusion-53", num(p, 20)) // normal
	e.Eval("fusion-53", num(p, -21))
	clk.Advance(11 * time.Second)
	if tr := e.Eval("fusion-53", num(p, -21)); tr == nil || tr.Severity != Critical {
		t.Fatalf("low critical = %+v", tr)
	}
	// -18 is above the critical raise (-20) but below its clear (-17):
	// the critical holds.
	clk.Advance(11 * time.Second)
	if tr := e.Eval("fusion-53", num(p, -18)); tr != nil {
		t.Fatalf("low hysteresis broken: %+v", tr)
	}
	// -16 passes the clear point and lands in the low minor band.
	e.Eval("fusion-53", num(p, -16))
	clk.Advance(11 * time.Second)
	if tr := e.Eval("fusion-53", num(p, -16)); tr == nil || tr.Severity != Minor || tr.Band != "low minor" {
		t.Fatalf("ease to low minor = %+v", tr)
	}
	// Back inside the normal range it clears.
	e.Eval("fusion-53", num(p, 20))
	clk.Advance(11 * time.Second)
	if tr := e.Eval("fusion-53", num(p, 20)); tr == nil || tr.Severity != Normal {
		t.Fatalf("clear = %+v", tr)
	}
}

func TestActiveOrdersSameSeverityByPath(t *testing.T) {
	clk := clock.NewFake(time.Unix(1700000000, 0))
	e := New(mustLoad(t, `{"model":"m","rows":[{"match":"**","kind":"text","normal":"ok","severity":"major","source":"s"}]}`), clk)
	for _, p := range []string{"z.path", "a.path", "m.path"} {
		e.Eval("dev", str(p, "bad"))
	}
	act := e.Active()
	if len(act) != 3 || act[0].Path != "a.path" || act[1].Path != "m.path" || act[2].Path != "z.path" {
		t.Errorf("Active order = %+v", act)
	}
}

func TestDurationUnmarshalRejectsNonJSON(t *testing.T) {
	var d Duration
	if err := d.UnmarshalJSON([]byte("{not json")); err == nil {
		t.Error("broken JSON must fail")
	}
}

func TestHoldAndStallFallbacks(t *testing.T) {
	// ClearHold applies on the way back to normal, Hold on the way up.
	r := Row{Hold: Duration(10 * time.Second), ClearHold: Duration(30 * time.Second)}
	if r.hold(false) != 10*time.Second || r.hold(true) != 30*time.Second {
		t.Errorf("hold = %v / %v", r.hold(false), r.hold(true))
	}
	// Without ClearHold both directions use Hold.
	r2 := Row{Hold: Duration(5 * time.Second)}
	if r2.hold(true) != 5*time.Second {
		t.Errorf("clear hold fallback = %v", r2.hold(true))
	}
	// A counter with no stalled_for uses the row's hold, then 30s.
	rc := Row{StalledFor: Duration(2 * time.Second), Hold: Duration(9 * time.Second)}
	if got := rc.stalledFor(); got != 2*time.Second {
		t.Errorf("stalled_for = %v", got)
	}
	rh := Row{Hold: Duration(9 * time.Second)}
	if got := rh.stalledFor(); got != 9*time.Second {
		t.Errorf("stall falls back to hold: %v", got)
	}
	rd := Row{}
	if got := rd.stalledFor(); got != 30*time.Second {
		t.Errorf("stall default = %v", got)
	}
}

func TestClearHoldIsUsedComingBack(t *testing.T) {
	clk := clock.NewFake(time.Unix(1700000000, 0))
	e := New(mustLoad(t, `{"model":"m","rows":[{"match":"x","high":[{"severity":"major","raise":10}],"hold":"1s","clear_hold":"20s","source":"s"}]}`), clk)
	e.Eval("d", num("x", 1))
	e.Eval("d", num("x", 11))
	clk.Advance(2 * time.Second)
	if tr := e.Eval("d", num("x", 11)); tr == nil || tr.Severity != Major {
		t.Fatalf("raise after hold = %+v", tr)
	}
	// Coming back needs the longer clear hold.
	e.Eval("d", num("x", 1))
	clk.Advance(5 * time.Second)
	if tr := e.Eval("d", num("x", 1)); tr != nil {
		t.Fatalf("cleared before clear_hold: %+v", tr)
	}
	clk.Advance(16 * time.Second)
	if tr := e.Eval("d", num("x", 1)); tr == nil || tr.Severity != Normal {
		t.Fatalf("clear after clear_hold = %+v", tr)
	}
}

func TestUncoveredObjectAndUnreadableCounterKeepTheirState(t *testing.T) {
	clk := clock.NewFake(time.Unix(1700000000, 0))
	e := New(mustLoad(t, fusionTemplate), clk)
	// An event with no path matches nothing.
	if tr := e.Eval("d", consumer.Event{Value: consumer.Value{Kind: consumer.KindInt, Int: 1}}); tr != nil {
		t.Errorf("pathless event = %+v", tr)
	}
}
