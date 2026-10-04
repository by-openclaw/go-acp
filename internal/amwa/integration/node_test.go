//go:build integration

package amwa_integration

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// bundle is the part of a Node bundle fixture this test reads.
type bundle struct {
	Node      resource   `json:"node"`
	Devices   []resource `json:"devices"`
	Sources   []resource `json:"sources"`
	Flows     []resource `json:"flows"`
	Senders   []resource `json:"senders"`
	Receivers []resource `json:"receivers"`
}

// held asks the oracle Registry's Query API whether it lists a Node.
func held(t *testing.T, reg, nodeID string) bool {
	t.Helper()
	resp, err := peer.Get(reg + "/x-nmos/query/v1.3/nodes/" + nodeID)
	if err != nil {
		t.Fatalf("TIMEOUT: the registry did not answer: %v", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// eventually polls cond until it holds or the time is up.
func eventually(cond func() bool, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return cond()
}

// Our Node registers into the oracle Registry, is kept there by its
// heartbeats past the Registry's expiry, and takes itself out when it
// stops. Everything is read from the Registry's own Query API.
func TestNodeRegistersIntoTheOracleRegistryAndLeavesIt(t *testing.T) {
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
		t.Fatalf("FAIL-real: the registry already lists node %s — someone else registered it; this test would not prove ours did", fx.Node.ID)
	}

	cmd := exec.Command(binary(t), "producer", "nmos", "serve",
		"--bind", advertise, "--advertise-host", advertise, "--no-mdns",
		"--registry", reg, "--config", config)
	var logs strings.Builder
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("FAIL-real: start the node: %v", err)
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}
	t.Cleanup(stop)

	if !eventually(func() bool { return held(t, reg, fx.Node.ID) }, 30*time.Second) {
		t.Fatalf("TIMEOUT: 30 s after it started the registry does not list node %s\n%s", fx.Node.ID, tail(logs.String()))
	}

	// Every resource of the bundle is in the registry, under this Node.
	var devices []resource
	oracle(t, reg+"/x-nmos/query/v1.3/devices?node_id="+fx.Node.ID+"&paging.limit=1000", &devices)
	sameIDs(t, "registered devices", fx.Devices, devices)
	deviceIDs := ids(fx.Devices)
	for _, c := range []struct {
		path string
		want []resource
	}{{"senders", fx.Senders}, {"receivers", fx.Receivers}, {"sources", fx.Sources}} {
		var all, ours []resource
		oracle(t, reg+"/x-nmos/query/v1.3/"+c.path+"?paging.limit=1000", &all)
		for _, r := range all {
			if deviceIDs[r.DeviceID] {
				ours = append(ours, r)
			}
		}
		sameIDs(t, "registered "+c.path, c.want, ours)
	}

	// Heartbeats keep it there past the registry's expiry (12 s).
	time.Sleep(15 * time.Second)
	if !held(t, reg, fx.Node.ID) {
		t.Fatalf("FAIL-real: 15 s on, the registry has dropped node %s — its heartbeats did not keep it\n%s", fx.Node.ID, tail(logs.String()))
	}

	// A Node that stops takes its resources out; it does not leave them
	// to the registry's garbage collection.
	stop()
	if !eventually(func() bool { return !held(t, reg, fx.Node.ID) }, 5*time.Second) {
		t.Errorf("FAIL-real: 5 s after it stopped the registry still lists node %s — it did not deregister\n%s", fx.Node.ID, tail(logs.String()))
	}
	t.Logf("PASS: node %s registered %d device(s), %d sender(s), %d receiver(s), stayed 15 s on its heartbeats, and deregistered on stop",
		fx.Node.ID, len(fx.Devices), len(fx.Senders), len(fx.Receivers))
}
