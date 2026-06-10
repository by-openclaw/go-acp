package acp1

import (
	"context"
	"net"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
)

// freeUDPPort grabs an ephemeral UDP port and releases it so Discover can
// re-bind it (Discover sets SO_REUSEADDR, and on loopback the brief gap is
// race-free enough for a unit test).
func freeUDPPort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	p := c.LocalAddr().(*net.UDPAddr).Port
	_ = c.Close()
	return p
}

func TestDiscover_PassiveAndActive(t *testing.T) {
	port := freeUDPPort(t)

	// Sender: once the listener is up, push an announcement (MTID=0), a
	// frame-status reply that fills NumSlots, and a malformed datagram.
	go func() {
		time.Sleep(60 * time.Millisecond)
		conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = conn.Write(buildReply(t, 0, codec.MTypeAnnounce, 0, codec.GroupFrame, 0, []byte{3, 2, 2, 0}))
		_, _ = conn.Write(buildReply(t, 5, codec.MTypeReply, byte(codec.MethodGetValue), codec.GroupFrame, 0, []byte{3, 2, 2, 2}))
		_, _ = conn.Write([]byte{0x01}) // malformed → skipped
	}()

	res, err := Discover(context.Background(), DiscoverConfig{
		Port: port, Duration: 400 * time.Millisecond, Active: true,
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	var found *DiscoverResult
	for i := range res {
		if res[i].IP == "127.0.0.1" {
			found = &res[i]
		}
	}
	if found == nil {
		t.Fatalf("127.0.0.1 not discovered; got %+v", res)
	}
	if found.NumSlots != 3 {
		t.Errorf("NumSlots = %d, want 3", found.NumSlots)
	}
	if found.Source != "announcement" {
		t.Errorf("Source = %q, want announcement", found.Source)
	}
}

func TestDiscover_PassiveNoDevices(t *testing.T) {
	port := freeUDPPort(t)
	res, err := Discover(context.Background(), DiscoverConfig{
		Port: port, Duration: 100 * time.Millisecond, Active: false,
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("expected no devices, got %+v", res)
	}
}

func TestProbeActive(t *testing.T) {
	// Best-effort: on hosts with no broadcast route this returns an error,
	// on others it succeeds. Either way the send path is exercised.
	_ = probeActive(freeUDPPort(t))
}
