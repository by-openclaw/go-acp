package acp2

import (
	"errors"
	"testing"

	"dhs/internal/acp2/codec"
	"dhs/internal/protocol"
)

func TestValidateValueAgainstType_Number_UnknownKindRejects(t *testing.T) {
	val := protocol.Value{Kind: protocol.KindUnknown}
	err := validateValueAgainstType(codec.ObjTypeNumber, codec.NumTypeU16, nil, val, nil)
	if !errors.Is(err, protocol.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed, got %v", err)
	}
}

func TestValidateValueAgainstType_Number_StringForIntRejects(t *testing.T) {
	val := protocol.Value{Kind: protocol.KindString, Str: "not_a_number"}
	err := validateValueAgainstType(codec.ObjTypeNumber, codec.NumTypeU16, nil, val, nil)
	if !errors.Is(err, protocol.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed, got %v", err)
	}
}

func TestValidateValueAgainstType_Number_IntAccepted(t *testing.T) {
	val := protocol.Value{Kind: protocol.KindInt, Int: 12700}
	if err := validateValueAgainstType(codec.ObjTypeNumber, codec.NumTypeU16, nil, val, nil); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestValidateValueAgainstType_Enum_OutOfOptionsRejects(t *testing.T) {
	reverse := map[string]uint32{"Input": 0, "Output": 1, "Off": 2}
	val := protocol.Value{Kind: protocol.KindEnum, Enum: 99, Str: "XYZNotAnOption"}
	err := validateValueAgainstType(codec.ObjTypeEnum, codec.NumTypePreset, nil, val, reverse)
	if !errors.Is(err, protocol.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed, got %v", err)
	}
}

func TestValidateValueAgainstType_Enum_KnownLabelAccepted(t *testing.T) {
	reverse := map[string]uint32{"Input": 0, "Output": 1, "Off": 2}
	val := protocol.Value{Kind: protocol.KindEnum, Str: "Output"}
	if err := validateValueAgainstType(codec.ObjTypeEnum, codec.NumTypePreset, nil, val, reverse); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestValidateValueAgainstType_Enum_KnownIndexAccepted(t *testing.T) {
	reverse := map[string]uint32{"Input": 0, "Output": 1, "Off": 2}
	val := protocol.Value{Kind: protocol.KindEnum, Enum: 1}
	if err := validateValueAgainstType(codec.ObjTypeEnum, codec.NumTypePreset, nil, val, reverse); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestValidateValueAgainstType_IPv4_MalformedRejects(t *testing.T) {
	val := protocol.Value{Kind: protocol.KindString, Str: "not.an.ip.really"}
	err := validateValueAgainstType(codec.ObjTypeIPv4, codec.NumTypeIPv4, nil, val, nil)
	if !errors.Is(err, protocol.ErrValidationFailed) {
		t.Fatalf("want ErrValidationFailed, got %v", err)
	}
}

func TestValidateValueAgainstType_IPv4_DottedQuadAccepted(t *testing.T) {
	val := protocol.Value{Kind: protocol.KindIPAddr, Str: "239.129.1.20"}
	if err := validateValueAgainstType(codec.ObjTypeIPv4, codec.NumTypeIPv4, nil, val, nil); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestValidateValueAgainstType_String_AnyValueAccepted(t *testing.T) {
	val := protocol.Value{Kind: protocol.KindString, Str: "SDI 01"}
	if err := validateValueAgainstType(codec.ObjTypeString, codec.NumTypeString, nil, val, nil); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}
