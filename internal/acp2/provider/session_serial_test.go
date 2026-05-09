package acp2

import (
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"dhs/internal/acp2/codec"
)

// TestSession_SerializesRequestsPerConnection pins the spec invariant
// from acp2_protocol.docx line 313 — "Should handle single request at
// a time" — for one TCP connection.
//
// We pipeline 64 ACP2 GetVersion requests on a real loopback TCP
// session without waiting for replies between sends. The session
// reader is a single goroutine that calls handleFrame inline before
// returning to ReadAN2Frame; handlers are synchronous. Replies must
// therefore arrive in send order.
//
// A real TCP socket (not net.Pipe) is required because net.Pipe is
// synchronous — every write blocks until the peer reads. Pipelining
// is the property under test.
func TestSession_SerializesRequestsPerConnection(t *testing.T) {
	srv := &server{
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		tree:     emptyTree(),
		sessions: map[*session]struct{}{},
	}
	srv.tree.slotN = 1
	srv.tree.perSlot[0] = map[uint32]*entry{}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()

	addr := listener.Addr().String()

	// Accept one connection and run the session.
	accepted := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		sess := newSession(srv, conn)
		close(accepted)
		sess.run()
	}()

	client, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()
	<-accepted

	const N = 64

	// Pipeline N GetVersion requests with mtid 1..N — write all
	// before reading any.
	go func() {
		for i := 1; i <= N; i++ {
			req := &codec.ACP2Message{
				Type: codec.ACP2TypeRequest,
				MTID: uint8(i),
				Func: codec.ACP2FuncGetVersion,
			}
			payload, _ := codec.EncodeACP2Message(req)
			frame := &codec.AN2Frame{
				Proto:   codec.AN2ProtoACP2,
				Slot:    0,
				Type:    codec.AN2TypeData,
				Payload: payload,
			}
			raw, _ := codec.EncodeAN2Frame(frame)
			if _, err := client.Write(raw); err != nil {
				t.Errorf("write mtid=%d: %v", i, err)
				return
			}
		}
	}()

	// Read N replies and verify mtids are 1..N in order.
	gotMTIDs := make([]uint8, 0, N)
	for len(gotMTIDs) < N {
		_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
		rep, err := codec.ReadAN2Frame(client)
		if err != nil {
			t.Fatalf("read reply #%d: %v (got %d/%d, sequence so far: %v)",
				len(gotMTIDs)+1, err, len(gotMTIDs), N, gotMTIDs)
		}
		if rep.Type != codec.AN2TypeData {
			t.Fatalf("reply #%d: AN2 type=%v want data", len(gotMTIDs)+1, rep.Type)
		}
		if len(rep.Payload) < 4 {
			t.Fatalf("reply #%d: payload too short (%d bytes)", len(gotMTIDs)+1, len(rep.Payload))
		}
		gotMTIDs = append(gotMTIDs, rep.Payload[1])
	}

	for i, got := range gotMTIDs {
		want := uint8(i + 1)
		if got != want {
			t.Fatalf("reply #%d: mtid=%d want %d (replies arrived out of order — concurrent dispatch?). full sequence: %v",
				i+1, got, want, gotMTIDs)
		}
	}
}
