package acp1

import (
	"context"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
	"dhs/internal/export/canonical"
)

// TestSortByNumber_Unsorted drives the inner swap+break of the insertion
// sort with a genuinely out-of-order slice so the comparison, the swap,
// and the early break are all exercised.
func TestSortByNumber_Unsorted(t *testing.T) {
	mk := func(n int) canonical.Element {
		g := &canonical.Node{}
		g.Number = n
		return g
	}
	els := []canonical.Element{mk(3), mk(1), mk(2), mk(1)}
	sortByNumber(els)
	want := []int{1, 1, 2, 3}
	for i, e := range els {
		if e.Common().Number != want[i] {
			t.Fatalf("index %d: got Number=%d, want %d", i, e.Common().Number, want[i])
		}
	}
}

// TestCoerceInt_DefaultAndError covers the all-zero default arm and the
// string-parse error arm of coerceInt.
func TestCoerceInt_DefaultAndError(t *testing.T) {
	n, err := coerceInt(consumer.Value{}, -10, 10) // all zero → default arm
	if err != nil || n != 0 {
		t.Fatalf("zero value: n=%d err=%v", n, err)
	}
	if _, err := coerceInt(consumer.Value{Str: "not-a-number"}, -10, 10); err == nil {
		t.Fatal("expected parse error")
	}
	if _, err := coerceInt(consumer.Value{Int: 100}, -10, 10); err == nil {
		t.Fatal("expected out-of-range error")
	}
}

// TestCoerceUint_AllArms covers the parse-error and explicit-zero arms of
// coerceUint plus the int>0 / float>0 fallbacks.
func TestCoerceUint_AllArms(t *testing.T) {
	if _, err := coerceUint(consumer.Value{Str: "nope"}, 255); err == nil {
		t.Fatal("expected parse error")
	}
	if n, err := coerceUint(consumer.Value{Str: "42"}, 255); err != nil || n != 42 {
		t.Fatalf("valid string: n=%d err=%v", n, err)
	}
	n, err := coerceUint(consumer.Value{}, 255) // explicit zero
	if err != nil || n != 0 {
		t.Fatalf("zero: n=%d err=%v", n, err)
	}
	if n, _ := coerceUint(consumer.Value{Int: 7}, 255); n != 7 {
		t.Fatalf("int>0: got %d", n)
	}
	if n, _ := coerceUint(consumer.Value{Float: 9}, 255); n != 9 {
		t.Fatalf("float>0: got %d", n)
	}
	if _, err := coerceUint(consumer.Value{Uint: 300}, 255); err == nil {
		t.Fatal("expected range error")
	}
}

// TestRelMethodSuffix covers every arm of the label helper.
func TestRelMethodSuffix(t *testing.T) {
	cases := map[codec.Method]string{
		codec.MethodSetIncValue: "Inc",
		codec.MethodSetDecValue: "Dec",
		codec.MethodSetDefValue: "Def",
		codec.MethodSetValue:    "Value", // default arm
	}
	for m, want := range cases {
		if got := relMethodSuffix(m); got != want {
			t.Errorf("relMethodSuffix(%v) = %q, want %q", m, got, want)
		}
	}
}

// TestKindToCanonicalType_UnknownFallback hits the final `return
// ParamString` for an unknown kind with a non-File ACP type.
func TestKindToCanonicalType_UnknownFallback(t *testing.T) {
	if got := kindToCanonicalType(consumer.KindUnknown, codec.TypeRoot); got != canonical.ParamString {
		t.Errorf("unknown fallback = %q", got)
	}
}

// newFakeClient builds a *Client over a fakeTransport for clientIface
// error-path tests.
func newFakeClient(t *testing.T, recv [][]byte) *Client {
	t.Helper()
	ft := &fakeTransport{recv: recv}
	c := NewClient(ft, nil, ClientConfig{
		MaxRetries:     1,
		ReceiveTimeout: 20 * time.Millisecond,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
	})
	return c
}

// TestIdentityField_ErrorPaths covers the Do-error, IsError, and
// decode-error arms of identityField.
func TestIdentityField_ErrorPaths(t *testing.T) {
	// Do error: empty recv queue → io.EOF surfaces as a fatal error.
	c := newFakeClient(t, nil)
	if _, err := identityField(context.Background(), c, 0, 0); err == nil {
		t.Fatal("expected Do error")
	}

	// IsError: device returns an object error reply.
	errReply := []byte{
		0x00, 0x00, 0x00, 0x00, // MTID patched by client? no — client filters by MTID.
	}
	_ = errReply
	// Build an error reply matching the client's allocated MTID. The client
	// allocates seq starting from a random seed, so use a client with a
	// known nextMTID.
	ce := newFakeClient(t, nil)
	ce.nextMTID = 500
	er := &codec.Message{MTID: 501, MType: codec.MTypeError, MAddr: 0, MCode: byte(codec.OErrNoReadAccess)}
	erb, err := er.Encode()
	if err != nil {
		t.Fatalf("encode err reply: %v", err)
	}
	ce.tr.(*fakeTransport).recv = [][]byte{erb}
	if _, err := identityField(context.Background(), ce, 0, 0); err == nil {
		t.Fatal("expected IsError path error")
	}

	// Decode error: a successful reply whose Value is not a valid object.
	cd := newFakeClient(t, nil)
	cd.nextMTID = 600
	ok := &codec.Message{
		MTID:     601,
		MType:    codec.MTypeReply,
		MAddr:    0,
		MCode:    byte(codec.MethodGetObject),
		ObjGroup: codec.GroupIdentity,
		ObjID:    0,
		Value:    []byte{0xFF}, // type=0xFF, truncated → DecodeObject fails
	}
	okb, err := ok.Encode()
	if err != nil {
		t.Fatalf("encode ok reply: %v", err)
	}
	cd.tr.(*fakeTransport).recv = [][]byte{okb}
	if _, err := identityField(context.Background(), cd, 0, 0); err == nil {
		t.Fatal("expected decode error path")
	}
}
