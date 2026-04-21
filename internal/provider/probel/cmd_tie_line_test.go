package probel

import (
	"testing"

	iprobel "acp/internal/probel"
)

// TestHandleTieLineInterrogateEmpty: unrouted dst yields NumSrcs=0.
func TestHandleTieLineInterrogateEmpty(t *testing.T) {
	srv := newComplianceServer(t)
	req := iprobel.EncodeTieLineInterrogate(iprobel.TieLineInterrogateParams{
		MatrixID: 0, DestAssociationID: 0,
	})
	res, err := srv.handle(req)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if res.reply == nil || res.reply.ID != iprobel.TxCrosspointTieLineTally {
		t.Fatalf("reply = %+v", res.reply)
	}
	decoded, err := iprobel.DecodeTieLineTally(*res.reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Sources) != 0 {
		t.Errorf("sources = %+v; want empty", decoded.Sources)
	}
}

// TestHandleTieLineInterrogateRouted: after a connect, the tally lists
// the routed source.
func TestHandleTieLineInterrogateRouted(t *testing.T) {
	srv := newComplianceServer(t)
	if err := srv.tree.applyConnect(0, 0, 1, 2); err != nil {
		t.Fatalf("applyConnect: %v", err)
	}

	req := iprobel.EncodeTieLineInterrogate(iprobel.TieLineInterrogateParams{
		MatrixID: 0, DestAssociationID: 1,
	})
	res, err := srv.handle(req)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	decoded, err := iprobel.DecodeTieLineTally(*res.reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Sources) != 1 {
		t.Fatalf("sources = %+v; want 1", decoded.Sources)
	}
	s := decoded.Sources[0]
	if s.MatrixID != 0 || s.LevelID != 0 || s.SourceID != 2 {
		t.Errorf("got %+v; want {0 0 2}", s)
	}
}
