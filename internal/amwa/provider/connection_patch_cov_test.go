package provider

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is05"
)

// A staged PATCH is read twice over: once for its values and once for
// which fields the body actually carried — master_enable is a plain
// bool, so "absent" must not read as "false".
func TestDecodePatchPresence(t *testing.T) {
	patch, present, err := decodePatch([]byte(`{"master_enable":false}`))
	if err != nil {
		t.Fatalf("decodePatch: %v", err)
	}
	if !present.MasterEnable {
		t.Error("a body carrying master_enable:false must record it as present")
	}
	if patch.MasterEnable {
		t.Error("the value must be the one the controller sent")
	}

	_, present, err = decodePatch([]byte(`{"activation":{"mode":null}}`))
	if err != nil {
		t.Fatalf("decodePatch: %v", err)
	}
	if present.MasterEnable {
		t.Error("a body that does not carry master_enable must not record it")
	}

	// A receiver PATCH names the far end sender_id, which the shared
	// struct holds in its one id slot; the view renames it back.
	patch, present, err = decodePatch([]byte(`{"sender_id":"11111111-1111-4111-8111-111111111111"}`))
	if err != nil {
		t.Fatalf("decodePatch: %v", err)
	}
	if !present.SenderID || present.ReceiverID {
		t.Errorf("presence = %+v, want sender_id alone", present)
	}
	if patch.ReceiverID == nil || *patch.ReceiverID != "11111111-1111-4111-8111-111111111111" {
		t.Errorf("the far end = %v, want it lifted into the id slot", patch.ReceiverID)
	}

	// A sender PATCH names receiver_id, which lands in the same slot.
	_, present, err = decodePatch([]byte(`{"receiver_id":null}`))
	if err != nil {
		t.Fatalf("decodePatch: %v", err)
	}
	if !present.ReceiverID {
		t.Error("an explicit null receiver_id is still present")
	}

	for name, tc := range map[string]struct {
		body string
		want string
	}{
		"a body that is not JSON":      {`{`, "unexpected"},
		"a body that is not an object": {`[]`, "cannot unmarshal"},
		"a field outside the staged schema": {
			`{"bad":"data"}`, "not a field of a staged endpoint",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := decodePatch([]byte(tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("= %v, want an error mentioning %q", err, tc.want)
			}
		})
	}

	// Every field the staged schemas do define is accepted.
	full := `{"master_enable":true,"activation":{"mode":null},` +
		`"transport_params":[{"destination_port":5004}],` +
		`"transport_file":{"type":"application/sdp","data":"v=0"},` +
		`"receiver_id":null}`
	if _, present, err := decodePatch([]byte(full)); err != nil {
		t.Errorf("a full staged body = %v", err)
	} else if !present.TransportParams || !present.TransportFile {
		t.Errorf("presence = %+v, want every field recorded", present)
	}
}

// The two collections publish different shapes: a receiver carries
// sender_id and always an object for transport_file, a sender carries
// receiver_id. Serving one in place of the other is a schema failure a
// controller rejects outright.
func TestViewOfPerCollection(t *testing.T) {
	far := "11111111-1111-4111-8111-111111111111"
	staged := is05.StagedSender{ReceiverID: &far}

	sender, ok := viewOf("senders", staged).(is05.StagedSender)
	if !ok {
		t.Fatalf("a sender view = %T", viewOf("senders", staged))
	}
	if sender.ReceiverID == nil || *sender.ReceiverID != far {
		t.Errorf("the sender view keeps receiver_id: %+v", sender)
	}

	receiver, ok := viewOf("receivers", staged).(is05.StagedReceiver)
	if !ok {
		t.Fatalf("a receiver view = %T", viewOf("receivers", staged))
	}
	if receiver.SenderID == nil || *receiver.SenderID != far {
		t.Errorf("the receiver view renames the id to sender_id: %+v", receiver)
	}
	// receiver-stage-schema types transport_file as an object, so a
	// receiver that has never been given an SDP still publishes one
	// with null members — omitting it fails validation on every read.
	if receiver.TransportFile == nil {
		t.Error("a receiver view must always carry a transport_file object")
	}

	// One the endpoint already carries is passed through.
	sdp := "v=0"
	typ := "application/sdp"
	staged.TransportFile = &is05.TransportFile{Type: &typ, Data: &sdp}
	receiver = viewOf("receivers", staged).(is05.StagedReceiver)
	if receiver.TransportFile == nil || receiver.TransportFile.Data == nil ||
		*receiver.TransportFile.Data != sdp {
		t.Errorf("transport_file = %+v, want the stored one", receiver.TransportFile)
	}
}

// IS-04 splits RTP into mcast and ucast; IS-05's schemas are keyed on
// the base URN, and every other transport passes through unchanged.
func TestIS05TransportTypeMapping(t *testing.T) {
	for in, want := range map[string]string{
		"urn:x-nmos:transport:rtp":       "urn:x-nmos:transport:rtp",
		"urn:x-nmos:transport:rtp.mcast": "urn:x-nmos:transport:rtp",
		"urn:x-nmos:transport:rtp.ucast": "urn:x-nmos:transport:rtp",
		"urn:x-nmos:transport:mqtt":      "urn:x-nmos:transport:mqtt",
		"urn:x-nmos:transport:websocket": "urn:x-nmos:transport:websocket",
	} {
		if got := is05TransportType(in); got != want {
			t.Errorf("is05TransportType(%q) = %q, want %q", in, got, want)
		}
	}
}
