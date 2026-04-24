//go:build integration

package tsl_integration

import (
	"context"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"acp/internal/tsl/codec"
	consumer "acp/internal/tsl/consumer"
	provider "acp/internal/tsl/provider"
)

// TestV40_Loopback drives a full v4.0 round-trip (v3.1 block + CHKSUM +
// VBC + 2-byte XDATA) via localhost UDP and verifies the consumer
// receives the frame with exact tally-colour parity on both displays.
func TestV40_Loopback(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cp := consumer.NewPluginV40(logger)
	defer func() { _ = cp.Disconnect() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := cp.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("consumer Connect: %v", err)
	}

	got := make(chan codec.V40Frame, 4)
	if err := cp.SubscribeV40(func(ev consumer.FrameV40Event) { got <- ev.Frame }); err != nil {
		t.Fatalf("SubscribeV40: %v", err)
	}

	sp := provider.NewServerV40(logger)
	defer func() { _ = sp.Stop() }()
	if err := sp.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("provider Bind: %v", err)
	}
	dest := cp.BoundAddr()
	if err := sp.AddDestination(dest.IP.String(), dest.Port); err != nil {
		t.Fatalf("AddDestination: %v", err)
	}

	// Drive every tally colour on each position + non-zero v3.1 tallies.
	sent := codec.V40Frame{
		V31: codec.V31Frame{
			Address:    11,
			Tally1:     true,
			Tally3:     true,
			Brightness: codec.BrightnessFull,
			Text:       "CAM 1 ISO",
		},
		DisplayLeft:  codec.XByte{LH: codec.TallyRed, Text: codec.TallyGreen, RH: codec.TallyAmber},
		DisplayRight: codec.XByte{LH: codec.TallyGreen, Text: codec.TallyRed, RH: codec.TallyOff},
	}
	if err := sp.SendV40(sent); err != nil {
		t.Fatalf("SendV40: %v", err)
	}

	select {
	case f := <-got:
		if f.V31.Address != 11 || !f.V31.Tally1 || f.V31.Tally2 || !f.V31.Tally3 {
			t.Errorf("v3.1 part mismatch: %+v", f.V31)
		}
		if f.V31.Brightness != codec.BrightnessFull {
			t.Errorf("brightness: %v", f.V31.Brightness)
		}
		if f.DisplayLeft.LH != codec.TallyRed || f.DisplayLeft.Text != codec.TallyGreen || f.DisplayLeft.RH != codec.TallyAmber {
			t.Errorf("DisplayLeft: %+v", f.DisplayLeft)
		}
		if f.DisplayRight.LH != codec.TallyGreen || f.DisplayRight.Text != codec.TallyRed || f.DisplayRight.RH != codec.TallyOff {
			t.Errorf("DisplayRight: %+v", f.DisplayRight)
		}
		if f.MinorVersion != 0 {
			t.Errorf("MinorVersion=%d, want 0", f.MinorVersion)
		}
		if f.XDataCount != 2 {
			t.Errorf("XDataCount=%d, want 2", f.XDataCount)
		}
		if len(f.Notes) != 0 || len(f.V31.Notes) != 0 {
			t.Errorf("clean frame should have no notes, got v31=%+v v40=%+v", f.V31.Notes, f.Notes)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for v4.0 frame")
	}
}

// TestV40_CorruptedChecksum_SurfacesNote verifies on-wire corruption is
// caught via compliance note rather than silently absorbed.
func TestV40_CorruptedChecksum_SurfacesNote(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cp := consumer.NewPluginV40(logger)
	defer func() { _ = cp.Disconnect() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = cp.Connect(ctx, "127.0.0.1", 0)

	got := make(chan codec.V40Frame, 1)
	_ = cp.SubscribeV40(func(ev consumer.FrameV40Event) { got <- ev.Frame })

	sp := provider.NewServerV40(logger)
	defer func() { _ = sp.Stop() }()
	_ = sp.Bind("127.0.0.1:0")
	dest := cp.BoundAddr()
	_ = sp.AddDestination(dest.IP.String(), dest.Port)

	// Encode a v4.0 frame, corrupt its checksum, send raw via the
	// sender's UDP socket.
	frame := codec.V40Frame{V31: codec.V31Frame{Address: 5, Text: "X"}}
	wire, err := frame.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	wire[codec.V40ChksumIdx] ^= 0xFF
	// Use a separate socket to send the corrupted bytes; the sender
	// struct is internal. The simplest path: craft a net.Dial and Write.
	conn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: dest.IP, Port: dest.Port})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write(wire); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case f := <-got:
		found := false
		for _, n := range f.Notes {
			if n.Kind == "tsl_checksum_fail" {
				found = true
			}
		}
		if !found {
			t.Errorf("want tsl_checksum_fail note, got %+v", f.Notes)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}
