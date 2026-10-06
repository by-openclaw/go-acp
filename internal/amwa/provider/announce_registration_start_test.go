package provider

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	dnssdcodec "dhs/internal/amwa/codec/dnssd"
)

// A registration tells its start before its first POST, and its
// failure. AMWA IS-04-01 test_12 browses for the Node's announce as soon
// as it has seen that first POST: whatever the Node does "in the
// presence of a Registration API" has to be done by then, not when the
// last receiver has been registered.
func TestRegistrationTellsItsStartBeforeTheFirstPOST(t *testing.T) {
	var mu sync.Mutex
	var order []string
	note := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, s)
	}
	var fail atomic.Bool
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method == stdhttp.MethodPost {
			note("POST")
		}
		if fail.Load() {
			w.WriteHeader(stdhttp.StatusInternalServerError)
			return
		}
		w.WriteHeader(stdhttp.StatusCreated)
	}))
	defer srv.Close()

	c := NewRegistrationClient(nil, srv.URL, "v1.2", validBundle())
	c.SetOnRegistering(func(v bool) {
		if v {
			note("starting")
		} else {
			note("failed")
		}
	})
	c.SetOnRegistered(func(v bool) {
		if v {
			note("registered")
		}
	})

	if err := c.registerAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(order) < 3 || order[0] != "starting" || order[1] != "POST" || order[len(order)-1] != "registered" {
		t.Errorf("a registration went %v, want starting, then its POSTs, then registered", order)
	}
	order = nil
	mu.Unlock()

	// One that fails says so, after having said it started.
	fail.Store(true)
	if err := c.registerAll(context.Background()); err == nil {
		t.Fatal("a Registry answering 500 registered the Node")
	}
	mu.Lock()
	if len(order) < 2 || order[0] != "starting" || order[len(order)-1] != "failed" {
		t.Errorf("a failed registration went %v, want starting … failed", order)
	}
	order = nil
	mu.Unlock()

	// Cleared, it stays silent.
	c.SetOnRegistering(nil)
	_ = c.registerAll(context.Background())
	mu.Lock()
	defer mu.Unlock()
	for _, step := range order {
		if step != "POST" {
			t.Errorf("a cleared callback fired: %v", order)
		}
	}
}

// Before v1.3 the ver_* records leave the announce when the registration
// starts, and come back if it fails. v1.3 and static discovery have
// nothing to change at that moment.
func TestNodeBeforeV13DropsVerRecordsWhenRegistrationStarts(t *testing.T) {
	for _, minor := range []string{"v1.0", "v1.1", "v1.2"} {
		r := &scriptedResponder{}
		useResponder(t, r)
		s := announcingNode(t, "mdns")
		s.cfg.APIVer = minor
		s.mu.Lock()
		s.announceInstance.TXT = s.buildNodeTXTLocked(minor)
		if err := s.startMDNSAnnounceLocked(); err != nil {
			s.mu.Unlock()
			t.Fatal(err)
		}
		s.mu.Unlock()
		last := func() dnssdcodec.Instance {
			r.mu.Lock()
			defer r.mu.Unlock()
			if len(r.updated) == 0 {
				t.Fatalf("%s: the announce was not republished", minor)
			}
			return r.updated[len(r.updated)-1]
		}

		s.onRegistrationStarting(true)
		for _, key := range verTXTKeys {
			if _, there := last().TXT[key]; there {
				t.Errorf("%s: %s is still announced once the registration has started", minor, key)
			}
		}
		s.onRegistrationStarting(false)
		if _, there := last().TXT[dnssdcodec.TXTKeyVerSlf]; !there {
			t.Errorf("%s: a failed registration left the announce without its ver_* records", minor)
		}
	}

	for _, tc := range []struct{ mode, minor string }{{"mdns", "v1.3"}, {"static", "v1.2"}} {
		r := &scriptedResponder{}
		useResponder(t, r)
		s := announcingNode(t, tc.mode)
		s.cfg.APIVer = tc.minor
		s.onRegistrationStarting(true)
		r.mu.Lock()
		if len(r.updated) != 0 {
			t.Errorf("%s %s: the start of a registration republished the announce", tc.mode, tc.minor)
		}
		r.mu.Unlock()
	}
}
