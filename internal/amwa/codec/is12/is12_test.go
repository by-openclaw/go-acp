package is12

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMessageTypeString(t *testing.T) {
	cases := map[MessageType]string{
		MessageTypeCommand:              "Command",
		MessageTypeCommandResponse:      "CommandResponse",
		MessageTypeNotification:         "Notification",
		MessageTypeSubscription:         "Subscription",
		MessageTypeSubscriptionResponse: "SubscriptionResponse",
		MessageTypeError:                "Error",
		MessageType(99):                 "",
	}
	for m, want := range cases {
		if got := m.String(); got != want {
			t.Errorf("(%d).String() = %q, want %q", int(m), got, want)
		}
	}
}

func TestCommandRoundTrip(t *testing.T) {
	in := CommandMessage{
		Commands: []Command{
			{
				Handle:    1,
				OID:       1,
				MethodID:  MethodID{Level: 1, Index: 5},
				Arguments: json.RawMessage(`{"propertyId":{"level":1,"index":3}}`),
			},
		},
	}
	body, err := Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := Decode(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got := out.(CommandMessage)
	if got.Kind() != MessageTypeCommand {
		t.Fatalf("kind mismatch")
	}
	if got.Commands[0].Handle != 1 {
		t.Fatalf("handle mismatch")
	}
}

func TestCommandResponseRoundTrip(t *testing.T) {
	in := CommandResponseMessage{
		Responses: []CommandResponseEntry{
			{
				Handle: 1,
				Result: MethodResult{
					Status: NcMethodStatusOK,
					Value:  json.RawMessage(`42`),
				},
			},
		},
	}
	body, err := Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := Decode(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got := out.(CommandResponseMessage)
	if got.Responses[0].Result.Status != 200 {
		t.Fatalf("status mismatch")
	}
}

func TestNotificationRoundTrip(t *testing.T) {
	idx := 0
	in := NotificationMessage{
		Notifications: []Notification{
			{
				OID:     1,
				EventID: EventID{Level: 1, Index: 1},
				EventData: PropertyChangedEventData{
					PropertyID:        PropertyID{Level: 1, Index: 3},
					ChangeType:        0,
					Value:             json.RawMessage(`"hello"`),
					SequenceItemIndex: &idx,
				},
			},
		},
	}
	body, err := Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := Decode(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got := out.(NotificationMessage)
	if got.Notifications[0].EventData.SequenceItemIndex == nil ||
		*got.Notifications[0].EventData.SequenceItemIndex != 0 {
		t.Fatalf("sequence_item_index mismatch")
	}
}

func TestSubscriptionRoundTrip(t *testing.T) {
	in := SubscriptionMessage{Subscriptions: []int{1, 2, 3}}
	body, err := Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := Decode(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got := out.(SubscriptionMessage)
	if len(got.Subscriptions) != 3 {
		t.Fatalf("subs length mismatch")
	}
}

func TestSubscriptionResponseRoundTrip(t *testing.T) {
	in := SubscriptionResponseMessage{Subscriptions: []int{1, 2}}
	body, err := Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := Decode(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got := out.(SubscriptionResponseMessage)
	if got.Kind() != MessageTypeSubscriptionResponse {
		t.Fatalf("kind mismatch")
	}
	if len(got.Subscriptions) != 2 {
		t.Fatalf("subs length mismatch")
	}
}

func TestErrorRoundTrip(t *testing.T) {
	in := ErrorMessage{Status: 500, ErrorMessage: "boom"}
	body, err := Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := Decode(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got := out.(ErrorMessage)
	if got.ErrorMessage != "boom" {
		t.Fatalf("error message mismatch")
	}
}

func TestErrorRequiresMessage(t *testing.T) {
	in := ErrorMessage{Status: 500}
	if _, err := Encode(in); err == nil {
		t.Fatalf("expected required errorMessage")
	}
}

func TestDecodeUnknownMessageType(t *testing.T) {
	body := []byte(`{"messageType":99}`)
	if _, err := Decode(body); err == nil {
		t.Fatalf("expected unknown messageType error")
	}
}

func TestDecodeMissingMessageType(t *testing.T) {
	body := []byte(`{}`)
	if _, err := Decode(body); err == nil {
		t.Fatalf("expected error on missing messageType")
	}
}

func TestDecodeRejectsUnknownField(t *testing.T) {
	body := []byte(`{"messageType":3,"subscriptions":[],"future_field":42}`)
	if _, err := Decode(body); err == nil {
		t.Fatalf("expected DisallowUnknownFields rejection")
	} else if !strings.Contains(err.Error(), "future_field") {
		t.Fatalf("error should name unknown field: %v", err)
	}
}

func TestCommandHandleRange(t *testing.T) {
	cases := []struct {
		name   string
		handle int
		ok     bool
	}{
		{"too-low", 0, false},
		{"too-high", 70000, false},
		{"low", 1, true},
		{"high", 65535, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := CommandMessage{
				Commands: []Command{
					{Handle: tc.handle, OID: 1, MethodID: MethodID{Level: 1, Index: 1}},
				},
			}
			_, err := Encode(in)
			if tc.ok && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("want error")
			}
		})
	}
}

func TestMethodIDValidation(t *testing.T) {
	in := CommandMessage{
		Commands: []Command{
			{Handle: 1, OID: 1, MethodID: MethodID{Level: 0, Index: 1}},
		},
	}
	if _, err := Encode(in); err == nil {
		t.Fatalf("level 0 should fail")
	}
	in.Commands[0].MethodID = MethodID{Level: 1, Index: 0}
	if _, err := Encode(in); err == nil {
		t.Fatalf("index 0 should fail")
	}
}

func TestCommandRequiresArray(t *testing.T) {
	in := CommandMessage{}
	if _, err := Encode(in); err == nil {
		t.Fatalf("nil commands should fail")
	}
}

func TestEncodeNormalisesDiscriminator(t *testing.T) {
	in := CommandMessage{
		MessageType: 99, // wrong on input
		Commands: []Command{
			{Handle: 1, OID: 1, MethodID: MethodID{Level: 1, Index: 1}},
		},
	}
	body, err := Encode(in)
	if err == nil {
		t.Fatalf("expected validate to reject mismatched messageType")
	}
	_ = body
	// With correct (or zero) discriminator, encode normalises to canonical.
	in.MessageType = 0
	body, err = Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !strings.Contains(string(body), `"messageType": 0`) {
		t.Fatalf("encoded body missing canonical messageType: %s", string(body))
	}
}
