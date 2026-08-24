package export

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAPIsWithoutADedicatedWalk covers the surfaces the exporter
// captures generically: IS-09 System, IS-08 Channel Mapping, and any
// API the device serves that we have no special handling for.
func TestAPIsWithoutADedicatedWalk(t *testing.T) {
	p := newPlant(t)
	p.paths["/x-nmos"] = []string{"system/", "channelmapping/", "events/"}
	p.paths["/x-nmos/system/"] = []string{"v1.0/"}
	p.paths["/x-nmos/system/v1.0/global"] = map[string]any{"id": "sys", "version": "1:0"}
	p.paths["/x-nmos/channelmapping/"] = []string{"v1.0/"}
	p.paths["/x-nmos/channelmapping/v1.0/io"] = map[string]any{"inputs": map[string]any{}}
	p.paths["/x-nmos/channelmapping/v1.0/map/active"] = map[string]any{}
	p.paths["/x-nmos/channelmapping/v1.0/map/staged"] = map[string]any{}
	p.paths["/x-nmos/events/"] = []string{"v1.0/"}
	p.paths["/x-nmos/events/v1.0/"] = []string{"sources/"}

	srv := httptest.NewServer(p)
	defer srv.Close()

	got, err := Run(context.Background(), baseOpts(t, strings.TrimPrefix(srv.URL, "http://")))
	if err != nil {
		t.Fatal(err)
	}
	// None of these identifies the device as a node or a registry.
	if got.Role != "unknown" {
		t.Errorf("role = %q; a device serving only these APIs is neither", got.Role)
	}

	tree := readTree(t, got.Dir)
	apis := tree["apis"].(map[string]any)
	for _, api := range []string{"system", "channelmapping", "events"} {
		if _, ok := apis[api]; !ok {
			t.Errorf("%s was not captured", api)
		}
	}
	cm := apis["channelmapping"].(map[string]any)["data"].(map[string]any)["v1.0"].(map[string]any)
	for _, k := range []string{"io", "map_active", "map_staged"} {
		if _, ok := cm[k]; !ok {
			t.Errorf("channelmapping capture is missing %s", k)
		}
	}
	ev := apis["events"].(map[string]any)["data"].(map[string]any)["v1.0"].(map[string]any)
	if _, ok := ev["root"]; !ok {
		t.Error("an API with no dedicated walk should still have its root captured")
	}
}

// TestSDPFetchedOnceAndFailuresRecorded: a sender's IS-04 manifest and
// its IS-05 transport file are usually the same document. Fetching it
// twice doubles the request count on a 176-sender device for nothing.
func TestSDPFetchedOnceAndFailuresRecorded(t *testing.T) {
	id := "44444444-4444-4444-8444-444444444444"
	p := newPlant(t)
	p.paths["/x-nmos"] = []string{"node/", "connection/"}
	p.paths["/x-nmos/node/"] = []string{"v1.3/"}
	p.paths["/x-nmos/node/v1.3/self"] = res("11111111-1111-4111-8111-111111111111", "n", nil)
	for _, r := range []string{"devices", "sources", "flows", "receivers"} {
		p.paths["/x-nmos/node/v1.3/"+r] = []any{}
	}
	p.paths["/x-nmos/connection/"] = []string{"v1.1/"}
	p.paths["/x-nmos/connection/v1.1/single/senders"] = []string{id + "/"}
	p.paths["/x-nmos/connection/v1.1/single/receivers"] = []any{}
	p.paths["/x-nmos/connection/v1.1/single/senders/"+id+"/active"] = map[string]any{"master_enable": true}
	p.paths["/x-nmos/connection/v1.1/bulk"] = []any{}

	srv := httptest.NewServer(p)
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	// The sender points its manifest at the IS-05 transport file — the
	// same URL the exporter would fetch on the IS-05 pass.
	tf := "/x-nmos/connection/v1.1/single/senders/" + id + "/transportfile"
	p.paths["/x-nmos/node/v1.3/senders"] = []any{
		res(id, "s", map[string]any{"manifest_href": srv.URL + tf}),
	}
	p.paths[tf] = "v=0\r\ns=one\r\n"

	got, err := Run(context.Background(), baseOpts(t, host))
	if err != nil {
		t.Fatal(err)
	}
	if p.hits[tf] != 1 {
		t.Errorf("the transport file was fetched %d times, want 1", p.hits[tf])
	}
	if got.SDPFiles != 1 {
		t.Errorf("SDP files = %d, want 1", got.SDPFiles)
	}
}

// TestSDPFailureRecorded: a 502 on a transport file is exactly the
// defect the audit reports, so the capture has to record it.
func TestSDPFailureRecorded(t *testing.T) {
	p := newPlant(t)
	p.paths["/x-nmos"] = []string{"node/"}
	p.paths["/x-nmos/node/"] = []string{"v1.3/"}
	p.paths["/x-nmos/node/v1.3/self"] = res("11111111-1111-4111-8111-111111111111", "n", nil)
	for _, r := range []string{"devices", "sources", "flows", "receivers"} {
		p.paths["/x-nmos/node/v1.3/"+r] = []any{}
	}
	srv := httptest.NewServer(p)
	defer srv.Close()
	// The manifest points somewhere that does not exist.
	p.paths["/x-nmos/node/v1.3/senders"] = []any{
		res("44444444-4444-4444-8444-444444444444", "s", map[string]any{"manifest_href": srv.URL + "/gone"}),
	}

	got, err := Run(context.Background(), baseOpts(t, strings.TrimPrefix(srv.URL, "http://")))
	if err != nil {
		t.Fatal(err)
	}
	if got.SDPFiles != 0 {
		t.Errorf("a failed fetch must not count as an SDP file, got %d", got.SDPFiles)
	}
	if got.Failures == 0 {
		t.Error("the failed manifest fetch was not counted")
	}
	if !strings.Contains(readReport(t, got.Dir), "404 ") {
		t.Errorf("the failure was not recorded:\n%s", readReport(t, got.Dir))
	}
}

// TestNonJSONAndNonArrayResponses: a device that answers a collection
// with an object, or with something that is not JSON at all, must be
// recorded rather than crashing the capture.
func TestNonJSONAndNonArrayResponses(t *testing.T) {
	p := newPlant(t)
	// /x-nmos answers with an object, not the array the spec defines.
	p.paths["/x-nmos"] = map[string]any{"oops": true}
	p.paths["/x-nmos/node/"] = []string{"v1.3/"}
	p.paths["/x-nmos/node/v1.3/self"] = res("11111111-1111-4111-8111-111111111111", "odd", nil)
	for _, r := range []string{"devices", "sources", "flows", "senders", "receivers"} {
		p.paths["/x-nmos/node/v1.3/"+r] = []any{}
	}
	srv := httptest.NewServer(p)
	defer srv.Close()

	got, err := Run(context.Background(), baseOpts(t, strings.TrimPrefix(srv.URL, "http://")))
	if err != nil {
		t.Fatalf("a malformed root must not abort the capture: %v", err)
	}
	// Falling back to probing the standard names is what recovers here.
	if got.Role != "node" {
		t.Errorf("role = %q, want node via the fallback probe", got.Role)
	}
}

// TestQueryCollectionAnsweredWithAnObject covers a registry that
// answers a paged collection with something that is not a list.
func TestQueryCollectionAnsweredWithAnObject(t *testing.T) {
	p := registryPlant(t)
	p.paths["/x-nmos/query/v1.1/nodes"] = map[string]any{"error": "not a list"}
	p.paths["/x-nmos/query/v1.3/nodes"] = []any{}
	srv := httptest.NewServer(p)
	defer srv.Close()

	got, err := Run(context.Background(), baseOpts(t, strings.TrimPrefix(srv.URL, "http://")))
	if err != nil {
		t.Fatal(err)
	}
	if got.NodesSeen != 0 {
		t.Errorf("nodes seen = %d; a non-list answer lists nobody", got.NodesSeen)
	}
	// The body is still captured verbatim — the audit decides what it
	// means, not the exporter.
	tree := readTree(t, got.Dir)
	q := tree["apis"].(map[string]any)["query"].(map[string]any)
	body := q["data"].(map[string]any)["v1.1"].(map[string]any)["nodes"]
	if body == nil {
		t.Error("the malformed collection was discarded instead of captured")
	}
}

// TestStampedFolderName is the default: two captures of the same device
// on the same day must not overwrite each other.
func TestStampedFolderName(t *testing.T) {
	p := newPlant(t)
	p.paths["/x-nmos"] = []string{}
	srv := httptest.NewServer(p)
	defer srv.Close()

	opts := baseOpts(t, strings.TrimPrefix(srv.URL, "http://"))
	opts.NoStamp = false
	got, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(filepath.Base(got.Dir), "_20260824-090000") {
		t.Errorf("folder %q carries no timestamp", filepath.Base(got.Dir))
	}
}

// TestHTTPSSchemeUsed proves --https reaches a TLS device.
func TestHTTPSSchemeUsed(t *testing.T) {
	p := newPlant(t)
	p.paths["/x-nmos"] = []string{"node/"}
	p.paths["/x-nmos/node/"] = []string{"v1.3/"}
	p.paths["/x-nmos/node/v1.3/self"] = res("11111111-1111-4111-8111-111111111111", "secure", nil)
	for _, r := range []string{"devices", "sources", "flows", "senders", "receivers"} {
		p.paths["/x-nmos/node/v1.3/"+r] = []any{}
	}
	srv := httptest.NewTLSServer(p)
	defer srv.Close()

	opts := baseOpts(t, strings.TrimPrefix(srv.URL, "https://"))
	opts.HTTPS = true
	opts.Client = srv.Client()
	got, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if got.Label != "secure" {
		t.Errorf("label = %q; the TLS capture did not reach the device", got.Label)
	}
}

// TestOutDirectoryMustBeCreatable surfaces a bad --out rather than
// failing silently partway through a long capture.
func TestOutDirectoryMustBeCreatable(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), Options{Target: "h:1", Out: filepath.Join(f, "under-a-file")})
	if err == nil {
		t.Error("an unusable --out should fail before any request is made")
	}
}

// TestCancelledContextStopsTheCapture: an operator pressing ctrl-C mid
// capture must not hang.
func TestCancelledContextStopsTheCapture(t *testing.T) {
	p := newPlant(t)
	p.paths["/x-nmos"] = []string{"node/"}
	srv := httptest.NewServer(p)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	opts := baseOpts(t, strings.TrimPrefix(srv.URL, "http://"))
	opts.Timeout = 500 * time.Millisecond
	got, err := Run(ctx, opts)
	if err != nil {
		t.Fatalf("a cancelled capture should still write its folder: %v", err)
	}
	if got.Requests != 0 {
		t.Errorf("requests = %d on a cancelled context", got.Requests)
	}
}

// TestLogReceivesProgress: a long capture that prints nothing looks
// hung.
func TestLogReceivesProgress(t *testing.T) {
	p := newPlant(t)
	p.paths["/x-nmos"] = []string{}
	srv := httptest.NewServer(p)
	defer srv.Close()

	var lines []string
	opts := baseOpts(t, strings.TrimPrefix(srv.URL, "http://"))
	opts.Log = func(s string) { lines = append(lines, s) }
	if _, err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 || !strings.Contains(lines[0], "capturing") {
		t.Errorf("no progress was reported: %v", lines)
	}
}

func TestWriteRawIgnoresUnusableNames(t *testing.T) {
	h := &harvester{opts: Options{}, dir: t.TempDir()}
	h.opts.defaults()
	h.writeRaw("///", []byte("x"))
	entries, err := os.ReadDir(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("an unusable path produced %d file(s)", len(entries))
	}
}

func TestRankOfRejectsNonNumeric(t *testing.T) {
	for _, v := range []string{"vX.3", "v1.Y", "v1", ""} {
		if got := rankOf(v); got != -1 {
			t.Errorf("rankOf(%q) = %d, want -1", v, got)
		}
	}
	if rankOf("v1.3") <= rankOf("v1.2") {
		t.Error("v1.3 must outrank v1.2")
	}
}

func TestFetchSenderManifestsIgnoresGarbage(t *testing.T) {
	h := &harvester{opts: Options{}, sdpSeen: map[string]bool{}}
	h.opts.defaults()
	// Not a sender array — must return without touching the network.
	h.fetchSenderManifests(context.Background(), json.RawMessage(`{"not":"an array"}`))
	if len(h.report) != 0 {
		t.Errorf("garbage produced report lines: %v", h.report)
	}
	// A sender with no id is not addressable and is skipped.
	h.fetchSenderManifests(context.Background(), json.RawMessage(`[{"manifest_href":"http://x/"}]`))
	if len(h.report) != 0 {
		t.Errorf("an id-less sender produced report lines: %v", h.report)
	}
}

func TestCollectNodesIgnoresGarbage(t *testing.T) {
	h := &harvester{}
	h.collectNodes(json.RawMessage(`{"not":"a list"}`), "v1.3")
	if len(h.nodes) != 0 || len(h.report) != 0 {
		t.Error("a non-list nodes payload should be ignored quietly")
	}
}
