package provider

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	dnssdcodec "dhs/internal/amwa/codec/dnssd"
)

// scriptSystemDiscovery routes the IS-09 mDNS browse at a scripted answer.
func scriptSystemDiscovery(t *testing.T, answer func() ([]dnssdcodec.Instance, error)) {
	t.Helper()
	prev := discoverSystemMDNS
	discoverSystemMDNS = func(context.Context, time.Duration, *slog.Logger) ([]dnssdcodec.Instance, error) {
		return answer()
	}
	t.Cleanup(func() { discoverSystemMDNS = prev })
}

func systemNode(t *testing.T, cfg IS04NodeConfig, tap *logTap) *IS04NodeServer {
	t.Helper()
	cfg.Bind = "127.0.0.1:0"
	s, err := NewIS04NodeServer(tap.logger(), validBundle(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// A Node told where its System API is reads /global straight from there,
// applies it, and reports it — no discovery, no watcher.
func TestFetchSystemGlobalDirect(t *testing.T) {
	api := newSystemAPI(t, "direct")
	tap := newLogTap()
	s := systemNode(t, IS04NodeConfig{SystemURL: strings.TrimPrefix(api.ts.URL, "http://")}, tap)

	got := s.fetchSystemGlobal(context.Background())
	if got == nil || got.Label != "direct" {
		t.Fatalf("fetchSystemGlobal = %+v, want the served global", got)
	}
	if s.SystemGlobal() == nil || s.SystemGlobal().Label != "direct" {
		t.Errorf("SystemGlobal = %+v, want what was applied", s.SystemGlobal())
	}
	if !tap.has("System API global applied") {
		t.Error("applying a global must be logged")
	}
	s.mu.Lock()
	watcher := s.systemWatcher
	s.mu.Unlock()
	if watcher != nil {
		t.Error("a Node told where its System API is must not also watch for one")
	}
}

// In static discovery mode there is no browse and no watcher: the
// operator publishes the records, and a Node that was given no URL simply
// has no System API.
func TestFetchSystemGlobalStaticMode(t *testing.T) {
	scriptSystemDiscovery(t, func() ([]dnssdcodec.Instance, error) {
		t.Error("static mode must not browse")
		return nil, nil
	})
	tap := newLogTap()
	s := systemNode(t, IS04NodeConfig{DiscoveryMode: "static"}, tap)
	if g := s.fetchSystemGlobal(context.Background()); g != nil {
		t.Errorf("static mode returned %+v, want no global", g)
	}
	if !tap.has("System API discovery skipped (static mode)") {
		t.Errorf("the skip must be logged; saw %v", tap.snapshot())
	}
	if s.SystemGlobal() != nil {
		t.Error("no global was applied")
	}
}

// A browse that finds nothing, and one whose instance cannot be read,
// both leave the Node running and watching for a System API to appear.
func TestFetchSystemGlobalFallsBackToWatching(t *testing.T) {
	fb := newFakeBrowser()
	useFakeBrowser(t, fb)

	for name, discovery := range map[string]func() ([]dnssdcodec.Instance, error){
		"browse failed": func() ([]dnssdcodec.Instance, error) { return nil, errors.New("no multicast") },
		"nothing found": func() ([]dnssdcodec.Instance, error) { return nil, nil },
	} {
		t.Run(name, func(t *testing.T) {
			scriptSystemDiscovery(t, discovery)
			tap := newLogTap()
			s := systemNode(t, IS04NodeConfig{}, tap)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if g := s.fetchSystemGlobal(ctx); g != nil {
				t.Errorf("got %+v, want no global", g)
			}
			if !tap.has("no System API yet, watching for one") {
				t.Errorf("the fallback must be logged; saw %v", tap.snapshot())
			}
			s.mu.Lock()
			watching := s.systemWatcher != nil
			s.mu.Unlock()
			if !watching {
				t.Error("the Node must be watching for a System API")
			}
		})
	}

	// An instance that is found but cannot be read: warned, and watched.
	broken := newSystemAPI(t, "broken")
	broken.fail = true
	scriptSystemDiscovery(t, func() ([]dnssdcodec.Instance, error) {
		return []dnssdcodec.Instance{broken.instance(t, "broken", 1, 120)}, nil
	})
	tap := newLogTap()
	s := systemNode(t, IS04NodeConfig{}, tap)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if g := s.fetchSystemGlobal(ctx); g != nil {
		t.Errorf("unreadable /global returned %+v", g)
	}
	if !tap.has("System API found but /global could not be read") {
		t.Errorf("the unreadable global must be warned; saw %v", tap.snapshot())
	}
}

// A discovered, readable System API is applied and then watched, and the
// watcher is built once however often the fetch runs.
func TestFetchSystemGlobalDiscoveredAndWatchedOnce(t *testing.T) {
	api := newSystemAPI(t, "discovered")
	fb := newFakeBrowser()
	useFakeBrowser(t, fb)
	scriptSystemDiscovery(t, func() ([]dnssdcodec.Instance, error) {
		return []dnssdcodec.Instance{api.instance(t, "sys", 1, 120)}, nil
	})
	tap := newLogTap()
	s := systemNode(t, IS04NodeConfig{}, tap)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := s.fetchSystemGlobal(ctx)
	if got == nil || got.Label != "discovered" {
		t.Fatalf("fetchSystemGlobal = %+v", got)
	}
	s.mu.Lock()
	first := s.systemWatcher
	s.mu.Unlock()
	if first == nil {
		t.Fatal("a Node that found a System API keeps watching for a better one")
	}
	s.watchForSystem(ctx)
	s.mu.Lock()
	second := s.systemWatcher
	s.mu.Unlock()
	if first != second {
		t.Error("the watcher must be built once")
	}
}

// A Node that cannot open a multicast socket says so and carries on.
func TestWatchForSystemReportsNoSocket(t *testing.T) {
	useFakeBrowser(t, nil)
	tap := newLogTap()
	s := systemNode(t, IS04NodeConfig{}, tap)
	s.watchForSystem(context.Background())
	if !tap.has("cannot watch for a System API") {
		t.Errorf("the failure must be logged; saw %v", tap.snapshot())
	}
	s.mu.Lock()
	watcher := s.systemWatcher
	s.mu.Unlock()
	if watcher != nil {
		t.Error("no watcher must be stored when one could not be built")
	}
}

// A watcher that cannot start its browse is reported; the Node keeps
// serving.
func TestWatchForSystemReportsBrowseFailure(t *testing.T) {
	fb := newFakeBrowser()
	fb.browseErr[dnssdcodec.ServiceSystem] = errors.New("group unavailable")
	useFakeBrowser(t, fb)
	tap := newLogTap()
	s := systemNode(t, IS04NodeConfig{}, tap)
	s.watchForSystem(context.Background())
	if !tap.has("System API watch failed to start") {
		t.Errorf("the failed start must be logged; saw %v", tap.snapshot())
	}
}
