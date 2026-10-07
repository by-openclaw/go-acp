package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/rrcs/codec"
)

func TestRRCSOpVerb(t *testing.T) {
	for method, want := range map[string]string{
		"SetXpPrio":               "set-xp-prio",
		"SetIFBVolumeMixMinus":    "set-ifb-volume-mix-minus",
		"SetLTC":                  "set-ltc",
		"PressKeyEx":              "press-key-ex",
		"SetSystemTimeOnAllNodes": "set-system-time-on-all-nodes",
		"GetGpInputState":         "get-gp-input-state",
		"SetStageRegistryUrl":     "set-stage-registry-url",
		"LineStatus":              "line-status",
	} {
		if got := rrcsOpVerb(method); got != want {
			t.Errorf("%s → %s, want %s", method, got, want)
		}
	}
	seen := map[string]string{}
	for _, op := range rrcsOps {
		v := rrcsOpVerb(op.Method)
		if other, dup := seen[v]; dup {
			t.Errorf("%s and %s share the verb %s", op.Method, other, v)
		}
		seen[v] = op.Method
		if rrcsFindOp(v) == nil {
			t.Errorf("%s is not found by its verb", op.Method)
		}
	}
}

// rrcsOpArgs gives every flag of an op a sample value, and says how many
// parameters the method must then carry besides the transaction key.
func rrcsOpArgs(t *testing.T, op rrcsOp, dir string) (args []string, params int) {
	t.Helper()
	const port = "net.1.node.61.port.7.in"
	for _, p := range op.Params {
		switch p.Kind {
		case "int":
			args, params = append(args, "--"+p.Flag, "3"), params+1
		case "bool":
			args, params = append(args, "--"+p.Flag, "yes"), params+1
		case "text":
			args, params = append(args, "--"+p.Flag, "abc"), params+1
		case "netnodeport":
			args, params = append(args, "--"+p.Flag, port), params+3
		case "nodeport":
			args, params = append(args, "--"+p.Flag, port), params+2
		case "isinput":
			params++
		case "xp":
			args, params = append(args, "--src", port, "--dst", "net.1.node.61.port.1026"), params+6
		case "key":
			args, params = append(args, "--"+p.Flag, "net.1.node.61.port.1026.key.2.1.5"), params+6
		case "portaddr":
			args, params = append(args, "--"+p.Flag, port), params+1
		case "cmdpos":
			args, params = append(args, "--"+p.Flag, "net.1.node.61.port.1026.key.0.1.5"), params+1
		case "changes":
			file := filepath.Join(dir, "changes.json")
			_ = os.WriteFile(file, []byte(`[{"ChangeType":"edit","ObjectType":"portex","SpecificParams":{"Alias":"x"}}]`), 0o644)
			args, params = append(args, "--"+p.Flag, file), params+1
		case "xplist":
			args, params = append(args, "--"+p.Flag, port+">net.1.node.61.port.1026"), params+1
		default:
			t.Fatalf("%s: unknown kind %q", op.Method, p.Kind)
		}
	}
	return args, params
}

// Every method of the table: its verb sends that method with as many
// parameters as its row says, refuses to write without the guard, and
// reads without it.
func TestRRCSOpsSendTheirMethod(t *testing.T) {
	var mu sync.Mutex
	var got []codec.Call
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		mu.Lock()
		got = append(got, call)
		mu.Unlock()
		return codec.Array(call.Params[0], codec.Int(0)), true
	})
	ctx := context.Background()
	var val *consumer.ValidationError
	for _, op := range rrcsOps {
		verb := rrcsOpVerb(op.Method)
		args, want := rrcsOpArgs(t, op, t.TempDir())
		base := append([]string{verb, f.addr()}, args...)

		mu.Lock()
		before := len(got)
		mu.Unlock()
		if !op.Read {
			if err := runRRCS(ctx, base); !errors.As(err, &val) || !strings.Contains(err.Error(), "--write-to") {
				t.Errorf("%s without --write-to: %v", verb, err)
			}
			mu.Lock()
			if len(got) != before {
				t.Errorf("%s reached the gateway without the guard", verb)
			}
			mu.Unlock()
			base = append(base, "--write-to", f.addr())
		}
		if _, err := rrcsStdout(t, func() error { return runRRCS(ctx, base) }); err != nil {
			t.Errorf("%s: %v", verb, err)
			continue
		}
		mu.Lock()
		last := got[len(got)-1]
		mu.Unlock()
		if last.Method != op.Method || len(last.Params)-1 != want {
			t.Errorf("%s sent %s with %d parameters, want %s with %d", verb, last.Method, len(last.Params)-1, op.Method, want)
		}
		// A flag left out is refused before anything is sent.
		if len(args) > 0 && op.Params[0].Kind != "text" {
			if err := runRRCS(ctx, []string{verb, f.addr(), "--write-to", f.addr()}); !errors.As(err, &val) {
				t.Errorf("%s with no flag: %v", verb, err)
			}
		}
	}
}

// The parameters of a few methods, value by value, against the order the
// specification prints.
func TestRRCSOpsParameterOrder(t *testing.T) {
	var last codec.Call
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		last = call
		return codec.Array(call.Params[0], codec.Int(0)), true
	})
	run := func(args ...string) []codec.Value {
		t.Helper()
		rrcsRun(t, append(args, "--write-to", f.addr())...)
		return last.Params[1:]
	}
	ints := func(vs []codec.Value) string {
		parts := make([]string, 0, len(vs))
		for _, v := range vs {
			parts = append(parts, rrcsCompact(v))
		}
		return strings.Join(parts, " ")
	}

	// §8.11 PressKey: Node, Port, IsInput, page, ExpansionPanel, KeyNumber, IsVirtKey, Press, PoolPort.
	if got := ints(run("press-key", f.addr(), "--key", "net.1.node.61.port.1026.key.2.1.5", "--virtual", "no", "--press", "yes", "--pool-port", "-1")); got != `61 1026 false 1 2 5 false true -1` {
		t.Errorf("PressKey: %s", got)
	}
	// §8.2 SetXpVolume: six addresses, single, conference, volume.
	if got := ints(run("set-xp-volume", f.addr(), "--src", "net.1.node.61.port.7.in", "--dst", "net.1.node.63.port.1043", "--single", "yes", "--conference", "no", "--volume", "218")); got != `1 61 7 1 63 1043 true false 218` {
		t.Errorf("SetXpVolume: %s", got)
	}
	// §8.3 SetPortAlias: Net, Node, Port, Alias, IsInput.
	if got := ints(run("set-port-alias", f.addr(), "--port-path", "net.1.node.61.port.7.in", "--alias", "CAM 1")); got != `1 61 7 "CAM 1" true` {
		t.Errorf("SetPortAlias: %s", got)
	}
	// §8.4 SetPortLabel: Node, Port, Label, IsInput — no net.
	if got := ints(run("set-port-label", f.addr(), "--port-path", "net.1.node.61.port.1026", "--label", "NOC2")); got != `61 1026 "NOC2" false` {
		t.Errorf("SetPortLabel: %s", got)
	}
	// §8.13 StartPortCloning: two TPortAddress structs.
	if got := ints(run("start-port-cloning", f.addr(), "--monitor", "net.1.node.61.port.7.out", "--clone", "net.1.node.61.port.1026")); got != `{"IsInput":false,"Node":61,"Port":7} {"IsInput":false,"Node":61,"Port":1026}` {
		t.Errorf("StartPortCloning: %s", got)
	}
	// §8.23 DeletePortCommands: one TCmdPosition, for a key, a virtual function, a whole port.
	if got := ints(run("delete-port-commands", f.addr(), "--position", "net.1.node.61.port.1026.key.0.1.5")); got != `{"ExpansionPanel":0,"IsInput":false,"KeyNumber":5,"Node":61,"Page":1,"Port":1026,"PositionType":"key"}` {
		t.Errorf("DeletePortCommands key: %s", got)
	}
	if got := ints(run("delete-port-commands", f.addr(), "--position", "net.1.node.61.port.1041.vfunc.always.0")); !strings.Contains(got, `"PositionType":"virtual-function"`) || !strings.Contains(got, `"VirtualFunctionType":"always"`) {
		t.Errorf("DeletePortCommands virtual function: %s", got)
	}
	if got := ints(run("delete-port-commands", f.addr(), "--position", "net.1.node.61.port.1026")); strings.Contains(got, "PositionType") {
		t.Errorf("DeletePortCommands port: %s", got)
	}
	// §8.15 XpVolumeChangeRegistryRemove: IP, port, [{Destination, Source}].
	got := run("xp-volume-change-registry-remove", f.addr(), "--ip", "10.0.0.9", "--tcp-port", "8195", "--xp", "net.1.node.61.port.7.in>net.1.node.61.port.1026")
	if len(got) != 3 || got[0].Str != "10.0.0.9" || got[1].Int != 8195 || len(got[2].Items) != 1 || got[2].Items[0].Members[0].Name != "Destination" {
		t.Errorf("XpVolumeChangeRegistryRemove: %s", ints(got))
	}
	// A text may be empty: an alias cleared.
	if got := ints(run("set-port-alias", f.addr(), "--port-path", "net.1.node.61.port.7.in", "--alias", "")); got != `1 61 7 "" true` {
		t.Errorf("empty alias: %s", got)
	}
}

func TestRRCSOpsRefusals(t *testing.T) {
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		if call.Method == "LineStatus" {
			return codec.Struct(rrcsMember("ErrorCode", codec.Int(0)), rrcsMember("Status", codec.Int(2)), rrcsMember("TransKey", call.Params[0])), true
		}
		return codec.Value{}, false
	})
	ctx := context.Background()
	var val *consumer.ValidationError
	w := []string{"--write-to", f.addr()}
	for name, args := range map[string][]string{
		"no host":       {"set-ltc", "--node", "3"},
		"bad int":       append([]string{"set-ltc", f.addr(), "--node", "x"}, w...),
		"bad bool":      append([]string{"set-logic-source-state", f.addr(), "--id", "5", "--state", "maybe"}, w...),
		"bad port":      append([]string{"set-input-gain", f.addr(), "--port-path", "group.1", "--gain", "0"}, w...),
		"bad key":       append([]string{"press-key", f.addr(), "--key", "net.1.node.61.port.1026", "--virtual", "no", "--press", "yes", "--pool-port", "-1"}, w...),
		"bad xp":        append([]string{"set-xp-prio", f.addr(), "--src", "a", "--dst", "b", "--priority", "1"}, w...),
		"bad position":  append([]string{"delete-port-commands", f.addr(), "--position", "group.1"}, w...),
		"bad xp list":   append([]string{"xp-volume-change-registry-reset", f.addr(), "--ip", "1.2.3.4", "--tcp-port", "1", "--xp", "nonsense"}, w...),
		"no xp list":    append([]string{"xp-volume-change-registry-reset", f.addr(), "--ip", "1.2.3.4", "--tcp-port", "1"}, w...),
		"bad port addr": append([]string{"stop-port-cloning", f.addr(), "--monitor", "x", "--clone", "y"}, w...),
	} {
		if err := runRRCS(ctx, args); !errors.As(err, &val) {
			t.Errorf("%s: got %v, want a validation error", name, err)
		}
	}
	file := filepath.Join(t.TempDir(), "notarray.json")
	_ = os.WriteFile(file, []byte(`{"a":1}`), 0o644)
	if err := runRRCS(ctx, append([]string{"configuration-change", f.addr(), "--file", file}, w...)); !errors.As(err, &val) {
		t.Errorf("changes not an array: %v", err)
	}
	if err := runRRCS(ctx, append([]string{"configuration-change", f.addr(), "--file", filepath.Join(t.TempDir(), "none.json")}, w...)); err == nil || errors.As(err, &val) {
		t.Errorf("missing changes file: %v", err)
	}
	// A read needs no guard, and prints the answer.
	rrcsWant(t, rrcsRun(t, "line-status", f.addr(), "--port-path", "net.1.node.61.port.7.in"), `{"Status":2}`)
	rrcsWant(t, rrcsRun(t, "line-status", f.addr(), "--port-path", "net.1.node.61.port.7.in", "--output", "json"), `"TransKey"`)
	// A fault from the gateway is the error of the verb.
	var fault *codec.Fault
	if err := runRRCS(ctx, append([]string{"reset-all-nodes", f.addr()}, w...)); !errors.As(err, &fault) {
		t.Errorf("fault: %v", err)
	}
}
