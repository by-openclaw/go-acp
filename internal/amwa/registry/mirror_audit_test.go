package registry

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAuditorWritesJSONLAndRing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a, err := newAuditor(path)
	if err != nil {
		t.Fatalf("newAuditor: %v", err)
	}
	a.event("mirror_start", map[string]any{"source": "s"})
	a.event("forward_failed", map[string]any{"op": "post", "err": "HTTP 400"})
	a.close()

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var kinds []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ev AuditEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("line not JSON: %v (%s)", err, sc.Text())
		}
		if ev.TS == "" {
			t.Error("event without timestamp")
		}
		kinds = append(kinds, ev.Kind)
	}
	if len(kinds) != 2 || kinds[0] != "mirror_start" || kinds[1] != "forward_failed" {
		t.Errorf("kinds = %v", kinds)
	}

	if got := a.recent(); len(got) != 2 || got[1].Detail["err"] != "HTTP 400" {
		t.Errorf("ring = %+v", got)
	}
}

func TestAuditorNilSafe(t *testing.T) {
	var a *auditor
	a.event("anything", nil) // must not panic — mirrors run without audit too
	if a.recent() != nil {
		t.Error("nil auditor must report nothing")
	}
}

func TestAuditorRingBounded(t *testing.T) {
	a, _ := newAuditor("")
	for i := 0; i < auditRingSize*2; i++ {
		a.event("k", nil)
	}
	if len(a.recent()) != auditRingSize {
		t.Errorf("ring = %d, want %d", len(a.recent()), auditRingSize)
	}
}
