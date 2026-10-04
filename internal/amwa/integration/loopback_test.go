//go:build integration

package amwa_integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"
)

// The rehearsal rig for a change in what our registry or our mirror
// puts on the wire: two of our registries with our mirror between them
// and the fixture Node — one binary, one host, no peer.
//
// This is a loopback regression (ADR-0025's last tier), not an oracle
// test: it cannot say our registry is right, only that our mirror and
// our registry still agree with each other. It exists because the
// pairing with nmos-cpp runs on a released binary only, and v0.36.1
// reached the plant with a mirror that misplaced resources behind its
// own, newly translating, registry (#1351). Run it on the build that is
// about to be released:
//
//	NMOS_TEST_LOOPBACK=1 DHS_BIN=<the build> \
//	NMOS_TEST_NODE_CONFIG=tests/integration/nmos/amwa/amwa-test-node.json \
//	go test -tags integration -run Loopback ./internal/amwa/integration/

// kill ends the process without letting it say goodbye.
func (p *proc) kill() {
	p.once.Do(func() {
		_ = p.cmd.Process.Kill()
		_ = p.cmd.Wait()
	})
}

// A Node's resources are deleted and registered again at once, three
// times, behind a mirror slowed so that it reaches the removals when the
// resources are already back. The target must end with every resource
// where the source has it — listed at v1.3 with no query.downgrade,
// document for document — and the mirror with nothing refused or
// failed. A mirror that takes a lower minor's view of a resource for a
// registration there leaves it out of that listing.
func TestLoopbackMirrorKeepsResourcesAtTheirMinorWhenTheyRegisterAgain(t *testing.T) {
	if os.Getenv("NMOS_TEST_LOOPBACK") == "" {
		t.Skip("NMOS_TEST_LOOPBACK is not set — the loopback rig starts two registries, a mirror and a Node on this host")
	}
	config := env(t, "NMOS_TEST_NODE_CONFIG", "the path of a Node bundle fixture on this host")
	src, dst, nodeAddr, statusAddr := freeAddr(t), freeAddr(t), freeAddr(t), freeAddr(t)
	startRegistry(t, src)
	startRegistry(t, dst)

	// The fixture Node registers once, so the documents are real ones;
	// from then on this test keeps them alive and registers them itself.
	node := spawn(t, "producer", "nmos", "serve", "--bind", nodeAddr, "--advertise-host", nodeAddr,
		"--no-mdns", "--registry", "http://"+src, "--config", config)
	var want catalogue
	if !eventually(func() bool {
		c, ok := queryAPI("http://" + src)
		want = c
		return ok && len(c["nodes"]) == 1 && len(c["receivers"]) > 0
	}, 30*time.Second) {
		t.Fatalf("FAIL-real: the fixture Node did not register into the source registry\n%s", tail(node.logs.String()))
	}
	var nodeID string
	for id := range want["nodes"] {
		nodeID = id
	}
	registration := "http://" + src + "/x-nmos/registration/v1.3"
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		for {
			select {
			case <-done:
				return
			case <-time.After(4 * time.Second):
				if resp, err := peer.Post(registration+"/health/nodes/"+nodeID, "application/json", nil); err == nil {
					_ = resp.Body.Close()
				}
			}
		}
	}()
	node.kill()

	mirror := spawn(t, "registry", "nmos", "mirror", "--source", "http://"+src, "--target", "http://"+dst,
		"--status-addr", statusAddr, "--target-pace", "8")
	source := func() catalogue { return want }
	target := func() (catalogue, bool) { return queryAPI("http://" + dst) }
	if diff := converges(source, target, true, 60*time.Second); len(diff) > 0 {
		t.Fatalf("FAIL-real: the target is not level after the fill: %v\n%s", head(diff), tail(mirror.logs.String()))
	}

	singular := map[string]string{"nodes": "node", "devices": "device", "sources": "source", "flows": "flow", "senders": "sender", "receivers": "receiver"}
	for round := 1; round <= 3; round++ {
		req, _ := http.NewRequest(http.MethodDelete, registration+"/resource/nodes/"+nodeID, nil)
		resp, err := peer.Do(req)
		if err != nil || resp.StatusCode != http.StatusNoContent {
			t.Fatalf("FAIL-real: round %d: DELETE the node at the source: %v", round, err)
		}
		_ = resp.Body.Close()
		for _, kind := range kinds {
			for id, doc := range want[kind] {
				body, _ := json.Marshal(map[string]any{"type": singular[kind], "data": doc})
				resp, err := peer.Post(registration+"/resource", "application/json", bytes.NewReader(body))
				if err != nil {
					t.Fatalf("FAIL-real: round %d: register %s %s again: %v", round, kind, id, err)
				}
				_ = resp.Body.Close()
				if resp.StatusCode/100 != 2 {
					t.Fatalf("FAIL-real: round %d: the source refused %s %s with %s", round, kind, id, resp.Status)
				}
			}
		}
		if diff := converges(source, target, true, 90*time.Second); len(diff) > 0 {
			t.Fatalf("FAIL-real: round %d: the target is not level at v1.3 after the resources registered again: %v\n%s",
				round, head(diff), tail(mirror.logs.String()))
		}
	}
	s := readMirror(t, statusAddr)
	if s.Failures != 0 || s.Skipped != 0 {
		t.Fatalf("FAIL-real: %d request(s) of the mirror were refused or failed, %d skipped\n%s", s.Failures, s.Skipped, tail(mirror.logs.String()))
	}
	t.Logf("PASS: %d resource(s) deleted and registered again three times; the target level at v1.3 each time, document for document; forwarded %d, deleted %d, refused or failed 0, ordered passes %d",
		want.count(), s.Forwarded, s.Deleted, s.Resyncs)
}
