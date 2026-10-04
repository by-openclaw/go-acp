package main

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is11"
	"dhs/internal/amwa/consumer"
)

func TestCompatReport(t *testing.T) {
	// A Receiver: the state and the Device's reason come first.
	got := compatReport(&consumer.CompatResult{
		Endpoint:   "http://node:3000/x-nmos/streamcompatibility/v1.0",
		ReceiverID: "rcv-1",
		Status:     is11.Status{State: is11.ReceiverNonCompliantStream, Debug: "grain_rate 50/1 is not in caps"},
		Outputs:    []is11.Output{{ResourceCore: is11.ResourceCore{ID: "out1", Label: "SDI out"}, Connected: true, Status: is11.Status{State: is11.OutputSignalPresent}}},
	})
	for _, want := range []string{
		"receiver rcv-1 via http://node:3000/x-nmos/streamcompatibility/v1.0",
		"state: non_compliant_stream",
		"the Device says: grain_rate 50/1 is not in caps",
		"output out1  SDI out  connected=true  signal_present",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("receiver report lacks %q:\n%s", want, got)
		}
	}

	// An unconstrained Sender.
	sender := &consumer.CompatResult{
		Endpoint:  "http://node:3000/x-nmos/streamcompatibility/v1.0",
		SenderID:  "snd-1",
		Status:    is11.Status{State: is11.SenderUnconstrained},
		Supported: []string{"urn:x-nmos:cap:format:frame_width"},
		Inputs:    []is11.Input{{ResourceCore: is11.ResourceCore{ID: "in1", Label: "SDI 1"}, Connected: true, Status: is11.Status{State: is11.InputSignalPresent}}},
	}
	got = compatReport(sender)
	for _, want := range []string{
		"sender snd-1 via", "state: unconstrained", "active constraints: none",
		"can be constrained on: urn:x-nmos:cap:format:frame_width",
		"input in1  SDI 1  connected=true  signal_present",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("sender report lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "the Device says") || strings.Contains(got, "DRY RUN") || strings.Contains(got, "changed") {
		t.Errorf("an unchanged, reasonless read must say neither:\n%s", got)
	}

	// After a change, and as a dry run.
	sender.Changed = true
	sender.Status.State = is11.SenderConstrained
	sender.Active = is11.ActiveConstraints{ConstraintSets: []is11.ConstraintSet{{"urn:x-nmos:cap:format:frame_width": map[string]any{"enum": []any{1920}}}}}
	got = compatReport(sender)
	if !strings.Contains(got, "constraints changed; the Device now reports:") || !strings.Contains(got, `"urn:x-nmos:cap:format:frame_width"`) {
		t.Errorf("changed report:\n%s", got)
	}
	sender.Changed, sender.DryRun = false, true
	if got = compatReport(sender); !strings.HasPrefix(got, "DRY RUN — nothing was changed") {
		t.Errorf("dry-run report:\n%s", got)
	}
}
