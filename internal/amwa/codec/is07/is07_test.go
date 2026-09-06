package is07

import (
	"strings"
	"testing"
)

const (
	srcUUID  = "1f1e1d1c-1b1a-4019-8817-161514131211"
	flowUUID = "2f2e2d2c-2b2a-4029-8827-262524232221"
)

func validIdentity() Identity { return Identity{SourceID: srcUUID} }
func validTiming() Timing     { return Timing{CreationTimestamp: "100:200"} }

func TestCategoryOf(t *testing.T) {
	cases := map[string]EventCategory{
		"boolean":            EventCategoryBoolean,
		"boolean/tally":      EventCategoryBoolean,
		"boolean/tally/red":  EventCategoryBoolean,
		"number":             EventCategoryNumber,
		"number/temperature": EventCategoryNumber,
		"string":             EventCategoryString,
		"object":             EventCategoryObject,
		"unknown":            "",
		"":                   "",
		"boolean//double":    "",
		"number/space here":  "",
	}
	for ev, want := range cases {
		if got := CategoryOf(ev); got != want {
			t.Errorf("CategoryOf(%q) = %q, want %q", ev, got, want)
		}
	}
}

func TestEventBooleanRoundTrip(t *testing.T) {
	in := EventBoolean{
		EventCommon: EventCommon{
			Identity:  validIdentity(),
			Timing:    validTiming(),
			EventType: "boolean/tally",
		},
		Payload: PayloadBoolean{Value: true},
	}
	body, err := EncodeMessage(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := DecodeMessage(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got, ok := out.(EventBoolean)
	if !ok {
		t.Fatalf("decoded into %T, want EventBoolean", out)
	}
	if got.Payload.Value != true {
		t.Fatalf("payload mismatch")
	}
	if got.MessageType != MessageTypeState {
		t.Fatalf("message_type %q, want state", got.MessageType)
	}
}

func TestEventNumberRoundTrip(t *testing.T) {
	in := EventNumber{
		EventCommon: EventCommon{
			Identity:  validIdentity(),
			Timing:    validTiming(),
			EventType: "number/temperature",
		},
		Payload: Number{Value: 273, Scale: 100},
	}
	body, err := EncodeMessage(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := DecodeMessage(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got := out.(EventNumber)
	if got.Payload.Value != 273 || got.Payload.Scale != 100 {
		t.Fatalf("payload mismatch: %+v", got.Payload)
	}
}

func TestEventStringRoundTrip(t *testing.T) {
	in := EventString{
		EventCommon: EventCommon{
			Identity:  Identity{SourceID: srcUUID, FlowID: flowUUID},
			Timing:    validTiming(),
			EventType: "string/level/db",
		},
		Payload: PayloadString{Value: "+0dB"},
	}
	body, err := EncodeMessage(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := DecodeMessage(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got := out.(EventString)
	if got.Payload.Value != "+0dB" {
		t.Fatalf("payload mismatch")
	}
	if got.Identity.FlowID != flowUUID {
		t.Fatalf("flow_id mismatch")
	}
}

func TestEventObjectRoundTrip(t *testing.T) {
	in := EventObject{
		EventCommon: EventCommon{
			Identity:  validIdentity(),
			Timing:    validTiming(),
			EventType: "object/coords",
		},
		Payload: PayloadObject{"x": 1.0, "y": 2.0},
	}
	body, err := EncodeMessage(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := DecodeMessage(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got := out.(EventObject)
	if got.Payload["x"] != 1.0 {
		t.Fatalf("payload mismatch: %+v", got.Payload)
	}
}

func TestMessageHealthRoundTrip(t *testing.T) {
	in := MessageHealth{
		Timing: Timing{CreationTimestamp: "10:20", OriginTimestamp: "9:0"},
	}
	body, err := EncodeMessage(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := DecodeMessage(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got := out.(MessageHealth)
	if got.Timing.OriginTimestamp != "9:0" {
		t.Fatalf("origin_timestamp mismatch")
	}
}

func TestMessageHealthRequiresOrigin(t *testing.T) {
	in := MessageHealth{Timing: Timing{CreationTimestamp: "10:20"}}
	if _, err := EncodeMessage(in); err == nil {
		t.Fatalf("expected error: missing origin_timestamp")
	}
}

func TestMessageShutdownRebootRoundTrip(t *testing.T) {
	for _, mt := range []MessageType{MessageTypeReboot, MessageTypeShutdown} {
		in := MessageShutdownReboot{
			MessageType: mt,
			Identity:    validIdentity(),
			Timing:      validTiming(),
		}
		body, err := EncodeMessage(in)
		if err != nil {
			t.Fatalf("Encode %s: %v", mt, err)
		}
		out, err := DecodeMessage(body)
		if err != nil {
			t.Fatalf("Decode %s: %v", mt, err)
		}
		got := out.(MessageShutdownReboot)
		if got.MessageType != mt {
			t.Fatalf("message_type mismatch: got %q want %q", got.MessageType, mt)
		}
	}
}

func TestMessageShutdownRebootRejectsBlankMessageType(t *testing.T) {
	in := MessageShutdownReboot{
		Identity: validIdentity(),
		Timing:   validTiming(),
	}
	if _, err := EncodeMessage(in); err == nil {
		t.Fatalf("expected error on blank message_type")
	}
}

func TestMessageConnectionStatusRoundTrip(t *testing.T) {
	in := MessageConnectionStatus{Active: true}
	body, err := EncodeMessage(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := DecodeMessage(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got := out.(MessageConnectionStatus)
	if !got.Active {
		t.Fatalf("active mismatch")
	}
}

func TestDecodeMessageUnknownType(t *testing.T) {
	body := []byte(`{"message_type":"unrecognised"}`)
	if _, err := DecodeMessage(body); err == nil {
		t.Fatalf("expected error on unknown message_type")
	} else if !strings.Contains(err.Error(), "unrecognised") {
		t.Fatalf("error should name discriminator: %v", err)
	}
}

func TestDecodeMessageMissingType(t *testing.T) {
	body := []byte(`{}`)
	if _, err := DecodeMessage(body); err == nil {
		t.Fatalf("expected error on missing message_type")
	}
}

func TestDecodeMessageRejectsUnknownField(t *testing.T) {
	body := []byte(`{"message_type":"health","timing":{"creation_timestamp":"1:2","origin_timestamp":"1:1"},"future_field":42}`)
	if _, err := DecodeMessage(body); err == nil {
		t.Fatalf("expected DisallowUnknownFields rejection")
	} else if !strings.Contains(err.Error(), "future_field") {
		t.Fatalf("error should name unknown field: %v", err)
	}
}

func TestEventTypeMustMatchPrefix(t *testing.T) {
	in := EventBoolean{
		EventCommon: EventCommon{
			Identity:  validIdentity(),
			Timing:    validTiming(),
			EventType: "string/wrong",
		},
	}
	if _, err := EncodeMessage(in); err == nil {
		t.Fatalf("expected error on category mismatch")
	}
}

func TestIdentityRequiresUUID(t *testing.T) {
	in := EventBoolean{
		EventCommon: EventCommon{
			Identity:  Identity{SourceID: "not-a-uuid"},
			Timing:    validTiming(),
			EventType: "boolean",
		},
	}
	if _, err := EncodeMessage(in); err == nil {
		t.Fatalf("expected UUID validation")
	}
}

func TestTimingRequiresCreation(t *testing.T) {
	in := EventBoolean{
		EventCommon: EventCommon{
			Identity:  validIdentity(),
			EventType: "boolean",
		},
	}
	if _, err := EncodeMessage(in); err == nil {
		t.Fatalf("expected creation_timestamp validation")
	}
}

func TestTimingRejectsBadTAI(t *testing.T) {
	in := EventBoolean{
		EventCommon: EventCommon{
			Identity:  validIdentity(),
			Timing:    Timing{CreationTimestamp: "not-tai"},
			EventType: "boolean",
		},
	}
	if _, err := EncodeMessage(in); err == nil {
		t.Fatalf("expected TAI grammar validation")
	}
}

func TestCommandHealthRoundTrip(t *testing.T) {
	in := CommandHealth{Timestamp: "12:34"}
	body, err := EncodeCommand(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := DecodeCommand(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got := out.(CommandHealth)
	if got.Command != CommandTypeHealth {
		t.Fatalf("command discriminator missing")
	}
	if got.Timestamp != "12:34" {
		t.Fatalf("timestamp mismatch")
	}
}

func TestCommandSubscriptionRoundTrip(t *testing.T) {
	in := CommandSubscription{Sources: []string{srcUUID, flowUUID}}
	body, err := EncodeCommand(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := DecodeCommand(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got := out.(CommandSubscription)
	if len(got.Sources) != 2 {
		t.Fatalf("sources length mismatch")
	}
}

func TestCommandSubscriptionRejectsDuplicates(t *testing.T) {
	in := CommandSubscription{Sources: []string{srcUUID, srcUUID}}
	if _, err := EncodeCommand(in); err == nil {
		t.Fatalf("expected dup rejection")
	}
}

func TestCommandSubscriptionAcceptsEmpty(t *testing.T) {
	in := CommandSubscription{Sources: []string{}}
	if _, err := EncodeCommand(in); err != nil {
		t.Fatalf("empty array should be valid: %v", err)
	}
}

func TestCommandSubscriptionRequiresSources(t *testing.T) {
	in := CommandSubscription{}
	if _, err := EncodeCommand(in); err == nil {
		t.Fatalf("expected required sources")
	}
}

func TestDecodeCommandUnknownType(t *testing.T) {
	body := []byte(`{"command":"reset"}`)
	if _, err := DecodeCommand(body); err == nil {
		t.Fatalf("expected error on unknown command")
	}
}

func TestEventBooleanKind(t *testing.T) {
	if (EventBoolean{}).Kind() != MessageTypeState {
		t.Fatal("EventBoolean.Kind() should be state")
	}
	if (CommandHealth{}).Kind() != CommandTypeHealth {
		t.Fatal("CommandHealth.Kind() should be health")
	}
}

func TestEventNumberRejectsBadScale(t *testing.T) {
	in := EventNumber{
		EventCommon: EventCommon{
			Identity:  validIdentity(),
			Timing:    validTiming(),
			EventType: "number",
		},
		Payload: Number{Value: 0, Scale: 0},
	}
	// scale=0 is OK (omitempty default)
	if _, err := EncodeMessage(in); err != nil {
		t.Fatalf("scale=0 should be valid: %v", err)
	}
	in.Payload.Scale = -1
	if _, err := EncodeMessage(in); err == nil {
		t.Fatalf("scale<1 should be rejected")
	}
}

func TestValidateTypeBoolean(t *testing.T) {
	if err := ValidateTypeBoolean(TypeBoolean{Type: TypeBaseNameBoolean}); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if err := ValidateTypeBoolean(TypeBoolean{Type: "string"}); err == nil {
		t.Fatalf("expected error")
	}
}

func TestValidateTypeBooleanEnum(t *testing.T) {
	good := TypeBooleanEnum{
		Type: TypeBaseNameBoolean,
		Values: []EnumValueBoolean{
			{Value: false, Label: "off", Description: "Inactive"},
			{Value: true, Label: "on", Description: "Active"},
		},
	}
	if err := ValidateTypeBooleanEnum(good); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if err := ValidateTypeBooleanEnum(TypeBooleanEnum{Type: TypeBaseNameBoolean}); err == nil {
		t.Fatalf("empty values should error")
	}
}

func TestValidateTypeNumber(t *testing.T) {
	good := TypeNumber{Type: TypeBaseNameNumber, Min: Number{Value: 0}, Max: Number{Value: 100}, Unit: "celsius"}
	if err := ValidateTypeNumber(good); err != nil {
		t.Fatalf("valid: %v", err)
	}
}

func TestValidateTypeStringEnum(t *testing.T) {
	good := TypeStringEnum{
		Type: TypeBaseNameString,
		Values: []EnumValueString{
			{Value: "off", Label: "off", Description: "Off"},
			{Value: "on", Label: "on", Description: "On"},
		},
	}
	if err := ValidateTypeStringEnum(good); err != nil {
		t.Fatalf("valid: %v", err)
	}
}
