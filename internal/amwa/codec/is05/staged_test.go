package is05

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"dhs/internal/amwa/codec/spec"
)

// TestActivationUnsetModeIsNull: every IS-05 activation schema types
// `mode` as a nullable enum with no "" member, so the Go zero value
// must render as null and the enum strings as themselves.
func TestActivationUnsetModeIsNull(t *testing.T) {
	raw, err := json.Marshal(Activation{})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"mode":null,"requested_time":null,"activation_time":null}` {
		t.Fatalf("unset activation = %s", raw)
	}
	raw, err = json.Marshal(Activation{Mode: ActivationModeImmediate})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"mode":"activate_immediate"`) {
		t.Fatalf("set activation = %s", raw)
	}
}

func sdp() *TransportFile {
	typ, data := "application/sdp", "v=0\r\no=- 1 1 IN IP4 10.6.250.101\r\n"
	return &TransportFile{Type: &typ, Data: &data}
}

// TestValidateStagedSenderRules: the activation rules apply first, then
// transport_params must be present, then an inline transport file that
// carries data must say what type it is.
func TestValidateStagedSenderRules(t *testing.T) {
	ok := StagedSender{TransportParams: []TransportParams{{}}}
	cases := []struct {
		name string
		s    StagedSender
		want string
	}{
		{"activation mode outside the enum", StagedSender{
			Activation: Activation{Mode: "activate_later"}, TransportParams: []TransportParams{{}},
		}, "activation.mode"},
		{"transport_params absent", StagedSender{}, "transport_params"},
		{"transport file data without a type", func() StagedSender {
			s := ok
			f := sdp()
			f.Type = nil
			s.TransportFile = f
			return s
		}(), "transport_file.type"},
		{"transport file data with an empty type", func() StagedSender {
			s := ok
			f := sdp()
			empty := ""
			f.Type = &empty
			s.TransportFile = f
			return s
		}(), "transport_file.type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateStagedSender(tc.s)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error naming %q, got %v", tc.want, err)
			}
		})
	}
	withFile := ok
	withFile.TransportFile = sdp()
	if err := ValidateStagedSender(withFile); err != nil {
		t.Fatalf("a typed transport file is valid: %v", err)
	}
	unset := ok
	unset.TransportFile = &TransportFile{}
	if err := ValidateStagedSender(unset); err != nil {
		t.Fatalf("{type:null,data:null} is how an unset file is spelled: %v", err)
	}
}

func TestValidateStagedReceiverRules(t *testing.T) {
	err := ValidateStagedReceiver(StagedReceiver{
		Activation: Activation{Mode: "activate_later"}, TransportParams: []TransportParams{{}},
	})
	if err == nil || !strings.Contains(err.Error(), "activation.mode") {
		t.Fatalf("want the activation rule first, got %v", err)
	}
	if err := ValidateStagedReceiver(StagedReceiver{TransportParams: []TransportParams{}}); err != nil {
		t.Fatalf("an empty transport_params array is valid: %v", err)
	}
}

func TestEncodeStagedSenderRefusesInvalid(t *testing.T) {
	_, err := EncodeStagedSender(StagedSender{})
	if err == nil || !strings.Contains(err.Error(), "transport_params") {
		t.Fatalf("want the validation error, got %v", err)
	}
}

// TestDecodeStagedSenderRefuses: the staged body is read strictly, and
// what parses is still validated.
func TestDecodeStagedSenderRefuses(t *testing.T) {
	cases := map[string]struct{ body, want string }{
		"trailing content": {
			`{"master_enable":true,"receiver_id":null,"activation":{"mode":null,"requested_time":null,"activation_time":null},"transport_params":[]} {}`,
			"trailing JSON",
		},
		"transport_params null": {
			`{"master_enable":true,"receiver_id":null,"activation":{"mode":null,"requested_time":null,"activation_time":null},"transport_params":null}`,
			"transport_params",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeStagedSender([]byte(tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error naming %q, got %v", tc.want, err)
			}
		})
	}
}

// TestStagedReceiverRoundTrip: a scheduled activation with an inline
// SDP survives encode and decode intact.
func TestStagedReceiverRoundTrip(t *testing.T) {
	sender := "33333333-3333-4333-8333-333333333333"
	at := "1700000037:0"
	in := StagedReceiver{
		MasterEnableField: MasterEnableField{MasterEnable: true},
		SenderID:          &sender,
		Activation:        Activation{Mode: ActivationModeScheduledAbsolute, RequestedTime: &at},
		TransportParams:   []TransportParams{{"destination_ip": "239.1.1.1", "destination_port": 5004}},
		TransportFile:     sdp(),
	}
	body, err := EncodeStagedReceiver(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := DecodeStagedReceiver(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if *got.SenderID != sender || got.Activation.Mode != ActivationModeScheduledAbsolute || *got.Activation.RequestedTime != at {
		t.Fatalf("round trip diverged: %+v", got)
	}
	if got.TransportFile == nil || *got.TransportFile.Data != *in.TransportFile.Data {
		t.Fatalf("transport file lost: %+v", got.TransportFile)
	}
	if got.TransportParams[0]["destination_ip"] != "239.1.1.1" {
		t.Fatalf("transport params lost: %v", got.TransportParams)
	}
}

func TestEncodeStagedReceiverRefusesInvalid(t *testing.T) {
	_, err := EncodeStagedReceiver(StagedReceiver{})
	if err == nil || !strings.Contains(err.Error(), "transport_params") {
		t.Fatalf("want the validation error, got %v", err)
	}
}

func TestDecodeStagedReceiverRefuses(t *testing.T) {
	valid := `{"master_enable":true,"sender_id":null,"activation":{"mode":null,"requested_time":null,"activation_time":null},"transport_params":[],"transport_file":{"type":null,"data":null}}`
	cases := map[string]struct{ body, want string }{
		"broken json":      {`{"master_enable":`, "decode staged receiver"},
		"unknown field":    {strings.Replace(valid, `"master_enable"`, `"future":1,"master_enable"`, 1), "future"},
		"trailing content": {valid + ` {}`, "trailing JSON"},
		"transport_params null": {
			strings.Replace(valid, `"transport_params":[]`, `"transport_params":null`, 1),
			"transport_params",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeStagedReceiver([]byte(tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error naming %q, got %v", tc.want, err)
			}
		})
	}
	if _, err := DecodeStagedReceiver([]byte(valid)); err != nil {
		t.Fatalf("the reference body must decode: %v", err)
	}
}

// TestTAIToTimeInvertsFormatTAINow: both directions go through the one
// suite-wide implementation, so an activation stamped here and read
// back lands on the same instant.
func TestTAIToTimeInvertsFormatTAINow(t *testing.T) {
	at := time.Unix(1700000000, 123).UTC()
	sec, nsec, ok := spec.ParseTAI(FormatTAINow(at))
	if !ok {
		t.Fatal("FormatTAINow must produce <sec>:<nsec>")
	}
	if sec != 1700000000+TAILeapSeconds {
		t.Fatalf("TAI seconds = %d, want UTC + %d leap seconds", sec, TAILeapSeconds)
	}
	if got := TAIToTime(sec, nsec); !got.Equal(at) {
		t.Fatalf("TAIToTime = %v, want %v", got, at)
	}
}
