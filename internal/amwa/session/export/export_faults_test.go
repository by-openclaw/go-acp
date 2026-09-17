package export

// The capture under adverse conditions: a folder that cannot be
// written, a registered node that is gone, a registry that gives no
// paging evidence, a device that answers with something other than
// JSON. The rule throughout is the package doc's: record the failure,
// never repair it — and when the capture itself cannot be recorded,
// say so and stop.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hostOf(srv *httptest.Server) string { return strings.TrimPrefix(srv.URL, "http://") }

// captureFolder is where an unstamped, identity-less capture of host
// lands under out.
func captureFolder(out, host string) string { return filepath.Join(out, sanitize(host)) }

// anonymousDevice serves one API and names itself nothing, so its
// folder keeps the address-only name and is easy to find.
func anonymousDevice(t *testing.T) *plant {
	t.Helper()
	p := newPlant(t)
	p.paths["/x-nmos"] = []string{"events/"}
	p.paths["/x-nmos/events/"] = []string{"v1.0/"}
	p.paths["/x-nmos/events/v1.0/"] = []string{}
	return p
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	mkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestCaptureFilesThatCannotBeWrittenStopTheCapture: device.json,
// tree.json, report.txt and manifest.json are the capture. A folder
// that will not take them is not a partial capture, it is no capture,
// and Run says which file refused.
func TestCaptureFilesThatCannotBeWrittenStopTheCapture(t *testing.T) {
	for _, name := range []string{"device.json", "tree.json", "report.txt", "manifest.json"} {
		t.Run(name+" is a directory", func(t *testing.T) {
			srv := httptest.NewServer(anonymousDevice(t))
			defer srv.Close()
			opts := baseOpts(t, hostOf(srv))
			blocker := filepath.Join(captureFolder(opts.Out, opts.Target), name)
			if name == "manifest.json" {
				blocker = filepath.Join(opts.Out, name)
			}
			mkdirAll(t, blocker)
			_, err := Run(context.Background(), opts)
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("Run = %v, want the refusal to name %s", err, name)
			}
		})
	}
}

// TestDeviceFileLostMidCaptureIsAnError: device.json is written twice
// — once before the walk so an interrupted capture is attributable,
// once after so it carries the role. Losing it between the two is the
// same failure as never writing it.
func TestDeviceFileLostMidCaptureIsAnError(t *testing.T) {
	srv := httptest.NewServer(anonymousDevice(t))
	defer srv.Close()
	opts := baseOpts(t, hostOf(srv))
	device := filepath.Join(captureFolder(opts.Out, opts.Target), "device.json")
	opts.Log = func(line string) {
		// The first walk line proves the initial device.json landed;
		// replacing it now hits the rewrite.
		if strings.HasPrefix(line, "  ") {
			if err := os.Remove(device); err != nil {
				t.Error(err)
			}
			mkdirAll(t, device)
		}
	}
	_, err := Run(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "device.json") {
		t.Fatalf("Run = %v, want the device.json rewrite to fail", err)
	}
}

// TestRenameKeepsTheAddressFolderWhenItCannotRename: the identity
// rename is a convenience; the capture is complete under the
// address-only name, so a taken name or a refused rename is a NOTE
// in the report and Result.Dir still points at what is on disk.
func TestRenameKeepsTheAddressFolderWhenItCannotRename(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, out, host string)
		want  string
	}{
		{"target name already taken", func(t *testing.T, out, host string) {
			mkdirAll(t, captureFolder(out, host)+"__cam_01")
		}, "a folder of that name already exists"},
		{"rename refused by the filesystem", func(t *testing.T, _, _ string) {
			renameDir = func(string, string) error { return errors.New("folder is locked") }
			t.Cleanup(func() { renameDir = os.Rename })
		}, "folder not renamed: folder is locked"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(&identityNode{label: "cam-01"})
			defer srv.Close()
			opts := baseOpts(t, hostOf(srv))
			tc.setup(t, opts.Out, opts.Target)
			got, err := Run(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			if got.Dir != captureFolder(opts.Out, opts.Target) {
				t.Errorf("Result.Dir = %q, want the address-only folder", got.Dir)
			}
			if _, err := os.Stat(filepath.Join(got.Dir, "tree.json")); err != nil {
				t.Errorf("the capture is not under Result.Dir: %v", err)
			}
			if rep := readReport(t, got.Dir); !strings.Contains(rep, tc.want) {
				t.Errorf("report does not say why the folder kept its name:\n%s", rep)
			}
		})
	}
}

// TestEmptyScaffoldingIsPrunedOrReported: an empty raw/ left behind by
// an earlier run is removed; one that cannot be removed is an error,
// because a capture folder that carries empty scaffolding audits as a
// device that served nothing.
func TestEmptyScaffoldingIsPrunedOrReported(t *testing.T) {
	t.Run("removed", func(t *testing.T) {
		srv := httptest.NewServer(anonymousDevice(t))
		defer srv.Close()
		opts := baseOpts(t, hostOf(srv))
		raw := filepath.Join(captureFolder(opts.Out, opts.Target), "raw")
		mkdirAll(t, raw)
		if _, err := Run(context.Background(), opts); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(raw); err == nil {
			t.Error("an empty raw/ survived the capture")
		}
	})
	t.Run("cannot be removed", func(t *testing.T) {
		removeDir = func(string) error { return errors.New("directory is busy") }
		t.Cleanup(func() { removeDir = os.Remove })
		srv := httptest.NewServer(anonymousDevice(t))
		defer srv.Close()
		opts := baseOpts(t, hostOf(srv))
		mkdirAll(t, filepath.Join(captureFolder(opts.Out, opts.Target), "raw"))
		_, err := Run(context.Background(), opts)
		if err == nil || !strings.Contains(err.Error(), "busy") {
			t.Fatalf("Run = %v, want the prune failure", err)
		}
	})
}

// registryListing serves a registry advertising one node at href.
func registryListing(t *testing.T, id, label, href string) *plant {
	t.Helper()
	p := registryPlant(t)
	p.paths["/x-nmos/query/v1.1/nodes"] = []any{res(id, label, map[string]any{"href": href})}
	p.paths["/x-nmos/query/v1.3/nodes"] = []any{}
	return p
}

const nodeID = "11111111-1111-4111-8111-111111111111"

// TestFollowingNodesNeedsAWritableNodesFolder: the nodes/ folder is
// where the plant goes; if it cannot be created there is no plant
// capture to speak of.
func TestFollowingNodesNeedsAWritableNodesFolder(t *testing.T) {
	nodeSrv := httptest.NewServer(&identityNode{label: "cam-01"})
	defer nodeSrv.Close()
	regSrv := httptest.NewServer(registryListing(t, nodeID, "cam-01", nodeSrv.URL+"/"))
	defer regSrv.Close()
	opts := baseOpts(t, hostOf(regSrv))
	writeFile(t, filepath.Join(captureFolder(opts.Out, opts.Target), "nodes"))
	if _, err := Run(context.Background(), opts); err == nil {
		t.Fatal("a nodes/ path that is a file must fail the registry capture")
	}
}

// TestFollowedNodeFailureAbortsTheCapture: a node whose folder cannot
// be written fails the whole capture — a plant capture missing a node
// for a local disk reason must not look like a plant with one node
// fewer.
func TestFollowedNodeFailureAbortsTheCapture(t *testing.T) {
	nodeSrv := httptest.NewServer(&identityNode{label: "cam-01"})
	defer nodeSrv.Close()
	regSrv := httptest.NewServer(registryListing(t, nodeID, "cam-01", nodeSrv.URL+"/"))
	defer regSrv.Close()
	opts := baseOpts(t, hostOf(regSrv))
	child := filepath.Join(captureFolder(opts.Out, opts.Target), "nodes", sanitize(hostOf(nodeSrv)))
	mkdirAll(t, filepath.Join(child, "device.json"))
	_, err := Run(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "device.json") {
		t.Fatalf("Run = %v, want the node's failure surfaced", err)
	}
}

// TestUnreachableNodeIsRecordedOnTheRegistry: a registration pointing
// at something that answers nothing is not a device with an empty
// tree. Its folder is removed and the skip is noted on the PARENT, so
// the audit sees a registry advertising a node nobody can reach.
func TestUnreachableNodeIsRecordedOnTheRegistry(t *testing.T) {
	gone := httptest.NewServer(http.NotFoundHandler())
	defer gone.Close()
	regSrv := httptest.NewServer(registryListing(t, nodeID, "ghost", gone.URL+"/"))
	defer regSrv.Close()
	opts := baseOpts(t, hostOf(regSrv))
	got, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Followed) != 0 {
		t.Errorf("an unreachable node was kept as a result: %+v", got.Followed)
	}
	if _, err := os.Stat(filepath.Join(got.Dir, "nodes", sanitize(hostOf(gone)))); err == nil {
		t.Error("the unreachable node's folder was left behind")
	}
	rep := readReport(t, got.Dir)
	for _, want := range []string{
		"SKIP  node " + nodeID + " 'ghost' unreachable at: " + hostOf(gone),
		"SUMMARY 1 listed / 0 followed / 0 capped",
	} {
		if !strings.Contains(rep, want) {
			t.Errorf("report is missing %q:\n%s", want, rep)
		}
	}
	b, err := os.ReadFile(filepath.Join(opts.Out, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Devices) != 1 || m.NodesListed != 1 {
		t.Errorf("manifest lists %d devices / %d nodes listed, want 1 / 1", len(m.Devices), m.NodesListed)
	}
}

// TestUnreachableNodeFolderThatCannotBeRemovedIsAnError: the folder
// of a node that answered nothing must go, or it audits as a fourth
// device with no APIs. If it cannot go, the capture says so rather
// than leaving the misleading folder quietly in place.
func TestUnreachableNodeFolderThatCannotBeRemovedIsAnError(t *testing.T) {
	removeAll = func(string) error { return errors.New("folder is in use") }
	t.Cleanup(func() { removeAll = os.RemoveAll })
	gone := httptest.NewServer(http.NotFoundHandler())
	defer gone.Close()
	regSrv := httptest.NewServer(registryListing(t, nodeID, "ghost", gone.URL+"/"))
	defer regSrv.Close()
	_, err := Run(context.Background(), baseOpts(t, hostOf(regSrv)))
	if err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("Run = %v, want the removal failure surfaced", err)
	}
}

// TestReportRewriteFailureAfterFollowingIsAnError: the registry's
// report gains the SKIP/SUMMARY lines after the nodes are done, so it
// is rewritten — and that rewrite failing loses exactly the lines the
// audit needs most.
func TestReportRewriteFailureAfterFollowingIsAnError(t *testing.T) {
	nodeSrv := httptest.NewServer(&identityNode{label: "cam-01"})
	defer nodeSrv.Close()
	regSrv := httptest.NewServer(registryListing(t, nodeID, "cam-01", nodeSrv.URL+"/"))
	defer regSrv.Close()
	opts := baseOpts(t, hostOf(regSrv))
	report := filepath.Join(captureFolder(opts.Out, opts.Target), "report.txt")
	opts.Log = func(line string) {
		// "[1/1] ..." is printed as a node is followed: the first
		// report.txt is on disk, the rewrite is still to come.
		if strings.HasPrefix(line, "[") {
			if err := os.Remove(report); err != nil {
				t.Error(err)
			}
			mkdirAll(t, report)
		}
	}
	_, err := Run(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "report.txt") {
		t.Fatalf("Run = %v, want the report rewrite to fail", err)
	}
}

// TestRootEntriesThatNameNothingAreSkipped: a device listing "/" or ""
// in /x-nmos has named no API; the exporter walks the rest and does
// not request `/x-nmos//`.
func TestRootEntriesThatNameNothingAreSkipped(t *testing.T) {
	p := newPlant(t)
	p.paths["/x-nmos"] = []string{"/", "", "node/"}
	p.paths["/x-nmos/node/"] = []string{"v1.3/"}
	p.paths["/x-nmos/node/v1.3/self"] = res(nodeID, "n", nil)
	srv := httptest.NewServer(p)
	defer srv.Close()
	got, err := Run(context.Background(), baseOpts(t, hostOf(srv)))
	if err != nil {
		t.Fatal(err)
	}
	if got.Role != "node" {
		t.Errorf("role = %q; the node API after the empty entries was not walked", got.Role)
	}
	if p.hits["/x-nmos//"] != 0 {
		t.Error("an empty API name was requested")
	}
}

// TestRoleFromTheAPISet: when no walk names the role, the API set
// does — a registration API alone is a registry, a node API whose
// self did not identify itself is still a node.
func TestRoleFromTheAPISet(t *testing.T) {
	cases := []struct {
		name  string
		paths map[string]any
		want  string
	}{
		{"registration API only", map[string]any{
			"/x-nmos":                    []string{"registration/"},
			"/x-nmos/registration/":      []string{"v1.3/"},
			"/x-nmos/registration/v1.3/": []string{"resource/"},
		}, "registry"},
		{"node API with an anonymous self", map[string]any{
			"/x-nmos":                []string{"node/"},
			"/x-nmos/node/":          []string{"v1.3/"},
			"/x-nmos/node/v1.3/self": map[string]any{},
		}, "node"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newPlant(t)
			p.paths = tc.paths
			srv := httptest.NewServer(p)
			defer srv.Close()
			got, err := Run(context.Background(), baseOpts(t, hostOf(srv)))
			if err != nil {
				t.Fatal(err)
			}
			if got.Role != tc.want {
				t.Errorf("role = %q, want %q", got.Role, tc.want)
			}
		})
	}
}

// TestConnectionWalkReportsProgressOnLargeDevices: a Neuron publishes
// 176 endpoints a side. Without a progress line the IS-05 pass looks
// hung; with an empty id in the list nothing is requested for it.
func TestConnectionWalkReportsProgressOnLargeDevices(t *testing.T) {
	ids := []string{""}
	for i := 0; i < 100; i++ {
		ids = append(ids, fmt.Sprintf("rcv-%03d/", i))
	}
	p := newPlant(t)
	p.paths["/x-nmos"] = []string{"connection/"}
	p.paths["/x-nmos/connection/"] = []string{"v1.1/"}
	p.paths["/x-nmos/connection/v1.1/single/senders"] = []any{}
	p.paths["/x-nmos/connection/v1.1/single/receivers"] = ids
	p.paths["/x-nmos/connection/v1.1/bulk"] = []any{}
	srv := httptest.NewServer(p)
	defer srv.Close()

	var lines []string
	opts := baseOpts(t, hostOf(srv))
	opts.NoSDP = true
	opts.Log = func(s string) { lines = append(lines, s) }
	if _, err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"IS-05 receivers, 101 endpoints x 1", "receivers 50/101", "receivers 100/101"} {
		if !strings.Contains(joined, want) {
			t.Errorf("progress line %q missing:\n%s", want, joined)
		}
	}
	if p.hits["/x-nmos/connection/v1.1/single/receivers//active"] != 0 {
		t.Error("an empty endpoint id was requested")
	}
}

// TestQueryCollectionFailureIsRecorded: a collection the registry
// refuses is a failure line in the report, and the walk continues to
// the next collection.
func TestQueryCollectionFailureIsRecorded(t *testing.T) {
	p := registryPlant(t)
	delete(p.paths, "/x-nmos/query/v1.3/subscriptions")
	p.paths["/x-nmos/query/v1.1/nodes"] = []any{}
	p.paths["/x-nmos/query/v1.3/nodes"] = []any{}
	srv := httptest.NewServer(p)
	defer srv.Close()
	got, err := Run(context.Background(), baseOpts(t, hostOf(srv)))
	if err != nil {
		t.Fatal(err)
	}
	if got.Failures == 0 {
		t.Error("the refused collection was not counted")
	}
	if rep := readReport(t, got.Dir); !strings.Contains(rep, "404   /x-nmos/query/v1.3/subscriptions") {
		t.Errorf("the refused collection was not recorded:\n%s", rep)
	}
}

// TestPagingEndsWhenNothingSaysWhereNextIs: no Link, no X-Paging-*,
// and resources without a version — there is no cursor to derive, so
// the walk ends on the page it has rather than guessing one.
func TestPagingEndsWhenNothingSaysWhereNextIs(t *testing.T) {
	p := registryPlant(t)
	p.paths["/x-nmos/query/v1.1/nodes"] = []any{map[string]any{"id": nodeID, "label": "unversioned"}}
	p.paths["/x-nmos/query/v1.3/nodes"] = []any{}
	srv := httptest.NewServer(p)
	defer srv.Close()
	got, err := Run(context.Background(), baseOpts(t, hostOf(srv)))
	if err != nil {
		t.Fatal(err)
	}
	if got.NodesSeen != 1 {
		t.Errorf("nodes seen = %d, want the one page that was served", got.NodesSeen)
	}
	if p.hits["/x-nmos/query/v1.1/nodes"] != 1 {
		t.Errorf("nodes requested %d times; with no cursor there is nothing to follow", p.hits["/x-nmos/query/v1.1/nodes"])
	}
}

// TestNonJSONRootIsDiscardedAndProbed: a 200 that is not JSON is
// recorded as answered but yields no listing, so the standard names
// are probed instead.
func TestNonJSONRootIsDiscardedAndProbed(t *testing.T) {
	p := newPlant(t)
	p.paths["/x-nmos"] = "<html>not an api</html>"
	p.paths["/x-nmos/node/"] = []string{"v1.3/"}
	p.paths["/x-nmos/node/v1.3/self"] = res(nodeID, "probed", nil)
	srv := httptest.NewServer(p)
	defer srv.Close()
	got, err := Run(context.Background(), baseOpts(t, hostOf(srv)))
	if err != nil {
		t.Fatal(err)
	}
	if got.Label != "probed" {
		t.Errorf("label = %q; the fallback probe did not reach the node API", got.Label)
	}
	if !strings.Contains(readReport(t, got.Dir), "no /x-nmos root") {
		t.Error("the fallback was not recorded")
	}
}

// TestRequestsThatCannotBeBuiltAreFailures: a target that does not
// form a URL fails every request before the network; each is an ERR
// line, not a panic and not a silent empty capture.
func TestRequestsThatCannotBeBuiltAreFailures(t *testing.T) {
	got, err := Run(context.Background(), baseOpts(t, "bad host:1"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Requests != 0 || got.Failures == 0 {
		t.Errorf("requests=%d failures=%d; nothing can have succeeded", got.Requests, got.Failures)
	}
	if !strings.Contains(readReport(t, got.Dir), "ERR") {
		t.Error("the failed requests were not recorded")
	}
}

// TestTruncatedBodyIsAFailure: a device that promises more bytes than
// it sends produced a read error, not a body — and a read error is a
// failure line like any other.
func TestTruncatedBodyIsAFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "4096")
		_, _ = w.Write([]byte("short"))
	}))
	defer srv.Close()
	got, err := Run(context.Background(), baseOpts(t, hostOf(srv)))
	if err != nil {
		t.Fatal(err)
	}
	if got.Requests != 0 || got.Failures == 0 {
		t.Errorf("requests=%d failures=%d; a truncated body must not count as answered", got.Requests, got.Failures)
	}
}

// TestSDPFolderThatIsAFileDoesNotStopTheCapture: the SDP writer
// creates its folders lazily; when it cannot, the JSON capture still
// completes — an SDP is evidence, the tree is the capture.
func TestSDPFolderThatIsAFileDoesNotStopTheCapture(t *testing.T) {
	p := newPlant(t)
	p.paths["/x-nmos"] = []string{"node/"}
	p.paths["/x-nmos/node/"] = []string{"v1.3/"}
	p.paths["/x-nmos/node/v1.3/self"] = res(nodeID, "n", nil)
	p.paths["/x-nmos/node/v1.3/senders"] = []any{
		res(sdpSenderID, "s", map[string]any{"manifest_href": "/sdp/1"}),
	}
	p.paths["/sdp/1"] = "v=0\r\ns=one\r\n"
	srv := httptest.NewServer(p)
	defer srv.Close()
	opts := baseOpts(t, hostOf(srv))
	writeFile(t, filepath.Join(captureFolder(opts.Out, opts.Target), "sdp"))
	got, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("an unwritable sdp/ must not abort the capture: %v", err)
	}
	if _, err := os.Stat(filepath.Join(got.Dir, "tree.json")); err != nil {
		t.Errorf("tree.json missing: %v", err)
	}
}

// TestWriteTreeRefusesABodyItCannotEncode pins the tree writer's
// guard directly: a captured body that is not JSON cannot be embedded
// in tree.json and the writer says so rather than emitting a file the
// audit cannot parse.
func TestWriteTreeRefusesABodyItCannotEncode(t *testing.T) {
	h := &harvester{opts: Options{}, dir: t.TempDir(), apis: map[string]*apiCapture{
		"node": {Versions: []string{"v1.3"}, Data: map[string]map[string]json.RawMessage{
			"v1.3": {"self": json.RawMessage("{not json")},
		}},
	}}
	h.opts.defaults()
	if err := h.writeTree(); err == nil {
		t.Fatal("writeTree accepted a body that is not JSON")
	}
}

func TestFetchSDPIgnoresAnEmptyURL(t *testing.T) {
	h := &harvester{opts: Options{}, sdpSeen: map[string]bool{}}
	h.opts.defaults()
	h.fetchSDP(context.Background(), "", sdpSenderID, "is04")
	if h.requests != 0 || h.failures != 0 || len(h.report) != 0 {
		t.Errorf("an empty URL produced activity: requests=%d failures=%d report=%v", h.requests, h.failures, h.report)
	}
}

func TestSmallParsers(t *testing.T) {
	if _, _, ok := splitTAI("12:xx"); ok {
		t.Error("a non-numeric nanosecond field parsed as a version")
	}
	if shortPath("nodes") != "nodes" {
		t.Error("a bare path must pass through shortPath unchanged")
	}
	if got := absolutize("http://h/x", ":://bad"); got != ":://bad" {
		t.Errorf("an unparseable cursor should pass through, got %q", got)
	}
	if host, port := splitHostPort("bare-host"); host != "bare-host" || port != "" {
		t.Errorf("splitHostPort(bare-host) = %q, %q", host, port)
	}
}

// TestManifestFolderFallsBackWhenNotRelative: the manifest wants a
// relative folder, but a Result.Dir that cannot be expressed relative
// to the root is still recorded rather than dropped from the index.
func TestManifestFolderFallsBackWhenNotRelative(t *testing.T) {
	abs := t.TempDir()
	var m Manifest
	collectManifest("relative-root", &Result{Dir: abs, Target: "h:1"}, &m)
	if len(m.Devices) != 1 || m.Devices[0].Folder != filepath.ToSlash(abs) {
		t.Errorf("manifest = %+v, want the absolute folder kept", m.Devices)
	}
}
