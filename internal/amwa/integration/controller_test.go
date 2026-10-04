//go:build integration

package amwa_integration

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The Registry's own Query API is the oracle: every resource it lists
// is in the CLI's walk, and nothing else is.
func TestWalkOfARegistryIsWhatTheRegistryLists(t *testing.T) {
	reg := env(t, "NMOS_TEST_REGISTRY", "an oracle Registry (nmos-cpp)")
	got := walk(t, "--registry", reg)
	for _, c := range collections {
		var want []resource
		oracle(t, reg+"/x-nmos/query/v1.3/"+c.path+"?paging.limit=1000&query.downgrade=v1.0", &want)
		if len(want) == 1000 {
			t.Fatalf("FAIL-real: the oracle's %s fill a whole page; this check reads one page and cannot vouch for more", c.path)
		}
		sameIDs(t, "registry "+c.path, got[c.key], want)
	}
	t.Logf("PASS: %d nodes, %d senders, %d receivers — the same ids as the registry's own Query API",
		len(got["Nodes"]), len(got["Senders"]), len(got["Receivers"]))
}

// A device's own Node API is the oracle for a walk of that device.
func TestWalkOfADeviceIsWhatTheDeviceLists(t *testing.T) {
	node := env(t, "NMOS_TEST_NODE", "a real device's Node API")
	got := walk(t, "--node", node)
	if len(got["Nodes"]) != 1 {
		t.Errorf("FAIL-real: a walk of one Node lists %d nodes", len(got["Nodes"]))
	}
	var self resource
	oracle(t, node+"/x-nmos/node/v1.3/self", &self)
	if len(got["Nodes"]) == 1 && got["Nodes"][0].ID != self.ID {
		t.Errorf("FAIL-real: the CLI names node %s, the device says it is %s", got["Nodes"][0].ID, self.ID)
	}
	for _, c := range collections[1:] {
		var want []resource
		oracle(t, node+"/x-nmos/node/v1.3/"+c.path, &want)
		sameIDs(t, "device "+c.path, got[c.key], want)
	}
	t.Logf("PASS: node %s — %d senders, %d receivers, the same ids as the device's own Node API",
		self.ID, len(got["Senders"]), len(got["Receivers"]))
}

// rtpPair picks a Sender and a Receiver of the same format on RTP from
// one Node: a route the Receiver can take.
func rtpPair(t *testing.T, node string) (sender, receiver resource) {
	t.Helper()
	var senders, receivers []resource
	oracle(t, node+"/x-nmos/node/v1.3/senders", &senders)
	oracle(t, node+"/x-nmos/node/v1.3/receivers", &receivers)
	var flows []struct {
		ID     string `json:"id"`
		Format string `json:"format"`
	}
	oracle(t, node+"/x-nmos/node/v1.3/flows", &flows)
	format := map[string]string{}
	for _, f := range flows {
		format[f.ID] = f.Format
	}
	var full []struct {
		ID        string  `json:"id"`
		FlowID    *string `json:"flow_id"`
		Transport string  `json:"transport"`
	}
	oracle(t, node+"/x-nmos/node/v1.3/senders", &full)
	for _, r := range receivers {
		if !strings.HasPrefix(r.Transport, "urn:x-nmos:transport:rtp") {
			continue
		}
		for i, s := range full {
			if s.FlowID == nil || !strings.HasPrefix(s.Transport, "urn:x-nmos:transport:rtp") {
				continue
			}
			if format[*s.FlowID] == r.Format {
				return senders[i], r
			}
		}
	}
	t.Skip("this Node has no RTP Sender and Receiver of the same format")
	return
}

// connectionBase is the highest sr-ctrl control the Node's first Device
// advertises: where the oracle reads a Receiver's IS-05 state.
func connectionBase(t *testing.T, node string) string {
	t.Helper()
	var devices []resource
	oracle(t, node+"/x-nmos/node/v1.3/devices", &devices)
	best, bestType := "", ""
	for _, d := range devices {
		for _, c := range d.Controls {
			if strings.HasPrefix(c.Type, "urn:x-nmos:control:sr-ctrl/") && c.Type > bestType {
				best, bestType = strings.TrimRight(c.Href, "/"), c.Type
			}
		}
	}
	if best == "" {
		t.Skip("this Node advertises no IS-05 control")
	}
	return best
}

// staged is the part of an IS-05 staged / active object these tests read.
type staged struct {
	SenderID     *string `json:"sender_id"`
	MasterEnable bool    `json:"master_enable"`
}

// A dry run names the device's own IS-05 endpoint and sends nothing:
// the Receiver's staged state is the same before and after.
func TestConnectDryRunSendsNothing(t *testing.T) {
	node := env(t, "NMOS_TEST_NODE", "a real device's Node API")
	sender, receiver := rtpPair(t, node)
	base := connectionBase(t, node)

	var before, after staged
	oracle(t, base+"/single/receivers/"+receiver.ID+"/staged", &before)
	out := mustRun(t, time.Minute, "consumer", "nmos", "connect", "--node", node,
		"--receiver", receiver.ID, "--sender", sender.ID, "--dry-run")
	oracle(t, base+"/single/receivers/"+receiver.ID+"/staged", &after)

	if !strings.Contains(out, "DRY RUN") {
		t.Errorf("FAIL-real: a dry run must say so:\n%s", tail(out))
	}
	if !strings.Contains(out, base) {
		t.Errorf("FAIL-real: the dry run does not name the device's own IS-05 endpoint %s:\n%s", base, tail(out))
	}
	if (before.SenderID == nil) != (after.SenderID == nil) ||
		(before.SenderID != nil && *before.SenderID != *after.SenderID) || before.MasterEnable != after.MasterEnable {
		t.Errorf("FAIL-real: the dry run changed the Receiver's staged state: %+v -> %+v", before, after)
	}
	t.Logf("PASS: dry run via %s, the device's staged state untouched", base)
}

// A Receiver that is not there is refused, and the CLI says which.
func TestConnectToAnUnknownReceiverIsRefused(t *testing.T) {
	node := env(t, "NMOS_TEST_NODE", "a real device's Node API")
	const ghost = "00000000-0000-4000-8000-00000000dead"
	out, errOut, err := run(t, time.Minute, "consumer", "nmos", "connect", "--node", node, "--receiver", ghost, "--dry-run")
	if err == nil {
		t.Fatalf("FAIL-real: a route to a Receiver the device does not have was accepted:\n%s", tail(out+errOut))
	}
	if !strings.Contains(out+errOut, "no receiver "+ghost) {
		t.Errorf("FAIL-real: the refusal does not name the Receiver:\n%s", tail(out+errOut))
	}
	t.Log("FAIL-expected: the unknown Receiver was refused by name")
}

// A watch opens a subscription on the Registry and prints the current
// state as its first grain.
func TestWatchOpensASubscriptionAndPrintsTheCatalogue(t *testing.T) {
	reg := env(t, "NMOS_TEST_REGISTRY", "an oracle Registry (nmos-cpp)")
	var nodes []resource
	oracle(t, reg+"/x-nmos/query/v1.3/nodes?paging.limit=1000", &nodes)
	if len(nodes) == 0 {
		t.Skip("the oracle Registry holds no Node to be told about")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, binary(t), "consumer", "nmos", "watch", "--registry", reg).CombinedOutput()
	text := string(out)
	if !strings.Contains(text, "Subscribed /nodes -> ws") {
		t.Fatalf("FAIL-real: the watch did not report its subscription:\n%s", tail(text))
	}
	for _, n := range nodes {
		if !strings.Contains(text, n.ID) {
			t.Errorf("FAIL-real: the first grain does not carry node %s (%s):\n%s", n.ID, n.Label, tail(text))
		}
	}
	t.Logf("PASS: subscription opened, %d node(s) in the first grain", len(nodes))
}

// On the reference node a route is made, read back from the node's own
// IS-05, unmade, and read back again.
func TestConnectAndDisconnectAreWhatTheNodeReports(t *testing.T) {
	node := env(t, "NMOS_TEST_MUTATE_NODE", "a Node a route may be made on (the reference node)")
	sender, receiver := rtpPair(t, node)
	base := connectionBase(t, node)
	active := base + "/single/receivers/" + receiver.ID + "/active"

	var before staged
	oracle(t, active, &before)
	t.Cleanup(func() {
		// Leave the Receiver as it was found.
		if before.SenderID != nil && before.MasterEnable {
			_, _, _ = run(t, time.Minute, "consumer", "nmos", "connect", "--node", node,
				"--receiver", receiver.ID, "--sender", *before.SenderID)
			return
		}
		_, _, _ = run(t, time.Minute, "consumer", "nmos", "connect", "--node", node,
			"--receiver", receiver.ID, "--disconnect")
	})

	mustRun(t, time.Minute, "consumer", "nmos", "connect", "--node", node,
		"--receiver", receiver.ID, "--sender", sender.ID)
	var connected staged
	oracle(t, active, &connected)
	if connected.SenderID == nil || *connected.SenderID != sender.ID || !connected.MasterEnable {
		t.Fatalf("FAIL-real: after the connect the node reports %+v, want sender %s enabled", connected, sender.ID)
	}

	mustRun(t, time.Minute, "consumer", "nmos", "connect", "--node", node,
		"--receiver", receiver.ID, "--disconnect")
	var disconnected staged
	oracle(t, active, &disconnected)
	if disconnected.MasterEnable {
		t.Fatalf("FAIL-real: after the disconnect the node still reports the Receiver enabled: %+v", disconnected)
	}
	t.Logf("PASS: receiver %s connected to sender %s and disconnected, as read from the node's own IS-05", receiver.ID, sender.ID)
}
