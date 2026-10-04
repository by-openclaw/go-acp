package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is12"
	"dhs/internal/amwa/consumer"
)

func TestControlReport(t *testing.T) {
	const ep = "ws://node:3001/x-nmos/ncp/v1.0"
	const rx = "root.receivers.rx1"
	for name, tc := range map[string]struct {
		res  *consumer.ControlResult
		req  consumer.ControlRequest
		want []string
	}{
		"listing": {
			res: &consumer.ControlResult{Endpoint: ep, Objects: []consumer.ControlObject{
				{RolePath: "root", OID: 1, ClassID: []int32{1, 1}},
				{RolePath: rx, OID: 11, ClassID: []int32{1, 2, 2, 1}, UserLabel: "RX 1"},
			}},
			want: []string{"objects via " + ep, "oid 1      class 1.1          root\n", "oid 11     class 1.2.2.1      " + rx + "  RX 1"},
		},
		"describing": {
			res:  &consumer.ControlResult{Endpoint: ep, RolePath: rx, OID: 11, Class: json.RawMessage(`{"name":"NcReceiverMonitor"}`)},
			req:  consumer.ControlRequest{RolePath: rx},
			want: []string{rx + " (oid 11) via " + ep, `"name": "NcReceiverMonitor"`},
		},
		"get": {
			res:  &consumer.ControlResult{Endpoint: ep, RolePath: rx, Value: json.RawMessage(`"RX 1"`)},
			req:  consumer.ControlRequest{RolePath: rx, Get: "1p6"},
			want: []string{rx + `.1p6 = "RX 1"`},
		},
		"set": {
			res:  &consumer.ControlResult{Endpoint: ep, RolePath: rx, Value: json.RawMessage(`"Studio A"`)},
			req:  consumer.ControlRequest{RolePath: rx, Set: "1p6", SetValue: json.RawMessage(`"Studio A"`)},
			want: []string{"SET " + rx + ".1p6 via " + ep + `: the Device now holds "Studio A"`},
		},
		"dry-run set": {
			res:  &consumer.ControlResult{Endpoint: ep, RolePath: rx, Value: json.RawMessage(`"RX 1"`), DryRun: true},
			req:  consumer.ControlRequest{RolePath: rx, Set: "1p6", SetValue: json.RawMessage(`"Studio A"`)},
			want: []string{"DRY RUN", "would set " + rx + `.1p6 = "Studio A" (the Device holds "RX 1")`},
		},
		"invoke": {
			res:  &consumer.ControlResult{Endpoint: ep, RolePath: rx, Value: json.RawMessage(`"done"`), Result: is12.MethodResult{Status: 200}},
			req:  consumer.ControlRequest{RolePath: rx, Invoke: "4m1"},
			want: []string{"INVOKED 4m1 on " + rx + " via " + ep + ": status 200", `value: "done"`},
		},
		"dry-run invoke": {
			res:  &consumer.ControlResult{Endpoint: ep, RolePath: rx, DryRun: true},
			req:  consumer.ControlRequest{RolePath: rx, Invoke: "4m1"},
			want: []string{"would invoke 4m1 on " + rx},
		},
		"watch": {
			res:  &consumer.ControlResult{Endpoint: ep, RolePath: rx, Changes: 3},
			req:  consumer.ControlRequest{RolePath: rx, Watch: true},
			want: []string{"3 change(s) via " + ep},
		},
	} {
		got := controlReport(tc.res, tc.req)
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Errorf("%s: report lacks %q:\n%s", name, want, got)
			}
		}
	}
}

func TestControlChangeLine(t *testing.T) {
	third := 2
	for _, tc := range []struct {
		change consumer.ControlChange
		want   string
	}{
		{consumer.ControlChange{RolePath: "root.receivers.rx1", OID: 11, Property: "1p6", Value: json.RawMessage(`"Studio A"`)},
			"changed  root.receivers.rx1.1p6 = \"Studio A\"\n"},
		{consumer.ControlChange{RolePath: "root", OID: 1, Property: "2p2", Value: json.RawMessage(`{}`), SequenceItemIndex: &third},
			"changed  root.2p2[2] = {}\n"},
		// An object the model did not list is still told apart: by its oid.
		{consumer.ControlChange{OID: 77, Property: "3p1", Value: json.RawMessage(`1`)}, "changed  oid 77.3p1 = 1\n"},
	} {
		if got := controlChangeLine(tc.change); got != tc.want {
			t.Errorf("line = %q, want %q", got, tc.want)
		}
	}
}

// What the verb refuses before it reaches a Device.
func TestControlVerbRefusals(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--node", "http://127.0.0.1:1", "--device", "d", "--set", "1p6"}, "want <property-id>=<json>"},
		{[]string{"--no-such-flag"}, "no-such-flag"},
		{[]string{"--node", "not a url", "--device", "d"}, "nmos control:"},
	} {
		if err := runNMOSControl(context.Background(), tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: err = %v, want it to say %q", tc.args, err, tc.want)
		}
	}
}
