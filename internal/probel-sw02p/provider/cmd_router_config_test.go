package probelsw02p

import (
	"io"
	"log/slog"
	"testing"

	"acp/internal/export/canonical"
	"acp/internal/probel-sw02p/codec"
)

// TestRouterConfigRequestDerivedFromTree drives rx 075 through the
// dispatcher against a 3-level canonical matrix and verifies tx 076
// reports the level bitmap + per-level counts derived from the tree.
func TestRouterConfigRequestDerivedFromTree(t *testing.T) {
	lvl := func(s string) canonical.MatrixLabel { return canonical.MatrixLabel{BasePath: "router.m0." + s} }
	exp := &canonical.Export{
		Root: &canonical.Node{
			Header: canonical.Header{
				Number: 1, Identifier: "router", OID: "1",
				Children: []canonical.Element{
					&canonical.Matrix{
						Header: canonical.Header{
							Number: 1, Identifier: "m0", OID: "1.1",
						},
						Type:        canonical.MatrixOneToN,
						Mode:        canonical.ModeLinear,
						TargetCount: 128,
						SourceCount: 128,
						Labels:      []canonical.MatrixLabel{lvl("video"), lvl("audio"), lvl("aux")},
					},
				},
			},
		},
	}
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), exp)

	res, err := srv.dispatch(codec.EncodeRouterConfigRequest(codec.RouterConfigRequestParams{}))
	if err != nil {
		t.Fatalf("dispatch rx 075: %v", err)
	}
	if res.reply == nil || res.reply.ID != codec.TxRouterConfigResponse1 {
		t.Fatalf("reply missing / wrong ID: %+v", res.reply)
	}
	r, err := codec.DecodeRouterConfigResponse1(*res.reply)
	if err != nil {
		t.Fatalf("decode tx 076: %v", err)
	}
	// 3 levels → bitmap bits 0, 1, 2.
	wantMap := uint32((1 << 0) | (1 << 1) | (1 << 2))
	if r.LevelMap != wantMap {
		t.Errorf("LevelMap = %#x; want %#x", r.LevelMap, wantMap)
	}
	if len(r.Levels) != 3 {
		t.Fatalf("Levels len = %d; want 3", len(r.Levels))
	}
	for i, lvlEntry := range r.Levels {
		if lvlEntry.NumDestinations != 128 || lvlEntry.NumSources != 128 {
			t.Errorf("level %d = %+v; want 128/128", i, lvlEntry)
		}
	}
}

// TestRouterConfigRequestEmptyTree verifies the bare-tree case — no
// matrices declared means bitmap=0 and no per-level entries.
func TestRouterConfigRequestEmptyTree(t *testing.T) {
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	res, err := srv.dispatch(codec.EncodeRouterConfigRequest(codec.RouterConfigRequestParams{}))
	if err != nil {
		t.Fatalf("dispatch rx 075: %v", err)
	}
	if res.reply == nil {
		t.Fatal("reply is nil; want tx 076")
	}
	r, err := codec.DecodeRouterConfigResponse1(*res.reply)
	if err != nil {
		t.Fatalf("decode tx 076: %v", err)
	}
	if r.LevelMap != 0 {
		t.Errorf("LevelMap = %#x; want 0", r.LevelMap)
	}
	if len(r.Levels) != 0 {
		t.Errorf("Levels len = %d; want 0", len(r.Levels))
	}
}
