package consumer

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"dhs/internal/plugin"
)

// SlotStatus round-trips through JSON by name (readable snapshots) and
// still accepts the numeric wire code; an unknown name leaves the zero
// value, an unknown code stays as given.
func TestSlotStatusJSONAndString(t *testing.T) {
	for _, s := range []SlotStatus{SlotNoCard, SlotPowerUp, SlotPresent, SlotError, SlotRemoved, SlotBootMode} {
		raw, err := json.Marshal(s)
		if err != nil || string(raw) != `"`+s.String()+`"` {
			t.Fatalf("marshal %d = %s, %v", s, raw, err)
		}
		var back SlotStatus
		if err := json.Unmarshal(raw, &back); err != nil || back != s {
			t.Errorf("round-trip %s -> %d (%v)", raw, back, err)
		}
	}
	if SlotStatus(99).String() != "unknown" {
		t.Errorf("SlotStatus(99) = %q, want unknown", SlotStatus(99).String())
	}
	var n SlotStatus
	if err := json.Unmarshal([]byte(`4`), &n); err != nil || n != SlotRemoved {
		t.Errorf("numeric code -> %d (%v), want removed", n, err)
	}
	if err := json.Unmarshal([]byte(`"martian"`), &n); err != nil {
		t.Errorf("unknown name must not error: %v", err)
	}
	if err := json.Unmarshal([]byte(`true`), &n); err == nil {
		t.Error("a non-numeric, non-string token must be rejected")
	}
}

// ValueKind names are stable identifiers in exported snapshots: every kind
// marshals to its name and parses back, numeric indexes are accepted for
// older files, and anything else is KindUnknown.
func TestValueKindJSONAndString(t *testing.T) {
	kinds := []ValueKind{KindBool, KindInt, KindUint, KindFloat, KindEnum, KindString, KindIPAddr, KindAlarm, KindFrame, KindRaw}
	for _, k := range kinds {
		raw, err := json.Marshal(k)
		if err != nil || string(raw) != `"`+k.String()+`"` {
			t.Fatalf("marshal %d = %s, %v", k, raw, err)
		}
		var back ValueKind
		if err := json.Unmarshal(raw, &back); err != nil || back != k {
			t.Errorf("round-trip %s -> %d (%v)", raw, back, err)
		}
	}
	if KindUnknown.String() != "unknown" || parseKind("nope") != KindUnknown {
		t.Error("unknown kind must render and parse as unknown")
	}
	var k ValueKind
	if err := json.Unmarshal([]byte(`4`), &k); err != nil || k != KindFloat {
		t.Errorf("numeric kind -> %d (%v), want float", k, err)
	}
	if err := json.Unmarshal([]byte(`[]`), &k); err == nil {
		t.Error("a non-numeric, non-string token must be rejected")
	}
}

// Value marshals only the fields its Kind owns and reads the same envelope
// back; a zero Value is null (cache files strip values).
func TestValueJSONRoundTripPerKind(t *testing.T) {
	cases := []Value{
		{Kind: KindBool, Bool: true},
		{Kind: KindInt, Int: -7},
		{Kind: KindUint, Uint: 42},
		{Kind: KindFloat, Float: 1.5},
		{Kind: KindEnum, Enum: 2, Str: "auto"},
		{Kind: KindEnum, Enum: 1},
		{Kind: KindString, Str: "CAM 1"},
		{Kind: KindIPAddr, IPAddr: [4]byte{10, 6, 250, 101}},
		{Kind: KindAlarm, Uint: 3},
		{Kind: KindFrame, SlotStatus: []SlotStatus{SlotPresent, SlotNoCard}},
		{Kind: KindRaw, Raw: []byte{1, 2, 3}},
	}
	for _, v := range cases {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal %+v: %v", v, err)
		}
		var back Value
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatalf("unmarshal %s: %v", raw, err)
		}
		if back.Kind != v.Kind || back.Bool != v.Bool || back.Int != v.Int || back.Uint != v.Uint ||
			back.Float != v.Float || back.Str != v.Str || back.IPAddr != v.IPAddr || back.Enum != v.Enum ||
			len(back.SlotStatus) != len(v.SlotStatus) || string(back.Raw) != string(v.Raw) {
			t.Errorf("round-trip %s -> %+v, want %+v", raw, back, v)
		}
		if strings.Contains(string(raw), `"kind":"unknown"`) {
			t.Errorf("kind name lost: %s", raw)
		}
	}
	if raw, _ := json.Marshal(Value{}); string(raw) != "null" {
		t.Errorf("zero Value = %s, want null", raw)
	}
	var v Value
	if err := json.Unmarshal([]byte(`{"kind":"ipaddr","ip":"not-an-ip"}`), &v); err != nil || v.IPAddr != [4]byte{} {
		t.Errorf("unparsable ip must be ignored, got %v %v", v.IPAddr, err)
	}
	// Syntactically valid JSON whose envelope does not fit: the decoder
	// reaches UnmarshalJSON (a syntax error never does) and it must fail.
	if err := json.Unmarshal([]byte(`{"kind":"int","int":"seven"}`), &v); err == nil {
		t.Error("an envelope with the wrong field type must error")
	}
}

// Access bits map to the three capabilities independently.
func TestObjectAccessBits(t *testing.T) {
	o := Object{Access: 0x05}
	if !o.HasRead() || o.HasWrite() || !o.HasSetDef() {
		t.Errorf("access 0x05: read=%v write=%v setdef=%v, want true/false/true", o.HasRead(), o.HasWrite(), o.HasSetDef())
	}
	if (Object{Access: 0x02}).HasRead() || !(Object{Access: 0x02}).HasWrite() {
		t.Error("access 0x02 is write-only")
	}
}

// CardIdentity.IsZero is true only when every field is empty.
func TestCardIdentityIsZero(t *testing.T) {
	if !(CardIdentity{}).IsZero() {
		t.Error("empty identity must be zero")
	}
	for _, c := range []CardIdentity{{Model: "GXG100"}, {SwRev: "1.2"}, {HwRev: "B"}} {
		if c.IsZero() {
			t.Errorf("%+v must not be zero", c)
		}
	}
}

// The two error families render, unwrap and satisfy DHSError so one
// errors.As catches both.
func TestErrorFamilies(t *testing.T) {
	inner := errors.New("boom")
	te := &TransportError{Op: "send", Err: inner}
	if te.Error() != "transport send: boom" || !errors.Is(te, inner) {
		t.Errorf("TransportError = %q, unwrap ok=%v", te.Error(), errors.Is(te, inner))
	}
	if (&TransportError{Op: "connect"}).Error() != "transport: connect" {
		t.Error("TransportError without a cause renders the op alone")
	}
	ve := &ValidationError{Field: "value", Reason: "out of range"}
	if ve.Error() != "validation: value: out of range" {
		t.Errorf("ValidationError = %q", ve.Error())
	}
	for _, err := range []error{te, ve} {
		var d DHSError
		if !errors.As(err, &d) {
			t.Errorf("%T must satisfy DHSError", err)
		}
	}
}

type fakeFactory struct{ name string }

func (f fakeFactory) Meta() ProtocolMeta       { return ProtocolMeta{Name: f.name} }
func (f fakeFactory) New(plugin.Deps) Protocol { return nil }

// The registry is case-insensitive, lists alphabetically, refuses
// duplicates and nil/empty registrations, and names the alternatives when
// a lookup misses.
func TestRegistryContract(t *testing.T) {
	Register(fakeFactory{name: "ZZ-Test-Proto"})
	if f, err := Get("zz-test-proto"); err != nil || f == nil {
		t.Fatalf("Get lower-case = %v, %v", f, err)
	}
	if _, err := Get("no-such-proto"); err == nil || !strings.Contains(err.Error(), "zz-test-proto") {
		t.Errorf("miss must name the available protocols: %v", err)
	}
	names := List()
	if len(names) == 0 || names[len(names)-1] != "zz-test-proto" {
		t.Errorf("List = %v, want sorted with zz-test-proto last", names)
	}
	for _, f := range []ProtocolFactory{nil, fakeFactory{name: ""}, fakeFactory{name: "zz-test-proto"}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Register(%v) must panic", f)
				}
			}()
			Register(f)
		}()
	}
}

// pathEqual is exact, element by element.
func TestPathEqual(t *testing.T) {
	if !pathEqual([]string{"a", "b"}, []string{"a", "b"}) {
		t.Error("equal paths")
	}
	if pathEqual([]string{"a", "b"}, []string{"a", "c"}) || pathEqual([]string{"a"}, []string{"a", "b"}) {
		t.Error("differing paths must not be equal")
	}
}
