package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	dnssdcodec "dhs/internal/amwa/codec/dnssd"
	"dhs/internal/amwa/codec/is09"
)

// systemAPI is an IS-09 System API that serves /global — or refuses to,
// when the test says so — and reports how often it was read.
type systemAPI struct {
	ts    *httptest.Server
	fail  bool
	hits  chan struct{}
	global is09.Global
}

func newSystemAPI(t *testing.T, domain string) *systemAPI {
	t.Helper()
	a := &systemAPI{hits: make(chan struct{}, 16)}
	a.global = is09.Global{
		ID:          "6e0a6e0a-0000-4000-8000-00000000000a",
		Version:     "1600000000:0",
		Label:       domain,
		Description: "system api under test",
		Tags:        map[string][]string{},
		IS04:        is09.IS04Config{HeartbeatInterval: 5},
		PTP:         is09.PTPConfig{AnnounceReceiptTimeout: 3, DomainNumber: 127},
	}
	mux := stdhttp.NewServeMux()
	mux.HandleFunc("/x-nmos/system/v1.0/global", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		select {
		case a.hits <- struct{}{}:
		default:
		}
		if a.fail {
			w.WriteHeader(stdhttp.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(a.global)
	})
	a.ts = httptest.NewServer(mux)
	t.Cleanup(a.ts.Close)
	return a
}

// instance is the advertisement a browser would deliver for this server:
// the IPv4 address wins over the SRV host, exactly as the fetcher reads it.
func (a *systemAPI) instance(t *testing.T, name string, pri int, ttl uint32) dnssdcodec.Instance {
	t.Helper()
	host, portStr, err := net.SplitHostPort(strings.TrimPrefix(a.ts.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)
	return dnssdcodec.Instance{
		Name: name, Service: dnssdcodec.ServiceSystem, Domain: "local",
		Host: "sys-" + name + ".local", Port: uint16(port), TTL: ttl,
		IPv4: []net.IP{net.ParseIP(host)},
		TXT: map[string]string{
			dnssdcodec.TXTKeyAPIProto: "http",
			dnssdcodec.TXTKeyAPIVer:   "v1.0",
			dnssdcodec.TXTKeyPriority: strconv.Itoa(pri),
		},
	}
}

// A watcher cannot be built without a multicast socket, and Run reports a
// browse that cannot be opened; Close is safe in either state.
func TestSystemWatcherConstructorAndRunFailures(t *testing.T) {
	useFakeBrowser(t, nil)
	if _, err := NewSystemWatcher(nil, "", nil); err == nil {
		t.Fatal("a watcher without a multicast socket must be refused")
	}

	fb := newFakeBrowser()
	fb.browseErr[dnssdcodec.ServiceSystem] = errors.New("group unavailable")
	fb.closeErr = errors.New("close failed")
	useFakeBrowser(t, fb)
	w, err := NewSystemWatcher(nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Run(context.Background()); err == nil {
		t.Error("Run must report a browse it could not open")
	}
	if err := w.Close(); err == nil {
		t.Error("Close must report the browser's own failure")
	}
}

// The watcher fetches /global from the highest-priority System API it is
// told about, hands it to the callback exactly once, and stops when its
// context ends.
func TestSystemWatcherFetchesBestAndStops(t *testing.T) {
	low := newSystemAPI(t, "low")   // pri 10
	high := newSystemAPI(t, "high") // pri 1 — wins
	fb := newFakeBrowser()
	useFakeBrowser(t, fb)

	type call struct {
		url   string
		label string
	}
	got := make(chan call, 8)
	w, err := NewSystemWatcher(newLogTap().logger(), "v1.0", func(g any, url string) {
		global, ok := g.(*is09.Global)
		if !ok {
			t.Errorf("callback got %T, want *is09.Global", g)
			return
		}
		got <- call{url: url, label: global.Label}
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := w.Run(ctx); err != nil {
		t.Fatal(err)
	}
	feed := fb.feed(t, dnssdcodec.ServiceSystem)
	feed <- low.instance(t, "low", 10, 120)
	first := <-got
	if first.label != "low" {
		t.Fatalf("first fetch = %+v, want the only instance", first)
	}
	feed <- high.instance(t, "high", 1, 120)
	second := <-got
	if second.label != "high" || !strings.Contains(second.url, "/x-nmos/system/v1.0/global") {
		t.Fatalf("second fetch = %+v, want the higher-priority instance", second)
	}

	// The same advertisement again is not re-fetched: the watcher already
	// holds that instance's /global.
	before := len(high.hits)
	feed <- high.instance(t, "high", 1, 120)
	time.Sleep(50 * time.Millisecond)
	if len(high.hits) != before {
		t.Error("an unchanged advertisement must not be re-fetched")
	}

	cancel()
	if err := w.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// A goodbye (TTL 0) forgets the instance, so the next advertisement from
// it is fetched again; an instance whose /global cannot be read is
// remembered as failed and the watcher falls through to the next one.
func TestSystemWatcherGoodbyeAndFailover(t *testing.T) {
	broken := newSystemAPI(t, "broken")
	broken.fail = true
	good := newSystemAPI(t, "good")
	fb := newFakeBrowser()
	useFakeBrowser(t, fb)

	got := make(chan string, 8)
	tap := newLogTap()
	w, err := NewSystemWatcher(tap.logger(), "v1.0", func(g any, _ string) {
		got <- g.(*is09.Global).Label
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	feed := fb.feed(t, dnssdcodec.ServiceSystem)

	// The broken one wins on priority but cannot be read; the watcher
	// records the failure and reads the other.
	feed <- broken.instance(t, "broken", 1, 120)
	tap.wait(t, "/global could not be read")
	feed <- good.instance(t, "good", 10, 120)
	if label := <-got; label != "good" {
		t.Fatalf("fetched %q, want the readable instance", label)
	}

	// A goodbye for the fetched instance clears it, so the same
	// advertisement is honoured again.
	gone := good.instance(t, "good", 10, 0)
	feed <- gone
	feed <- good.instance(t, "good", 10, 120)
	if label := <-got; label != "good" {
		t.Fatalf("after a goodbye the instance must be fetched again, got %q", label)
	}
}

// An advertisement with no address of its own reuses the address a
// previous advertisement carried for the same host.
func TestSystemWatcherRemembersHostAddress(t *testing.T) {
	api := newSystemAPI(t, "addr")
	fb := newFakeBrowser()
	useFakeBrowser(t, fb)
	got := make(chan string, 4)
	w, err := NewSystemWatcher(newLogTap().logger(), "v1.0", func(g any, _ string) {
		got <- g.(*is09.Global).Label
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()

	feed := fb.feed(t, dnssdcodec.ServiceSystem)
	withAddr := api.instance(t, "a", 5, 120)
	feed <- withAddr
	<-got

	// A second instance on the same host, advertised without an address of
	// its own, and at a priority that wins outright.
	noAddr := api.instance(t, "b", 1, 120)
	noAddr.IPv4 = nil
	noAddr.Host = withAddr.Host
	feed <- noAddr
	if label := <-got; label != "addr" {
		t.Fatalf("fetched %q; the cached host address must have been reused", label)
	}
}

// consume returns when its channel closes, and dropInstance removes only
// the named instance.
func TestSystemWatcherConsumeCloseAndDropInstance(t *testing.T) {
	fb := newFakeBrowser()
	useFakeBrowser(t, fb)
	w, err := NewSystemWatcher(nil, "v1.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan dnssdcodec.Instance)
	done := make(chan struct{})
	go func() { w.consume(context.Background(), ch); close(done) }()
	close(ch)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("consume did not return when its channel closed")
	}

	in := []dnssdcodec.Instance{
		{Name: "a", Service: dnssdcodec.ServiceSystem},
		{Name: "b", Service: dnssdcodec.ServiceSystem},
	}
	out := dropInstance(in, "a."+dnssdcodec.ServiceSystem)
	if len(out) != 1 || out[0].Name != "b" {
		t.Errorf("dropInstance = %+v, want only b", out)
	}
	if len(dropInstance(out, "b."+dnssdcodec.ServiceSystem)) != 0 {
		t.Error("dropping the last instance must leave nothing")
	}
}

// An advertisement that names no usable version selects nothing, and a
// watcher with no callback still records what it fetched.
func TestSystemWatcherNoMatchAndNoCallback(t *testing.T) {
	api := newSystemAPI(t, "quiet")
	fb := newFakeBrowser()
	useFakeBrowser(t, fb)
	tap := newLogTap()
	w, err := NewSystemWatcher(tap.logger(), "v1.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	feed := fb.feed(t, dnssdcodec.ServiceSystem)

	wrongVer := api.instance(t, "old", 1, 120)
	wrongVer.TXT[dnssdcodec.TXTKeyAPIVer] = "v9.9"
	feed <- wrongVer
	tap.wait(t, "System API advertisement observed")
	if len(api.hits) != 0 {
		t.Error("an instance serving no version we speak must not be fetched")
	}

	feed <- api.instance(t, "ok", 1, 120)
	deadline := time.Now().Add(5 * time.Second)
	for len(api.hits) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(api.hits) == 0 {
		t.Error("a matching instance must be fetched even with no callback")
	}
}
