package codec

import (
	"encoding/xml"
	"errors"
	"strings"
	"testing"
)

// TestParseRoot_NonEOFTokenError exercises the defensive non-EOF error
// arm of parseRoot's pre-root token loop via the nextToken test seam.
// This arm can't be reached through real input (see the seam comment in
// element.go), so the seam is overridden here to inject a non-EOF error.
func TestParseRoot_NonEOFTokenError(t *testing.T) {
	orig := nextToken
	defer func() { nextToken = orig }()
	want := errors.New("boom")
	nextToken = func(*xml.Decoder) (xml.Token, error) { return nil, want }

	_, err := ParseElement([]byte(`<ACK/>`))
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want wrapping %v", err, want)
	}
}

// This file completes branch coverage of the codec, exercising the
// §4.2-§4.4 action encoders, §5 subscribe-item encoders, the §6 NACK
// table accessors, and the element-AST edge cases that the feature
// tests don't reach. Where the bytes are on-wire, the want-strings are
// derived from the 0v16 worked examples (page cites inline); for pure
// helpers the assertions pin documented behaviour.

// ----------------------------------------------------------------------
// FrameKind.String — every arm including the unknown fallthrough.
// ----------------------------------------------------------------------

func TestFrameKindString_All(t *testing.T) {
	tests := []struct {
		k    FrameKind
		want string
	}{
		{KindLoginReply, "login_reply"},
		{KindPollReply, "poll_reply"},
		{KindAck, "ack"},
		{KindNack, "nack"},
		{KindBusy, "busy"},
		{KindContinue, "continue"},
		{KindWildcardComplete, "wildcard_complete"},
		{KindDeviceConfigResult, "device_config_result"},
		{KindRoutingChange, "routing_change"},
		{KindCategoryChange, "category_change"},
		{KindSalvoChange, "salvo_change"},
		{KindDeviceChange, "device_change"},
		{KindDatastoreChange, "datastore_change"},
		{KindUnknown, "unknown"},
		{FrameKind(9999), "unknown"},
	}
	for _, tc := range tests {
		if got := tc.k.String(); got != tc.want {
			t.Errorf("FrameKind(%d).String() = %q want %q", tc.k, got, tc.want)
		}
	}
}

// ----------------------------------------------------------------------
// §4.2 / §4.3 / §4.4 action encoders.
// ----------------------------------------------------------------------

func TestEncodeAction_Category(t *testing.T) {
	// §4.2.1 p16 Modify Item worked example — EXCEPT the item type:
	// the spec example writes ITEM_TYPE="SOURCE", but the live server
	// NACKs 8 on it and accepts only "SRCE" (staging RM 2026-08-18;
	// reads still report "SOURCE"). The encoder emits the wire-actual
	// spelling — see TestEncodeCategoryAction_SourceSpelling.
	body := &CategoryAction{
		Type:     "MODIFY_ITEM",
		Category: "SERVERS",
		Index:    "4",
		ItemType: ItemSource,
		Value:    "SRVR-1",
	}
	got := string(EncodeAction(1, body))
	want := `<ACTION MTID="1"><CATEGORY TYPE="MODIFY_ITEM" CATEGORY="SERVERS" INDEX="4" ITEM_TYPE="SRCE" VALUE="SRVR-1"/></ACTION>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestEncodeAction_CategoryCreate(t *testing.T) {
	// §4.2.4 p16 Create Category — exercises NAME/LABEL/INHERITS/DESCRIPTION.
	body := &CategoryAction{
		Type:        "CREATE",
		Name:        "SERVERS",
		Label:       "Servers",
		Inherits:    "PARENT_CATEGORY",
		Description: "All Servers",
	}
	got := string(EncodeAction(1, body))
	want := `<ACTION MTID="1"><CATEGORY TYPE="CREATE" NAME="SERVERS" LABEL="Servers" INHERITS="PARENT_CATEGORY" DESCRIPTION="All Servers"/></ACTION>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestEncodeAction_Salvo(t *testing.T) {
	// §4.3.3 p18 Rename worked example.
	body := &SalvoAction{
		Type:     "RENAME",
		Group:    "Multiviewer 1",
		Instance: "Line up",
		NewName:  "Line-up",
	}
	got := string(EncodeAction(1, body))
	want := `<ACTION MTID="1"><SALVO TYPE="RENAME" GROUP="Multiviewer 1" INSTANCE="Line up" NEW_NAME="Line-up"/></ACTION>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestEncodeAction_SalvoDescription(t *testing.T) {
	// §4.3.4 p18 Set Description — exercises DESCRIPTION arm.
	body := &SalvoAction{
		Type:        "DESCRIPTION",
		Group:       "Multiviewer 1",
		Instance:    "Line up",
		Description: "The layout for line up",
	}
	got := string(EncodeAction(1, body))
	want := `<ACTION MTID="1"><SALVO TYPE="DESCRIPTION" GROUP="Multiviewer 1" INSTANCE="Line up" DESCRIPTION="The layout for line up"/></ACTION>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestEncodeAction_Device(t *testing.T) {
	// §4.4.1 p19 Set Value (DEVICE_NAME + SUB_DEVICE + OBJECT + VALUE).
	body := &DeviceAction{
		Type:       "SET_VALUE",
		DeviceName: "Cam1",
		SubDevice:  "1",
		Object:     "PSU.Fan Control",
		Value:      "Automatic",
	}
	got := string(EncodeAction(1, body))
	want := `<ACTION MTID="1"><DEVICE TYPE="SET_VALUE" DEVICE_NAME="Cam1" SUB_DEVICE="1" OBJECT="PSU.Fan Control" VALUE="Automatic"/></ACTION>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestEncodeAction_RoutingFullAttrs(t *testing.T) {
	// Exercise the association + tag attribute arms of RoutingAction
	// (§4.1.7 p13 Source Association + §4.2.1 p15 RM tags fields).
	body := &RoutingAction{
		Type:               "SRCE_ASSOC",
		IPAddress:          "10.0.0.1",
		SrceName:           "CAM",
		SrceLevelID:        "1",
		SrceLevelName:      "HD",
		DestName:           "MON",
		DestLevelID:        "2",
		DestLevelName:      "UHD",
		UseTags:            "1",
		LevelName:          "L",
		Lock:               LockLocked,
		Duration:           "1000",
		Mnemonic:           "M",
		AltMne:             "1",
		OnDevice:           "0",
		LogicalSrceID:      "199",
		LogicalDestID:      "200",
		LogicalLevelID:     "2",
		TargetDeviceName:   "Hybrid",
		TargetDeviceType:   ConfigDeviceTypeRouterAlias(),
		TargetLevelID:      "1",
		TargetSrceID:       "100",
		TargetDestID:       "101",
		TargetSenderName:   "atx01",
		TargetReceiverName: "ARX-1",
		SubDevice:          "0",
		Tags:               "Tag1",
	}
	got := string(EncodeAction(1, body))
	// Spot-check that the association + tag arms all serialised.
	for _, sub := range []string{
		`LOGICAL_SRCE_ID="199"`, `LOGICAL_DEST_ID="200"`, `TARGET_SENDER_NAME="atx01"`,
		`TARGET_RECEIVER_NAME="ARX-1"`, `SUB_DEVICE="0"`, `TAGS="Tag1"`, `LOCK="LOCKED"`,
	} {
		if !strings.Contains(got, sub) {
			t.Errorf("missing %s in %s", sub, got)
		}
	}
}

// ConfigDeviceTypeRouterAlias returns the §3.1 DeviceType for ROUTER as
// used by routing association targets (kept local to avoid leaking a
// test-only const into the package surface).
func ConfigDeviceTypeRouterAlias() DeviceType { return DeviceType("ROUTER") }

// ----------------------------------------------------------------------
// §5 subscribe-item encoders (Category / Salvo / Device / Datastore).
// ----------------------------------------------------------------------

func TestEncodeSubscribe_CategoryChange(t *testing.T) {
	// §5.2.2 p27 Category Details subscribe.
	items := []SubItem{&CategoryChange{Type: "CATEGORY_DETAILS", Category: "EVS"}}
	got := string(EncodeSubscribe(1, items))
	want := `<SUBSCRIBE MTID="1"><CATEGORY_CHANGE TYPE="CATEGORY_DETAILS" CATEGORY="EVS"/></SUBSCRIBE>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestEncodeSubscribe_SalvoChange(t *testing.T) {
	// §5.3.3 p28 Instance Details subscribe.
	items := []SubItem{&SalvoChange{Type: "INSTANCE_DETAILS", Group: "Multiviewer 1", Instance: "Line up"}}
	got := string(EncodeSubscribe(1, items))
	want := `<SUBSCRIBE MTID="1"><SALVO_CHANGE TYPE="INSTANCE_DETAILS" GROUP="Multiviewer 1" INSTANCE="Line up"/></SUBSCRIBE>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestEncodeSubscribe_DeviceChangeValue(t *testing.T) {
	// §5.4.3 p29 Device Value subscribe.
	items := []SubItem{&DeviceChange{Type: "VALUE", DeviceName: "Camera1", SubDevice: "0", Object: "Status.Connected"}}
	got := string(EncodeSubscribe(1, items))
	want := `<SUBSCRIBE MTID="1"><DEVICE_CHANGE TYPE="VALUE" DEVICE_NAME="Camera1" SUB_DEVICE="0" OBJECT="Status.Connected"/></SUBSCRIBE>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestEncodeUnsubscribe_RoutingChange(t *testing.T) {
	// §2.4 p8 UNSUBSCRIBE wraps the same item shape as SUBSCRIBE.
	items := []SubItem{&RoutingChange{Type: "ROUTE", DeviceName: "MTX1", DeviceType: DeviceType("ROUTER")}}
	got := string(EncodeUnsubscribe(2, items))
	want := `<UNSUBSCRIBE MTID="2"><ROUTING_CHANGE TYPE="ROUTE" DEVICE_NAME="MTX1" DEVICE_TYPE="ROUTER"/></UNSUBSCRIBE>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

// ----------------------------------------------------------------------
// §6 NACK table accessors.
// ----------------------------------------------------------------------

func TestNackCode_Accessors(t *testing.T) {
	if NackNotLoggedIn.Code() != "NOT_LOGGED_IN" {
		t.Errorf("Code = %q", NackNotLoggedIn.Code())
	}
	if NackNotLoggedIn.Description() != "a successful login is required" {
		t.Errorf("Description = %q", NackNotLoggedIn.Description())
	}
	if !strings.Contains(NackNotLoggedIn.String(), "NOT_LOGGED_IN") {
		t.Errorf("String = %q", NackNotLoggedIn.String())
	}
}

func TestNackCode_OutOfRange(t *testing.T) {
	// Defensive arms for an id outside the §6 table.
	bad := NackCode(999)
	if bad.Code() != "UNKNOWN" {
		t.Errorf("Code = %q want UNKNOWN", bad.Code())
	}
	if bad.Description() != "" {
		t.Errorf("Description = %q want empty", bad.Description())
	}
	if !strings.Contains(bad.String(), "UNKNOWN") {
		t.Errorf("String = %q", bad.String())
	}
	negative := NackCode(-1)
	if negative.Code() != "UNKNOWN" || negative.Description() != "" {
		t.Errorf("negative accessors: %q / %q", negative.Code(), negative.Description())
	}
}

func TestNackCodeFromString_Miss(t *testing.T) {
	if _, ok := NackCodeFromString("NO_SUCH_CODE"); ok {
		t.Error("expected miss for unknown code")
	}
}

func TestNackError_Error(t *testing.T) {
	resolved := &NackError{ID: NackMtidError, Code: "MTID_ERROR"}
	if !strings.Contains(resolved.Error(), "MTID_ERROR") {
		t.Errorf("resolved Error = %q", resolved.Error())
	}
	unresolved := &NackError{ID: -1, Code: "WEIRD", Message: "huh"}
	if !strings.Contains(unresolved.Error(), "WEIRD") || !strings.Contains(unresolved.Error(), "huh") {
		t.Errorf("unresolved Error = %q", unresolved.Error())
	}
}

func TestParseNack_DescriptionFallbackToText(t *testing.T) {
	// description carried as element text, id resolved via code string.
	f, err := Decode([]byte(`<nack mtid="1" code="NOT_LOGGED_IN">login first</nack>`))
	if err != nil {
		t.Fatal(err)
	}
	if f.Nack.ID != NackNotLoggedIn || f.Nack.Message != "login first" {
		t.Fatalf("nack = %+v", f.Nack)
	}
}

// ----------------------------------------------------------------------
// element.go edge cases.
// ----------------------------------------------------------------------

func TestElement_NilSafety(t *testing.T) {
	var e *Element
	if e.Attr("x") != "" {
		t.Error("nil.Attr should be empty")
	}
	if e.Child("x") != nil {
		t.Error("nil.Child should be nil")
	}
	if e.ChildrenNamed("x") != nil {
		t.Error("nil.ChildrenNamed should be nil")
	}
	if e.String() != "" {
		t.Error("nil.String should be empty")
	}
}

func TestParseElement_EmptyXML(t *testing.T) {
	if _, err := ParseElement([]byte("   ")); err == nil {
		t.Error("expected error for empty XML")
	}
}

func TestParseElement_SkipsCommentsAndProcInst(t *testing.T) {
	// Leading <?xml?>, a comment, then the root with a child comment.
	wire := `<?xml version="1.0"?><!-- top --><ACK MTID="1"><!-- inner -->text</ACK>`
	e, err := ParseElement([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "ack" || e.Attr("mtid") != "1" || e.Text != "text" {
		t.Fatalf("parsed = %+v", e)
	}
}

func TestParseElement_MalformedReturnsError(t *testing.T) {
	if _, err := ParseElement([]byte(`<ACK><UNCLOSED>`)); err == nil {
		t.Error("expected parse error for unclosed element")
	}
}

func TestElement_StringRoundtrip(t *testing.T) {
	// String() renders attrs + children + text, escaping reserved chars
	// in text. (Attr map order is non-deterministic, so assert on a
	// single-attr element.)
	e := &Element{
		Name:  "root",
		Attrs: map[string]string{"k": `a&b`},
		Children: []*Element{
			{Name: "child", Attrs: map[string]string{}, Text: "x<y>&z"},
		},
	}
	got := e.String()
	if !strings.Contains(got, `k="a&amp;b"`) {
		t.Errorf("attr escape missing: %s", got)
	}
	if !strings.Contains(got, "x&lt;y&gt;&amp;z") {
		t.Errorf("text escape missing: %s", got)
	}
	if !strings.Contains(got, "<child") {
		t.Errorf("child missing: %s", got)
	}
}

func TestXMLEscapeAttr_AllSpecials(t *testing.T) {
	// Drive every arm of xmlEscapeAttr via a single-attr element.
	e := &Element{Name: "x", Attrs: map[string]string{"a": "&<>\"'\n\r\t"}}
	got := e.String()
	for _, want := range []string{"&amp;", "&lt;", "&gt;", "&quot;", "&apos;", "&#10;", "&#13;", "&#9;"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
}

func TestEmitRawChild_WithAttrs(t *testing.T) {
	// emitRawChild with non-nil attrs (the PANEL_SPECIFIC path covers the
	// attr-bearing branch; assert escaping + verbatim inner here).
	var b strings.Builder
	emitRawChild(&b, "WRAP", []Attr{{"K", `a&b`}}, `<INNER/>`)
	got := b.String()
	want := `<WRAP K="a&amp;b"><INNER/></WRAP>`
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}
