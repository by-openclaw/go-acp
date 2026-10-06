package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/rrcs/codec"
)

// rrcsEnsureFake is the tree stand-in with one sender whose multicast
// address follows the edits, and one crosspoint that follows SetXp and
// KillXp.
func rrcsEnsureFake(t *testing.T) (*rrcsFake, func() []string) {
	t.Helper()
	var mu sync.Mutex
	multicast := "239.1.2.3"
	xp := false
	var writes []string
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		mu.Lock()
		defer mu.Unlock()
		k := call.Params[0]
		switch call.Method {
		case "ConfigurationChangeEx":
			sp, _ := call.Params[1].Items[0].Field("SpecificParams")
			if out, ok := sp.Field("PortAes67Output"); ok {
				if m, ok := out.Field("Multicast"); ok {
					multicast = m.Str
				}
			}
			writes = append(writes, call.Method)
			return k, true
		case "SetXp", "KillXp":
			xp = call.Method == "SetXp"
			writes = append(writes, call.Method)
			return codec.Array(k, codec.Int(0)), true
		case "GetXpStatus":
			return codec.Array(k, codec.Int(0), codec.Bool(xp)), true
		case "GetAllPorts":
			v, ok := rrcsTreeAnswer(call)
			for _, p := range v.Items[1].Items {
				if number, _ := p.Field("Port"); number.Int != 7 {
					continue
				}
				if out, has := p.Field("PortAes67Output"); has {
					for j, m := range out.Members {
						if m.Name == "Multicast" {
							out.Members[j].Value = codec.String(multicast)
						}
					}
				}
			}
			return v, ok
		}
		return rrcsTreeAnswer(call)
	})
	return f, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), writes...)
	}
}

const rrcsEnsureDesired = `{
  "values": {
    "net.1.node.61.port.7.out.PortAes67Output.Multicast": "239.9.9.9",
    "net.1.node.61.port.7.out.PortAes67Output.MulticastPort": 5004,
    "net.1.node.61.port.7.out.PortAes67Output.Protocol": "Manual"
  },
  "crosspoints": [
    {"source": "net.1.node.61.port.7.in", "destination": "net.1.node.61.port.1026", "state": "present"}
  ]
}`

// The contract of every dhs ensure (ADR-0007): --check reports and sends
// nothing, the first run changes, the second run changes nothing.
func TestRRCSEnsureContract(t *testing.T) {
	f, writes := rrcsEnsureFake(t)
	file := filepath.Join(t.TempDir(), "desired.json")
	_ = os.WriteFile(file, []byte(rrcsEnsureDesired), 0o644)
	field := "net.1.node.61.port.7.out.PortAes67Output.Multicast"
	xpField := "xp.net.1.node.61.port.7.in>net.1.node.61.port.1026"

	var dry struct {
		WouldChange bool `json:"would_change"`
		Current     map[string]any
		Target      map[string]any
		Diff        []rrcsDiffEntry
		Failed      []rrcsEnsureFailure
	}
	out := rrcsRun(t, "ensure", f.addr(), "--file", file, "--check", "--output", "json")
	if err := json.Unmarshal([]byte(out), &dry); err != nil {
		t.Fatalf("check output: %v\n%s", err, out)
	}
	if !dry.WouldChange || len(dry.Diff) != 2 || dry.Current[field] != "239.1.2.3" || dry.Target[field] != "239.9.9.9" ||
		dry.Current[xpField] != "absent" || dry.Target[xpField] != "present" || len(dry.Failed) != 0 {
		t.Errorf("check: %s", out)
	}
	if len(writes()) != 0 {
		t.Fatalf("--check wrote: %v", writes())
	}

	var run struct {
		Changed  bool
		Previous map[string]any
		Current  map[string]any
		Diff     []rrcsDiffEntry
		Failed   []rrcsEnsureFailure
	}
	out = rrcsRun(t, "ensure", f.addr(), "--file", file, "--output", "json", "--write-to", f.addr())
	if err := json.Unmarshal([]byte(out), &run); err != nil {
		t.Fatalf("apply output: %v\n%s", err, out)
	}
	if !run.Changed || len(run.Diff) != 2 || run.Previous[field] != "239.1.2.3" || run.Current[field] != "239.9.9.9" || len(run.Failed) != 0 {
		t.Errorf("first run: %s", out)
	}
	if got := strings.Join(writes(), ","); got != "ConfigurationChangeEx,SetXp" {
		t.Errorf("writes %s", got)
	}

	// Run twice: no change, an empty diff that is [] and not null.
	out = rrcsRun(t, "ensure", f.addr(), "--file", file, "--output", "json", "--write-to", f.addr())
	if !strings.Contains(out, `"changed":false`) || !strings.Contains(out, `"diff":[]`) || !strings.Contains(out, `"failed":[]`) {
		t.Errorf("second run: %s", out)
	}
	if len(writes()) != 2 {
		t.Errorf("the second run wrote again: %v", writes())
	}
	out = rrcsRun(t, "ensure", f.addr(), "--file", file, "--check", "--diff", "--output", "json")
	if !strings.Contains(out, `"would_change":false`) || !strings.Contains(out, `"diff":[]`) {
		t.Errorf("check after converge: %s", out)
	}

	// absent removes the crosspoint; text output.
	_ = os.WriteFile(file, []byte(`{"crosspoints":[{"source":"net.1.node.61.port.7.in","destination":"net.1.node.61.port.1026","state":"absent"}]}`), 0o644)
	rrcsWant(t, rrcsRun(t, "ensure", f.addr(), "--file", file, "--write-to", f.addr()), "changed       "+xpField, "present -> absent", "changed 1, failed 0")
	if w := writes(); w[len(w)-1] != "KillXp" {
		t.Errorf("writes %v", w)
	}
}

func TestRRCSEnsureFailuresAndRefusals(t *testing.T) {
	f, writes := rrcsEnsureFake(t)
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		_ = os.WriteFile(p, []byte(body), 0o644)
		return p
	}
	ctx := context.Background()

	// A read-only value and a value that does not exist are failures of
	// the request: exit 1, and the output says which.
	bad := write("bad.json", `{"values":{"net.1.node.61.port.1026.LongName":"X","net.1.node.61.port.999.PortAes67Output.Multicast":"239.1.1.1"}}`)
	out, err := rrcsStdout(t, func() error { return runRRCS(ctx, []string{"ensure", f.addr(), "--file", bad, "--check", "--output", "json"}) })
	var val *consumer.ValidationError
	if err == nil || errors.As(err, &val) || !strings.Contains(out, `"reason":"read_only"`) || !strings.Contains(out, `not_on_device`) {
		t.Errorf("%v\n%s", err, out)
	}

	for name, args := range map[string][]string{
		"no file":       {"ensure", f.addr()},
		"no host":       {"ensure", "--file", bad},
		"no guard":      {"ensure", f.addr(), "--file", write("ok.json", rrcsEnsureDesired)},
		"unknown key":   {"ensure", f.addr(), "--file", write("k.json", `{"conferences":[]}`), "--check"},
		"not json":      {"ensure", f.addr(), "--file", write("n.json", `{`), "--check"},
		"value object":  {"ensure", f.addr(), "--file", write("o.json", `{"values":{"a.B":{"x":1}}}`), "--check"},
		"xp bad path":   {"ensure", f.addr(), "--file", write("x.json", `{"crosspoints":[{"source":"group.1","destination":"net.1.node.61.port.7.out"}]}`), "--check"},
		"xp bad state":  {"ensure", f.addr(), "--file", write("s.json", `{"crosspoints":[{"source":"net.1.node.61.port.7.in","destination":"net.1.node.61.port.7.out","state":"maybe"}]}`), "--check"},
		"guard another": {"ensure", f.addr(), "--file", write("ok2.json", rrcsEnsureDesired), "--write-to", "10.0.0.1"},
	} {
		if err := runRRCS(ctx, args); !errors.As(err, &val) {
			t.Errorf("%s: got %v, want a validation error", name, err)
		}
	}
	if err := runRRCS(ctx, []string{"ensure", f.addr(), "--file", filepath.Join(dir, "missing.json"), "--check"}); err == nil || errors.As(err, &val) {
		t.Errorf("missing file: %v", err)
	}
	if len(writes()) != 0 {
		t.Errorf("a refused ensure wrote: %v", writes())
	}
}
