package provider

import (
	"context"
	"strconv"
	"sync"
	"testing"

	dnssdcodec "dhs/internal/amwa/codec/dnssd"
	"dhs/internal/amwa/codec/is04"
	dnssdsession "dhs/internal/amwa/session/dnssd"
)

// mockResponder is a test-only [dnssdsession.Responder] that records the
// last Announce + every Update call so tests can assert on the TXT
// records that flowed through the IS-04 §3.1.1 P2P path.
type mockResponder struct {
	mu       sync.Mutex
	announce dnssdcodec.Instance
	updates  []dnssdcodec.Instance
	closed   bool
}

func (m *mockResponder) Announce(_ context.Context, ins dnssdcodec.Instance) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.announce = ins
	return nil
}

func (m *mockResponder) Update(_ context.Context, ins dnssdcodec.Instance) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updates = append(m.updates, ins)
	return nil
}

func (m *mockResponder) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockResponder) lastUpdate() (dnssdcodec.Instance, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.updates) == 0 {
		return dnssdcodec.Instance{}, false
	}
	return m.updates[len(m.updates)-1], true
}

// Compile-time interface check.
var _ dnssdsession.Responder = (*mockResponder)(nil)

func newTestNodeServer(t *testing.T) *IS04NodeServer {
	t.Helper()
	s, err := NewIS04NodeServer(nil, validBundle(), IS04NodeConfig{
		Bind:          ":0",
		AdvertiseHost: "node.local:18080",
		DiscoveryMode: "mdns",
		APIVer:        "v1.3",
	})
	if err != nil {
		t.Fatalf("NewIS04NodeServer: %v", err)
	}
	return s
}

func TestBuildNodeTXTLockedHasAllKeys(t *testing.T) {
	s := newTestNodeServer(t)
	got := s.buildNodeTXTLocked("v1.0,v1.1,v1.2,v1.3")

	want := map[string]string{
		dnssdcodec.TXTKeyAPIProto: "http",
		dnssdcodec.TXTKeyAPIVer:   "v1.0,v1.1,v1.2,v1.3",
		dnssdcodec.TXTKeyAPIAuth:  "false",
		dnssdcodec.TXTKeyPriority: "0",
		dnssdcodec.TXTKeyVerSlf:   "0",
		dnssdcodec.TXTKeyVerDvc:   "0",
		dnssdcodec.TXTKeyVerSrc:   "0",
		dnssdcodec.TXTKeyVerFlw:   "0",
		dnssdcodec.TXTKeyVerSnd:   "0",
		dnssdcodec.TXTKeyVerRcv:   "0",
	}
	if len(got) != len(want) {
		t.Fatalf("TXT key count = %d, want %d (got=%v)", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("TXT[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestBumpResourceVersionRepublishesTXT(t *testing.T) {
	s := newTestNodeServer(t)
	mr := &mockResponder{}
	// Stage the announce instance the way Serve() would, then attach
	// the mock responder so BumpResourceVersion sees a live target.
	s.mu.Lock()
	s.announceInstance = dnssdcodec.Instance{
		Name:    "test-node",
		Service: dnssdcodec.ServiceNode,
		Domain:  dnssdcodec.DefaultDomain,
		Host:    "test-node.local",
		Port:    18080,
		TXT:     s.buildNodeTXTLocked("v1.3"),
	}
	s.responder = mr
	s.announceCtx = context.Background()
	s.mu.Unlock()

	cases := []struct {
		t   is04.ResourceType
		key string
	}{
		{is04.ResourceNode, dnssdcodec.TXTKeyVerSlf},
		{is04.ResourceDevice, dnssdcodec.TXTKeyVerDvc},
		{is04.ResourceSource, dnssdcodec.TXTKeyVerSrc},
		{is04.ResourceFlow, dnssdcodec.TXTKeyVerFlw},
		{is04.ResourceSender, dnssdcodec.TXTKeyVerSnd},
		{is04.ResourceReceiver, dnssdcodec.TXTKeyVerRcv},
	}
	for i, c := range cases {
		s.BumpResourceVersion(c.t)
		ins, ok := mr.lastUpdate()
		if !ok {
			t.Fatalf("Bump #%d (%s): no Update call recorded", i, c.t)
		}
		if got := ins.TXT[c.key]; got != "1" {
			t.Errorf("after Bump(%s): TXT[%q] = %q, want %q", c.t, c.key, got, "1")
		}
		// Other resource counters stay at 0 in this iteration.
		for _, other := range cases {
			if other.t == c.t {
				continue
			}
			if got := ins.TXT[other.key]; got != "0" {
				t.Errorf("after Bump(%s): TXT[%q] = %q, want %q", c.t, other.key, got, "0")
			}
		}
		// Reset that counter so the next iteration starts clean.
		ctr, _ := s.counterForResource(c.t)
		ctr.Store(0)
		s.mu.Lock()
		s.announceInstance.TXT[c.key] = "0"
		s.mu.Unlock()
	}
}

func TestBumpResourceVersionWrapsAt256(t *testing.T) {
	s := newTestNodeServer(t)
	mr := &mockResponder{}
	s.mu.Lock()
	s.announceInstance = dnssdcodec.Instance{
		Name:    "test-node",
		Service: dnssdcodec.ServiceNode,
		Host:    "test-node.local",
		Port:    18080,
		TXT:     s.buildNodeTXTLocked("v1.3"),
	}
	s.responder = mr
	s.announceCtx = context.Background()
	s.mu.Unlock()

	// 256 bumps takes the counter from 0 → 256 → wraps to 0 (uint8 trunc).
	for i := 0; i < 256; i++ {
		s.BumpResourceVersion(is04.ResourceSender)
	}
	ins, ok := mr.lastUpdate()
	if !ok {
		t.Fatalf("no Update call recorded after 256 bumps")
	}
	got := ins.TXT[dnssdcodec.TXTKeyVerSnd]
	if got != "0" {
		t.Fatalf("after 256 bumps: TXT[ver_snd] = %q, want %q (wraparound)", got, "0")
	}

	// One more bump → 1.
	s.BumpResourceVersion(is04.ResourceSender)
	ins, _ = mr.lastUpdate()
	if ins.TXT[dnssdcodec.TXTKeyVerSnd] != "1" {
		t.Fatalf("after 257 bumps: TXT[ver_snd] = %q, want %q",
			ins.TXT[dnssdcodec.TXTKeyVerSnd], "1")
	}
}

func TestBumpResourceVersionNoResponderInRegisteredMode(t *testing.T) {
	s := newTestNodeServer(t)
	// Simulate registered mode: announceInstance staged (mDNS mode is
	// active) but responder is suspended per IS-04 §4.2.1.
	s.mu.Lock()
	s.announceInstance = dnssdcodec.Instance{
		Name:    "test-node",
		Service: dnssdcodec.ServiceNode,
		Host:    "test-node.local",
		Port:    18080,
		TXT:     s.buildNodeTXTLocked("v1.3"),
	}
	s.responder = nil
	s.announceCtx = context.Background()
	s.mu.Unlock()

	for i := 0; i < 5; i++ {
		s.BumpResourceVersion(is04.ResourceFlow)
	}

	// Counter should have advanced …
	if got := s.verFlow.Load(); got != 5 {
		t.Errorf("verFlow.Load() = %d, want 5", got)
	}
	// … and the staged TXT must reflect that, ready for a future
	// re-announce on lose-registration.
	s.mu.Lock()
	staged := s.announceInstance.TXT[dnssdcodec.TXTKeyVerFlw]
	s.mu.Unlock()
	if staged != strconv.Itoa(5) {
		t.Errorf("staged TXT[ver_flw] = %q, want %q (counter must persist while suspended)",
			staged, "5")
	}
}

func TestBumpResourceVersionStaticDiscoveryNoOp(t *testing.T) {
	s := newTestNodeServer(t)
	// Static-discovery mode never builds an announceInstance.
	s.mu.Lock()
	s.announceInstance = dnssdcodec.Instance{}
	s.responder = nil
	s.mu.Unlock()

	s.BumpResourceVersion(is04.ResourceSender)
	// Counter still advances (cheap, lock-free) so a later switch into
	// mDNS mode would see the right value, but no panic / no nil deref.
	if got := s.verSender.Load(); got != 1 {
		t.Errorf("verSender = %d, want 1", got)
	}
}
