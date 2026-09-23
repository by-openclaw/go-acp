package acp2

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/consumer"
	"dhs/internal/consumer/alarm"
)

// TestShippedAlarmTemplateJudgesTheShelf guards the reference template
// an operator installs with `alarm import`. The paths and the enum
// items below were read from the EVS Neuron shelf (10.6.255.102, slot
// 0, SHPRM1@6.0.4) on 2026-09-23: the device grades itself
// (NA|OK|Warning|Error) and declares its own ranges, so every row has
// a source and none of them is a guess.
func TestShippedAlarmTemplateJudgesTheShelf(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "alarm", "SHPRM1@6.0.4.json"))
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
		// The rows match the path the plugin reports, root and all,
		// and the shorter one the CLI prints.
		{"ROOT_NODE_V2.PSU.1.Status", "OK", alarm.Normal},
		{"ROOT_NODE_V2.PSU.1.Status", "Warning", alarm.Minor},
		{"ROOT_NODE_V2.PSU.1.Status", "Error", alarm.Critical},
		{"PSU.2.Status", "Error", alarm.Critical},
		{"ROOT_NODE_V2.PSU.2.Power", "Error", alarm.Critical},
		{"ROOT_NODE_V2.PSU.2.Present", "NA", alarm.Major},
		{"ROOT_NODE_V2.PSU.1.Fan Health 3", "OK", alarm.Normal},
		{"ROOT_NODE_V2.PSU.1.Fan Health 3", "Error", alarm.Major},
		{"ROOT_NODE_V2.PSU.1.Temperature", "25", alarm.Normal}, // as read
		{"ROOT_NODE_V2.PSU.1.Temperature", "141", alarm.Critical},
		{"ROOT_NODE_V2.PSU.BOARD.Temperature", "39", alarm.Normal}, // as read
		{"ROOT_NODE_V2.PSU.BOARD.Temperature", "-41", alarm.Critical},
		{"ROOT_NODE_V2.PSU.1.Fan Speed", "12", alarm.Normal}, // as read
		{"ROOT_NODE_V2.PSU.1.Fan Speed", "100", alarm.Minor},
		{"ROOT_NODE_V2.PSU.BOARD.Power Consumption", "110", alarm.Normal}, // as read
		{"ROOT_NODE_V2.PSU.BOARD.Power Consumption", "251", alarm.Major},
		{"ROOT_NODE_V2.GENERAL.Device Name", "neuron-01", alarm.Info}, // no rule
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

	// The shelf board has its own row, and it must win over the
	// per-supply one: first match wins, so it is listed first.
	if r := tpl.RowFor("ROOT_NODE_V2.PSU.BOARD.Temperature"); r == nil || r.Text != "shelf board temperature" {
		t.Errorf("BOARD temperature resolves to %+v", r)
	}
	for i, r := range tpl.Rows {
		if r.Source == "" {
			t.Errorf("row %d (%s) has no source", i, r.Match)
		}
	}
}
