package session

import (
	"context"
	"fmt"

	"dhs/internal/snell-rollcall/codec"
)

// WalkFunc receives one item of a multi-packet transfer. Returning an error
// stops the walk and is returned to the caller.
type WalkFunc func(index int, f codec.Frame) error

// Walk performs a multi-packet transfer in the 16-bit generation.
//
// The shape is: a request, a BlockHeader saying how many items follow, then one
// GetNextPkt per item. It carries menus, device lists, port lists and file
// directories, which is why it lives here rather than in any one of them.
//
// The index in a GetNextPkt is an offset from the base of the request that
// opened the transfer, and the server holds that base as state. So a walk owns
// the session for its duration and cannot be interleaved with another, which
// the one-in-flight rule already guarantees.
//
// A server that answers the opening request with something other than a
// BlockHeader is not refusing: some answer a single-item transfer with the item
// itself. That is handled rather than treated as an error.
func Walk(ctx context.Context, s *Session, req codec.PacketType, payload []byte, fn WalkFunc) error {
	first, err := s.Do(ctx, req, payload)
	if err != nil {
		return err
	}

	if first.Type != codec.MsgBlockHeader {
		// A one-item answer with no block around it.
		return fn(0, first)
	}

	hdr, err := codec.DecodeBlockHeader(first.Payload)
	if err != nil {
		return fmt.Errorf("rollcall: walk %s: %w", req, err)
	}

	for i := range int(hdr.Count) {
		item, err := s.Do(ctx, codec.MsgGetNextPkt,
			codec.GetNext{Index: uint16(i), PktType: req}.AppendTo(nil))
		if err != nil {
			return fmt.Errorf("rollcall: walk %s item %d of %d: %w", req, i+1, hdr.Count, err)
		}
		if err := fn(i, item); err != nil {
			return err
		}
	}
	return nil
}

// WalkMenu32 performs a menu walk in the 32-bit generation.
//
// It is a different shape, and simpler. GetMenuCount returns a count, and each
// item is fetched by absolute index. Nothing is held as state on the server,
// so items may be fetched in any order — this walks in order because a menu is
// a tree written depth-first and reading it that way is what makes the spans
// meaningful.
func WalkMenu32(ctx context.Context, s *Session, base uint32, fn func(codec.MenuItem) error) error {
	reply, err := s.Do(ctx, codec.MsgGetMenuCount, codec.MenuReq{MenuIndex: base}.AppendTo(nil))
	if err != nil {
		return err
	}
	if reply.Type != codec.MsgRetMenuCount {
		return protocolError("menu count", reply)
	}

	size, err := codec.DecodeMenuSize(reply.Payload)
	if err != nil {
		return fmt.Errorf("rollcall: menu count: %w", err)
	}

	for i := range size.MenuCount {
		item, err := GetMenuItem(ctx, s, base+i)
		if err != nil {
			return fmt.Errorf("rollcall: menu item %d of %d: %w", i+1, size.MenuCount, err)
		}
		if err := fn(item); err != nil {
			return err
		}
	}
	return nil
}

// GetMenuItem fetches one menu line by absolute index.
func GetMenuItem(ctx context.Context, s *Session, index uint32) (codec.MenuItem, error) {
	reply, err := s.Do(ctx, codec.MsgGetMenuItem, codec.MenuReq{MenuIndex: index}.AppendTo(nil))
	if err != nil {
		return codec.MenuItem{}, err
	}
	if reply.Type != codec.MsgRetMenuItem {
		return codec.MenuItem{}, protocolError("menu item", reply)
	}
	return codec.DecodeMenuItem(reply.Payload)
}
