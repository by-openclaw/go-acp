package mnset

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/consumer"
	"dhs/internal/consumer/alarm"
)

// TestShippedAlarmTemplateJudgesTheModule guards the reference template
// an operator installs with `alarm import`. Every expectation below is
// a value read from the lab FusioN6 (10.6.40.53) on 2026-09-23, judged
// against the thresholds the module itself publishes.
func TestShippedAlarmTemplateJudgesTheModule(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "alarm", "fusion6.alarm.json"))
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := alarm.Load(data)
	if err != nil {
		t.Fatalf("the shipped template must load: %v", err)
	}
	e := alarm.New(tpl, clock.NewFake(time.Unix(1700000000, 0)))

	cases := []struct {
		path, value string
		want        alarm.Severity
	}{
		{"port.3.sfp_ddm_info.temperature.current", "34.92", alarm.Normal}, // as read
		{"port.3.sfp_ddm_info.temperature.current", "81", alarm.Minor},     // DDM high warning 80
		{"port.3.sfp_ddm_info.temperature.current", "86", alarm.Critical},  // DDM high alarm 85
		{"port.3.sfp_ddm_info.temperature.current", "-16", alarm.Critical}, // DDM low alarm -15
		{"port.3.sfp_ddm_info.vcc.current", "3.32", alarm.Normal},
		{"port.3.sfp_ddm_info.vcc.current", "3.52", alarm.Critical},
		{"refclk.status", "3", alarm.Normal}, // locked to the grandmaster
		{"refclk.status", "0", alarm.Major},
		{"telemetry.node.interfaces.e1.link_status", "up", alarm.Normal},
		{"telemetry.node.interfaces.e1.link_status", "down", alarm.Critical},
		{"self.ipconfig.hostname", "fusion-01", alarm.Info}, // no rule: reported, never alarmed
	}
	for _, c := range cases {
		sev, _, _ := e.Explain(consumer.Event{
			Path:  c.path,
			Value: consumer.Value{Kind: consumer.KindString, Str: c.value},
		})
		if sev != c.want {
			t.Errorf("%s = %q → %v, want %v", c.path, c.value, sev, c.want)
		}
	}
	// Every row names where its numbers come from: this repo ships no
	// guessed threshold.
	for i, r := range tpl.Rows {
		if r.Source == "" {
			t.Errorf("row %d (%s) has no source", i, r.Match)
		}
	}
}
