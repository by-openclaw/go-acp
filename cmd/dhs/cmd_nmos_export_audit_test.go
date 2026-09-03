package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dhs/internal/amwa/audit"
	"dhs/internal/amwa/session/export"
)

// The export verb writes a layout; the audit verb reads it. Nothing in
// either package forces the two to agree — a rename on one side would
// silently produce empty audits. This test is that force: it captures a
// fake plant built with known defects and asserts the audit finds them.
//
// It also proves the pair end-to-end without hardware, which is what
// makes the workflow runnable in CI.

// fakeDevice serves a scripted NMOS surface over httptest.
type fakeDevice struct {
	paths map[string]any
	pages map[string][][]any
}

func newFake() *fakeDevice {
	return &fakeDevice{paths: map[string]any{}, pages: map[string][][]any{}}
}

func (f *fakeDevice) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if chunks, ok := f.pages[r.URL.Path]; ok {
		idx := 0
		if s := r.URL.Query().Get("paging.until"); s != "" {
			_, _ = fmt.Sscanf(s, "%d", &idx)
		}
		if idx >= len(chunks) {
			idx = len(chunks) - 1
		}
		if idx < len(chunks)-1 {
			w.Header().Set("Link", fmt.Sprintf(`<%s?paging.until=%d>; rel="prev"`, r.URL.Path, idx+1))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(chunks[idx])
		return
	}
	body, ok := f.paths[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	// A sender that 502s on its transport file is a real, observed
	// device behaviour, and one the audit must report as a fault rather
	// than as an absent endpoint.
	if s, isStr := body.(string); isStr && s == "502" {
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func rsrc(id, label string, over map[string]any) map[string]any {
	m := map[string]any{"id": id, "version": "1755000000:0", "label": label, "tags": map[string]any{}}
	for k, v := range over {
		m[k] = v
	}
	return m
}

const (
	nodeAID   = "11111111-1111-4111-8111-111111111111"
	nodeBID   = "22222222-2222-4222-8222-222222222222"
	ghostID   = "33333333-3333-4333-8333-333333333333"
	devAID    = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	senderAID = "44444444-4444-4444-8444-444444444444"
	senderBID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

// buildNode serves a node whose sender is master-enabled and emitting
// to the group passed in — the same group for both nodes, so the plant
// has a collision no single device can see.
func buildNode(t *testing.T, id, label, senderID, group string) *httptest.Server {
	t.Helper()
	f := newFake()
	f.paths["/x-nmos"] = []string{"node/", "connection/"}
	f.paths["/x-nmos/node/"] = []string{"v1.3/"}
	f.paths["/x-nmos/node/v1.3/self"] = rsrc(id, label, nil)
	f.paths["/x-nmos/node/v1.3/devices"] = []any{
		rsrc(devAID, label+" device", map[string]any{
			"type": "urn:x-nmos:device:generic", "node_id": id,
			"senders": []any{}, "receivers": []any{},
			// No sr-ctrl control, though the Connection API is served:
			// a controller walking controls[] never finds IS-05 here.
			"controls": []any{},
		}),
	}
	for _, r := range []string{"sources", "flows", "receivers"} {
		f.paths["/x-nmos/node/v1.3/"+r] = []any{}
	}
	f.paths["/x-nmos/node/v1.3/senders"] = []any{
		rsrc(senderID, label+" out", map[string]any{
			"transport": "urn:x-nmos:transport:rtp.mcast", "device_id": devAID,
			// A flow that is not published: the controller drops the
			// whole branch.
			"flow_id":            "99999999-9999-4999-8999-999999999999",
			"manifest_href":      "/x-nmos/connection/v1.1/single/senders/" + senderID + "/transportfile",
			"interface_bindings": []string{"eth0", "eth1"},
			"subscription":       map[string]any{"receiver_id": nil, "active": true},
		}),
	}
	f.paths["/x-nmos/connection/"] = []string{"v1.1/"}
	f.paths["/x-nmos/connection/v1.1/single/senders"] = []string{senderID + "/"}
	f.paths["/x-nmos/connection/v1.1/single/receivers"] = []any{}
	f.paths["/x-nmos/connection/v1.1/bulk"] = []any{}
	f.paths["/x-nmos/connection/v1.1/single/senders/"+senderID+"/active"] = map[string]any{
		"master_enable": true,
		"transport_params": []any{
			map[string]any{"destination_ip": group, "destination_port": 20000, "rtp_enabled": true},
		},
	}
	// The transport file is broken.
	f.paths["/x-nmos/connection/v1.1/single/senders/"+senderID+"/transportfile"] = "502"

	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return srv
}

func TestExportThenAuditFindsPlantedDefects(t *testing.T) {
	nodeA := buildNode(t, nodeAID, "cam-01", senderAID, "233.252.0.20")
	nodeB := buildNode(t, nodeBID, "mv-01", senderBID, "233.252.0.20")

	// A node that registered and then died. Starting a server and
	// closing it gives an address that refuses connections immediately,
	// which is what a dead device on a reachable network looks like —
	// and keeps the test fast, unlike an address that black-holes.
	dead := httptest.NewServer(newFake())
	deadURL := dead.URL
	dead.Close()

	reg := newFake()
	reg.paths["/x-nmos"] = []string{"query/", "registration/"}
	reg.paths["/x-nmos/query/"] = []string{"v1.1/", "v1.3/"}
	reg.paths["/x-nmos/registration/"] = []string{"v1.3/"}
	reg.paths["/x-nmos/registration/v1.3/"] = []string{"health/", "resource/"}
	for _, r := range []string{"devices", "sources", "flows", "senders", "receivers", "subscriptions"} {
		reg.paths["/x-nmos/query/v1.1/"+r] = []any{}
		reg.paths["/x-nmos/query/v1.3/"+r] = []any{}
	}
	// v1.1 lists all three, across two pages. One of them answers
	// nowhere, and v1.3 hides two of them.
	reg.pages["/x-nmos/query/v1.1/nodes"] = [][]any{
		{
			rsrc(nodeAID, "cam-01", map[string]any{"href": nodeA.URL + "/"}),
			rsrc(nodeBID, "mv-01", map[string]any{"href": nodeB.URL + "/"}),
		},
		{rsrc(ghostID, "legacy-gw", map[string]any{"href": deadURL + "/"})},
	}
	reg.paths["/x-nmos/query/v1.3/nodes"] = []any{
		rsrc(nodeAID, "cam-01", map[string]any{"href": nodeA.URL + "/"}),
	}
	regSrv := httptest.NewServer(reg)
	defer regSrv.Close()

	dir := t.TempDir()
	res, err := export.Run(context.Background(), export.Options{
		Target:  strings.TrimPrefix(regSrv.URL, "http://"),
		Out:     dir,
		Timeout: 2 * time.Second,
		NoStamp: true,
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	// Paging is what makes this three rather than two.
	if res.NodesSeen != 3 {
		t.Fatalf("exported %d nodes, want 3", res.NodesSeen)
	}

	harvests, err := audit.Load(dir)
	if err != nil {
		t.Fatalf("audit could not read what export wrote: %v", err)
	}
	got := audit.Run(harvests, audit.Options{})

	seen := map[string]int{}
	for _, f := range got.Findings {
		seen[f.Code]++
	}

	// Each of these is a defect the fake plant was deliberately built
	// with. A miss means export and audit have drifted apart.
	for _, want := range []string{
		"NMOS-PLANT-MCAST-COLLISION",   // both nodes on 233.252.0.20:20000
		"NMOS-QUERY-VERSION-ISOLATION", // 3 nodes at v1.1, 1 at v1.3
		"NMOS-NODE-UNREACHABLE",        // legacy-gw answers nowhere
		"NMOS-HTTP-SERVER-FAULT",       // 502 on the transport files
		"NMOS-IS04-DANGLING-REF",       // sender points at an unpublished flow
		"NMOS-IS04-CONTROL-MISSING",    // IS-05 served, no sr-ctrl control
		"NMOS-2022-7-SINGLE-LEG",       // two NICs bound, one leg staged
	} {
		if seen[want] == 0 {
			t.Errorf("audit did not report %s\n  reported: %v", want, seen)
		}
	}

	// The plant is a registry plus the two nodes it could reach.
	if len(got.Inventory) != 3 {
		t.Errorf("inventory has %d devices, want 3", len(got.Inventory))
	}
	if worst, any := got.Worst(); !any || worst != audit.SevCritical {
		t.Errorf("worst = %s,%v; this plant has critical defects", worst, any)
	}
}

// TestExportVerbRejectsBadArguments covers the CLI wrapper's own rules.
func TestExportVerbRejectsBadArguments(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"no target", nil},
		{"target twice", []string{"h:1", "--target", "h:2"}},
		{"extra positional", []string{"--target", "h:1", "--", "extra"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := runNMOSExport(ctx, tc.args); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

// TestAuditVerbRejectsBadArguments does the same for the audit wrapper,
// including the flag-after-positional trap.
func TestAuditVerbRejectsBadArguments(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"no dir", nil},
		{"dir twice", []string{"a", "--dir", "b"}},
		{"bad severity", []string{"--dir", t.TempDir(), "--min-severity", "loud"}},
		{"bad fail-on", []string{"--dir", t.TempDir(), "--fail-on", "loud"}},
		{"missing dir", []string{"--dir", "no-such-directory-anywhere"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := runNMOSAudit(ctx, tc.args); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

// TestAuditVerbFormats proves each output format is wired, and that
// --fail-on gates while a plain run does not.
func TestAuditVerbFormats(t *testing.T) {
	ctx := context.Background()
	dir := "../../internal/amwa/audit/testdata/plant"
	out := t.TempDir()

	for _, format := range []string{"text", "json", "jsonl"} {
		if err := runNMOSAudit(ctx, []string{"--dir", dir, "--format", format, "--out", out + "/r." + format}); err != nil {
			t.Errorf("--format %s: %v", format, err)
		}
	}
	if err := runNMOSAudit(ctx, []string{"--dir", dir, "--format", "yaml", "--out", out + "/x"}); err == nil {
		t.Error("an unknown format should be rejected")
	}

	// Reporting is not failing: without --fail-on a plant with critical
	// defects still exits cleanly.
	if err := runNMOSAudit(ctx, []string{"--dir", dir, "--out", out + "/plain"}); err != nil {
		t.Errorf("a plain audit should not fail: %v", err)
	}
	if err := runNMOSAudit(ctx, []string{"--dir", dir, "--fail-on", "critical", "--out", out + "/gated"}); err == nil {
		t.Error("--fail-on critical should have failed on this plant")
	}
	// Filtering the report does not change the verdict: --min-severity
	// decides what is printed, --fail-on decides whether it fails.
	if err := runNMOSAudit(ctx, []string{"--dir", dir, "--min-severity", "critical", "--fail-on", "critical", "--out", out + "/crit"}); err == nil {
		t.Error("the gate should fire on a plant with criticals")
	}
}
