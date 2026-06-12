package acp2

import (
	"context"
	"net"
	"testing"
	"time"

	"dhs/internal/consumer"

	"dhs/internal/acp2/codec"
)

// session_io_test.go exercises the session-layer routing branches that
// the higher-level method tests don't reach directly: AN2 slot events
// and AN2 errors on the read loop, orphan-reply handling, the
// SessionHealth snapshot, and the raw-body fallbacks in GetValue.

// TestSession_AN2SlotEvent pushes an unsolicited AN2 slot event
// (proto=0, type=event) and asserts the session updates the cached slot
// status (handleAN2Internal AN2TypeEvent branch).
func TestSession_AN2SlotEvent(t *testing.T) {
	srv, host, port := newFakeServer(t)
	// After the handshake, push an AN2 slot event for slot 1 → status 0
	// (card removed). The harness reuses announceFn as a post-handshake
	// hook; we send a raw AN2 event frame instead of an ACP2 announce.
	srv.announceFn = func(send func(slot uint8, msg *codec.ACP2Message)) {
		// send is ACP2-only; reach the connection directly for the AN2
		// event frame via the server's stored conn.
		srv.mu.Lock()
		conns := append([]net.Conn(nil), srv.conns...)
		srv.mu.Unlock()
		for _, c := range conns {
			srv.sendFrame(c, &codec.AN2Frame{
				Proto:   codec.AN2ProtoInternal,
				Slot:    1,
				MTID:    0,
				Type:    codec.AN2TypeEvent,
				Payload: []byte{0}, // status 0 = no card
			})
		}
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	p.mu.Lock()
	s := p.session
	p.mu.Unlock()

	ok := waitFor(t, 2*time.Second, func() bool {
		return s.SlotStatus(1) == consumer.SlotNoCard
	})
	if !ok {
		t.Errorf("slot 1 status = %v, want SlotNoCard after AN2 event", s.SlotStatus(1))
	}
}

// TestSession_OrphanReply pushes an ACP2 reply for an mtid with no
// registered waiter — the session drops it and fires the orphan
// compliance event (routeReply no-waiter branch).
func TestSession_OrphanReply(t *testing.T) {
	srv, host, port := newFakeServer(t)
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	prof := p.ComplianceProfile()
	if prof == nil {
		t.Fatal("nil compliance profile")
	}

	// Push an ACP2 reply with mtid=200 that no DoACP2 is waiting on,
	// AFTER Connect so the session's compliance profile is attached.
	srv.mu.Lock()
	conns := append([]net.Conn(nil), srv.conns...)
	srv.mu.Unlock()
	for _, c := range conns {
		srv.sendACP2(c, 0, &codec.ACP2Message{
			Type: codec.ACP2TypeReply,
			MTID: 200,
			Func: codec.ACP2FuncGetObject,
			Body: objectBody(1, nil),
		})
	}
	// The orphan reply increments OrphanReplyMtid.
	waitFor(t, 2*time.Second, func() bool {
		return prof.Snapshot()[OrphanReplyMtid] > 0
	})
	if prof.Snapshot()[OrphanReplyMtid] == 0 {
		t.Error("orphan reply did not fire OrphanReplyMtid event")
	}
}

// TestSessionHealth covers the SessionHealth snapshot on a live session.
func TestSessionHealth(t *testing.T) {
	srv, host, port := newFakeServer(t)
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)

	h := p.SessionHealth(context.Background())
	if !h.Connected {
		t.Error("Connected = false on live session")
	}
	if !h.Reachable {
		t.Error("Reachable = false on loopback session")
	}
	if h.LastRx.IsZero() {
		t.Error("LastRx zero — handshake frames should have advanced it")
	}
}

func TestSessionHealth_LoopbackNoSession(t *testing.T) {
	p := &Plugin{logger: testLogger()}
	h := p.SessionHealth(context.Background())
	if h.Connected {
		t.Error("Connected = true with no session")
	}
}

// TestGetValue_RawBodyFallback exercises the branch where the reply does
// not carry the requested pid at all — GetValue returns the raw body.
func TestGetValue_RawBodyFallback(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		switch req.Func {
		case codec.ACP2FuncGetObject:
			return objectReply(4, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNumber)),
				u32Prop(codec.PIDNumberType, 0, uint32(codec.NumTypeU32)),
			})
		case codec.ACP2FuncGetProperty:
			// Reply carries a DIFFERENT pid than requested (pid=8) →
			// GetValue falls through to the raw-body return.
			return propertyReply(codec.ACP2FuncGetProperty, 4,
				u32Prop(codec.PIDAccess, 0, 1))
		}
		return errorReply(codec.ErrProtocol)
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	val, err := p.GetValue(context.Background(), consumer.ValueRequest{Slot: 0, ID: 4})
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if val.Kind != consumer.KindRaw {
		t.Errorf("kind = %v, want KindRaw fallback", val.Kind)
	}
}
