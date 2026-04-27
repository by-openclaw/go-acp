package codec

import (
	"strings"
	"testing"
)

func TestEncodeLogin(t *testing.T) {
	got := string(EncodeLogin(7, "alice", "s3cr3t"))
	want := `<LOGIN USERNAME="alice" PASSWORD="s3cr3t" MTID="7"/>`
	if got != want {
		t.Fatalf("EncodeLogin\n got %s\nwant %s", got, want)
	}
}

func TestEncodePoll(t *testing.T) {
	got := string(EncodePoll(0xffff))
	want := `<POLL MTID="65535"/>`
	if got != want {
		t.Fatalf("EncodePoll\n got %s\nwant %s", got, want)
	}
}

func TestEncodeUnsubscribeAll(t *testing.T) {
	got := string(EncodeUnsubscribeAll(1))
	want := `<UNSUBSCRIBE_ALL MTID="1"/>`
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
	want := `<ACTION MTID="42"><ROUTING TYPE="ROUTE" DEVICE_NAME="RTR-A" DEVICE_TYPE="Router" SRCE_ID="12" DEST_ID="34" LEVEL_ID="1"/></ACTION>`
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
	want := `<SUBSCRIBE MTID="3"><ROUTING_CHANGE TYPE="ROUTE" DEVICE_NAME="*" DEVICE_TYPE="*"/></SUBSCRIBE>`
	if got != want {
		t.Fatalf("EncodeSubscribe\n got %s\nwant %s", got, want)
	}
}

func TestEncodeObtain_DeviceList(t *testing.T) {
	items := []SubItem{
		&DeviceChange{Type: "LIST"},
	}
	got := string(EncodeObtain(9, items))
	want := `<OBTAIN MTID="9"><DEVICE_CHANGE TYPE="LIST"/></OBTAIN>`
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

func TestDecodeNack_WireActualUPPERCASE(t *testing.T) {
	// Real Cerebrum emits UPPERCASE NACK with ERROR + ERROR_CODE
	// attribute names (verified 2026-04-26). Decoder must accept it.
	f, err := Decode([]byte(`<NACK MTID="0" ERROR="MTID_ERROR" ERROR_CODE="1"/>`))
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind != KindNack {
		t.Fatalf("kind: got %s want nack", f.Kind)
	}
	if f.Nack == nil {
		t.Fatal("nil Nack body")
	}
	if f.Nack.ID != NackMtidError {
		t.Fatalf("ID: got %d want NackMtidError(1)", f.Nack.ID)
	}
	if f.Nack.Code != "MTID_ERROR" {
		t.Fatalf("Code: got %q", f.Nack.Code)
	}
	if f.Nack.MTID != "0" {
		t.Fatalf("MTID: got %q", f.Nack.MTID)
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
	// Lowercase root + UPPERCASE attrs — mixed case fires CaseChanged.
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
		t.Fatal("CaseChanged should be true (lowercase root + UPPERCASE attrs is non-canonical)")
	}
}

func TestDecodeRoutingChange_UPPERCASE(t *testing.T) {
	// Live Cerebrum emits UPPERCASE everywhere — this is the canonical
	// wire form and should NOT fire CaseChanged.
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
	if f.CaseChanged {
		t.Fatal("CaseChanged should be false — UPPERCASE is the canonical form")
	}
}

func TestDecodeRoutingChange_lowercase_FiresCaseChanged(t *testing.T) {
	// A spec-strict lowercase server (none observed in the wild) — must
	// still parse, with CaseChanged set so the consumer can fire the
	// cerebrum_case_normalized compliance event.
	f, err := Decode([]byte(`<routing_change type="ROUTE" device_name="RTR-A"/>`))
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind != KindRoutingChange {
		t.Fatalf("kind: got %s", f.Kind)
	}
	if !f.CaseChanged {
		t.Fatal("CaseChanged should be true on lowercase wire form")
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
	wantSubstr := `USERNAME="a&quot;b&lt;c&gt;&amp;d"`
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

// ----------------------------------------------------------------------
// Live-wire fixtures (Cerebrum 10.41.64.90:40008, 2026-04-27)
// Verbatim payloads pulled from bin/cerebrum-nb-login-cmd.pcapng
// frames 131 / 3357 / 3360. They surface the spec deviations the
// initial parser missed: short IP= attribute, INSTANCE/DEVICE_TYPE
// inner child, and comma-separated LIST attribute on CATEGORY /
// GROUPS.
// ----------------------------------------------------------------------

func TestDecodeDeviceChange_LiveListShape(t *testing.T) {
	wire := `<DEVICE_CHANGE TYPE="LIST">` +
		`<DEVICE IP="10.41.63.131"><INSTANCE DEVICE_TYPE="DEVICE"/></DEVICE>` +
		`<DEVICE IP="10.80.4.13"><INSTANCE DEVICE_TYPE="DEVICE"/></DEVICE>` +
		`</DEVICE_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if f.Kind != KindDeviceChange || f.Device == nil {
		t.Fatalf("kind: %v device=%v", f.Kind, f.Device)
	}
	if f.Device.Type != "LIST" {
		t.Errorf("Type = %q", f.Device.Type)
	}
	if got, want := len(f.Device.Devices), 2; got != want {
		t.Fatalf("Devices len = %d, want %d", got, want)
	}
	d0 := f.Device.Devices[0]
	if d0.IPAddress != "10.41.63.131" {
		t.Errorf("Devices[0].IPAddress = %q (short IP= attr)", d0.IPAddress)
	}
	if d0.DeviceType != "DEVICE" {
		t.Errorf("Devices[0].DeviceType = %q (should come from INSTANCE child)", d0.DeviceType)
	}
	if d0.DeviceName != "" {
		t.Errorf("Devices[0].DeviceName = %q (server omits this attr)", d0.DeviceName)
	}
}

func TestDecodeCategoryChange_LiveCategoryListShape(t *testing.T) {
	wire := `<CATEGORY_CHANGE TYPE="CATEGORY_LIST"><CATEGORY LIST="DST_SI_FEDERATED,DST_INTERPHONIE,SRC_PLAYER"/></CATEGORY_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if f.Kind != KindCategoryChange || f.Category == nil {
		t.Fatalf("kind: %v cat=%v", f.Kind, f.Category)
	}
	if f.Category.Type != "CATEGORY_LIST" {
		t.Errorf("Type = %q", f.Category.Type)
	}
	want := []string{"DST_SI_FEDERATED", "DST_INTERPHONIE", "SRC_PLAYER"}
	if got := f.Category.Categories; !equalSlice(got, want) {
		t.Errorf("Categories = %v, want %v", got, want)
	}
}

func TestDecodeSalvoChange_LiveGroupListShape(t *testing.T) {
	wire := `<SALVO_CHANGE TYPE="GROUP_LIST"><GROUPS LIST="Salvo Group 1,Salvo Group 2"/></SALVO_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if f.Kind != KindSalvoChange || f.Salvo == nil {
		t.Fatalf("kind: %v salvo=%v", f.Kind, f.Salvo)
	}
	if f.Salvo.Type != "GROUP_LIST" {
		t.Errorf("Type = %q", f.Salvo.Type)
	}
	want := []string{"Salvo Group 1", "Salvo Group 2"}
	if got := f.Salvo.Groups; !equalSlice(got, want) {
		t.Errorf("Groups = %v, want %v", got, want)
	}
}

func TestDecodeDeviceChange_AcceptsIPAddressLongForm(t *testing.T) {
	// Spec wording uses IP_ADDRESS; we still accept it on the chance
	// some firmware revision returns the long form.
	wire := `<DEVICE_CHANGE TYPE="LIST"><DEVICE IP_ADDRESS="10.0.0.5" DEVICE_TYPE="DEVICE" DEVICE_NAME="RTR-A"/></DEVICE_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, want := len(f.Device.Devices), 1; got != want {
		t.Fatalf("Devices len = %d, want %d", got, want)
	}
	d := f.Device.Devices[0]
	if d.IPAddress != "10.0.0.5" || d.DeviceType != "DEVICE" || d.DeviceName != "RTR-A" {
		t.Errorf("entry = %+v", d)
	}
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDecodeDeviceChange_LiveDetailsShape(t *testing.T) {
	// Verbatim payload captured 2026-04-27 from real Cerebrum
	// 10.41.64.90:40008 (bin/cerebrum-nb-device-details (1).pcapng)
	// for device 10.107.30.100 (a Lawo Powercore 2M1).
	wire := `<device_change type="DETAILS" ip_address="10.107.30.100" device_type="DEVICE">` +
		`<details ip1="10.107.30.100" ip2="" name="Powercore 2M1" type="Powercore"/>` +
		`<service ip1="10.41.69.61" ip2="10.41.69.62"/>` +
		`<connection primary_state="Connection Active" secondary_state="Connection Not Configured"/>` +
		`<sub_devices/>` +
		`</device_change>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if f.Kind != KindDeviceChange || f.Device == nil {
		t.Fatalf("kind: %v device=%v", f.Kind, f.Device)
	}
	d := f.Device
	if d.Type != "DETAILS" {
		t.Errorf("Type = %q", d.Type)
	}
	if d.IPAddress != "10.107.30.100" {
		t.Errorf("IPAddress = %q", d.IPAddress)
	}
	if d.DeviceType != "DEVICE" {
		t.Errorf("DeviceType = %q", d.DeviceType)
	}
	if d.Details == nil {
		t.Fatalf("Details nil")
	}
	if d.Details.IP1 != "10.107.30.100" || d.Details.IP2 != "" {
		t.Errorf("Details ip1/ip2 = %q/%q", d.Details.IP1, d.Details.IP2)
	}
	if d.Details.Name != "Powercore 2M1" {
		t.Errorf("Details.Name = %q", d.Details.Name)
	}
	if d.Details.VendorType != "Powercore" {
		t.Errorf("Details.VendorType = %q", d.Details.VendorType)
	}
	if d.Service == nil {
		t.Fatalf("Service nil")
	}
	if d.Service.IP1 != "10.41.69.61" || d.Service.IP2 != "10.41.69.62" {
		t.Errorf("Service ip1/ip2 = %q/%q", d.Service.IP1, d.Service.IP2)
	}
	if d.Connection == nil {
		t.Fatalf("Connection nil")
	}
	if d.Connection.PrimaryState != "Connection Active" {
		t.Errorf("PrimaryState = %q", d.Connection.PrimaryState)
	}
	if d.Connection.SecondaryState != "Connection Not Configured" {
		t.Errorf("SecondaryState = %q", d.Connection.SecondaryState)
	}
	if len(d.SubDevices) != 0 {
		t.Errorf("SubDevices = %v, want empty (sub_devices was empty in capture)", d.SubDevices)
	}
}

func TestDecodeDeviceChange_LiveValueShape(t *testing.T) {
	wire := `<device_change type="VALUE" ip_address="10.107.30.100" device_name="Powercore 2M1" sub_device="0" object="foo">` +
		`<object_value available="0" object="foo"/>` +
		`</device_change>`
	f, err := Decode([]byte(wire))
	if err != nil { t.Fatalf("decode: %v", err) }
	if f.Kind != KindDeviceChange { t.Fatalf("kind: %v", f.Kind) }
	d := f.Device
	if d.Type != "VALUE" || d.IPAddress != "10.107.30.100" || d.DeviceName != "Powercore 2M1" {
		t.Errorf("outer attrs: %+v", d)
	}
	if d.SubDevice != "0" || d.Object != "foo" {
		t.Errorf("addressing: sub=%q obj=%q", d.SubDevice, d.Object)
	}
	if d.ObjectValue == nil { t.Fatalf("ObjectValue nil") }
	if d.ObjectValue.Available || d.ObjectValue.Object != "foo" {
		t.Errorf("ObjectValue: %+v", d.ObjectValue)
	}
}

func TestDecodeCategoryChange_LiveDetailsShape(t *testing.T) {
	// Server typo on the wire: descsription (sic). Decoder accepts it.
	wire := `<category_change type="CATEGORY_DETAILS" category="DST_PLAYER">` +
		`<details label="PLA" available="1" descsription=""/>` +
		`<items/>` +
		`</category_change>`
	f, err := Decode([]byte(wire))
	if err != nil { t.Fatalf("decode: %v", err) }
	c := f.Category
	if c.Type != "CATEGORY_DETAILS" || c.Category != "DST_PLAYER" {
		t.Errorf("outer attrs: %+v", c)
	}
	if c.Details == nil { t.Fatalf("Details nil") }
	if c.Details.Label != "PLA" || !c.Details.Available || c.Details.Description != "" {
		t.Errorf("Details: %+v", c.Details)
	}
}

func TestDecodeCategoryChange_DescriptionTypoFallback(t *testing.T) {
	// Future-proof: when a fixed firmware emits the correct attr, the
	// decoder picks it up just as readily.
	wire := `<category_change type="CATEGORY_DETAILS" category="X"><details label="L" available="1" description="hello"/></category_change>`
	f, err := Decode([]byte(wire))
	if err != nil { t.Fatalf("decode: %v", err) }
	if f.Category.Details.Description != "hello" {
		t.Errorf("Description = %q", f.Category.Details.Description)
	}
}

func TestDecodeSalvoChange_LiveInstanceListShape(t *testing.T) {
	// Empty list — captured live; field is nil-or-empty.
	wire := `<salvo_change type="INSTANCE_LIST" group="Salvo Group 1"><instances list=""/></salvo_change>`
	f, err := Decode([]byte(wire))
	if err != nil { t.Fatalf("decode: %v", err) }
	s := f.Salvo
	if s.Type != "INSTANCE_LIST" || s.Group != "Salvo Group 1" {
		t.Errorf("outer attrs: %+v", s)
	}
	if len(s.Instances) != 0 {
		t.Errorf("Instances = %v, want empty", s.Instances)
	}
	// Populated list (synthetic — no live capture yet, but pin the
	// CSV-split shape so when we see it we know the parser handles it).
	wire2 := `<salvo_change type="INSTANCE_LIST" group="G"><instances list="A,B,C"/></salvo_change>`
	f2, _ := Decode([]byte(wire2))
	if !equalSlice(f2.Salvo.Instances, []string{"A","B","C"}) {
		t.Errorf("populated list: %v", f2.Salvo.Instances)
	}
}

func TestDecodeSalvoChange_LiveInstanceDetailsShape(t *testing.T) {
	wire := `<salvo_change type="INSTANCE_DETAILS" group="Salvo Group 1" instance="x"><details available="0"/></salvo_change>`
	f, err := Decode([]byte(wire))
	if err != nil { t.Fatalf("decode: %v", err) }
	s := f.Salvo
	if s.Type != "INSTANCE_DETAILS" || s.Instance != "x" {
		t.Errorf("outer attrs: %+v", s)
	}
	if s.InstanceDetails == nil { t.Fatalf("InstanceDetails nil") }
	if s.InstanceDetails.Available {
		t.Errorf("Available = true, want false")
	}
}

func TestDecodeCategoryChange_LiveItemsPositional(t *testing.T) {
	// Live SOURCES capture 2026-04-27 (slots 1..25 with BLANKs interleaved).
	wire := `<category_change type="CATEGORY_DETAILS" category="SOURCES">` +
		`<details available="1" descsription="" label="SOURCES"/>` +
		`<items>` +
		`<item_1 type="CATEGORY" value="SRC_SI_GATEWAYS"/>` +
		`<item_2 type="CATEGORY" value="SRC_SI_INGEST_SRV"/>` +
		`<item_10 type="SOURCE" value="465"/>` +
		`<item_11 type="SOURCE" value="466"/>` +
		`<item_12 type="BLANK" value=""/>` +
		`<item_17 type="CATEGORY" value="SRC-TEST-YOB"/>` +
		`</items>` +
		`</category_change>`
	f, err := Decode([]byte(wire))
	if err != nil { t.Fatalf("decode: %v", err) }
	d := f.Category.Details
	if d == nil { t.Fatalf("Details nil") }
	if d.Label != "SOURCES" || !d.Available {
		t.Errorf("details: %+v", d)
	}
	// BLANK dropped, 5 non-blank items remain in their original positions.
	want := []CategoryItem{
		{Index: 1,  Type: "CATEGORY", Value: "SRC_SI_GATEWAYS"},
		{Index: 2,  Type: "CATEGORY", Value: "SRC_SI_INGEST_SRV"},
		{Index: 10, Type: "SOURCE",   Value: "465"},
		{Index: 11, Type: "SOURCE",   Value: "466"},
		{Index: 17, Type: "CATEGORY", Value: "SRC-TEST-YOB"},
	}
	if len(d.Items) != len(want) {
		t.Fatalf("Items len = %d, want %d: %+v", len(d.Items), len(want), d.Items)
	}
	for i, w := range want {
		if d.Items[i] != w {
			t.Errorf("Items[%d] = %+v, want %+v", i, d.Items[i], w)
		}
	}
}
