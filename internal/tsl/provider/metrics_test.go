package tsl

import (
	"net"
	"strconv"
	"testing"

	"dhs/internal/metrics"
	"dhs/internal/plugin"
	"dhs/internal/tsl/codec"
)

// The provider exposed no metrics at all, so `producer tsl-v50 serve
// --metrics-addr` mounted an endpoint with no tsl series in it — the CLI
// warns "provider does not expose Metrics() — skipping".
func TestServerExposesMetrics(t *testing.T) {
	srv := (&Factory{version: V50}).New(plugin.Deps{}, nil).(*Server)
	if srv.Metrics() == nil {
		t.Fatal("Metrics must be non-nil — WithDefaults always fills it")
	}
}

// Fanning one UMD packet to N receivers really is N datagrams on the wire,
// so it counts N times. A per-packet count would understate the load by the
// fan-out factor, which for tally is often the whole point.
func TestUDPSenderCountsPerDestination(t *testing.T) {
	met := metrics.NewConnector()
	s := newUDPSender(met)
	if err := s.bind("127.0.0.1:0"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	t.Cleanup(func() { _ = s.close() })

	for i := 0; i < 2; i++ {
		rc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		t.Cleanup(func() { _ = rc.Close() })
		if err := s.addDest("127.0.0.1", rc.LocalAddr().(*net.UDPAddr).Port); err != nil {
			t.Fatalf("addDest: %v", err)
		}
	}

	payload := []byte{0x80, 0x00, 0x00}
	if err := s.sendBytes(payload); err != nil {
		t.Fatalf("sendBytes: %v", err)
	}

	snap := met.Snapshot()
	if snap.TxFrames != 2 {
		t.Errorf("tx frames = %d, want 2 — one per destination", snap.TxFrames)
	}
	if snap.TxBytes != uint64(2*len(payload)) {
		t.Errorf("tx bytes = %d, want %d", snap.TxBytes, 2*len(payload))
	}
}

// A sender with no connector must still send.
func TestUDPSenderWithoutAConnector(t *testing.T) {
	s := newUDPSender(nil)
	if err := s.bind("127.0.0.1:0"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	t.Cleanup(func() { _ = s.close() })

	rc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = rc.Close() })
	if err := s.addDest("127.0.0.1", rc.LocalAddr().(*net.UDPAddr).Port); err != nil {
		t.Fatalf("addDest: %v", err)
	}
	if err := s.sendBytes([]byte{0x80}); err != nil {
		t.Fatalf("sendBytes: %v", err)
	}
}

// The v5.0 TCP path counts the DLE-wrapped bytes actually written — the
// wrapper is on the wire, so it is part of what went out.
func TestTCPDialerCountsWrappedBytes(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			go func() {
				buf := make([]byte, 512)
				for {
					if _, rerr := c.Read(buf); rerr != nil {
						_ = c.Close()
						return
					}
				}
			}()
		}
	}()

	met := metrics.NewConnector()
	d := newTCPDialer(met)
	t.Cleanup(func() { _ = d.close() })

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("port: %v", err)
	}

	if err := d.sendV50TCP(host, port, codec.V50Packet{}); err != nil {
		t.Fatalf("sendV50TCP: %v", err)
	}

	snap := met.Snapshot()
	if snap.TxFrames != 1 || snap.TxBytes == 0 {
		t.Errorf("tx = %d frames / %d bytes, want 1 and non-zero", snap.TxFrames, snap.TxBytes)
	}
}
