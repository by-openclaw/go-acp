package session

import (
	"context"
	"errors"
	"testing"

	"dhs/internal/snell-rollcall/codec"
)

// TestWalk_BlockHeaderThenItems covers the 16-bit multi-packet shape: a
// request, a header saying how many items follow, then one fetch per item.
//
// The index in each fetch is an offset from the base of the request that
// opened the transfer, and the server holds that base as state. So a walk owns
// the session for its duration, which the one-in-flight rule already
// guarantees.
func TestWalk_BlockHeaderThenItems(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcMenus)

	const count = 3
	type collected struct {
		index int
		frame codec.Frame
	}
	got := make([]collected, 0, count)

	done := make(chan error, 1)
	go func() {
		done <- Walk(context.Background(), s, codec.MsgGetFunc, []byte{0, 0},
			func(i int, f codec.Frame) error {
				got = append(got, collected{i, f})
				return nil
			})
	}()

	open := h.peer.recvType(codec.MsgGetFunc)
	hdr := codec.BlockHeader{PktType: codec.MsgGetFunc, Count: count, MaxSize: codec.FuncSize}
	h.peer.send(open, codec.MsgBlockHeader, hdr.AppendTo(nil))

	for i := range count {
		req := h.peer.recvType(codec.MsgGetNextPkt)

		next, err := codec.DecodeGetNext(req.Payload)
		if err != nil {
			t.Fatalf("DecodeGetNext: %v", err)
		}
		if int(next.Index) != i {
			t.Errorf("fetch %d asked for index %d", i, next.Index)
		}
		// The type names the request that opened the transfer, which is how
		// the server knows which base the offset applies to.
		if next.PktType != codec.MsgGetFunc {
			t.Errorf("fetch %d named %s, want GETFUNC", i, next.PktType)
		}

		line, err := codec.Func{MenuIndex: uint16(i), Style: codec.StyleNumber, Text: "line"}.AppendTo(nil)
		if err != nil {
			t.Fatalf("AppendTo: %v", err)
		}
		h.peer.send(req, codec.MsgRetFunc, line)
	}

	if err := <-done; err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(got) != count {
		t.Fatalf("collected %d items, want %d", len(got), count)
	}
	for i, c := range got {
		if c.index != i {
			t.Errorf("item %d reported index %d", i, c.index)
		}
		if c.frame.Type != codec.MsgRetFunc {
			t.Errorf("item %d = %s, want RETFUNC", i, c.frame.Type)
		}
	}
}

// TestWalk_SingleItemWithoutABlock covers a server that answers a one-item
// transfer with the item itself. It is not refusing, and treating it as an
// error would lose the answer.
func TestWalk_SingleItemWithoutABlock(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcMenus)

	var seen int
	done := make(chan error, 1)
	go func() {
		done <- Walk(context.Background(), s, codec.MsgGetDevList, nil,
			func(int, codec.Frame) error { seen++; return nil })
	}()

	req := h.peer.recvType(codec.MsgGetDevList)
	info, err := codec.DeviceInfo{Address: peerAddr}.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	h.peer.send(req, codec.MsgRetDevInfo, info)

	if err := <-done; err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if seen != 1 {
		t.Errorf("collected %d items, want 1", seen)
	}
}

// TestWalk_EmptyBlock covers a device with nothing to list, which is ordinary
// on an empty slot rather than an error.
func TestWalk_EmptyBlock(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcMenus)

	var seen int
	done := make(chan error, 1)
	go func() {
		done <- Walk(context.Background(), s, codec.MsgGetFunc, []byte{0, 0},
			func(int, codec.Frame) error { seen++; return nil })
	}()

	req := h.peer.recvType(codec.MsgGetFunc)
	h.peer.send(req, codec.MsgBlockHeader, codec.BlockHeader{PktType: codec.MsgGetFunc}.AppendTo(nil))

	if err := <-done; err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if seen != 0 {
		t.Errorf("collected %d items from an empty block", seen)
	}
	h.peer.quiet()
}

// TestWalk_CallerStops covers a caller that has seen enough. The walk stops
// where it is rather than fetching every remaining item.
func TestWalk_CallerStops(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcMenus)

	stop := errors.New("enough")
	done := make(chan error, 1)
	go func() {
		done <- Walk(context.Background(), s, codec.MsgGetFunc, []byte{0, 0},
			func(int, codec.Frame) error { return stop })
	}()

	open := h.peer.recvType(codec.MsgGetFunc)
	h.peer.send(open, codec.MsgBlockHeader,
		codec.BlockHeader{PktType: codec.MsgGetFunc, Count: 500}.AppendTo(nil))

	req := h.peer.recvType(codec.MsgGetNextPkt)
	h.peer.send(req, codec.MsgRetFunc, make([]byte, codec.FuncSize))

	if err := <-done; !errors.Is(err, stop) {
		t.Fatalf("err = %v, want the caller's own error", err)
	}
	h.peer.quiet()
}

func TestWalk_Errors(t *testing.T) {
	t.Run("request refused", func(t *testing.T) {
		h := newHarness(t, Config{})
		s := h.callSession(t, codec.SvcMenus)

		done := make(chan error, 1)
		go func() {
			done <- Walk(context.Background(), s, codec.MsgGetFunc, nil,
				func(int, codec.Frame) error { return nil })
		}()
		req := h.peer.recvType(codec.MsgGetFunc)
		h.peer.send(req, codec.MsgNack, nil)

		if err := <-done; !errors.Is(err, ErrRefused) {
			t.Errorf("err = %v, want ErrRefused", err)
		}
	})

	t.Run("malformed header", func(t *testing.T) {
		h := newHarness(t, Config{})
		s := h.callSession(t, codec.SvcMenus)

		done := make(chan error, 1)
		go func() {
			done <- Walk(context.Background(), s, codec.MsgGetFunc, nil,
				func(int, codec.Frame) error { return nil })
		}()
		req := h.peer.recvType(codec.MsgGetFunc)
		h.peer.send(req, codec.MsgBlockHeader, []byte{1, 2})

		if err := <-done; !errors.Is(err, codec.ErrShortBuffer) {
			t.Errorf("err = %v, want ErrShortBuffer", err)
		}
	})

	t.Run("item refused", func(t *testing.T) {
		h := newHarness(t, Config{})
		s := h.callSession(t, codec.SvcMenus)

		done := make(chan error, 1)
		go func() {
			done <- Walk(context.Background(), s, codec.MsgGetFunc, nil,
				func(int, codec.Frame) error { return nil })
		}()
		open := h.peer.recvType(codec.MsgGetFunc)
		h.peer.send(open, codec.MsgBlockHeader,
			codec.BlockHeader{PktType: codec.MsgGetFunc, Count: 2}.AppendTo(nil))

		req := h.peer.recvType(codec.MsgGetNextPkt)
		h.peer.send(req, codec.MsgNack, nil)

		err := <-done
		if !errors.Is(err, ErrRefused) {
			t.Errorf("err = %v, want ErrRefused", err)
		}
		// The failure names which item of how many, because "walk failed"
		// alone does not say whether a menu is half read.
		if !contains(err.Error(), "item 1 of 2") {
			t.Errorf("err = %q, want it to say which item failed", err)
		}
	})
}

// TestWalkMenu32 covers the 32-bit menu shape, which is a different one: a
// count, then each item by absolute index, with no state held on the server.
func TestWalkMenu32(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcMenus|codec.SvcLongStr)

	const base = 0x0100
	const count = 3

	var got []codec.MenuItem
	done := make(chan error, 1)
	go func() {
		done <- WalkMenu32(context.Background(), s, base, func(m codec.MenuItem) error {
			got = append(got, m)
			return nil
		})
	}()

	req := h.peer.recvType(codec.MsgGetMenuCount)
	askedFor, err := codec.DecodeMenuReq(req.Payload)
	if err != nil {
		t.Fatalf("DecodeMenuReq: %v", err)
	}
	if askedFor.MenuIndex != base {
		t.Errorf("asked for base %d, want %d", askedFor.MenuIndex, base)
	}
	h.peer.send(req, codec.MsgRetMenuCount,
		codec.MenuSize{MenuIndex: base, MenuCount: count}.AppendTo(nil))

	for i := range count {
		item := h.peer.recvType(codec.MsgGetMenuItem)
		r, err := codec.DecodeMenuReq(item.Payload)
		if err != nil {
			t.Fatalf("DecodeMenuReq: %v", err)
		}
		// Absolute, not an offset. That is what makes the request stateless.
		if r.MenuIndex != base+uint32(i) {
			t.Errorf("item %d asked for index %d, want the absolute %d",
				i, r.MenuIndex, base+uint32(i))
		}

		payload, err := codec.MenuItem{
			MenuIndex: r.MenuIndex,
			Style:     codec.StyleNumber,
			Command:   0x0001_0000 + r.MenuIndex,
			Text:      "Level",
		}.AppendTo(nil)
		if err != nil {
			t.Fatalf("AppendTo: %v", err)
		}
		h.peer.send(item, codec.MsgRetMenuItem, payload)
	}

	if err := <-done; err != nil {
		t.Fatalf("WalkMenu32: %v", err)
	}
	if len(got) != count {
		t.Fatalf("collected %d items, want %d", len(got), count)
	}
	for i, m := range got {
		if m.MenuIndex != base+uint32(i) {
			t.Errorf("item %d has index %d", i, m.MenuIndex)
		}
		if m.Text != "Level" {
			t.Errorf("item %d text = %q", i, m.Text)
		}
	}
}

func TestWalkMenu32_Errors(t *testing.T) {
	t.Run("wrong reply to the count", func(t *testing.T) {
		h := newHarness(t, Config{})
		s := h.callSession(t, codec.SvcMenus|codec.SvcLongStr)

		done := make(chan error, 1)
		go func() {
			done <- WalkMenu32(context.Background(), s, 0, func(codec.MenuItem) error { return nil })
		}()
		req := h.peer.recvType(codec.MsgGetMenuCount)
		// An acknowledgement is not a count, and treating it as one would
		// walk a menu of whatever the empty payload decoded to.
		h.peer.send(req, codec.MsgAck, nil)

		if err := <-done; !errors.Is(err, ErrUnexpectedReply) {
			t.Errorf("err = %v, want ErrUnexpectedReply", err)
		}
	})

	t.Run("malformed count", func(t *testing.T) {
		h := newHarness(t, Config{})
		s := h.callSession(t, codec.SvcMenus|codec.SvcLongStr)

		done := make(chan error, 1)
		go func() {
			done <- WalkMenu32(context.Background(), s, 0, func(codec.MenuItem) error { return nil })
		}()
		req := h.peer.recvType(codec.MsgGetMenuCount)
		h.peer.send(req, codec.MsgRetMenuCount, []byte{1, 2})

		if err := <-done; !errors.Is(err, codec.ErrShortBuffer) {
			t.Errorf("err = %v, want ErrShortBuffer", err)
		}
	})

	t.Run("count refused", func(t *testing.T) {
		h := newHarness(t, Config{})
		s := h.callSession(t, codec.SvcMenus|codec.SvcLongStr)

		done := make(chan error, 1)
		go func() {
			done <- WalkMenu32(context.Background(), s, 0, func(codec.MenuItem) error { return nil })
		}()
		req := h.peer.recvType(codec.MsgGetMenuCount)
		h.peer.send(req, codec.MsgNack, nil)

		if err := <-done; !errors.Is(err, ErrRefused) {
			t.Errorf("err = %v, want ErrRefused", err)
		}
	})

	t.Run("item refused", func(t *testing.T) {
		h := newHarness(t, Config{})
		s := h.callSession(t, codec.SvcMenus|codec.SvcLongStr)

		done := make(chan error, 1)
		go func() {
			done <- WalkMenu32(context.Background(), s, 0, func(codec.MenuItem) error { return nil })
		}()
		req := h.peer.recvType(codec.MsgGetMenuCount)
		h.peer.send(req, codec.MsgRetMenuCount, codec.MenuSize{MenuCount: 4}.AppendTo(nil))

		item := h.peer.recvType(codec.MsgGetMenuItem)
		h.peer.send(item, codec.MsgNack, nil)

		err := <-done
		if !errors.Is(err, ErrRefused) {
			t.Errorf("err = %v, want ErrRefused", err)
		}
		if !contains(err.Error(), "item 1 of 4") {
			t.Errorf("err = %q, want it to say which item failed", err)
		}
	})

	t.Run("caller stops", func(t *testing.T) {
		h := newHarness(t, Config{})
		s := h.callSession(t, codec.SvcMenus|codec.SvcLongStr)

		stop := errors.New("enough")
		done := make(chan error, 1)
		go func() {
			done <- WalkMenu32(context.Background(), s, 0, func(codec.MenuItem) error { return stop })
		}()
		req := h.peer.recvType(codec.MsgGetMenuCount)
		h.peer.send(req, codec.MsgRetMenuCount, codec.MenuSize{MenuCount: 500}.AppendTo(nil))

		item := h.peer.recvType(codec.MsgGetMenuItem)
		h.peer.send(item, codec.MsgRetMenuItem, make([]byte, codec.MenuItemSize))

		if err := <-done; !errors.Is(err, stop) {
			t.Errorf("err = %v, want the caller's own error", err)
		}
		h.peer.quiet()
	})
}

// TestGetMenuItem covers fetching one line on its own, which is what a
// back-channel update prompts: the peer says a line changed, and only that
// line is re-read.
func TestGetMenuItem(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcMenus|codec.SvcLongStr)

	type result struct {
		item codec.MenuItem
		err  error
	}
	done := make(chan result, 1)
	go func() {
		m, err := GetMenuItem(context.Background(), s, 42)
		done <- result{m, err}
	}()

	req := h.peer.recvType(codec.MsgGetMenuItem)
	payload, err := codec.MenuItem{MenuIndex: 42, Text: "Gain"}.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	h.peer.send(req, codec.MsgRetMenuItem, payload)

	r := <-done
	if r.err != nil {
		t.Fatalf("GetMenuItem: %v", r.err)
	}
	if r.item.MenuIndex != 42 || r.item.Text != "Gain" {
		t.Errorf("= %+v", r.item)
	}
}

func TestGetMenuItem_Errors(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcMenus|codec.SvcLongStr)

	done := make(chan error, 1)
	go func() {
		_, err := GetMenuItem(context.Background(), s, 1)
		done <- err
	}()
	req := h.peer.recvType(codec.MsgGetMenuItem)
	h.peer.send(req, codec.MsgAck, nil)

	if err := <-done; !errors.Is(err, ErrUnexpectedReply) {
		t.Errorf("err = %v, want ErrUnexpectedReply", err)
	}

	go func() {
		_, err := GetMenuItem(context.Background(), s, 1)
		done <- err
	}()
	req = h.peer.recvType(codec.MsgGetMenuItem)
	h.peer.send(req, codec.MsgRetMenuItem, []byte{1, 2})

	if err := <-done; !errors.Is(err, codec.ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
}
