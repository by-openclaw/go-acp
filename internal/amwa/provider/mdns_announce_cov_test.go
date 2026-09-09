package provider

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	dnssdcodec "dhs/internal/amwa/codec/dnssd"
	"dhs/internal/amwa/codec/is09"
)

// announcingNode builds a Node server whose announce fields are primed the
// way Serve primes them, so the mDNS lifecycle can be driven without
// binding a port.
func announcingNode(t *testing.T, mode string) *IS04NodeServer {
	t.Helper()
	s, err := NewIS04NodeServer(newLogTap().logger(), validBundle(), IS04NodeConfig{
		Bind:          "127.0.0.1:0",
		DiscoveryMode: mode,
		APIVer:        "v1.3",
		Priority:      100,
	})
	if err != nil {
		t.Fatal(err)
	}
	s.announceCtx = context.Background()
	s.announceInstance = dnssdcodec.Instance{
		Name: "dhs-node", Service: dnssdcodec.ServiceNode, Domain: "local",
		Host: "node.local", Port: 8080, TXT: map[string]string{},
	}
	return s
}

// IS-04 §4.2.1 makes the two discovery modes exclusive: a Node that has
// registered stops advertising _nmos-node._tcp, and re-announces when it
// loses the Registry. The responder is opened and closed with that
// transition, and a responder that cannot be opened is reported, not
// fatal.
func TestNodeMDNSAnnounceFollowsRegistration(t *testing.T) {
	r := &scriptedResponder{}
	useResponder(t, r)
	s := announcingNode(t, "mdns")

	s.mu.Lock()
	if err := s.startMDNSAnnounceLocked(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	// Starting twice keeps the one responder: the announce is a state,
	// not an event.
	if err := s.startMDNSAnnounceLocked(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	if ins := r.lastAnnounce(t); ins.Service != dnssdcodec.ServiceNode || ins.Host != "node.local" {
		t.Errorf("announced %+v, want the Node's own instance", ins)
	}
	if n, _, _ := r.counts(); n != 1 {
		t.Errorf("announced %d times, want once", n)
	}

	// Registered: the advertisement stops.
	s.onRegistrationStateChanged(true)
	if _, _, closed := r.counts(); closed != 1 {
		t.Errorf("responder closed %d times, want 1 on registration", closed)
	}
	s.onRegistrationStateChanged(true) // idempotent
	if _, _, closed := r.counts(); closed != 1 {
		t.Errorf("a second registration must not close again (%d)", closed)
	}

	// Registry lost: the advertisement comes back.
	s.onRegistrationStateChanged(false)
	if n, _, _ := r.counts(); n != 2 {
		t.Errorf("announced %d times, want a re-announce on losing the Registry", n)
	}

	// Stopping without a responder is a no-op.
	s.mu.Lock()
	s.stopMDNSAnnounceLocked()
	s.stopMDNSAnnounceLocked()
	s.mu.Unlock()
}

// In static discovery mode there is no responder to toggle, so a
// registration change touches nothing.
func TestNodeMDNSAnnounceIgnoredInStaticMode(t *testing.T) {
	r := &scriptedResponder{}
	useResponder(t, r)
	s := announcingNode(t, "static")
	s.onRegistrationStateChanged(false)
	s.onRegistrationStateChanged(true)
	if n, _, closed := r.counts(); n != 0 || closed != 0 {
		t.Errorf("static mode touched the responder: announced=%d closed=%d", n, closed)
	}
}

// A responder that cannot be opened, and one that refuses the announce,
// are both reported — and the refused one is closed rather than leaked.
func TestNodeMDNSAnnounceFailures(t *testing.T) {
	s := announcingNode(t, "mdns")
	useResponder(t, nil) // constructor fails
	s.mu.Lock()
	err := s.startMDNSAnnounceLocked()
	s.mu.Unlock()
	if err == nil || !strings.Contains(err.Error(), "open mDNS responder") {
		t.Errorf("responder that cannot open = %v", err)
	}

	refusing := &scriptedResponder{announceErr: errors.New("group busy")}
	useResponder(t, refusing)
	s2 := announcingNode(t, "mdns")
	s2.mu.Lock()
	err = s2.startMDNSAnnounceLocked()
	s2.mu.Unlock()
	if err == nil || !strings.Contains(err.Error(), "announce") {
		t.Errorf("refused announce = %v", err)
	}
	if _, _, closed := refusing.counts(); closed != 1 {
		t.Errorf("a refused announce must close the responder (%d)", closed)
	}

	// The warn path: a re-announce that fails on losing the Registry is
	// logged, not fatal.
	tap := newLogTap()
	s3 := announcingNode(t, "mdns")
	s3.logger = tap.logger()
	s3.onRegistrationStateChanged(false)
	if !tap.has("re-announce on lose-registration failed") {
		t.Errorf("a failed re-announce must be logged; saw %v", tap.snapshot())
	}
}

// The System API announces itself the same way, and reports a responder
// it cannot open; "static" mode skips the announce entirely.
func TestSystemServerAnnounceAndFailure(t *testing.T) {
	global := &is09.Global{
		ID: "6e0a6e0a-0000-4000-8000-00000000000b", Version: "1600000000:0",
		Label: "sys", Description: "system", Tags: map[string][]string{},
		IS04: is09.IS04Config{HeartbeatInterval: 5},
		PTP:  is09.PTPConfig{AnnounceReceiptTimeout: 3, DomainNumber: 127},
	}

	r := &scriptedResponder{}
	useResponder(t, r)
	srv, err := NewIS09Server(newLogTap().logger(), global, IS09Config{Bind: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	until := time.Now().Add(5 * time.Second)
	for {
		if n, _, _ := r.counts(); n > 0 || !time.Now().Before(until) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if ins := r.lastAnnounce(t); ins.Service != dnssdcodec.ServiceSystem ||
		ins.TXT[dnssdcodec.TXTKeyAPIVer] != is09.APIVersion {
		t.Errorf("announced %+v, want the System API instance", ins)
	}
	cancel()
	<-done
	if err := srv.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
	if _, _, closed := r.counts(); closed == 0 {
		t.Error("Stop must close the responder")
	}

	// Serving twice is refused.
	if err := srv.Serve(context.Background()); err == nil || !strings.Contains(err.Error(), "already serving") {
		t.Errorf("second Serve = %v, want 'already serving'", err)
	}

	// A responder that cannot be opened is reported.
	useResponder(t, nil)
	srv2, err := NewIS09Server(nil, global, IS09Config{Bind: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv2.Serve(context.Background()); err == nil || !strings.Contains(err.Error(), "open mDNS responder") {
		t.Errorf("System Serve with no responder = %v", err)
	}

	// One that refuses the announce is reported too.
	useResponder(t, &scriptedResponder{announceErr: errors.New("group busy")})
	srv3, err := NewIS09Server(nil, global, IS09Config{Bind: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv3.Serve(context.Background()); err == nil || !strings.Contains(err.Error(), "announce") {
		t.Errorf("System Serve with a refused announce = %v", err)
	}

	// MarshalGlobal renders what is served.
	raw, err := srv.MarshalGlobal()
	if err != nil || !strings.Contains(string(raw), `"label":"sys"`) {
		t.Errorf("MarshalGlobal = %s, %v", raw, err)
	}
}

// The advertised host and port come from --advertise-host when it names
// both, fall back to the bind port when it names only a host, and to the
// OS hostname (then "localhost") when the bind is unspecified.
func TestSplitHostPortPrecedence(t *testing.T) {
	useHostname(t, "node-host", nil)
	cases := []struct {
		name, advertise, bind, wantHost string
		wantPort                        int
	}{
		{"advertise names both", "adv.local:9000", ":8080", "adv.local", 9000},
		{"advertise names only a host", "adv.local", ":8080", "adv.local", 8080},
		{"bind names both", "", "10.0.0.1:8080", "10.0.0.1", 8080},
		{"unspecified bind takes the hostname", "", ":8080", "node-host", 8080},
		{"all-interfaces bind takes the hostname", "", "0.0.0.0:8080", "node-host", 8080},
		{"IPv6 all-interfaces bind takes the hostname", "", "::", "node-host", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host, port := splitHostPort(tc.advertise, tc.bind)
			if host != tc.wantHost || port != tc.wantPort {
				t.Errorf("splitHostPort(%q, %q) = %q, %d; want %q, %d",
					tc.advertise, tc.bind, host, port, tc.wantHost, tc.wantPort)
			}
		})
	}

	// With no hostname to be had, the advertised host is "localhost".
	useHostname(t, "", errors.New("no hostname"))
	if host, _ := splitHostPort("", ":8080"); host != "localhost" {
		t.Errorf("host without a hostname = %q, want localhost", host)
	}
}
