package dnssd

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"acp/internal/amwa/codec/dnssd"
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
