package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ADR-0037 P4: the JSON of get names the schema and the device model of
// the point; the manifest extract wrote completes model and template.
func TestRRCSGenericGetDescribes(t *testing.T) {
	if treeStore == nil {
		t.Skip("no tree store in this test binary")
	}
	f, _ := rrcsSetFake(t, true)
	host, _, _ := strings.Cut(f.addr(), ":")
	dir := filepath.Join(treeStore.BaseDir(), "manifest", "rrcs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mf := `{"device":{"name":"test","protocol":"rrcs","ip":"` + host + `","endpoints":[{"ip":"` + host + `","port":8193,"transport":"tcp"}]},
"frames":[{"name":"net.1.node.60","slots":[
 {"addr":{"bay":0,"node":60,"type_name":"ARTIST_1024"},"dm":"ARTIST_1024@9.0.1"},
 {"addr":{"bay":1,"card_node":60,"node":61,"type_name":"AES67"},"dm":"AES67@9.0.1"},
 {"addr":{"bay":1,"node":61,"port":7,"template":"FOUR_WIRE","type":2},"dm":"AES67@9.0.1"}]}]}`
	path := filepath.Join(dir, host+".json")
	if err := os.WriteFile(path, []byte(mf), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	out, err := rrcsStdout(t, func() error {
		return runGet(context.Background(), []string{f.addr(), "--protocol", "rrcs", "--path", "net.1.node.61.port.7.out.PortAes67Output.Channels", "--output", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"meta":{"kind":"int","access":"RW-","min":1,"max":16`, `"dm":{"model":"AES67","sw_rev":"9.0.1","template":"FOUR_WIRE","path":"out.PortAes67Output.Channels"}`} {
		if !strings.Contains(out, want) {
			t.Errorf("get --output json lacks %s:\n%s", want, out)
		}
	}
	// Text mode prints the same, for a path (not only a label).
	out, err = rrcsStdout(t, func() error {
		return runGet(context.Background(), []string{f.addr(), "--protocol", "rrcs", "--path", "net.1.node.61.port.7.out.PortAes67Output.Channels"})
	})
	if err != nil || !strings.Contains(out, "range = 1 .. 16") || !strings.Contains(out, "dm = AES67@9.0.1  template = FOUR_WIRE") {
		t.Errorf("text meta: %v\n%s", err, out)
	}
}
