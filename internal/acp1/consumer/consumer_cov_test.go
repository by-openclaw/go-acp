package acp1

import (
	"context"
	"log/slog"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
)

// rwIntegerObject is integerObject with read+write access so ValidateValue's
// write-access gate passes.
func rwIntegerObject(value int16, label string) []byte {
	b := integerObject(value, label)
	b[2] = 0x03 // access = read|write
	return b
}

// ---------------------------------------------------------------- catalogue

func TestCatalogue_AndLookup(t *testing.T) {
	cat := Catalogue()
	if len(cat) == 0 {
		t.Fatal("Catalogue empty")
	}
	for _, e := range cat {
		if e.Address() == "" || e.Name == "" {
			t.Errorf("bad entry %+v", e)
		}
	}
	// Lookup hits.
	for _, addr := range []string{"method:0", "objgroup:2", "msgtype:1", "obj-err:16"} {
		if _, ok := LookupCatalogue(addr); !ok {
			t.Errorf("LookupCatalogue(%q) miss, want hit", addr)
		}
	}
	// Lookup misses.
	for _, addr := range []string{"method:250", "bogus:0", "no-colon", "method:abc"} {
		if _, ok := LookupCatalogue(addr); ok {
			t.Errorf("LookupCatalogue(%q) hit, want miss", addr)
		}
	}
}

func TestCatalogueConverters(t *testing.T) {
	if uint8ToDec(0) != "0" || uint8ToDec(42) != "42" || uint8ToDec(255) != "255" {
		t.Error("uint8ToDec wrong")
	}
	ok := map[string]uint8{"0": 0, "42": 42, "255": 255}
	for s, want := range ok {
		if n, bad := decToUint8(s); bad || n != want {
			t.Errorf("decToUint8(%q) = %d,%v", s, n, bad)
		}
	}
	for _, s := range []string{"", "256", "12a", "1000", "999"} {
		if _, bad := decToUint8(s); !bad {
			t.Errorf("decToUint8(%q): want bad=true", s)
		}
	}
}

// ---------------------------------------------------------------- accessors

func TestTransportKind_String(t *testing.T) {
	if TransportUDP.String() != "udp" || TransportTCPDirect.String() != "tcp" || TransportAuto.String() != "auto" {
		t.Error("TransportKind.String mismatch")
	}
}

func TestPlugin_Accessors(t *testing.T) {
	p := &Plugin{}
	if p.ComplianceProfile() != nil {
		t.Error("ComplianceProfile before connect should be nil")
	}
	p.SetRecorder(nil) // exercises the setter
	p.SetTransport(TransportTCPDirect)
	if p.Transport() != TransportTCPDirect {
		t.Errorf("Transport = %v, want tcp", p.Transport())
	}
	k1 := reqKey(consumer.ValueRequest{Slot: 1, Group: "control", Label: "Gain", ID: 5})
	k2 := reqKey(consumer.ValueRequest{Slot: 1, Group: "control", Label: "Gain", ID: 5})
	if k1 != k2 {
		t.Error("reqKey not stable for identical requests")
	}
}

// ---------------------------------------------------------------- ValidateValue

func TestValidateValue_NotConnected(t *testing.T) {
	p := &Plugin{}
	err := p.ValidateValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Group: "control", ID: 5}, consumer.Value{Int: 1})
	if err != consumer.ErrNotConnected {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
}

func TestValidateValue_ReadOnlyRejected(t *testing.T) {
	p, ft, mtid := newPluginWithClient(t)
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeReply, byte(codec.MethodGetObject),
			codec.GroupControl, 5, integerObject(0, "Level")), // access = read-only
	}
	err := p.ValidateValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Group: "control", ID: 5},
		consumer.Value{Kind: consumer.KindInt, Int: 7})
	if err == nil {
		t.Fatal("read-only object: want validation error")
	}
}

func TestValidateValue_HappyPath(t *testing.T) {
	p, ft, mtid := newPluginWithClient(t)
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeReply, byte(codec.MethodGetObject),
			codec.GroupControl, 5, rwIntegerObject(0, "Level")),
	}
	err := p.ValidateValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Group: "control", ID: 5},
		consumer.Value{Kind: consumer.KindInt, Int: 7})
	if err != nil {
		t.Fatalf("valid RW integer set: %v", err)
	}
}

func TestValidateValue_ObjectNotFound(t *testing.T) {
	p, ft, mtid := newPluginWithClient(t)
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeError, 17 /* instance not exist */, codec.GroupControl, 5, nil),
	}
	err := p.ValidateValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Group: "control", ID: 5},
		consumer.Value{Kind: consumer.KindInt, Int: 7})
	if err == nil {
		t.Fatal("missing object: want error")
	}
}

// ---------------------------------------------------------------- Connect/Disconnect

// TestConnectDisconnect_UDP drives the real UDP connect path (connectionless,
// so no server is required), the already-connected idempotent path, the
// host-mismatch rejection, and teardown.
func TestConnectDisconnect_UDP(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	ctx := context.Background()
	if err := p.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = p.Disconnect() })

	// Same host → idempotent no-op.
	if err := p.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Errorf("re-Connect same host: %v", err)
	}
	// Different host while connected → error.
	if err := p.Connect(ctx, "127.0.0.2", 0); err == nil {
		t.Error("Connect to a different host while connected should error")
	}
	if p.ComplianceProfile() == nil {
		t.Error("ComplianceProfile should be set after Connect")
	}
	if err := p.Disconnect(); err != nil {
		t.Errorf("Disconnect: %v", err)
	}
	// Disconnect is idempotent.
	if err := p.Disconnect(); err != nil {
		t.Errorf("second Disconnect: %v", err)
	}
}
