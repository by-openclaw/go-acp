package export

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixedNow keeps folder names deterministic so a test can assert on the
// path it expects.
func fixedNow() time.Time { return time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC) }

func baseOpts(t *testing.T, target string) Options {
	t.Helper()
	return Options{
		Target:  target,
		Out:     t.TempDir(),
		Timeout: 2 * time.Second,
		Now:     fixedNow,
		NoStamp: true,
	}
}

// plant serves a scripted NMOS surface. Paths not in the map 404, which
// is what a device does for an API it does not have.
type plant struct {
	t     *testing.T
	paths map[string]any
	// pages lets one path answer with a Link: rel="prev" chain — the
	// OLDER direction, which is how a collection is enumerated.
	pages map[string][][]any
	// hits counts requests per path, so a test can prove the exporter
	// did not walk something four times.
	hits map[string]int
	// stuck makes the paging cursor repeat forever.
	stuck bool
}

func newPlant(t *testing.T) *plant {
	return &plant{t: t, paths: map[string]any{}, pages: map[string][][]any{}, hits: map[string]int{}}
}

func (p *plant) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.hits[r.URL.Path]++

	if chunks, ok := p.pages[r.URL.Path]; ok {
		idx := 0
		if s := r.URL.Query().Get("paging.until"); s != "" {
			_, _ = fmt.Sscanf(s, "%d", &idx)
		}
		if idx >= len(chunks) {
			idx = len(chunks) - 1
		}
		if idx < len(chunks)-1 || p.stuck {
			prev := idx + 1
			if p.stuck {
				prev = idx
			}
			w.Header().Set("Link", fmt.Sprintf(`<%s?paging.until=%d>; rel="prev"`, r.URL.Path, prev))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(chunks[idx])
		return
	}

	body, ok := p.paths[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if s, isStr := body.(string); isStr {
		w.Header().Set("Content-Type", "application/sdp")
		_, _ = w.Write([]byte(s))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func res(id, label string, over map[string]any) map[string]any {
	m := map[string]any{"id": id, "version": "1755000000:0", "label": label, "tags": map[string]any{}}
	for k, v := range over {
		m[k] = v
	}
	return m
}

// readTree loads the tree.json a capture produced.
func readTree(t *testing.T, dir string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "tree.json"))
	if err != nil {
		t.Fatalf("tree.json: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("tree.json: %v", err)
	}
	return m
}

func readReport(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "report.txt"))
	if err != nil {
		t.Fatalf("report.txt: %v", err)
	}
	return string(b)
}

// TestExportNode captures a node and pins the whole on-disk shape.
func TestExportNode(t *testing.T) {
	p := newPlant(t)
	p.paths["/x-nmos"] = []string{"node/", "connection/"}
	p.paths["/x-nmos/node/"] = []string{"v1.2/", "v1.3/"}
	p.paths["/x-nmos/node/v1.3/self"] = res("11111111-1111-4111-8111-111111111111", "cam-01", nil)
	p.paths["/x-nmos/node/v1.3/devices"] = []any{}
	p.paths["/x-nmos/node/v1.3/sources"] = []any{}
	p.paths["/x-nmos/node/v1.3/flows"] = []any{}
	p.paths["/x-nmos/node/v1.3/receivers"] = []any{}
	p.paths["/x-nmos/node/v1.3/senders"] = []any{
		res("44444444-4444-4444-8444-444444444444", "prog", map[string]any{
			// The manifest lives where the device says it lives — here,
			// under the node API, not at an IS-05 transportfile path.
			"manifest_href": "/x-nmos/node/v1.3/sdp/44444444-4444-4444-8444-444444444444",
		}),
		res("55555555-5555-4555-8555-555555555555", "iso", map[string]any{"manifest_href": nil}),
	}
	p.paths["/x-nmos/node/v1.3/sdp/44444444-4444-4444-8444-444444444444"] =
		"v=0\r\no=- 1 1 IN IP4 198.51.100.21\r\ns=cam-01 prog\r\n"

	srv := httptest.NewServer(p)
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	got, err := Run(context.Background(), baseOpts(t, host))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got.Role != "node" {
		t.Errorf("role = %q, want node", got.Role)
	}
	if got.Label != "cam-01" {
		t.Errorf("label = %q, want cam-01", got.Label)
	}
	if got.ID != "11111111-1111-4111-8111-111111111111" {
		t.Errorf("id = %q", got.ID)
	}

	// A node repeats identical resources at every minor. Walking both
	// v1.2 and v1.3 would double the requests for the same bytes.
	if p.hits["/x-nmos/node/v1.2/senders"] != 0 {
		t.Error("the lower minor was walked; only the highest should be")
	}
	if p.hits["/x-nmos/node/v1.3/senders"] != 1 {
		t.Errorf("senders fetched %d times, want 1", p.hits["/x-nmos/node/v1.3/senders"])
	}

	tree := readTree(t, got.Dir)
	apis, _ := tree["apis"].(map[string]any)
	node, _ := apis["node"].(map[string]any)
	data, _ := node["data"].(map[string]any)
	if _, ok := data["v1.3"]; !ok {
		t.Errorf("tree has no v1.3 bucket: %v", data)
	}
	if _, ok := data["v1.2"]; ok {
		t.Error("tree carries a v1.2 bucket that was never walked")
	}

	// manifest_href here is a relative URI, which IS-04 permits. It has
	// to resolve against the device's own base, or the SDP is lost.
	if got.SDPFiles != 1 {
		t.Errorf("SDP files = %d, want 1 (relative manifest_href must resolve)", got.SDPFiles)
	}

	rep := readReport(t, got.Dir)
	if !strings.Contains(rep, "NOSDP sender 55555555-5555-4555-8555-555555555555") {
		t.Errorf("a null manifest_href should be recorded, not silently skipped:\n%s", rep)
	}
	if !strings.Contains(rep, "NOTE  node : versions v1.2,v1.3 present, walking v1.3 only") {
		t.Errorf("the version decision should be recorded:\n%s", rep)
	}

	// device.json must be attributable even though it is written twice.
	b, err := os.ReadFile(filepath.Join(got.Dir, "device.json"))
	if err != nil {
		t.Fatal(err)
	}
	var dev map[string]any
	if err := json.Unmarshal(b, &dev); err != nil {
		t.Fatal(err)
	}
	if dev["role"] != "node" || dev["label"] != "cam-01" {
		t.Errorf("device.json = %v", dev)
	}
}

// TestManifestHrefFollowedVerbatim is the behaviour that produced 1088
// errors when it was got wrong: the SDP lives where the sender says,
// not at a path the exporter constructs.
func TestManifestHrefFollowedVerbatim(t *testing.T) {
	p := newPlant(t)
	p.paths["/x-nmos"] = []string{"node/"}
	p.paths["/x-nmos/node/"] = []string{"v1.3/"}
	p.paths["/x-nmos/node/v1.3/self"] = res("11111111-1111-4111-8111-111111111111", "n", nil)
	for _, r := range []string{"devices", "sources", "flows", "receivers"} {
		p.paths["/x-nmos/node/v1.3/"+r] = []any{}
	}

	srv := httptest.NewServer(p)
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	// A vendor-specific manifest location, absolute.
	p.paths["/x-nmos/node/v1.3/senders"] = []any{
		res("44444444-4444-4444-8444-444444444444", "s", map[string]any{
			"manifest_href": srv.URL + "/vendor/private/sdp/1",
		}),
	}
	p.paths["/vendor/private/sdp/1"] = "v=0\r\ns=vendor\r\n"

	got, err := Run(context.Background(), baseOpts(t, host))
	if err != nil {
		t.Fatal(err)
	}
	if p.hits["/vendor/private/sdp/1"] != 1 {
		t.Error("the vendor manifest_href was not followed")
	}
	if got.SDPFiles != 1 {
		t.Errorf("SDP files = %d, want 1", got.SDPFiles)
	}
	sdp, err := os.ReadFile(filepath.Join(got.Dir, "sdp", "is04", "44444444-4444-4444-8444-444444444444.sdp"))
	if err != nil {
		t.Fatalf("SDP not written where the audit looks for it: %v", err)
	}
	if !strings.HasPrefix(string(sdp), "v=0") {
		t.Errorf("SDP body = %q", sdp)
	}
}

// registryPlant builds a registry serving two minors, paged, with the
// lower minor listing a node the higher one hides.
func registryPlant(t *testing.T) *plant {
	p := newPlant(t)
	p.paths["/x-nmos"] = []string{"query/", "registration/"}
	p.paths["/x-nmos/query/"] = []string{"v1.1/", "v1.3/"}
	p.paths["/x-nmos/registration/"] = []string{"v1.3/"}
	p.paths["/x-nmos/registration/v1.3/"] = []string{"health/", "resource/"}
	for _, r := range []string{"devices", "sources", "flows", "senders", "receivers", "subscriptions"} {
		p.paths["/x-nmos/query/v1.1/"+r] = []any{}
		p.paths["/x-nmos/query/v1.3/"+r] = []any{}
	}
	return p
}

// TestExportRegistryPagesAndVersions covers the two behaviours that
// decide whether an export describes the whole plant or part of it.
func TestExportRegistryPagesAndVersions(t *testing.T) {
	p := registryPlant(t)
	// v1.1 serves three nodes across two pages.
	p.pages["/x-nmos/query/v1.1/nodes"] = [][]any{
		{res("11111111-1111-4111-8111-111111111111", "a", nil), res("22222222-2222-4222-8222-222222222222", "b", nil)},
		{res("33333333-3333-4333-8333-333333333333", "c", nil)},
	}
	// v1.3 hides two of them — IS-04 version isolation.
	p.paths["/x-nmos/query/v1.3/nodes"] = []any{res("11111111-1111-4111-8111-111111111111", "a", nil)}

	srv := httptest.NewServer(p)
	defer srv.Close()

	opts := baseOpts(t, strings.TrimPrefix(srv.URL, "http://"))
	opts.MaxNodes = -1 // no href on these nodes; nothing to follow
	got, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}

	if got.Role != "registry" {
		t.Errorf("role = %q, want registry", got.Role)
	}
	// Paging: without Link following this is 2, not 3.
	if got.NodesSeen != 3 {
		t.Errorf("nodes seen = %d, want 3 (2 pages at v1.1 union 1 at v1.3)", got.NodesSeen)
	}
	// Both minors must be walked. Collapsing to the highest reports 1.
	if p.hits["/x-nmos/query/v1.1/nodes"] == 0 {
		t.Error("the lower query minor was not walked — version isolation would hide nodes")
	}
	if p.hits["/x-nmos/query/v1.3/nodes"] == 0 {
		t.Error("the higher query minor was not walked")
	}

	rep := readReport(t, got.Dir)
	for _, want := range []string{
		"VERSION v1.1 nodes: 3 listed, 3 new",
		"VERSION v1.3 nodes: 1 listed, 0 new",
		"(page 1, +2, total 2)",
		"(page 2, +1, total 3)",
	} {
		if !strings.Contains(rep, want) {
			t.Errorf("report is missing %q:\n%s", want, rep)
		}
	}

	tree := readTree(t, got.Dir)
	apis := tree["apis"].(map[string]any)
	q := apis["query"].(map[string]any)
	data := q["data"].(map[string]any)
	if len(data) != 2 {
		t.Errorf("query captured at %d minors, want 2", len(data))
	}
	nodes := data["v1.1"].(map[string]any)["nodes"].([]any)
	if len(nodes) != 3 {
		t.Errorf("v1.1 nodes captured = %d, want all 3 pages merged", len(nodes))
	}
}

// TestStuckPagingCursorStops proves a registry defect ends the walk and
// is recorded, rather than hanging the capture.
func TestStuckPagingCursorStops(t *testing.T) {
	p := registryPlant(t)
	p.stuck = true
	p.pages["/x-nmos/query/v1.1/nodes"] = [][]any{
		{res("11111111-1111-4111-8111-111111111111", "a", nil)},
	}
	p.paths["/x-nmos/query/v1.3/nodes"] = []any{}

	srv := httptest.NewServer(p)
	defer srv.Close()

	done := make(chan *Result, 1)
	go func() {
		opts := baseOpts(t, strings.TrimPrefix(srv.URL, "http://"))
		r, err := Run(context.Background(), opts)
		if err != nil {
			t.Error(err)
		}
		done <- r
	}()

	select {
	case got := <-done:
		rep := readReport(t, got.Dir)
		if !strings.Contains(rep, "paging cursor did not advance") {
			t.Errorf("the stuck cursor was not recorded:\n%s", rep)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a stuck paging cursor hung the capture")
	}
}

// TestFollowRegisteredNodes proves a registry export is a plant export.
func TestFollowRegisteredNodes(t *testing.T) {
	nodeP := newPlant(t)
	nodeP.paths["/x-nmos"] = []string{"node/"}
	nodeP.paths["/x-nmos/node/"] = []string{"v1.3/"}
	nodeP.paths["/x-nmos/node/v1.3/self"] = res("11111111-1111-4111-8111-111111111111", "cam-01", nil)
	for _, r := range []string{"devices", "sources", "flows", "senders", "receivers"} {
		nodeP.paths["/x-nmos/node/v1.3/"+r] = []any{}
	}
	nodeSrv := httptest.NewServer(nodeP)
	defer nodeSrv.Close()

	p := registryPlant(t)
	p.paths["/x-nmos/query/v1.1/nodes"] = []any{
		res("11111111-1111-4111-8111-111111111111", "cam-01", map[string]any{"href": nodeSrv.URL + "/"}),
	}
	p.paths["/x-nmos/query/v1.3/nodes"] = []any{}
	regSrv := httptest.NewServer(p)
	defer regSrv.Close()

	got, err := Run(context.Background(), baseOpts(t, strings.TrimPrefix(regSrv.URL, "http://")))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Followed) != 1 {
		t.Fatalf("followed %d nodes, want 1", len(got.Followed))
	}
	if got.Followed[0].Label != "cam-01" {
		t.Errorf("followed node label = %q", got.Followed[0].Label)
	}
	// The node folder must nest under the registry's, or the audit
	// loads it as an unrelated second plant.
	want := filepath.Join(got.Dir, "nodes")
	if !strings.HasPrefix(got.Followed[0].Dir, want) {
		t.Errorf("node captured at %q, want under %q", got.Followed[0].Dir, want)
	}
}

// TestMaxNodesRecordsWhatItSkipped: a coverage cap that is not recorded
// makes a partial audit look complete.
func TestMaxNodesRecordsWhatItSkipped(t *testing.T) {
	nodeP := newPlant(t)
	nodeP.paths["/x-nmos"] = []string{"node/"}
	nodeP.paths["/x-nmos/node/"] = []string{"v1.3/"}
	nodeP.paths["/x-nmos/node/v1.3/self"] = res("11111111-1111-4111-8111-111111111111", "n1", nil)
	for _, r := range []string{"devices", "sources", "flows", "senders", "receivers"} {
		nodeP.paths["/x-nmos/node/v1.3/"+r] = []any{}
	}
	nodeSrv := httptest.NewServer(nodeP)
	defer nodeSrv.Close()

	p := registryPlant(t)
	p.paths["/x-nmos/query/v1.1/nodes"] = []any{
		res("11111111-1111-4111-8111-111111111111", "n1", map[string]any{"href": nodeSrv.URL + "/"}),
		res("22222222-2222-4222-8222-222222222222", "n2", map[string]any{"href": nodeSrv.URL + "/"}),
		res("33333333-3333-4333-8333-333333333333", "n3", map[string]any{"href": "http://198.51.100.99:3212/"}),
	}
	p.paths["/x-nmos/query/v1.3/nodes"] = []any{}
	regSrv := httptest.NewServer(p)
	defer regSrv.Close()

	opts := baseOpts(t, strings.TrimPrefix(regSrv.URL, "http://"))
	opts.MaxNodes = 1
	got, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	rep := readReport(t, got.Dir)
	if !strings.Contains(rep, "CAPPED 2 node(s) not followed") {
		t.Errorf("the cap was not recorded:\n%s", rep)
	}
	if !strings.Contains(rep, "SUMMARY 3 listed / 1 followed / 2 capped") {
		t.Errorf("summary missing or wrong:\n%s", rep)
	}
}

// TestNodeWithoutHrefRecorded: a registered node with no usable href
// cannot be followed, and that is a finding, not a silent omission.
func TestNodeWithoutHrefRecorded(t *testing.T) {
	p := registryPlant(t)
	p.paths["/x-nmos/query/v1.1/nodes"] = []any{res("11111111-1111-4111-8111-111111111111", "ghost", nil)}
	p.paths["/x-nmos/query/v1.3/nodes"] = []any{}
	srv := httptest.NewServer(p)
	defer srv.Close()

	got, err := Run(context.Background(), baseOpts(t, strings.TrimPrefix(srv.URL, "http://")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readReport(t, got.Dir), "SKIP  node 11111111-1111-4111-8111-111111111111 'ghost'") {
		t.Errorf("a node with no href should be recorded as skipped:\n%s", readReport(t, got.Dir))
	}
}

// TestConnectionDeepAndTransportTypeGating covers the IS-05 walk,
// including the version rule that avoided 5,951 live 404s.
func TestConnectionDeepAndTransportTypeGating(t *testing.T) {
	id := "44444444-4444-4444-8444-444444444444"
	p := newPlant(t)
	p.paths["/x-nmos"] = []string{"connection/"}
	p.paths["/x-nmos/connection/"] = []string{"v1.0/"}
	p.paths["/x-nmos/connection/v1.0/single/senders"] = []string{id + "/"}
	p.paths["/x-nmos/connection/v1.0/single/receivers"] = []any{}
	p.paths["/x-nmos/connection/v1.0/bulk"] = []any{}
	for _, sub := range []string{"staged", "active", "constraints"} {
		p.paths["/x-nmos/connection/v1.0/single/senders/"+id+"/"+sub] = map[string]any{"master_enable": true}
	}
	srv := httptest.NewServer(p)
	defer srv.Close()

	opts := baseOpts(t, strings.TrimPrefix(srv.URL, "http://"))
	opts.Deep = true
	opts.NoSDP = true
	if _, err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}

	// transporttype arrived in IS-05 v1.1. Requesting it here would be
	// a guaranteed 404 for every endpoint.
	if p.hits["/x-nmos/connection/v1.0/single/senders/"+id+"/transporttype"] != 0 {
		t.Error("transporttype was requested on a v1.0 Connection API")
	}
	for _, sub := range []string{"staged", "active", "constraints"} {
		if p.hits["/x-nmos/connection/v1.0/single/senders/"+id+"/"+sub] != 1 {
			t.Errorf("--deep did not fetch %s", sub)
		}
	}
}

// TestEmbeddedReceiverSDP: a receiver's SDP is pushed into it by a
// controller and lives in transport_file.data.
func TestEmbeddedReceiverSDP(t *testing.T) {
	id := "88888888-8888-4888-8888-888888888888"
	p := newPlant(t)
	p.paths["/x-nmos"] = []string{"connection/"}
	p.paths["/x-nmos/connection/"] = []string{"v1.1/"}
	p.paths["/x-nmos/connection/v1.1/single/senders"] = []any{}
	p.paths["/x-nmos/connection/v1.1/single/receivers"] = []string{id + "/"}
	p.paths["/x-nmos/connection/v1.1/bulk"] = []any{}
	p.paths["/x-nmos/connection/v1.1/single/receivers/"+id+"/active"] = map[string]any{
		"master_enable":  true,
		"transport_file": map[string]any{"data": "v=0\r\no=- 1 1 IN IP4 198.51.100.5\r\ns=incoming\r\n", "type": "application/sdp"},
	}
	srv := httptest.NewServer(p)
	defer srv.Close()

	got, err := Run(context.Background(), baseOpts(t, strings.TrimPrefix(srv.URL, "http://")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(got.Dir, "sdp", "receivers", id+"_active.sdp")); err != nil {
		t.Errorf("the embedded receiver SDP was not written: %v", err)
	}
	if !strings.Contains(readReport(t, got.Dir), "SDP   receivers "+id+" active (embedded)") {
		t.Error("the embedded SDP was not recorded in the report")
	}
}

// TestEmptyTransportFileIsNotSDP: an unconnected receiver carries an
// empty transport_file, which must not become a 0-byte SDP file.
func TestEmptyTransportFileIsNotSDP(t *testing.T) {
	for _, body := range []string{"", "   ", "v=0"} {
		if got := embeddedSDP(mustJSON(map[string]any{
			"transport_file": map[string]any{"data": body},
		})); got != "" {
			t.Errorf("embeddedSDP(%q) = %q, want empty", body, got)
		}
	}
	if got := embeddedSDP(mustJSON(map[string]any{"transport_file": map[string]any{"data": nil}})); got != "" {
		t.Errorf("a null transport_file should yield nothing, got %q", got)
	}
	if got := embeddedSDP(nil); got != "" {
		t.Errorf("embeddedSDP(nil) = %q", got)
	}
	if got := embeddedSDP(json.RawMessage("not json")); got != "" {
		t.Errorf("embeddedSDP of garbage = %q", got)
	}
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// TestNoXNMOSRootProbesStandardNames: some devices do not serve the
// root listing, and their APIs are still there.
func TestNoXNMOSRootProbesStandardNames(t *testing.T) {
	p := newPlant(t)
	p.paths["/x-nmos/node/"] = []string{"v1.3/"}
	p.paths["/x-nmos/node/v1.3/self"] = res("11111111-1111-4111-8111-111111111111", "quiet", nil)
	for _, r := range []string{"devices", "sources", "flows", "senders", "receivers"} {
		p.paths["/x-nmos/node/v1.3/"+r] = []any{}
	}
	srv := httptest.NewServer(p)
	defer srv.Close()

	got, err := Run(context.Background(), baseOpts(t, strings.TrimPrefix(srv.URL, "http://")))
	if err != nil {
		t.Fatal(err)
	}
	if got.Role != "node" || got.Label != "quiet" {
		t.Errorf("probing did not find the node API: role=%q label=%q", got.Role, got.Label)
	}
	if !strings.Contains(readReport(t, got.Dir), "no /x-nmos root") {
		t.Error("the fallback probe should be recorded")
	}
}

// TestUnreachableTargetStillWritesAFolder: a capture that reaches
// nothing must still leave an attributable folder, or the operator
// cannot tell a failed run from one that was never started.
func TestUnreachableTargetStillWritesAFolder(t *testing.T) {
	opts := baseOpts(t, "198.51.100.253:1")
	opts.Timeout = 200 * time.Millisecond
	got, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("an unreachable target should not abort the capture: %v", err)
	}
	if got.Failures == 0 {
		t.Error("failures were not counted")
	}
	if _, err := os.Stat(filepath.Join(got.Dir, "device.json")); err != nil {
		t.Errorf("no device.json for an unreachable target: %v", err)
	}
	if !strings.Contains(readReport(t, got.Dir), "ERR") {
		t.Error("the failures were not recorded in the report")
	}
	// Empty scaffolding must not be left behind.
	if _, err := os.Stat(filepath.Join(got.Dir, "sdp")); err == nil {
		t.Error("an empty sdp/ directory was left in the capture folder")
	}
	if _, err := os.Stat(filepath.Join(got.Dir, "raw")); err == nil {
		t.Error("an empty raw/ directory was left in the capture folder")
	}
}

// TestNeverWritesToOutputRoot: a harvest folder that IS the output root
// leaves raw/ and sdp/ loose at the top, attributable to nothing.
func TestNeverWritesToOutputRoot(t *testing.T) {
	if _, err := Run(context.Background(), Options{Target: "!!!", Out: t.TempDir(), Now: fixedNow}); err == nil {
		t.Error("a target with no usable name should be refused")
	}
	if _, err := Run(context.Background(), Options{}); err == nil {
		t.Error("an empty target should be refused")
	}
}

func TestSanitizeAndHostPort(t *testing.T) {
	if got := sanitize("10.44.55.56:8080"); got != "10_44_55_56_8080" {
		t.Errorf("sanitize = %q", got)
	}
	if got := sanitize("///"); got != "" {
		t.Errorf("sanitize(///) = %q", got)
	}
	for href, want := range map[string]string{
		"http://198.51.100.5:3212/": "198.51.100.5:3212",
		"http://198.51.100.5/":      "198.51.100.5:80",
		"https://198.51.100.5/":     "198.51.100.5:443",
		"not a url":                 "",
		"":                          "",
	} {
		if got := hostPortOf(href); got != want {
			t.Errorf("hostPortOf(%q) = %q, want %q", href, got, want)
		}
	}
}

func TestHighestOrdersMinorsNumerically(t *testing.T) {
	if got := highest([]string{"v1.9", "v1.10", "v1.2"}); got != "v1.10" {
		t.Errorf("highest = %q, want v1.10 (a string sort gets this wrong)", got)
	}
	if got := highest([]string{"bogus"}); got != "bogus" {
		t.Errorf("highest of one unparseable version = %q", got)
	}
	if got := highest(nil); got != "" {
		t.Errorf("highest(nil) = %q", got)
	}
}

func TestAbsolutizeAndStatusToken(t *testing.T) {
	base := "http://h:80/x-nmos/query/v1.3/nodes"
	if got := absolutize(base, "?paging.since=2"); got != base+"?paging.since=2" {
		t.Errorf("relative cursor = %q", got)
	}
	if got := absolutize(base, "http://other/x"); got != "http://other/x" {
		t.Errorf("absolute cursor = %q", got)
	}
	if got := absolutize(":://bad", "x"); got != "x" {
		t.Errorf("unparseable base = %q", got)
	}
	if got := statusToken(0); got != "ERR" {
		t.Errorf("statusToken(0) = %q", got)
	}
	if got := statusToken(502); got != "502" {
		t.Errorf("statusToken(502) = %q", got)
	}
}

func TestDedupeByID(t *testing.T) {
	in := []json.RawMessage{
		mustJSON(map[string]any{"id": "a"}),
		mustJSON(map[string]any{"id": "a"}),
		mustJSON(map[string]any{"id": "b"}),
		mustJSON("no id here"),
	}
	out, dupes := dedupeByID(in)
	if dupes != 1 {
		t.Errorf("dupes = %d, want 1", dupes)
	}
	if len(out) != 3 {
		t.Errorf("kept %d, want 3", len(out))
	}
}

// TestVisitedTargetsCaptureOnce guards against a registry that lists
// itself, or two nodes sharing an address, being captured twice.
func TestVisitedTargetsCaptureOnce(t *testing.T) {
	visited := newVisitTracker()
	visited.claim("h:1")
	got, err := capture(context.Background(), Options{Now: fixedNow, Log: func(string) {}}, t.TempDir(), "h:1", visited)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("an already-captured target should be skipped")
	}
}

func TestDefaultsAreSane(t *testing.T) {
	var o Options
	o.defaults()
	if o.Out == "" || o.Timeout <= 0 || o.Now == nil || o.Log == nil || o.Client == nil {
		t.Errorf("defaults left something unset: %+v", o)
	}
}
