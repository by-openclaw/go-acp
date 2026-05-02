package provider

import (
	"testing"
	"time"

	dnssdcodec "acp/internal/amwa/codec/dnssd"
	"acp/internal/amwa/codec/is04"
)

func TestCandidateFromInstance(t *testing.T) {
	ins := dnssdcodec.Instance{
		Name:    "reg-1",
		Service: dnssdcodec.ServiceRegister,
		Domain:  "local",
		Host:    "reg-1.local",
		Port:    8235,
		TXT: map[string]string{
			dnssdcodec.TXTKeyAPIProto: "http",
			dnssdcodec.TXTKeyAPIVer:   "v1.2,v1.3",
			dnssdcodec.TXTKeyAPIAuth:  "false",
			dnssdcodec.TXTKeyPriority: "5",
		},
	}
	cand, ok := candidateFromInstance(ins, "v1.3")
	if !ok {
		t.Fatal("candidateFromInstance ok=false")
	}
	if cand.URL != "http://reg-1.local:8235" {
		t.Errorf("URL = %q", cand.URL)
	}
	if cand.Priority != 5 {
		t.Errorf("Priority = %d", cand.Priority)
	}
	if cand.APIVer != "v1.3" {
		t.Errorf("APIVer = %q (want v1.3 — preferred match)", cand.APIVer)
	}
	if cand.APIAuth {
		t.Errorf("APIAuth must be false")
	}
}

func TestCandidateRejectsRegistryWithoutPreferredVersion(t *testing.T) {
	// AMWA test_01_01 — Node must NOT register with a Registry that
	// doesn't advertise a version we speak.
	ins := dnssdcodec.Instance{
		Name:    "reg-2",
		Service: dnssdcodec.ServiceRegister,
		Domain:  "local",
		Host:    "reg-2.local",
		Port:    8235,
		TXT: map[string]string{
			dnssdcodec.TXTKeyAPIProto: "http",
			dnssdcodec.TXTKeyAPIVer:   "v1.0,v1.1",
		},
	}
	if _, ok := candidateFromInstance(ins, "v1.3"); ok {
		t.Error("candidateFromInstance ok=true; v1.3 not in {v1.0,v1.1} should reject")
	}
}

func TestCandidateRejectsHTTPS(t *testing.T) {
	// dhs doesn't speak https yet (IS-10 / TLS pending). Reject.
	ins := dnssdcodec.Instance{
		Name:    "reg-3",
		Service: dnssdcodec.ServiceRegister,
		Host:    "reg-3.local",
		Port:    8235,
		TXT: map[string]string{
			dnssdcodec.TXTKeyAPIProto: "https",
			dnssdcodec.TXTKeyAPIVer:   "v1.3",
		},
	}
	if _, ok := candidateFromInstance(ins, "v1.3"); ok {
		t.Error("candidateFromInstance ok=true for https; should reject")
	}
}

func TestCandidateRejectsEmptyHostPort(t *testing.T) {
	if _, ok := candidateFromInstance(dnssdcodec.Instance{Port: 0}, "v1.3"); ok {
		t.Error("ok must be false for empty Host/Port")
	}
}

// TestCandidateAcceptsLegacyServiceName documents that a Registry
// advertising on `_nmos-registration._tcp` (IS-04 v1.0/v1.1) is just as
// valid as one on the modern `_nmos-register._tcp` — candidateFromInstance
// is service-name-agnostic. The watcher's Run() method must browse both
// service names; a Node that filtered on service name alone would miss
// every legacy Registry. Closes #193.
func TestCandidateAcceptsLegacyServiceName(t *testing.T) {
	if dnssdcodec.ServiceRegisterLegacy != "_nmos-registration._tcp" {
		t.Fatalf("ServiceRegisterLegacy = %q, want _nmos-registration._tcp",
			dnssdcodec.ServiceRegisterLegacy)
	}
	ins := dnssdcodec.Instance{
		Name:    "reg-legacy",
		Service: dnssdcodec.ServiceRegisterLegacy,
		Domain:  "local",
		Host:    "reg-legacy.local",
		Port:    8235,
		TXT: map[string]string{
			dnssdcodec.TXTKeyAPIProto: "http",
			dnssdcodec.TXTKeyAPIVer:   "v1.0",
			dnssdcodec.TXTKeyPriority: "10",
		},
	}
	cand, ok := candidateFromInstance(ins, "v1.0")
	if !ok {
		t.Fatal("legacy-service Registry should be accepted when api_ver matches preferred")
	}
	if cand.APIVer != "v1.0" {
		t.Errorf("APIVer = %q (want v1.0)", cand.APIVer)
	}
	if cand.URL != "http://reg-legacy.local:8235" {
		t.Errorf("URL = %q", cand.URL)
	}
}

func TestRegistryWatcherSelectsByPriority(t *testing.T) {
	w := &RegistryWatcher{
		preferAPIVer:  "v1.3",
		disqualifyTTL: 30 * time.Second, // long enough to outlast the test
		byFull:        map[string]RegistryCandidate{},
		disqualified:  map[string]time.Time{},
	}
	w.byFull["reg-low.example."] = RegistryCandidate{FullName: "reg-low.example.", URL: "http://low", Priority: 50}
	w.byFull["reg-hi.example."] = RegistryCandidate{FullName: "reg-hi.example.", URL: "http://hi", Priority: 5}
	w.byFull["reg-mid.example."] = RegistryCandidate{FullName: "reg-mid.example.", URL: "http://mid", Priority: 20}

	best, ok := w.Best()
	if !ok || best.URL != "http://hi" {
		t.Fatalf("Best = %+v ok=%v, want URL=http://hi", best, ok)
	}

	w.Disqualify("reg-hi.example.")
	best, ok = w.Best()
	if !ok || best.URL != "http://mid" {
		t.Fatalf("after Disqualify(hi) Best = %+v ok=%v, want URL=http://mid", best, ok)
	}

	w.Disqualify("reg-mid.example.")
	best, ok = w.Best()
	if !ok || best.URL != "http://low" {
		t.Fatalf("after Disqualify(mid) Best = %+v ok=%v, want URL=http://low", best, ok)
	}

	w.Disqualify("reg-low.example.")
	if _, ok := w.Best(); ok {
		t.Fatalf("after disqualifying all, Best must return ok=false")
	}
}

// Cerebrum (and any IS-04 v1.2 transitional Registry) advertises the
// same URL on both _nmos-register._tcp (modern) AND
// _nmos-registration._tcp (legacy). Best() must dedupe by URL — without
// it, byFull's random map iteration makes Best() pick a different
// FullName each tick, and the registration loop's
// shouldSwitchToBetter() flap-switches every second between the two
// names, hammering the Registry with deregister + re-register pairs
// that show up as Senders/Receivers churning in the UI.
//
// Verified live on Proxmox LXC rig 2026-05-02 against Cerebrum @
// 10.100.0.5 advertising on both names.
func TestRegistryWatcherBestDedupesByURL(t *testing.T) {
	w := &RegistryWatcher{
		preferAPIVer:  "v1.3",
		disqualifyTTL: 30 * time.Second,
		byFull:        map[string]RegistryCandidate{},
		disqualified:  map[string]time.Time{},
	}
	// Cerebrum on both service names, same URL, same priority.
	w.byFull["Cerebrum._nmos-register._tcp.local."] = RegistryCandidate{
		FullName: "Cerebrum._nmos-register._tcp.local.",
		URL:      "http://10.100.0.5:8080", Priority: 0,
	}
	w.byFull["Cerebrum._nmos-registration._tcp.local."] = RegistryCandidate{
		FullName: "Cerebrum._nmos-registration._tcp.local.",
		URL:      "http://10.100.0.5:8080", Priority: 0,
	}

	// 100 calls in a row must return the SAME FullName — anything else
	// proves the flap.
	first, ok := w.Best()
	if !ok {
		t.Fatal("Best returned ok=false")
	}
	if first.URL != "http://10.100.0.5:8080" {
		t.Fatalf("Best.URL = %q, want http://10.100.0.5:8080", first.URL)
	}
	for i := 0; i < 100; i++ {
		got, _ := w.Best()
		if got.FullName != first.FullName {
			t.Fatalf("call %d returned FullName=%q, want stable %q", i, got.FullName, first.FullName)
		}
	}

	// Tie-breaker prefers _nmos-register (modern) over _nmos-registration
	// (legacy) — alphabetically earlier.
	if first.FullName != "Cerebrum._nmos-register._tcp.local." {
		t.Fatalf("Best.FullName = %q, want modern name (alphabetically first)", first.FullName)
	}

	// Disqualifying the modern name leaves only the legacy name —
	// Best() must promote the legacy entry, NOT return ok=false.
	w.Disqualify("Cerebrum._nmos-register._tcp.local.")
	got, ok := w.Best()
	if !ok {
		t.Fatal("Best ok=false after disqualifying only modern name; legacy should remain")
	}
	if got.FullName != "Cerebrum._nmos-registration._tcp.local." {
		t.Fatalf("Best.FullName = %q after disqualify(modern), want legacy", got.FullName)
	}
}

// Empty-URL pre-resolution stubs (DNS-SD instances seen before their A
// record arrives) must not appear as Best() — pickBase would synthesize
// an invalid base URL `/x-nmos/registration/v1.3` that 5xx'd every
// registration attempt.
func TestRegistryWatcherBestDropsEmptyURL(t *testing.T) {
	w := &RegistryWatcher{
		preferAPIVer:  "v1.3",
		disqualifyTTL: 30 * time.Second,
		byFull:        map[string]RegistryCandidate{},
		disqualified:  map[string]time.Time{},
	}
	w.byFull["pre-resolve.example."] = RegistryCandidate{
		FullName: "pre-resolve.example.", URL: "", Priority: 0,
	}
	w.byFull["resolved.example."] = RegistryCandidate{
		FullName: "resolved.example.", URL: "http://r:8080", Priority: 50,
	}
	got, ok := w.Best()
	if !ok || got.URL != "http://r:8080" {
		t.Fatalf("Best = %+v ok=%v, want URL=http://r:8080 (empty-URL skipped)", got, ok)
	}
}

func TestRegistryWatcherDisqualifyExpires(t *testing.T) {
	w := &RegistryWatcher{
		preferAPIVer:  "v1.3",
		disqualifyTTL: 1 * time.Millisecond,
		byFull:        map[string]RegistryCandidate{},
		disqualified:  map[string]time.Time{},
	}
	w.byFull["reg.example."] = RegistryCandidate{FullName: "reg.example.", URL: "http://r", Priority: 1}
	w.Disqualify("reg.example.")
	if _, ok := w.Best(); ok {
		t.Fatal("immediate Best after Disqualify must be empty")
	}
	time.Sleep(10 * time.Millisecond)
	if _, ok := w.Best(); !ok {
		t.Fatal("Best after disqualifyTTL elapsed must return the candidate again")
	}
}

func TestExpandNodeEndpointsAddsAdvertiseHost(t *testing.T) {
	n := &is04.Node{}
	expandNodeEndpoints(n, "dhs-node:18080", ":18080")
	found := false
	for _, e := range n.API.Endpoints {
		if e.Host == "dhs-node" && e.Port == 18080 && e.Protocol == "http" {
			found = true
		}
	}
	if !found {
		t.Fatalf("dhs-node:18080 not in endpoints: %+v", n.API.Endpoints)
	}
}

func TestExpandNodeEndpointsIdempotent(t *testing.T) {
	n := &is04.Node{}
	n.API.Endpoints = []is04.NodeEndpoint{
		{Host: "dhs-node", Port: 18080, Protocol: "http"},
	}
	expandNodeEndpoints(n, "dhs-node:18080", ":18080")
	count := 0
	for _, e := range n.API.Endpoints {
		if e.Host == "dhs-node" && e.Port == 18080 {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("dhs-node:18080 listed %d times, want 1: %+v", count, n.API.Endpoints)
	}
}

func TestClearUnservedManifestHrefs(t *testing.T) {
	href := "http://wrong/transportfile"
	senders := []is04.Sender{
		{ManifestHref: &href},
		{ManifestHref: &href},
	}
	clearUnservedManifestHrefs(senders)
	for i, s := range senders {
		if s.ManifestHref != nil {
			t.Errorf("senders[%d].ManifestHref = %q, want nil", i, *s.ManifestHref)
		}
	}
}
