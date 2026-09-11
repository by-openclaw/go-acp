package is07

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/spec"
)

const (
	covSourceID = "11111111-1111-4111-8111-111111111111"
	covFlowID   = "22222222-2222-4222-8222-222222222222"
)

func covCommon(eventType string) EventCommon {
	return EventCommon{
		MessageType: MessageTypeState,
		Identity:    Identity{SourceID: covSourceID, FlowID: covFlowID},
		Timing:      Timing{CreationTimestamp: "1600000000:0"},
		EventType:   eventType,
	}
}

func intPtr(i int) *int { return &i }

// Every message variant reports the discriminator it goes out under,
// whatever the struct was built with — the encoders rely on it for
// the wire form, so a blank field must not produce a blank frame.
func TestMessageKinds(t *testing.T) {
	for name, tc := range map[string]struct {
		msg  Message
		want MessageType
	}{
		"a boolean event":     {EventBoolean{}, MessageTypeState},
		"a number event":      {EventNumber{}, MessageTypeState},
		"a string event":      {EventString{}, MessageTypeState},
		"an object event":     {EventObject{}, MessageTypeState},
		"a health message":    {MessageHealth{}, MessageTypeHealth},
		"a reboot message":    {MessageShutdownReboot{MessageType: MessageTypeReboot}, MessageTypeReboot},
		"a shutdown message":  {MessageShutdownReboot{MessageType: MessageTypeShutdown}, MessageTypeShutdown},
		"a connection status": {MessageConnectionStatus{}, MessageTypeConnectionStatus},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tc.msg.Kind(); got != tc.want {
				t.Errorf("Kind = %q, want %q", got, tc.want)
			}
		})
	}

	if got := (CommandHealth{}).Kind(); got != CommandTypeHealth {
		t.Errorf("CommandHealth.Kind = %q", got)
	}
	if got := (CommandSubscription{}).Kind(); got != CommandTypeSubscription {
		t.Errorf("CommandSubscription.Kind = %q", got)
	}
}

// The identity block names the IS-04 source the event came from, and
// optionally the flow carrying it. Both are UUIDs when present.
func TestValidateIdentityAndTiming(t *testing.T) {
	for name, tc := range map[string]struct {
		id   Identity
		want string
	}{
		"no source":                   {Identity{}, "required"},
		"a source that is not a UUID": {Identity{SourceID: "src-1"}, "not a UUID"},
		"a flow that is not a UUID": {
			Identity{SourceID: covSourceID, FlowID: "flow-1"}, "flow_id",
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateIdentity(tc.id)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("= %v, want an error mentioning %q", err, tc.want)
			}
		})
	}
	if err := validateIdentity(Identity{SourceID: covSourceID}); err != nil {
		t.Errorf("a source alone = %v", err)
	}

	if err := validateTiming(Timing{CreationTimestamp: "1:0", OriginTimestamp: "2:0", ActionTimestamp: "3:0"}, true); err != nil {
		t.Errorf("a fully-timed event = %v", err)
	}
	for name, tc := range map[string]struct {
		timing        Timing
		requireOrigin bool
		want          string
	}{
		"no creation timestamp":                {Timing{}, false, "creation_timestamp: required"},
		"a creation timestamp that is not TAI": {Timing{CreationTimestamp: "noon"}, false, "creation_timestamp"},
		"a required origin that is absent":     {Timing{CreationTimestamp: "1:0"}, true, "origin_timestamp: required"},
		"an origin that is not TAI": {
			Timing{CreationTimestamp: "1:0", OriginTimestamp: "noon"}, false, "origin_timestamp",
		},
		"an action timestamp that is not TAI": {
			Timing{CreationTimestamp: "1:0", ActionTimestamp: "noon"}, false, "action_timestamp",
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateTiming(tc.timing, tc.requireOrigin)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("= %v, want an error mentioning %q", err, tc.want)
			}
		})
	}
}

// An event's category comes from its event_type prefix, and the
// payload variant must agree with it — a `number/...` event decoded
// as a boolean would silently read the wrong field.
func TestCategoryOfAndEventValidation(t *testing.T) {
	for eventType, want := range map[string]EventCategory{
		"boolean":            EventCategoryBoolean,
		"boolean/gpi/0":      EventCategoryBoolean,
		"number":             EventCategoryNumber,
		"number/temperature": EventCategoryNumber,
		"string":             EventCategoryString,
		"object":             EventCategoryObject,
		"object/vendor/x":    EventCategoryObject,
		"measurement":        "",
		"":                   "",
		"number/ ":           "",
	} {
		if got := CategoryOf(eventType); got != want {
			t.Errorf("CategoryOf(%q) = %q, want %q", eventType, got, want)
		}
	}

	// The shared event_core rules.
	e := EventBoolean{EventCommon: covCommon("boolean")}
	if err := e.validate(); err != nil {
		t.Errorf("a valid boolean event = %v", err)
	}
	wrongKind := e
	wrongKind.MessageType = MessageTypeHealth
	if err := wrongKind.validate(); err == nil || !strings.Contains(err.Error(), "message_type") {
		t.Errorf("an event carrying another message_type = %v", err)
	}
	noSource := e
	noSource.Identity = Identity{}
	if err := noSource.validate(); err == nil {
		t.Error("an event with no source must be refused")
	}
	noTiming := e
	noTiming.Timing = Timing{}
	if err := noTiming.validate(); err == nil {
		t.Error("an event with no creation timestamp must be refused")
	}
	noType := e
	noType.EventType = ""
	if err := noType.validate(); err == nil || !strings.Contains(err.Error(), "event_type: required") {
		t.Errorf("an event with no event_type = %v", err)
	}
	crossed := EventBoolean{EventCommon: covCommon("number/temperature")}
	if err := crossed.validate(); err == nil || !strings.Contains(err.Error(), "expected category") {
		t.Errorf("a boolean event carrying a number event_type = %v", err)
	}

	// The per-variant extras.
	num := EventNumber{EventCommon: covCommon("number"), Payload: Number{Value: 1, Scale: 10}}
	if err := num.validate(); err != nil {
		t.Errorf("a scaled number event = %v", err)
	}
	num.Payload.Scale = -1
	if err := num.validate(); err == nil || !strings.Contains(err.Error(), "scale") {
		t.Errorf("a negative scale = %v", err)
	}
	if err := (EventNumber{EventCommon: covCommon("boolean")}).validate(); err == nil {
		t.Error("a number event carrying a boolean event_type must be refused")
	}

	str := EventString{EventCommon: covCommon("string"), Payload: PayloadString{Value: "x"}}
	if err := str.validate(); err != nil {
		t.Errorf("a string event = %v", err)
	}

	obj := EventObject{EventCommon: covCommon("object"), Payload: PayloadObject{}}
	if err := obj.validate(); err != nil {
		t.Errorf("an object event with an empty payload = %v", err)
	}
	if err := (EventObject{EventCommon: covCommon("object")}).validate(); err == nil ||
		!strings.Contains(err.Error(), "payload") {
		t.Error("an object event with no payload must be refused")
	}
	if err := (EventObject{EventCommon: covCommon("string")}).validate(); err == nil {
		t.Error("an object event carrying a string event_type must be refused")
	}
}

// The three non-event envelopes carry their own rules: health needs
// an origin timestamp, shutdown/reboot names which of the two it is,
// and connection_status refuses another type's name.
func TestValidateNonEventMessages(t *testing.T) {
	health := MessageHealth{
		MessageType: MessageTypeHealth,
		Timing:      Timing{CreationTimestamp: "1:0", OriginTimestamp: "1:0"},
	}
	if err := health.validate(); err != nil {
		t.Errorf("a valid health message = %v", err)
	}
	if err := (MessageHealth{Timing: health.Timing}).validate(); err != nil {
		t.Errorf("a health message with a blank type = %v", err)
	}
	wrong := health
	wrong.MessageType = MessageTypeState
	if err := wrong.validate(); err == nil || !strings.Contains(err.Error(), "message_type") {
		t.Errorf("a health message typed as state = %v", err)
	}
	noOrigin := health
	noOrigin.Timing.OriginTimestamp = ""
	if err := noOrigin.validate(); err == nil {
		t.Error("a health message without an origin timestamp must be refused")
	}

	sd := MessageShutdownReboot{
		MessageType: MessageTypeShutdown,
		Identity:    Identity{SourceID: covSourceID},
		Timing:      Timing{CreationTimestamp: "1:0"},
	}
	if err := sd.validate(); err != nil {
		t.Errorf("a valid shutdown = %v", err)
	}
	if err := (MessageShutdownReboot{MessageType: MessageTypeState}).validate(); err == nil ||
		!strings.Contains(err.Error(), "reboot or shutdown") {
		t.Error("shutdown_reboot must name one of its two types")
	}
	noID := sd
	noID.Identity = Identity{}
	if err := noID.validate(); err == nil {
		t.Error("a shutdown with no identity must be refused")
	}
	badTiming := sd
	badTiming.Timing = Timing{CreationTimestamp: "noon"}
	if err := badTiming.validate(); err == nil {
		t.Error("a shutdown with a non-TAI timestamp must be refused")
	}

	if err := (MessageConnectionStatus{Active: true}).validate(); err != nil {
		t.Errorf("a connection status with a blank type = %v", err)
	}
	if err := (MessageConnectionStatus{MessageType: MessageTypeConnectionStatus}).validate(); err != nil {
		t.Errorf("a valid connection status = %v", err)
	}
	if err := (MessageConnectionStatus{MessageType: MessageTypeHealth}).validate(); err == nil {
		t.Error("a connection status typed as health must be refused")
	}
}

// A subscription names the sources the receiver wants, each of them
// once — a duplicate is a receiver bug the sender should not have to
// reconcile.
func TestValidateCommands(t *testing.T) {
	if err := (CommandHealth{Command: CommandTypeHealth, Timestamp: "1:0"}).validate(); err != nil {
		t.Errorf("a valid health command = %v", err)
	}
	if err := (CommandHealth{Timestamp: "1:0"}).validate(); err != nil {
		t.Errorf("a health command with a blank command = %v", err)
	}
	if err := (CommandHealth{Command: CommandTypeSubscription, Timestamp: "1:0"}).validate(); err == nil {
		t.Error("a health command named subscription must be refused")
	}
	if err := (CommandHealth{}).validate(); err == nil || !strings.Contains(err.Error(), "required") {
		t.Error("a health command with no timestamp must be refused")
	}
	if err := (CommandHealth{Timestamp: "noon"}).validate(); err == nil ||
		!strings.Contains(err.Error(), "TAI") {
		t.Error("a health command with a non-TAI timestamp must be refused")
	}

	if err := (CommandSubscription{Sources: []string{}}).validate(); err != nil {
		t.Errorf("an empty subscription = %v", err)
	}
	if err := (CommandSubscription{
		Command: CommandTypeSubscription, Sources: []string{covSourceID, covFlowID},
	}).validate(); err != nil {
		t.Errorf("a valid subscription = %v", err)
	}
	if err := (CommandSubscription{Command: CommandTypeHealth, Sources: []string{}}).validate(); err == nil {
		t.Error("a subscription named health must be refused")
	}
	if err := (CommandSubscription{}).validate(); err == nil || !strings.Contains(err.Error(), "required") {
		t.Error("a subscription with no sources array must be refused")
	}
	if err := (CommandSubscription{Sources: []string{"src-1"}}).validate(); err == nil ||
		!strings.Contains(err.Error(), "not a UUID") {
		t.Error("a source that is not a UUID must be refused")
	}
	if err := (CommandSubscription{Sources: []string{covSourceID, covSourceID}}).validate(); err == nil ||
		!strings.Contains(err.Error(), "duplicate") {
		t.Error("a duplicated source must be refused")
	}
}

// Each type descriptor names its base type and carries the rules its
// schema attaches to it.
func TestValidateTypeDescriptors(t *testing.T) {
	if err := ValidateTypeBoolean(TypeBoolean{Type: TypeBaseNameBoolean}); err != nil {
		t.Errorf("type_boolean = %v", err)
	}
	if err := ValidateTypeBoolean(TypeBoolean{Type: TypeBaseNameNumber}); err == nil {
		t.Error("type_boolean must name the boolean base type")
	}

	boolEnum := TypeBooleanEnum{
		Type:   TypeBaseNameBoolean,
		Values: []EnumValueBoolean{{Value: true, Label: "on", Description: "closed"}},
	}
	if err := ValidateTypeBooleanEnum(boolEnum); err != nil {
		t.Errorf("type_boolean_enum = %v", err)
	}
	if err := ValidateTypeBooleanEnum(TypeBooleanEnum{Type: TypeBaseNameString}); err == nil {
		t.Error("type_boolean_enum must name the boolean base type")
	}
	if err := ValidateTypeBooleanEnum(TypeBooleanEnum{Type: TypeBaseNameBoolean}); err == nil {
		t.Error("an enum with no values must be refused")
	}
	noLabel := boolEnum
	noLabel.Values = []EnumValueBoolean{{Description: "d"}}
	if err := ValidateTypeBooleanEnum(noLabel); err == nil {
		t.Error("an enum entry with no label must be refused")
	}
	noDesc := boolEnum
	noDesc.Values = []EnumValueBoolean{{Label: "l"}}
	if err := ValidateTypeBooleanEnum(noDesc); err == nil {
		t.Error("an enum entry with no description must be refused")
	}

	num := TypeNumber{
		Type: TypeBaseNameNumber, Min: Number{Value: 0}, Max: Number{Value: 100, Scale: 1},
		Scale: 1, Step: &Number{Value: 1}, Unit: "celsius",
	}
	if err := ValidateTypeNumber(num); err != nil {
		t.Errorf("type_number = %v", err)
	}
	if err := ValidateTypeNumber(TypeNumber{Type: TypeBaseNameString}); err == nil {
		t.Error("type_number must name the number base type")
	}
	for name, bad := range map[string]TypeNumber{
		"a descriptor scale below 1": {Type: TypeBaseNameNumber, Scale: -1},
		"a min scale below 1":        {Type: TypeBaseNameNumber, Min: Number{Scale: -1}},
		"a max scale below 1":        {Type: TypeBaseNameNumber, Max: Number{Scale: -1}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateTypeNumber(bad); err == nil {
				t.Error("was accepted")
			}
		})
	}

	numEnum := TypeNumberEnum{
		Type:   TypeBaseNameNumber,
		Values: []EnumValueNumber{{Value: Number{Value: 1}, Label: "one", Description: "the first"}},
	}
	if err := ValidateTypeNumberEnum(numEnum); err != nil {
		t.Errorf("type_number_enum = %v", err)
	}
	if err := ValidateTypeNumberEnum(TypeNumberEnum{Type: TypeBaseNameBoolean}); err == nil {
		t.Error("type_number_enum must name the number base type")
	}
	if err := ValidateTypeNumberEnum(TypeNumberEnum{Type: TypeBaseNameNumber}); err == nil {
		t.Error("an enum with no values must be refused")
	}
	if err := ValidateTypeNumberEnum(TypeNumberEnum{
		Type: TypeBaseNameNumber, Values: []EnumValueNumber{{Description: "d"}},
	}); err == nil {
		t.Error("an enum entry with no label must be refused")
	}
	if err := ValidateTypeNumberEnum(TypeNumberEnum{
		Type: TypeBaseNameNumber, Values: []EnumValueNumber{{Label: "l"}},
	}); err == nil {
		t.Error("an enum entry with no description must be refused")
	}

	if err := ValidateTypeString(TypeString{
		Type: TypeBaseNameString, MinLength: intPtr(0), MaxLength: intPtr(32), Pattern: "^[a-z]+$",
	}); err != nil {
		t.Errorf("type_string = %v", err)
	}
	if err := ValidateTypeString(TypeString{Type: TypeBaseNameString}); err != nil {
		t.Errorf("type_string with no bounds = %v", err)
	}
	if err := ValidateTypeString(TypeString{Type: TypeBaseNameNumber}); err == nil {
		t.Error("type_string must name the string base type")
	}
	if err := ValidateTypeString(TypeString{Type: TypeBaseNameString, MinLength: intPtr(-1)}); err == nil {
		t.Error("a negative min_length must be refused")
	}
	if err := ValidateTypeString(TypeString{Type: TypeBaseNameString, MaxLength: intPtr(0)}); err == nil {
		t.Error("a max_length of 0 must be refused")
	}

	strEnum := TypeStringEnum{
		Type:   TypeBaseNameString,
		Values: []EnumValueString{{Value: "a", Label: "A", Description: "the letter"}},
	}
	if err := ValidateTypeStringEnum(strEnum); err != nil {
		t.Errorf("type_string_enum = %v", err)
	}
	if err := ValidateTypeStringEnum(TypeStringEnum{Type: TypeBaseNameNumber}); err == nil {
		t.Error("type_string_enum must name the string base type")
	}
	if err := ValidateTypeStringEnum(TypeStringEnum{Type: TypeBaseNameString}); err == nil {
		t.Error("an enum with no values must be refused")
	}
	if err := ValidateTypeStringEnum(TypeStringEnum{
		Type: TypeBaseNameString, Values: []EnumValueString{{Description: "d"}},
	}); err == nil {
		t.Error("an enum entry with no label must be refused")
	}
	if err := ValidateTypeStringEnum(TypeStringEnum{
		Type: TypeBaseNameString, Values: []EnumValueString{{Label: "l"}},
	}); err == nil {
		t.Error("an enum entry with no description must be refused")
	}
}

// Every variant round-trips, and the encoder fills the discriminator
// so a struct built without one still goes out as a legal frame.
func TestEncodeDecodeEveryVariant(t *testing.T) {
	for name, msg := range map[string]Message{
		"a boolean event": EventBoolean{EventCommon: covCommon("boolean/gpi/0"), Payload: PayloadBoolean{Value: true}},
		"a number event":  EventNumber{EventCommon: covCommon("number/temp"), Payload: Number{Value: 21, Scale: 1}},
		"a string event":  EventString{EventCommon: covCommon("string/label"), Payload: PayloadString{Value: "x"}},
		"an object event": EventObject{EventCommon: covCommon("object/vendor"), Payload: PayloadObject{"k": "v"}},
		"a health message": MessageHealth{
			Timing: Timing{CreationTimestamp: "1:0", OriginTimestamp: "1:0"},
		},
		"a reboot message": MessageShutdownReboot{
			MessageType: MessageTypeReboot,
			Identity:    Identity{SourceID: covSourceID},
			Timing:      Timing{CreationTimestamp: "1:0"},
		},
		"a connection status": MessageConnectionStatus{Active: true},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := EncodeMessage(msg)
			if err != nil {
				t.Fatalf("EncodeMessage: %v", err)
			}
			back, err := DecodeMessage(raw)
			if err != nil {
				t.Fatalf("DecodeMessage: %v (%s)", err, raw)
			}
			if back.Kind() != msg.Kind() {
				t.Errorf("round-tripped kind = %q, want %q", back.Kind(), msg.Kind())
			}
		})
	}

	for name, cmd := range map[string]Command{
		"a health command":       CommandHealth{Timestamp: "1:0"},
		"a subscription command": CommandSubscription{Sources: []string{covSourceID}},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := EncodeCommand(cmd)
			if err != nil {
				t.Fatalf("EncodeCommand: %v", err)
			}
			back, err := DecodeCommand(raw)
			if err != nil {
				t.Fatalf("DecodeCommand: %v (%s)", err, raw)
			}
			if back.Kind() != cmd.Kind() {
				t.Errorf("round-tripped kind = %q, want %q", back.Kind(), cmd.Kind())
			}
		})
	}
}

// Encoding refuses what it cannot ship: a nil variant, a document
// that does not validate, and a type outside the closed sum.
func TestEncodeRefusals(t *testing.T) {
	if _, err := EncodeMessage(nil); err == nil {
		t.Error("EncodeMessage(nil) was accepted")
	}
	if _, err := EncodeCommand(nil); err == nil {
		t.Error("EncodeCommand(nil) was accepted")
	}
	if _, err := EncodeMessage(EventBoolean{}); err == nil {
		t.Error("an event with no identity was encoded")
	}
	if _, err := EncodeCommand(CommandHealth{}); err == nil {
		t.Error("a health command with no timestamp was encoded")
	}
	if _, err := marshalNormalised(struct{ X int }{}); err == nil ||
		!strings.Contains(err.Error(), "unsupported variant") {
		t.Error("a type outside the sum must be refused")
	}
}

// The wire decoders switch on the discriminator, and say which one
// they could not act on — a receiver reports a compliance event
// rather than guessing.
func TestDecodeRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		raw  string
		want string
	}{
		"a frame that is not JSON":      {`{`, "peek message_type"},
		"no message_type":               {`{"identity":{}}`, "message_type: required"},
		"a message_type we do not know": {`{"message_type":"telemetry"}`, "unknown"},
		"a state frame that is not JSON past the peek": {
			`{"message_type":"state","event_type":1}`, "event_type",
		},
		"an event_type in no category": {
			`{"message_type":"state","event_type":"measurement"}`, "must start with",
		},
		"an unknown field on an event": {
			`{"message_type":"state","event_type":"boolean","identity":{"source_id":"` + covSourceID +
				`"},"timing":{"creation_timestamp":"1:0"},"payload":{"value":true},"extra":1}`, "decode",
		},
		"an event that does not validate": {
			`{"message_type":"state","event_type":"boolean","identity":{"source_id":"nope"},
			  "timing":{"creation_timestamp":"1:0"},"payload":{"value":true}}`, "not a UUID",
		},
		"a health frame with an unknown field": {
			`{"message_type":"health","timing":{"creation_timestamp":"1:0","origin_timestamp":"1:0"},"x":1}`, "decode",
		},
		"a health frame that does not validate": {
			`{"message_type":"health","timing":{"creation_timestamp":"1:0"}}`, "origin_timestamp",
		},
		"a reboot frame with an unknown field": {
			`{"message_type":"reboot","identity":{"source_id":"` + covSourceID +
				`"},"timing":{"creation_timestamp":"1:0"},"x":1}`, "decode",
		},
		"a reboot frame that does not validate": {
			`{"message_type":"reboot","identity":{},"timing":{"creation_timestamp":"1:0"}}`, "source_id",
		},
		"a connection status with an unknown field": {
			`{"message_type":"connection_status","active":true,"x":1}`, "decode",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeMessage([]byte(tc.raw))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("= %v, want an error mentioning %q", err, tc.want)
			}
		})
	}

	// Every event category decodes on its own path, so each one's
	// strict-decode and validation arms are reachable.
	for _, category := range []string{"number", "string", "object"} {
		raw := `{"message_type":"state","event_type":"` + category + `","identity":{"source_id":"` +
			covSourceID + `"},"timing":{"creation_timestamp":"1:0"},"payload":{},"x":1}`
		if _, err := DecodeMessage([]byte(raw)); err == nil {
			t.Errorf("an unknown field on a %s event was accepted", category)
		}
		bad := `{"message_type":"state","event_type":"` + category +
			`","identity":{"source_id":"nope"},"timing":{"creation_timestamp":"1:0"},"payload":{}}`
		if _, err := DecodeMessage([]byte(bad)); err == nil {
			t.Errorf("an invalid %s event was accepted", category)
		}
	}

	for name, tc := range map[string]struct {
		raw  string
		want string
	}{
		"a frame that is not JSON":                {`{`, "peek command"},
		"no command":                              {`{"timestamp":"1:0"}`, "command: required"},
		"a command we do not know":                {`{"command":"reset"}`, "unknown"},
		"an unknown field on health":              {`{"command":"health","timestamp":"1:0","x":1}`, "decode"},
		"a health command that does not validate": {`{"command":"health"}`, "timestamp"},
		"an unknown field on a subscription": {
			`{"command":"subscription","sources":[],"x":1}`, "decode",
		},
		"a subscription that does not validate": {
			`{"command":"subscription","sources":["nope"]}`, "not a UUID",
		},
	} {
		t.Run("command: "+name, func(t *testing.T) {
			_, err := DecodeCommand([]byte(tc.raw))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("= %v, want an error mentioning %q", err, tc.want)
			}
		})
	}

	// Trailing content past a complete frame is refused — one frame
	// per message, per the transport — and the strict decode is what
	// names it, so the fault reads as what it is rather than as a
	// failed peek.
	twoFrames := `{"command":"health","timestamp":"1:0"} {"command":"health","timestamp":"1:0"}`
	if _, err := DecodeCommand([]byte(twoFrames)); err == nil ||
		!strings.Contains(err.Error(), "trailing JSON") {
		t.Errorf("two frames in one payload = %v", err)
	}
	if _, err := DecodeMessage([]byte(
		`{"message_type":"connection_status","active":true} {"message_type":"health"}`)); err == nil ||
		!strings.Contains(err.Error(), "trailing JSON") {
		t.Errorf("two message frames in one payload = %v", err)
	}
}

// stubCodec is a second IS-07 codec used to exercise the registry
// without disturbing the v1.0 one the process registers.
type stubCodec struct{ ver string }

func (c stubCodec) SpecID() string    { return SpecID }
func (c stubCodec) APIVer() string    { return c.ver }
func (c stubCodec) SpecPatch() string { return c.ver + ".0" }

func (stubCodec) EncodeMessage(m Message) ([]byte, error)   { return EncodeMessage(m) }
func (stubCodec) DecodeMessage(raw []byte) (Message, error) { return DecodeMessage(raw) }
func (stubCodec) EncodeCommand(c Command) ([]byte, error)   { return EncodeCommand(c) }
func (stubCodec) DecodeCommand(raw []byte) (Command, error) { return DecodeCommand(raw) }

var _ Codec = stubCodec{}

type wrongSpec struct{ stubCodec }

func (wrongSpec) SpecID() string { return "is-05" }

// The per-minor registry answers by version, negotiates the highest
// mutual minor, and refuses a codec registered under another spec.
func TestCodecRegistry(t *testing.T) {
	Register(stubCodec{ver: "v1.1"})
	Register(stubCodec{ver: "v1.1"}) // idempotent

	if c, ok := Get("v1.1"); !ok || c.APIVer() != "v1.1" {
		t.Errorf("Get(v1.1) = %v, %v", c, ok)
	}
	if _, ok := Get("v9.9"); ok {
		t.Error("Get answered for a minor nobody registered")
	}
	if len(AllCodecs()) == 0 {
		t.Error("AllCodecs must list what is registered")
	}
	vers := SupportedVersions()
	if len(vers) == 0 {
		t.Fatal("SupportedVersions must list what is registered")
	}
	if Default().APIVer() != vers[len(vers)-1] {
		t.Errorf("Default = %q, want the newest minor", Default().APIVer())
	}
	if c, err := SelectHighest([]string{"v1.1", "v9.9"}); err != nil || c.APIVer() != "v1.1" {
		t.Errorf("SelectHighest = %v, %v", c, err)
	}
	if _, err := SelectHighest([]string{"v9.9"}); err == nil {
		t.Error("a peer with no mutual minor must be reported")
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Error("registering a codec for another spec must panic")
			}
		}()
		Register(wrongSpec{})
	}()

	prev := versions
	versions = spec.NewRegistry[Codec]()
	defer func() {
		versions = prev
		if r := recover(); r == nil {
			t.Error("Default with no codec registered must panic")
		}
	}()
	_ = Default()
}
