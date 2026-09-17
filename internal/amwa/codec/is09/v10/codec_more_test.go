package v10

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is09"
)

func TestNewIsZeroValueCodec(t *testing.T) {
	if New() != (Codec{}) {
		t.Fatal("New must return the zero-value Codec")
	}
	got, ok := is09.Get("v1.0")
	if !ok || got != any(New()) {
		t.Fatal("v1.0 codec not registered under its own identity")
	}
}

// Decode and Validate both refuse an out-of-spec Global with the same
// verdict the parent package gives: the minor adds no leniency.
func TestDecodeAndValidateRefuseViolations(t *testing.T) {
	c := New()
	if _, err := c.DecodeGlobal([]byte(`{"id":"x"}`)); err == nil {
		t.Error("DecodeGlobal must refuse a resource that fails validation")
	}
	if _, err := c.DecodeGlobal([]byte(`{"id":`)); err == nil {
		t.Error("DecodeGlobal must refuse malformed JSON")
	}
	g := validGlobal()
	if err := c.ValidateGlobal(g); err != nil {
		t.Fatalf("ValidateGlobal(valid) = %v", err)
	}
	g.PTP.DomainNumber = 128
	err := c.ValidateGlobal(g)
	if err == nil || !strings.Contains(err.Error(), "ptp.domain_number=128") {
		t.Fatalf("ValidateGlobal error = %v, want the domain range violation", err)
	}
}
