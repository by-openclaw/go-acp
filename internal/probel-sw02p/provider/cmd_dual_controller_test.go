package probelsw02p

import (
	"testing"

	"acp/internal/probel-sw02p/codec"
)

// TestDualControllerStatusRequestRepliesMasterHealthy locks in the
// rx 050 / tx 051 contract (§3.2.45 / §3.2.46). The plugin is
// single-controller by construction, so the reply is always Master
// active + Idle OK.
func TestDualControllerStatusRequestRepliesMasterHealthy(t *testing.T) {
	srv := newTestServer(t)

	in := codec.EncodeDualControllerStatusRequest(codec.DualControllerStatusRequestParams{})
	res, err := srv.dispatch(in)
	if err != nil {
		t.Fatalf("dispatch rx 050: %v", err)
	}
	if res.reply == nil || res.reply.ID != codec.TxDualControllerStatusResponse {
		t.Fatalf("reply missing / wrong ID: %+v", res.reply)
	}
	p, err := codec.DecodeDualControllerStatusResponse(*res.reply)
	if err != nil {
		t.Fatalf("decode tx 051: %v", err)
	}
	if p.Active != codec.ActiveControllerMaster {
		t.Errorf("Active = %#x; want Master (0x00)", p.Active)
	}
	if p.IdleStatus != codec.IdleControllerOK {
		t.Errorf("IdleStatus = %#x; want OK (0x00)", p.IdleStatus)
	}
}
