package provider

// The node never hands out its live TXT map (#1319). Each changed
// resource bumps a ver_* counter from its own goroutine; a responder
// ranges over the TXT it is given, and may keep it. Sharing the map
// across that boundary killed a peer-to-peer node with "fatal error:
// concurrent map iteration and map write" when two resources changed
// at once.

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"testing"

	dnssdcodec "dhs/internal/amwa/codec/dnssd"
	"dhs/internal/amwa/codec/is04"
	dnssdsession "dhs/internal/amwa/session/dnssd"
)

// rangingResponder does what the Avahi backend does with an Update: it
// ranges over the TXT it was handed, outside any lock of the node's.
type rangingResponder struct {
	mu       sync.Mutex
	announce dnssdcodec.Instance
	last     map[string]string
	updates  int
}

func (r *rangingResponder) Announce(_ context.Context, ins dnssdcodec.Instance) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.announce = ins // kept as given, the way a careless responder would
	return nil
}

func (r *rangingResponder) Update(_ context.Context, ins dnssdcodec.Instance) error {
	seen := make(map[string]string, len(ins.TXT))
	for pass := 0; pass < 8; pass++ { // a wide window for a writer to land in
		for k, v := range ins.TXT {
			seen[k] = v
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.last = seen
	r.updates++
	return nil
}

func (r *rangingResponder) Close() error { return nil }

// announcing stages the node the way Serve does in peer-to-peer mode.
func announcing(t *testing.T, resp *rangingResponder) *IS04NodeServer {
	t.Helper()
	s := newTestNodeServer(t)
	s.mu.Lock()
	s.announceInstance = dnssdcodec.Instance{
		Name:    "test-node",
		Service: dnssdcodec.ServiceNode,
		Domain:  dnssdcodec.DefaultDomain,
		Host:    "test-node.local",
		Port:    18080,
		TXT:     s.buildNodeTXTLocked("v1.3"),
	}
	s.responder = resp
	s.announceCtx = context.Background()
	s.mu.Unlock()
	return s
}

// Every kind of resource changes at once, many times over. The process
// survives it, and what is left on the wire is the node's own counters:
// publications do not overtake each other.
func TestConcurrentResourceChangesPublishTheirOwnTXT(t *testing.T) {
	resp := &rangingResponder{}
	s := announcing(t, resp)

	kinds := []is04.ResourceType{
		is04.ResourceNode, is04.ResourceDevice, is04.ResourceSource,
		is04.ResourceFlow, is04.ResourceSender, is04.ResourceReceiver,
	}
	const perKind, bumps = 4, 300
	var wg sync.WaitGroup
	for _, kind := range kinds {
		for g := 0; g < perKind; g++ {
			wg.Add(1)
			go func(kind is04.ResourceType) {
				defer wg.Done()
				for i := 0; i < bumps; i++ {
					s.BumpResourceVersion(kind)
				}
			}(kind)
		}
	}
	wg.Wait()

	resp.mu.Lock()
	last, updates := resp.last, resp.updates
	resp.mu.Unlock()
	if want := len(kinds) * perKind * bumps; updates != want {
		t.Errorf("%d publications, want one per change (%d)", updates, want)
	}
	for _, kind := range kinds {
		counter, key := s.counterForResource(kind)
		if want := strconv.Itoa(int(uint8(counter.Load()))); last[key] != want {
			t.Errorf("the last TXT on the wire has %s=%s, the node counts %s", key, last[key], want)
		}
	}
}

// What the responder was announced with is a copy: a later change does
// not write into it.
func TestAnnounceHandsTheResponderItsOwnTXT(t *testing.T) {
	resp := &rangingResponder{}
	s := announcing(t, resp)
	s.mu.Lock()
	s.responder = nil // not announced yet
	s.mu.Unlock()
	prev := setDNSSDResponder(func(*slog.Logger) (dnssdsession.Responder, error) { return resp, nil })
	t.Cleanup(func() { setDNSSDResponder(prev) })

	s.mu.Lock()
	err := s.startMDNSAnnounceLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	s.BumpResourceVersion(is04.ResourceSender)

	resp.mu.Lock()
	announced := resp.announce.TXT[dnssdcodec.TXTKeyVerSnd]
	resp.mu.Unlock()
	if announced != "0" {
		t.Errorf("the announced TXT now reads ver_snd=%s — the responder was handed the node's own map", announced)
	}
	s.mu.Lock()
	s.stopMDNSAnnounceLocked()
	s.mu.Unlock()
}
