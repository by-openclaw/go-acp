package probel

import (
	"testing"

	"acp/internal/probel-sw08p/codec"
)

// TestHandleAllSourceNames: loopback dispatch returns a tx 106 with
// labelled + positional-default source names.
func TestHandleAllSourceNames(t *testing.T) {
	srv := newComplianceServer(t)
	req := codec.EncodeAllSourceNamesRequest(codec.AllSourceNamesRequestParams{
		MatrixID: 0, LevelID: 0, NameLength: codec.NameLen8,
	})
	res, err := srv.handle(req)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if res.reply == nil || res.reply.ID != codec.TxSourceNamesResponse {
		t.Fatalf("reply = %+v; want tx 0x6A", res.reply)
	}
	decoded, err := codec.DecodeSourceNamesResponse(*res.reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Names) != 4 { // demo tree: sourceCount=4
		t.Errorf("got %d names; want 4", len(decoded.Names))
	}
	if decoded.NameLength != codec.NameLen8 {
		t.Errorf("NameLength = %#x; want 8", decoded.NameLength)
	}
	// positional defaults since no labels declared
	for i, n := range decoded.Names {
		want := []string{"SRC 0001", "SRC 0002", "SRC 0003", "SRC 0004"}[i]
		if n != want {
			t.Errorf("Names[%d] = %q; want %q", i, n, want)
		}
	}
}

// TestHandleSingleSourceName: returns one name via tx 106 with NumNames=1.
func TestHandleSingleSourceName(t *testing.T) {
	srv := newComplianceServer(t)
	req := codec.EncodeSingleSourceNameRequest(codec.SingleSourceNameRequestParams{
		MatrixID: 0, LevelID: 0, NameLength: codec.NameLen4, SourceID: 2,
	})
	res, err := srv.handle(req)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	decoded, err := codec.DecodeSourceNamesResponse(*res.reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 4-char truncation of "SRC 0003" packs as "SRC " → unpacks with trailing space trimmed to "SRC".
	if len(decoded.Names) != 1 || decoded.Names[0] != "SRC" {
		t.Errorf("Names = %v; want [SRC]", decoded.Names)
	}
	if decoded.FirstSourceID != 2 {
		t.Errorf("FirstSourceID = %d; want 2", decoded.FirstSourceID)
	}
}

// TestHandleAllDestAssocNames: returns tx 107 populated with dst positional names.
func TestHandleAllDestAssocNames(t *testing.T) {
	srv := newComplianceServer(t)
	req := codec.EncodeAllDestAssocNamesRequest(codec.AllDestAssocNamesRequestParams{
		MatrixID: 0, NameLength: codec.NameLen8,
	})
	res, err := srv.handle(req)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if res.reply.ID != codec.TxDestAssocNamesResponse {
		t.Fatalf("reply ID = %#x; want 0x6B", res.reply.ID)
	}
	decoded, err := codec.DecodeDestAssocNamesResponse(*res.reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Names) != 4 {
		t.Errorf("got %d names; want 4", len(decoded.Names))
	}
	for i, n := range decoded.Names {
		want := []string{"DST 0001", "DST 0002", "DST 0003", "DST 0004"}[i]
		if n != want {
			t.Errorf("Names[%d] = %q; want %q", i, n, want)
		}
	}
}

// TestHandleSingleDestAssocName: returns one dest name.
func TestHandleSingleDestAssocName(t *testing.T) {
	srv := newComplianceServer(t)
	req := codec.EncodeSingleDestAssocNameRequest(codec.SingleDestAssocNameRequestParams{
		MatrixID: 0, NameLength: codec.NameLen12, DestAssociationID: 1,
	})
	res, err := srv.handle(req)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	decoded, err := codec.DecodeDestAssocNamesResponse(*res.reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Names) != 1 || decoded.Names[0] != "DST 0002" {
		t.Errorf("Names = %v; want [DST 0002]", decoded.Names)
	}
}
