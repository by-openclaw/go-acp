package cerebrumnb

import (
	"context"
	"strings"
	"testing"
	"time"

	"dhs/internal/cerebrum-nb/codec"
	"dhs/internal/consumer"
)

// valueReply builds one §5.4.3 VALUE reply document.
func valueReply(dev, obj, val, dataType, avail string) string {
	return `<DEVICE_CHANGE TYPE="VALUE" DEVICE_NAME="` + dev + `" SUB_DEVICE="1" OBJECT="` + obj + `">` +
		`<OBJECT_VALUE OBJECT="` + obj + `" VALUE="` + val + `" AVAILABLE="` + avail + `" DATA_TYPE="` + dataType + `" READABLE="1" WRITABLE="1"/>` +
		`</DEVICE_CHANGE>`
}

// serveValueObtain answers every client frame with the given docs (in
// order) followed by an ACK for the frame's mtid.
func serveValueObtain(docs ...string) func(*fakeConn) {
	return func(fc *fakeConn) {
		for {
			frame, err := fc.readClientFrame()
			if err != nil {
				return
			}
			for _, d := range docs {
				_ = fc.writeText([]byte(d))
			}
			_ = fc.writeText([]byte(`<ACK MTID="` + mtidOf(frame) + `"/>`))
		}
	}
}

func TestGetValue_CanonicalFloat(t *testing.T) {
	// Decoys first (a DETAILS doc and another device's VALUE doc) — the
	// obtain watcher must filter both before accepting the real reply.
	fs := newFakeCerebrum(t, serveValueObtain(
		`<DEVICE_CHANGE TYPE="DETAILS" DEVICE_NAME="dev1"/>`,
		valueReply("other", "A.B", "9", "FLOAT", "1"),
		valueReply("dev1", "A.B", "5.5", "FLOAT", "1")))
	p, _ := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	v, err := p.GetValue(ctx, consumer.ValueRequest{Path: "dev1.1.A.B"})
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if v.Kind != consumer.KindFloat || v.Float != 5.5 {
		t.Fatalf("value = %+v", v)
	}
}

func TestGetValue_NotAvailable(t *testing.T) {
	fs := newFakeCerebrum(t, serveValueObtain(valueReply("dev1", "A.B", "0", "FLOAT", "0")))
	p, _ := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	if _, err := p.GetValue(ctx, consumer.ValueRequest{Path: "dev1.1.A.B"}); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("want not-available error, got %v", err)
	}
}

func TestGetValue_FallbackFirstValue(t *testing.T) {
	// Reply names a DIFFERENT object (group echo) — the fallback arm
	// returns the first OBJECT_VALUE.
	fs := newFakeCerebrum(t, serveValueObtain(valueReply("dev1", "A.OTHER", "7", "INTEGER", "1")))
	p, _ := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	v, err := p.GetValue(ctx, consumer.ValueRequest{Path: "dev1.1.A.B"})
	if err != nil {
		t.Fatalf("GetValue fallback: %v", err)
	}
	if v.Kind != consumer.KindInt || v.Int != 7 {
		t.Fatalf("fallback value = %+v", v)
	}
}

func TestGetValue_NoReply(t *testing.T) {
	// ACK only, no VALUE event → deadline arm. A short ctx deadline also
	// covers the deadline-shorter-than-default branch.
	fs := newFakeCerebrum(t, serveValueObtain())
	p, _ := dialFake(t, fs)
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	if _, err := p.GetValue(ctx, consumer.ValueRequest{Path: "dev1.1.A.B"}); err == nil || !strings.Contains(err.Error(), "no VALUE reply") {
		t.Fatalf("want no-reply error, got %v", err)
	}
}

func TestGetValue_DuplicateEventsHitDefaultArms(t *testing.T) {
	// Two matching docs then two group-echo docs: the second of each
	// pair finds the result channel already full — the non-blocking
	// send's default arm (both the match and the fallback branch).
	fs := newFakeCerebrum(t, serveValueObtain(
		valueReply("dev1", "A.B", "1.5", "FLOAT", "1"),
		valueReply("dev1", "A.B", "2.5", "FLOAT", "1"),
		valueReply("dev1", "A.OTHER", "3", "INTEGER", "1"),
		valueReply("dev1", "A.OTHER", "4", "INTEGER", "1"),
	))
	p, _ := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	v, err := p.GetValue(ctx, consumer.ValueRequest{Path: "dev1.1.A.B"})
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if v.Kind != consumer.KindFloat || v.Float != 1.5 {
		t.Fatalf("first event must win, got %+v", v)
	}
}

func TestGetValue_FallbackDuplicate(t *testing.T) {
	// Only group-echo docs, twice: covers the fallback branch's
	// default arm when the channel is already filled by the first.
	fs := newFakeCerebrum(t, serveValueObtain(
		valueReply("dev1", "A.OTHER", "3", "INTEGER", "1"),
		valueReply("dev1", "A.OTHER", "4", "INTEGER", "1"),
	))
	p, _ := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	v, err := p.GetValue(ctx, consumer.ValueRequest{Path: "dev1.1.A.B"})
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if v.Int != 3 {
		t.Fatalf("first fallback must win, got %+v", v)
	}
}

func TestGetValue_ObtainRefused(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		for {
			frame, err := fc.readClientFrame()
			if err != nil {
				return
			}
			_ = fc.writeText(nackFor(frame, "ONE_OR_MORE_OBTAINS_INVALID", 10))
		}
	})
	p, _ := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	if _, err := p.GetValue(ctx, consumer.ValueRequest{Path: "dev1.1.A.B"}); err == nil {
		t.Fatal("want obtain-refused error")
	}
}

func TestGetValue_Guards(t *testing.T) {
	p := NewPlugin(nil)
	if _, err := p.GetValue(context.Background(), consumer.ValueRequest{Path: "a.1.b"}); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("want not-connected, got %v", err)
	}
	fs := newFakeCerebrum(t, serveValueObtain())
	p, _ = dialFake(t, fs)
	if _, err := p.GetValue(context.Background(), consumer.ValueRequest{}); err == nil || !strings.Contains(err.Error(), "requires Path") {
		t.Fatalf("want path-required, got %v", err)
	}
	if _, err := p.GetValue(context.Background(), consumer.ValueRequest{Path: "nodots"}); err == nil {
		t.Fatal("want bad-path error")
	}
}

func TestSubscribe_EventDispatchAndUnsubscribe(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		for {
			frame, err := fc.readClientFrame()
			if err != nil {
				return
			}
			_ = fc.writeText([]byte(`<ACK MTID="` + mtidOf(frame) + `"/>`))
			// After the SUBSCRIBE ack, stream a mixed batch: non-VALUE,
			// wrong device, wrong object, then the real event.
			_ = fc.writeText([]byte(`<DEVICE_CHANGE TYPE="DETAILS" DEVICE_NAME="dev1"/>`))
			_ = fc.writeText([]byte(valueReply("other", "A.B", "1", "FLOAT", "1")))
			_ = fc.writeText([]byte(valueReply("dev1", "X.Y", "2", "FLOAT", "1")))
			_ = fc.writeText([]byte(valueReply("dev1", "A.B", "3.25", "FLOAT", "1")))
		}
	})
	p, _ := dialFake(t, fs)

	got := make(chan consumer.Event, 4)
	if err := p.Subscribe(consumer.ValueRequest{Path: "dev1.1.A.B"}, func(e consumer.Event) { got <- e }); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	select {
	case e := <-got:
		if e.Path != "dev1.1.A.B" || e.Value.Kind != consumer.KindFloat || e.Value.Float != 3.25 {
			t.Fatalf("event = %+v", e)
		}
		if e.Access != 0x03 {
			t.Fatalf("access = %x", e.Access)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event dispatched")
	}

	// Duplicate subscribe replaces the old local dispatch.
	if err := p.Subscribe(consumer.ValueRequest{Path: "dev1.1.A.B"}, func(consumer.Event) {}); err != nil {
		t.Fatalf("re-subscribe: %v", err)
	}
	if err := p.Unsubscribe(consumer.ValueRequest{Path: "dev1.1.A.B"}); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	if err := p.Unsubscribe(consumer.ValueRequest{Path: "dev1.1.A.B"}); err == nil || !strings.Contains(err.Error(), "no subscription") {
		t.Fatalf("want no-subscription error, got %v", err)
	}
}

func TestSubscribe_GuardsAndNack(t *testing.T) {
	p := NewPlugin(nil)
	if err := p.Subscribe(consumer.ValueRequest{Path: "a.1.b"}, func(consumer.Event) {}); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("want not-connected, got %v", err)
	}
	if err := p.Unsubscribe(consumer.ValueRequest{Path: "a.1.b"}); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("unsubscribe want not-connected, got %v", err)
	}

	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		for {
			frame, err := fc.readClientFrame()
			if err != nil {
				return
			}
			_ = fc.writeText(nackFor(frame, "NOT_LOGGED_IN", 6))
		}
	})
	p, _ = dialFake(t, fs)
	if err := p.Subscribe(consumer.ValueRequest{}, func(consumer.Event) {}); err == nil || !strings.Contains(err.Error(), "requires Path") {
		t.Fatalf("want path-required, got %v", err)
	}
	if err := p.Subscribe(consumer.ValueRequest{Path: "nodots"}, func(consumer.Event) {}); err == nil {
		t.Fatal("want bad-path error")
	}
	if err := p.Subscribe(consumer.ValueRequest{Path: "dev1.1.A.B"}, func(consumer.Event) {}); err == nil {
		t.Fatal("want subscribe NACK error")
	}
}

func TestUnsubscribe_WireNack(t *testing.T) {
	subscribed := false
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		for {
			frame, err := fc.readClientFrame()
			if err != nil {
				return
			}
			if !subscribed {
				subscribed = true
				_ = fc.writeText([]byte(`<ACK MTID="` + mtidOf(frame) + `"/>`))
				continue
			}
			_ = fc.writeText(nackFor(frame, "NOT_LOGGED_IN", 6))
		}
	})
	p, _ := dialFake(t, fs)
	if err := p.Subscribe(consumer.ValueRequest{Path: "dev1.1.A.B"}, func(consumer.Event) {}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := p.Unsubscribe(consumer.ValueRequest{Path: "dev1.1.A.B"}); err == nil {
		t.Fatal("want wire-unsubscribe NACK error")
	}
}

func TestDeviceValueToCanonical(t *testing.T) {
	ov := func(v, dt string) *codec.DeviceObjectValue {
		return &codec.DeviceObjectValue{Value: v, DataType: dt}
	}
	cases := []struct {
		v, dt string
		kind  consumer.ValueKind
		check func(consumer.Value) bool
	}{
		{"5.5", "FLOAT", consumer.KindFloat, func(x consumer.Value) bool { return x.Float == 5.5 }},
		{"2.25", "DOUBLE", consumer.KindFloat, func(x consumer.Value) bool { return x.Float == 2.25 }},
		{"bogus", "FLOAT", consumer.KindString, func(x consumer.Value) bool { return x.Str == "bogus" }},
		{"42", "INTEGER", consumer.KindInt, func(x consumer.Value) bool { return x.Int == 42 }},
		{"9", "LONG", consumer.KindInt, func(x consumer.Value) bool { return x.Int == 9 }},
		{"x", "INT", consumer.KindString, func(x consumer.Value) bool { return x.Str == "x" }},
		{"1", "BOOL", consumer.KindBool, func(x consumer.Value) bool { return x.Bool }},
		{"false", "BOOLEAN", consumer.KindBool, func(x consumer.Value) bool { return !x.Bool }},
		{"maybe", "BOOL", consumer.KindString, func(x consumer.Value) bool { return x.Str == "maybe" }},
		{"2", "ENUM", consumer.KindEnum, func(x consumer.Value) bool { return x.Enum == 2 && x.Str == "2" }},
		{"High", "ENUM", consumer.KindEnum, func(x consumer.Value) bool { return x.Str == "High" }},
		{"hello", "STRING", consumer.KindString, func(x consumer.Value) bool { return x.Str == "hello" }},
	}
	for _, c := range cases {
		got := deviceValueToCanonical(ov(c.v, c.dt))
		if got.Kind != c.kind || !c.check(got) {
			t.Errorf("(%q,%q) = %+v, want kind %v", c.v, c.dt, got, c.kind)
		}
	}
}

func TestDeviceAccessBits(t *testing.T) {
	cases := []struct {
		r, w bool
		want uint8
	}{{true, true, 0x03}, {true, false, 0x01}, {false, true, 0x02}, {false, false, 0x00}}
	for _, c := range cases {
		if got := deviceAccessBits(&codec.DeviceObjectValue{Readable: c.r, Writable: c.w}); got != c.want {
			t.Errorf("access(%v,%v) = %x want %x", c.r, c.w, got, c.want)
		}
	}
}
