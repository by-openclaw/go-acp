package tsl

import (
	"context"
	"net"
	"testing"
	"time"

	"acp/internal/tsl/codec"
)

func TestUDPSession_V31ReceiveAndDispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sess := newUDPSession()
	if err := sess.listen(ctx, "127.0.0.1:0", decodeV31Payload); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = sess.close() }()

	received := make(chan FrameV31Event, 1)
	sess.subscribeV31(func(ev FrameV31Event) { received <- ev })

	// Send a v3.1 frame to the session's port.
	sent := codec.V31Frame{Address: 42, Tally1: true, Tally3: true, Brightness: codec.BrightnessFull, Text: "PGM"}
	wire, err := sent.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	sock, err := net.DialUDP("udp", nil, sess.boundAddr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = sock.Close() }()
	if _, err := sock.Write(wire); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case ev := <-received:
		if ev.Frame.Address != 42 || !ev.Frame.Tally1 || ev.Frame.Tally2 || !ev.Frame.Tally3 {
			t.Errorf("unexpected frame: %+v", ev.Frame)
		}
		if ev.Frame.Brightness != codec.BrightnessFull {
			t.Errorf("brightness: %v", ev.Frame.Brightness)
		}
		if len(ev.Raw) != codec.V31FrameSize {
			t.Errorf("raw len %d", len(ev.Raw))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no frame received")
	}
}

func TestUDPSession_WrongSize_SurfacesNote(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sess := newUDPSession()
	if err := sess.listen(ctx, "127.0.0.1:0", decodeV31Payload); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = sess.close() }()

	got := make(chan FrameV31Event, 1)
	sess.subscribeV31(func(ev FrameV31Event) { got <- ev })

	// Send a 10-byte packet (not 18) — size mismatch.
	sock, _ := net.DialUDP("udp", nil, sess.boundAddr())
	defer func() { _ = sock.Close() }()
	if _, err := sock.Write(make([]byte, 10)); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case ev := <-got:
		if len(ev.Frame.Notes) == 0 {
			t.Fatalf("expected compliance note on size mismatch")
		}
		if ev.Frame.Notes[0].Kind != "tsl_label_length_mismatch" {
			t.Errorf("first note kind = %q, want tsl_label_length_mismatch", ev.Frame.Notes[0].Kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event")
	}
}
