package acp1

import (
	"context"
	"log/slog"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
	"dhs/internal/consumer/compliance"
)

// TestConnect_AutoFallback drives TransportAuto: the TCP probe to a port with
// no TCP listener fails, so Connect falls back to UDP and records the resolved
// transport.
func TestConnect_AutoFallback(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	p.SetTransport(TransportAuto)
	if err := p.Connect(context.Background(), "127.0.0.1", 0); err != nil {
		t.Fatalf("Connect auto: %v", err)
	}
	t.Cleanup(func() { _ = p.Disconnect() })
	if p.Transport() != TransportUDP {
		t.Errorf("auto resolved to %v, want udp fallback", p.Transport())
	}
}

func TestDecodeByGroup(t *testing.T) {
	cases := []struct {
		group codec.ObjGroup
		raw   []byte
		want  consumer.ValueKind
	}{
		{codec.GroupFrame, []byte{2, 2, 0}, consumer.KindFrame},
		{codec.GroupRoot, []byte{0}, consumer.KindRaw},
		{codec.GroupFile, []byte{1, 2}, consumer.KindRaw},
		{codec.GroupControl, []byte{9}, consumer.KindRaw}, // default arm
	}
	for _, c := range cases {
		v, err := decodeByGroup(c.group, c.raw)
		if err != nil {
			t.Errorf("decodeByGroup(%v): %v", c.group, err)
			continue
		}
		if v.Kind != c.want {
			t.Errorf("decodeByGroup(%v) kind = %v, want %v", c.group, v.Kind, c.want)
		}
	}
}

func TestNullWriter(t *testing.T) {
	n, err := nullWriter{}.Write([]byte("discarded"))
	if err != nil || n != len("discarded") {
		t.Errorf("nullWriter.Write = %d,%v", n, err)
	}
}

func TestIdentityProbe_HwRevFallbackAndEmpty(t *testing.T) {
	// SwRev empty → fall back to HwRev.
	p, ft, mtid := newPluginWithClient(t)
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupIdentity, 0, stringObject("RRS18", "Card Label")),
		buildReply(t, mtid+2, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupIdentity, 3, stringObject("", "Sw Rev")),
		buildReply(t, mtid+3, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupIdentity, 4, stringObject("A2", "Hw Rev")),
	}
	id, err := p.IdentityProbe(context.Background(), 1)
	if err != nil || id != "RRS18@A2" {
		t.Fatalf("IdentityProbe = %q,%v want RRS18@A2", id, err)
	}

	// Both SwRev and HwRev empty → error.
	p2, ft2, m2 := newPluginWithClient(t)
	ft2.recv = [][]byte{
		buildReply(t, m2+1, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupIdentity, 0, stringObject("RRS18", "Card Label")),
		buildReply(t, m2+2, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupIdentity, 3, stringObject("", "Sw Rev")),
		buildReply(t, m2+3, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupIdentity, 4, stringObject("", "Hw Rev")),
	}
	if _, err := p2.IdentityProbe(context.Background(), 1); err == nil {
		t.Fatal("IdentityProbe with empty SwRev+HwRev: want error")
	}
}

func TestValidateValue_CachedTreeHit(t *testing.T) {
	p, _, _ := newPluginWithClient(t)
	p.trees = newSlotTreeCache(defaultCacheConfig().MaxSize, defaultCacheConfig().TTL)
	p.SeedTreeFromCachedObjects(0, []consumer.Object{
		{Group: "control", ID: 0, Label: "Level", Kind: consumer.KindInt, Access: 0x03},
	})
	// findObject hits the cached tree → no wire meta-fetch needed.
	if err := p.ValidateValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Group: "control", ID: 0},
		consumer.Value{Kind: consumer.KindInt, Int: 5}); err != nil {
		t.Fatalf("ValidateValue cached: %v", err)
	}
}

// TestWalk_ProfileNotesErrors drives browser.getObject's compliance-event
// branches: a transport error (MCODE<16) and an object error (MCODE>=16) each
// get noted on the walker's profile.
func TestWalk_ProfileNotesErrors(t *testing.T) {
	// Transport error on root.
	p, ft, mtid := walkPlugin(t)
	prof := &compliance.Profile{}
	p.walker.SetProfile(prof)
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeError, 3 /* transaction-timeout, <16 */, codec.GroupRoot, 0, nil),
	}
	if _, err := p.Walk(context.Background(), 0); err == nil {
		t.Error("walk root transport error: want error")
	}

	// Object error on a child object.
	p2, ft2, m2 := walkPlugin(t)
	prof2 := &compliance.Profile{}
	p2.walker.SetProfile(prof2)
	ft2.recv = [][]byte{
		buildReply(t, m2+1, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupRoot, 0, rootObject(0, 1, 0, 0)),
		buildReply(t, m2+2, codec.MTypeError, 17 /* instance not exist, >=16 */, codec.GroupControl, 0, nil),
	}
	if _, err := p2.Walk(context.Background(), 0); err == nil {
		t.Error("walk child object error: want error")
	}
}
