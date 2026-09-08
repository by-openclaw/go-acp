package rollcall

import (
	"context"
	"testing"

	"dhs/internal/snell-rollcall/codec"
)

// A session negotiates one generation and keeps it. A message from the other
// one is answered anyway — it is well formed and refusing would break a client
// that otherwise works — but it must leave a trace, which is exactly what it
// failed to do while a vendor panel was connecting and failing for reasons no
// log could explain.

func TestA32BitMessageOnA16BitSessionIsCounted(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcMenus|codec.SvcControl) // no LongStr => 16-bit

	// GetMenuCount belongs to the long-string generation.
	if _, err := sess.Do(context.Background(), codec.MsgGetMenuCount,
		codec.MenuReq{MenuIndex: 0}.AppendTo(nil)); err != nil {
		t.Fatalf("the message should still be answered: %v", err)
	}
	if !hasEvent(s.p, EventMixedGeneration) {
		t.Error("a 32-bit message on a 16-bit session went unrecorded")
	}
}

func TestA16BitMessageOnA32BitSessionIsCounted(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	// GetFStat belongs to the 16-bit generation.
	_, _ = sess.Do(context.Background(), codec.MsgGetFStat,
		codec.GetFStat{Command: 1}.AppendTo(nil))

	if !hasEvent(s.p, EventMixedGeneration) {
		t.Error("a 16-bit message on a 32-bit session went unrecorded")
	}
}

func TestAMessageBelongingToBothIsNotCounted(t *testing.T) {
	// Most of the protocol is the same in both generations, and none of it
	// should be reported as mixing.
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcMenus|codec.SvcControl)
	ctx := context.Background()

	for _, typ := range []codec.PacketType{codec.MsgGetID, codec.MsgGetStat, codec.MsgKeepAlive} {
		if _, err := sess.Do(ctx, typ, nil); err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
	}
	if hasEvent(s.p, EventMixedGeneration) {
		t.Error("messages common to both generations were reported as mixed")
	}
}

func TestA16BitFrameWithholdsLongStrings(t *testing.T) {
	// A frame that does not advertise SV_LONGSTR cannot be asked for the newer
	// generation, so every client on it speaks 16-bit. It is how a 16-bit
	// device is emulated from the same tree, without a second implementation
	// to disagree with the first.
	s := newServed(t, testTree())
	s.p.SetLongStrings(false)

	if s.p.served().LongStrings() {
		t.Error("a 16-bit frame still offers the long-string service")
	}
	id, ok := s.p.identityOf(0)
	if !ok {
		t.Fatal("the gateway has no identity")
	}
	if id.Services.LongStrings() {
		t.Error("the gateway still advertises long strings")
	}
	card, ok := s.p.identityOf(1)
	if !ok {
		t.Fatal("card 1 has no identity")
	}
	if card.Services.LongStrings() {
		t.Error("a card still advertises long strings")
	}
	// What it announces has to agree with what it answers.
	if s.p.gatewayInfo().ID.Services.LongStrings() {
		t.Error("the announcement still offers long strings")
	}
}

func TestA32BitFrameOffersLongStrings(t *testing.T) {
	s := newServed(t, testTree())

	if !s.p.served().LongStrings() {
		t.Error("the default frame should offer the newer generation")
	}
	s.p.SetLongStrings(false)
	s.p.SetLongStrings(true)
	if !s.p.served().LongStrings() {
		t.Error("turning it back on did not restore it")
	}
}
