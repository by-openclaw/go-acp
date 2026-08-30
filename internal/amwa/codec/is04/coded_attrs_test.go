package is04_test

// NMOS parameter-register attribute round-trips (BCP-006-01 JPEG XS /
// BCP-006-04 MPEG-TS): Flow profile/level/sublevel/bit_rate and Sender
// bit_rate/packet_transmission_mode/st2110_21_sender_type must survive
// decode→encode — the AMWA suites read them from the wire.

import (
	"encoding/json"
	"testing"

	"dhs/internal/amwa/codec/is04"
)

func TestFlowCodedAttrsRoundTrip(t *testing.T) {
	raw := []byte(`{
		"id":"aaaaaaaa-1111-4111-8111-111111111111","version":"1:0",
		"label":"jxsv","description":"d","tags":{},
		"source_id":"bbbbbbbb-2222-4222-8222-222222222222",
		"device_id":"cccccccc-3333-4333-8333-333333333333",
		"parents":[],"format":"urn:x-nmos:format:video",
		"media_type":"video/jxsv","frame_width":1920,"frame_height":1080,
		"profile":"High444.12","level":"2k-1","sublevel":"Sublev3bpp",
		"bit_rate":497664
	}`)
	f, err := is04.DecodeFlow(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if f.Profile != "High444.12" || f.Level != "2k-1" || f.Sublevel != "Sublev3bpp" || f.FlowBitRate != 497664 {
		t.Fatalf("attrs lost: %+v", f)
	}
	out, _ := json.Marshal(f)
	for _, want := range []string{`"profile":"High444.12"`, `"level":"2k-1"`, `"sublevel":"Sublev3bpp"`, `"bit_rate":497664`} {
		if !contains(out, want) {
			t.Errorf("re-encode dropped %s: %s", want, out)
		}
	}
}

func TestSenderRegisterAttrsRoundTrip(t *testing.T) {
	raw := []byte(`{
		"id":"cccccccc-3333-4333-8333-333333333333","version":"1:0",
		"label":"s","description":"d","tags":{},
		"flow_id":"dddddddd-4444-4444-8444-444444444444",
		"transport":"urn:x-nmos:transport:rtp.mcast",
		"device_id":"eeeeeeee-5555-4555-8555-555555555555",
		"manifest_href":"http://h/tf","interface_bindings":["eth0"],
		"subscription":{"receiver_id":null,"active":false},
		"bit_rate":497664,"packet_transmission_mode":"codestream",
		"st2110_21_sender_type":"2110TPW"
	}`)
	s, err := is04.DecodeSender(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if s.BitRate != 497664 || s.PacketTransmissionMode != "codestream" || s.ST211021SenderType != "2110TPW" {
		t.Fatalf("attrs lost: %+v", s)
	}
	out, _ := json.Marshal(s)
	for _, want := range []string{`"bit_rate":497664`, `"packet_transmission_mode":"codestream"`, `"st2110_21_sender_type":"2110TPW"`} {
		if !contains(out, want) {
			t.Errorf("re-encode dropped %s: %s", want, out)
		}
	}
}
