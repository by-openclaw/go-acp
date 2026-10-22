package codec

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// The CVT example of §5.15.4, verbatim (line breaks of the PDF removed).
const docCVTItem = `{"customerId" : "tbd","system" : "5464a897ddbba8f452f11d305d653983","query" : "consumer/cvtItem/antenna/motor/status/targetPlatAz","timeMs" : "1573739995672","parameters" : [{"UID":"cpiasd/antenna/motor/status/targetPlatAz","Value":"180000","GroupID":0,"LastUTS":0,"ConfigValue":"@INVALID","configLastUTS":null,"subCategory":0,"autoInserted":0,"tags":null,"overrideRPID":null,"isExpress":0,"isPrivate":1}]}`

func TestDecodeEnvelopeCVTItem(t *testing.T) {
	t.Parallel()
	e, err := DecodeEnvelope([]byte(docCVTItem))
	if err != nil {
		t.Fatal(err)
	}
	if e.CustomerID != "tbd" || e.System != "5464a897ddbba8f452f11d305d653983" || e.Query != "consumer/cvtItem/antenna/motor/status/targetPlatAz" || e.Time() != 1573739995672 {
		t.Fatalf("header = %+v", e)
	}
	var rows []CVTRow
	single, err := e.Rows(&rows)
	if err != nil || single || len(rows) != 1 {
		t.Fatalf("rows=%d single=%v err=%v", len(rows), single, err)
	}
	r := rows[0]
	if r.UID != "cpiasd/antenna/motor/status/targetPlatAz" || r.Value != "180000" || r.ConfigValue != Invalid || r.ConfigLastUTS != "" || r.LastUTS != "0" || r.IsPrivate != "1" {
		t.Fatalf("row = %+v", r)
	}
}

func TestEnvelopeEmptyAndSingleObject(t *testing.T) {
	t.Parallel()
	// §5.15.4: a nonexistent item answers the empty set.
	e, err := DecodeEnvelope([]byte(`{"customerId":"tbd","system":"f8","query":"consumer/cvtItem/antenna/motor/status/targetPlat","timeMs":"1554389828239","parameters":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	var rows []CVTRow
	if _, err := e.Rows(&rows); err != nil || len(rows) != 0 {
		t.Fatalf("empty set: rows=%d err=%v", len(rows), err)
	}
	// §5.34.4: msgExchangePurge answers the driver's result object.
	e, err = DecodeEnvelope([]byte(`{"customerId":"tbd","system":"f8","query":"consumer/msgExchangePurge/101/147777","timeMs":"1554230056767","parameters":{"fieldCount":0,"affectedRows":2,"insertId":0,"serverStatus":2,"warningCount":0,"message":"","protocol41":true,"changedRows":0}}`))
	if err != nil {
		t.Fatal(err)
	}
	var purge []struct {
		AffectedRows Text `json:"affectedRows"`
	}
	single, err := e.Rows(&purge)
	if err != nil || !single || len(purge) != 1 || purge[0].AffectedRows != "2" {
		t.Fatalf("purge: %+v single=%v err=%v", purge, single, err)
	}
	if _, err := DecodeEnvelope([]byte(`{"a":1}`)); !errors.Is(err, ErrNotEnvelope) {
		t.Fatalf("non-envelope: %v", err)
	}
}

func TestTablesFromDocumentExamples(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, body string
		check      func(t *testing.T, e Envelope)
	}{
		{"enumerations §5.27.4", `{"customerId":"tbd","system":"7d","query":"consumer/enumerations","timeMs":"1557244533088","parameters":[{"UID":"cpiasd/tracking/general/status/trackState","enumIndex":0,"enDisplayText":"uninitialized"},{"UID":"cpiasd/tracking/general/status/trackState","enumIndex":1,"enDisplayText":"not present"}]}`,
			func(t *testing.T, e Envelope) {
				var rows []Enumeration
				if _, err := e.Rows(&rows); err != nil || len(rows) != 2 || rows[1].EnumIndex != "1" || rows[1].EnDisplayText != "not present" {
					t.Fatalf("%+v %v", rows, err)
				}
			}},
		{"dependencies §5.26.4", `{"customerId":"tbd","system":"54","query":"consumer/dependencies","timeMs":"1573740459049","parameters":[{"uid":"cpiasd/antenna/motor/status/targetPlatAz","isHidden":1},{"uid":"cpiasd/antenna/motor/status/targetPlatEl","isHidden":0}]}`,
			func(t *testing.T, e Envelope) {
				var rows []Dependency
				if _, err := e.Rows(&rows); err != nil || len(rows) != 2 || !rows[0].Hidden() || rows[1].Hidden() {
					t.Fatalf("%+v %v", rows, err)
				}
			}},
		{"svt §5.57.4", `{"customerId":"tbd","system":"54","query":"consumer/svtList/antenna/motor","timeMs":"1573743074047","parameters":[{"UID":"cpiasd/antenna/motor","enName":"General Motorization","NodeType":2,"ValueType":null,"RangeMin":"@NOMIN","Units":null,"volatility":0,"unitScale":"1"},{"UID":"cpiasd/antenna/motor/alarms/azCcwHwLimit","enName":"Azimuth CCW hardware limit switch","NodeType":0,"ValueType":"3","Units":"@NOUNITS","volatility":-1,"guiDisplayPrecision":"0"}]}`,
			func(t *testing.T, e Envelope) {
				var rows []SVTRow
				if _, err := e.Rows(&rows); err != nil || len(rows) != 2 {
					t.Fatalf("%+v %v", rows, err)
				}
				if !rows[0].IsNode() || rows[1].IsNode() || rows[0].MQTTOnly() || !rows[1].MQTTOnly() || rows[1].ValueType != "3" || rows[0].ValueType != "" {
					t.Fatalf("%+v", rows)
				}
			}},
		{"login §6.2.4", `{"customerId":"tbd","system":"35","query":"security/login","timeMs":"1662556322900","parameters":[{"Operator":"superuser","AccessLevel":255,"language":"en"}]}`,
			func(t *testing.T, e Envelope) {
				var rows []LoginReply
				if _, err := e.Rows(&rows); err != nil || len(rows) != 1 || rows[0].Operator != "superuser" || rows[0].AccessLevel != "255" {
					t.Fatalf("%+v %v", rows, err)
				}
			}},
		{"positionLog §5.38.4", `{"customerId":"tbd","system":"54","query":"consumer/positionLog","timeMs":"1573741441482","parameters":[{"positionLogSequence":17371,"positionLogTimestamp":1564392947000,"positionLogDateTime":"20190729 093547","positionLogAntenna":"0","positionLogFlags":2,"positionLogAzimuth":"205.660000","positionLogAzimuthCmd":"205.711000","positionLogElevation":"48.000000","positionLogElevationCmd":"48.000000","positionLogPolarization":"0.000000","positionLogPolarizationCmd":"0.000000","positionLogBeaconLevel":"-20.000","positionLogAvgBeaconLevel":null,"positionLogSatelliteNumber":1,"positionLogAntennaSubsystem":1}]}`,
			func(t *testing.T, e Envelope) {
				var rows []PositionRow
				if _, err := e.Rows(&rows); err != nil || len(rows) != 1 || rows[0].Azimuth != "205.660000" || rows[0].BeaconLevel != "-20.000" || rows[0].Sequence != "17371" {
					t.Fatalf("%+v %v", rows, err)
				}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, err := DecodeEnvelope([]byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, e)
		})
	}
}

func TestMessages(t *testing.T) {
	t.Parallel()
	// §5.39.2 sample: { MCII: 1, NumArgs: 2, Message: 'cpiasd/move/command/targetAz,120000' }
	m, err := SetMessage("cpiasd/move/command/targetAz", "120000")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(m.Encode()); got != `{"MCII":1,"NumArgs":2,"Message":"cpiasd/move/command/targetAz,120000"}` {
		t.Fatalf("set = %s", got)
	}
	// §9 sample: { MCII: 2, NumArgs: 1, Message: RECOMPUTE-SAT-FAVORITE-LIST' }
	m, err = CommandMessage("RECOMPUTE-SAT-FAVORITE-LIST")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(m.Encode()); got != `{"MCII":2,"NumArgs":1,"Message":"RECOMPUTE-SAT-FAVORITE-LIST"}` {
		t.Fatalf("command = %s", got)
	}
	m, _ = CommandMessage("POINT-AT-GLOBAL-SAT", "42")
	if m.Message != "POINT-AT-GLOBAL-SAT 42" {
		t.Fatalf("command with arg = %q", m.Message)
	}
	for _, bad := range []struct {
		err error
		fn  func() error
	}{
		{ErrEmptyUID, func() error { _, e := SetMessage(" ", "1"); return e }},
		{ErrCommaInUID, func() error { _, e := SetMessage("a,b", "1"); return e }},
		{ErrEmptyVerb, func() error { _, e := CommandMessage(""); return e }},
		{ErrSpaceInVerb, func() error { _, e := CommandMessage("STOW NOW"); return e }},
		{ErrSpaceInCommand, func() error { _, e := CommandMessage("INJECT-LOG", "two words"); return e }},
	} {
		if err := bad.fn(); !errors.Is(err, bad.err) {
			t.Errorf("want %v, got %v", bad.err, err)
		}
	}
}

func TestUIDRules(t *testing.T) {
	t.Parallel()
	if QueryPath("cpiasd/antenna/motor/status/targetPlatAz") != "antenna/motor/status/targetPlatAz" || QueryPath("cpiasd") != "" {
		t.Fatal("QueryPath")
	}
	if Root("cpiasd/antenna") != "cpiasd" {
		t.Fatal("Root")
	}
	for _, in := range []string{"antenna.motor.status.targetPlatAz", "/antenna/motor/status/targetPlatAz", "cpiasd/antenna/motor/status/targetPlatAz", "cpiasd.antenna.motor.status.targetPlatAz"} {
		if got := UIDFor("cpiasd", in); got != "cpiasd/antenna/motor/status/targetPlatAz" {
			t.Errorf("UIDFor(%q) = %q", in, got)
		}
	}
	if UIDFor("cpiasd", "") != "cpiasd" {
		t.Fatal("UIDFor root")
	}
	for _, c := range []struct {
		tpl, uid string
		want     bool
	}{
		{"a/b/#", "a/b/3", true}, {"a/b/#", "a/b/12", true}, {"a/b#", "a/b7", true},
		{"a/b/#", "a/b/", false}, {"a/b/#", "a/b/x", false}, {"a/b/c", "a/b/c", false},
	} {
		if MatchArray(c.tpl, c.uid) != c.want {
			t.Errorf("MatchArray(%q,%q) != %v", c.tpl, c.uid, c.want)
		}
	}
	for _, c := range []struct {
		f, topic string
		want     bool
	}{
		{"cpiasd/#", "cpiasd/antenna/motor", true}, {"#", "a", true},
		{"cpiasd/+/motor", "cpiasd/antenna/motor", true}, {"cpiasd/+/motor", "cpiasd/antenna/x", false},
		{"cpiasd/antenna", "cpiasd/antenna/motor", false}, {"cpiasd/antenna/motor", "cpiasd/antenna", false},
	} {
		if TopicMatches(c.f, c.topic) != c.want {
			t.Errorf("TopicMatches(%q,%q) != %v", c.f, c.topic, c.want)
		}
	}
}

// MQTT 3.1.1 §2.2.3 Table 2.4: the size of Remaining Length field.
func TestRemainingLength(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		n    int
		want []byte
	}{
		{0, []byte{0x00}}, {127, []byte{0x7F}},
		{128, []byte{0x80, 0x01}}, {16383, []byte{0xFF, 0x7F}},
		{16384, []byte{0x80, 0x80, 0x01}}, {2097151, []byte{0xFF, 0xFF, 0x7F}},
		{2097152, []byte{0x80, 0x80, 0x80, 0x01}}, {MaxRemainingLength, []byte{0xFF, 0xFF, 0xFF, 0x7F}},
	} {
		if got := AppendRemainingLength(nil, c.n); !bytes.Equal(got, c.want) {
			t.Errorf("%d: % x, want % x", c.n, got, c.want)
		}
	}
	if _, err := ReadPacket(bytes.NewReader([]byte{0x30, 0xFF, 0xFF, 0xFF, 0xFF, 0x01}), 0); !errors.Is(err, ErrRemainingLength) {
		t.Fatalf("5-byte length: %v", err)
	}
}

func TestMQTTPackets(t *testing.T) {
	t.Parallel()
	// CONNECT §3.1: 00 04 'MQTT', level 4, flags (user 0x80 | password 0x40 | clean 0x02) = 0xC2, keep-alive 60.
	want := []byte{0x10, 0x1A,
		0x00, 0x04, 'M', 'Q', 'T', 'T', 0x04, 0xC2, 0x00, 0x3C,
		0x00, 0x03, 'd', 'h', 's',
		0x00, 0x04, 'u', 's', 'e', 'r',
		0x00, 0x03, 'p', 'w', 'd'}
	if got := Connect("dhs", "user", "pwd", 60); !bytes.Equal(got, want) {
		t.Fatalf("CONNECT\n got % x\nwant % x", got, want)
	}
	// Anonymous (low-security model, §6.1 scenario 1): clean session only.
	want = []byte{0x10, 0x0F, 0x00, 0x04, 'M', 'Q', 'T', 'T', 0x04, 0x02, 0x00, 0x3C, 0x00, 0x03, 'd', 'h', 's'}
	if got := Connect("dhs", "", "", 60); !bytes.Equal(got, want) {
		t.Fatalf("anonymous CONNECT\n got % x\nwant % x", got, want)
	}
	// SUBSCRIBE §3.8: fixed header 0x82, remaining length 2 (id) + 2 (len) + 8 ("cpiasd/#") + 1 (QoS) = 13.
	want = []byte{0x82, 0x0D, 0x00, 0x01, 0x00, 0x08, 'c', 'p', 'i', 'a', 's', 'd', '/', '#', 0x00}
	if got := Subscribe(1, "cpiasd/#"); !bytes.Equal(got, want) {
		t.Fatalf("SUBSCRIBE\n got % x\nwant % x", got, want)
	}
	want = []byte{0xA2, 0x0A, 0x00, 0x02, 0x00, 0x06, 'c', 'p', 'i', 'a', 's', 'd'}
	if got := Unsubscribe(2, "cpiasd"); !bytes.Equal(got, want) {
		t.Fatalf("UNSUBSCRIBE\n got % x\nwant % x", got, want)
	}
	if !bytes.Equal(Pingreq(), []byte{0xC0, 0x00}) || !bytes.Equal(Disconnect(), []byte{0xE0, 0x00}) || !bytes.Equal(Puback(7), []byte{0x40, 0x02, 0x00, 0x07}) {
		t.Fatal("PINGREQ / DISCONNECT / PUBACK")
	}

	// CONNACK accepted and refused (§3.2.2.3 code 4).
	p, _ := ReadPacket(bytes.NewReader([]byte{0x20, 0x02, 0x00, 0x00}), 0)
	if err := ParseConnack(p); err != nil {
		t.Fatal(err)
	}
	p, _ = ReadPacket(bytes.NewReader([]byte{0x20, 0x02, 0x00, 0x04}), 0)
	var ce ConnackError
	if err := ParseConnack(p); !errors.As(err, &ce) || ce.Code != 4 || !strings.Contains(err.Error(), "bad user name or password") {
		t.Fatalf("refused CONNACK: %v", err)
	}
	// SUBACK: id 1, granted QoS 0.
	p, _ = ReadPacket(bytes.NewReader([]byte{0x90, 0x03, 0x00, 0x01, 0x00}), 0)
	if id, codes, err := ParseSuback(p); err != nil || id != 1 || !bytes.Equal(codes, []byte{0}) {
		t.Fatalf("SUBACK %d % x %v", id, codes, err)
	}

	// PUBLISH QoS 0 then QoS 1 (packet id before the payload).
	topic := "cpiasd/antenna/motor/status/targetPlatAz"
	body := append([]byte{0x00, byte(len(topic))}, topic...)
	body = append(body, "180000"...)
	p, _ = ReadPacket(bytes.NewReader(append([]byte{0x31, byte(len(body))}, body...)), 0)
	pub, err := ParsePublish(p)
	if err != nil || pub.Topic != topic || string(pub.Payload) != "180000" || pub.QoS != 0 || !pub.Retain {
		t.Fatalf("PUBLISH qos0 %+v %v", pub, err)
	}
	body = append([]byte{0x00, byte(len(topic))}, topic...)
	body = append(body, 0x00, 0x09)
	body = append(body, "1"...)
	p, _ = ReadPacket(bytes.NewReader(append([]byte{0x32, byte(len(body))}, body...)), 0)
	if pub, err = ParsePublish(p); err != nil || pub.QoS != 1 || pub.PacketID != 9 || string(pub.Payload) != "1" {
		t.Fatalf("PUBLISH qos1 %+v %v", pub, err)
	}
	if _, err := ReadPacket(bytes.NewReader([]byte{0x30, 0x05, 0, 0, 0, 0, 0}), 4); !errors.Is(err, ErrPacketTooLarge) {
		t.Fatalf("limit: %v", err)
	}
	if _, err := ParsePublish(Packet{Type: PacketPUBLISH, Body: []byte{0x00, 0x09, 'a'}}); !errors.Is(err, ErrShortPacket) {
		t.Fatalf("short publish: %v", err)
	}
}
