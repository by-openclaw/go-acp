package provider

// RegistryWatcher against a scripted browser: the IS-04 §3.1 folding
// rules (both service names, A-record aggregation across packets,
// re-announcement clearing a penalty), the browse-loop lifecycle, and
// the URL / version / protocol helpers the folding relies on.

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	dnssdcodec "dhs/internal/amwa/codec/dnssd"
)

func regTXT(apiVer, pri string) map[string]string {
	return map[string]string{
		dnssdcodec.TXTKeyAPIProto: "http",
		dnssdcodec.TXTKeyAPIVer:   apiVer,
		dnssdcodec.TXTKeyPriority: pri,
	}
}

func TestNewRegistryWatcherFailsWithoutBrowser(t *testing.T) {
	useFakeBrowser(t, nil)
	if _, err := NewRegistryWatcher(nil, ""); err == nil {
		t.Fatal("a browser that cannot open must fail the constructor, not yield a deaf watcher")
	}
}

func TestNewRegistryWatcherDefaultsPreferredVersion(t *testing.T) {
	useFakeBrowser(t, newFakeBrowser())
	w, err := NewRegistryWatcher(nil, "")
	if err != nil {
		t.Fatalf("NewRegistryWatcher: %v", err)
	}
	if w.preferAPIVer != "v1.3" {
		t.Errorf("preferAPIVer = %q, want the v1.3 default", w.preferAPIVer)
	}
}

func TestRegistryWatcherRunBrowsesBothServiceNames(t *testing.T) {
	cases := []struct {
		name    string
		failing string
	}{
		{"modern name fails to open", dnssdcodec.ServiceRegister},
		{"legacy name fails to open", dnssdcodec.ServiceRegisterLegacy},
		{"both open", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fb := newFakeBrowser()
			if tc.failing != "" {
				fb.browseErr[tc.failing] = errors.New("socket gone")
			}
			useFakeBrowser(t, fb)
			w, err := NewRegistryWatcher(nil, "v1.3")
			if err != nil {
				t.Fatalf("NewRegistryWatcher: %v", err)
			}
			err = w.Run(context.Background())
			if tc.failing != "" {
				if err == nil {
					t.Fatal("Run must surface a browse that failed to open")
				}
				return
			}
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			fb.feed(t, dnssdcodec.ServiceRegister)
			fb.feed(t, dnssdcodec.ServiceRegisterLegacy)
			if err := w.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			// Idempotent: the second Close finds no loop to cancel.
			if err := w.Close(); err != nil {
				t.Fatalf("second Close: %v", err)
			}
			if fb.closed != 2 {
				t.Errorf("browser closed %d times, want 2 (Close is idempotent, and always releases the transport)", fb.closed)
			}
		})
	}
}

func TestRegistryWatcherCloseWithoutBrowser(t *testing.T) {
	w := &RegistryWatcher{}
	if err := w.Close(); err != nil {
		t.Fatalf("Close on a bare watcher: %v", err)
	}
}

// consume drives the whole folding rule set through the two browse
// channels, with the log tap as the only clock.
func TestRegistryWatcherConsumeFoldsAdvertisements(t *testing.T) {
	tap := newLogTap()
	w := &RegistryWatcher{
		logger: tap.logger(), preferAPIVer: "v1.3", disqualifyTTL: time.Minute,
		byFull: map[string]RegistryCandidate{}, disqualified: map[string]time.Time{},
		hostIPv4: map[string]string{},
	}
	modern := make(chan dnssdcodec.Instance)
	legacy := make(chan dnssdcodec.Instance)
	w.outModern, w.outLegacy = modern, legacy
	done := make(chan struct{})
	go func() { w.consume(context.Background()); close(done) }()

	// A Registry that does not speak our version is rejected, and
	// said so (AMWA test_01_01).
	modern <- registryInstance("old", dnssdcodec.ServiceRegister, "old.local.", 8235, regTXT("v1.0", "1"))
	tap.wait(t, "registry rejected")
	if len(w.All()) != 0 {
		t.Fatalf("rejected registry must not be a candidate: %+v", w.All())
	}

	// A hostname-only SRV on the legacy name is kept as-is …
	legacy <- registryInstance("reg-a", dnssdcodec.ServiceRegisterLegacy, "shared.local.", 8235, regTXT("v1.3", "10"))
	tap.wait(t, "registry discovered")
	best, ok := w.Best()
	if !ok || best.URL != "http://shared.local:8235" {
		t.Fatalf("Best = %+v ok=%v, want the hostname URL before any A record", best, ok)
	}
	fullA := best.FullName

	// … until an A record for that host arrives in ANOTHER packet:
	// the earlier candidate is rewritten retroactively.
	withA := registryInstance("reg-b", dnssdcodec.ServiceRegister, "shared.local.", 8236, regTXT("v1.3", "20"))
	withA.IPv4 = []net.IP{net.IPv4(10, 0, 0, 9).To4()}
	modern <- withA
	tap.wait(t, "registry discovered")
	all := w.All()
	if len(all) != 2 || all[0].FullName != fullA || all[0].URL != "http://10.0.0.9:8235" {
		t.Fatalf("All = %+v, want reg-a first (pri 10) rewritten to the cached A record", all)
	}
	if all[1].URL != "http://10.0.0.9:8236" {
		t.Fatalf("reg-b URL = %q, want its own A record", all[1].URL)
	}

	// A re-announcement clears a standing disqualification.
	w.Disqualify(fullA)
	if b, _ := w.Best(); b.FullName == fullA {
		t.Fatal("disqualified registry must not be Best")
	}
	legacy <- registryInstance("reg-a", dnssdcodec.ServiceRegisterLegacy, "shared.local.", 8235, regTXT("v1.3", "10"))
	tap.wait(t, "registry discovered")
	if b, _ := w.Best(); b.FullName != fullA {
		t.Fatalf("Best after re-announce = %+v, want reg-a restored", b)
	}

	// One browse ending does not end the watcher; both ending does.
	close(legacy)
	modern <- registryInstance("reg-c", dnssdcodec.ServiceRegister, "c.local.", 1, regTXT("v1.3", "30"))
	tap.wait(t, "registry discovered")
	close(modern)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("consume did not return after both browse channels closed")
	}
}

func TestRegistryWatcherConsumeStopsOnContext(t *testing.T) {
	w := &RegistryWatcher{byFull: map[string]RegistryCandidate{}, disqualified: map[string]time.Time{}, hostIPv4: map[string]string{}}
	w.outModern, w.outLegacy = make(chan dnssdcodec.Instance), make(chan dnssdcodec.Instance)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.consume(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("consume did not return on cancel")
	}
}

// Without a logger the folding still runs — the rejected and the
// discovered branches both guard their log line.
func TestRegistryWatcherConsumeQuietWithoutLogger(t *testing.T) {
	w := &RegistryWatcher{
		preferAPIVer: "v1.3", byFull: map[string]RegistryCandidate{},
		disqualified: map[string]time.Time{}, hostIPv4: map[string]string{},
	}
	modern := make(chan dnssdcodec.Instance)
	legacy := make(chan dnssdcodec.Instance)
	w.outModern, w.outLegacy = modern, legacy
	done := make(chan struct{})
	go func() { w.consume(context.Background()); close(done) }()
	modern <- registryInstance("old", dnssdcodec.ServiceRegister, "old.local.", 8235, regTXT("v1.0", "1"))
	modern <- registryInstance("new", dnssdcodec.ServiceRegister, "new.local.", 8235, regTXT("v1.3", "1"))
	close(modern)
	close(legacy)
	<-done
	if b, ok := w.Best(); !ok || b.FullName != "new."+dnssdcodec.ServiceRegister+"."+dnssdcodec.DefaultDomain {
		t.Fatalf("Best = %+v ok=%v", b, ok)
	}
}

func TestRewriteURLLocked(t *testing.T) {
	w := &RegistryWatcher{hostIPv4: map[string]string{"reg.local": "10.1.1.1"}}
	cases := []struct{ name, in, want string }{
		{"hostname with port and path is rewritten", "http://reg.local:8235/x-nmos", "http://10.1.1.1:8235/x-nmos"},
		{"query string counts as path", "http://reg.local:8235?x=1", "http://10.1.1.1:8235?x=1"},
		{"no port keeps no port", "http://reg.local", "http://10.1.1.1"},
		{"no scheme still resolves the host", "reg.local:1", "://10.1.1.1:1"},
		{"IPv4 literal is left alone", "http://10.9.9.9:8235", "http://10.9.9.9:8235"},
		{"unknown host is left alone", "http://other.local:8235", "http://other.local:8235"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := w.rewriteURLLocked(tc.in); got != tc.want {
				t.Errorf("rewriteURLLocked(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsIPv4Literal(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"10.0.0.1", true},
		{"reg.local", false},
		{"1.2.3", false},
		{"1..2.3", false},
		{"1.2.3.x", false},
	}
	for _, tc := range cases {
		if got := isIPv4Literal(tc.in); got != tc.want {
			t.Errorf("isIPv4Literal(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestPickAPIVerAndSupportedProto(t *testing.T) {
	verCases := []struct{ name, advert, preferred, want string }{
		{"no advert trusts the preference", "", "v1.3", "v1.3"},
		{"only separators is no advert", " , ", "v1.3", "v1.3"},
		{"preferred present", "v1.2, v1.3", "v1.3", "v1.3"},
		{"preferred absent rejects", "v1.0,v1.1", "v1.3", ""},
	}
	for _, tc := range verCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pickAPIVer(tc.advert, tc.preferred); got != tc.want {
				t.Errorf("pickAPIVer(%q, %q) = %q, want %q", tc.advert, tc.preferred, got, tc.want)
			}
		})
	}
	protoCases := []struct {
		in   string
		want bool
	}{
		{" HTTP ", true},
		{"https", false},
		{"gopher", false},
	}
	for _, tc := range protoCases {
		if got := isSupportedProto(tc.in); got != tc.want {
			t.Errorf("isSupportedProto(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// Equal priority across DIFFERENT URLs orders by FullName, so the
// pick is stable across map iterations.
func TestBestCandidateTiesByFullName(t *testing.T) {
	byFull := map[string]RegistryCandidate{
		"zed.local.":   {FullName: "zed.local.", URL: "http://z", Priority: 5},
		"alpha.local.": {FullName: "alpha.local.", URL: "http://a", Priority: 5},
	}
	for i := 0; i < 50; i++ {
		got, ok := bestCandidate(byFull, map[string]time.Time{})
		if !ok || got.FullName != "alpha.local." {
			t.Fatalf("bestCandidate = %+v ok=%v, want alpha (name tie-break)", got, ok)
		}
	}
}
