package acp1

import (
	"errors"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/protocol"
)

// rw is the access byte for read+write (bit 0 + bit 1 set).
const rw = uint8(0x03)

// ro is read-only (bit 0 only).
const ro = uint8(0x01)

func TestValidateValueAgainstType_Integer_BadKindRejects(t *testing.T) {
	obj := protocol.Object{Access: rw, Kind: protocol.KindInt}
	val := protocol.Value{Kind: protocol.KindUnknown}
	if err := validateValueAgainstType(codec.TypeInteger, obj, val); !errors.Is(err, protocol.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed, got %v", err)
	}
}

func TestValidateValueAgainstType_Integer_BelowMinRejects(t *testing.T) {
	obj := protocol.Object{Access: rw, Kind: protocol.KindInt, Min: int64(0), Max: int64(65535)}
	val := protocol.Value{Kind: protocol.KindInt, Int: -1}
	if err := validateValueAgainstType(codec.TypeInteger, obj, val); !errors.Is(err, protocol.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed, got %v", err)
	}
}

func TestValidateValueAgainstType_Integer_AboveMaxRejects(t *testing.T) {
	obj := protocol.Object{Access: rw, Kind: protocol.KindInt, Min: int64(0), Max: int64(65535)}
	val := protocol.Value{Kind: protocol.KindInt, Int: 999999}
	if err := validateValueAgainstType(codec.TypeInteger, obj, val); !errors.Is(err, protocol.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed, got %v", err)
	}
}

func TestValidateValueAgainstType_Integer_InRangeAccepted(t *testing.T) {
	obj := protocol.Object{Access: rw, Kind: protocol.KindInt, Min: int64(0), Max: int64(65535)}
	val := protocol.Value{Kind: protocol.KindInt, Int: 12700}
	if err := validateValueAgainstType(codec.TypeInteger, obj, val); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestValidateValueAgainstType_ReadOnlyRejects(t *testing.T) {
	obj := protocol.Object{Access: ro, Kind: protocol.KindInt}
	val := protocol.Value{Kind: protocol.KindInt, Int: 10}
	if err := validateValueAgainstType(codec.TypeInteger, obj, val); !errors.Is(err, protocol.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed for read-only, got %v", err)
	}
}

func TestValidateValueAgainstType_Enum_OutOfOptionsRejects(t *testing.T) {
	obj := protocol.Object{Access: rw, Kind: protocol.KindEnum, EnumItems: []string{"Off", "On", "Auto"}}
	val := protocol.Value{Kind: protocol.KindEnum, Enum: 99, Str: "XYZNotAnOption"}
	if err := validateValueAgainstType(codec.TypeEnum, obj, val); !errors.Is(err, protocol.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed, got %v", err)
	}
}

func TestValidateValueAgainstType_Enum_KnownLabelAccepted(t *testing.T) {
	obj := protocol.Object{Access: rw, Kind: protocol.KindEnum, EnumItems: []string{"Off", "On", "Auto"}}
	val := protocol.Value{Kind: protocol.KindEnum, Str: "Auto"}
	if err := validateValueAgainstType(codec.TypeEnum, obj, val); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestValidateValueAgainstType_Enum_KnownIndexAccepted(t *testing.T) {
	obj := protocol.Object{Access: rw, Kind: protocol.KindEnum, EnumItems: []string{"Off", "On", "Auto"}}
	val := protocol.Value{Kind: protocol.KindEnum, Enum: 1}
	if err := validateValueAgainstType(codec.TypeEnum, obj, val); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestValidateValueAgainstType_IPAddr_MalformedRejects(t *testing.T) {
	obj := protocol.Object{Access: rw, Kind: protocol.KindIPAddr}
	val := protocol.Value{Kind: protocol.KindString, Str: "not.an.ip.really"}
	if err := validateValueAgainstType(codec.TypeIPAddr, obj, val); !errors.Is(err, protocol.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed, got %v", err)
	}
}

func TestValidateValueAgainstType_IPAddr_DottedQuadAccepted(t *testing.T) {
	obj := protocol.Object{Access: rw, Kind: protocol.KindIPAddr}
	val := protocol.Value{Kind: protocol.KindIPAddr, IPAddr: [4]byte{239, 129, 1, 20}}
	if err := validateValueAgainstType(codec.TypeIPAddr, obj, val); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestValidateValueAgainstType_String_AnyValueAccepted(t *testing.T) {
	obj := protocol.Object{Access: rw, Kind: protocol.KindString, MaxLen: 16}
	val := protocol.Value{Kind: protocol.KindString, Str: "RRS18-rack"}
	if err := validateValueAgainstType(codec.TypeString, obj, val); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestValidateValueAgainstType_String_TooLongRejects(t *testing.T) {
	obj := protocol.Object{Access: rw, Kind: protocol.KindString, MaxLen: 8}
	val := protocol.Value{Kind: protocol.KindString, Str: "this-is-way-too-long-for-eight"}
	if err := validateValueAgainstType(codec.TypeString, obj, val); !errors.Is(err, protocol.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed, got %v", err)
	}
}

func TestValidateValueAgainstType_Byte_OutOfRangeRejects(t *testing.T) {
	obj := protocol.Object{Access: rw, Kind: protocol.KindUint}
	val := protocol.Value{Kind: protocol.KindUint, Uint: 999}
	if err := validateValueAgainstType(codec.TypeByte, obj, val); !errors.Is(err, protocol.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed, got %v", err)
	}
}

func TestValidateValueAgainstType_Byte_NegativeRejects(t *testing.T) {
	obj := protocol.Object{Access: rw, Kind: protocol.KindUint}
	val := protocol.Value{Kind: protocol.KindInt, Int: -5}
	if err := validateValueAgainstType(codec.TypeByte, obj, val); !errors.Is(err, protocol.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed, got %v", err)
	}
}
