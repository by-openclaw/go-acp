package is05

import (
	"strings"
	"testing"
)

// What the nmos-cpp reference node answers on /active after an
// immediate activation (captured 2026-10-04): requested_time carries the
// time the request arrived.
const activeAfterImmediate = `{"activation":{"activation_time":"1791123035:738690889","mode":"activate_immediate","requested_time":"1791123035:736877991"}`

func TestActiveViewOfAnImmediateActivationIsRead(t *testing.T) {
	sender := activeAfterImmediate + `,"master_enable":true,"receiver_id":null,"transport_params":[{"destination_ip":"239.255.200.77"}]}`
	s, err := DecodeActiveSender([]byte(sender))
	if err != nil {
		t.Fatalf("a Sender's /active after an immediate activation: %v", err)
	}
	if s.Activation.Mode != ActivationModeImmediate || s.Activation.RequestedTime == nil || *s.Activation.RequestedTime != "1791123035:736877991" {
		t.Errorf("activation = %+v", s.Activation)
	}

	receiver := activeAfterImmediate + `,"master_enable":true,"sender_id":null,"transport_file":{"data":null,"type":null},"transport_params":[{}]}`
	if _, err := DecodeActiveReceiver([]byte(receiver)); err != nil {
		t.Fatalf("a Receiver's /active after an immediate activation: %v", err)
	}

	// The staged endpoint keeps its rule: there the field is always null.
	if _, err := DecodeStagedSender([]byte(sender)); err == nil || !strings.Contains(err.Error(), "must be null for activate_immediate") {
		t.Errorf("the same body read as /staged: %v", err)
	}
}

func TestActiveViewThatIsNotOne(t *testing.T) {
	const tail = `,"master_enable":true,"receiver_id":null,"transport_params":[]}`
	for _, tc := range []struct {
		name, body, want string
	}{
		{"an unknown mode", `{"activation":{"mode":"activate_whenever","requested_time":null,"activation_time":null}` + tail, "activation.mode"},
		{"a requested_time that is not TAI", `{"activation":{"mode":"activate_immediate","requested_time":"now","activation_time":null}` + tail, "TAI form"},
		{"no transport_params", `{"activation":{"mode":null,"requested_time":null,"activation_time":null},"master_enable":true,"receiver_id":null}`, "transport_params: required"},
		{"a key IS-05 does not define", `{"activation":{"mode":null,"requested_time":null,"activation_time":null},"vendor":1` + tail, "decode active"},
		{"trailing content", `{"activation":{"mode":null,"requested_time":null,"activation_time":null}` + tail + `{}`, "trailing JSON"},
	} {
		if _, err := DecodeActiveSender([]byte(tc.body)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("sender, %s: err = %v, want it to say %q", tc.name, err, tc.want)
		}
		body := strings.Replace(tc.body, `"receiver_id":null`, `"sender_id":null,"transport_file":{"data":null,"type":null}`, 1)
		if _, err := DecodeActiveReceiver([]byte(body)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("receiver, %s: err = %v, want it to say %q", tc.name, err, tc.want)
		}
	}
}
