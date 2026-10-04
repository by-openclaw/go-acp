package main

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is08"
	"dhs/internal/amwa/consumer"
)

func TestParseChannelRoute(t *testing.T) {
	for arg, want := range map[string]consumer.ChannelRoute{
		"out1:1=in1:0":           {Output: "out1", Channel: 1, Input: "in1", InputChannel: 0},
		" out1:0 = in2:3 ":       {Output: "out1", Channel: 0, Input: "in2", InputChannel: 3},
		"out1:1=":                {Output: "out1", Channel: 1},
		"urn:x:out:2=urn:x:in:0": {Output: "urn:x:out", Channel: 2, Input: "urn:x:in", InputChannel: 0},
	} {
		got, err := parseChannelRoute(arg)
		if err != nil || got != want {
			t.Errorf("parseChannelRoute(%q) = %+v, %v — want %+v", arg, got, err, want)
		}
	}
	for _, bad := range []string{"out1:1", "out1=in1:0", ":1=in1:0", "out1:x=in1:0", "out1:-1=in1:0", "out1:1=in1", "out1:1=in1:x"} {
		if _, err := parseChannelRoute(bad); err == nil || !strings.Contains(err.Error(), "want <output>:<channel>=") {
			t.Errorf("parseChannelRoute(%q) must be refused with the expected form, got %v", bad, err)
		}
	}
}

func mapResult() *consumer.ChannelMapResult {
	in1, zero, one := "in1", 0, 1
	gone := "gone"
	return &consumer.ChannelMapResult{
		DeviceID: "dev-1",
		Endpoint: "http://node:3000/x-nmos/channelmapping/v1.0",
		IO: is08.IO{
			Inputs: map[string]is08.Input{"in1": {Channels: []is08.Channel{{Label: "L"}, {Label: "R"}}}},
			Outputs: map[string]is08.Output{
				"out1": {Properties: &is08.OutputProperties{Name: "Tx 1"}, Channels: []is08.Channel{{Label: "1"}, {Label: "2"}, {Label: "3"}}},
				"out0": {Channels: []is08.Channel{{Label: "A"}}},
			},
		},
		Active: is08.MapActive{Map: is08.MapEntries{
			"out1": {"0": {Input: &in1, ChannelIndex: &one}, "1": {}, "2": {Input: &gone, ChannelIndex: &zero}},
		}},
	}
}

func TestChannelMapReport(t *testing.T) {
	// A read: outputs in id order, each channel with what feeds it.
	res := mapResult()
	res.Pending = map[string]is08.MapActivationResponse{"act-9": {}, "act-2": {}}
	got := channelMapReport(res, "")
	for _, want := range []string{
		"channel map of device dev-1 via http://node:3000/x-nmos/channelmapping/v1.0",
		"output out1  Tx 1",
		"<- in1:1  R",   // routed, with the input channel's label
		"<- (unrouted)", // explicit no-input
		"<- gone:0",     // an input the Device does not list: shown as it is
		"scheduled, not yet applied: act-2, act-9",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("read report lacks %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "output out0") > strings.Index(got, "output out1") {
		t.Errorf("outputs must be listed in id order:\n%s", got)
	}

	// Applied.
	res = mapResult()
	res.ActivationID = "act-1"
	if got := channelMapReport(res, ""); !strings.Contains(got, "MAPPED via") || !strings.Contains(got, "the Device now reports") {
		t.Errorf("applied report:\n%s", got)
	}

	// Scheduled.
	res.Scheduled = true
	if got := channelMapReport(res, ""); !strings.Contains(got, "SCHEDULED via") || !strings.Contains(got, "--cancel act-1") {
		t.Errorf("scheduled report:\n%s", got)
	}

	// Cancelled.
	res = mapResult()
	res.Cancelled = "act-1"
	if got := channelMapReport(res, "act-1"); !strings.Contains(got, "CANCELLED activation act-1") {
		t.Errorf("cancel report:\n%s", got)
	}

	// Dry runs: of a map, and of a cancel.
	res = mapResult()
	res.DryRun = true
	res.Request = &is08.MapActivationRequest{Activation: is08.Activation{Mode: is08.ActivationModeImmediate}}
	if got := channelMapReport(res, ""); !strings.Contains(got, "DRY RUN") || !strings.Contains(got, "would POST") || !strings.Contains(got, "current map:") {
		t.Errorf("dry-run report:\n%s", got)
	}
	if got := channelMapReport(res, "act-1"); !strings.Contains(got, "would DELETE") || !strings.Contains(got, "/map/activations/act-1") {
		t.Errorf("dry-run cancel report:\n%s", got)
	}
}
