package codec

import (
	"strings"
	"testing"
)

func TestEncodeLogin(t *testing.T) {
	got := string(EncodeLogin(7, "alice", "s3cr3t"))
	want := `<login username="alice" password="s3cr3t" mtid="7"/>`
	if got != want {
		t.Fatalf("EncodeLogin\n got %s\nwant %s", got, want)
	}
}

func TestEncodePoll(t *testing.T) {
	got := string(EncodePoll(0xffff))
	want := `<poll mtid="65535"/>`
	if got != want {
		t.Fatalf("EncodePoll\n got %s\nwant %s", got, want)
	}
}

func TestEncodeUnsubscribeAll(t *testing.T) {
	got := string(EncodeUnsubscribeAll(1))
	want := `<unsubscribe_all mtid="1"/>`
	if got != want {
		t.Fatalf("EncodeUnsubscribeAll\n got %s\nwant %s", got, want)
	}
}

func TestEncodeAction_Routing(t *testing.T) {
	body := &RoutingAction{
		Type:       "ROUTE",
		DeviceName: "RTR-A",
		DeviceType: DeviceTypeRouter,
		SrceID:     "12",
		DestID:     "34",
		LevelID:    "1",
	}
	got := string(EncodeAction(42, body))
	// Attribute order matches AttrsBuilder declaration order.
	want := `<action mtid="42"><routing TYPE="ROUTE" DEVICE_NAME="RTR-A" DEVICE_TYPE="Router" SRCE_ID="12" DEST_ID="34" LEVEL_ID="1"/></action>`
	if got != want {
		t.Fatalf("EncodeAction\n got %s\nwant %s", got, want)
	}
}

func TestEncodeSubscribe_RoutingChange(t *testing.T) {
	items := []SubItem{
		&RoutingChange{
			Type:       "ROUTE",
			DeviceName: "*",
			DeviceType: DeviceType("*"),
		},
	}
	got := string(EncodeSubscribe(3, items))
	want := `<subscribe mtid="3"><routing_change type="ROUTE" device_name="*" device_type="*"/></subscribe>`
	if got != want {
		t.Fatalf("EncodeSubscribe\n got %s\nwant %s", got, want)
	}
}

func TestEncodeObtain_DeviceList(t *testing.T) {
	items := []SubItem{
		&DeviceChange{Type: "LIST"},
	}
	got := string(EncodeObtain(9, items))
	want := `<obtain mtid="9"><device_change type="LIST"/></obtain>`
	if got != want {
		t.Fatalf("EncodeObtain\n got %s\nwant %s", got, want)
	}
}

func TestDecodeAck(t *testing.T) {
	f, err := Decode([]byte(`<ack mtid="1"/>`))
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind != KindAck || f.MTID != "1" {
		t.Fatalf("got kind=%s mtid=%q", f.Kind, f.MTID)
	}
}

func TestDecodeNack_Numeric(t *testing.T) {
	f, err := Decode([]byte(`<nack mtid="2" id="6" code="NOT_LOGGED_IN" description="a successful login is required"/>`))
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind != KindNack {
		t.Fatalf("kind: got %s want nack", f.Kind)
	}
	if f.Nack == nil || f.Nack.ID != NackNotLoggedIn {
		t.Fatalf("nack id: got %+v want NackNotLoggedIn", f.Nack)
	}
}

func TestDecodeNack_CodeOnly(t *testing.T) {
	// Some code paths may omit the numeric id; resolve via the code string.
	f, err := Decode([]byte(`<nack mtid="3" code="NO_LICENCE_AVAILABLE"/>`))
	if err != nil {
		t.Fatal(err)
	}
	if f.Nack == nil || f.Nack.ID != NackNoLicenceAvailable {
		t.Fatalf("nack id resolved from code: got %+v", f.Nack)
	}
}

func TestDecodeLoginReply(t *testing.T) {
	f, err := Decode([]byte(`<login_reply mtid="1" api_ver="0.13"/>`))
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind != KindLoginReply || f.LoginReply.APIVer != "0.13" {
		t.Fatalf("got %+v", f.LoginReply)
	}
}

func TestDecodePollReply(t *testing.T) {
	f, err := Decode([]byte(`<poll_reply mtid="5" CONNECTED_SERVER_ACTIVE="1" PRIMARY_SERVER_STATE="1" SECONDARY_SERVER_STATE="0"/>`))
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind != KindPollReply {
		t.Fatalf("kind: got %s", f.Kind)
	}
	pr := f.PollReply
	if !pr.ConnectedServerActive || !pr.PrimaryServerState || pr.SecondaryServerState {
		t.Fatalf("flags: got %+v", pr)
	}
	if !f.CaseChanged {
		t.Fatal("CaseChanged should be true (UPPERCASE attrs in spec example)")
	}
}

func TestDecodeRoutingChange_UPPERCASE(t *testing.T) {
	// Skyline driver emits UPPERCASE everywhere; we accept it.
	f, err := Decode([]byte(`<ROUTING_CHANGE TYPE="ROUTE" DEVICE_NAME="RTR-A" DEVICE_TYPE="Router" SRCE_ID="12" SRCE_NAME="CAM1" DEST_ID="34" DEST_NAME="MV1" LEVEL_ID="1" LEVEL_NAME="HD"/>`))
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind != KindRoutingChange {
		t.Fatalf("kind: got %s", f.Kind)
	}
	rc := f.Routing
	if rc.Type != "ROUTE" || rc.DeviceName != "RTR-A" || rc.SrceID != "12" || rc.LevelName != "HD" {
		t.Fatalf("got %+v", rc)
	}
	if !f.CaseChanged {
		t.Fatal("CaseChanged should be true on UPPERCASE input")
	}
}

func TestDecodeBusy(t *testing.T) {
	f, err := Decode([]byte(`<busy mtid="9"/>`))
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind != KindBusy || f.MTID != "9" {
		t.Fatalf("got kind=%s mtid=%q", f.Kind, f.MTID)
	}
}

func TestDecodeUnknown(t *testing.T) {
	// An RX root we don't recognise — Kind=Unknown, Root carries AST.
	f, err := Decode([]byte(`<some_future_event mtid="1" foo="bar"/>`))
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind != KindUnknown {
		t.Fatalf("kind: got %s want unknown", f.Kind)
	}
	if f.Root.Attr("foo") != "bar" {
		t.Fatal("root AST not populated")
	}
}

func TestNackCodeRoundtrip(t *testing.T) {
	for i := 0; i <= 13; i++ {
		c := NackCode(i)
		s := c.Code()
		if back, ok := NackCodeFromString(s); !ok || back != c {
			t.Fatalf("nack %d: %q didn't roundtrip (back=%d ok=%v)", i, s, back, ok)
		}
	}
}

func TestNackCode_BritishLicence(t *testing.T) {
	// Spec uses British "Licence", not US "License" — match exactly.
	if !strings.Contains(NackNoLicenceAvailable.Code(), "LICENCE") {
		t.Fatal("NO_LICENCE_AVAILABLE must use British spelling")
	}
}

func TestEscape_AttrAndText(t *testing.T) {
	// Make sure encoder escapes ampersands + angle brackets.
	got := string(EncodeLogin(1, `a"b<c>&d`, "p"))
	wantSubstr := `username="a&quot;b&lt;c&gt;&amp;d"`
	if !strings.Contains(got, wantSubstr) {
		t.Fatalf("expected attr-escape, got %s", got)
	}
}

func TestParseElement_PreservesAttrValueCase(t *testing.T) {
	// Element/attribute keys lowercased; values preserved.
	e, err := ParseElement([]byte(`<DEVICE_CHANGE TYPE="LIST" Device_Name="Mixed-Case-Name"/>`))
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "device_change" {
		t.Fatalf("name: got %q want device_change", e.Name)
	}
	if e.Attr("device_name") != "Mixed-Case-Name" {
		t.Fatalf("value preserved: got %q", e.Attr("device_name"))
	}
}
