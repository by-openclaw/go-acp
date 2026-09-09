package provider

// The registration loop's less-travelled paths: a Node that follows a
// higher-priority Registry when one appears, a cascade through several
// Registries when the best one refuses, a republish that arrives at
// the wrong moment, and every way a request can fail before it is even
// sent.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	stdhttp "net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/transport"
)

// scriptedRegistries is a registrySource a test drives: Best returns
// whichever candidate is currently on top, and Disqualify drops it.
type scriptedRegistries struct {
	mu           sync.Mutex
	candidates   []RegistryCandidate
	disqualified []string
}

func (s *scriptedRegistries) Best() (RegistryCandidate, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.candidates) == 0 {
		return RegistryCandidate{}, false
	}
	return s.candidates[0], true
}

func (s *scriptedRegistries) Disqualify(fullName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disqualified = append(s.disqualified, fullName)
	if len(s.candidates) > 0 && s.candidates[0].FullName == fullName {
		s.candidates = s.candidates[1:]
	}
}

func (s *scriptedRegistries) set(fn func(*scriptedRegistries)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(s)
}

func (s *scriptedRegistries) refusedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.disqualified)
}

// candidateFor names a fake registry as the watcher would advertise it.
func candidateFor(name string, reg *fakeRegistry, pri int) RegistryCandidate {
	return RegistryCandidate{
		FullName: name + "._nmos-register._tcp.local",
		URL:      reg.ts.URL,
		Priority: pri,
		APIVer:   "v1.3",
		APIProto: "http",
	}
}

// A client with no wire version takes the one this build serves,
// rather than posting to a path with an empty version in it.
func TestRegistrationClientDefaultsItsAPIVersion(t *testing.T) {
	c := NewRegistrationClient(newLogTap().logger(), "http://registry.invalid:8235", "", validBundle())
	if c.apiVer != is04.APIVersion {
		t.Fatalf("apiVer = %q, want %q", c.apiVer, is04.APIVersion)
	}
}

// IS-04 §3.1: the Node registers with the highest-priority Registry
// currently advertised. One that appears after registration is
// followed — DELETE from the old, register with the new — because the
// operator set those priorities to be obeyed.
func TestRegistrationFollowsABetterRegistry(t *testing.T) {
	first, second := newFakeRegistry(t), newFakeRegistry(t)
	src := &scriptedRegistries{candidates: []RegistryCandidate{candidateFor("a", first, 10)}}

	c := NewRegistrationClient(newLogTap().logger(), "", "v1.3", fullBundle(t))
	c.SetWatcher(src)
	runClient(t, c)

	waitUntil(t, "the first registration", func() bool {
		posted, _ := first.snapshot()
		return len(posted) > 0
	})

	// A better one appears.
	src.set(func(s *scriptedRegistries) {
		s.candidates = []RegistryCandidate{candidateFor("b", second, 1)}
	})

	waitUntil(t, "the switch to the better registry", func() bool {
		posted, _ := second.snapshot()
		return len(posted) > 0
	})
	if _, deleted := first.snapshot(); len(deleted) == 0 {
		t.Error("the Node must deregister from the Registry it left")
	}
}

// AMWA test_15: when the best Registry refuses, the Node works down
// the advertised list within the tick rather than waiting a whole
// heartbeat interval per attempt.
func TestRegistrationCascadesThroughRefusingRegistries(t *testing.T) {
	bad, good := newFakeRegistry(t), newFakeRegistry(t)
	bad.set(func(r *fakeRegistry) { r.postFail = true })

	src := &scriptedRegistries{candidates: []RegistryCandidate{
		candidateFor("bad", bad, 1),
		candidateFor("good", good, 10),
	}}

	c := NewRegistrationClient(newLogTap().logger(), "", "v1.3", fullBundle(t))
	c.SetWatcher(src)
	runClient(t, c)

	waitUntil(t, "the cascade to reach the working registry", func() bool {
		posted, _ := good.snapshot()
		return len(posted) > 0
	})
	if src.refusedCount() == 0 {
		t.Error("the refusing Registry must be disqualified, not retried forever")
	}
}

// With nothing advertised there is nothing to register with, and the
// loop waits rather than posting into the void.
func TestRegistrationWaitsWhenNoRegistryIsAdvertised(t *testing.T) {
	src := &scriptedRegistries{}
	c := NewRegistrationClient(newLogTap().logger(), "", "v1.3", validBundle())
	c.SetWatcher(src)
	runClient(t, c)

	time.Sleep(300 * time.Millisecond)
	if c.registered.Load() {
		t.Error("a Node with no Registry in sight must not think it is registered")
	}
}

// A republish that arrives while the Node is not registered is
// dropped: the next registration posts everything fresh anyway, and
// posting one resource to a Registry that has none of the others is
// how a Query API ends up with a Sender whose Device is unknown.
func TestRepublishIsDroppedWhileUnregistered(t *testing.T) {
	reg := newFakeRegistry(t)
	reg.set(func(r *fakeRegistry) { r.postFail = true })

	c := NewRegistrationClient(newLogTap().logger(), reg.ts.URL, "v1.3", validBundle())
	runClient(t, c)

	time.Sleep(50 * time.Millisecond)
	c.Republish(is04.ResourceNode, &c.bundle.Node)
	time.Sleep(200 * time.Millisecond)

	if c.registered.Load() {
		t.Error("a Registry refusing every POST must not leave the Node registered")
	}
}

// A republish the Registry refuses is reported and counted, not
// retried into silence: the Query API is now describing a resource
// that no longer looks like that, and the operator needs to know.
func TestRepublishFailureIsReportedAndCounted(t *testing.T) {
	reg := newFakeRegistry(t)
	tap := newLogTap()

	c := NewRegistrationClient(tap.logger(), reg.ts.URL, "v1.3", validBundle())
	runClient(t, c)
	waitUntil(t, "the initial registration", c.registered.Load)

	reg.set(func(r *fakeRegistry) { r.postFail = true })
	c.Republish(is04.ResourceNode, &c.bundle.Node)

	tap.until(t, "republish failed")
	waitUntil(t, "the failure to be counted", func() bool {
		return c.Stats()["failures"] > 0
	})
}

// ---------------------------------------------------------------
// requests that fail before they are sent
// ---------------------------------------------------------------

// withBrokenBase points the client at a base URL no request can be
// built from, so the pre-flight arms run without a network at all.
func withBrokenBase(t *testing.T, c *RegistrationClient) {
	t.Helper()
	c.mu.Lock()
	c.base = "http://registry.invalid/\x7f"
	c.mu.Unlock()
}

// A URL that cannot become a request is reported where it happens.
// The alternative is a Node that reports a healthy registration it
// never attempted.
func TestRequestsThatCannotBeBuiltAreReported(t *testing.T) {
	tap := newLogTap()
	c := NewRegistrationClient(tap.logger(), "http://registry.invalid:8235", "v1.3", validBundle())
	withBrokenBase(t, c)

	if err := c.sendHeartbeat(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "build heartbeat") {
		t.Errorf("heartbeat = %v, want the build failure reported", err)
	}
	if _, err := c.postResourceOnce(context.Background(), is04.ResourceNode, &c.bundle.Node); err == nil {
		t.Error("a POST that cannot be built must be reported")
	}
	c.deleteResource(context.Background(), is04.ResourceNode, c.bundle.Node.ID)
	if !tap.has("build DELETE") {
		t.Errorf("a DELETE that cannot be built must be reported; saw %v", tap.snapshot())
	}
}

// BCP-003-02: every request carries the Node's access token, so an
// Authorization Server that will not mint one stops the request rather
// than sending it unauthenticated for the Registry to refuse.
func TestRequestsWithoutATokenAreNotSent(t *testing.T) {
	reg := newFakeRegistry(t)
	tap := newLogTap()
	c := NewRegistrationClient(tap.logger(), reg.ts.URL, "v1.3", validBundle())
	c.SetTokenSource(func(context.Context) (string, error) {
		return "", errors.New("the authorization server is down")
	})

	if err := c.sendHeartbeat(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "obtain access token") {
		t.Errorf("heartbeat = %v, want the token failure reported", err)
	}
	if _, err := c.postResourceOnce(context.Background(), is04.ResourceNode, &c.bundle.Node); err == nil {
		t.Error("a POST with no token must not be sent")
	}
	c.deleteResource(context.Background(), is04.ResourceNode, c.bundle.Node.ID)
	if !tap.has("DELETE token") {
		t.Errorf("a DELETE with no token must be reported; saw %v", tap.snapshot())
	}
	if posted, deleted := reg.snapshot(); len(posted)+len(deleted) > 0 {
		t.Errorf("nothing must reach the Registry: posted=%v deleted=%v", posted, deleted)
	}
}

// A Registry that answers something other than 200 to a heartbeat is
// a failure with the Registry's own words in it — an operator reading
// the log should not have to go and reproduce the request.
func TestHeartbeatCarriesTheRegistrysRefusal(t *testing.T) {
	reg := newFakeRegistry(t)
	reg.set(func(r *fakeRegistry) { r.healthCode = stdhttp.StatusServiceUnavailable })

	c := NewRegistrationClient(newLogTap().logger(), reg.ts.URL, "v1.3", validBundle())
	if err := c.sendHeartbeat(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "503") {
		t.Fatalf("= %v, want the Registry's status reported", err)
	}
}

// Trust anchors that cannot be turned into a client posture leave the
// verifying stdlib default in place. Installing half a configuration
// would be the one outcome worse than not installing the roots.
func TestTLSRootsThatCannotBeAppliedLeaveTheDefault(t *testing.T) {
	c := NewRegistrationClient(newLogTap().logger(), "http://registry.invalid:8235", "v1.3", validBundle())
	before := c.http.Transport

	c.SetTLSRoots(nil) // nothing to install
	if c.http.Transport != before {
		t.Error("nil roots must not touch the transport")
	}

	prev := tlsClientConfig
	tlsClientConfig = func(transport.TLSOptions) (*tls.Config, error) {
		return nil, errors.New("refused")
	}
	t.Cleanup(func() { tlsClientConfig = prev })

	c.SetTLSRoots(x509.NewCertPool())
	if c.http.Transport != before {
		t.Error("a posture that could not be built must not be installed")
	}
}
