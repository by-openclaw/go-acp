package probelsw08p

import (
	"io"
	"log/slog"
	"testing"

	"dhs/internal/export/canonical"
	"dhs/internal/probel-sw08p/codec"
)

// labelledServer builds a provider whose single level carries explicit
// source + target labels and a wide (40×40) matrix so the name handlers
// exercise both the declared-label branch and the count > max cap.
func labelledServer(t *testing.T) *server {
	t.Helper()
	exp := &canonical.Export{
		Root: &canonical.Node{
			Header: canonical.Header{
				Number: 1, Identifier: "router", OID: "1",
				Children: []canonical.Element{
					&canonical.Matrix{
						Header: canonical.Header{Number: 1, Identifier: "matrix-0", OID: "1.1"},
						Type:   canonical.MatrixOneToN, Mode: canonical.ModeLinear,
						TargetCount: 40, SourceCount: 40,
						Labels: []canonical.MatrixLabel{{BasePath: "router.matrix-0.video", Description: strPtr("Video")}},
						SourceLabels: map[string]map[string]string{
							"Video": {"0": "CAM01", "3": "CAM04"},
						},
						TargetLabels: map[string]map[string]string{
							"Video": {"0": "MON01", "3": "MON04"},
						},
					},
				},
			},
		},
	}
	return newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), exp)
}

// TestCrosspointConnectReject: a connect to an out-of-range dst is
// rejected by the tree; the handler returns the error and emits no
// reply / tally.
func TestCrosspointConnectReject(t *testing.T) {
	srv := newComplianceServer(t) // 4×4
	res, err := srv.handle(codec.EncodeCrosspointConnect(codec.CrosspointConnectParams{
		MatrixID: 0, LevelID: 0, DestinationID: 99, SourceID: 1,
	}))
	if err == nil {
		t.Fatal("connect to dst=99 on 4×4: want error, got nil")
	}
	if res.reply != nil || len(res.tallies) != 0 {
		t.Errorf("rejected connect must not reply/fan-out; got %+v", res)
	}
}

// TestMaintenanceAllFunctions drives every non-clear branch of the
// rx 007 switch (hard reset, soft reset, database transfer, unknown
// function). None reply; all are logged and absorbed.
func TestMaintenanceAllFunctions(t *testing.T) {
	srv := newComplianceServer(t)
	for _, fn := range []codec.MaintenanceFunction{
		codec.MaintHardReset,
		codec.MaintSoftReset,
		codec.MaintDatabaseTransfer,
		codec.MaintenanceFunction(0x7F), // unknown function → default arm
	} {
		res, err := srv.handle(codec.EncodeMaintenance(codec.MaintenanceParams{
			Function: fn, MatrixID: 0, LevelID: 0,
		}))
		if err != nil {
			t.Fatalf("maintenance fn=%#x: %v", fn, err)
		}
		if res.reply != nil {
			t.Errorf("maintenance fn=%#x must not reply", fn)
		}
	}
}

// TestProtectDisconnectReject: disconnecting a protect owned by another
// device is rejected; no reply / tally.
func TestProtectDisconnectReject(t *testing.T) {
	srv := newComplianceServer(t)
	if err := srv.tree.applyProtectConnect(0, 0, 1, 42, uint8(codec.ProtectProbel), false); err != nil {
		t.Fatalf("seed protect: %v", err)
	}
	res, err := srv.handle(codec.EncodeProtectDisconnect(codec.ProtectDisconnectParams{
		MatrixID: 0, LevelID: 0, DestinationID: 1, DeviceID: 7, // not the owner
	}))
	if err == nil {
		t.Fatal("disconnect by non-owner: want error, got nil")
	}
	if res.reply != nil || len(res.tallies) != 0 {
		t.Errorf("rejected disconnect must not reply/fan-out; got %+v", res)
	}
}

// TestProtectTallyDumpUnknownMatrix: an unknown (matrix, level) yields a
// single empty tx 020 rather than an error or a hang.
func TestProtectTallyDumpUnknownMatrix(t *testing.T) {
	srv := newComplianceServer(t)
	res, err := srv.handle(codec.EncodeProtectTallyDumpRequest(codec.ProtectTallyDumpRequestParams{
		MatrixID: 9, LevelID: 0, DestinationID: 0,
	}))
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if res.reply == nil || res.reply.ID != codec.TxProtectTallyDump {
		t.Fatalf("reply = %+v; want empty tx 020", res.reply)
	}
	dec, err := codec.DecodeProtectTallyDump(*res.reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(dec.Items) != 0 {
		t.Errorf("items = %d; want 0 for unknown matrix", len(dec.Items))
	}
}

// TestProtectTallyDumpStartBeyondTarget: a FirstDestinationID at or past
// targetCount yields the empty reply via the second guard.
func TestProtectTallyDumpStartBeyondTarget(t *testing.T) {
	srv := newComplianceServer(t) // 4×4
	res, err := srv.handle(codec.EncodeProtectTallyDumpRequest(codec.ProtectTallyDumpRequestParams{
		MatrixID: 0, LevelID: 0, DestinationID: 4, // == targetCount
	}))
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	dec, err := codec.DecodeProtectTallyDump(*res.reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(dec.Items) != 0 {
		t.Errorf("items = %d; want 0 when start >= targetCount", len(dec.Items))
	}
}

// TestCrosspointTallyDumpUnknownMatrix exercises the unknown-(matrix,
// level) single-frame empty byte-form reply.
func TestCrosspointTallyDumpUnknownMatrix(t *testing.T) {
	srv := newComplianceServer(t)
	res, err := srv.handle(codec.EncodeCrosspointTallyDumpRequest(codec.CrosspointTallyDumpRequestParams{
		MatrixID: 9, LevelID: 0,
	}))
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if res.reply == nil || res.reply.ID != codec.TxCrosspointTallyDumpByte {
		t.Fatalf("reply = %+v; want empty byte-form dump", res.reply)
	}
}

// TestCrosspointTallyDumpWordForm: a matrix wider than 255 selects the
// word-form streaming path. Drive the emitter and assert it covers every
// destination across multiple frames, then assert an emit error
// propagates back out of the stream callback.
func TestCrosspointTallyDumpWordForm(t *testing.T) {
	exp := &canonical.Export{
		Root: &canonical.Node{
			Header: canonical.Header{
				Number: 1, Identifier: "router", OID: "1",
				Children: []canonical.Element{
					&canonical.Matrix{
						Header: canonical.Header{Number: 1, Identifier: "matrix-0", OID: "1.1"},
						Type:   canonical.MatrixOneToN, Mode: canonical.ModeLinear,
						TargetCount: 300, SourceCount: 300,
						Labels: []canonical.MatrixLabel{{BasePath: "router.matrix-0.video"}},
					},
				},
			},
		},
	}
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), exp)
	if err := srv.tree.applyConnect(0, 0, 260, 42); err != nil {
		t.Fatalf("seed route: %v", err)
	}
	res, err := srv.handle(codec.EncodeCrosspointTallyDumpRequest(codec.CrosspointTallyDumpRequestParams{
		MatrixID: 0, LevelID: 0,
	}))
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if res.streamToSender == nil {
		t.Fatal("word-form dump must stream to sender")
	}

	covered := map[int]bool{}
	var sawRoute bool
	frames := 0
	emitErr := res.streamToSender(func(f codec.Frame) error {
		frames++
		if f.ID != codec.TxCrosspointTallyDumpWord {
			t.Errorf("frame id = %#x; want word-form", f.ID)
		}
		dec, derr := codec.DecodeCrosspointTallyDumpWord(f)
		if derr != nil {
			t.Fatalf("decode word frame: %v", derr)
		}
		for i, src := range dec.SourceIDs {
			covered[int(dec.FirstDestinationID)+i] = true
			if int(dec.FirstDestinationID)+i == 260 && src == 42 {
				sawRoute = true
			}
		}
		return nil
	})
	if emitErr != nil {
		t.Fatalf("stream: %v", emitErr)
	}
	if frames < 2 {
		t.Errorf("word-form 300-dst dump produced %d frames; want >1", frames)
	}
	if len(covered) != 300 {
		t.Errorf("covered %d dsts; want 300", len(covered))
	}
	if !sawRoute {
		t.Error("seeded route dst=260 src=42 not present in word dump")
	}

	// Emit error must short-circuit the stream and surface to the caller.
	wantErr := io.ErrClosedPipe
	gotErr := res.streamToSender(func(codec.Frame) error { return wantErr })
	if gotErr != wantErr {
		t.Errorf("stream emit error = %v; want %v", gotErr, wantErr)
	}
}

// TestCrosspointTallyDumpByteEmitError exercises the byte-form emit
// error short-circuit (line cmd_rx021:105).
func TestCrosspointTallyDumpByteEmitError(t *testing.T) {
	srv := newComplianceServer(t) // 4×4 byte form
	res, err := srv.handle(codec.EncodeCrosspointTallyDumpRequest(codec.CrosspointTallyDumpRequestParams{
		MatrixID: 0, LevelID: 0,
	}))
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if res.streamToSender == nil {
		t.Fatal("byte-form dump must stream to sender")
	}
	wantErr := io.ErrClosedPipe
	if got := res.streamToSender(func(codec.Frame) error { return wantErr }); got != wantErr {
		t.Errorf("byte stream emit error = %v; want %v", got, wantErr)
	}
}

// TestAllSourceNamesDeclaredLabelsAndCap drives the rx 100 handler on a
// labelled 40-source matrix with NameLen8 (max 16). Asserts the count is
// capped at 16, declared labels surface where present, and positional
// defaults fill the gaps.
func TestAllSourceNamesDeclaredLabelsAndCap(t *testing.T) {
	srv := labelledServer(t)
	res, err := srv.handle(codec.EncodeAllSourceNamesRequest(codec.AllSourceNamesRequestParams{
		MatrixID: 0, LevelID: 0, NameLength: codec.NameLen8,
	}))
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	dec, err := codec.DecodeSourceNamesResponse(*res.reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(dec.Names) != 16 {
		t.Fatalf("names = %d; want 16 (capped by NameLen8 max)", len(dec.Names))
	}
	if dec.Names[0] != "CAM01" {
		t.Errorf("Names[0] = %q; want declared CAM01", dec.Names[0])
	}
	if dec.Names[3] != "CAM04" {
		t.Errorf("Names[3] = %q; want declared CAM04", dec.Names[3])
	}
	if dec.Names[1] != "SRC 0002" {
		t.Errorf("Names[1] = %q; want positional default SRC 0002", dec.Names[1])
	}
}

// TestAllSourceNamesUnknownMatrix: unknown (matrix, level) → empty tx 106.
func TestAllSourceNamesUnknownMatrix(t *testing.T) {
	srv := newComplianceServer(t)
	res, err := srv.handle(codec.EncodeAllSourceNamesRequest(codec.AllSourceNamesRequestParams{
		MatrixID: 9, LevelID: 0, NameLength: codec.NameLen8,
	}))
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	dec, err := codec.DecodeSourceNamesResponse(*res.reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(dec.Names) != 0 {
		t.Errorf("names = %d; want 0 for unknown matrix", len(dec.Names))
	}
}

// TestSingleSourceNameUnknownSource: a source id past sourceCount falls
// back to a positional name rather than erroring.
func TestSingleSourceNameUnknownSource(t *testing.T) {
	srv := newComplianceServer(t) // 4×4
	res, err := srv.handle(codec.EncodeSingleSourceNameRequest(codec.SingleSourceNameRequestParams{
		MatrixID: 0, LevelID: 0, NameLength: codec.NameLen8, SourceID: 99,
	}))
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	dec, err := codec.DecodeSourceNamesResponse(*res.reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(dec.Names) != 1 || dec.Names[0] != "SRC 0100" {
		t.Errorf("Names = %v; want [SRC 0100]", dec.Names)
	}
}

// TestAllDestAssocNamesUnknownMatrix + declared labels.
func TestAllDestAssocNames(t *testing.T) {
	t.Run("unknown_matrix", func(t *testing.T) {
		srv := newComplianceServer(t)
		res, err := srv.handle(codec.EncodeAllDestAssocNamesRequest(codec.AllDestAssocNamesRequestParams{
			MatrixID: 9, NameLength: codec.NameLen8,
		}))
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		dec, err := codec.DecodeDestAssocNamesResponse(*res.reply)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(dec.Names) != 0 {
			t.Errorf("names = %d; want 0", len(dec.Names))
		}
	})
	t.Run("declared_and_cap", func(t *testing.T) {
		srv := labelledServer(t)
		res, err := srv.handle(codec.EncodeAllDestAssocNamesRequest(codec.AllDestAssocNamesRequestParams{
			MatrixID: 0, NameLength: codec.NameLen8,
		}))
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		dec, err := codec.DecodeDestAssocNamesResponse(*res.reply)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(dec.Names) != 16 {
			t.Fatalf("names = %d; want 16 (capped)", len(dec.Names))
		}
		if dec.Names[0] != "MON01" || dec.Names[3] != "MON04" {
			t.Errorf("declared labels missing: [0]=%q [3]=%q", dec.Names[0], dec.Names[3])
		}
	})
}

// TestSingleDestAssocNameUnknown: dest id past targetCount → positional.
func TestSingleDestAssocNameUnknown(t *testing.T) {
	srv := newComplianceServer(t)
	res, err := srv.handle(codec.EncodeSingleDestAssocNameRequest(codec.SingleDestAssocNameRequestParams{
		MatrixID: 0, NameLength: codec.NameLen8, DestAssociationID: 50,
	}))
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	dec, err := codec.DecodeDestAssocNamesResponse(*res.reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(dec.Names) != 1 || dec.Names[0] != "DST 0051" {
		t.Errorf("Names = %v; want [DST 0051]", dec.Names)
	}
}

// TestAllSourceAssocNames covers rx 114 (previously 0%): unknown matrix
// empty reply, plus the populated + capped path.
func TestAllSourceAssocNames(t *testing.T) {
	t.Run("unknown_matrix", func(t *testing.T) {
		srv := newComplianceServer(t)
		res, err := srv.handle(codec.EncodeAllSourceAssocNamesRequest(codec.AllSourceAssocNamesRequestParams{
			MatrixID: 9, NameLength: codec.NameLen8,
		}))
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		dec, err := codec.DecodeSourceAssocNamesResponse(*res.reply)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(dec.Names) != 0 {
			t.Errorf("names = %d; want 0", len(dec.Names))
		}
	})
	t.Run("declared_and_cap", func(t *testing.T) {
		srv := labelledServer(t)
		res, err := srv.handle(codec.EncodeAllSourceAssocNamesRequest(codec.AllSourceAssocNamesRequestParams{
			MatrixID: 0, NameLength: codec.NameLen8,
		}))
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		dec, err := codec.DecodeSourceAssocNamesResponse(*res.reply)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(dec.Names) != 16 {
			t.Fatalf("names = %d; want 16 (capped)", len(dec.Names))
		}
		if dec.Names[0] != "CAM01" || dec.Names[3] != "CAM04" {
			t.Errorf("declared labels missing: [0]=%q [3]=%q", dec.Names[0], dec.Names[3])
		}
	})
}

// TestSingleSourceAssocName covers rx 115 (previously 0%): both the
// in-range declared-label path and the unknown-source positional path.
func TestSingleSourceAssocName(t *testing.T) {
	t.Run("declared", func(t *testing.T) {
		srv := labelledServer(t)
		res, err := srv.handle(codec.EncodeSingleSourceAssocNameRequest(codec.SingleSourceAssocNameRequestParams{
			MatrixID: 0, NameLength: codec.NameLen8, SourceAssociationID: 3,
		}))
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		dec, err := codec.DecodeSourceAssocNamesResponse(*res.reply)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(dec.Names) != 1 || dec.Names[0] != "CAM04" {
			t.Errorf("Names = %v; want [CAM04]", dec.Names)
		}
	})
	t.Run("unknown_source", func(t *testing.T) {
		srv := newComplianceServer(t)
		res, err := srv.handle(codec.EncodeSingleSourceAssocNameRequest(codec.SingleSourceAssocNameRequestParams{
			MatrixID: 0, NameLength: codec.NameLen8, SourceAssociationID: 99,
		}))
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		dec, err := codec.DecodeSourceAssocNamesResponse(*res.reply)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(dec.Names) != 1 || dec.Names[0] != "SRC 0100" {
			t.Errorf("Names = %v; want [SRC 0100]", dec.Names)
		}
	})
}

// TestTieLineInterrogateSkipBranches covers the two continue arms in the
// matrix walk: a dst at/over targetCount, and a dst with no routed
// source. Both must be skipped so the tally lists only real routes.
func TestTieLineInterrogateSkipBranches(t *testing.T) {
	srv := newComplianceServer(t) // matrix 0 level 0, 4×4
	// Add a second level on matrix 0 with a smaller target count so the
	// "dst >= targetCount" continue fires for that level.
	srv.tree.matrices[matrixKey{matrix: 0, level: 1}] = &matrixState{
		targetCount: 2, sourceCount: 4,
		sources:  map[uint16]uint16{},
		protects: map[uint16]protectRecord{},
	}
	// Route only level 0 dst=3. Level 1 has targetCount=2 so dst=3 is
	// out of range there (first continue); level 0 dst=3 is routed.
	if err := srv.tree.applyConnect(0, 0, 3, 2); err != nil {
		t.Fatalf("seed: %v", err)
	}
	res, err := srv.handle(codec.EncodeTieLineInterrogate(codec.TieLineInterrogateParams{
		MatrixID: 0, DestAssociationID: 3,
	}))
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	dec, err := codec.DecodeTieLineTally(*res.reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(dec.Sources) != 1 {
		t.Fatalf("sources = %+v; want exactly 1 (level 0 only)", dec.Sources)
	}
	if dec.Sources[0].LevelID != 0 || dec.Sources[0].SourceID != 2 {
		t.Errorf("source = %+v; want level=0 src=2", dec.Sources[0])
	}

	// Also cover the "routed=false" continue: interrogate an in-range but
	// unrouted dst.
	res2, err := srv.handle(codec.EncodeTieLineInterrogate(codec.TieLineInterrogateParams{
		MatrixID: 0, DestAssociationID: 1,
	}))
	if err != nil {
		t.Fatalf("handle unrouted: %v", err)
	}
	dec2, _ := codec.DecodeTieLineTally(*res2.reply)
	if len(dec2.Sources) != 0 {
		t.Errorf("unrouted dst sources = %+v; want empty", dec2.Sources)
	}
}

// TestUpdateNameSourceAssoc covers the rx 117 UpdateNameSourceAssoc arm
// (writes source labels on level 0).
func TestUpdateNameSourceAssoc(t *testing.T) {
	srv := newComplianceServer(t)
	res, err := srv.handle(codec.EncodeUpdateNameRequest(codec.UpdateNameRequestParams{
		NameType: codec.UpdateNameSourceAssoc, NameLength: codec.NameLen4,
		MatrixID: 0, LevelID: 0, FirstID: 0,
		Names: []string{"SA01"},
	}))
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if res.reply != nil {
		t.Errorf("rx 117 must not reply; got %+v", res.reply)
	}
	st, _ := srv.tree.lookup(0, 0)
	if st.sourceLabels[0] != "SA01" {
		t.Errorf("sourceLabels[0] = %q; want SA01", st.sourceLabels[0])
	}
}

// TestSalvoConnectOnGoEcho covers the rx 120 happy path returning a
// tx 122 ack mirroring the request (the success arm after the decode
// guard).
func TestSalvoConnectOnGoEcho(t *testing.T) {
	srv := newComplianceServer(t)
	res, err := srv.handle(codec.EncodeSalvoConnectOnGo(codec.SalvoConnectOnGoParams{
		MatrixID: 0, LevelID: 0, DestinationID: 1, SourceID: 2, SalvoID: 3,
	}))
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if res.reply == nil || res.reply.ID != codec.TxSalvoConnectOnGoAck {
		t.Fatalf("reply = %+v; want tx 122 ack", res.reply)
	}
	ack, err := codec.DecodeSalvoConnectOnGoAck(*res.reply)
	if err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if ack.DestinationID != 1 || ack.SourceID != 2 || ack.SalvoID != 3 {
		t.Errorf("ack = %+v; want dst=1 src=2 sid=3", ack)
	}
}

// TestMasterProtectConnectReject: rx 029 with an out-of-range dst is
// rejected even though override=true (the dst-overflow arm of
// applyProtectConnect); no reply / tally.
func TestMasterProtectConnectReject(t *testing.T) {
	srv := newComplianceServer(t) // 4×4
	res, err := srv.handle(codec.EncodeMasterProtectConnect(codec.MasterProtectConnectParams{
		MatrixID: 0, LevelID: 0, DestinationID: 99, DeviceID: 1,
	}))
	if err == nil {
		t.Fatal("master-protect to dst=99: want error, got nil")
	}
	if res.reply != nil || len(res.tallies) != 0 {
		t.Errorf("rejected master-protect must not reply/fan-out; got %+v", res)
	}
}

// TestTieLineInterrogateOtherMatrixSkipped: a second matrix registered in
// the tree must be skipped by the rx 112 walk (the key.matrix !=
// p.MatrixID continue arm), so only the requested matrix's routes appear.
func TestTieLineInterrogateOtherMatrixSkipped(t *testing.T) {
	srv := newComplianceServer(t) // matrix 0 level 0
	// Register a routed crosspoint on a DIFFERENT matrix (1).
	srv.tree.matrices[matrixKey{matrix: 1, level: 0}] = &matrixState{
		targetCount: 4, sourceCount: 4,
		sources:  map[uint16]uint16{1: 3},
		protects: map[uint16]protectRecord{},
	}
	// Route matrix 0 dst=1 too.
	if err := srv.tree.applyConnect(0, 0, 1, 2); err != nil {
		t.Fatalf("seed: %v", err)
	}
	res, err := srv.handle(codec.EncodeTieLineInterrogate(codec.TieLineInterrogateParams{
		MatrixID: 0, DestAssociationID: 1,
	}))
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	dec, err := codec.DecodeTieLineTally(*res.reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(dec.Sources) != 1 {
		t.Fatalf("sources = %+v; want only matrix 0 route", dec.Sources)
	}
	if dec.Sources[0].MatrixID != 0 || dec.Sources[0].SourceID != 2 {
		t.Errorf("source = %+v; want matrix=0 src=2 (matrix 1 must be skipped)", dec.Sources[0])
	}
}

// TestSalvoGoStreamEmitError drives the streamToSender error arm of the
// rx 121 Set path (cmd_rx121:72) by returning an error from emit.
func TestSalvoGoStreamEmitError(t *testing.T) {
	srv := newComplianceServer(t)
	if _, err := srv.handle(codec.EncodeSalvoConnectOnGo(codec.SalvoConnectOnGoParams{
		MatrixID: 0, LevelID: 0, DestinationID: 0, SourceID: 1, SalvoID: 8,
	})); err != nil {
		t.Fatalf("build: %v", err)
	}
	res, err := srv.handle(codec.EncodeSalvoGo(codec.SalvoGoParams{Op: codec.SalvoOpSet, SalvoID: 8}))
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if res.streamToSender == nil {
		t.Fatal("set with applied slots must stream cmd 04 to sender")
	}
	wantErr := io.ErrClosedPipe
	if got := res.streamToSender(func(codec.Frame) error { return wantErr }); got != wantErr {
		t.Errorf("stream emit error = %v; want %v", got, wantErr)
	}
}
