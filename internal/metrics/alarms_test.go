package metrics

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func scrape(t *testing.T, reg *PromRegistry) string {
	t.Helper()
	srv := httptest.NewServer(reg.Handler())
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(body)
}

// A watched device shows its alarms the same way whatever protocol it
// speaks: the device label is on every series, and the rules/active
// series exist even while nothing is wrong, so a dashboard can list
// the device at all.
func TestAlarmCollectorScrapes(t *testing.T) {
	reg := NewPromRegistry()
	snap := AlarmSnapshot{
		Model: "SHPRM1@6.0.4",
		Rules: 8,
		Counts: map[string]int{
			"normal": 3, "minor": 1, "major": 0, "critical": 1, "error": 0,
		},
		Transitions: map[string]uint64{"minor": 2, "critical": 1},
		Active: []AlarmSample{
			{Slot: 1, Path: "REFERENCE.Clock Status", Label: "Clock Status", Band: "value", Severity: "major", Code: 3},
			{Slot: 0, Path: "PSU.2.Status", Label: "Status", Band: "value", Severity: "critical", Code: 4},
			{Slot: 0, Path: "PSU.1.Fan Health 3", Label: "Fan Health 3", Band: "value", Severity: "minor", Code: 2},
		},
	}
	err := reg.AttachAlarms(func() AlarmSnapshot { return snap }, map[string]string{
		"proto": "acp2", "device": "10.6.255.102", "role": "consumer",
	})
	if err != nil {
		t.Fatalf("AttachAlarms: %v", err)
	}

	body := scrape(t, reg)
	for _, want := range []string{
		`dhs_alarm_rules{device="10.6.255.102",model="SHPRM1@6.0.4",proto="acp2",role="consumer"} 8`,
		`dhs_alarm_active{device="10.6.255.102",proto="acp2",role="consumer",severity="critical"} 1`,
		`dhs_alarm_active{device="10.6.255.102",proto="acp2",role="consumer",severity="major"} 0`,
		`dhs_alarm_transitions_total{device="10.6.255.102",proto="acp2",role="consumer",severity="minor"} 2`,
		`band="value",device="10.6.255.102",label="Status",path="PSU.2.Status"`,
		`severity="critical",slot="0"} 4`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape is missing %q\n%s", want, body)
		}
	}

	// Two scrapes of an unchanged device agree line for line (the Go
	// runtime's own numbers move, ours must not), so a diff shows what
	// changed and not how a map felt that second.
	if again := alarmLines(scrape(t, reg)); again != alarmLines(body) {
		t.Errorf("two scrapes of the same state disagree:\n%s\n---\n%s", alarmLines(body), again)
	}
	if !strings.Contains(body, `path="REFERENCE.Clock Status"`) {
		t.Error("an object on another slot must be exported too")
	}
}

// A device with no template at all still appears — with no rules, and
// nothing active. That is how an operator sees "nobody is judging
// this one" instead of seeing nothing.
func TestAlarmCollectorWithoutATemplate(t *testing.T) {
	reg := NewPromRegistry()
	if err := reg.AttachAlarms(func() AlarmSnapshot { return AlarmSnapshot{} },
		map[string]string{"proto": "acp1", "device": "10.6.255.110"}); err != nil {
		t.Fatalf("AttachAlarms: %v", err)
	}
	body := scrape(t, reg)
	if !strings.Contains(body, `dhs_alarm_rules{device="10.6.255.110",model="",proto="acp1"} 0`) {
		t.Errorf("empty snapshot scrape:\n%s", body)
	}
	if strings.Contains(body, "dhs_alarm_severity") {
		t.Error("a healthy device must export no per-object series")
	}
}

func TestAttachAlarmsRefusesNoSource(t *testing.T) {
	if err := NewPromRegistry().AttachAlarms(nil, nil); err == nil {
		t.Error("a nil source must be refused")
	}
}

// alarmLines keeps the dhs_alarm_* samples of a scrape, dropping the
// HELP/TYPE headers and every metric the Go runtime moves on its own.
func alarmLines(body string) string {
	var keep []string
	for _, ln := range strings.Split(body, "\n") {
		if strings.HasPrefix(ln, "dhs_alarm_") {
			keep = append(keep, ln)
		}
	}
	return strings.Join(keep, "\n")
}
