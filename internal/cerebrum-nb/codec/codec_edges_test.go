package codec

import "testing"

// Defensive-branch coverage: malformed-but-tolerated wire shapes that
// the parsers absorb (skip a bad child, fall through to Unknown) rather
// than erroring. These pin the spec-deviation-absorption posture
// (root CLAUDE.md "Spec-strict, no-workaround").

func TestParseRoutingChange_BadAltMneAndAssocIndexes(t *testing.T) {
	// alt_mne_X / association_X with non-numeric suffixes must be
	// skipped, not crash. The good siblings still parse.
	wire := `<ROUTING_CHANGE TYPE="SRCE_MNE" SRCE_ID="1" LEVEL_ID="1">` +
		`<MNE AVAILABLE="1" MNEMONIC="M"/>` +
		`<ALT_MNE_X MNEMONIC="bad" AVAILABLE="1"/>` + // non-numeric → skipped
		`<ALT_MNE_2 MNEMONIC="ok" AVAILABLE="1"/>` +
		`<ASSOCIATIONS>` +
		`<ASSOCIATION_X SRCE_ID="9"/>` + // non-numeric → skipped
		`<NOT_AN_ASSOC FOO="1"/>` + // wrong prefix → skipped
		`<ASSOCIATION_7 SRCE_ID="7"/>` +
		`</ASSOCIATIONS>` +
		`</ROUTING_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	r := f.Routing
	if r.Mnemonics[2] != "ok" {
		t.Errorf("alt_mne_2 = %q", r.Mnemonics[2])
	}
	if _, bad := r.Mnemonics[0]; !bad { // slot 0 from <MNE>
		t.Errorf("primary mne missing: %+v", r.Mnemonics)
	}
	if len(r.Associations) != 1 || r.Associations[0].Index != 7 {
		t.Fatalf("associations = %+v", r.Associations)
	}
}

func TestParseCategoryChange_BadItemChildren(t *testing.T) {
	// A non-item_ child and an item_ child with a non-numeric suffix are
	// both skipped; the valid item survives.
	wire := `<CATEGORY_CHANGE TYPE="CATEGORY_DETAILS" CATEGORY="C">` +
		`<DETAILS AVAILABLE="1" LABEL="L"/>` +
		`<ITEMS>` +
		`<NOISE FOO="1"/>` + // no item_ prefix → skipped
		`<ITEM_X TYPE="SOURCE" VALUE="bad"/>` + // non-numeric → skipped
		`<ITEM_3 TYPE="SOURCE" VALUE="ok"/>` +
		`</ITEMS>` +
		`</CATEGORY_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	items := f.Category.Details.Items
	if len(items) != 1 || items[0].Index != 3 || items[0].Value != "ok" {
		t.Fatalf("items = %+v", items)
	}
}

func TestParseDeviceChange_ValueChangeEventTopLevelAttr(t *testing.T) {
	// Live NOC frame 2026-08-16 (CONVERT audio delay, VALUE SUBSCRIBE):
	// change events carry the new value as a TOP-LEVEL attribute with no
	// OBJECT_VALUE child — the decoder synthesizes one so consumers see a
	// single shape.
	wire := `<DEVICE_CHANGE TYPE="VALUE" IP_ADDRESS="10.44.72.28" DEVICE_NAME="bm-n-nncvt-001 " SUB_DEVICE="1" OBJECT="PROCESSING AUDIO.AUDIO DELAY.BANK 1.Delay" VALUE="2.000000"/>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	d := f.Device
	if d == nil || len(d.ObjectValues) != 1 {
		t.Fatalf("ObjectValues = %+v", d)
	}
	ov := d.ObjectValues[0]
	if ov.Value != "2.000000" || !ov.Available || ov.Object != "PROCESSING AUDIO.AUDIO DELAY.BANK 1.Delay" {
		t.Errorf("synthesized value = %+v", ov)
	}
}

func TestParseDeviceChange_ValueDescriptorMinMaxStep(t *testing.T) {
	// Live NOC snapshot 2026-08-16: the full descriptor carries MIN/MAX/
	// STEP range attrs on FLOAT objects.
	wire := `<DEVICE_CHANGE TYPE="VALUE" IP_ADDRESS="10.44.72.28" DEVICE_NAME="bm-n-nncvt-001 " SUB_DEVICE="1" OBJECT="PROCESSING AUDIO.AUDIO DELAY.BANK 1.Delay">` +
		`<OBJECT_VALUE OBJECT="PROCESSING AUDIO.AUDIO DELAY.BANK 1.Delay" VALUE="0.000000" AVAILABLE="1" DATA_TYPE="FLOAT" READABLE="1" WRITABLE="1" MIN="0.000000" MAX="3000.000000" STEP="1.000000" DEFAULT="0.000000"/>` +
		`</DEVICE_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	ov := f.Device.ObjectValue
	if ov == nil || ov.Min != "0.000000" || ov.Max != "3000.000000" || ov.Step != "1.000000" || ov.Default != "0.000000" {
		t.Errorf("descriptor = %+v", ov)
	}
}

func TestParseDeviceChange_DetailsPositionalSubDevices(t *testing.T) {
	// Live NOC frame 2026-08-16 (Neuron shelf bm-n-nnshf-004, DEVICE-class
	// view): SUB_DEVICES carries positional <DEVICE_N TYPE="model"
	// PRIMARY_STATE SECONDARY_STATE/> children — DEVICE_N like ITEM_N /
	// ASSOCIATION_N, TYPE = sub-device model (class-filtered: the ROUTER-
	// class sub-device of the same shelf is absent from the DEVICE view).
	wire := `<DEVICE_CHANGE TYPE="DETAILS" IP_ADDRESS="10.44.72.27" DEVICE_TYPE="DEVICE">` +
		`<DETAILS IP1="10.44.72.27" IP2="" NAME="bm-n-nnshf-004" TYPE="bm-n-nnshf-004"/>` +
		`<SERVICE/>` +
		`<CONNECTION PRIMARY_STATE="Connection Active" SECONDARY_STATE="Connection Not Configured"/>` +
		`<SUB_DEVICES><DEVICE_1 TYPE="SHUFFLE-256" PRIMARY_STATE="Connection Active" SECONDARY_STATE="Connection Not Configured"/></SUB_DEVICES>` +
		`</DEVICE_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	d := f.Device
	if d == nil || len(d.SubDevices) != 1 {
		t.Fatalf("SubDevices = %+v", d)
	}
	sd := d.SubDevices[0]
	if sd.Index != 1 || sd.DeviceName != "SHUFFLE-256" ||
		sd.PrimaryState != "Connection Active" || sd.SecondaryState != "Connection Not Configured" {
		t.Errorf("sub-device = %+v", sd)
	}
}

func TestParseDeviceChange_DetailsWithPopulatedSubDevices(t *testing.T) {
	// §5.4.2 p29-style DETAILS with a populated <SUB_DEVICES> containing
	// <DEVICE> children — exercises the sub-device loop + the
	// instance/back-compat arms (device-level DEVICE_TYPE present and
	// absent).
	wire := `<DEVICE_CHANGE TYPE="DETAILS" IP_ADDRESS="10.1.1.1" DEVICE_TYPE="GENERIC">` +
		`<DETAILS IP1="10.1.1.1" IP2="10.1.1.2" NAME="Synapse" TYPE="Synapse"/>` +
		`<SERVICE IP1="172.10.1.100" IP2="172.10.2.100"/>` +
		`<CONNECTION PRIMARY_STATE="ACTIVE" SECONDARY_STATE="UP"/>` +
		`<SUB_DEVICES>` +
		`<DEVICE IP="10.2.0.1" DEVICE_TYPE="DEVICE" DEVICE_NAME="Card1"/>` + // back-compat: DeviceType already set
		`<DEVICE IP="10.2.0.2"><INSTANCE DEVICE_TYPE="ROUTER"/></DEVICE>` + // back-compat: DeviceType from instance
		`</SUB_DEVICES>` +
		`</DEVICE_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	d := f.Device
	if d.Details == nil || d.Service == nil || d.Connection == nil {
		t.Fatalf("nested nil: %+v", d)
	}
	if len(d.SubDevices) != 2 {
		t.Fatalf("sub-devices = %+v", d.SubDevices)
	}
	if d.SubDevices[0].DeviceType != "DEVICE" || d.SubDevices[0].DeviceName != "Card1" {
		t.Errorf("sub[0] = %+v", d.SubDevices[0])
	}
	if d.SubDevices[1].DeviceType != "ROUTER" {
		t.Errorf("sub[1] back-compat DeviceType = %q", d.SubDevices[1].DeviceType)
	}
}

func TestDecode_MalformedXMLErrors(t *testing.T) {
	// Decode wraps a ParseElement failure (transport-level XML error).
	if _, err := Decode([]byte(`<ACK><UNCLOSED>`)); err == nil {
		t.Fatal("expected decode error on malformed XML")
	}
}

func TestParseRoutingChange_MneUnavailableAndRouteChild(t *testing.T) {
	// <MNE AVAILABLE="0"> is dropped (only ORIGINAL_MNE captured), and a
	// TYPE=ROUTE row carries a <ROUTE> source child (§5.1.1 p23-24).
	wire := `<ROUTING_CHANGE TYPE="ROUTE" DEVICE_NAME="MTX1" DEVICE_TYPE="ROUTER" DEST_ID="1" LEVEL_ID="2">` +
		`<MNE AVAILABLE="0" ORIGINAL_MNE="GONE"/>` +
		`<ROUTE AVAILABLE="1" SOURCE_ID="1" SOURCE_LEVEL_ID="1"/>` +
		`</ROUTING_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	r := f.Routing
	if r.OriginalMne != "GONE" {
		t.Errorf("original_mne = %q", r.OriginalMne)
	}
	if _, ok := r.Mnemonics[0]; ok {
		t.Errorf("unavailable MNE should be dropped: %+v", r.Mnemonics)
	}
	if r.RouteSourceID != "1" || r.RouteSourceLevelID != "1" || !r.RouteAvailable {
		t.Errorf("route = id:%q lvl:%q avail:%v", r.RouteSourceID, r.RouteSourceLevelID, r.RouteAvailable)
	}
}

func TestParseRoutingChange_AltMneWithoutPrimaryMne(t *testing.T) {
	// An <ALT_MNE_N> arriving with no preceding <MNE> must still
	// lazily-init the Mnemonics map (the nil-init arm in the alt branch).
	wire := `<ROUTING_CHANGE TYPE="SRCE_MNE" SRCE_ID="1" LEVEL_ID="1">` +
		`<ALT_MNE_2 MNEMONIC="solo" AVAILABLE="1"/>` +
		`</ROUTING_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	if f.Routing.Mnemonics[2] != "solo" {
		t.Fatalf("mnemonics = %+v", f.Routing.Mnemonics)
	}
}

func TestParseDeviceChange_ListEntryWithExplicitDeviceType(t *testing.T) {
	// A LIST <DEVICE> carrying DEVICE_TYPE directly (no <INSTANCE>) — the
	// back-compat "DeviceType already set" branch.
	wire := `<DEVICE_CHANGE TYPE="LIST"><DEVICE IP="10.0.0.1" DEVICE_TYPE="SNMP"/></DEVICE_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	if f.Device.Devices[0].DeviceType != "SNMP" || len(f.Device.Devices[0].DeviceTypes) != 0 {
		t.Fatalf("entry = %+v", f.Device.Devices[0])
	}
}
