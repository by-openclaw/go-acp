package provider

import (
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"

	// The Connection API mounts the IS-05 minors that are REGISTERED,
	// exactly as the shipped binary does. Without these the provider's
	// own test binary would serve no version tree and every route
	// below would 404 for a reason that has nothing to do with the
	// code under test.
	_ "dhs/internal/amwa/codec/is05/v10"
	_ "dhs/internal/amwa/codec/is05/v11"
	_ "dhs/internal/amwa/codec/is05/v12"
)

// serveNodeWithConnection starts a real Node server with IS-05 mounted
// and returns its address.
func serveNodeWithConnection(t *testing.T) string {
	t.Helper()
	addr := freeAddr(t)
	s, err := NewIS04NodeServer(nil, routableBundle(t), IS04NodeConfig{
		Bind:          addr,
		DiscoveryMode: "static",
		// Pin IS-05 to one minor so the paths under test are fixed;
		// the multi-minor case is covered by TestConnectionMountsEveryMinor.
		ConnectionAPIVer: "v1.2",
	})
	if err != nil {
		t.Fatalf("NewIS04NodeServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = s.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = s.Stop()
		wg.Wait()
	})
	if !waitReachable(t, "http://"+addr+"/__ready__", 2*time.Second) {
		t.Fatal("server never came up")
	}
	return addr
}

func getJSON(t *testing.T, url string, into any) int {
	t.Helper()
	resp, err := stdhttp.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if into != nil && len(body) > 0 {
		_ = json.Unmarshal(body, into)
	}
	return resp.StatusCode
}

// TestConnectionAPIIsReachable walks the tree a controller walks: the
// version list, the bulk/single split, the collection, and one
// endpoint's index. Every rung is a place a controller gives up if it
// 404s.
func TestConnectionAPIIsReachable(t *testing.T) {
	addr := serveNodeWithConnection(t)
	b := "http://" + addr

	var vers []string
	if code := getJSON(t, b+"/x-nmos/connection/", &vers); code != 200 {
		t.Fatalf("GET /x-nmos/connection/ = %d, want 200", code)
	}
	if len(vers) == 0 || vers[0] != "v1.2/" {
		t.Errorf("version list = %v, want [v1.2/]", vers)
	}

	var root []string
	getJSON(t, b+"/x-nmos/connection/v1.2/", &root)
	if len(root) != 2 {
		t.Errorf("connection root = %v, want bulk/ and single/", root)
	}

	var single []string
	getJSON(t, b+"/x-nmos/connection/v1.2/single/", &single)
	if len(single) != 2 {
		t.Errorf("single/ = %v, want receivers/ and senders/", single)
	}

	var senders []string
	if code := getJSON(t, b+"/x-nmos/connection/v1.2/single/senders/", &senders); code != 200 {
		t.Fatalf("senders listing = %d", code)
	}
	if len(senders) == 0 {
		t.Fatal("no senders listed — the Connection API must expose every IS-04 sender")
	}
	id := strings.TrimSuffix(senders[0], "/")

	var index []string
	getJSON(t, b+"/x-nmos/connection/v1.2/single/senders/"+id+"/", &index)
	// A sender index carries transportfile; a receiver's does not.
	if len(index) != 5 {
		t.Errorf("sender index = %v, want 5 entries incl. transportfile/", index)
	}
}

// TestConnectionIDsMatchTheNodeAPI pins IS-05 §4.1: "the ids used in
// the Connection API MUST match those in the Node API". A mismatch is
// the dangling reference that makes a controller drop the branch — the
// same failure mode that produced 5,128 findings on a real plant.
func TestConnectionIDsMatchTheNodeAPI(t *testing.T) {
	addr := serveNodeWithConnection(t)
	b := "http://" + addr

	for _, kind := range []string{"senders", "receivers"} {
		var nodeSide []map[string]any
		if code := getJSON(t, b+"/x-nmos/node/v1.3/"+kind, &nodeSide); code != 200 {
			t.Fatalf("node %s = %d", kind, code)
		}
		var connSide []string
		if code := getJSON(t, b+"/x-nmos/connection/v1.2/single/"+kind+"/", &connSide); code != 200 {
			t.Fatalf("connection %s = %d", kind, code)
		}
		if len(nodeSide) != len(connSide) {
			t.Errorf("%s: Node API has %d, Connection API has %d — the id sets must match",
				kind, len(nodeSide), len(connSide))
			continue
		}
		have := map[string]bool{}
		for _, id := range connSide {
			have[strings.TrimSuffix(id, "/")] = true
		}
		for _, r := range nodeSide {
			id, _ := r["id"].(string)
			if !have[id] {
				t.Errorf("%s %s is in the Node API but not the Connection API", kind, id)
			}
		}
	}
}

// TestDeviceAdvertisesConnectionControl pins how IS-05 is FOUND. A
// controller never guesses the Connection API URL — it reads
// device.controls[] from IS-04. An API that is served but not
// advertised is, to every controller, absent. This is what IS-05-02
// exists to check.
func TestDeviceAdvertisesConnectionControl(t *testing.T) {
	addr := serveNodeWithConnection(t)

	var devices []map[string]any
	if code := getJSON(t, "http://"+addr+"/x-nmos/node/v1.3/devices", &devices); code != 200 {
		t.Fatalf("devices = %d", code)
	}
	if len(devices) == 0 {
		t.Fatal("no devices")
	}
	for _, d := range devices {
		controls, _ := d["controls"].([]any)
		found := ""
		for _, c := range controls {
			cm, _ := c.(map[string]any)
			typ, _ := cm["type"].(string)
			if strings.HasPrefix(typ, controlTypeSRCtrl) {
				found, _ = cm["href"].(string)
			}
		}
		if found == "" {
			t.Errorf("device %v advertises no %s control — a controller cannot find IS-05",
				d["id"], controlTypeSRCtrl)
			continue
		}
		if !strings.Contains(found, "/x-nmos/connection/") {
			t.Errorf("sr-ctrl href %q does not point at the Connection API", found)
		}
	}
}

// TestStagedPatchOverHTTP drives the real request path: PATCH staged,
// confirm active did NOT move, then activate and confirm it did.
func TestStagedPatchOverHTTP(t *testing.T) {
	addr := serveNodeWithConnection(t)
	b := "http://" + addr + "/x-nmos/connection/v1.2/single/senders/"

	var senders []string
	getJSON(t, b, &senders)
	if len(senders) == 0 {
		t.Fatal("no senders")
	}
	id := strings.TrimSuffix(senders[0], "/")

	patch := func(body string) (int, map[string]any) {
		req, _ := stdhttp.NewRequest(stdhttp.MethodPatch, b+id+"/staged/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := stdhttp.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("PATCH: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return resp.StatusCode, out
	}

	// Stage only — no activation block.
	code, out := patch(`{"master_enable":true,"transport_params":[{"destination_ip":"239.9.9.9"}]}`)
	if code != 200 {
		t.Fatalf("stage returned %d (%v)", code, out)
	}
	var active map[string]any
	getJSON(t, b+id+"/active/", &active)
	if active["master_enable"] == true {
		t.Error("ACTIVE moved on a PATCH with no activation — staging must not activate")
	}

	// Now activate.
	code, out = patch(`{"activation":{"mode":"activate_immediate"}}`)
	if code != 200 {
		t.Fatalf("activate returned %d (%v)", code, out)
	}
	getJSON(t, b+id+"/active/", &active)
	if active["master_enable"] != true {
		t.Errorf("ACTIVE did not take the staged state after activate_immediate: %v", active)
	}
}

// TestScheduledPatchOverHTTPIs202: the wire status matters on its own.
// 200 tells a controller the switch is done; a scheduled activation
// has not happened yet, and 202 is how the spec says so.
func TestScheduledPatchOverHTTPIs202(t *testing.T) {
	addr := serveNodeWithConnection(t)
	b := "http://" + addr + "/x-nmos/connection/v1.2/single/receivers/"

	var rcv []string
	getJSON(t, b, &rcv)
	if len(rcv) == 0 {
		t.Fatal("no receivers")
	}
	id := strings.TrimSuffix(rcv[0], "/")

	body := `{"activation":{"mode":"activate_scheduled_relative","requested_time":"3600:0"}}`
	req, _ := stdhttp.NewRequest(stdhttp.MethodPatch, b+id+"/staged/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 202 {
		raw, _ := io.ReadAll(resp.Body)
		t.Errorf("scheduled activation returned %d, want 202: %s", resp.StatusCode, raw)
	}
}

// TestReceiverHasNoTransportFile: a controller PUSHES the sender's SDP
// into a receiver through the staged PATCH; it never fetches one from
// the receiver. Serving that route would look plausible and never be
// read, so its absence is deliberate and worth pinning.
func TestReceiverHasNoTransportFile(t *testing.T) {
	addr := serveNodeWithConnection(t)
	b := "http://" + addr + "/x-nmos/connection/v1.2/single/receivers/"

	var rcv []string
	getJSON(t, b, &rcv)
	if len(rcv) == 0 {
		t.Fatal("no receivers")
	}
	id := strings.TrimSuffix(rcv[0], "/")

	var index []string
	getJSON(t, b+id+"/", &index)
	for _, e := range index {
		if strings.Contains(e, "transportfile") {
			t.Errorf("receiver index offers %q — receivers have no transport file of their own", e)
		}
	}
	if len(index) != 4 {
		t.Errorf("receiver index = %v, want 4 entries", index)
	}
}

// TestBulkReportsPerEndpointCodes: a bulk request can partially
// succeed. Collapsing that into one status would leave the controller
// unable to tell WHICH endpoint refused.
func TestBulkReportsPerEndpointCodes(t *testing.T) {
	addr := serveNodeWithConnection(t)
	base := "http://" + addr + "/x-nmos/connection/v1.2"

	var senders []string
	getJSON(t, base+"/single/senders/", &senders)
	if len(senders) == 0 {
		t.Fatal("no senders")
	}
	good := strings.TrimSuffix(senders[0], "/")

	body := `[{"id":"` + good + `","params":{"master_enable":true}},` +
		`{"id":"99999999-9999-4999-8999-999999999999","params":{"master_enable":true}}]`
	resp, err := stdhttp.Post(base+"/bulk/senders/", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("bulk POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)

	var results []map[string]any
	if err := json.Unmarshal(raw, &results); err != nil {
		t.Fatalf("bulk response is not an array: %s", raw)
	}
	if len(results) != 2 {
		t.Fatalf("bulk returned %d results for 2 items: %s", len(results), raw)
	}
	if results[0]["code"] != float64(200) {
		t.Errorf("known sender got code %v, want 200", results[0]["code"])
	}
	if results[1]["code"] != float64(404) {
		t.Errorf("unknown sender got code %v, want 404 — a partial failure must be reported per id",
			results[1]["code"])
	}
}

// TestConnectionMountsEveryMinor: with no pin, every registered IS-05
// minor is served in parallel. A v1.0-pinned controller and a v1.2 one
// must each find a tree they can speak — that is what "support every
// published minor" means on the wire.
func TestConnectionMountsEveryMinor(t *testing.T) {
	addr := freeAddr(t)
	s, err := NewIS04NodeServer(nil, routableBundle(t), IS04NodeConfig{
		Bind:          addr,
		DiscoveryMode: "static",
	})
	if err != nil {
		t.Fatalf("NewIS04NodeServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = s.Serve(ctx) }()
	defer func() {
		cancel()
		_ = s.Stop()
		wg.Wait()
	}()
	if !waitReachable(t, "http://"+addr+"/__ready__", 2*time.Second) {
		t.Fatal("server never came up")
	}

	var vers []string
	getJSON(t, "http://"+addr+"/x-nmos/connection/", &vers)
	if len(vers) < 3 {
		t.Errorf("connection serves %v — every registered IS-05 minor should be mounted", vers)
	}
	for _, v := range vers {
		var root []string
		if code := getJSON(t, "http://"+addr+"/x-nmos/connection/"+v, &root); code != 200 {
			t.Errorf("advertised minor %s is not actually served (%d)", v, code)
		}
	}
}

// routableBundle is validBundle() plus one sender and one receiver.
//
// The base bundle has neither, which is a legitimate IS-04 Node and a
// useless subject for IS-05: a Connection API with nothing to connect
// proves only that the routes exist.
func routableBundle(t *testing.T) *NodeConfig {
	t.Helper()
	b := validBundle()
	dev := b.Devices[0].ID
	snd := is04.Sender{
		ResourceCore: is04.ResourceCore{
			ID: "aaaaaaaa-1111-4111-8111-111111111111", Version: "0:0",
			Label: "snd-1", Description: "test sender", Tags: map[string][]string{},
		},
		Transport:         is04.TransportRTP,
		DeviceID:          dev,
		InterfaceBindings: []string{"eth0"},
	}
	rcv := is04.Receiver{
		ResourceCore: is04.ResourceCore{
			ID: "bbbbbbbb-2222-4222-8222-222222222222", Version: "0:0",
			Label: "rcv-1", Description: "test receiver", Tags: map[string][]string{},
		},
		Transport:         is04.TransportRTP,
		DeviceID:          dev,
		Format:            is04.FormatVideo,
		InterfaceBindings: []string{"eth0"},
	}
	b.Senders = append(b.Senders, snd)
	b.Receivers = append(b.Receivers, rcv)
	b.Devices[0].Senders = []string{snd.ID}
	b.Devices[0].Receivers = []string{rcv.ID}
	return b
}
