package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/rrcs/codec"
)

func TestRRCSValueOfJSON(t *testing.T) {
	tests := map[string]codec.Value{
		`1`:              codec.Int(1),
		`-128`:           codec.Int(-128),
		`1.5`:            codec.Double(1.5),
		`true`:           codec.Bool(true),
		`"text"`:         codec.String("text"),
		`""`:             codec.String(""),
		`[]`:             codec.Array(),
		`[1,"a",[true]]`: codec.Array(codec.Int(1), codec.String("a"), codec.Array(codec.Bool(true))),
		// The order of the members is the order written.
		`{"Node":66,"Port":1045,"IsInput":false}`: codec.Struct(rrcsMember("Node", codec.Int(66)),
			rrcsMember("Port", codec.Int(1045)), rrcsMember("IsInput", codec.Bool(false))),
		`{"A":{"B":[{"C":1}]}}`: codec.Struct(rrcsMember("A", codec.Struct(rrcsMember("B",
			codec.Array(codec.Struct(rrcsMember("C", codec.Int(1)))))))),
	}
	for in, want := range tests {
		got, err := rrcsValueOfJSON(in)
		if err != nil || !valuesAlike(got, want) {
			t.Errorf("%s: got %+v, %v", in, got, err)
		}
	}
	for _, bad := range []string{``, `null`, `{`, `[1,`, `1 2`, `9999999999`, `{"a":null}`, `text`} {
		if _, err := rrcsValueOfJSON(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// valuesAlike compares two values, an empty and a nil list being alike.
func valuesAlike(a, b codec.Value) bool {
	if a.Kind != b.Kind || len(a.Items) != len(b.Items) || len(a.Members) != len(b.Members) {
		return false
	}
	for i := range a.Items {
		if !valuesAlike(a.Items[i], b.Items[i]) {
			return false
		}
	}
	for i := range a.Members {
		if a.Members[i].Name != b.Members[i].Name || !valuesAlike(a.Members[i].Value, b.Members[i].Value) {
			return false
		}
	}
	a.Items, b.Items, a.Members, b.Members = nil, nil, nil, nil
	return reflect.DeepEqual(a, b)
}

func TestRRCSReadOnlyMethod(t *testing.T) {
	for _, m := range []string{"GetAllPorts", "GetXpStatus", "IsConnectedToArtist"} {
		if !rrcsReadOnlyMethod(m) {
			t.Errorf("%s taken for a write", m)
		}
	}
	for _, m := range []string{"SetXp", "KillXp", "ConfigurationChangeEx", "RegisterForAllEvents", "ResetAllNodes", "ConnectToArtist"} {
		if rrcsReadOnlyMethod(m) {
			t.Errorf("%s taken for a read", m)
		}
	}
}

func TestRRCSCall(t *testing.T) {
	var mu sync.Mutex
	var got []codec.Call
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		mu.Lock()
		got = append(got, call)
		mu.Unlock()
		switch call.Method {
		case "GetPortAlias":
			return codec.Array(call.Params[0], codec.Int(0), codec.String("PNL TWO")), true
		case "SetXp":
			return codec.Array(call.Params[0], codec.Int(0)), true
		case "GetAlive":
			return codec.Array(), true
		case "GetXpStatus":
			return codec.Array(call.Params[0], codec.Int(4)), true
		}
		return codec.Value{}, false
	})
	ctx := context.Background()

	out := rrcsRun(t, "call", f.addr(), "GetPortAlias", "--arg", "1", "--arg", "61", "--arg", "1026", "--arg", "false")
	if strings.TrimSpace(out) != `["PNL TWO"]` {
		t.Errorf("payload: %s", out)
	}
	if p := got[0].Params; len(p) != 5 || p[1].Int != 1 || p[3].Int != 1026 || p[4].Kind != codec.KindBool {
		t.Errorf("sent %+v", got[0])
	}
	whole := rrcsRun(t, "call", f.addr(), "GetPortAlias", "--output", "json")
	if !strings.Contains(whole, `,0,"PNL TWO"]`) {
		t.Errorf("whole answer: %s", whole)
	}
	if out := rrcsRun(t, "call", f.addr(), "GetAlive", "--key", "no"); strings.TrimSpace(out) != "[]" || len(got[len(got)-1].Params) != 0 {
		t.Errorf("no key: %s, sent %+v", out, got[len(got)-1])
	}

	// A write names its target twice.
	var val *consumer.ValidationError
	before := len(got)
	set := []string{"call", f.addr(), "SetXp", "--arg", "1", "--arg", "61", "--arg", "7", "--arg", "1", "--arg", "61", "--arg", "1026"}
	if err := runRRCS(ctx, set); !errors.As(err, &val) || len(got) != before {
		t.Errorf("write without --write-to: %v, %d requests sent", err, len(got)-before)
	}
	rrcsRun(t, append(set, "--write-to", f.addr())...)
	if last := got[len(got)-1]; last.Method != "SetXp" || len(last.Params) != 7 {
		t.Errorf("sent %+v", last)
	}

	// An error code and a fault are errors; the answer is still shown.
	out, err := rrcsStdout(t, func() error { return runRRCS(ctx, []string{"call", f.addr(), "GetXpStatus"}) })
	if !errors.Is(err, &codec.CodeError{Code: codec.ErrorCode(4)}) || !strings.Contains(out, ",4]") {
		t.Errorf("error code: %v\n%s", err, out)
	}
	var fault *codec.Fault
	if err := runRRCS(ctx, []string{"call", f.addr(), "GetNothing"}); !errors.As(err, &fault) {
		t.Errorf("fault: %v", err)
	}
	// Parameters from a file.
	file := filepath.Join(t.TempDir(), "args.json")
	_ = os.WriteFile(file, []byte("ï»¿[1, 61, 1026, false]"), 0o644)
	if out := rrcsRun(t, "call", f.addr(), "GetPortAlias", "--args-file", file); strings.TrimSpace(out) != `["PNL TWO"]` || len(got[len(got)-1].Params) != 5 {
		t.Errorf("args file: %s, sent %+v", out, got[len(got)-1])
	}
	notArray := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(notArray, []byte(`{"a":1}`), 0o644)
	for name, args := range map[string][]string{
		"file not array": {"call", f.addr(), "GetState", "--args-file", notArray},
		"file and arg":   {"call", f.addr(), "GetState", "--args-file", file, "--arg", "1"},
		"no method":      {"call", f.addr()},
		"bad arg":        {"call", f.addr(), "GetState", "--arg", "{"},
		"bad key":        {"call", f.addr(), "GetState", "--key", "maybe"},
	} {
		if err := runRRCS(ctx, args); !errors.As(err, &val) {
			t.Errorf("%s: got %v, want a validation error", name, err)
		}
	}
}

func TestRRCSXp(t *testing.T) {
	var mu sync.Mutex
	on := false
	obey := true
	var methods []string
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		mu.Lock()
		defer mu.Unlock()
		methods = append(methods, call.Method)
		k := call.Params[0]
		switch call.Method {
		case "SetXp", "KillXp":
			// §11.1: key, source net, node, port, destination net, node, port.
			if len(call.Params) != 7 || call.Params[2].Int != 61 || call.Params[3].Int != 7 || call.Params[6].Int != 1026 {
				return codec.Array(k, codec.Int(4)), true
			}
			if obey {
				on = call.Method == "SetXp"
			}
			return codec.Array(k, codec.Int(0)), true
		case "GetXpStatus":
			return codec.Array(k, codec.Int(0), codec.Bool(on)), true
		}
		return codec.Value{}, false
	})
	src, dst := "net.1.node.61.port.7.in", "net.1.node.61.port.1026"
	ctx := context.Background()

	rrcsWant(t, rrcsRun(t, "xp", f.addr(), "--src", src, "--dst", dst), "xp."+src+">"+dst+" State = off")
	rrcsWant(t, rrcsRun(t, "xp", f.addr(), "--src", src, "--dst", dst, "--state", "on", "--write-to", f.addr()), "State = on")
	rrcsWant(t, rrcsRun(t, "xp", f.addr(), "--src", src, "--dst", dst, "--state", "off", "--write-to", f.addr()), "State = off")
	if got := strings.Join(methods, ","); got != "GetXpStatus,SetXp,GetXpStatus,KillXp,GetXpStatus" {
		t.Errorf("methods %s", got)
	}

	var val *consumer.ValidationError
	for name, args := range map[string][]string{
		"no guard":  {"xp", f.addr(), "--src", src, "--dst", dst, "--state", "on"},
		"bad state": {"xp", f.addr(), "--src", src, "--dst", dst, "--state", "maybe"},
		"bad path":  {"xp", f.addr(), "--src", "group.1", "--dst", dst},
		"no host":   {"xp", "--src", src, "--dst", dst},
	} {
		if err := runRRCS(ctx, args); !errors.As(err, &val) {
			t.Errorf("%s: got %v, want a validation error", name, err)
		}
	}
	// Accepted and not taken is a failure.
	obey = false
	if _, err := rrcsStdout(t, func() error {
		return runRRCS(ctx, []string{"xp", f.addr(), "--src", src, "--dst", dst, "--state", "on", "--write-to", f.addr()})
	}); err == nil || !strings.Contains(err.Error(), "reads off") {
		t.Errorf("not taken: %v", err)
	}
	// Refused by the gateway.
	if _, err := rrcsStdout(t, func() error {
		return runRRCS(ctx, []string{"xp", f.addr(), "--src", "net.1.node.9.port.9", "--dst", dst, "--state", "on", "--write-to", f.addr()})
	}); !errors.Is(err, &codec.CodeError{Code: codec.ErrorCode(4)}) {
		t.Errorf("refused: %v", err)
	}
}
