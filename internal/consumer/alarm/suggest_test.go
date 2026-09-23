package alarm

import (
	"strings"
	"testing"

	"dhs/internal/consumer"
)

// A generated template is only worth having if it refuses to guess.
// These tests pin both halves: what a device's own words support, and
// what it takes to be left alone.

func obj(path []string, kind consumer.ValueKind) consumer.Object {
	return consumer.Object{Path: path, Kind: kind, Label: path[len(path)-1], Access: 1}
}

func TestSuggestReadsTheDevicesOwnWords(t *testing.T) {
	objs := []consumer.Object{
		func() consumer.Object {
			o := obj([]string{"ROOT", "PSU", "1", "Status"}, consumer.KindEnum)
			o.EnumItems = []string{"NA", "OK", "Warning", "Error"}
			return o
		}(),
		func() consumer.Object {
			o := obj([]string{"ROOT", "PSU", "2", "Status"}, consumer.KindEnum)
			o.EnumItems = []string{"NA", "OK", "Warning", "Error"}
			return o
		}(),
		func() consumer.Object {
			o := obj([]string{"ROOT", "PSU", "1", "Temperature"}, consumer.KindInt)
			o.Min, o.Max, o.Unit = -40, 140, "C"
			return o
		}(),
	}
	tpl, rep := Suggest(objs, "acp2", "SHPRM1@6.0.4")
	if err := tpl.Validate(); err != nil {
		t.Fatalf("a draft the engine would refuse is not a draft: %v", err)
	}
	if rep.Objects != 3 || rep.Rows != 2 || rep.Merged != 1 {
		t.Fatalf("report = %s", rep)
	}

	status := tpl.RowFor("ROOT.PSU.7.Status")
	if status == nil || status.Kind != KindEnum {
		t.Fatalf("the two supplies must fold into one indexed rule: %+v", tpl.Rows)
	}
	if status.Values["Error"] != "critical" || status.Values["Warning"] != "minor" || status.Values["NA"] != "minor" {
		t.Errorf("values = %v", status.Values)
	}
	// The state the device did not call bad is not in the rule at all,
	// so it stays normal: a generated file invents no policy.
	if _, listed := status.Values["OK"]; listed {
		t.Errorf("the good state must not be listed: %v", status.Values)
	}
	if status.Normal != "" {
		t.Errorf("a generated enum rule names no expected value: %q", status.Normal)
	}
	if !strings.Contains(status.Source, "NA|OK|Warning|Error") {
		t.Errorf("source = %q", status.Source)
	}

	temp := tpl.RowFor("ROOT.PSU.1.Temperature")
	if temp == nil || len(temp.High) != 1 || temp.High[0].Raise != 140 || len(temp.Low) != 1 || temp.Low[0].Raise != -40 {
		t.Fatalf("temperature rule = %+v", temp)
	}
	if !strings.Contains(temp.Source, "-40..140 C") {
		t.Errorf("source = %q", temp.Source)
	}
	if *temp.High[0].Clear >= 140 || *temp.Low[0].Clear <= -40 {
		t.Errorf("a band must clear inside its raise point: %+v", temp.High[0])
	}
}

func TestSuggestLeavesAloneWhatTheDeviceDidNotSay(t *testing.T) {
	writable := obj([]string{"ROOT", "Clock", "Source"}, consumer.KindEnum)
	writable.EnumItems = []string{"OK", "Error"}
	writable.Access = 3 // read + write: a setting, not a symptom

	choice := obj([]string{"ROOT", "Mode"}, consumer.KindEnum)
	choice.EnumItems = []string{"Standalone", "Controlled", "Auto"} // nothing bad

	noGood := obj([]string{"ROOT", "Fault"}, consumer.KindEnum)
	noGood.EnumItems = []string{"Error", "Failure"} // nothing good: misread

	wideOpen := obj([]string{"ROOT", "Counter"}, consumer.KindUint)
	wideOpen.Min, wideOpen.Max = 0, 2147483647 // the type's width, not a limit

	backwards := obj([]string{"ROOT", "Weird"}, consumer.KindInt)
	backwards.Min, backwards.Max = 10, 10

	text := obj([]string{"ROOT", "Name"}, consumer.KindString)
	text.Min, text.Max = 0, 64 // a length, on a kind no band can judge

	bare := obj([]string{"ROOT", "Something"}, consumer.KindFloat)

	tpl, rep := Suggest([]consumer.Object{writable, choice, noGood, wideOpen, backwards, text, bare}, "acp2", "m")
	if len(tpl.Rows) != 0 {
		t.Fatalf("nothing here is evidence: %+v", tpl.Rows)
	}
	if rep.Writable != 1 || rep.NoEvidence != 6 {
		t.Errorf("report = %s", rep)
	}
	if !strings.Contains(rep.String(), "1 writable setting(s) skipped") {
		t.Errorf("report line = %q", rep.String())
	}
}

func TestSuggestUsesTheDevicesOwnAlarmObject(t *testing.T) {
	// ACP1 cards carry alarm objects: the device already decided this
	// is an alarm and how loud, and says what it means in words.
	o := obj([]string{"alarm", "PSU Fail"}, consumer.KindEnum)
	o.EnumItems = []string{"Ok", "Failed"}
	o.AlarmPriority = 2
	o.AlarmOnMsg = "PSU has failed"
	o.AlarmOffMsg = "PSU restored"

	tpl, _ := Suggest([]consumer.Object{o}, "acp1", "GDR100@1.4")
	row := tpl.RowFor("alarm.PSU Fail")
	if row == nil || row.Values["Failed"] != "critical" {
		t.Fatalf("row = %+v", row)
	}
	if row.Text != "PSU has failed" || !strings.Contains(row.Source, "priority 2") {
		t.Errorf("text = %q source = %q", row.Text, row.Source)
	}

	// A lower priority is a quieter verdict, and an alarm object with
	// no readable states falls back to the plain enum path.
	o.AlarmPriority = 4
	o.AlarmOnMsg = ""
	tpl, _ = Suggest([]consumer.Object{o}, "acp1", "GDR100@1.4")
	row = tpl.RowFor("alarm.PSU Fail")
	if row.Values["Failed"] != "minor" || row.Text != "PSU Fail" {
		t.Errorf("row = %+v", row)
	}
	o.AlarmPriority = 3
	tpl, _ = Suggest([]consumer.Object{o}, "acp1", "GDR100@1.4")
	if tpl.RowFor("alarm.PSU Fail").Values["Failed"] != "major" {
		t.Errorf("priority 3 = %+v", tpl.Rows[0].Values)
	}
	o.AlarmPriority, o.AlarmOnMsg = 0, "still an alarm object"
	tpl, _ = Suggest([]consumer.Object{o}, "acp1", "GDR100@1.4")
	if tpl.RowFor("alarm.PSU Fail").Values["Failed"] != "major" {
		t.Errorf("priority 0 = %+v", tpl.Rows[0].Values)
	}
	// An alarm object the device gave no states for: nothing to judge.
	o.EnumItems = nil
	if tpl, _ := Suggest([]consumer.Object{o}, "acp1", "x"); len(tpl.Rows) != 0 {
		t.Errorf("stateless alarm object = %+v", tpl.Rows)
	}
}

func TestIndexedMatchGlobsIndexesAndNotVersions(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"ROOT_NODE_V2.PSU.1.Fan Health 3", "**.ROOT_NODE_V2.PSU.*.Fan Health *"},
		{"ROOT.SFP 12.Power Rx", "**.ROOT.SFP *.Power Rx"},
		{"ROOT.LOCK_CIRCUIT_1.Status", "**.ROOT.LOCK_CIRCUIT_*.Status"},
		{"port.3.link", "**.port.*.link"},
		{"telemetry.node.health", "**.telemetry.node.health"},
	} {
		if got := indexedMatch(strings.Split(c.in, ".")); got != c.want {
			t.Errorf("indexedMatch(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSuggestMergesToTheWidestEvidence(t *testing.T) {
	// One rule covers every instance, so it must be right about the
	// worst of them: the union of the bad states and the wider range.
	a := obj([]string{"port", "1", "temp"}, consumer.KindFloat)
	a.Min, a.Max = -5.0, 70.0
	b := obj([]string{"port", "2", "temp"}, consumer.KindFloat)
	b.Min, b.Max = -20.0, 85.0

	e1 := obj([]string{"port", "1", "state"}, consumer.KindEnum)
	e1.EnumItems = []string{"OK", "Error"}
	e2 := obj([]string{"port", "2", "state"}, consumer.KindEnum)
	e2.EnumItems = []string{"OK", "Warning"}

	tpl, rep := Suggest([]consumer.Object{a, b, e1, e2}, "mnset", "m")
	temp := tpl.RowFor("port.9.temp")
	if temp.High[0].Raise != 85 || temp.Low[0].Raise != -20 {
		t.Errorf("merged range = %+v %+v", temp.High, temp.Low)
	}
	state := tpl.RowFor("port.9.state")
	if state.Values["Error"] != "critical" || state.Values["Warning"] != "minor" {
		t.Errorf("merged values = %v", state.Values)
	}
	if rep.Merged != 2 {
		t.Errorf("report = %s", rep)
	}
	// The text names the rule, not the instance it was read from.
	if strings.ContainsAny(temp.Text, "0123456789") {
		t.Errorf("text = %q", temp.Text)
	}
}

func TestSuggestSkipsWhatIsNotAnObject(t *testing.T) {
	marker := obj([]string{"ROOT", "PSU"}, consumer.KindRaw)
	marker.SubGroupMarker = true
	pathless := consumer.Object{Label: "orphan", Kind: consumer.KindEnum, EnumItems: []string{"OK", "Error"}}

	tpl, rep := Suggest([]consumer.Object{marker, pathless}, "acp2", "m")
	if len(tpl.Rows) != 0 || rep.Objects != 0 {
		t.Errorf("rows=%v report=%s", tpl.Rows, rep)
	}
}

func TestClassifyReadsWordsNotFragments(t *testing.T) {
	for _, c := range []struct {
		item string
		want Severity
	}{
		{"Error", Critical}, {"No Signal", Critical}, {"Not Locked", Critical},
		{"disconnected", Critical}, {"Warning", Minor}, {"Initialising...", Minor},
		{"N/A", Minor}, {"OK", Normal}, {"Locked", Normal}, {"up", Normal},
		{"Standalone", Info}, // contains "alone", not "lost"
		{"Controlled", Info}, {"Slave", Info}, {"Passive", Info}, {"", Info},
		{"PTP 1", Info},
	} {
		if got := classify(c.item); got != c.want {
			t.Errorf("classify(%q) = %v, want %v", c.item, got, c.want)
		}
	}
}

func TestNumberishReadsEveryShapeADeviceSendsIts(t *testing.T) {
	for _, v := range []any{
		float64(7), float32(7), int(7), int32(7), int64(7),
		uint(7), uint32(7), uint64(7), " 7 ",
	} {
		if n, ok := numberish(v); !ok || n != 7 {
			t.Errorf("numberish(%#v) = %v, %v", v, n, ok)
		}
	}
	for _, v := range []any{nil, "seven", struct{}{}, true} {
		if _, ok := numberish(v); ok {
			t.Errorf("numberish(%#v) must refuse", v)
		}
	}
}

func TestRangeRowGivesACountNoFloor(t *testing.T) {
	// A percentage or a count starts at zero because that is how it is
	// spelled, not because zero is a fault.
	o := obj([]string{"psu", "fan"}, consumer.KindUint)
	o.Min, o.Max, o.Unit = 0, 100, "%"
	row, ok := rangeRow(o)
	if !ok || len(row.Low) != 0 || row.High[0].Raise != 100 {
		t.Fatalf("row = %+v ok=%v", row, ok)
	}
	if !strings.HasSuffix(row.Source, "0..100 %") {
		t.Errorf("source = %q", row.Source)
	}
	// A range with no unit says so without a dangling space.
	o.Unit = ""
	row, _ = rangeRow(o)
	if strings.HasSuffix(row.Source, " ") {
		t.Errorf("source = %q", row.Source)
	}
	// An unreadable limit is no limit.
	o.Min = "not a number"
	if _, ok := rangeRow(o); ok {
		t.Error("a limit that is not a number must be refused")
	}
}

func TestAnIndexedRuleIsNotNamedAfterOneInstance(t *testing.T) {
	// "Fan Health 1" and "Fan Health 2" become one rule, and an
	// operator reading it must not think it watches only fan 1.
	var objs []consumer.Object
	for _, n := range []string{"1", "2", "3", "4"} {
		o := obj([]string{"PSU", "1", "Fan Health " + n}, consumer.KindEnum)
		o.EnumItems = []string{"NA", "OK", "Error"}
		objs = append(objs, o)
	}
	tpl, rep := Suggest(objs, "acp2", "m")
	row := tpl.RowFor("PSU.2.Fan Health 4")
	if row == nil || row.Text != "Fan Health" {
		t.Fatalf("row = %+v", row)
	}
	if rep.Rows != 1 || rep.Merged != 3 {
		t.Errorf("report = %s", rep)
	}
}
