package osc

import (
	"net"
	"testing"
	"time"

	"acp/internal/osc/codec"
)

func TestUDPSender_MessageReachesListener(t *testing.T) {
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()
	dest := listener.LocalAddr().(*net.UDPAddr)

	s := newUDPSender()
	defer func() { _ = s.close() }()
	if err := s.bind("127.0.0.1:0"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if err := s.addDest(dest.IP.String(), dest.Port); err != nil {
		t.Fatalf("addDest: %v", err)
	}

	m := codec.Message{
		Address: "/mixer/ch/1/fader",
		Args:    []codec.Arg{codec.Float32(0.75)},
	}
	if err := s.sendMessage(m); err != nil {
		t.Fatalf("send: %v", err)
	}

	_ = listener.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 2048)
	n, _, err := listener.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("listener read: %v", err)
	}
	got, err := codec.DecodeMessage(buf[:n])
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Address != "/mixer/ch/1/fader" || got.Args[0].Float32 != 0.75 {
		t.Errorf("round-trip: %+v", got)
	}
}

func TestUDPSender_BundleReachesListener(t *testing.T) {
	listener, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	defer func() { _ = listener.Close() }()
	dest := listener.LocalAddr().(*net.UDPAddr)

	s := newUDPSender()
	defer func() { _ = s.close() }()
	_ = s.bind("127.0.0.1:0")
	_ = s.addDest(dest.IP.String(), dest.Port)

	b := codec.Bundle{
		Timetag: 1,
		Elements: []codec.Packet{
			codec.Message{Address: "/a", Args: []codec.Arg{codec.Int32(1)}},
			codec.Message{Address: "/b", Args: []codec.Arg{codec.Int32(2)}},
		},
	}
	if err := s.sendBundle(b); err != nil {
		t.Fatalf("sendBundle: %v", err)
	}
	_ = listener.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 2048)
	n, _, _ := listener.ReadFromUDP(buf)
	got, err := codec.DecodeBundle(buf[:n])
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Timetag != 1 || len(got.Elements) != 2 {
		t.Errorf("bundle round-trip: %+v", got)
	}
}

func TestUDPSender_NoDest(t *testing.T) {
	s := newUDPSender()
	_ = s.bind("127.0.0.1:0")
	defer func() { _ = s.close() }()
	if err := s.sendMessage(codec.Message{Address: "/x"}); err == nil {
		t.Errorf("want error when no destinations configured")
	}
}
