package is12

import (
	"encoding/json"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/spec"
)

// Every frame reports the discriminator it goes out under, whatever
// the struct was built with — Encode normalises the field from it, so
// a zero-valued struct must not ship as a Command by accident.
func TestMessageKinds(t *testing.T) {
	for want, msg := range map[MessageType]Message{
		MessageTypeCommand:              CommandMessage{},
		MessageTypeCommandResponse:      CommandResponseMessage{},
		MessageTypeNotification:         NotificationMessage{},
		MessageTypeSubscription:         SubscriptionMessage{},
		MessageTypeSubscriptionResponse: SubscriptionResponseMessage{},
		MessageTypeError:                ErrorMessage{},
	} {
		if got := msg.Kind(); got != want {
			t.Errorf("%T.Kind = %d, want %d", msg, got, want)
		}
	}
}

// oneCommand is a minimal well-formed command frame.
func oneCommand() CommandMessage {
	return CommandMessage{Commands: []Command{{
		Handle: 1, OID: 1, MethodID: MethodID{Level: 1, Index: 1},
		Arguments: json.RawMessage(`{"id":{"level":1,"index":1}}`),
	}}}
}

// The addressing rules are what keep a controller from talking to
// nothing: handles are 1-based and bounded, oids and method ids are
// 1-based, statuses fit the MS-05-02 range.
func TestValidateCommandMessage(t *testing.T) {
	if err := oneCommand().validate(); err != nil {
		t.Fatalf("a valid command = %v", err)
	}
	if err := (CommandMessage{Commands: []Command{}}).validate(); err != nil {
		t.Errorf("an empty commands array is legal: %v", err)
	}
	if err := (CommandMessage{}).validate(); err == nil ||
		!strings.Contains(err.Error(), "required") {
		t.Error("a command frame with no commands array must be refused")
	}
	if err := (CommandMessage{MessageType: MessageTypeError, Commands: []Command{}}).validate(); err == nil ||
		!strings.Contains(err.Error(), "messageType") {
		t.Error("a command frame typed as an error must be refused")
	}

	for name, cmd := range map[string]Command{
		"a handle below 1":         {Handle: 0, OID: 1, MethodID: MethodID{Level: 1, Index: 1}},
		"a handle above the range": {Handle: 65536, OID: 1, MethodID: MethodID{Level: 1, Index: 1}},
		"an oid below 1":           {Handle: 1, OID: 0, MethodID: MethodID{Level: 1, Index: 1}},
		"a method level below 1":   {Handle: 1, OID: 1, MethodID: MethodID{Level: 0, Index: 1}},
		"a method index below 1":   {Handle: 1, OID: 1, MethodID: MethodID{Level: 1, Index: 0}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := (CommandMessage{Commands: []Command{cmd}}).validate(); err == nil {
				t.Error("was accepted")
			}
		})
	}
}

// A response is matched to its command by handle, and carries a
// status the controller can act on.
func TestValidateCommandResponseMessage(t *testing.T) {
	ok := CommandResponseMessage{Responses: []CommandResponseEntry{{
		Handle: 1, Result: MethodResult{Status: NcMethodStatusOK, Value: json.RawMessage(`true`)},
	}}}
	if err := ok.validate(); err != nil {
		t.Fatalf("a valid response = %v", err)
	}
	if err := (CommandResponseMessage{Responses: []CommandResponseEntry{}}).validate(); err != nil {
		t.Errorf("an empty responses array is legal: %v", err)
	}
	if err := (CommandResponseMessage{}).validate(); err == nil {
		t.Error("a response frame with no responses array must be refused")
	}
	// messageType 0 IS Command, and Go cannot tell an explicit 0 from
	// an unset field — so the mismatch check can only catch a
	// non-zero wrong type. That is the shape of the wire enum, not an
	// oversight.
	if err := (CommandResponseMessage{
		MessageType: MessageTypeError, Responses: []CommandResponseEntry{},
	}).validate(); err == nil {
		t.Error("a response frame typed as an error must be refused")
	}
	if err := (CommandResponseMessage{Responses: []CommandResponseEntry{{Handle: 0}}}).validate(); err == nil {
		t.Error("a response with no handle must be refused")
	}
	for _, status := range []int{-1, 65536} {
		if err := (CommandResponseMessage{Responses: []CommandResponseEntry{{
			Handle: 1, Result: MethodResult{Status: status},
		}}}).validate(); err == nil {
			t.Errorf("status %d was accepted", status)
		}
	}
}

// A notification names the object, the event and the property that
// changed — a controller cannot apply one that is missing any of the
// three.
func TestValidateNotificationMessage(t *testing.T) {
	good := Notification{
		OID:     1,
		EventID: EventID{Level: 1, Index: 1},
		EventData: EventData{
			PropertyID: PropertyID{Level: 1, Index: 6},
			ChangeType: 0,
			Value:      json.RawMessage(`"label"`),
		},
	}
	if err := (NotificationMessage{Notifications: []Notification{good}}).validate(); err != nil {
		t.Fatalf("a valid notification = %v", err)
	}
	if err := (NotificationMessage{Notifications: []Notification{}}).validate(); err != nil {
		t.Errorf("an empty notifications array is legal: %v", err)
	}
	if err := (NotificationMessage{}).validate(); err == nil {
		t.Error("a notification frame with no notifications array must be refused")
	}
	if err := (NotificationMessage{
		MessageType: MessageTypeError, Notifications: []Notification{},
	}).validate(); err == nil {
		t.Error("a notification frame typed as an error must be refused")
	}

	for name, mutate := range map[string]func(n *Notification){
		"no oid":                     func(n *Notification) { n.OID = 0 },
		"no event id":                func(n *Notification) { n.EventID = EventID{} },
		"no property id":             func(n *Notification) { n.EventData.PropertyID = PropertyID{} },
		"a change type below zero":   func(n *Notification) { n.EventData.ChangeType = -1 },
		"a change type out of range": func(n *Notification) { n.EventData.ChangeType = 65536 },
	} {
		t.Run(name, func(t *testing.T) {
			n := good
			mutate(&n)
			if err := (NotificationMessage{Notifications: []Notification{n}}).validate(); err == nil {
				t.Error("was accepted")
			}
		})
	}
}

// The subscription pair and the error frame carry the remaining
// rules: an array that must be present even when empty, and an error
// that must say something.
func TestValidateSubscriptionAndErrorMessages(t *testing.T) {
	if err := (SubscriptionMessage{Subscriptions: []int{1, 2}}).validate(); err != nil {
		t.Errorf("a valid subscription = %v", err)
	}
	if err := (SubscriptionMessage{}).validate(); err == nil {
		t.Error("a subscription frame with no subscriptions array must be refused")
	}
	if err := (SubscriptionMessage{
		MessageType: MessageTypeError, Subscriptions: []int{},
	}).validate(); err == nil {
		t.Error("a subscription frame typed as an error must be refused")
	}

	if err := (SubscriptionResponseMessage{Subscriptions: []int{1}}).validate(); err != nil {
		t.Errorf("a valid subscription response = %v", err)
	}
	if err := (SubscriptionResponseMessage{}).validate(); err == nil {
		t.Error("a subscription response with no subscriptions array must be refused")
	}
	if err := (SubscriptionResponseMessage{
		MessageType: MessageTypeError, Subscriptions: []int{},
	}).validate(); err == nil {
		t.Error("a subscription response typed as an error must be refused")
	}

	if err := (ErrorMessage{Status: 400, ErrorMessage: "bad request"}).validate(); err != nil {
		t.Errorf("a valid error frame = %v", err)
	}
	if err := (ErrorMessage{
		MessageType: MessageTypeNotification, Status: 400, ErrorMessage: "x",
	}).validate(); err == nil {
		t.Error("an error frame typed as a notification must be refused")
	}
	if err := (ErrorMessage{Status: 65536, ErrorMessage: "x"}).validate(); err == nil {
		t.Error("a status out of range must be refused")
	}
	if err := (ErrorMessage{Status: 400}).validate(); err == nil ||
		!strings.Contains(err.Error(), "errorMessage") {
		t.Error("an error frame that says nothing must be refused")
	}
}

// The message type's label is what a log column and the Info view
// print; an unrecognised value has no label, and no valid frame.
func TestMessageTypeLabels(t *testing.T) {
	for m, want := range map[MessageType]string{
		MessageTypeCommand:              "Command",
		MessageTypeCommandResponse:      "CommandResponse",
		MessageTypeNotification:         "Notification",
		MessageTypeSubscription:         "Subscription",
		MessageTypeSubscriptionResponse: "SubscriptionResponse",
		MessageTypeError:                "Error",
		MessageType(9):                  "",
		MessageType(-1):                 "",
	} {
		if got := m.String(); got != want {
			t.Errorf("MessageType(%d).String() = %q, want %q", m, got, want)
		}
		if IsValidMessageType(m) != (want != "") {
			t.Errorf("IsValidMessageType(%d) disagrees with its label", m)
		}
	}
}

// Every variant round-trips, and the encoder stamps the discriminator
// so a struct built without one still goes out as a legal frame.
func TestEncodeDecodeEveryFrame(t *testing.T) {
	for name, msg := range map[string]Message{
		"a command": oneCommand(),
		"a command response": CommandResponseMessage{Responses: []CommandResponseEntry{{
			Handle: 1, Result: MethodResult{Status: NcMethodStatusOK},
		}}},
		"a notification": NotificationMessage{Notifications: []Notification{{
			OID: 1, EventID: EventID{Level: 1, Index: 1},
			EventData: EventData{PropertyID: PropertyID{Level: 1, Index: 6}, Value: json.RawMessage(`1`)},
		}}},
		"a subscription":          SubscriptionMessage{Subscriptions: []int{1}},
		"a subscription response": SubscriptionResponseMessage{Subscriptions: []int{1}},
		"an error":                ErrorMessage{Status: 400, ErrorMessage: "bad request"},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := Encode(msg)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			back, err := Decode(raw)
			if err != nil {
				t.Fatalf("Decode: %v (%s)", err, raw)
			}
			if back.Kind() != msg.Kind() {
				t.Errorf("round-tripped kind = %d, want %d", back.Kind(), msg.Kind())
			}
		})
	}
}

// Encoding refuses what it cannot ship, and decoding refuses what it
// cannot act on — an unknown discriminator, an unknown field, a frame
// that decodes but does not validate.
func TestEncodeAndDecodeRefusals(t *testing.T) {
	if _, err := Encode(nil); err == nil {
		t.Error("Encode(nil) was accepted")
	}
	if _, err := Encode(CommandMessage{}); err == nil {
		t.Error("a command frame with no commands array was encoded")
	}
	if _, err := Encode(unknownVariant{}); err == nil ||
		!strings.Contains(err.Error(), "unsupported variant") {
		t.Error("a variant outside the sum must be refused")
	}

	for name, tc := range map[string]struct {
		raw  string
		want string
	}{
		"a frame that is not JSON": {`{`, "peek messageType"},
		"two frames in one payload": {
			`{"messageType":3,"subscriptions":[]} {"messageType":3,"subscriptions":[]}`, "trailing JSON",
		},
		"a messageType we do not know":     {`{"messageType":9}`, "unknown"},
		"an unknown field on a command":    {`{"messageType":0,"commands":[],"x":1}`, "decode"},
		"a command that does not validate": {`{"messageType":0}`, "commands"},
		"an unknown field on a response":   {`{"messageType":1,"responses":[],"x":1}`, "decode"},
		"a response that does not validate": {
			`{"messageType":1,"responses":[{"handle":0,"result":{"status":200}}]}`, "handle",
		},
		"an unknown field on a notification":    {`{"messageType":2,"notifications":[],"x":1}`, "decode"},
		"a notification that does not validate": {`{"messageType":2}`, "notifications"},
		"an unknown field on a subscription":    {`{"messageType":3,"subscriptions":[],"x":1}`, "decode"},
		"a subscription that does not validate": {`{"messageType":3}`, "subscriptions"},
		"an unknown field on a subscription response": {
			`{"messageType":4,"subscriptions":[],"x":1}`, "decode",
		},
		"a subscription response that does not validate": {`{"messageType":4}`, "subscriptions"},
		"an unknown field on an error": {
			`{"messageType":5,"status":400,"errorMessage":"x","y":1}`, "decode",
		},
		"an error that does not validate": {`{"messageType":5,"status":400}`, "errorMessage"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Decode([]byte(tc.raw))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("= %v, want an error mentioning %q", err, tc.want)
			}
		})
	}
}

// unknownVariant satisfies Message without being one of the six.
type unknownVariant struct{}

func (unknownVariant) Kind() MessageType { return MessageTypeCommand }
func (unknownVariant) validate() error   { return nil }

// stubCodec is an IS-12 codec used to exercise the registry.
type stubCodec struct{ ver string }

func (c stubCodec) SpecID() string    { return SpecID }
func (c stubCodec) APIVer() string    { return c.ver }
func (c stubCodec) SpecPatch() string { return c.ver + ".0" }

func (stubCodec) Encode(m Message) ([]byte, error)   { return Encode(m) }
func (stubCodec) Decode(raw []byte) (Message, error) { return Decode(raw) }

var _ Codec = stubCodec{}

type wrongSpec struct{ stubCodec }

func (wrongSpec) SpecID() string { return "is-04" }

// The per-minor registry answers by version, negotiates with a peer,
// and refuses a codec registered under another spec.
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
