package acp2

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"dhs/internal/acp2/codec"
	"dhs/internal/metrics"
	"dhs/internal/plugin"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// Every connector is supposed to expose live frame and byte counters; this
// one exposed none, so `--metrics-addr` served a scrape with no acp2 series
// in it at all.
func TestPluginExposesMetrics(t *testing.T) {
	p := (&Factory{}).New(plugin.Deps{Logger: discard()}).(*Plugin)
	if p.Metrics() == nil {
		t.Fatal("Metrics must be non-nil — WithDefaults always fills it")
	}
}

// Counting is attributed by AN2 Type, the natural command axis for AN2 and
// the same one the acp2 provider registers, so consumer and provider series
// line up in one scrape.
func TestSetMetricsRegistersEveryAN2Type(t *testing.T) {
	met := metrics.NewConnector()
	s := NewSession(nil, discard())
	s.SetMetrics(met)

	names := met.Snapshot().CmdNames
	for _, tp := range []codec.AN2Type{
		codec.AN2TypeRequest, codec.AN2TypeReply, codec.AN2TypeEvent,
		codec.AN2TypeError, codec.AN2TypeData,
	} {
		if got := names[uint8(tp)]; got != tp.String() {
			t.Errorf("AN2 type %d registered as %q, want %q", tp, got, tp.String())
		}
	}
}

// A Session built directly by a test has no connector, and counting must
// stay a no-op rather than a nil dereference.
func TestSetMetricsIgnoresNil(t *testing.T) {
	s := NewSession(nil, discard())
	s.SetMetrics(nil)
	if s.met != nil {
		t.Error("a nil connector must not be stored")
	}
}

// The wire length of a frame is its 8-byte AN2 header plus payload, read off
// the decoded frame rather than by re-encoding it.
func TestAN2FrameLen(t *testing.T) {
	for _, tc := range []struct {
		payload int
		want    int
	}{{0, 8}, {1, 9}, {512, 520}} {
		f := &codec.AN2Frame{Payload: make([]byte, tc.payload)}
		if got := an2FrameLen(f); got != tc.want {
			t.Errorf("an2FrameLen(payload %d) = %d, want %d", tc.payload, got, tc.want)
		}
	}
}

// Both directions are counted on a real socket: what sendFrame writes, and
// what readLoop reads back.
func TestSessionCountsBothDirections(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	// The peer drains one frame, then answers once. A real socket rather
	// than net.Pipe: the pipe is unbuffered, so the peer's reply blocks
	// until readLoop is already reading, which the ordering here cannot
	// promise.
	served := make(chan struct{})
	go func() {
		defer close(served)
		conn, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		if _, rerr := codec.ReadAN2Frame(conn); rerr != nil {
			return
		}
		raw, eerr := codec.EncodeAN2Frame(&codec.AN2Frame{
			Proto: codec.AN2ProtoInternal, MTID: 1,
			Type: codec.AN2TypeReply, Payload: []byte{0x00, 0x01},
		})
		if eerr == nil {
			_, _ = conn.Write(raw)
		}
		<-time.After(200 * time.Millisecond)
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	met := metrics.NewConnector()
	s := NewSession(nil, discard())
	s.SetMetrics(met)
	s.conn = conn
	go s.readLoop(conn)

	if err := s.sendFrame(context.Background(), &codec.AN2Frame{
		Proto: codec.AN2ProtoInternal, Slot: 0, MTID: 1,
		Type: codec.AN2TypeRequest, Payload: []byte{0x00},
	}); err != nil {
		t.Fatalf("sendFrame: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && met.Snapshot().RxFrames == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	<-served

	snap := met.Snapshot()
	if snap.TxFrames == 0 || snap.TxBytes == 0 {
		t.Errorf("nothing counted on the write path: tx=%d/%d", snap.TxFrames, snap.TxBytes)
	}
	if snap.RxFrames == 0 || snap.RxBytes == 0 {
		t.Errorf("nothing counted on the read path: rx=%d/%d", snap.RxFrames, snap.RxBytes)
	}
}

// Most consumer verbs are one-shot, so the summary logged on Disconnect is
// where a session's counters become visible at all.
func TestDisconnectLogsTheSessionSummary(t *testing.T) {
	var buf bytes.Buffer
	p := (&Factory{}).New(plugin.Deps{
		Logger: slog.New(slog.NewTextHandler(&buf, nil)),
	}).(*Plugin)

	p.session = NewSession(nil, discard())
	if err := p.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	if got := buf.String(); !strings.Contains(got, "acp2 session metrics") ||
		!strings.Contains(got, "rx=") {
		t.Errorf("Disconnect logged no session summary: %s", got)
	}
}
