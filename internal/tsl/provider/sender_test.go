package tsl

import (
	"net"
	"testing"
	"time"

	"acp/internal/tsl/codec"
)

func TestUDPSender_V31SendReachesListener(t *testing.T) {
	// Start a plain UDP listener on an ephemeral port to stand in for
	// the MV we push tallies to.
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()
	dest := listener.LocalAddr().(*net.UDPAddr)

	sender := newUDPSender()
	if err := sender.bind("127.0.0.1:0"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	defer func() { _ = sender.close() }()
	if err := sender.addDest(dest.IP.String(), dest.Port); err != nil {
		t.Fatalf("addDest: %v", err)
	}

	frame := codec.V31Frame{Address: 12, Tally2: true, Brightness: codec.BrightnessHalf, Text: "ISO"}
	if err := sender.encodeAndSendV31(frame); err != nil {
		t.Fatalf("send: %v", err)
	}

	if err := listener.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	buf := make([]byte, 64)
	n, _, err := listener.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("listener read: %v", err)
	}
	if n != codec.V31FrameSize {
		t.Fatalf("got %d bytes, want %d", n, codec.V31FrameSize)
	}

	got, err := codec.DecodeV31(buf[:n])
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Address != 12 || !got.Tally2 || got.Brightness != codec.BrightnessHalf {
		t.Errorf("round-trip wrong: %+v", got)
	}
}

func TestUDPSender_NoDestIsError(t *testing.T) {
	s := newUDPSender()
	if err := s.bind("127.0.0.1:0"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	defer func() { _ = s.close() }()
	err := s.encodeAndSendV31(codec.V31Frame{Address: 1})
	if err == nil {
		t.Errorf("want error when no destinations configured")
	}
}
