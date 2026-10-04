//go:build integration

package amwa_integration

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The registry and the mirror, scored by a third party (audit R1, R2).
//
// The AMWA tool scores our registry with its own mock nodes. Here the
// scorer is the reference implementation itself: an nmos-cpp Node that
// registers into OUR registry, and the nmos-cpp Registry on either side
// of our mirror. What that Node says of itself on its own Node API, and
// what that Registry lists on its own Query API and WebSocket, are the
// oracle.
//
//	NMOS_TEST_PEER_NODE       the Node API of a third-party Node …
//	NMOS_TEST_REGISTRY_BIND   … set to register at this host:port, on this host
//	NMOS_TEST_PEER_KILL       a command that kills that Node without letting
//	                          it deregister (it is the play's to start and stop)
//	NMOS_TEST_REGISTRY        the oracle Registry (nmos-cpp)
//
// ansible/playbooks/amwa-interop-nmos-cpp.yml opens that window.

// registrationWait covers the peer's retry back-off while nothing
// listened at the address it registers at.
const registrationWait = 90 * time.Second

func TestOurRegistryIsScoredByAThirdPartyNode(t *testing.T) {
	node := env(t, "NMOS_TEST_PEER_NODE", "the Node API of a third-party Node that registers at NMOS_TEST_REGISTRY_BIND")
	bind := env(t, "NMOS_TEST_REGISTRY_BIND", "the host:port that Node registers at, on this host")
	ours := "http://" + bind

	theirs := func() catalogue { return nodeAPI(t, node) }
	first := theirs()
	var nodeID string
	for id := range first["nodes"] {
		nodeID = id
	}
	listed := func() (catalogue, bool) { return queryAPI(ours) }
	registry := startRegistry(t, bind)

	// Everything below reads the state the step before it left.
	registered := false

	t.Run("registers", func(t *testing.T) {
		if !eventually(func() bool { return status(ours+"/x-nmos/query/v1.3/nodes/"+nodeID) == http.StatusOK }, registrationWait) {
			t.Fatalf("TIMEOUT: %s after our registry came up the peer has not registered node %s\n%s", registrationWait, nodeID, tail(registry.logs.String()))
		}
		// Every document the Node serves is in our Query API, as the JSON
		// it registered, and nothing else is.
		if diff := converges(theirs, listed, true, 20*time.Second); len(diff) > 0 {
			t.Fatalf("FAIL-real: our registry does not list what the peer registered: %v", head(diff))
		}
		registered = true
		t.Logf("PASS: node %s — %d resource(s) of its Node API are in our Query API, document for document", nodeID, first.count())
	})

	t.Run("held by its heartbeats", func(t *testing.T) {
		if !registered {
			t.Skip("the peer is not registered")
		}
		health := ours + "/x-nmos/registration/v1.3/health/nodes/" + nodeID
		var before, after struct {
			Health string `json:"health"`
		}
		own(t, health, &before)
		// Past the 12 s expiry, looked at every second: a Node our
		// registry dropped and took back would be missed by one look.
		for i := 0; i < 15; i++ {
			time.Sleep(time.Second)
			if s := status(ours + "/x-nmos/query/v1.3/nodes/" + nodeID); s != http.StatusOK {
				t.Fatalf("FAIL-real: %d s on, our registry answers %d for node %s — it did not keep it\n%s", i+1, s, nodeID, tail(registry.logs.String()))
			}
		}
		own(t, health, &after)
		if after.Health == before.Health {
			t.Fatalf("FAIL-real: the node's health stayed at %s for 15 s — no heartbeat was taken", before.Health)
		}
		t.Logf("PASS: listed at every one of 15 looks, health %s -> %s", before.Health, after.Health)
	})

	t.Run("paged query", func(t *testing.T) {
		if !registered {
			t.Skip("the peer is not registered")
		}
		want := theirs()["senders"]
		const limit = 2
		if len(want) <= limit {
			t.Skipf("the peer has %d sender(s): one page, paging is not exercised", len(want))
		}
		seen := map[string]int{}
		pages := 0
		next := ours + fmt.Sprintf("/x-nmos/query/v1.3/senders?paging.limit=%d", limit)
		for next != "" && pages < 200 {
			rows, header, ok := page(next)
			if !ok {
				t.Fatalf("FAIL-real: GET %s is not a list", next)
			}
			if len(rows) == 0 {
				break
			}
			if len(rows) > limit {
				t.Fatalf("FAIL-real: a page of %d for paging.limit=%d", len(rows), limit)
			}
			pages++
			for id := range rows {
				seen[id]++
			}
			// The first page is the newest; rel="prev" walks back in time.
			next = linkRel(header.Values("Link"), "prev")
		}
		for id := range want {
			if seen[id] != 1 {
				t.Errorf("FAIL-real: sender %s came %d time(s) over %d page(s)", id, seen[id], pages)
			}
		}
		if len(seen) != len(want) {
			t.Errorf("FAIL-real: %d sender(s) over the pages, the peer has %d", len(seen), len(want))
		}
		t.Logf("PASS: %d sender(s) over %d page(s) of %d, each once", len(seen), pages, limit)
	})

	t.Run("websocket grain", func(t *testing.T) {
		if !registered {
			t.Skip("the peer is not registered")
		}
		want := theirs()["receivers"]
		ws := subscribe(t, ours, "/receivers")
		g, ok := nextGrain(ws, 10*time.Second, func(grain) bool { return true })
		if !ok {
			t.Fatal("FAIL-real: no grain within 10 s of subscribing to /receivers")
		}
		got := docs{}
		for _, d := range g.Grain.Data {
			if d.Pre == nil || d.Post == nil {
				t.Errorf("FAIL-real: the first grain's entry %s is not a sync entry (pre and post)", d.Path)
			}
			got[d.Path] = d.Post
		}
		if diff := differences(catalogue{"receivers": want}, catalogue{"receivers": got}, true); len(diff) > 0 {
			t.Fatalf("FAIL-real: the first grain is not the peer's receivers: %v", head(diff))
		}
		t.Logf("PASS: the first grain carries the peer's %d receiver(s), document for document", len(want))
	})

	t.Run("registers again after a 404", func(t *testing.T) {
		if !registered {
			t.Skip("the peer is not registered")
		}
		// Our registry forgets the Node; the Node finds out from the 404
		// on its next heartbeat and has to register everything again.
		req, _ := http.NewRequest(http.MethodDelete, ours+"/x-nmos/registration/v1.3/resource/nodes/"+nodeID, nil)
		resp, err := peer.Do(req)
		if err != nil {
			t.Fatalf("FAIL-real: DELETE the node: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("FAIL-real: our registry answered the DELETE of node %s with %s", nodeID, resp.Status)
		}
		if s := status(ours + "/x-nmos/query/v1.3/nodes/" + nodeID); s != http.StatusNotFound {
			t.Fatalf("FAIL-real: after its DELETE our registry answers %d for node %s", s, nodeID)
		}
		registered = false
		started := time.Now()
		if diff := converges(theirs, listed, true, 40*time.Second); len(diff) > 0 {
			t.Fatalf("FAIL-real: 40 s after the 404 the peer's resources are not all back: %v\n%s", head(diff), tail(registry.logs.String()))
		}
		registered = true
		t.Logf("PASS: everything registered again %.0f s after our registry forgot the node", time.Since(started).Seconds())
	})

	t.Run("mirrored into the oracle registry", func(t *testing.T) {
		if !registered {
			t.Skip("the peer is not registered")
		}
		reg := env(t, "NMOS_TEST_REGISTRY", "an oracle Registry (nmos-cpp)")
		inOracle := func() bool { return status(reg+"/x-nmos/query/v1.3/nodes/"+nodeID) == http.StatusOK }
		if inOracle() {
			t.Fatalf("FAIL-real: the oracle registry already lists node %s — this test would not prove our mirror put it there", nodeID)
		}
		ws := subscribe(t, reg, "/nodes")
		if _, ok := nextGrain(ws, 10*time.Second, func(grain) bool { return true }); !ok {
			t.Fatal("TIMEOUT: the oracle registry sent no first grain on /nodes")
		}
		about := func(pre, post bool) func(grain) bool {
			return func(g grain) bool {
				for _, d := range g.Grain.Data {
					if d.Path == nodeID && (d.Pre != nil) == pre && (d.Post != nil) == post {
						return true
					}
				}
				return false
			}
		}

		statusAddr := freeAddr(t)
		mirror := spawn(t, "registry", "nmos", "mirror", "--source", ours, "--target", reg, "--status-addr", statusAddr)
		oracleLists := func() (catalogue, bool) { return queryAPI(reg) }

		// The oracle lists every resource, as the JSON the Node serves.
		if diff := converges(theirs, oracleLists, false, 40*time.Second); len(diff) > 0 {
			t.Fatalf("FAIL-real: the oracle registry does not list what our mirror forwarded: %v\n%s", head(diff), tail(mirror.logs.String()))
		}
		if _, ok := nextGrain(ws, 10*time.Second, about(false, true)); !ok {
			t.Error("FAIL-real: the oracle's WebSocket did not announce the node as added")
		}
		fill := readMirror(t, statusAddr)

		// Our mirror's heartbeats keep it there past the oracle's expiry.
		time.Sleep(15 * time.Second)
		if !inOracle() {
			t.Fatalf("FAIL-real: 15 s on, the oracle registry has dropped node %s — the mirror's heartbeats did not keep it\n%s", nodeID, tail(mirror.logs.String()))
		}

		// A Node that registers while the mirror runs: our registry
		// forgets it, it registers again, and the mirror carries both.
		req, _ := http.NewRequest(http.MethodDelete, ours+"/x-nmos/registration/v1.3/resource/nodes/"+nodeID, nil)
		if resp, err := peer.Do(req); err == nil {
			_ = resp.Body.Close()
		}
		if diff := converges(theirs, listed, true, 40*time.Second); len(diff) > 0 {
			t.Fatalf("FAIL-real: the peer did not register again in our registry: %v", head(diff))
		}
		// The oracle's own word that the node came back: without it, the
		// listing from before the DELETE would pass for the new one.
		back := func(g grain) bool {
			for _, d := range g.Grain.Data {
				if d.Path == nodeID && d.Post != nil {
					return true
				}
			}
			return false
		}
		if _, ok := nextGrain(ws, 40*time.Second, back); !ok {
			t.Fatalf("FAIL-real: the oracle's WebSocket did not announce the node coming back\n%s", tail(mirror.logs.String()))
		}
		if diff := converges(theirs, oracleLists, false, 40*time.Second); len(diff) > 0 {
			t.Fatalf("FAIL-real: after the node registered again the oracle registry is not level: %v\n%s", head(diff), tail(mirror.logs.String()))
		}
		live := readMirror(t, statusAddr)

		// Stopped, the mirror leaves the oracle as it found it.
		mirror.stop()
		if !eventually(func() bool { return !inOracle() }, 30*time.Second) {
			t.Errorf("FAIL-real: 30 s after the mirror stopped the oracle registry still lists node %s", nodeID)
		}
		if live.Failures != 0 || live.Skipped != 0 {
			t.Fatalf("FAIL-real: %d request(s) of our mirror to the oracle registry were refused or failed (%d at the fill, %d skipped, %d repair(s))\n%s",
				live.Failures, fill.Failures, live.Skipped, live.Resyncs, tail(mirror.logs.String()))
		}
		t.Logf("PASS: %d resource(s) level in the oracle registry at the fill and after a live registration; forwarded %d, deleted %d, refused or failed 0, repairs %d; announced on its WebSocket; gone after the mirror stopped",
			first.count(), live.Forwarded, live.Deleted, live.Resyncs)
	})

	t.Run("expires when killed", func(t *testing.T) {
		if !registered {
			t.Skip("the peer is not registered")
		}
		kill := strings.TrimSpace(os.Getenv("NMOS_TEST_PEER_KILL"))
		if kill == "" {
			t.Skip("NMOS_TEST_PEER_KILL is not set — the play that owns the peer gives the command that kills it")
		}
		if out, err := exec.Command("sh", "-c", kill).CombinedOutput(); err != nil {
			t.Fatalf("FAIL-real: %s: %v\n%s", kill, err, tail(string(out)))
		}
		killed := time.Now()
		gone := func() bool {
			c, ok := queryAPI(ours)
			return ok && c.count() == 0
		}
		// 12 s of silence, a 1 s watchdog, and the heartbeat it was owed.
		if !eventually(gone, 25*time.Second) {
			c, _ := queryAPI(ours)
			t.Fatalf("FAIL-real: 25 s after the peer was killed our registry still lists %d of its resource(s)", c.count())
		}
		took := time.Since(killed).Seconds()
		if took < 5 {
			t.Fatalf("FAIL-real: the node was gone %.0f s after the kill, before a 12 s expiry could have run — it deregistered, or our registry dropped it early", took)
		}
		t.Logf("PASS: the node and every resource under it expired %.0f s after the kill", took)
	})
}

// linkRel returns the URL of one relation of an IS-04 paging Link header.
func linkRel(headers []string, rel string) string {
	for _, h := range headers {
		for _, part := range strings.Split(h, ",") {
			url, params, ok := strings.Cut(strings.TrimSpace(part), ";")
			if ok && strings.Contains(params, `rel="`+rel+`"`) {
				return strings.Trim(strings.TrimSpace(url), "<>")
			}
		}
	}
	return ""
}

// Our mirror copies the oracle Registry into a registry of ours: the
// target lists what the source lists, document for document.
func TestMirrorCopiesTheOracleRegistry(t *testing.T) {
	reg := env(t, "NMOS_TEST_REGISTRY", "an oracle Registry (nmos-cpp)")
	source := func() catalogue {
		c, ok := queryAPI(reg)
		if !ok {
			t.Fatal("TIMEOUT: the oracle registry did not answer its Query API")
		}
		return c
	}
	if source().count() == 0 {
		t.Skip("the oracle Registry holds nothing to copy")
	}

	target := freeAddr(t)
	registry := startRegistry(t, target)
	statusAddr := freeAddr(t)
	mirror := spawn(t, "registry", "nmos", "mirror", "--source", reg, "--target", "http://"+target, "--status-addr", statusAddr)

	copied := func() (catalogue, bool) { return queryAPI("http://" + target) }
	if diff := converges(source, copied, true, 40*time.Second); len(diff) > 0 {
		t.Fatalf("FAIL-real: our registry is not level with the oracle registry: %v\n%s\n%s", head(diff), tail(mirror.logs.String()), tail(registry.logs.String()))
	}
	// Past the target's expiry: the mirror's heartbeats keep the copy.
	time.Sleep(15 * time.Second)
	if diff := converges(source, copied, true, 10*time.Second); len(diff) > 0 {
		t.Fatalf("FAIL-real: 15 s on, the copy is no longer level: %v\n%s", head(diff), tail(mirror.logs.String()))
	}
	s := readMirror(t, statusAddr)
	if s.Failures != 0 || s.Skipped != 0 || s.Resyncs != 0 {
		t.Fatalf("FAIL-real: the copy took %d refused or failed request(s), %d skipped, %d repair(s)\n%s", s.Failures, s.Skipped, s.Resyncs, tail(mirror.logs.String()))
	}
	t.Logf("PASS: %d resource(s) of the oracle registry are in ours, document for document, held 15 s; forwarded %d, refused or failed 0, repairs 0", source().count(), s.Forwarded)
}
