package provider

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	dnssdcodec "dhs/internal/amwa/codec/dnssd"
)

// scriptZone routes the unicast DNS-SD lookup at a scripted zone for the
// test's lifetime and reports how many times each service was asked.
func scriptZone(t *testing.T, answer func(service string) ([]dnssdcodec.Instance, error)) *int32Counter {
	t.Helper()
	c := &int32Counter{}
	prev := resolveUnicast
	resolveUnicast = func(_ context.Context, _, service, _ string, _ time.Duration) ([]dnssdcodec.Instance, error) {
		c.add(service)
		return answer(service)
	}
	t.Cleanup(func() { resolveUnicast = prev })
	return c
}

type int32Counter struct {
	mu sync.Mutex
	n  map[string]int
}

func (c *int32Counter) add(k string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n == nil {
		c.n = map[string]int{}
	}
	c.n[k]++
}

func (c *int32Counter) get(k string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n[k]
}

func regInstance(name, service, host string, port uint16, apiVer string, pri int) dnssdcodec.Instance {
	return dnssdcodec.Instance{
		Name: name, Service: service, Domain: "example.arpa",
		Host: host, Port: port,
		TXT: map[string]string{
			dnssdcodec.TXTKeyAPIProto: "http",
			dnssdcodec.TXTKeyAPIVer:   apiVer,
			dnssdcodec.TXTKeyPriority: strconv.Itoa(pri),
		},
	}
}

// The unicast watcher asks its zone for both the current and the legacy
// service name, keeps what it can use, and re-asks on its interval until
// Close stops it. A registry the Node cannot speak to is rejected, and a
// zone that cannot be reached is logged, not fatal.
func TestUnicastWatcherResolvesAndReResolves(t *testing.T) {
	prev := unicastReresolveInterval
	unicastReresolveInterval = 5 * time.Millisecond
	t.Cleanup(func() { unicastReresolveInterval = prev })

	tap := newLogTap()
	counts := scriptZone(t, func(service string) ([]dnssdcodec.Instance, error) {
		switch service {
		case dnssdcodec.ServiceRegister:
			return []dnssdcodec.Instance{
				regInstance("reg-a", service, "reg-a.example.arpa", 8235, "v1.3", 10),
				regInstance("reg-b", service, "reg-b.example.arpa", 8235, "v0.1", 1), // no mutual version
			}, nil
		default:
			return nil, errors.New("NXDOMAIN")
		}
	})

	w := NewUnicastRegistryWatcher(tap.logger(), "10.0.0.53", "example.arpa", "")
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	tap.wait(t, "registry discovered (unicast DNS-SD)")
	if !tap.has("unicast registry rejected") {
		t.Error("a registry advertising no mutual api_ver must be reported as rejected")
	}

	best, ok := w.Best()
	if !ok || best.FullName != "reg-a."+dnssdcodec.ServiceRegister+".example.arpa" {
		t.Fatalf("Best = %+v, %v; want the usable registry", best, ok)
	}
	if best.URL != "http://reg-a.example.arpa:8235" || best.APIVer != "v1.3" {
		t.Errorf("candidate = %+v", best)
	}

	// The zone is re-asked on the interval, for both service names.
	deadline := time.Now().Add(5 * time.Second)
	for counts.get(dnssdcodec.ServiceRegister) < 2 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if counts.get(dnssdcodec.ServiceRegister) < 2 {
		t.Errorf("zone asked %d times, want a re-resolve", counts.get(dnssdcodec.ServiceRegister))
	}
	if counts.get(dnssdcodec.ServiceRegisterLegacy) == 0 {
		t.Error("the pre-v1.2 legacy service name must be asked too")
	}
	if !tap.has("unicast DNS-SD resolve failed") {
		t.Error("a zone that answers NXDOMAIN must be logged, not fatal")
	}

	if err := w.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	stopped := counts.get(dnssdcodec.ServiceRegister)
	time.Sleep(30 * time.Millisecond)
	if counts.get(dnssdcodec.ServiceRegister) != stopped {
		t.Error("Close must stop the re-resolve loop")
	}
	if err := w.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// A disqualified registry is skipped until its penalty expires, and a
// re-resolve that still sees it clears the penalty — the zone is the
// authority on what exists.
func TestUnicastWatcherDisqualifyAndRecover(t *testing.T) {
	scriptZone(t, func(service string) ([]dnssdcodec.Instance, error) {
		if service != dnssdcodec.ServiceRegister {
			return nil, errors.New("no legacy records")
		}
		return []dnssdcodec.Instance{regInstance("reg", service, "reg.example.arpa", 8235, "v1.3", 1)}, nil
	})
	w := NewUnicastRegistryWatcher(nil, "10.0.0.53", "example.arpa", "v1.3")
	w.resolveOnce(context.Background())
	best, ok := w.Best()
	if !ok {
		t.Fatal("the resolved registry must be a candidate")
	}

	w.Disqualify(best.FullName)
	if _, ok := w.Best(); ok {
		t.Error("a disqualified registry must not be offered")
	}

	// Its penalty expires with time.
	w.mu.Lock()
	w.disqualified[best.FullName] = time.Now().Add(-time.Second)
	w.mu.Unlock()
	if _, ok := w.Best(); !ok {
		t.Error("an expired penalty must release the registry")
	}

	// And a re-resolve clears it outright.
	w.Disqualify(best.FullName)
	w.resolveOnce(context.Background())
	if _, ok := w.Best(); !ok {
		t.Error("a registry still in the zone must be released")
	}
}
