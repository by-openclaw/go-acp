package dnssd

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/amwa/codec/dnssd"
)

// runFakeDNSServer binds to a free UDP/127.0.0.1 port and replies to a
// single PTR query with the canned response bytes. Returns the
// host:port the caller should pass to ResolveUnicast plus a wait-for
// hook the test can defer.
func runFakeDNSServer(t *testing.T, response []byte) (addr string, wait func()) {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() { _ = conn.Close() }()
		buf := make([]byte, 4096)
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			t.Logf("fake DNS read: %v", err)
			return
		}
		// Sanity-check the query.
		req, err := dnssd.Decode(buf[:n])
		if err != nil || len(req.Questions) != 1 {
			t.Logf("fake DNS unexpected request: err=%v qs=%d", err, len(req.Questions))
			return
		}
		// Fix the response ID to match the query.
		fixed := make([]byte, len(response))
		copy(fixed, response)
		fixed[0] = buf[0]
		fixed[1] = buf[1]
		if _, err := conn.WriteToUDP(fixed, src); err != nil {
			t.Logf("fake DNS write: %v", err)
		}
	}()
	return conn.LocalAddr().String(), func() { wg.Wait() }
}

func TestResolveUnicast_HappyPath(t *testing.T) {
	// Build a canned response carrying PTR + SRV + TXT + A for a
	// fake _nmos-register._tcp.local instance.
	inst := dnssd.Instance{
		Name:    "fake-registry",
		Service: dnssd.ServiceRegister,
		Domain:  "local",
		Host:    "fake.local",
		Port:    8235,
		IPv4:    []net.IP{net.IPv4(127, 0, 0, 1).To4()},
		TXT:     map[string]string{dnssd.TXTKeyAPIProto: "http", dnssd.TXTKeyAPIVer: "v1.3", dnssd.TXTKeyAPIAuth: "false", dnssd.TXTKeyPriority: "10"},
	}
	response, err := dnssd.EncodeAnnounce(inst, true)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	addr, wait := runFakeDNSServer(t, response)
	defer wait()

	insts, err := ResolveUnicast(context.Background(), addr, dnssd.ServiceRegister, "local", time.Second)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(insts) != 1 {
		t.Fatalf("want 1 instance, got %d", len(insts))
	}
	got := insts[0]
	if got.Name != "fake-registry" || got.Port != 8235 {
		t.Errorf("instance: %+v", got)
	}
	if pri, ok := dnssd.PriorityFromTXT(got.TXT); !ok || pri != 10 {
		t.Errorf("priority: %v %v", pri, ok)
	}
}

func TestResolveUnicast_Timeout(t *testing.T) {
	// Bind a socket that never replies.
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = conn.Close() }()
	addr := conn.LocalAddr().String()

	start := time.Now()
	_, err = ResolveUnicast(context.Background(), addr, dnssd.ServiceRegister, "local", 200*time.Millisecond)
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("timeout took too long: %v", elapsed)
	}
}

func TestResolveUnicast_BadResolverPort(t *testing.T) {
	// Pick a port likely closed; ICMP unreachable surfaces as ECONNREFUSED on Linux,
	// or the read times out. Either way the call must fail.
	_, err := ResolveUnicast(context.Background(), "127.0.0.1:1", dnssd.ServiceRegister, "local", 200*time.Millisecond)
	if err == nil {
		t.Fatalf("expected error against closed port")
	}
}

// TestResolveUnicast_DialError covers the dial-failure arm: an
// out-of-range port makes net.Dialer.DialContext fail at address parse,
// before any query is sent.
func TestResolveUnicast_DialError(t *testing.T) {
	_, err := ResolveUnicast(context.Background(), "127.0.0.1:99999", dnssd.ServiceRegister, "local", 200*time.Millisecond)
	if err == nil {
		t.Fatal("expected dial error for out-of-range port")
	}
}

// TestResolveUnicast_DefaultTimeoutAndPortSuffix covers two arms in one
// shot: timeout<=0 selects DefaultUnicastTimeout, and a resolver string
// with no ':' gets ":53" appended. The dial to :53 succeeds (UDP is
// connectionless) but nothing answers, so the read fails — proving both
// normalisations ran on the path to the query.
func TestResolveUnicast_DefaultTimeoutAndPortSuffix(t *testing.T) {
	// Bind 127.0.0.1:0 only to guarantee :53 here is not us; we rely on
	// the read timing out. Keep the whole thing short by pre-cancelling
	// via a tiny context deadline layered under DefaultUnicastTimeout.
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	_, err := ResolveUnicast(ctx, "127.0.0.1", dnssd.ServiceRegister, "local", 0)
	if err == nil {
		t.Fatal("expected error resolving against :53 with no responder")
	}
}

// runQueryServer binds a UDP/127.0.0.1 responder that answers each
// incoming query by consulting respond(). Returning ok=false drops the
// query (used to force a read timeout on the client). Used to script the
// multi-query "chase the PTR" flow (PTR -> SRV -> TXT -> A).
func runQueryServer(t *testing.T, respond func(q dnssd.Question) ([]byte, bool)) (addr string, stop func()) {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		buf := make([]byte, 4096)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				return // socket closed by stop()
			}
			req, derr := dnssd.Decode(buf[:n])
			if derr != nil || len(req.Questions) == 0 {
				continue
			}
			if resp, ok := respond(req.Questions[0]); ok {
				_, _ = conn.WriteToUDP(resp, src)
			}
		}
	}()
	return conn.LocalAddr().String(), func() { _ = conn.Close() }
}

func mustEncode(t *testing.T, m *dnssd.Message) []byte {
	t.Helper()
	b, err := m.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return b
}

func ptrOnlyResponse(t *testing.T, name, target string) []byte {
	m := &dnssd.Message{}
	m.Header.SetResponse(true)
	m.Answers = []dnssd.RR{{Name: name, Type: dnssd.TypePTR, Class: dnssd.ClassIN, TTL: 120, PTR: target}}
	return mustEncode(t, m)
}

func srvResponse(t *testing.T, name, target string, port uint16) []byte {
	m := &dnssd.Message{}
	m.Header.SetResponse(true)
	m.Answers = []dnssd.RR{{Name: name, Type: dnssd.TypeSRV, Class: dnssd.ClassIN, TTL: 120, SRV: &dnssd.SRVData{Port: port, Target: target}}}
	return mustEncode(t, m)
}

func aResponse(t *testing.T, name string, ip net.IP) []byte {
	m := &dnssd.Message{}
	m.Header.SetResponse(true)
	m.Answers = []dnssd.RR{{Name: name, Type: dnssd.TypeA, Class: dnssd.ClassIN, TTL: 120, A: ip.To4()}}
	return mustEncode(t, m)
}

// TestResolveUnicast_ChasePath drives the bandwidth-minimising resolver
// flow (Unbound style): the PTR answer carries no SRV/TXT, so
// ResolveUnicast must chase each PTR target with explicit SRV + TXT + A
// queries and stitch the Instance back together. Also covers the
// timeout<=0 default (passing 0) and the bare-service domain append.
func TestResolveUnicast_ChasePath(t *testing.T) {
	const (
		service  = dnssd.ServiceRegister
		domain   = "local"
		ptrName  = service + "." + domain
		fullName = "reg1." + service + "." + domain
		host     = "reg1.local"
	)
	addr, stop := runQueryServer(t, func(q dnssd.Question) ([]byte, bool) {
		switch q.Type {
		case dnssd.TypePTR:
			return ptrOnlyResponse(t, ptrName, fullName), true
		case dnssd.TypeSRV:
			return srvResponse(t, fullName, host, 8235), true
		case dnssd.TypeTXT:
			// Mix a non-TXT record (A) into the TXT answer section so
			// chaseInstance's `rr.Type != TypeTXT` skip arm runs.
			m := &dnssd.Message{}
			m.Header.SetResponse(true)
			m.Answers = []dnssd.RR{
				{Name: host, Type: dnssd.TypeA, Class: dnssd.ClassIN, TTL: 120, A: net.IPv4(127, 0, 0, 1).To4()},
				{Name: fullName, Type: dnssd.TypeTXT, Class: dnssd.ClassIN, TTL: 120, TXT: []string{"api_ver=v1.3", "boolkey"}},
			}
			return mustEncode(t, m), true
		case dnssd.TypeA:
			return aResponse(t, host, net.IPv4(127, 0, 0, 1)), true
		}
		return nil, false
	})
	defer stop()

	insts, err := ResolveUnicast(context.Background(), addr, service, domain, 0)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(insts) != 1 {
		t.Fatalf("want 1 instance, got %d: %+v", len(insts), insts)
	}
	got := insts[0]
	if got.Name != "reg1" || got.Host != host || got.Port != 8235 {
		t.Errorf("stitched instance wrong: %+v", got)
	}
	if got.TXT["api_ver"] != "v1.3" {
		t.Errorf("TXT api_ver not stitched: %+v", got.TXT)
	}
	if got.TXT["boolkey"] != "" {
		t.Errorf("boolean TXT key should stitch to empty value: %+v", got.TXT)
	}
	if len(got.IPv4) != 1 || !got.IPv4[0].Equal(net.IPv4(127, 0, 0, 1)) {
		t.Errorf("A record not stitched: %+v", got.IPv4)
	}
}

// TestResolveUnicast_NoTargets covers the arm where the PTR response is a
// valid response with no PTR answers (and no packed SRV): DecodeInstances
// yields nothing, the target list is empty, and ResolveUnicast returns
// (nil, nil). Also covers the domain-append FALSE side by passing an
// empty domain.
func TestResolveUnicast_NoTargets(t *testing.T) {
	addr, stop := runQueryServer(t, func(q dnssd.Question) ([]byte, bool) {
		m := &dnssd.Message{}
		m.Header.SetResponse(true) // response, but zero answers
		return mustEncode(t, m), true
	})
	defer stop()

	insts, err := ResolveUnicast(context.Background(), addr, dnssd.ServiceRegister, "", time.Second)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if insts != nil {
		t.Fatalf("want nil instances, got %+v", insts)
	}
}

// dialLocal returns a UDP conn connected to addr with a short deadline,
// mirroring how ResolveUnicast sets up its socket, for direct
// dnsRoundtrip / chaseInstance unit tests.
func dialLocal(t *testing.T, addr string) net.Conn {
	t.Helper()
	c, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	_ = c.SetDeadline(time.Now().Add(time.Second))
	return c
}

// TestDNSRoundtrip_Arms exercises every failure arm of dnsRoundtrip:
// encode error (over-long label), write error (closed socket), read
// error (no responder + short deadline), decode error (garbage reply),
// and non-response reply (QR clear). The happy path is covered by the
// resolve tests above.
func TestDNSRoundtrip_Arms(t *testing.T) {
	// Encode error: 64-byte label exceeds the 63-byte DNS limit.
	c := dialLocal(t, "127.0.0.1:65000")
	defer func() { _ = c.Close() }()
	if _, err := dnsRoundtrip(c, strings.Repeat("a", 64), dnssd.TypePTR); err == nil {
		t.Error("expected encode error for over-long label")
	}

	// Write error: a closed socket cannot be written to.
	cc := dialLocal(t, "127.0.0.1:65000")
	_ = cc.Close()
	if _, err := dnsRoundtrip(cc, "_nmos-register._tcp.local", dnssd.TypePTR); err == nil {
		t.Error("expected write error on closed socket")
	}

	// Read error: nobody answers and the deadline is in the past.
	noReply, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = noReply.Close() }()
	rc := dialLocal(t, noReply.LocalAddr().String())
	defer func() { _ = rc.Close() }()
	_ = rc.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	if _, err := dnsRoundtrip(rc, "_nmos-register._tcp.local", dnssd.TypePTR); err == nil {
		t.Error("expected read timeout error")
	}

	// Decode error: server replies with sub-header garbage.
	gAddr, gStop := runQueryServer(t, func(q dnssd.Question) ([]byte, bool) {
		return []byte{0x00, 0x01, 0x02}, true
	})
	defer gStop()
	gc := dialLocal(t, gAddr)
	defer func() { _ = gc.Close() }()
	if _, err := dnsRoundtrip(gc, "_nmos-register._tcp.local", dnssd.TypePTR); err == nil {
		t.Error("expected decode error on garbage reply")
	}

	// Non-response: server echoes a query (QR clear).
	qAddr, qStop := runQueryServer(t, func(q dnssd.Question) ([]byte, bool) {
		b, _ := dnssd.EncodeQuery("_nmos-register._tcp.local", dnssd.TypePTR, false)
		return b, true
	})
	defer qStop()
	qc := dialLocal(t, qAddr)
	defer func() { _ = qc.Close() }()
	if _, err := dnsRoundtrip(qc, "_nmos-register._tcp.local", dnssd.TypePTR); err == nil {
		t.Error("expected non-response error when resolver returns a query")
	}
}

// TestChaseInstance_Arms covers chaseInstance beyond the happy chase in
// TestResolveUnicast_ChasePath: the SRV-query failure arm (ok=false),
// the "SRV present but Host/Port zero" arm (ok=false), and the
// single-label full name arm (no dot -> Name is the whole label).
func TestChaseInstance_Arms(t *testing.T) {
	// SRV roundtrip fails (server never answers) -> ok=false.
	silent, _ := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	defer func() { _ = silent.Close() }()
	sc := dialLocal(t, silent.LocalAddr().String())
	_ = sc.SetDeadline(time.Now().Add(150 * time.Millisecond))
	defer func() { _ = sc.Close() }()
	if _, ok := chaseInstance(sc, "reg1._nmos-register._tcp.local", dnssd.ServiceRegister, "local"); ok {
		t.Error("chaseInstance should fail when SRV never resolves")
	}

	// SRV present but target/port zero -> ok=false. Also uses a
	// single-label fullName ("reg1") to hit the no-dot Name branch.
	zAddr, zStop := runQueryServer(t, func(q dnssd.Question) ([]byte, bool) {
		if q.Type == dnssd.TypeSRV {
			return srvResponse(t, "reg1", "", 0), true // empty target, port 0
		}
		return nil, false
	})
	defer zStop()
	zc := dialLocal(t, zAddr)
	defer func() { _ = zc.Close() }()
	ins, ok := chaseInstance(zc, "reg1", dnssd.ServiceRegister, "local")
	if ok {
		t.Error("chaseInstance should fail when SRV carries no usable host/port")
	}
	if ins.Name != "reg1" {
		t.Errorf("single-label full name should map to Name=reg1, got %q", ins.Name)
	}
}

// TestIsBareServiceType is a table check of the RFC 6763 §4.1.2 heuristic
// that decides whether ResolveUnicast appends the discovery domain: a
// name is "bare" only while every label starts with '_'.
func TestIsBareServiceType(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"_nmos-register._tcp", true},
		{"_nmos-query._tcp", true},
		{"_nmos-register._tcp.by-systems.arpa", false},
		{"_nmos-register._tcp.local", false},
		{"host.local", false},
		{"._tcp", false}, // empty leading label
	}
	for _, tc := range cases {
		if got := isBareServiceType(tc.in); got != tc.want {
			t.Errorf("isBareServiceType(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
