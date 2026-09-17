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
	"testing"
	"time"

	"dhs/internal/amwa/codec/is09"
)

func validGlobal() *is09.Global {
	return &is09.Global{
		ID:          "3b8be755-08ff-452b-b217-c9151eb21193",
		Version:     "1441700172:318426300",
		Label:       "ZBQ System",
		Description: "System Global Information for ZBQ",
		Tags:        map[string][]string{},
		IS04:        is09.IS04Config{HeartbeatInterval: 8},
		PTP:         is09.PTPConfig{AnnounceReceiptTimeout: 2, DomainNumber: 57},
	}
}

func TestNewIS09ServerRejectsInvalidGlobal(t *testing.T) {
	bad := *validGlobal()
	bad.IS04.HeartbeatInterval = 0 // out of [1..1000]
	_, err := NewIS09Server(nil, &bad, IS09Config{Bind: ":1"})
	if err == nil || !strings.Contains(err.Error(), "heartbeat_interval") {
		t.Fatalf("expected heartbeat range error, got %v", err)
	}
}

func TestNewIS09ServerRequiresBind(t *testing.T) {
	_, err := NewIS09Server(nil, validGlobal(), IS09Config{})
	if err == nil || !strings.Contains(err.Error(), "Bind") {
		t.Fatalf("expected Bind error, got %v", err)
	}
}

func TestLoadIS09GlobalFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "global.json")
	body := `{
  "id": "3b8be755-08ff-452b-b217-c9151eb21193",
  "version": "1441700172:318426300",
  "label": "Test",
  "description": "Y",
  "tags": {},
  "is04": {"heartbeat_interval": 5},
  "ptp": {"announce_receipt_timeout": 3, "domain_number": 127}
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	g, err := LoadIS09GlobalFromFile(path)
	if err != nil {
		t.Fatalf("LoadIS09GlobalFromFile: %v", err)
	}
	if g.IS04.HeartbeatInterval != 5 {
		t.Fatalf("heartbeat = %d", g.IS04.HeartbeatInterval)
	}
}

func TestLoadIS09GlobalFromFileRejectsBadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "global.json")
	if err := os.WriteFile(path, []byte(`{"id":"not-uuid"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadIS09GlobalFromFile(path); err == nil {
		t.Fatal("expected error on invalid global")
	}
}

// TestServeEndToEnd boots the IS-09 server in static mode (no mDNS) on
// an ephemeral port, then probes both endpoints over HTTP.
func TestServeEndToEnd(t *testing.T) {
	addr := freeAddr(t)
	s, err := NewIS09Server(nil, validGlobal(), IS09Config{
		Bind:          addr,
		DiscoveryMode: "static",
	})
	if err != nil {
		t.Fatalf("NewIS09Server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = s.Serve(ctx)
	}()
	defer func() {
		cancel()
		_ = s.Stop()
		wg.Wait()
	}()

	// Wait until the server is actually listening — probe a path the
	// router 404s on so the index counter stays at zero.
	if !waitReachable(t, "http://"+addr+"/__ready__", 2*time.Second) {
		t.Fatal("server never came up")
	}

	// The base-path indexes above the versioned tree (the AMWA
	// IS-09-01 auto_system_1/2 rows — 404 until #958).
	for path, want := range map[string]string{
		"/x-nmos/":        "system/",
		"/x-nmos/system/": "v1.0/",
	} {
		br, err := stdhttp.Get("http://" + addr + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		bb, _ := io.ReadAll(br.Body)
		_ = br.Body.Close()
		var listing []string
		_ = json.Unmarshal(bb, &listing)
		if br.StatusCode != 200 || len(listing) != 1 || listing[0] != want {
			t.Fatalf("GET %s = %d %s, want [%q]", path, br.StatusCode, bb, want)
		}
	}

	// GET / index
	resp, err := stdhttp.Get("http://" + addr + "/x-nmos/system/v1.0/")
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
	if len(idx) != 1 || idx[0] != "global/" {
		t.Fatalf("index body = %v", idx)
	}

	// GET /global
	resp2, err := stdhttp.Get("http://" + addr + "/x-nmos/system/v1.0/global")
	if err != nil {
		t.Fatalf("GET global: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != 200 {
		t.Fatalf("global status %d", resp2.StatusCode)
	}
	body2, _ := io.ReadAll(resp2.Body)
	g, err := is09.Decode(body2)
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if g.IS04.HeartbeatInterval != 8 {
		t.Fatalf("heartbeat = %d", g.IS04.HeartbeatInterval)
	}

	idxHits, globalHits := s.Stats()
	if idxHits != 1 || globalHits != 1 {
		t.Fatalf("stats = %d / %d", idxHits, globalHits)
	}
}

func TestServeRejectsUnknownPath(t *testing.T) {
	addr := freeAddr(t)
	s, err := NewIS09Server(nil, validGlobal(), IS09Config{Bind: addr, DiscoveryMode: "static"})
	if err != nil {
		t.Fatalf("NewIS09Server: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Serve(ctx) }()
	defer func() { _ = s.Stop() }()
	if !waitReachable(t, "http://"+addr+"/__ready__", 2*time.Second) {
		t.Fatal("server never came up")
	}
	resp, err := stdhttp.Get("http://" + addr + "/wrong")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 404 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

// freeAddr returns an unused port via httptest.NewServer/Close trick.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln := httptest.NewServer(stdhttp.NewServeMux())
	ln.Close()
	return strings.TrimPrefix(ln.URL, "http://")
}

func waitReachable(t *testing.T, url string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := stdhttp.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
