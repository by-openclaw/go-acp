package provider

import (
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
	// Blank import to register the v1.3 IS-04 Codec via init() so that
	// NewIS04NodeServer's spec.Registry lookup succeeds in unit tests.
	// Layer 3 plugin code (which provider/ lives in) MUST NOT depend on
	// vXX/ packages directly per docs/dependencies.md — but tests are
	// the explicit exemption: unit tests need the codec wired up to
	// exercise the plugin end-to-end. Mirrors the pattern in
	// internal/amwa/registry/*_test.go.
	_ "dhs/internal/amwa/codec/is04/v13"
)

func validNode() is04.Node {
	chassis := "ff-ff-ff-ff-ff-ff"
	return is04.Node{
		ResourceCore: is04.ResourceCore{
			ID: "f47ac10b-58cc-4372-a567-0e02b2c3d479", Version: "0:0",
			Label: "lab-node", Description: "test", Tags: map[string][]string{},
		},
		Href: "http://10.6.239.113:8080/",
		Caps: map[string]any{},
		API: is04.NodeAPI{
			Versions: []string{"v1.3"},
			Endpoints: []is04.NodeEndpoint{{Host: "10.6.239.113", Port: 8080, Protocol: "http"}},
		},
		Services: []is04.NodeService{},
		Clocks:   []is04.NodeClock{{Name: "clk0", RefType: "internal"}},
		Interfaces: []is04.NodeIface{
			{ChassisID: &chassis, PortID: "ff-ff-ff-ff-ff-ff", Name: "eth0"},
		},
	}
}

func validBundle() *NodeConfig {
	n := validNode()
	dev := is04.Device{
		ResourceCore: is04.ResourceCore{
			ID: "12345678-1234-4abc-9def-1234567890ab", Version: "0:0",
			Label: "dev-1", Description: "lab dev", Tags: map[string][]string{},
		},
		Type:      "urn:x-nmos:device:generic",
		NodeID:    n.ID,
		Senders:   []string{},
		Receivers: []string{},
		Controls:  []is04.DeviceControl{},
	}
	return &NodeConfig{
		Node:      n,
		Devices:   []is04.Device{dev},
		Sources:   []is04.Source{},
		Flows:     []is04.Flow{},
		Senders:   []is04.Sender{},
		Receivers: []is04.Receiver{},
	}
}

func TestLoadNodeConfigFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "node.json")
	raw, _ := json.MarshalIndent(validBundle(), "", "  ")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := LoadNodeConfigFromFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Node.ID != "f47ac10b-58cc-4372-a567-0e02b2c3d479" {
		t.Fatalf("id = %q", cfg.Node.ID)
	}
}

func TestValidateBundleRequiresMatchingNodeID(t *testing.T) {
	b := validBundle()
	b.Devices[0].NodeID = "aaaaaaaa-1234-4abc-9def-1234567890ab"
	if err := validateBundle(b); err == nil || !strings.Contains(err.Error(), "node_id") {
		t.Fatalf("expected node_id mismatch error, got %v", err)
	}
}

func TestValidateBundleRequiresExistingDevice(t *testing.T) {
	b := validBundle()
	b.Sources = []is04.Source{
		{
			ResourceCore: is04.ResourceCore{
				ID: "bbbbbbbb-1234-4abc-9def-1234567890ab", Version: "0:0",
				Label: "src", Description: "x", Tags: map[string][]string{},
			},
			Caps:     map[string]any{},
			DeviceID: "cccccccc-1234-4abc-9def-1234567890ab", // not in bundle
			Parents:  []string{},
			Format:   is04.FormatVideo,
		},
	}
	if err := validateBundle(b); err == nil || !strings.Contains(err.Error(), "device_id") {
		t.Fatalf("expected device_id ref error, got %v", err)
	}
}

func TestNodeInstanceName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "dhs-nmos-node"},
		{"dhs-debian-node", "dhs-debian-node"},
		{"Studio A — Camera 3", "Studio A — Camera 3"},
	}
	for _, c := range cases {
		if got := nodeInstanceName(c.in); got != c.want {
			t.Errorf("nodeInstanceName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNodeServerEndToEnd(t *testing.T) {
	addr := freeAddr(t)
	s, err := NewIS04NodeServer(nil, validBundle(), IS04NodeConfig{
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

	// Index
	resp, err := stdhttp.Get("http://" + addr + "/x-nmos/node/v1.3/")
	if err != nil {
		t.Fatalf("GET index: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("index status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var idx []string
	_ = json.Unmarshal(body, &idx)
	if len(idx) != 6 {
		t.Fatalf("index = %v", idx)
	}

	// /self
	r2, _ := stdhttp.Get("http://" + addr + "/x-nmos/node/v1.3/self")
	defer func() { _ = r2.Body.Close() }()
	body2, _ := io.ReadAll(r2.Body)
	var node is04.Node
	if err := json.Unmarshal(body2, &node); err != nil {
		t.Fatalf("decode /self: %v", err)
	}
	if node.ID != "f47ac10b-58cc-4372-a567-0e02b2c3d479" {
		t.Fatalf("/self id = %q", node.ID)
	}

	// /devices (list)
	r3, _ := stdhttp.Get("http://" + addr + "/x-nmos/node/v1.3/devices")
	defer func() { _ = r3.Body.Close() }()
	body3, _ := io.ReadAll(r3.Body)
	var devs []is04.Device
	if err := json.Unmarshal(body3, &devs); err != nil {
		t.Fatalf("decode /devices: %v", err)
	}
	if len(devs) != 1 {
		t.Fatalf("devices = %v", devs)
	}

	// /devices/{id}
	r4, _ := stdhttp.Get("http://" + addr + "/x-nmos/node/v1.3/devices/12345678-1234-4abc-9def-1234567890ab")
	defer func() { _ = r4.Body.Close() }()
	if r4.StatusCode != 200 {
		t.Fatalf("/devices/{id} status %d", r4.StatusCode)
	}

	// /devices/{wrong-id}
	r5, _ := stdhttp.Get("http://" + addr + "/x-nmos/node/v1.3/devices/00000000-0000-1000-8000-000000000000")
	defer func() { _ = r5.Body.Close() }()
	if r5.StatusCode != 404 {
		t.Fatalf("/devices/{wrong-id} status = %d", r5.StatusCode)
	}

	// Stats
	stats := s.Stats()
	if stats["self"] == 0 {
		t.Fatalf("self counter not incremented: %+v", stats)
	}

	// Parent listings the AMWA NMOS Testing tool's auto_node_1 / auto_node_2
	// require. /x-nmos -> ["node/"], /x-nmos/node -> [<api_ver>/].
	for _, c := range []struct {
		path string
		want string
	}{
		{"/x-nmos", "node/"},
		{"/x-nmos/", "node/"},
		{"/x-nmos/node", "v1.3/"},
		{"/x-nmos/node/", "v1.3/"},
	} {
		rp, gerr := stdhttp.Get("http://" + addr + c.path)
		if gerr != nil {
			t.Fatalf("GET %s: %v", c.path, gerr)
		}
		body, _ := io.ReadAll(rp.Body)
		_ = rp.Body.Close()
		if rp.StatusCode != 200 {
			t.Fatalf("%s status = %d, want 200", c.path, rp.StatusCode)
		}
		var arr []string
		if err := json.Unmarshal(body, &arr); err != nil {
			t.Fatalf("%s decode: %v body=%s", c.path, err, body)
		}
		// The root lists every API TREE served, so a Node with IS-05
		// mounted advertises "connection/" alongside "node/". Requiring
		// exactly one entry was asserting a bug: the AMWA
		// auto_connection_1 round fails when the Connection API is
		// served but unlisted.
		found := false
		for _, got := range arr {
			if got == c.want {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s = %v, want it to contain %q", c.path, arr, c.want)
		}
	}

	// CORS header should be on every response.
	rcors, _ := stdhttp.Get("http://" + addr + "/x-nmos/node/v1.3/self")
	if got := rcors.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q on /self", got)
	}
	_ = rcors.Body.Close()

	// OPTIONS preflight on any /x-nmos/node path — auto_node_10 hits
	// receivers/{id}/target and expects NOT 405. Even on a path with
	// no registered method handler the dispatcher returns 204.
	req, _ := stdhttp.NewRequest(stdhttp.MethodOptions,
		"http://"+addr+"/x-nmos/node/v1.3/receivers/00000000-0000-1000-8000-000000000000/target", nil)
	ropt, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS receivers target: %v", err)
	}
	defer func() { _ = ropt.Body.Close() }()
	if ropt.StatusCode != stdhttp.StatusOK {
		t.Fatalf("OPTIONS receivers/{id}/target status = %d, want 200", ropt.StatusCode)
	}
}

func TestRegistrationClientPostsAllResources(t *testing.T) {
	var (
		mu        sync.Mutex
		posts     []string
		deletes   []string
		heartbeats int32
	)
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		switch {
		case r.Method == stdhttp.MethodPost && strings.HasSuffix(r.URL.Path, "/resource"):
			body, _ := io.ReadAll(r.Body)
			env, err := is04.DecodeRegistration(body)
			if err != nil {
				w.WriteHeader(stdhttp.StatusBadRequest)
				return
			}
			mu.Lock()
			posts = append(posts, string(env.Type))
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(stdhttp.StatusCreated)
			_, _ = io.WriteString(w, `{}`)
		case r.Method == stdhttp.MethodPost && strings.Contains(r.URL.Path, "/health/nodes/"):
			atomic.AddInt32(&heartbeats, 1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(stdhttp.StatusOK)
			_, _ = io.WriteString(w, `{"health":"123"}`)
		case r.Method == stdhttp.MethodDelete && strings.Contains(r.URL.Path, "/resource/"):
			parts := strings.Split(r.URL.Path, "/")
			mu.Lock()
			deletes = append(deletes, parts[len(parts)-2]+"/"+parts[len(parts)-1])
			mu.Unlock()
			w.WriteHeader(stdhttp.StatusNoContent)
		default:
			w.WriteHeader(stdhttp.StatusNotFound)
		}
	}))
	defer srv.Close()

	rc := NewRegistrationClient(nil, srv.URL, "v1.3", validBundle())
	// Make heartbeat fire often so the test doesn't drag.
	ctx, cancel := context.WithCancel(context.Background())
	go rc.Run(ctx)

	// Wait for at least one heartbeat.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&heartbeats) == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	_ = rc.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(posts) < 2 {
		t.Fatalf("expected node + at least 1 device POST, got %v", posts)
	}
	if posts[0] != "node" {
		t.Fatalf("first POST should be node, got %v", posts)
	}
	if len(deletes) < 2 {
		t.Fatalf("expected DELETEs on shutdown, got %v", deletes)
	}
	stats := rc.Stats()
	if stats["registrations"] < 2 {
		t.Fatalf("registrations counter low: %d", stats["registrations"])
	}
	if stats["deletions"] < 2 {
		t.Fatalf("deletions counter low: %d", stats["deletions"])
	}
}

func TestRegistrationClientReregistersOn404(t *testing.T) {
	var (
		registrations int32
	)
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		switch {
		case r.Method == stdhttp.MethodPost && strings.HasSuffix(r.URL.Path, "/resource"):
			atomic.AddInt32(&registrations, 1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(stdhttp.StatusCreated)
			_, _ = io.WriteString(w, `{}`)
		case r.Method == stdhttp.MethodPost && strings.Contains(r.URL.Path, "/health/nodes/"):
			// Always 404 — Registry "lost" the node.
			w.WriteHeader(stdhttp.StatusNotFound)
		case r.Method == stdhttp.MethodDelete:
			w.WriteHeader(stdhttp.StatusNoContent)
		default:
			w.WriteHeader(stdhttp.StatusNotFound)
		}
	}))
	defer srv.Close()

	rc := NewRegistrationClient(nil, srv.URL, "v1.3", validBundle())
	rc.http.Timeout = 1 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	go rc.Run(ctx)

	// Two heartbeats should each trigger a re-register (Resource POSTs).
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && rc.Stats()["reregister"] < 1 {
		time.Sleep(100 * time.Millisecond)
	}
	cancel()
	_ = rc.Close()

	if rc.Stats()["reregister"] < 1 {
		t.Fatalf("expected reregister to fire, stats=%+v", rc.Stats())
	}
}
