package acp1

import (
	"errors"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
)

// rw is the access byte for read+write (bit 0 + bit 1 set).
const rw = uint8(0x03)

// ro is read-only (bit 0 only).
const ro = uint8(0x01)

func TestValidateValueAgainstType_Integer_BadKindRejects(t *testing.T) {
	obj := consumer.Object{Access: rw, Kind: consumer.KindInt}
	val := consumer.Value{Kind: consumer.KindUnknown}
	if err := validateValueAgainstType(codec.TypeInteger, obj, val); !errors.Is(err, consumer.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed, got %v", err)
	}
}

// Out-of-range numerics are NOT a validation error: ACP1 devices clamp a
// setValue to [min,max] (spec p.28; emulator-verified). The validator must not
// be stricter than the device — the consumer predicts the clamped value
// client-side (cmd/dhs predictStored) to stay idempotent. Was a reject before.
func TestValidateValueAgainstType_Integer_BelowMinAccepted(t *testing.T) {
	obj := consumer.Object{Access: rw, Kind: consumer.KindInt, Min: int64(0), Max: int64(65535)}
	val := consumer.Value{Kind: consumer.KindInt, Int: -1}
	if err := validateValueAgainstType(codec.TypeInteger, obj, val); err != nil {
		t.Fatalf("want nil (device clamps to min), got %v", err)
	}
}

func TestValidateValueAgainstType_Integer_AboveMaxAccepted(t *testing.T) {
	obj := consumer.Object{Access: rw, Kind: consumer.KindInt, Min: int64(0), Max: int64(65535)}
	val := consumer.Value{Kind: consumer.KindInt, Int: 999999}
	if err := validateValueAgainstType(codec.TypeInteger, obj, val); err != nil {
		t.Fatalf("want nil (device clamps to max), got %v", err)
	}
}

func TestValidateValueAgainstType_Integer_InRangeAccepted(t *testing.T) {
	obj := consumer.Object{Access: rw, Kind: consumer.KindInt, Min: int64(0), Max: int64(65535)}
	val := consumer.Value{Kind: consumer.KindInt, Int: 12700}
	if err := validateValueAgainstType(codec.TypeInteger, obj, val); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestValidateValueAgainstType_ReadOnlyRejects(t *testing.T) {
	obj := consumer.Object{Access: ro, Kind: consumer.KindInt}
	val := consumer.Value{Kind: consumer.KindInt, Int: 10}
	if err := validateValueAgainstType(codec.TypeInteger, obj, val); !errors.Is(err, consumer.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed for read-only, got %v", err)
	}
}

func TestValidateValueAgainstType_Enum_OutOfOptionsRejects(t *testing.T) {
	obj := consumer.Object{Access: rw, Kind: consumer.KindEnum, EnumItems: []string{"Off", "On", "Auto"}}
	val := consumer.Value{Kind: consumer.KindEnum, Enum: 99, Str: "XYZNotAnOption"}
	if err := validateValueAgainstType(codec.TypeEnum, obj, val); !errors.Is(err, consumer.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed, got %v", err)
	}
}

// Regression: a mistyped option name with no numeric index (Str set, Enum 0)
// must be rejected, not silently treated as index 0. This is the input shape
// `ensure --value Bogus` produces, and it must fail pre-flight (exit 2) the
// same way the encoder's coerceEnum rejects it (was wrongly accepted before).
func TestValidateValueAgainstType_Enum_UnknownLabelNoIndexRejects(t *testing.T) {
	obj := consumer.Object{Access: rw, Kind: consumer.KindEnum, EnumItems: []string{"Off", "On", "Auto"}}
	val := consumer.Value{Kind: consumer.KindEnum, Str: "Bogus"}
	if err := validateValueAgainstType(codec.TypeEnum, obj, val); !errors.Is(err, consumer.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed for unknown label, got %v", err)
	}
}

// A numeric index supplied as a string ("--value 2") is accepted when in range.
func TestValidateValueAgainstType_Enum_NumericStringIndexAccepted(t *testing.T) {
	obj := consumer.Object{Access: rw, Kind: consumer.KindEnum, EnumItems: []string{"Off", "On", "Auto"}}
	val := consumer.Value{Kind: consumer.KindEnum, Str: "2"}
	if err := validateValueAgainstType(codec.TypeEnum, obj, val); err != nil {
		t.Fatalf("want nil for in-range numeric index, got %v", err)
	}
}

func TestValidateValueAgainstType_Enum_KnownLabelAccepted(t *testing.T) {
	obj := consumer.Object{Access: rw, Kind: consumer.KindEnum, EnumItems: []string{"Off", "On", "Auto"}}
	val := consumer.Value{Kind: consumer.KindEnum, Str: "Auto"}
	if err := validateValueAgainstType(codec.TypeEnum, obj, val); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestValidateValueAgainstType_Enum_KnownIndexAccepted(t *testing.T) {
	obj := consumer.Object{Access: rw, Kind: consumer.KindEnum, EnumItems: []string{"Off", "On", "Auto"}}
	val := consumer.Value{Kind: consumer.KindEnum, Enum: 1}
	if err := validateValueAgainstType(codec.TypeEnum, obj, val); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestValidateValueAgainstType_IPAddr_MalformedRejects(t *testing.T) {
	obj := consumer.Object{Access: rw, Kind: consumer.KindIPAddr}
	val := consumer.Value{Kind: consumer.KindString, Str: "not.an.ip.really"}
	if err := validateValueAgainstType(codec.TypeIPAddr, obj, val); !errors.Is(err, consumer.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed, got %v", err)
	}
}

func TestValidateValueAgainstType_IPAddr_DottedQuadAccepted(t *testing.T) {
	obj := consumer.Object{Access: rw, Kind: consumer.KindIPAddr}
	val := consumer.Value{Kind: consumer.KindIPAddr, IPAddr: [4]byte{239, 129, 1, 20}}
	if err := validateValueAgainstType(codec.TypeIPAddr, obj, val); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestValidateValueAgainstType_String_AnyValueAccepted(t *testing.T) {
	obj := consumer.Object{Access: rw, Kind: consumer.KindString, MaxLen: 16}
	val := consumer.Value{Kind: consumer.KindString, Str: "RRS18-rack"}
	if err := validateValueAgainstType(codec.TypeString, obj, val); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestValidateValueAgainstType_String_TooLongRejects(t *testing.T) {
	obj := consumer.Object{Access: rw, Kind: consumer.KindString, MaxLen: 8}
	val := consumer.Value{Kind: consumer.KindString, Str: "this-is-way-too-long-for-eight"}
	if err := validateValueAgainstType(codec.TypeString, obj, val); !errors.Is(err, consumer.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed, got %v", err)
	}
}

func TestValidateValueAgainstType_Byte_OutOfRangeRejects(t *testing.T) {
	obj := consumer.Object{Access: rw, Kind: consumer.KindUint}
	val := consumer.Value{Kind: consumer.KindUint, Uint: 999}
	if err := validateValueAgainstType(codec.TypeByte, obj, val); !errors.Is(err, consumer.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed, got %v", err)
	}
}

func TestValidateValueAgainstType_Byte_NegativeRejects(t *testing.T) {
	obj := consumer.Object{Access: rw, Kind: consumer.KindUint}
	val := consumer.Value{Kind: consumer.KindInt, Int: -5}
	if err := validateValueAgainstType(codec.TypeByte, obj, val); !errors.Is(err, consumer.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed, got %v", err)
	}
}
