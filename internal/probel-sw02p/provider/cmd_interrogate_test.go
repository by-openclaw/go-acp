package probelsw02p

import (
	"testing"

	"acp/internal/probel-sw02p/codec"
)

// TestInterrogateRepliesWithTally locks in the rx 01 / tx 03 contract
// from SW-P-02 §3.2.3 + §3.2.5:
//   - handler decodes Destination from the Multiplier + MESSAGE byte
//   - queries the tree on (matrix=0, level=0)
//   - replies with tx 03 TALLY echoing dst + reporting the routed src
//   - when no route is recorded, Source encodes §3.2.5 sentinel 1023
func TestInterrogateRepliesWithTally(t *testing.T) {
	srv := newTestServer(t)

	// Seed a route: dst=2 → src=3 on (matrix=0, level=0).
	if err := srv.tree.applyConnect(0, 0, 2, 3); err != nil {
		t.Fatalf("seed applyConnect: %v", err)
	}

	cases := []struct {
		name    string
		dst     uint16
		wantSrc uint16
	}{
		{"routed dst", 2, 3},
		{"unrouted dst returns §3.2.5 sentinel", 1, codec.DestOutOfRangeSource},
		{"unknown dst returns §3.2.5 sentinel", 999, codec.DestOutOfRangeSource},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := codec.EncodeInterrogate(codec.InterrogateParams{Destination: tc.dst})
			res, err := srv.dispatch(in)
			if err != nil {
				t.Fatalf("dispatch rx 01: %v", err)
			}
			if res.reply == nil {
				t.Fatal("rx 01 produced no reply; want tx 03 TALLY")
			}
			if res.reply.ID != codec.TxTally {
				t.Fatalf("reply ID = %#x; want TxTally (%#x)", res.reply.ID, codec.TxTally)
			}
			tally, err := codec.DecodeTally(*res.reply)
			if err != nil {
				t.Fatalf("decode tx 03: %v", err)
			}
			if tally.Destination != tc.dst || tally.Source != tc.wantSrc {
				t.Errorf("tally = (dst=%d src=%d); want (dst=%d src=%d)",
					tally.Destination, tally.Source, tc.dst, tc.wantSrc)
			}
		})
	}
}

// TestInterrogateHandlerDecodeError confirms a short-payload rx 01
// flows back to the dispatcher as a decode error — the session loop
// notes the compliance event and does not write a reply.
func TestInterrogateHandlerDecodeError(t *testing.T) {
	srv := newTestServer(t)
	bad := codec.Frame{ID: codec.RxInterrogate, Payload: []byte{0x00}} // 1 byte
	res, err := srv.dispatch(bad)
	if err == nil {
		t.Fatal("want ErrShortPayload; got nil")
	}
	if res.reply != nil {
		t.Errorf("got reply on decode error; want none")
	}
}
