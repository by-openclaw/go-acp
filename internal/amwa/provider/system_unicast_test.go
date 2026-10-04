package provider

// A Node in unicast discovery mode reads its IS-09 System API from the
// zone, not from a multicast link it was told does not exist. The zone
// is scripted here (discoverSystemUnicast is the seam); the System APIs
// are real HTTP servers.

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	dnssdcodec "dhs/internal/amwa/codec/dnssd"
	"dhs/internal/amwa/codec/is09"
)

// scriptedZone answers the `_nmos-system._tcp` question with whatever
// the test last published, and records what it was asked.
type scriptedZone struct {
	mu        sync.Mutex
	instances []dnssdcodec.Instance
	err       error
	asked     int
	resolver  string
	domain    string
}

func (z *scriptedZone) publish(ins ...dnssdcodec.Instance) {
	z.mu.Lock()
	z.instances, z.err = ins, nil
	z.mu.Unlock()
}

func (z *scriptedZone) fail(err error) {
	z.mu.Lock()
	z.instances, z.err = nil, err
	z.mu.Unlock()
}

func (z *scriptedZone) questions() int {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.asked
}

func useScriptedZone(t *testing.T) *scriptedZone {
	t.Helper()
	z := &scriptedZone{}
	prev := discoverSystemUnicast
	discoverSystemUnicast = func(_ context.Context, resolver, domain string, _ time.Duration) ([]dnssdcodec.Instance, error) {
		z.mu.Lock()
		defer z.mu.Unlock()
		z.asked++
		z.resolver, z.domain = resolver, domain
		return append([]dnssdcodec.Instance(nil), z.instances...), z.err
	}
	t.Cleanup(func() { discoverSystemUnicast = prev })
	prevInterval, prevMin, prevMax := unicastReresolveInterval, unicastBackoffMin, unicastBackoffMax
	unicastReresolveInterval, unicastBackoffMin, unicastBackoffMax = 5*time.Millisecond, 5*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() {
		unicastReresolveInterval, unicastBackoffMin, unicastBackoffMax = prevInterval, prevMin, prevMax
	})
	return z
}

func awaitLabel(t *testing.T, got <-chan string, want string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case label := <-got:
			if label == want {
				return
			}
		case <-deadline:
			t.Fatalf("no global labelled %q was read", want)
		}
	}
}

// The watcher asks the zone at once, reads the best System API it
// names, follows a better one when the zone changes, forgets one the
// zone no longer carries, and retries one that failed — on a DNS feed
// the record still being published is the instruction to try again.
func TestUnicastSystemWatcherFollowsTheZone(t *testing.T) {
	z := useScriptedZone(t)
	low := newSystemAPI(t, "low")
	high := newSystemAPI(t, "high")
	z.publish(low.instance(t, "low", 10, 120))

	got := make(chan string, 64)
	w := NewUnicastSystemWatcher(nil, "", "10.0.0.53:53", "plant.example", func(g any, _ string) {
		if global, ok := g.(*is09.Global); ok {
			got <- global.Label
		}
	})
	if w.apiVer != "v1.0" {
		t.Errorf("apiVer defaulted to %q, want v1.0", w.apiVer)
	}
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	awaitLabel(t, got, "low")
	z.mu.Lock()
	resolver, domain := z.resolver, z.domain
	z.mu.Unlock()
	if resolver != "10.0.0.53:53" || domain != "plant.example" {
		t.Errorf("the zone was asked at %q for %q", resolver, domain)
	}

	// A better-priority record appears: the Node moves to it.
	z.publish(low.instance(t, "low", 10, 120), high.instance(t, "high", 1, 120))
	awaitLabel(t, got, "high")

	// The unchanged zone is re-asked but the same instance is not
	// re-read on every interval.
	drain(high.hits)
	asked := z.questions()
	waitFor(t, func() bool { return z.questions() > asked+3 })
	if n := len(high.hits); n != 0 {
		t.Errorf("an unchanged zone re-read the System API %d times", n)
	}

	// The record leaves the zone: the next-best is read, and when it
	// comes back it is read again (there was no goodbye to wait for).
	z.publish(low.instance(t, "low", 10, 120))
	awaitLabel(t, got, "low")
	z.publish(low.instance(t, "low", 10, 120), high.instance(t, "high", 1, 120))
	awaitLabel(t, got, "high")

	// A resolver that stops answering empties what is known, so the
	// same record is read afresh when the zone answers again.
	z.fail(errors.New("resolver unreachable"))
	asked = z.questions()
	waitFor(t, func() bool { return z.questions() > asked+1 })
	z.publish(low.instance(t, "low", 10, 120))
	awaitLabel(t, got, "low")

	// A System API that fails is walked past to the one that answers,
	// and asked again on a later round: the zone still names it.
	broken := newSystemAPI(t, "broken")
	broken.fail = true
	z.publish(broken.instance(t, "broken", 0, 120), high.instance(t, "high", 1, 120))
	awaitLabel(t, got, "high")
	waitFor(t, func() bool { return len(broken.hits) >= 2 })

	if err := w.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	// Stopped means stopped: no question after Close returns.
	asked = z.questions()
	time.Sleep(30 * time.Millisecond)
	if z.questions() != asked {
		t.Error("the resolve loop outlived Close")
	}
	// And Close on a watcher that was never Run is safe.
	if err := NewUnicastSystemWatcher(slog.Default(), "v1.0", "r", "d", nil).Close(); err != nil {
		t.Errorf("Close before Run: %v", err)
	}
}

func drain(c chan struct{}) {
	for {
		select {
		case <-c:
		default:
			return
		}
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition never held")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// The Node side of it: in unicast mode fetchSystemGlobal never browses
// mDNS, the watcher it starts is the unicast one carrying the Node's
// resolver and domain, the global it reads is applied, and Stop joins
// the loop without deadlocking on the lock the loop applies under.
func TestUnicastNodeReadsItsSystemAPIFromTheZone(t *testing.T) {
	z := useScriptedZone(t)
	api := newSystemAPI(t, "from-the-zone")
	api.global.IS04.HeartbeatInterval = 3
	z.publish(api.instance(t, "sys", 0, 120))

	scriptSystemDiscovery(t, func() ([]dnssdcodec.Instance, error) {
		t.Error("a unicast Node browsed mDNS for its System API")
		return nil, nil
	})
	tap := newLogTap()
	s := systemNode(t, IS04NodeConfig{
		DiscoveryMode:   "unicast",
		UnicastResolver: "10.0.0.53",
		UnicastDomain:   "plant.example",
	}, tap)

	if got := s.fetchSystemGlobal(context.Background()); got != nil {
		t.Errorf("fetchSystemGlobal = %+v; in unicast mode the watcher delivers it", got)
	}
	s.mu.Lock()
	watcher := s.systemWatcher
	s.mu.Unlock()
	if watcher == nil || watcher.browser != nil || watcher.resolver != "10.0.0.53" || watcher.domain != "plant.example" {
		t.Fatalf("watcher = %+v, want the unicast one on the Node's resolver and domain", watcher)
	}
	waitFor(t, func() bool { return s.SystemGlobal() != nil })
	if g := s.SystemGlobal(); g.Label != "from-the-zone" {
		t.Errorf("SystemGlobal = %+v", g)
	}
	if hb := s.systemHeartbeatInterval(); hb != 3*time.Second {
		t.Errorf("heartbeat interval = %v, want the System API's 3s", hb)
	}
	if !tap.has("System API discovered (unicast DNS-SD)") {
		t.Error("the discovery must be logged")
	}

	// A second call starts no second watcher.
	s.watchForSystem(context.Background())
	s.mu.Lock()
	same := s.systemWatcher == watcher
	s.mu.Unlock()
	if !same {
		t.Error("the watch was started twice")
	}

	stopped := make(chan struct{})
	go func() { _ = s.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop deadlocked against the unicast resolve loop")
	}
	asked := z.questions()
	time.Sleep(30 * time.Millisecond)
	if z.questions() != asked {
		t.Error("the resolve loop outlived Stop")
	}
}

// The re-ask schedule both unicast watchers keep: a zone that names
// nothing is asked again after 1 s, then half as long again each time
// up to 30 s; once something is known the interval applies and the
// backoff starts over.
func TestUnicastReaskSchedule(t *testing.T) {
	backoff := unicastBackoffMin
	var waits []time.Duration
	for i := 0; i < 11; i++ {
		var delay time.Duration
		delay, backoff = nextUnicastDelay(false, backoff)
		waits = append(waits, delay)
	}
	if waits[0] != time.Second || waits[1] != 1500*time.Millisecond || waits[2] != 2250*time.Millisecond {
		t.Errorf("first waits = %v, want 1s, 1.5s, 2.25s", waits[:3])
	}
	if last := waits[len(waits)-1]; last != unicastBackoffMax {
		t.Errorf("the backoff settled at %v, want the %v ceiling", last, unicastBackoffMax)
	}
	for i := 1; i < len(waits); i++ {
		if waits[i] < waits[i-1] {
			t.Errorf("the backoff shrank: %v", waits)
		}
	}
	delay, next := nextUnicastDelay(true, backoff)
	if delay != unicastReresolveInterval || next != unicastBackoffMin {
		t.Errorf("with something known = %v then %v, want the interval and a fresh backoff", delay, next)
	}
}

// An empty zone is re-asked on the backoff, not the interval: with the
// interval left at a minute, a registry published after the Node
// started is still found.
func TestUnicastRegistryWatcherReasksAnEmptyZoneSoon(t *testing.T) {
	prevMin, prevMax := unicastBackoffMin, unicastBackoffMax
	unicastBackoffMin, unicastBackoffMax = 5*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { unicastBackoffMin, unicastBackoffMax = prevMin, prevMax })

	var mu sync.Mutex
	published := false
	scriptZone(t, func(service string) ([]dnssdcodec.Instance, error) {
		mu.Lock()
		defer mu.Unlock()
		if !published || service != dnssdcodec.ServiceRegister {
			return nil, nil
		}
		return []dnssdcodec.Instance{regInstance("late", service, "late.example.arpa", 8235, "v1.3", 0)}, nil
	})
	w := NewUnicastRegistryWatcher(nil, "10.0.0.53", "plant.example", "v1.3")
	w.Run(context.Background())
	t.Cleanup(func() { _ = w.Close() })

	if _, ok := w.Best(); ok {
		t.Fatal("a registry before one was published")
	}
	mu.Lock()
	published = true
	mu.Unlock()
	waitFor(t, func() bool { _, ok := w.Best(); return ok })
}
