//go:build integration

package amwa_integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// The compat verb (IS-11) against a Node somebody else wrote: the open
// NMOS-Reference Node (github.com/alabou/NMOS-Reference), which serves
// the Stream Compatibility Management API on its Senders and Receivers.
// nmos-cpp has no IS-11, and the IS-11 Nodes on the plant are our own —
// an oracle that is ourselves is none (ADR-0034, #1328).
//
// Everything the verb reports or changes is read back from the peer's
// own API, and every change is undone.

// is11Peer is the third-party Node, its IS-11 base, and one Sender that
// can be constrained on the two parameters the test uses.
func is11Peer(t *testing.T) (node, compat, sender string, params []string) {
	t.Helper()
	node = env(t, "NMOS_TEST_IS11_NODE", "a third-party Node that serves IS-11 (ansible/playbooks/amwa-interop-is11.yml starts one)")
	var devices []resource
	oracle(t, node+"/x-nmos/node/v1.3/devices", &devices)
	for _, d := range devices {
		for _, c := range d.Controls {
			if strings.HasPrefix(c.Type, "urn:x-nmos:control:stream-compat/") {
				compat = strings.TrimRight(c.Href, "/")
			}
		}
	}
	if compat == "" {
		t.Fatalf("FAIL-real: the Node at %s advertises no stream-compat control — it is not an IS-11 peer", node)
	}
	want := []string{"urn:x-nmos:cap:format:frame_height", "urn:x-nmos:cap:format:frame_width"}
	var senders []string
	oracle(t, compat+"/senders/", &senders)
	sort.Strings(senders)
	for _, s := range senders {
		s = strings.TrimRight(s, "/")
		var supported struct {
			Parameters []string `json:"parameter_constraints"`
		}
		oracle(t, compat+"/senders/"+s+"/constraints/supported", &supported)
		has := map[string]bool{}
		for _, p := range supported.Parameters {
			has[p] = true
		}
		if has[want[0]] && has[want[1]] {
			return node, compat, s, want
		}
	}
	t.Skip("no Sender of this Node can be constrained on frame width and height")
	return
}

// peerConstraints is what the peer itself holds for the Sender: its
// active constraint sets and its state.
func peerConstraints(t *testing.T, compat, sender string) (sets []map[string]json.RawMessage, state string) {
	t.Helper()
	var active struct {
		Sets []map[string]json.RawMessage `json:"constraint_sets"`
	}
	oracle(t, compat+"/senders/"+sender+"/constraints/active", &active)
	var status struct {
		State string `json:"state"`
	}
	oracle(t, compat+"/senders/"+sender+"/status", &status)
	return active.Sets, status.State
}

func TestCompatConstrainsAndReleasesAThirdPartySender(t *testing.T) {
	node, compat, sender, params := is11Peer(t)
	args := []string{"consumer", "nmos", "compat", "--node", node, "--sender", sender}
	release := append(append([]string{}, args...), "--release")
	t.Cleanup(func() { _, _, _ = run(t, time.Minute, release...) })
	mustRun(t, time.Minute, release...)
	if sets, _ := peerConstraints(t, compat, sender); len(sets) != 0 {
		t.Fatalf("FAIL-real: released, the peer still holds %d constraint set(s)", len(sets))
	}

	dir := t.TempDir()
	file := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	wanted := file("wanted.json", `{"constraint_sets":[{"`+params[0]+`":{"enum":[1080]},"`+params[1]+`":{"enum":[1920]}}]}`)

	// Reading: the state the verb prints is the state the peer reports.
	out := mustRun(t, time.Minute, args...)
	_, state := peerConstraints(t, compat, sender)
	if !strings.Contains(out, "state: "+state) {
		t.Fatalf("FAIL-real: the peer reports state %q, the verb printed:\n%s", state, tail(out))
	}

	// A dry run checks and changes nothing.
	out = mustRun(t, time.Minute, append(append([]string{}, args...), "--constraints", wanted, "--dry-run")...)
	if sets, _ := peerConstraints(t, compat, sender); len(sets) != 0 {
		t.Fatalf("FAIL-real: a dry run left %d constraint set(s) on the peer:\n%s", len(sets), tail(out))
	}

	// Constraining: the peer holds what was sent, and says so.
	out = mustRun(t, time.Minute, append(append([]string{}, args...), "--constraints", wanted)...)
	sets, state := peerConstraints(t, compat, sender)
	if len(sets) != 1 {
		t.Fatalf("FAIL-real: after the PUT the peer holds %d constraint set(s), want 1:\n%s", len(sets), tail(out))
	}
	for p, want := range map[string]string{params[0]: `{"enum":[1080]}`, params[1]: `{"enum":[1920]}`} {
		if got := strings.Join(strings.Fields(string(sets[0][p])), ""); got != want {
			t.Fatalf("FAIL-real: the peer holds %s = %s, want %s", p, got, want)
		}
	}
	if state != "constrained" || !strings.Contains(out, "state: constrained") {
		t.Fatalf("FAIL-real: the peer reports state %q after the PUT; the verb printed:\n%s", state, tail(out))
	}

	// A parameter the Sender cannot be constrained on is refused before
	// anything is sent: the peer keeps what it had.
	supported := map[string]bool{}
	var sup struct {
		Parameters []string `json:"parameter_constraints"`
	}
	oracle(t, compat+"/senders/"+sender+"/constraints/supported", &sup)
	for _, p := range sup.Parameters {
		supported[p] = true
	}
	const foreign = "urn:x-nmos:cap:format:channel_count"
	if !supported[foreign] {
		bad := file("foreign.json", `{"constraint_sets":[{"`+foreign+`":{"enum":[2]}}]}`)
		o, e, err := run(t, time.Minute, append(append([]string{}, args...), "--constraints", bad)...)
		if err == nil || !strings.Contains(o+e, "does not support constraining") {
			t.Fatalf("FAIL-real: constraining on %s, which the Sender does not offer, was not refused:\n%s", foreign, tail(o+e))
		}
		if after, _ := peerConstraints(t, compat, sender); len(after) != 1 {
			t.Fatalf("FAIL-real: the refused request changed the peer: %d constraint set(s)", len(after))
		}
		t.Logf("FAIL-expected: %s is not among the Sender's supported parameters, refused", foreign)
	}

	// Releasing: the peer holds nothing.
	mustRun(t, time.Minute, release...)
	sets, state = peerConstraints(t, compat, sender)
	if len(sets) != 0 || state == "constrained" {
		t.Fatalf("FAIL-real: released, the peer holds %d constraint set(s) in state %q", len(sets), state)
	}
	t.Logf("PASS: sender %s read, dry-run, constrained to 1920x1080, refused on a foreign parameter and released — each step as the peer's own IS-11 API reports it", sender)
}

// A Receiver's state, as the peer reports it; and the same Sender found
// through a Registry the peer registered into, not named by its address.
func TestCompatReadsAThirdPartyReceiverAndFindsTheSenderByRegistry(t *testing.T) {
	node, compat, sender, _ := is11Peer(t)
	var receivers []string
	oracle(t, compat+"/receivers/", &receivers)
	if len(receivers) == 0 {
		t.Skip("this Node has no IS-11 Receiver")
	}
	sort.Strings(receivers)
	receiver := strings.TrimRight(receivers[0], "/")
	var status struct {
		State string `json:"state"`
	}
	oracle(t, compat+"/receivers/"+receiver+"/status", &status)
	out := mustRun(t, time.Minute, "consumer", "nmos", "compat", "--node", node, "--receiver", receiver)
	if !strings.Contains(out, "state: "+status.State) {
		t.Fatalf("FAIL-real: the peer reports receiver state %q, the verb printed:\n%s", status.State, tail(out))
	}

	registry := env(t, "NMOS_TEST_IS11_REGISTRY", "the Registry the IS-11 peer registered into")
	out = mustRun(t, time.Minute, "consumer", "nmos", "compat", "--registry", registry, "--mdns=false", "--sender", sender)
	_, state := peerConstraints(t, compat, sender)
	if !strings.Contains(out, compat) || !strings.Contains(out, "state: "+state) {
		t.Fatalf("FAIL-real: through the Registry the verb did not reach %s in state %q:\n%s", compat, state, tail(out))
	}
	t.Logf("PASS: receiver %s read as %q; sender %s found through the Registry and read as %q", receiver, status.State, sender, state)
}
