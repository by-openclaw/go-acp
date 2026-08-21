package emberplus

import (
	"context"
	"encoding/hex"
	"net"
	"testing"
	"time"

	"dhs/internal/emberplus/codec/s101"
)

// Captured live from a Lawo VSM Studio gadgetserver Ember+ provider
// (10.6.239.117:9000, 2026-08-20, issue #728): the reply to
// GetDirectory(root) is a multi-packet S101 message: a first packet
// (flags 0x80) carrying the whole indefinite-length BER document
// (Root > RootElementCollection > Node#1 "Device"), and a payload-less
// last packet (flags 0x60). Frames are verbatim wire bytes incl. CRC.
var vsmRootFirst, _ = hex.DecodeString(
	"fe000e0001800102280260806b80a0806380a003020101a1803180a0080c06446576696365a2030101fddfa3030101fddf000000000000000000000000bed3ff")
var vsmRootLast, _ = hex.DecodeString(
	"fe000e000160010228029043ff")

// TestWalk_VSMGadgetserverDialect replays the captured VSM exchange
// against the real Connect+Walk pipeline: the fake provider answers
// every EmBER frame with the captured multi-packet root reply. The
// walk must at minimum store the delivered "Device" node (the live
// failure was 0 objects, #728).
func TestWalk_VSMGadgetserverDialect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		r := s101.NewReader(conn)
		for {
			f, err := r.ReadFrame()
			if err != nil {
				return
			}
			if f.IsKeepAlive() {
				if f.Command == s101.CmdKeepAliveReq {
					w := s101.NewWriter(conn)
					_ = w.WriteFrame(s101.NewKeepAliveResponse())
				}
				continue
			}
			if f.IsEmBER() {
				// An empty SINGLE-packet EmBER frame precedes the real
				// reply: the read loop must skip it (the #729 guard —
				// empty-skip applies ONLY to FlagSingle) and still
				// decode what follows. Deterministic pin for the skip
				// branch: live sessions only hit it when a keepalive-ish
				// empty frame happens to race in (coverage floor red,
				// PR #742 run 32536961316).
				_, _ = conn.Write(s101.Encode(&s101.Frame{
					MsgType: s101.MsgEmBER, Command: s101.CmdEmBER, Flags: s101.FlagSingle,
				}))
				// Reply exactly like VSM: first + empty last packet.
				_, _ = conn.Write(vsmRootFirst)
				_, _ = conn.Write(vsmRootLast)
			}
		}
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port int
	for i := 0; i < len(portStr); i++ {
		port = port*10 + int(portStr[i]-'0')
	}

	p := (&Factory{}).New(discardLogger()).(*Plugin)
	// Short settle timings so the test finishes fast.
	p.walkSettleInitial = 2 * time.Second
	p.walkSettleInterval = 500 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := p.Connect(ctx, host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	objs, err := p.Walk(ctx, 0)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(objs) == 0 {
		t.Fatalf("walk stored 0 objects - the VSM 'Device' node was dropped (#728)")
	}
	var found bool
	for _, o := range objs {
		if o.Label == "Device" {
			found = true
		}
	}
	if !found {
		t.Fatalf("'Device' node not in snapshot: %+v", objs)
	}
}

