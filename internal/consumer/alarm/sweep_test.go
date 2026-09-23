package alarm

import (
	"strings"
	"testing"
	"time"

	"dhs/internal/clock"
)

// A device that pushes says a thing once. These tests pin what a
// sweep — time passing, no sample — is allowed to conclude from that.

const pushTemplate = `{
 "model": "acp2-card",
 "protocol": "acp2",
 "rows": [
  {"match": "card.*.input.status", "kind": "text", "normal": "ok", "severity": "major",
   "hold": "30s", "clear_hold": "5s", "text": "input", "source": "test"},
  {"match": "card.*.rx.pkt_cnt", "kind": "counter", "stalled_for": "10s", "severity": "critical",
   "text": "stream stopped", "source": "test"}
 ]}`

func TestSweepAdoptsAHoldNoSecondSampleWillEver(t *testing.T) {
	clk := clock.NewFake(time.Unix(1700000000, 0))
	e := New(mustLoad(t, pushTemplate), clk)

	// The announce arrives once. Its hold is not elapsed, so nothing
	// is raised yet — and on a push protocol nothing more will come.
	if tr := e.Eval("neuron", str("card.1.input.status", "loss")); tr != nil {
		t.Fatalf("raised before the hold: %v", tr)
	}
	clk.Advance(29 * time.Second)
	if trs := e.Sweep(); len(trs) != 0 {
		t.Fatalf("raised one second early: %v", trs)
	}

	clk.Advance(2 * time.Second)
	trs := e.Sweep()
	if len(trs) != 1 || trs[0].Severity != Major || trs[0].Band != "drift" {
		t.Fatalf("sweep = %v", trs)
	}
	if trs[0].Device != "neuron" || trs[0].Path != "card.1.input.status" || trs[0].Value != "loss" {
		t.Errorf("a swept transition must name its object: %+v", trs[0])
	}
	// Once adopted it is not raised again on every tick.
	clk.Advance(time.Minute)
	if trs := e.Sweep(); len(trs) != 0 {
		t.Fatalf("re-raised an adopted verdict: %v", trs)
	}

	// The device announces the recovery; the clear waits out its own
	// hold, which the sweep also serves.
	if tr := e.Eval("neuron", str("card.1.input.status", "ok")); tr != nil {
		t.Fatalf("cleared before the clear hold: %v", tr)
	}
	clk.Advance(6 * time.Second)
	trs = e.Sweep()
	if len(trs) != 1 || trs[0].Severity != Normal || trs[0].Prior != Major {
		t.Fatalf("clear = %v", trs)
	}
}

func TestSweepRaisesTheCounterThatStopped(t *testing.T) {
	clk := clock.NewFake(time.Unix(1700000000, 0))
	e := New(mustLoad(t, pushTemplate), clk)

	// Two samples, then silence: on a push protocol a stopped stream
	// sends nothing at all, so the stall can only be found by time.
	e.Eval("neuron", num("card.1.rx.pkt_cnt", 1000))
	clk.Advance(time.Second)
	e.Eval("neuron", num("card.1.rx.pkt_cnt", 2000))

	clk.Advance(9 * time.Second)
	if trs := e.Sweep(); len(trs) != 0 {
		t.Fatalf("called the stall too early: %v", trs)
	}
	clk.Advance(2 * time.Second)
	trs := e.Sweep()
	if len(trs) != 1 || trs[0].Severity != Critical || trs[0].Band != "stalled" {
		t.Fatalf("stall = %v", trs)
	}

	// The stream comes back: the next sample clears it.
	clk.Advance(time.Second)
	tr := e.Eval("neuron", num("card.1.rx.pkt_cnt", 3000))
	if tr == nil || tr.Severity != Normal {
		t.Fatalf("recovery = %v", tr)
	}
}

func TestSweepIsQuietWhenNothingIsDue(t *testing.T) {
	clk := clock.NewFake(time.Unix(1700000000, 0))
	e := New(mustLoad(t, pushTemplate), clk)

	// Nothing seen at all.
	if trs := e.Sweep(); len(trs) != 0 {
		t.Fatalf("empty evaluator = %v", trs)
	}
	// A healthy object, swept for an hour: a text row has no way to
	// worsen without a sample.
	e.Eval("neuron", str("card.1.input.status", "ok"))
	clk.Advance(time.Hour)
	if trs := e.Sweep(); len(trs) != 0 {
		t.Fatalf("healthy object = %v", trs)
	}
	// A counter with only one sample is not a stall either until its
	// window has passed.
	e.Eval("neuron", num("card.2.rx.pkt_cnt", 7))
	if trs := e.Sweep(); len(trs) != 0 {
		t.Fatalf("fresh counter = %v", trs)
	}
}

func TestSweptTransitionKeepsWhatTheEventNamedTheObject(t *testing.T) {
	// The sweep has no event in hand, so the label and the unit it
	// prints are the ones an earlier sample brought. A later sample
	// that omits them does not erase them: a poller may resolve a
	// label once and leave it out of the repeats.
	clk := clock.NewFake(time.Unix(1700000000, 0))
	e := New(mustLoad(t, pushTemplate), clk)

	ev := num("card.1.rx.pkt_cnt", 10)
	ev.Label = "SDI input 1 packets"
	ev.Unit = "pkt"
	e.Eval("neuron", ev)
	clk.Advance(time.Second)
	e.Eval("neuron", num("card.1.rx.pkt_cnt", 20)) // no label, no unit

	clk.Advance(11 * time.Second)
	trs := e.Sweep()
	if len(trs) != 1 || trs[0].Label != "SDI input 1 packets" || trs[0].Unit != "pkt" {
		t.Fatalf("swept transition = %+v", trs)
	}
	if act := e.Active(); len(act) != 1 || act[0].Label != "SDI input 1 packets" {
		t.Errorf("active = %+v", act)
	}
}

func TestMatchPathGlobsInsideASegment(t *testing.T) {
	// A device that numbers its leaves ("Fan Health 1".."Fan Health 4",
	// "CONTROL PORT MAC 1") is covered by one row, and a leading "**"
	// absorbs the root the plugin puts in front of every path.
	for _, c := range []struct {
		pattern, path string
		want          bool
	}{
		{"**.PSU.*.Fan Health *", "ROOT_NODE_V2.PSU.1.Fan Health 3", true},
		{"**.PSU.*.Fan Health *", "PSU.2.Fan Health 1", true},
		{"**.PSU.*.Fan Health *", "ROOT_NODE_V2.PSU.1.Fan Speed", false},
		{"*.PSU.*.Status", "ROOT_NODE_V2.PSU.1.Status", true},
		{"PSU.*.Status", "ROOT_NODE_V2.PSU.1.Status", false}, // no "**": the root counts
		{"CONTROL PORT MAC *", "CONTROL PORT MAC 2", true},
		{"CONTROL PORT MAC *", "CONTROL PORT MAC", false}, // the space is literal
		{"CONTROL PORT MAC *", "CONTROL PORT", false},
		{"Fan*", "Fan", true}, // the run may be empty
		{"*Temperature*", "PSU Temperature Mode", true},
		{"a*b*c", "azzbzzc", true},
		{"a*b*c", "azzbzz", false},
		{"a*b*c", "ac", false},
		{"plain", "plain", true},
		{"plain", "other", false},
	} {
		if got := MatchPath(c.pattern, strings.Split(c.path, ".")); got != c.want {
			t.Errorf("MatchPath(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestCountsShowEverySeverityIncludingTheEmptyOnes(t *testing.T) {
	clk := clock.NewFake(time.Unix(1700000000, 0))
	e := New(mustLoad(t, pushTemplate), clk)

	c := e.Counts()
	if len(c) != 6 || c["critical"] != 0 || c["normal"] != 0 {
		t.Fatalf("empty evaluator = %v", c)
	}

	e.Eval("neuron", str("card.1.input.status", "ok"))
	e.Eval("neuron", str("card.2.input.status", "loss"))
	clk.Advance(31 * time.Second)
	e.Sweep()

	c = e.Counts()
	if c["normal"] != 1 || c["major"] != 1 || c["critical"] != 0 {
		t.Errorf("counts = %v", c)
	}
	// An object no row covers is not counted at all: it is info, and
	// the evaluator keeps no state for it.
	e.Eval("neuron", str("card.1.label", "anything"))
	if got := e.Counts(); got["info"] != 0 || got["normal"] != 1 {
		t.Errorf("uncovered object changed the counts: %v", got)
	}
}
