package acp1

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
	"dhs/internal/consumer/compliance"
)

// stringObject builds the wire bytes for a String identity object
// (type=5, num_props=6) with the given value+label.
func stringObject(value, label string) []byte {
	v := []byte(value)
	l := []byte(label)
	v = append(v, 0x00) // NUL-terminate value
	l = append(l, 0x00) // NUL-terminate label
	out := []byte{
		0x05, // type = String
		0x06, // num_props
		0x01, // access (read)
	}
	out = append(out, v...)
	out = append(out, byte(len(value))) // max_len
	out = append(out, l...)
	return out
}

// integerObject builds the wire bytes for an Integer identity object
// (type=1, num_props=10) with a given i16 value. min/max/default/step
// are zero — sufficient for identity purposes.
func integerObject(value int16, label string) []byte {
	out := []byte{
		0x01,                                  // type = Integer
		0x0A,                                  // num_props
		0x01,                                  // access (read)
		byte(uint16(value) >> 8), byte(value), // value (i16 BE)
		0x00, 0x00, // default
		0x00, 0x01, // step (1)
		0x00, 0x00, // min
		0xFF, 0xFF, // max
	}
	l := append([]byte(label), 0x00)
	out = append(out, l...)
	out = append(out, 0x00) // unit (empty + NUL)
	return out
}

// newPluginWithClient builds a Plugin wired to a fake client transport.
// Mirrors the live Connect flow's bookkeeping just enough that
// GetIdentity / GetValue work in unit tests.
func newPluginWithClient(t *testing.T) (*Plugin, *fakeTransport, uint32) {
	t.Helper()
	ft := &fakeTransport{}
	c := NewClient(ft, nil, ClientConfig{
		MaxRetries:     2,
		ReceiveTimeout: 50 * time.Millisecond,
	})
	c.nextMTID = 1000
	p := &Plugin{
		logger:  slog.Default(),
		client:  c,
		profile: &compliance.Profile{},
	}
	return p, ft, 1000
}

func TestGetIdentity_HappyPath(t *testing.T) {
	p, ft, mtid := newPluginWithClient(t)

	// Three replies in order: id=0 (Card Label), id=3 (Software Rev), id=4 (Hardware Rev).
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeReply, byte(codec.MethodGetObject),
			codec.GroupIdentity, 0, stringObject("RRS18", "Card Label")),
		buildReply(t, mtid+2, codec.MTypeReply, byte(codec.MethodGetObject),
			codec.GroupIdentity, 3, integerObject(1601, "SwRev")),
		buildReply(t, mtid+3, codec.MTypeReply, byte(codec.MethodGetObject),
			codec.GroupIdentity, 4, integerObject(100, "HwRev")),
	}

	id, err := p.GetIdentity(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetIdentity: %v", err)
	}
	if id.Model != "RRS18" {
		t.Fatalf("Model = %q, want RRS18", id.Model)
	}
	if id.SwRev != "1601" {
		t.Fatalf("SwRev = %q, want 1601", id.SwRev)
	}
	if id.HwRev != "100" {
		t.Fatalf("HwRev = %q, want 100", id.HwRev)
	}
	if got := p.profile.Snapshot()[IdentityNAK]; got != 0 {
		t.Fatalf("IdentityNAK fired %d times, want 0", got)
	}
}

func TestGetIdentity_CardLabelNAK_FiresComplianceEvent(t *testing.T) {
	p, ft, mtid := newPluginWithClient(t)

	// First reply (id=0) is an error; the probe should bail with
	// ErrIdentityUnresolved and fire the IdentityNAK compliance event.
	errReply := buildReply(t, mtid+1, codec.MTypeError, 17, /* object instance not exist */
		codec.GroupIdentity, 0, nil)
	ft.recv = [][]byte{errReply}

	_, err := p.GetIdentity(context.Background(), 1)
	if !errors.Is(err, consumer.ErrIdentityUnresolved) {
		t.Fatalf("err = %v, want ErrIdentityUnresolved", err)
	}
	if got := p.profile.Snapshot()[IdentityNAK]; got != 1 {
		t.Fatalf("IdentityNAK fired %d times, want 1", got)
	}
}

func TestGetIdentity_PartialIdentity_OnSwRevNAK(t *testing.T) {
	p, ft, mtid := newPluginWithClient(t)

	// id=0 OK, id=3 NAK, id=4 OK. The probe should NOT fail —
	// partial fingerprints are allowed; SwRev empty.
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeReply, byte(codec.MethodGetObject),
			codec.GroupIdentity, 0, stringObject("RRS18", "Card Label")),
		buildReply(t, mtid+2, codec.MTypeError, 17,
			codec.GroupIdentity, 3, nil),
		buildReply(t, mtid+3, codec.MTypeReply, byte(codec.MethodGetObject),
			codec.GroupIdentity, 4, integerObject(100, "HwRev")),
	}

	id, err := p.GetIdentity(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetIdentity: %v", err)
	}
	if id.Model != "RRS18" || id.SwRev != "" || id.HwRev != "100" {
		t.Fatalf("partial identity wrong: %+v", id)
	}
	// Soft-NAK must NOT trip the IdentityNAK compliance event.
	if got := p.profile.Snapshot()[IdentityNAK]; got != 0 {
		t.Fatalf("IdentityNAK fired %d times, want 0", got)
	}
}

func TestGetIdentity_NotConnected(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	_, err := p.GetIdentity(context.Background(), 1)
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
}

func TestCardIdentity_IsZero(t *testing.T) {
	if !(consumer.CardIdentity{}).IsZero() {
		t.Fatal("zero value should be zero")
	}
	if (consumer.CardIdentity{Model: "RRS18"}).IsZero() {
		t.Fatal("non-zero Model should not be zero")
	}
}

func TestPlugin_SatisfiesIdentifier(t *testing.T) {
	// Compile-time assertion that *Plugin satisfies consumer.Identifier.
	var _ consumer.Identifier = (*Plugin)(nil)
}
