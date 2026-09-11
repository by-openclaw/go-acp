package system

import (
	"context"
	"errors"
	"log/slog"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	dnssdcodec "dhs/internal/amwa/codec/dnssd"
	"dhs/internal/amwa/codec/is09"
	dnssdsession "dhs/internal/amwa/session/dnssd"
)

// globalServer serves a minimal IS-09 /global and returns its host and
// port. The body is the shape a conforming System API publishes; the
// tests that care about a deviating one build their own.
func globalServer(t *testing.T) (host string, port uint16) {
	t.Helper()
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if !strings.HasSuffix(r.URL.Path, "/global") {
			stdhttp.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"6c6e2d15-b4e0-4b6a-9a3f-1c0f0b3a9f01",
			"version":"1600000000:0",
			"label":"system",
			"description":"test system",
			"tags":{}
		}`))
	}))
	t.Cleanup(srv.Close)

	h, p, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		t.Fatal(err)
	}
	return h, uint16(n)
}

// A Fetch that names nothing takes the IS-09 defaults — v1.0 over
// plain HTTP — and builds its own client. With no instances to choose
// from it says so rather than guessing at one.
func TestFetchWithoutOptionsTakesTheDefaults(t *testing.T) {
	_, err := Fetch(context.Background(), IS09FetchOptions{})
	if !errors.Is(err, ErrNoInstances) {
		t.Fatalf("= %v, want ErrNoInstances", err)
	}
}

// --direct skips discovery: the operator already knows where the
// System API is, and asking mDNS to rediscover it is how a Node ends
// up unable to reach one that is sitting right there.
func TestFetchDirectSkipsDiscovery(t *testing.T) {
	host, port := globalServer(t)

	res, err := Fetch(context.Background(), IS09FetchOptions{
		Direct: net.JoinHostPort(host, strconv.Itoa(int(port))),
	})
	if err != nil {
		t.Fatalf("direct fetch: %v", err)
	}
	if res.Selected.Name != "direct" {
		t.Errorf("selected %q, want the synthesised direct instance", res.Selected.Name)
	}
	if !strings.HasSuffix(res.URL, "/x-nmos/system/"+is09.APIVersion+"/global") {
		t.Errorf("url = %q, want the IS-09 global path", res.URL)
	}
	if res.Global == nil {
		t.Error("a successful fetch must carry the Global")
	}
}

// A --direct that is not host:port is refused before anything is
// dialled, and the refusal quotes what the operator typed.
func TestFetchDirectRefusesWhatItCannotParse(t *testing.T) {
	for _, tc := range []struct{ direct, want string }{
		{"no-port", "bad --direct"},
		{"host:0", "bad --direct port"},
		{"host:70000", "bad --direct port"},
		{"host:not-a-number", "bad --direct port"},
	} {
		_, err := Fetch(context.Background(), IS09FetchOptions{Direct: tc.direct})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("--direct %q = %v, want %q", tc.direct, err, tc.want)
		}
	}
}

// An instance advertising a different service is not a System API,
// whatever its TXT record claims.
func TestSelectInstanceIgnoresAnotherService(t *testing.T) {
	insts := []dnssdcodec.Instance{{
		Name:    "a-registry",
		Service: dnssdcodec.ServiceQuery,
		Host:    "registry.local",
		Port:    80,
		TXT: map[string]string{
			dnssdcodec.TXTKeyAPIProto: "http",
			dnssdcodec.TXTKeyAPIVer:   "v1.0",
		},
	}}
	if _, err := SelectInstance(insts, "http", "v1.0", nil); !errors.Is(err, ErrNoInstances) {
		t.Fatalf("= %v, want ErrNoInstances", err)
	}
}

// Instances that were discovered but that none of them is a usable
// System API is not the same as having discovered nothing, and Fetch
// reports the selection rule's refusal rather than dialling one it
// already filtered out.
func TestFetchReportsASelectionThatMatchesNothing(t *testing.T) {
	_, err := Fetch(context.Background(), IS09FetchOptions{
		APIProto: "https",
		Discovered: []dnssdcodec.Instance{{
			Name:    "sys",
			Service: dnssdcodec.ServiceSystem,
			Host:    "sys.local",
			Port:    80,
			TXT: map[string]string{
				dnssdcodec.TXTKeyAPIProto: "http", // the operator asked for https
				dnssdcodec.TXTKeyAPIVer:   "v1.0",
			},
		}},
	})
	if !errors.Is(err, ErrNoInstances) {
		t.Fatalf("= %v, want ErrNoInstances", err)
	}
}

// The tie-break is reported when there is one to report: an operator
// wondering why two runs picked different System APIs should find the
// answer in the log rather than in the spec.
func TestSelectInstanceLogsATieBreak(t *testing.T) {
	tie := func(name string) dnssdcodec.Instance {
		return dnssdcodec.Instance{
			Name:    name,
			Service: dnssdcodec.ServiceSystem,
			Host:    name + ".local",
			Port:    80,
			TXT: map[string]string{
				dnssdcodec.TXTKeyAPIProto: "http",
				dnssdcodec.TXTKeyAPIVer:   "v1.0",
				dnssdcodec.TXTKeyPriority: "10",
			},
		}
	}
	tap := &logTap{}
	if _, err := SelectInstance([]dnssdcodec.Instance{tie("a"), tie("b")},
		"http", "v1.0", slog.New(tap)); err != nil {
		t.Fatal(err)
	}
	if !tap.saw("tie-break") {
		t.Errorf("the tie-break must be reported; saw %v", tap.lines)
	}

	// One candidate is not a tie, and reporting it as one would train
	// the operator to ignore the message.
	tap2 := &logTap{}
	if _, err := SelectInstance([]dnssdcodec.Instance{tie("a")},
		"http", "v1.0", slog.New(tap2)); err != nil {
		t.Fatal(err)
	}
	if tap2.saw("tie-break") {
		t.Errorf("a single candidate is not a tie-break; saw %v", tap2.lines)
	}
}

// The advertised address wins over the SRV hostname: mDNS names its
// targets in .local, and a host without an mDNS resolver cannot look
// one up — while the A record travelled in the same announcement.
func TestFetchPrefersTheAdvertisedAddress(t *testing.T) {
	host, port := globalServer(t)

	res, err := Fetch(context.Background(), IS09FetchOptions{
		Discovered: []dnssdcodec.Instance{{
			Name:    "sys",
			Service: dnssdcodec.ServiceSystem,
			Host:    "unresolvable.invalid.", // the SRV name, deliberately dead
			IPv4:    []net.IP{net.ParseIP(host)},
			Port:    port,
			TXT: map[string]string{
				dnssdcodec.TXTKeyAPIProto: "http",
				dnssdcodec.TXTKeyAPIVer:   "v1.0",
			},
		}},
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !strings.Contains(res.URL, host) {
		t.Errorf("url = %q, want the advertised address, not the SRV name", res.URL)
	}
}

// A caller who set no deadline gets one: a System API that accepts the
// connection and then says nothing must not hold a Node's bootstrap
// open forever.
func TestFetchAppliesItsOwnDeadline(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		<-block
	}))
	t.Cleanup(func() { close(block); srv.Close() })

	prev := fetchTimeout
	fetchTimeout = 50 * time.Millisecond
	t.Cleanup(func() { fetchTimeout = prev })

	// context.Background() carries no deadline, so the one under test
	// is the only thing that ends this call.
	_, err := Fetch(context.Background(), IS09FetchOptions{
		Direct: strings.TrimPrefix(srv.URL, "http://"),
	})
	if err == nil || !strings.Contains(err.Error(), "GET ") {
		t.Fatalf("= %v, want the GET reported as failed", err)
	}
}

// ---------------------------------------------------------------
// discovery
// ---------------------------------------------------------------

// fakeBrowser answers one Browse with a fixed instance list.
type fakeBrowser struct {
	insts    []dnssdcodec.Instance
	browseEr error
	closed   bool
}

func (b *fakeBrowser) Browse(ctx context.Context, service string) (<-chan dnssdcodec.Instance, error) {
	if b.browseEr != nil {
		return nil, b.browseEr
	}
	ch := make(chan dnssdcodec.Instance, len(b.insts))
	for _, i := range b.insts {
		ch <- i
	}
	close(ch)
	return ch, nil
}

func (b *fakeBrowser) Close() error {
	b.closed = true
	return nil
}

func useBrowser(t *testing.T, br dnssdsession.Browser, err error) *fakeBrowser {
	t.Helper()
	prev := newBrowser
	newBrowser = func(*slog.Logger) (dnssdsession.Browser, error) { return br, err }
	t.Cleanup(func() { newBrowser = prev })
	fb, _ := br.(*fakeBrowser)
	return fb
}

// The browse is de-duplicated by full name — RFC 6762 lets the same
// instance be announced repeatedly, and a caller counting System APIs
// must not see one of them three times.
func TestDiscoverMDNSDeduplicates(t *testing.T) {
	one := dnssdcodec.Instance{Name: "sys", Service: dnssdcodec.ServiceSystem, Host: "a.local", Port: 80}
	two := dnssdcodec.Instance{Name: "other", Service: dnssdcodec.ServiceSystem, Host: "b.local", Port: 80}
	fb := useBrowser(t, &fakeBrowser{insts: []dnssdcodec.Instance{one, one, two}}, nil)

	got, err := DiscoverMDNS(context.Background(), time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d instances, want the two distinct ones", len(got))
	}
	if !fb.closed {
		t.Error("the browser must be closed when the browse ends")
	}
}

// A browser that will not open, and a browse that will not start, are
// both reported: a Node that cannot discover must say so rather than
// report an empty network.
func TestDiscoverMDNSReportsWhatItCannotDo(t *testing.T) {
	useBrowser(t, nil, errors.New("no socket"))
	if _, err := DiscoverMDNS(context.Background(), time.Second, nil); err == nil ||
		!strings.Contains(err.Error(), "no socket") {
		t.Errorf("a browser that will not open = %v", err)
	}

	useBrowser(t, &fakeBrowser{browseEr: errors.New("browse refused")}, nil)
	if _, err := DiscoverMDNS(context.Background(), time.Second, nil); err == nil ||
		!strings.Contains(err.Error(), "browse refused") {
		t.Errorf("a browse that will not start = %v", err)
	}
}

// Unicast discovery names the System service and, when the caller
// gives no domain, the DNS-SD default — RFC 6763 §11.
func TestDiscoverUnicastPassesServiceAndDomain(t *testing.T) {
	var gotService, gotDomain, gotResolver string
	prev := resolveUnicast
	resolveUnicast = func(_ context.Context, resolver, service, domain string, _ time.Duration) ([]dnssdcodec.Instance, error) {
		gotResolver, gotService, gotDomain = resolver, service, domain
		return []dnssdcodec.Instance{{Name: "sys"}}, nil
	}
	t.Cleanup(func() { resolveUnicast = prev })

	if _, err := DiscoverUnicast(context.Background(), "192.0.2.53:53", "", time.Second); err != nil {
		t.Fatal(err)
	}
	if gotResolver != "192.0.2.53:53" || gotService != dnssdcodec.ServiceSystem ||
		gotDomain != dnssdcodec.DefaultDomain {
		t.Errorf("resolver=%q service=%q domain=%q", gotResolver, gotService, gotDomain)
	}

	if _, err := DiscoverUnicast(context.Background(), "192.0.2.53:53", "lab.example.", time.Second); err != nil {
		t.Fatal(err)
	}
	if gotDomain != "lab.example." {
		t.Errorf("domain = %q, want the caller's", gotDomain)
	}
}

// ---------------------------------------------------------------
// tie-break entropy
// ---------------------------------------------------------------

// The tie-break asks crypto/rand. If the platform has no entropy to
// give, the pick is the first tied candidate — a deterministic choice
// among equals, not a failed bootstrap.
func TestSecureRandIndexFallsBackWhenEntropyRefuses(t *testing.T) {
	prev := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
	t.Cleanup(func() { randRead = prev })

	if got := secureRandIndex(5); got != 0 {
		t.Fatalf("= %d, want the first candidate", got)
	}
}

// The index is always inside the tied set, which is the only property
// the selection rule actually depends on.
func TestSecureRandIndexStaysInRange(t *testing.T) {
	if got := secureRandIndex(1); got != 0 {
		t.Fatalf("one candidate = %d, want 0", got)
	}
	for i := 0; i < 200; i++ {
		if got := secureRandIndex(3); got < 0 || got > 2 {
			t.Fatalf("= %d, want an index into the tied set", got)
		}
	}
}

// ---------------------------------------------------------------
// helpers
// ---------------------------------------------------------------

// logTap collects the messages a *slog.Logger is asked to emit.
type logTap struct {
	lines []string
}

func (h *logTap) Enabled(context.Context, slog.Level) bool { return true }

func (h *logTap) Handle(_ context.Context, r slog.Record) error {
	h.lines = append(h.lines, r.Message)
	return nil
}

func (h *logTap) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *logTap) WithGroup(string) slog.Handler      { return h }

func (h *logTap) saw(sub string) bool {
	for _, l := range h.lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}
