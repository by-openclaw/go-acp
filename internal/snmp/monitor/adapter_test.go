package snmpmon

import (
	"context"
	"errors"
	"net"
	"testing"

	dhsc "dhs/internal/consumer"
	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/mib"
)

// compile-time proof the adapter is a neutral Protocol.
var _ dhsc.Protocol = (*Protocol)(nil)

type fakeSession struct {
	getResp []codec.VarBind
	getErr  error
	setResp []codec.VarBind
	setErr  error
	lastSet []codec.VarBind
	closed  bool
}

func (f *fakeSession) Get(ctx context.Context, names ...codec.OID) ([]codec.VarBind, error) {
	return f.getResp, f.getErr
}

func (f *fakeSession) Set(ctx context.Context, binds ...codec.VarBind) ([]codec.VarBind, error) {
	f.lastSet = binds
	if f.setErr != nil {
		return nil, f.setErr
	}
	if f.setResp != nil {
		return f.setResp, nil
	}
	return binds, nil
}

func (f *fakeSession) Close() error { f.closed = true; return nil }

func oid(s string) codec.OID { return codec.MustParseOID(s) }

func TestGetValueConvertsTypes(t *testing.T) {
	cases := []struct {
		name string
		in   codec.Value
		kind dhsc.ValueKind
		want any
	}{
		{"integer", codec.Int(5), dhsc.KindInt, int64(5)},
		{"gauge", codec.Gauge32(42), dhsc.KindUint, uint64(42)},
		{"counter", codec.Counter32(7), dhsc.KindUint, uint64(7)},
		{"timeticks", codec.TimeTicks(100), dhsc.KindUint, uint64(100)},
		{"string", codec.String("RX1290"), dhsc.KindString, "RX1290"},
		{"oid", codec.ObjectID(oid("1.3.6.1")), dhsc.KindString, "1.3.6.1"},
		{"ip", codec.IPAddress(net.ParseIP("10.6.250.104")), dhsc.KindIPAddr, [4]byte{10, 6, 250, 104}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := &fakeSession{getResp: []codec.VarBind{{Name: oid("1.1"), Value: c.in}}}
			p := New(fs, nil)
			got, err := p.GetValue(context.Background(), dhsc.ValueRequest{Path: "1.1"})
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != c.kind {
				t.Fatalf("kind = %v, want %v", got.Kind, c.kind)
			}
			switch want := c.want.(type) {
			case int64:
				if got.Int != want {
					t.Errorf("Int = %d, want %d", got.Int, want)
				}
			case uint64:
				if got.Uint != want {
					t.Errorf("Uint = %d, want %d", got.Uint, want)
				}
			case string:
				if got.Str != want {
					t.Errorf("Str = %q, want %q", got.Str, want)
				}
			case [4]byte:
				if got.IPAddr != want {
					t.Errorf("IPAddr = %v, want %v", got.IPAddr, want)
				}
			}
		})
	}
}

func TestGetValueException(t *testing.T) {
	fs := &fakeSession{getResp: []codec.VarBind{{Name: oid("1.1"), Value: codec.NoSuchObject()}}}
	if _, err := New(fs, nil).GetValue(context.Background(), dhsc.ValueRequest{Path: "1.1"}); err == nil {
		t.Fatal("noSuchObject should be an error, not data")
	}
}

func TestGetValueEmptyAndError(t *testing.T) {
	if _, err := New(&fakeSession{}, nil).GetValue(context.Background(), dhsc.ValueRequest{Path: "1.1"}); err == nil {
		t.Error("empty response should error")
	}
	fs := &fakeSession{getErr: errors.New("timeout")}
	if _, err := New(fs, nil).GetValue(context.Background(), dhsc.ValueRequest{Path: "1.1"}); err == nil {
		t.Error("session error should propagate")
	}
}

func TestGetValueBadRequest(t *testing.T) {
	p := New(&fakeSession{}, nil)
	if _, err := p.GetValue(context.Background(), dhsc.ValueRequest{}); err == nil {
		t.Error("no address should error")
	}
	if _, err := p.GetValue(context.Background(), dhsc.ValueRequest{Path: "not-an-oid"}); err == nil {
		t.Error("bad OID should error")
	}
}

func TestSetValueRoundTrip(t *testing.T) {
	fs := &fakeSession{}
	p := New(fs, nil)
	got, err := p.SetValue(context.Background(), dhsc.ValueRequest{Path: "1.3.6.1.4.1.1773.1.1.1.8.0"},
		dhsc.Value{Kind: dhsc.KindInt, Int: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != dhsc.KindInt || got.Int != 1 {
		t.Errorf("echoed = %+v, want int 1", got)
	}
	if len(fs.lastSet) != 1 || fs.lastSet[0].Value.Type != codec.TypeInteger || fs.lastSet[0].Value.Int != 1 {
		t.Errorf("wire set = %+v, want Integer 1", fs.lastSet)
	}
}

func TestSetValueKinds(t *testing.T) {
	cases := []struct {
		name string
		in   dhsc.Value
		typ  codec.ValueType
	}{
		{"int", dhsc.Value{Kind: dhsc.KindInt, Int: 3}, codec.TypeInteger},
		{"enum", dhsc.Value{Kind: dhsc.KindEnum, Enum: 4}, codec.TypeInteger},
		{"uint", dhsc.Value{Kind: dhsc.KindUint, Uint: 9}, codec.TypeGauge32},
		{"string", dhsc.Value{Kind: dhsc.KindString, Str: "x"}, codec.TypeOctetString},
		{"ip", dhsc.Value{Kind: dhsc.KindIPAddr, IPAddr: [4]byte{1, 2, 3, 4}}, codec.TypeIPAddress},
		{"bool", dhsc.Value{Kind: dhsc.KindBool, Bool: true}, codec.TypeInteger},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := &fakeSession{}
			if _, err := New(fs, nil).SetValue(context.Background(), dhsc.ValueRequest{Path: "1.1"}, c.in); err != nil {
				t.Fatal(err)
			}
			if fs.lastSet[0].Value.Type != c.typ {
				t.Errorf("wire type = %v, want %v", fs.lastSet[0].Value.Type, c.typ)
			}
		})
	}
}

func TestSetValueErrors(t *testing.T) {
	// unsupported kind
	if _, err := New(&fakeSession{}, nil).SetValue(context.Background(), dhsc.ValueRequest{Path: "1.1"},
		dhsc.Value{Kind: dhsc.KindFrame}); err == nil {
		t.Error("unsupported kind should error")
	}
	// bad OID
	if _, err := New(&fakeSession{}, nil).SetValue(context.Background(), dhsc.ValueRequest{Path: "nope"},
		dhsc.Value{Kind: dhsc.KindInt}); err == nil {
		t.Error("bad OID should error")
	}
	// session error
	fs := &fakeSession{setErr: errors.New("readOnly")}
	if _, err := New(fs, nil).SetValue(context.Background(), dhsc.ValueRequest{Path: "1.1"},
		dhsc.Value{Kind: dhsc.KindInt, Int: 1}); err == nil {
		t.Error("session set error should propagate")
	}
	// empty echo falls back to the written value
	got, err := New(&fakeSession{setResp: []codec.VarBind{}}, nil).SetValue(context.Background(),
		dhsc.ValueRequest{Path: "1.1"}, dhsc.Value{Kind: dhsc.KindInt, Int: 7})
	if err != nil || got.Int != 7 {
		t.Errorf("empty echo: got %+v err %v, want int 7", got, err)
	}
}

func TestDisconnectAndUnsupported(t *testing.T) {
	fs := &fakeSession{}
	p := New(fs, nil)
	if err := p.Connect(context.Background(), "10.0.0.1", 161); err != nil {
		t.Errorf("Connect should be a no-op, got %v", err)
	}
	if err := p.Disconnect(); err != nil || !fs.closed {
		t.Errorf("Disconnect should close the session")
	}
	if err := New(nil, nil).Disconnect(); err != nil {
		t.Errorf("Disconnect with nil session should be safe")
	}
	if _, err := p.GetDeviceInfo(context.Background()); err == nil {
		t.Error("GetDeviceInfo should be unsupported")
	}
	if _, err := p.GetSlotInfo(context.Background(), 0); err == nil {
		t.Error("GetSlotInfo should be unsupported")
	}
	if _, err := p.Walk(context.Background(), 0); err == nil {
		t.Error("Walk should be unsupported")
	}
	if err := p.Subscribe(dhsc.ValueRequest{}, nil); err == nil {
		t.Error("Subscribe should be unsupported")
	}
	if err := p.Unsubscribe(dhsc.ValueRequest{}); err == nil {
		t.Error("Unsubscribe should be unsupported")
	}
}

// The same rule as the walk: the object's textual convention decides.
func TestAnAddressReadsAsAnAddress(t *testing.T) {
	table, err := mib.Load("ateme-dr5000")
	if err != nil {
		t.Fatal(err)
	}
	const o = "1.3.6.1.4.1.27338.5.3.2.2.2.1.0" // an InetAddress
	s := &fakeSession{getResp: []codec.VarBind{{Name: codec.MustParseOID(o), Value: codec.Bytes([]byte{10, 6, 255, 114})}}}
	v, err := New(s, table).GetValue(context.Background(), dhsc.ValueRequest{Path: o})
	if err != nil || v.Str != "10.6.255.114" {
		t.Fatalf("%+v, %v", v, err)
	}
	// With no table the octets stay as the wire carried them.
	if v, _ := New(s, nil).GetValue(context.Background(), dhsc.ValueRequest{Path: o}); v.Str != "\x0a\x06\xff\x72" {
		t.Errorf("%q", v.Str)
	}
}

func TestAnAddressIsWrittenAsItsOctets(t *testing.T) {
	table, err := mib.Load("ateme-dr5000")
	if err != nil {
		t.Fatal(err)
	}
	const o = "1.3.6.1.4.1.27338.5.3.2.2.2.1.0"
	s := &fakeSession{}
	if _, err := New(s, table).SetValue(context.Background(), dhsc.ValueRequest{Path: o},
		dhsc.Value{Kind: dhsc.KindString, Str: "10.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	if len(s.lastSet) != 1 || string(s.lastSet[0].Value.Bytes) != "\x0a\x00\x00\x01" {
		t.Errorf("%+v", s.lastSet)
	}
}
