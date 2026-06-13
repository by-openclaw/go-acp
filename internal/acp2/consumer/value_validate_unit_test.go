package acp2

import (
	"context"
	"errors"
	"testing"

	"dhs/internal/acp2/codec"
	"dhs/internal/consumer"
)

// TestValidateValue_NotConnected covers the nil-session guard at the top of
// ValidateValue.
func TestValidateValue_NotConnected(t *testing.T) {
	p := &Plugin{trees: newWalkedTreeCache(4, 0)}
	err := p.ValidateValue(context.Background(), consumer.ValueRequest{Slot: 1}, consumer.Value{})
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("want ErrNotConnected, got %v", err)
	}
}

// TestValidateValueAgainstType_RawBytesEscapeHatch covers the raw-bytes
// trust path.
func TestValidateValueAgainstType_RawBytesEscapeHatch(t *testing.T) {
	val := consumer.Value{Raw: []byte{0xDE, 0xAD}}
	if err := validateValueAgainstType(codec.ObjTypeNumber, codec.NumTypeU32, nil, val, nil); err != nil {
		t.Fatalf("raw bytes should be trusted, got %v", err)
	}
}

// TestValidateValueAgainstType_IPv4Empty covers the empty-IPv4 reject branch.
func TestValidateValueAgainstType_IPv4Empty(t *testing.T) {
	val := consumer.Value{Kind: consumer.KindString, Str: ""}
	err := validateValueAgainstType(codec.ObjTypeIPv4, codec.NumTypeIPv4, nil, val, nil)
	if !errors.Is(err, consumer.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed for empty ipv4, got %v", err)
	}
}

// TestValidateValueAgainstType_IPv4WrongPartCount covers the malformed
// (wrong number of octets) ipv4 branch.
func TestValidateValueAgainstType_IPv4WrongPartCount(t *testing.T) {
	val := consumer.Value{Kind: consumer.KindString, Str: "10.0.1"}
	err := validateValueAgainstType(codec.ObjTypeIPv4, codec.NumTypeIPv4, nil, val, nil)
	if !errors.Is(err, consumer.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed for 3-octet ipv4, got %v", err)
	}
}

// TestValidateValueAgainstType_IPv4OctetOutOfRange covers the >255 octet
// branch.
func TestValidateValueAgainstType_IPv4OctetOutOfRange(t *testing.T) {
	val := consumer.Value{Kind: consumer.KindString, Str: "10.0.0.999"}
	err := validateValueAgainstType(codec.ObjTypeIPv4, codec.NumTypeIPv4, nil, val, nil)
	if !errors.Is(err, consumer.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed for out-of-range octet, got %v", err)
	}
}

// TestValidateValueAgainstType_IPv4FromIPAddr covers the non-zero IPAddr
// fast-accept branch.
func TestValidateValueAgainstType_IPv4FromIPAddr(t *testing.T) {
	val := consumer.Value{Kind: consumer.KindIPAddr, IPAddr: [4]byte{192, 168, 1, 1}}
	if err := validateValueAgainstType(codec.ObjTypeIPv4, codec.NumTypeIPv4, nil, val, nil); err != nil {
		t.Fatalf("non-zero IPAddr should accept, got %v", err)
	}
}

// TestValidateValueAgainstType_EnumNoOptionsBestEffort covers the
// no-options-map best-effort accept branch.
func TestValidateValueAgainstType_EnumNoOptionsBestEffort(t *testing.T) {
	val := consumer.Value{Kind: consumer.KindEnum, Enum: 5}
	if err := validateValueAgainstType(codec.ObjTypeEnum, codec.NumTypePreset, nil, val, nil); err != nil {
		t.Fatalf("no options map should best-effort accept, got %v", err)
	}
}

// TestValidateValueAgainstType_NodeAndUnknownAccept covers the node /
// fall-through accept path.
func TestValidateValueAgainstType_NodeAndUnknownAccept(t *testing.T) {
	val := consumer.Value{Kind: consumer.KindString, Str: "anything"}
	if err := validateValueAgainstType(codec.ObjTypeNode, codec.NumTypeU8, nil, val, nil); err != nil {
		t.Fatalf("node container should accept, got %v", err)
	}
}
