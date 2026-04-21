package probel

import (
	"testing"

	"acp/internal/protocol/probel/codec"
)

// TestHandleUpdateNameSource: rx 117 with NameType=Source overwrites
// the source labels on the specified (matrix, level).
func TestHandleUpdateNameSource(t *testing.T) {
	srv := newComplianceServer(t)
	frame := codec.EncodeUpdateNameRequest(codec.UpdateNameRequestParams{
		NameType: codec.UpdateNameSource, NameLength: codec.NameLen4,
		MatrixID: 0, LevelID: 0, FirstID: 1,
		Names: []string{"CAM2", "CAM3"},
	})
	res, err := srv.handle(frame)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if res.reply != nil || len(res.tallies) != 0 {
		t.Errorf("want empty handlerResult; got %+v", res)
	}
	st, _ := srv.tree.lookup(0, 0)
	if st.sourceLabels[1] != "CAM2" || st.sourceLabels[2] != "CAM3" {
		t.Errorf("sourceLabels = %v; want [_, CAM2, CAM3, _]", st.sourceLabels)
	}
}

// TestHandleUpdateNameDestAssoc: updates target labels on matrix/0.
func TestHandleUpdateNameDestAssoc(t *testing.T) {
	srv := newComplianceServer(t)
	frame := codec.EncodeUpdateNameRequest(codec.UpdateNameRequestParams{
		NameType: codec.UpdateNameDestAssoc, NameLength: codec.NameLen4,
		MatrixID: 0, LevelID: 0, FirstID: 0,
		Names: []string{"OUT1"},
	})
	if _, err := srv.handle(frame); err != nil {
		t.Fatalf("handle: %v", err)
	}
	st, _ := srv.tree.lookup(0, 0)
	if st.targetLabels[0] != "OUT1" {
		t.Errorf("targetLabels[0] = %q; want OUT1", st.targetLabels[0])
	}
}

// TestHandleUpdateNameUMDIgnored: UMD label type is accepted but the
// tree remains unchanged (not modelled).
func TestHandleUpdateNameUMDIgnored(t *testing.T) {
	srv := newComplianceServer(t)
	frame := codec.EncodeUpdateNameRequest(codec.UpdateNameRequestParams{
		NameType: codec.UpdateNameUMDLabel, NameLength: codec.NameLen4,
		MatrixID: 0, LevelID: 0, FirstID: 0,
		Names: []string{"UMD1"},
	})
	if _, err := srv.handle(frame); err != nil {
		t.Fatalf("handle: %v", err)
	}
}
