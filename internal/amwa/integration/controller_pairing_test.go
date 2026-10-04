//go:build integration

package amwa_integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The controller paired with the reference implementation (audit C3):
// a change seen live on the nmos-cpp Registry's WebSocket, and IS-05 and
// IS-07 driven on the nmos-cpp Node. Every result is read from
// nmos-cpp's own API, and every change is undone.

// leaveAsFound puts a Receiver back the way the test found it.
func leaveAsFound(t *testing.T, node, receiverID string, before staged) {
	t.Helper()
	t.Cleanup(func() {
		if before.SenderID != nil && before.MasterEnable {
			_, _, _ = run(t, time.Minute, "consumer", "nmos", "connect", "--node", node,
				"--receiver", receiverID, "--sender", *before.SenderID)
			return
		}
		_, _, _ = run(t, time.Minute, "consumer", "nmos", "connect", "--node", node,
			"--receiver", receiverID, "--disconnect")
	})
}

// on reports whether an IS-05 active object carries this Sender, enabled.
func (s staged) on(senderID string) bool {
	return s.SenderID != nil && *s.SenderID == senderID && s.MasterEnable
}

// A watch that is running when a Node registers and leaves prints both,
// from the Registry's own grains.
func TestWatchReportsANodeComingAndGoing(t *testing.T) {
	reg := env(t, "NMOS_TEST_REGISTRY", "an oracle Registry (nmos-cpp)")
	config := env(t, "NMOS_TEST_NODE_CONFIG", "the path of a Node bundle fixture on this host")
	advertise := env(t, "NMOS_TEST_NODE_ADVERTISE", "the host:port this host's Node is reached at")

	raw, err := os.ReadFile(config)
	if err != nil {
		t.Fatalf("FAIL-real: %v", err)
	}
	var fx bundle
	if err := json.Unmarshal(raw, &fx); err != nil || fx.Node.ID == "" {
		t.Fatalf("FAIL-real: %s is not a Node bundle: %v", config, err)
	}
	if held(t, reg, fx.Node.ID) {
		t.Fatalf("FAIL-real: the registry already lists node %s — its arrival could not be watched", fx.Node.ID)
	}

	watch := spawn(t, "consumer", "nmos", "watch", "--registry", reg, "--duration", "90s")
	said := func(kind string) func() bool {
		return func() bool {
			for _, line := range strings.Split(watch.logs.String(), "\n") {
				if strings.HasPrefix(line, kind) && strings.Contains(line, fx.Node.ID) {
					return true
				}
			}
			return false
		}
	}
	if !eventually(func() bool { return strings.Contains(watch.logs.String(), "Watching.") }, 15*time.Second) {
		t.Fatalf("FAIL-real: the watch did not open its subscription:\n%s", tail(watch.logs.String()))
	}

	node := spawn(t, "producer", "nmos", "serve", "--bind", advertise, "--advertise-host", advertise,
		"--no-mdns", "--registry", reg, "--config", config)
	if !eventually(func() bool { return held(t, reg, fx.Node.ID) }, 30*time.Second) {
		t.Fatalf("TIMEOUT: 30 s after it started the registry does not list node %s\n%s", fx.Node.ID, tail(node.logs.String()))
	}
	if !eventually(said("added"), 10*time.Second) {
		t.Fatalf("FAIL-real: the registry lists node %s and the watch did not print it as added:\n%s", fx.Node.ID, tail(watch.logs.String()))
	}

	node.stop()
	if !eventually(func() bool { return !held(t, reg, fx.Node.ID) }, 20*time.Second) {
		t.Fatalf("TIMEOUT: 20 s after it stopped the registry still lists node %s", fx.Node.ID)
	}
	if !eventually(said("removed"), 10*time.Second) {
		t.Fatalf("FAIL-real: the registry dropped node %s and the watch did not print it as removed:\n%s", fx.Node.ID, tail(watch.logs.String()))
	}
	t.Logf("PASS: the watch printed node %s as added when it registered and as removed when it left", fx.Node.ID)
}

// A scheduled connect is staged at once and takes effect at its time,
// not before.
func TestScheduledConnectActivatesAtItsTime(t *testing.T) {
	node := env(t, "NMOS_TEST_MUTATE_NODE", "a Node a route may be made on (the reference node)")
	sender, receiver := rtpPair(t, node)
	base := connectionBase(t, node)
	active := base + "/single/receivers/" + receiver.ID + "/active"

	var before staged
	oracle(t, active, &before)
	leaveAsFound(t, node, receiver.ID, before)
	if before.on(sender.ID) {
		mustRun(t, time.Minute, "consumer", "nmos", "connect", "--node", node, "--receiver", receiver.ID, "--disconnect")
	}

	const after = 5 * time.Second
	sent := time.Now()
	mustRun(t, time.Minute, "consumer", "nmos", "connect", "--node", node,
		"--receiver", receiver.ID, "--sender", sender.ID,
		"--mode", "activate_scheduled_relative", "--when", fmt.Sprintf("%d:0", int(after.Seconds())))

	var pending struct {
		Activation struct {
			Mode *string `json:"mode"`
		} `json:"activation"`
	}
	oracle(t, base+"/single/receivers/"+receiver.ID+"/staged", &pending)
	var early staged
	oracle(t, active, &early)
	if time.Since(sent) < after-time.Second {
		if early.on(sender.ID) {
			t.Fatalf("FAIL-real: %.1f s after a connect scheduled %s ahead the node already reports it active", time.Since(sent).Seconds(), after)
		}
		if pending.Activation.Mode == nil || *pending.Activation.Mode != "activate_scheduled_relative" {
			t.Fatalf("FAIL-real: the node holds no scheduled activation for the receiver: staged mode %v", pending.Activation.Mode)
		}
	}

	var now staged
	if !eventually(func() bool { oracle(t, active, &now); return now.on(sender.ID) }, after+10*time.Second) {
		t.Fatalf("FAIL-real: %s after a connect scheduled %s ahead the node reports %+v", time.Since(sent).Round(time.Second), after, now)
	}
	took := time.Since(sent)
	if took < after-time.Second {
		t.Fatalf("FAIL-real: scheduled %s ahead, active after %s", after, took.Round(100*time.Millisecond))
	}
	t.Logf("PASS: receiver %s staged as scheduled, not active before its time, active on sender %s %.1f s after the request (asked: %s)",
		receiver.ID, sender.ID, took.Seconds(), after)
}

// A salvo routes several Receivers and unroutes them again.
func TestSalvoConnectsAndDisconnectsEveryRoute(t *testing.T) {
	node := env(t, "NMOS_TEST_MUTATE_NODE", "a Node a route may be made on (the reference node)")
	pairs := rtpPairs(t, node, 2)
	if len(pairs) < 2 {
		t.Skip("this Node has fewer than two RTP Receivers with a Sender of their format")
	}
	base := connectionBase(t, node)
	active := func(receiverID string) staged {
		var s staged
		oracle(t, base+"/single/receivers/"+receiverID+"/active", &s)
		return s
	}

	connect := []string{"consumer", "nmos", "connect", "--node", node}
	disconnect := append([]string{}, connect...)
	for _, p := range pairs {
		leaveAsFound(t, node, p[1].ID, active(p[1].ID))
		connect = append(connect, "--route", p[1].ID+"="+p[0].ID)
		disconnect = append(disconnect, "--route", p[1].ID+"=")
	}

	mustRun(t, time.Minute, connect...)
	for _, p := range pairs {
		if got := active(p[1].ID); !got.on(p[0].ID) {
			t.Fatalf("FAIL-real: after the salvo the node reports receiver %s as %+v, want sender %s enabled", p[1].ID, got, p[0].ID)
		}
	}
	mustRun(t, time.Minute, disconnect...)
	for _, p := range pairs {
		if got := active(p[1].ID); got.MasterEnable {
			t.Fatalf("FAIL-real: after the disconnect salvo the node still reports receiver %s enabled: %+v", p[1].ID, got)
		}
	}
	t.Logf("PASS: %d receivers connected in one salvo and disconnected in another, as read from the node's own IS-05", len(pairs))
}

// senderActive is the part of an IS-05 Sender's active object the set
// test reads.
type senderActive struct {
	MasterEnable    bool `json:"master_enable"`
	TransportParams []struct {
		DestinationIP string `json:"destination_ip"`
	} `json:"transport_params"`
}

func (s senderActive) destinations() []string {
	out := make([]string, len(s.TransportParams))
	for i, p := range s.TransportParams {
		out[i] = p.DestinationIP
	}
	return out
}

// A set changes where a Sender sends, on every leg, and the node says so.
func TestSetChangesWhereASenderSends(t *testing.T) {
	node := env(t, "NMOS_TEST_MUTATE_NODE", "a Node whose Senders may be configured (the reference node)")
	sender, _ := rtpPair(t, node)
	base := connectionBase(t, node)
	active := base + "/single/senders/" + sender.ID + "/active"

	var before senderActive
	oracle(t, active, &before)
	if len(before.TransportParams) == 0 {
		t.Skipf("sender %s reports no transport leg", sender.ID)
	}
	t.Cleanup(func() {
		_, _, _ = run(t, time.Minute, "consumer", "nmos", "set", "--node", node, "--sender", sender.ID,
			"--destination", strings.Join(before.destinations(), ","))
	})

	// Administratively scoped, outside the plant's red and blue ranges;
	// one group per leg — ST 2022-7 legs must not share one.
	want := make([]string, len(before.TransportParams))
	for i := range want {
		want[i] = fmt.Sprintf("239.255.%d.77", 200+i)
	}
	mustRun(t, time.Minute, "consumer", "nmos", "set", "--node", node, "--sender", sender.ID,
		"--destination", strings.Join(want, ","))

	var after senderActive
	oracle(t, active, &after)
	if got := after.destinations(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("FAIL-real: after the set the node reports the sender's destinations as %v, want %v", got, want)
	}
	if after.MasterEnable != before.MasterEnable {
		t.Errorf("FAIL-real: a set that named no enable state changed master_enable from %t to %t", before.MasterEnable, after.MasterEnable)
	}

	// And back. A Sender that has been activated once is the ordinary
	// case, and the one a first set does not meet.
	mustRun(t, time.Minute, "consumer", "nmos", "set", "--node", node, "--sender", sender.ID,
		"--destination", strings.Join(before.destinations(), ","))
	var restored senderActive
	oracle(t, active, &restored)
	if got := restored.destinations(); strings.Join(got, ",") != strings.Join(before.destinations(), ",") {
		t.Fatalf("FAIL-real: set back, the node reports the sender's destinations as %v, want %v", got, before.destinations())
	}
	t.Logf("PASS: sender %s — %d leg(s) moved from %v to %v and back, as read from the node's own IS-05", sender.ID, len(want), before.destinations(), want)
}

// An IS-07 subscription delivers the state of the Source it names.
func TestEventsSubscriptionDeliversTheSourcesState(t *testing.T) {
	node := env(t, "NMOS_TEST_MUTATE_NODE", "a Node with IS-07 Senders (the reference node)")
	base := connectionBase(t, node)
	var senders []resource
	oracle(t, node+"/x-nmos/node/v1.3/senders", &senders)

	var from struct {
		TransportParams []struct {
			URI      string `json:"connection_uri"`
			SourceID string `json:"ext_is_07_source_id"`
			REST     string `json:"ext_is_07_rest_api_url"`
		} `json:"transport_params"`
	}
	for _, s := range senders {
		if s.Transport == "urn:x-nmos:transport:websocket" {
			oracle(t, base+"/single/senders/"+s.ID+"/active", &from)
			if len(from.TransportParams) > 0 && from.TransportParams[0].URI != "" {
				break
			}
		}
	}
	if len(from.TransportParams) == 0 || from.TransportParams[0].URI == "" {
		t.Skip("this Node has no WebSocket Sender with a connection_uri")
	}
	leg := from.TransportParams[0]

	// What the node says the Source's state is, on its Events API.
	var state struct {
		EventType string `json:"event_type"`
	}
	oracle(t, strings.TrimRight(leg.REST, "/")+"/state", &state)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary(t), "consumer", "nmos", "events", "watch",
		"--url", leg.URI, "--sources", leg.SourceID, "--timeout", "8s")
	out, _ := cmd.Output()

	// The verb prints one JSON object per message.
	dec := json.NewDecoder(strings.NewReader(string(out)))
	states := 0
	for {
		var m struct {
			Kind    string `json:"kind"`
			Message struct {
				Identity struct {
					SourceID string `json:"source_id"`
				} `json:"identity"`
				EventType string `json:"event_type"`
			} `json:"message"`
		}
		if err := dec.Decode(&m); err != nil {
			if err != io.EOF {
				t.Fatalf("FAIL-real: the verb's output is not a stream of JSON messages: %v\n%s", err, tail(string(out)))
			}
			break
		}
		if m.Kind != "state" {
			continue
		}
		if m.Message.Identity.SourceID != leg.SourceID {
			t.Errorf("FAIL-real: a state for source %s on a subscription to %s", m.Message.Identity.SourceID, leg.SourceID)
		}
		if m.Message.EventType != state.EventType {
			t.Errorf("FAIL-real: a state of type %q, the node's Events API says %q", m.Message.EventType, state.EventType)
		}
		states++
	}
	if states == 0 {
		t.Fatalf("FAIL-real: 8 s on %s and no state of source %s:\n%s", leg.URI, leg.SourceID, tail(string(out)))
	}
	t.Logf("PASS: %d state message(s) of source %s, type %s — the type the node's own Events API reports", states, leg.SourceID, state.EventType)
}
