package cerebrumnb

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/cerebrum-nb/codec"
	"dhs/internal/consumer"
)

func conValReq(path string) consumer.ValueRequest { return consumer.ValueRequest{Path: path} }
func conStrVal(s string) consumer.Value           { return consumer.Value{Kind: consumer.KindString, Str: s} }
func conIntVal() consumer.Value                    { return consumer.Value{Kind: consumer.KindInt, Int: 1} }

func ctx2s(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 2*time.Second)
}

// dialFake connects a Plugin to a fakeCerebrum WITHOUT auto-login (no
// credentials). Returns the plugin + live session.
func dialFake(t *testing.T, fs *fakeCerebrum) (*Plugin, *Session) {
	t.Helper()
	p := NewPlugin(nil)
	ctx, cancel := ctx2s(t)
	defer cancel()
	if err := p.Connect(ctx, fs.host(), fs.port()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = p.Disconnect() })
	return p, p.Session()
}

// ----------------------------------------------------------------------
// Connect / Login / Disconnect
// ----------------------------------------------------------------------

func TestConnect_NoLogin(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		// No credentials → Connect does not LOGIN; just hold the conn.
		_, _ = fc.readClientFrame()
	})
	p, sess := dialFake(t, fs)
	if sess == nil {
		t.Fatal("nil session")
	}
	if sess.LoggedIn() {
		t.Fatal("should not be logged in without creds")
	}
	h, port := sess.RemoteHostPort()
	if h != fs.host() || port != fs.port() {
		t.Fatalf("RemoteHostPort = %s:%d", h, port)
	}
	_ = p.Disconnect()
	_ = p.Disconnect() // idempotent
}

func TestConnect_AutoLogin_Success(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		serveLoginThen(fc, "0.16", func(fc *fakeConn) { _, _ = fc.readClientFrame() })
	})
	p := NewPlugin(nil)
	p.Username = "admin"
	p.Password = "s3cr3t"
	ctx, cancel := ctx2s(t)
	defer cancel()
	if err := p.Connect(ctx, fs.host(), fs.port()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()
	sess := p.Session()
	if !sess.LoggedIn() {
		t.Fatal("expected logged in")
	}
	if sess.APIVersion() != "0.16" {
		t.Fatalf("api_ver = %q", sess.APIVersion())
	}
	if sess.APIVersionMajor() != 0 {
		t.Fatalf("api major = %d", sess.APIVersionMajor())
	}
}

func TestConnect_AlreadyConnected(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) { _, _ = fc.readClientFrame() })
	p, _ := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	err := p.Connect(ctx, fs.host(), fs.port())
	if err == nil || !strings.Contains(err.Error(), "already connected") {
		t.Fatalf("want already-connected error, got %v", err)
	}
}

func TestConnect_AutoLogin_Nack(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		frame, err := fc.readClientFrame()
		if err != nil {
			return
		}
		_ = fc.writeText(nackFor(frame, "INVALID_USER_OR_PASS", 0))
	})
	p := NewPlugin(nil)
	p.Username = "x"
	p.Password = "y"
	ctx, cancel := ctx2s(t)
	defer cancel()
	err := p.Connect(ctx, fs.host(), fs.port())
	if err == nil || !strings.Contains(err.Error(), "login") {
		t.Fatalf("want login nack error, got %v", err)
	}
}

func TestConnect_DialError(t *testing.T) {
	p := NewPlugin(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	// Port 0 → DefaultPort 40007 on localhost, nothing listening.
	err := p.Connect(ctx, "127.0.0.1", 0)
	if err == nil || !strings.Contains(err.Error(), "ws dial") {
		t.Fatalf("want ws dial error, got %v", err)
	}
}

func TestPlugin_Login_Guards(t *testing.T) {
	p := NewPlugin(nil)
	if err := p.Login(context.Background()); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("want not-connected, got %v", err)
	}
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		serveLoginThen(fc, "0.16", func(fc *fakeConn) { _, _ = fc.readClientFrame() })
	})
	p, _ = dialFake(t, fs)
	if err := p.Login(context.Background()); err == nil || !strings.Contains(err.Error(), "must be set") {
		t.Fatalf("want creds-required, got %v", err)
	}
	p.Username, p.Password = "a", "b"
	ctx, cancel := ctx2s(t)
	defer cancel()
	if err := p.Login(ctx); err != nil {
		t.Fatalf("explicit Login: %v", err)
	}
}

// ----------------------------------------------------------------------
// Poll
// ----------------------------------------------------------------------

func TestPoll_Active(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		drainClient(fc, func(frame []byte) ([]byte, bool) {
			return []byte(`<POLL_REPLY MTID="` + mtidOf(frame) +
				`" CONNECTED_SERVER_ACTIVE="1" PRIMARY_SERVER_STATE="1" SECONDARY_SERVER_STATE="0"/>`), true
		})
	})
	_, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	pr, err := sess.Poll(ctx)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if !pr.ConnectedServerActive || !pr.PrimaryServerState || pr.SecondaryServerState {
		t.Fatalf("poll flags: %+v", pr)
	}
}

func TestPoll_InactiveFiresCompliance(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		drainClient(fc, func(frame []byte) ([]byte, bool) {
			return []byte(`<POLL_REPLY MTID="` + mtidOf(frame) + `" CONNECTED_SERVER_ACTIVE="0"/>`), true
		})
	})
	p, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	if _, err := sess.Poll(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if p.Compliance().Counts()["cerebrum_server_inactive"] != 1 {
		t.Fatalf("expected cerebrum_server_inactive, got %+v", p.Compliance().Counts())
	}
}

func TestPoll_UnexpectedReply(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		drainClient(fc, func(frame []byte) ([]byte, bool) { return ackFor(frame), true })
	})
	_, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	if _, err := sess.Poll(ctx); err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("want unexpected reply, got %v", err)
	}
}

func TestPoll_Timeout(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) { _, _ = fc.readClientFrame() }) // never replies
	_, sess := dialFake(t, fs)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := sess.Poll(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline exceeded, got %v", err)
	}
}

// ----------------------------------------------------------------------
// Action: ACK / NACK / BUSY
// ----------------------------------------------------------------------

func actionFakeReplying(reply func(frame []byte) []byte) func(*fakeConn) {
	return func(fc *fakeConn) {
		drainClient(fc, func(frame []byte) ([]byte, bool) { return reply(frame), true })
	}
}

func TestAction_Ack(t *testing.T) {
	fs := newFakeCerebrum(t, actionFakeReplying(func(f []byte) []byte { return ackFor(f) }))
	_, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	if err := sess.SetDeviceValue(ctx, "DEV", "SUB", "OBJ", "42"); err != nil {
		t.Fatalf("SetDeviceValue: %v", err)
	}
}

func TestAction_Nack_RecordsCompliance(t *testing.T) {
	fs := newFakeCerebrum(t, actionFakeReplying(func(f []byte) []byte {
		return nackFor(f, "ONE_OR_MORE_ACTIONS_INVALID", 8)
	}))
	p, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	err := sess.SetDeviceValue(ctx, "DEV", "SUB", "OBJ", "42")
	var ne *codec.NackError
	if !errors.As(err, &ne) {
		t.Fatalf("want NackError, got %v", err)
	}
	if p.Compliance().Counts()["cerebrum_nack_one_or_more_actions_invalid"] != 1 {
		t.Fatalf("nack compliance not recorded: %+v", p.Compliance().Counts())
	}
}

func TestAction_Nack_UnknownCode(t *testing.T) {
	fs := newFakeCerebrum(t, actionFakeReplying(func(f []byte) []byte {
		// Bogus error name + non-resolving code → ID stays -1.
		return []byte(`<NACK MTID="` + mtidOf(f) + `" ERROR="WAT" ERROR_CODE="zzz"/>`)
	}))
	p, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	_ = sess.SetDeviceValue(ctx, "D", "S", "O", "v")
	if p.Compliance().Counts()["cerebrum_nack_unknown"] != 1 {
		t.Fatalf("want cerebrum_nack_unknown, got %+v", p.Compliance().Counts())
	}
}

func TestAction_Busy(t *testing.T) {
	fs := newFakeCerebrum(t, actionFakeReplying(func(f []byte) []byte {
		return []byte(`<BUSY MTID="` + mtidOf(f) + `"/>`)
	}))
	p, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	err := sess.SetDeviceValue(ctx, "D", "S", "O", "v")
	if err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("want busy error, got %v", err)
	}
	if p.Compliance().Counts()["cerebrum_busy_received"] != 1 {
		t.Fatalf("busy compliance not recorded: %+v", p.Compliance().Counts())
	}
}

func TestAction_UnexpectedReply(t *testing.T) {
	fs := newFakeCerebrum(t, actionFakeReplying(func(f []byte) []byte {
		return []byte(`<POLL_REPLY MTID="` + mtidOf(f) + `"/>`)
	}))
	_, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	err := sess.SetDeviceValue(ctx, "D", "S", "O", "v")
	if err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("want unexpected reply, got %v", err)
	}
}

// ----------------------------------------------------------------------
// New write-action wrappers (all map to existing codec encoders)
// ----------------------------------------------------------------------

func TestActionWrappers_AllAck(t *testing.T) {
	fs := newFakeCerebrum(t, actionFakeReplying(func(f []byte) []byte { return ackFor(f) }))
	_, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	tgt := DefaultRouteTarget()

	cases := []struct {
		name string
		call func() error
	}{
		{"lock", func() error { return sess.Lock(ctx, "DEST_LOCK", codec.LockProtect, tgt, "", "60", "1", "") }},
		{"unlock", func() error { return sess.Lock(ctx, "DEST_LOCK", codec.LockRelease, tgt, "", "60", "1", "") }},
		{"set-mnemonic", func() error { return sess.SetMnemonic(ctx, "DEST_MNE", tgt, "", "60", "1", "CAM1", "") }},
		{"set-tags", func() error { return sess.SetTags(ctx, "RM_DEST_TAGS", tgt, "", "60", "live,hd") }},
		{"salvo-run", func() error { return sess.Salvo(ctx, "RUN", "G", "I", "", "") }},
		{"salvo-save", func() error { return sess.Salvo(ctx, "SAVE", "G", "I", "", "desc") }},
		{"salvo-rename", func() error { return sess.Salvo(ctx, "RENAME", "G", "I", "NEW", "") }},
		{"salvo-delete", func() error { return sess.Salvo(ctx, "DELETE", "G", "I", "", "") }},
		{"category-create", func() error {
			return sess.Category(ctx, &codec.CategoryAction{Type: "CREATE", Category: "C", Name: "N"})
		}},
		{"category-modify", func() error {
			return sess.Category(ctx, &codec.CategoryAction{Type: "MODIFY_ITEM", Category: "C", Index: "1"})
		}},
		{"category-delete", func() error {
			return sess.Category(ctx, &codec.CategoryAction{Type: "DELETE", Category: "C"})
		}},
		{"obtain-datastore", func() error { return sess.ObtainDatastore(ctx, "path/to/file") }},
		{"subscribe-datastore", func() error { return sess.SubscribeDatastore(ctx, "path/to/file") }},
		{"unsubscribe", func() error {
			return sess.Unsubscribe(ctx, []codec.SubItem{&codec.DeviceChange{Type: "LIST"}})
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.call(); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
		})
	}
}

func TestCategory_NilGuard(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) { _, _ = fc.readClientFrame() })
	_, sess := dialFake(t, fs)
	if err := sess.Category(context.Background(), nil); err == nil ||
		!strings.Contains(err.Error(), "nil action") {
		t.Fatalf("want nil-action guard, got %v", err)
	}
}

func TestUnsubscribe_Unexpected(t *testing.T) {
	fs := newFakeCerebrum(t, actionFakeReplying(func(f []byte) []byte {
		return []byte(`<POLL_REPLY MTID="` + mtidOf(f) + `"/>`)
	}))
	_, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	err := sess.Unsubscribe(ctx, []codec.SubItem{&codec.DeviceChange{Type: "LIST"}})
	if err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("want unexpected, got %v", err)
	}
}

// ----------------------------------------------------------------------
// Subscribe / Obtain / UnsubscribeAll happy + error paths
// ----------------------------------------------------------------------

func TestSubscribeObtainUnsubscribeAll(t *testing.T) {
	fs := newFakeCerebrum(t, actionFakeReplying(func(f []byte) []byte { return ackFor(f) }))
	_, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	if err := sess.Subscribe(ctx, []codec.SubItem{&codec.DeviceChange{Type: "LIST"}}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := sess.Obtain(ctx, []codec.SubItem{&codec.DeviceChange{Type: "LIST"}}); err != nil {
		t.Fatalf("obtain: %v", err)
	}
	if err := sess.UnsubscribeAll(ctx); err != nil {
		t.Fatalf("unsubscribe_all: %v", err)
	}
}

func TestSubscribe_Unexpected(t *testing.T) {
	fs := newFakeCerebrum(t, actionFakeReplying(func(f []byte) []byte {
		return []byte(`<BUSY MTID="` + mtidOf(f) + `"/>`)
	}))
	_, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	if err := sess.Subscribe(ctx, []codec.SubItem{&codec.DeviceChange{Type: "LIST"}}); err == nil ||
		!strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("want unexpected, got %v", err)
	}
	if err := sess.Obtain(ctx, []codec.SubItem{&codec.DeviceChange{Type: "LIST"}}); err == nil {
		t.Fatal("want obtain unexpected error")
	}
	if err := sess.UnsubscribeAll(ctx); err == nil {
		t.Fatal("want unsubscribe_all unexpected error")
	}
}

// ----------------------------------------------------------------------
// Event dispatch + OnEvent + Cancel + listen (CONTINUE / snapshot)
// ----------------------------------------------------------------------

func TestDispatch_EventFanout_AndCancel(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		frame, err := fc.readClientFrame() // the SUBSCRIBE
		if err != nil {
			return
		}
		_ = fc.writeText(ackFor(frame))
		// Stream a DEVICE_CHANGE snapshot row carrying the SUBSCRIBE mtid,
		// then a free-standing ROUTING_CHANGE event.
		_ = fc.writeText([]byte(`<DEVICE_CHANGE MTID="` + mtidOf(frame) + `" TYPE="LIST"><DEVICE IP="10.0.0.1"><INSTANCE DEVICE_TYPE="Router"/></DEVICE></DEVICE_CHANGE>`))
		_ = fc.writeText([]byte(`<ROUTING_CHANGE TYPE="ROUTE" DEST_ID="60" SRCE_ID="61" LEVEL_ID="1"/>`))
		_, _ = fc.readClientFrame() // hold open until the client disconnects
	})
	p, sess := dialFake(t, fs)

	var devN, routeN atomic.Int32
	// Active subscribers: the device sub reliably fires on the snapshot row
	// (mtid-matched but non-terminal → fans out), the routing sub on the
	// free-standing event.
	sess.OnEvent(codec.KindDeviceChange, func(*codec.Frame) { devN.Add(1) })
	sess.OnEvent(codec.KindRoutingChange, func(*codec.Frame) { routeN.Add(1) })
	// A wildcard subscriber that we cancel BEFORE subscribing must NOT fire —
	// this deterministically exercises Cancel's removal path (plus the
	// nil-safe early return) without racing the event stream.
	var cancelled atomic.Int32
	wc := sess.OnEvent(codec.KindUnknown, func(*codec.Frame) { cancelled.Add(1) })
	sess.Cancel(wc)
	sess.Cancel(nil) // nil-safe

	ctx, cancel := ctx2s(t)
	defer cancel()
	if err := sess.Subscribe(ctx, []codec.SubItem{&codec.DeviceChange{Type: "LIST"}}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	// Both the device snapshot row and the routing event must reach their
	// subscribers; wait on both so the fan-out path is covered regardless
	// of scheduling.
	waitFor(t, 2*time.Second, func() bool { return devN.Load() == 1 && routeN.Load() == 1 }, "both events")
	if cancelled.Load() != 0 {
		t.Fatalf("cancelled subscriber fired %d times", cancelled.Load())
	}
	_ = p.Disconnect()
}

func TestDispatch_DecodeFailureCompliance(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		_ = fc.writeText([]byte("<<<not-xml")) // undecodable
		_, _ = fc.readClientFrame()            // hold open until the client disconnects
	})
	p, _ := dialFake(t, fs)
	waitFor(t, time.Second, func() bool {
		return p.Compliance().Counts()["cerebrum_decode_failed"] >= 1
	}, "decode_failed compliance")
}

func TestDispatch_CaseNormalizedCompliance(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		// lowercase element name → CaseChanged.
		_ = fc.writeText([]byte(`<routing_change type="ROUTE" dest_id="1"/>`))
		_, _ = fc.readClientFrame() // hold open until the client disconnects
	})
	p, sess := dialFake(t, fs)
	sess.OnEvent(codec.KindUnknown, func(*codec.Frame) {})
	waitFor(t, time.Second, func() bool {
		return p.Compliance().Counts()["cerebrum_case_normalized"] >= 1
	}, "case_normalized compliance")
}

func TestDispatch_UnknownNotificationCompliance(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		_ = fc.writeText([]byte(`<SOMETHING_ELSE FOO="1"/>`))
		_, _ = fc.readClientFrame() // hold open until the client disconnects
	})
	p, _ := dialFake(t, fs)
	waitFor(t, time.Second, func() bool {
		return p.Compliance().Counts()["cerebrum_unknown_notification"] >= 1
	}, "unknown_notification compliance")
}

func TestReadLoop_DropsNonText(t *testing.T) {
	// Wait for the client's SUBSCRIBE (callback registered) before pushing a
	// PING (handled inline by ws.Conn, never reaching dispatch) and the TEXT
	// routing event.
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		frame, err := fc.readClientFrame()
		if err != nil {
			return
		}
		_ = fc.writeText(ackFor(frame))
		_ = fc.writePing()
		_ = fc.writeText([]byte(`<ROUTING_CHANGE TYPE="ROUTE" DEST_ID="1"/>`))
		_, _ = fc.readClientFrame() // hold open until the client disconnects
	})
	_, sess := dialFake(t, fs)
	var n atomic.Int32
	sess.OnEvent(codec.KindRoutingChange, func(*codec.Frame) { n.Add(1) })
	ctx, cancel := ctx2s(t)
	defer cancel()
	if err := sess.Subscribe(ctx, []codec.SubItem{&codec.DeviceChange{Type: "LIST"}}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool { return n.Load() == 1 }, "routing after ping")
}

// ----------------------------------------------------------------------
// MTID reuse compliance
// ----------------------------------------------------------------------

func TestMTIDReuse_Compliance(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		// Read the first POLL but never reply, so its mtid stays in-flight.
		_, _ = fc.readClientFrame()
		time.Sleep(300 * time.Millisecond)
	})
	p, sess := dialFake(t, fs)

	// Pin the allocator so the second roundTrip reuses the same mtid string.
	sess.mtidNext.Store(5)
	go func() {
		c, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		defer cancel()
		_, _ = sess.roundTrip(c, 5, []byte(`<POLL MTID="5"/>`))
	}()
	// Wait until mtid 5 is registered, then fire a duplicate.
	waitFor(t, time.Second, func() bool {
		sess.mu.Lock()
		_, ok := sess.pending["5"]
		sess.mu.Unlock()
		return ok
	}, "mtid 5 in flight")

	c2, cancel2 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel2()
	_, err := sess.roundTrip(c2, 5, []byte(`<POLL MTID="5"/>`))
	if err == nil || !strings.Contains(err.Error(), "already in flight") {
		t.Fatalf("want mtid-in-flight error, got %v", err)
	}
	if p.Compliance().Counts()["cerebrum_mtid_reused"] != 1 {
		t.Fatalf("mtid reuse compliance: %+v", p.Compliance().Counts())
	}
}

func TestRoundTrip_WriteError(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) { _ = fc.c.Close() })
	_, sess := dialFake(t, fs)
	// Give the server a moment to close so the write fails.
	waitFor(t, time.Second, func() bool {
		c, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_, err := sess.Poll(c)
		return err != nil && strings.Contains(err.Error(), "write")
	}, "write error after server close")
}

// ----------------------------------------------------------------------
// consumer.Protocol shim methods
// ----------------------------------------------------------------------

func TestShim_GetDeviceInfo(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		drainClient(fc, func(frame []byte) ([]byte, bool) {
			return []byte(`<POLL_REPLY MTID="` + mtidOf(frame) + `" CONNECTED_SERVER_ACTIVE="1"/>`), true
		})
	})
	p, _ := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	di, err := p.GetDeviceInfo(ctx)
	if err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}
	if di.Port != fs.port() {
		t.Fatalf("device info port = %d", di.Port)
	}
}

func TestShim_NotConnectedErrors(t *testing.T) {
	p := NewPlugin(nil)
	ctx := context.Background()
	if _, err := p.GetDeviceInfo(ctx); err == nil {
		t.Fatal("GetDeviceInfo should error when not connected")
	}
	if _, err := p.SetValue(ctx, conValReq("x.y.z"), conStrVal("v")); err == nil {
		t.Fatal("SetValue should error when not connected")
	}
	if p.Compliance() != nil {
		t.Fatal("Compliance should be nil when not connected")
	}
	if p.Session() != nil {
		t.Fatal("Session should be nil when not connected")
	}
}

func TestShim_NotApplicable(t *testing.T) {
	p := NewPlugin(nil)
	if _, err := p.GetSlotInfo(context.Background(), 0); err == nil {
		t.Fatal("GetSlotInfo should error")
	}
	if _, err := p.Walk(context.Background(), 0); err == nil {
		t.Fatal("Walk should error")
	}
	if _, err := p.GetValue(context.Background(), conValReq("a")); err == nil {
		t.Fatal("GetValue should error")
	}
	if err := p.Subscribe(conValReq("a"), nil); err == nil {
		t.Fatal("Subscribe should error")
	}
	if err := p.Unsubscribe(conValReq("a")); err == nil {
		t.Fatal("Unsubscribe should error")
	}
}

func TestShim_SetValue(t *testing.T) {
	fs := newFakeCerebrum(t, actionFakeReplying(func(f []byte) []byte { return ackFor(f) }))
	p, _ := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()

	// Empty path → guard.
	if _, err := p.SetValue(ctx, conValReq(""), conStrVal("v")); err == nil {
		t.Fatal("empty path should error")
	}
	// Wrong kind → guard.
	if _, err := p.SetValue(ctx, conValReq("a.b.c"), conIntVal()); err == nil {
		t.Fatal("non-string value should error")
	}
	// Malformed path → guard from splitDevicePath.
	if _, err := p.SetValue(ctx, conValReq("oneonly"), conStrVal("v")); err == nil {
		t.Fatal("malformed path should error")
	}
	// Happy path.
	v, err := p.SetValue(ctx, conValReq("DEV.SUB.OBJ"), conStrVal("hello"))
	if err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if v.Str != "hello" {
		t.Fatalf("returned value = %q", v.Str)
	}
}

func TestShim_SetValue_ActionError(t *testing.T) {
	fs := newFakeCerebrum(t, actionFakeReplying(func(f []byte) []byte {
		return nackFor(f, "NOT_LOGGED_IN", 6)
	}))
	p, _ := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	if _, err := p.SetValue(ctx, conValReq("DEV.SUB.OBJ"), conStrVal("hello")); err == nil {
		t.Fatal("want action nack error")
	}
}

// ----------------------------------------------------------------------
// Factory + NewPlugin
// ----------------------------------------------------------------------

func TestFactory(t *testing.T) {
	f := &Factory{}
	m := f.Meta()
	if m.Name != "cerebrum-nb" || m.DefaultPort != DefaultPort {
		t.Fatalf("meta = %+v", m)
	}
	if f.New(nil) == nil {
		t.Fatal("New returned nil")
	}
	if NewPlugin(nil) == nil {
		t.Fatal("NewPlugin(nil) returned nil")
	}
}

// closeWhileReading exercises Session.close racing the readLoop EOF path.
func TestClose_StopsReadLoop(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		defer wg.Done()
		_, _ = fc.readClientFrame()
		<-time.After(50 * time.Millisecond)
	})
	p, sess := dialFake(t, fs)
	if err := sess.close(); err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("close: %v", err)
	}
	// Second close is idempotent.
	_ = sess.close()
	_ = p.Disconnect()
	wg.Wait()
}
