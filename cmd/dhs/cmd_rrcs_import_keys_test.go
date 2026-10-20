package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"dhs/internal/rrcs/codec"
)

func TestRRCSKeyRow(t *testing.T) {
	for _, tc := range []struct {
		path, panel, key, field string
		ok                      bool
	}{
		{"net.1.node.61.port.1026.key.0.1.14.cmd.CommandType", "net.1.node.61.port.1026", "0.1.14", "cmd.CommandType", true},
		{"net.1.node.61.port.1026.key.0.1.14.LabelValue", "net.1.node.61.port.1026", "0.1.14", "LabelValue", true},
		{"net.1.node.61.port.1041.vfunc.always.0.cmd.Target", "net.1.node.61.port.1041", "vfunc.always.0", "cmd.Target", true},
		{"net.1.node.61.port.1026.LongName", "", "", "", false},
		{"net.1.node.61.port.1026.key.0.1", "", "", "", false},
	} {
		panel, key, field, ok := rrcsKeyRow(tc.path)
		if ok != tc.ok || panel != tc.panel || key != tc.key || field != tc.field {
			t.Errorf("%s: %q %q %q %v", tc.path, panel, key, field, ok)
		}
	}
	if rrcsKeyModeWord("3", "") != "latching" || rrcsKeyModeWord("", "Momentary(PTT)") != "momentary" || rrcsKeyModeWord("auto", "") != "auto" || rrcsKeyModeWord("9", "") != "" {
		t.Errorf("key mode words")
	}
	if rrcsKeyFunctionWord("call-to-port-cmd") != "call-to-port" || rrcsKeyFunctionWord("reply-cmd") != "reply" || rrcsKeyFunctionWord("route-cmd") != "" {
		t.Errorf("key function words")
	}
}

// The key rows of an export go through the key path of ensure: a
// function created or removed, the label and the mode edited; the other
// key properties stay read only; a key already as wanted is unchanged.
func TestRRCSImportKeys(t *testing.T) {
	var mu sync.Mutex
	var changes []codec.Value
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		if call.Method == "ConfigurationChange" {
			mu.Lock()
			changes = append(changes, call.Params[1].Items[0])
			mu.Unlock()
			return call.Params[0], true
		}
		return rrcsTreeAnswer(call)
	})
	file := filepath.Join(t.TempDir(), "keys.csv")
	_ = os.WriteFile(file, []byte("path,value,value_name\n"+
		"net.1.node.61.port.1026.key.0.1.2.cmd.CommandType,call-to-port-cmd,\n"+
		"net.1.node.61.port.1026.key.0.1.2.cmd.Target,net.1.node.61.port.1041,\n"+
		"net.1.node.61.port.1026.key.0.1.2.LabelValue,CODEC,\n"+
		"net.1.node.61.port.1026.key.0.1.2.KeyMode,3,Latching\n"+
		"net.1.node.61.port.1026.key.0.1.5.cmd.CommandType,,\n"+ // emptied
		"net.1.node.61.port.1026.key.0.1.1.cmd.CommandType,call-to-port-cmd,\n"+ // as it is
		"net.1.node.61.port.1026.key.0.1.1.cmd.Target,net.1.node.61.port.7.out,\n"+
		"net.1.node.61.port.1026.key.0.1.1.Dim,true,\n"+ // not through import
		"net.1.node.61.port.1026.key.0.1.9.KeyMode,9,\n"+ // not a mode
		"net.1.node.61.port.7.out.PortAes67Output.Channels,1,\n"), 0o600) // a port row beside them, unchanged

	out := rrcsRun(t, "import", f.addr(), "--file", file, "--dry-run")
	rrcsWant(t, out,
		"would_apply net.1.node.61.port.1026.key.0.1.2.function", "-> call-to-port-cmd net.1.node.61.port.1041",
		"would_apply net.1.node.61.port.1026.key.0.1.2 ", "label CODEC, mode 2 latching",
		"would_apply net.1.node.61.port.1026.key.0.1.5.function", "call-to-group group.200 -> ",
		"would apply 3, unchanged 2, skipped 2, failed 0",
		"bad_value (1)", "read_only (1)")
	mu.Lock()
	if len(changes) != 0 {
		t.Fatalf("--dry-run sent %d changes", len(changes))
	}
	mu.Unlock()

	// The write sends the create, the delete and the edit; the stand-in
	// takes nothing, so the run ends with not_taken.
	_, err := rrcsStdout(t, func() error {
		return runRRCS(context.Background(), []string{"import", f.addr(), "--file", file, "--write-to", f.addr()})
	})
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Errorf("a key the stand-in did not take passed: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	kinds := map[string]int{}
	for _, c := range changes {
		ct, _ := c.Field("ChangeType")
		ot, _ := c.Field("ObjectType")
		kinds[ct.Str+" "+ot.Str]++
	}
	// Each once per pass: ensure sends again what a second reading still
	// wants, then gives up (not_taken).
	if kinds["create call-to-port-cmd"] < 1 || kinds["delete call-to-group"] < 1 || kinds["edit panel-key"] < 1 || len(kinds) != 3 {
		t.Errorf("changes sent: %v", kinds)
	}
}
