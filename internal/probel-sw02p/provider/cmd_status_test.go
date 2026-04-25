package probelsw02p

import (
	"testing"

	"acp/internal/probel-sw02p/codec"
)

// TestStatusRequestRepliesHealthyResponse2 locks in the rx 07 / tx 09
// contract from §3.2.9 + §3.2.11: the provider replies with tx 09
// STATUS RESPONSE - 2 carrying all-zero fault flags (active, no bus
// fault, no overheat) regardless of whether LH or RH controller was
// queried.
func TestStatusRequestRepliesHealthyResponse2(t *testing.T) {
	srv := newTestServer(t)

	for _, ctl := range []codec.Controller{codec.ControllerLH, codec.ControllerRH} {
		in := codec.EncodeStatusRequest(codec.StatusRequestParams{Controller: ctl})
		res, err := srv.dispatch(in)
		if err != nil {
			t.Fatalf("dispatch rx 07 ctl=%#x: %v", ctl, err)
		}
		if res.reply == nil || res.reply.ID != codec.TxStatusResponse2 {
			t.Fatalf("reply missing / wrong ID for ctl=%#x: %+v", ctl, res.reply)
		}
		sp, err := codec.DecodeStatusResponse2(*res.reply)
		if err != nil {
			t.Fatalf("decode tx 09: %v", err)
		}
		if sp.Idle || sp.BusFault || sp.Overheat {
			t.Errorf("ctl=%#x status = %+v; want healthy zero flags", ctl, sp)
		}
	}
}

// TestStatusRequestHandlerDecodeError confirms a short-payload rx 07
// flows back as a decode error.
func TestStatusRequestHandlerDecodeError(t *testing.T) {
	srv := newTestServer(t)
	bad := codec.Frame{ID: codec.RxStatusRequest, Payload: nil}
	res, err := srv.dispatch(bad)
	if err == nil {
		t.Fatal("want ErrShortPayload; got nil")
	}
	if res.reply != nil {
		t.Errorf("got reply on decode error; want none")
	}
}
