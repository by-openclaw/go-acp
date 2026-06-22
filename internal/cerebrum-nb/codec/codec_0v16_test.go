package codec

import (
	"strings"
	"testing"
)

// Tests in this file pin the EVS Cerebrum Northbound API 0v16 additions
// over the 0v13 baseline. Every expected byte / XML string is taken
// FROM THE 0v16 PDF worked examples (page cites inline), not from the
// codec's own output. 0v16 is a pure superset of 0v13 — the 0v13 tests
// in codec_test.go must continue to pass unchanged.
//
// Spec: internal/cerebrum-nb/assets/Cerebrum Northbound API 0v16.pdf

// ----------------------------------------------------------------------
// §1.4 CONTINUE (p5) + §1.6 WILDCARD_COMPLETE (p6) control frames.
// ----------------------------------------------------------------------

func TestDecodeContinue(t *testing.T) {
	// §1.4 p5: RX <CONTINUE/> after a BUSY.
	f, err := Decode([]byte(`<CONTINUE/>`))
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind != KindContinue {
		t.Fatalf("kind: got %s want continue", f.Kind)
	}
}

func TestDecodeWildcardComplete(t *testing.T) {
	// §1.6 p6: RX <WILDCARD_COMPLETE MTID="1"/>.
	f, err := Decode([]byte(`<WILDCARD_COMPLETE MTID="1"/>`))
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind != KindWildcardComplete {
		t.Fatalf("kind: got %s want wildcard_complete", f.Kind)
	}
	if f.MTID != "1" {
		t.Fatalf("MTID: got %q want 1", f.MTID)
	}
}

// ----------------------------------------------------------------------
// §2.1 LOGIN_REPLY with DATASTORES body (p7).
// ----------------------------------------------------------------------

func TestDecodeLoginReply_Datastores(t *testing.T) {
	// Verbatim §2.1 p7 worked example.
	wire := `<LOGIN_REPLY MTID="1" API_VER="0.1">` +
		`<DATASTORES>` +
		`<DATASTORE NAME="Global Data Files\Index.xml" AVAILABLE="1"/>` +
		`<DATASTORE NAME="Data Files\Device.xml" AVAILABLE="0"/>` +
		`</DATASTORES>` +
		`</LOGIN_REPLY>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind != KindLoginReply {
		t.Fatalf("kind: got %s", f.Kind)
	}
	lr := f.LoginReply
	if lr.MTID != "1" || lr.APIVer != "0.1" {
		t.Fatalf("MTID/APIVer: %+v", lr)
	}
	if len(lr.Datastores) != 2 {
		t.Fatalf("Datastores len = %d want 2", len(lr.Datastores))
	}
	if lr.Datastores[0].Name != `Global Data Files\Index.xml` || !lr.Datastores[0].Available {
		t.Errorf("Datastores[0] = %+v", lr.Datastores[0])
	}
	if lr.Datastores[1].Name != `Data Files\Device.xml` || lr.Datastores[1].Available {
		t.Errorf("Datastores[1] = %+v", lr.Datastores[1])
	}
}

func TestDecodeLoginReply_NoDatastores_0v13Compat(t *testing.T) {
	// 0v13 bare reply must still decode with Datastores == nil.
	f, err := Decode([]byte(`<login_reply mtid="1" api_ver="0.13"/>`))
	if err != nil {
		t.Fatal(err)
	}
	if f.LoginReply.APIVer != "0.13" || f.LoginReply.Datastores != nil {
		t.Fatalf("got %+v", f.LoginReply)
	}
}

// ----------------------------------------------------------------------
// §3.1 DEVICE_TYPE sub-device colon syntax (p9).
// ----------------------------------------------------------------------

func TestDeviceType_ColonSyntax(t *testing.T) {
	tests := []struct {
		in        DeviceType
		wantBase  DeviceType
		wantIdx   int
		wantHasIx bool
	}{
		// §3.1 p9 worked examples.
		{"ROUTER", "ROUTER", 0, false},
		{"ROUTER:2", "ROUTER", 2, true},
		{"SNMP", "SNMP", 0, false},
		{"DEVICE", "DEVICE", 0, false},
		{"DEVICE:4", "DEVICE", 4, true},
		{"DEVICE:nope", "DEVICE", 0, false}, // non-numeric suffix → no index
	}
	for _, tc := range tests {
		if got := tc.in.Base(); got != tc.wantBase {
			t.Errorf("%q.Base() = %q want %q", tc.in, got, tc.wantBase)
		}
		idx, ok := tc.in.Index()
		if ok != tc.wantHasIx || idx != tc.wantIdx {
			t.Errorf("%q.Index() = (%d,%v) want (%d,%v)", tc.in, idx, ok, tc.wantIdx, tc.wantHasIx)
		}
	}
}

func TestMakeDeviceType(t *testing.T) {
	if got := MakeDeviceType("ROUTER", 2); got != "ROUTER:2" {
		t.Errorf("MakeDeviceType(ROUTER,2) = %q want ROUTER:2", got)
	}
	if got := MakeDeviceType("SNMP", -1); got != "SNMP" {
		t.Errorf("MakeDeviceType(SNMP,-1) = %q want SNMP", got)
	}
}

// ----------------------------------------------------------------------
// §3.2 LOCK 5-value enum (p9).
// ----------------------------------------------------------------------

func TestLockKind_FiveValues(t *testing.T) {
	// §3.2 p9 table — all five canonical values present and exact.
	want := []LockKind{LockUnlocked, LockLocked, LockProtected, LockLockedPath, LockProtectedPath}
	wantStr := []string{"UNLOCKED", "LOCKED", "PROTECTED", "LOCKED_PATH", "PROTECTED_PATH"}
	for i, lk := range want {
		if string(lk) != wantStr[i] {
			t.Errorf("LockKind[%d] = %q want %q", i, lk, wantStr[i])
		}
	}
}

// ----------------------------------------------------------------------
// §4.5 Device Configuration encode (pp20-23) — the headline.
// All want-strings are derived from the worked TX examples.
// ----------------------------------------------------------------------

func TestEncodeDeviceConfiguration_GenericAdd(t *testing.T) {
	// §4.5.1.1 p20 worked example (attribute order normalised to the
	// codec's AttrsBuilder declared order, values verbatim).
	dc := &DeviceConfiguration{
		Type:       DeviceConfigAdd,
		IPAddress:  "10.10.10.1",
		DeviceType: ConfigDeviceGeneric,
		Generic: &GenericConfig{
			Device:  "Shotoku STAR Protocol",
			Version: "Latest",
			Name:    "Shotoku STAR Protocol",
			Protocol: &ProtocolConfiguration{
				ConnectionType: ConnTCP,
				PortNumber:     "8000",
				Timeout:        "5000",
				PollPeriod:     "3000",
			},
		},
	}
	got := string(EncodeDeviceConfiguration(7, dc))
	want := `<DEVICE_CONFIGURATION TYPE="ADD" IP_ADDRESS="10.10.10.1" DEVICE_TYPE="GENERIC" MTID="7">` +
		`<CONFIGURATION DEVICE="Shotoku STAR Protocol" VERSION="Latest" NAME="Shotoku STAR Protocol">` +
		`<PROTOCOL_CONFIGURATION CONNECTION_TYPE="TCP" PORT_NUMBER="8000" TIMEOUT="5000" POLL_PERIOD="3000"/>` +
		`</CONFIGURATION>` +
		`</DEVICE_CONFIGURATION>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestEncodeDeviceConfiguration_GenericModify(t *testing.T) {
	// §4.5.1.1 p20 MODIFY example carries DEVICE_NAME.
	dc := &DeviceConfiguration{
		Type:       DeviceConfigModify,
		IPAddress:  "10.10.10.1",
		DeviceName: "Shotoku STAR Protocol",
		DeviceType: ConfigDeviceGeneric,
		Generic: &GenericConfig{
			Device:  "Shotoku STAR Protocol",
			Version: "Latest",
			Name:    "Shotoku STAR Protocol New",
			Protocol: &ProtocolConfiguration{
				ConnectionType: ConnTCP,
				PortNumber:     "8000",
				Timeout:        "5000",
				PollPeriod:     "3000",
			},
		},
	}
	got := string(EncodeDeviceConfiguration(8, dc))
	want := `<DEVICE_CONFIGURATION TYPE="MODIFY" IP_ADDRESS="10.10.10.1" DEVICE_NAME="Shotoku STAR Protocol" DEVICE_TYPE="GENERIC" MTID="8">` +
		`<CONFIGURATION DEVICE="Shotoku STAR Protocol" VERSION="Latest" NAME="Shotoku STAR Protocol New">` +
		`<PROTOCOL_CONFIGURATION CONNECTION_TYPE="TCP" PORT_NUMBER="8000" TIMEOUT="5000" POLL_PERIOD="3000"/>` +
		`</CONFIGURATION>` +
		`</DEVICE_CONFIGURATION>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestEncodeDeviceConfiguration_GenericWithProtocolSpecific(t *testing.T) {
	// §4.5.1.1 p20 — optional <PROTOCOL_SPECIFIC> child; content is
	// protocol-dependent so we carry raw inner XML. Empty inner emits
	// the open/close pair.
	empty := ""
	dc := &DeviceConfiguration{
		Type:       DeviceConfigAdd,
		IPAddress:  "10.10.10.1",
		DeviceType: ConfigDeviceGeneric,
		Generic: &GenericConfig{
			Device:           "X",
			Protocol:         &ProtocolConfiguration{ConnectionType: ConnUDP, PortNumber: "9000"},
			ProtocolSpecific: &empty,
		},
	}
	got := string(EncodeDeviceConfiguration(1, dc))
	want := `<DEVICE_CONFIGURATION TYPE="ADD" IP_ADDRESS="10.10.10.1" DEVICE_TYPE="GENERIC" MTID="1">` +
		`<CONFIGURATION DEVICE="X">` +
		`<PROTOCOL_CONFIGURATION CONNECTION_TYPE="UDP" PORT_NUMBER="9000"/>` +
		`<PROTOCOL_SPECIFIC></PROTOCOL_SPECIFIC>` +
		`</CONFIGURATION>` +
		`</DEVICE_CONFIGURATION>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestEncodeDeviceConfiguration_PanelAdd(t *testing.T) {
	// §4.5.1.2 p21 worked example.
	dc := &DeviceConfiguration{
		Type:       DeviceConfigAdd,
		IPAddress:  "10.10.40.1",
		DeviceType: ConfigDevicePanel,
		Panel: &PanelConfig{
			Device:  "CCP-1601",
			Version: "Latest",
			Name:    "Panel",
			Protocol: &ProtocolConfiguration{
				ConnectionType: ConnTCP,
				PortNumber:     "8000",
			},
			CPF:       "DEFAULT.cpf",
			PanelType: "CCP-1601",
		},
	}
	got := string(EncodeDeviceConfiguration(3, dc))
	want := `<DEVICE_CONFIGURATION TYPE="ADD" IP_ADDRESS="10.10.40.1" DEVICE_TYPE="PANEL" MTID="3">` +
		`<CONFIGURATION DEVICE="CCP-1601" VERSION="Latest" NAME="Panel">` +
		`<PROTOCOL_CONFIGURATION CONNECTION_TYPE="TCP" PORT_NUMBER="8000"/>` +
		`<PROTOCOL_SPECIFIC CPF="DEFAULT.cpf" PANEL_TYPE="CCP-1601">` +
		`<PANEL_CONFIG></PANEL_CONFIG>` +
		`</PROTOCOL_SPECIFIC>` +
		`</CONFIGURATION>` +
		`</DEVICE_CONFIGURATION>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestEncodeDeviceConfiguration_RouterAdd(t *testing.T) {
	// §4.5.1.3 p22 worked example: bare <SERIAL/> + <ROUTER …/>.
	dc := &DeviceConfiguration{
		Type:       DeviceConfigAdd,
		IPAddress:  "10.10.20.1",
		DeviceName: "Blackmagic Videohub TCP",
		DeviceType: ConfigDeviceRouter,
		Router: &RouterConfig{
			Device: "Blackmagic Videohub TCP",
			Name:   "Blackmagic Videohub TCP",
			Type:   "20",
			Serial: &SerialConfig{},
			Router: &RouterParams{
				MaxLevel:  "8",
				MaxSource: "128",
				MaxDest:   "128",
			},
		},
	}
	got := string(EncodeDeviceConfiguration(4, dc))
	want := `<DEVICE_CONFIGURATION TYPE="ADD" IP_ADDRESS="10.10.20.1" DEVICE_NAME="Blackmagic Videohub TCP" DEVICE_TYPE="ROUTER" MTID="4">` +
		`<CONFIGURATION DEVICE="Blackmagic Videohub TCP" NAME="Blackmagic Videohub TCP" TYPE="20">` +
		`<SERIAL/>` +
		`<ROUTER MAX_LEVEL="8" MAX_SOURCE="128" MAX_DEST="128"/>` +
		`</CONFIGURATION>` +
		`</DEVICE_CONFIGURATION>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestEncodeDeviceConfiguration_SNMPAdd(t *testing.T) {
	// §4.5.1.4 p23 worked example.
	dc := &DeviceConfiguration{
		Type:       DeviceConfigAdd,
		IPAddress:  "10.10.30.1",
		DeviceType: ConfigDeviceSNMP,
		SNMP:       &SNMPConfig{Name: "SNMP Device", Port: "161"},
	}
	got := string(EncodeDeviceConfiguration(5, dc))
	want := `<DEVICE_CONFIGURATION TYPE="ADD" IP_ADDRESS="10.10.30.1" DEVICE_TYPE="SNMP" MTID="5">` +
		`<CONFIGURATION NAME="SNMP Device" PORT="161"/>` +
		`</DEVICE_CONFIGURATION>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestEncodeAction_DeviceSetValue_0v16(t *testing.T) {
	// §4.4.1 p19 worked example: IS_ENUM + SUB_DEVICE + MODE, addressed
	// by IP_ADDRESS instead of DEVICE_NAME. IS_ENUM emits only when true;
	// MODE/IP_ADDRESS emit when non-empty. Attr order is the codec's
	// deterministic order (XML attr order is not significant).
	body := &DeviceAction{
		Type:      "SET_VALUE",
		IPAddress: "10.0.0.5",
		SubDevice: "1",
		Object:    "PSU.Fan Control",
		Value:     "Automatic",
		IsEnum:    true,
		Mode:      "SET",
	}
	got := string(EncodeAction(1, body))
	want := `<ACTION MTID="1"><DEVICE TYPE="SET_VALUE" IP_ADDRESS="10.0.0.5" SUB_DEVICE="1" OBJECT="PSU.Fan Control" VALUE="Automatic" MODE="SET" IS_ENUM="1"/></ACTION>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestEncodeDeviceConfiguration_Remove(t *testing.T) {
	// §4.5.2 p23 worked example: self-closing, no body.
	dc := &DeviceConfiguration{
		Type:       DeviceConfigRemove,
		IPAddress:  "10.10.30.1",
		DeviceType: ConfigDeviceSNMP,
	}
	got := string(EncodeDeviceConfiguration(6, dc))
	want := `<DEVICE_CONFIGURATION TYPE="REMOVE" IP_ADDRESS="10.10.30.1" DEVICE_TYPE="SNMP" MTID="6"/>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestEncodeDeviceConfiguration_AddNoBodyStruct(t *testing.T) {
	// ADD with no body struct set → self-closing (defensive: caller
	// supplied only the envelope).
	dc := &DeviceConfiguration{
		Type:       DeviceConfigAdd,
		IPAddress:  "10.0.0.9",
		DeviceType: ConfigDeviceGeneric,
	}
	got := string(EncodeDeviceConfiguration(2, dc))
	want := `<DEVICE_CONFIGURATION TYPE="ADD" IP_ADDRESS="10.0.0.9" DEVICE_TYPE="GENERIC" MTID="2"/>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestEncodeDeviceConfiguration_RouterNoChildren(t *testing.T) {
	// Router body with neither <SERIAL> nor <ROUTER> set → empty
	// <CONFIGURATION>…</CONFIGURATION> (exercises both nil branches).
	dc := &DeviceConfiguration{
		Type:       DeviceConfigAdd,
		IPAddress:  "10.0.0.1",
		DeviceType: ConfigDeviceRouter,
		Router:     &RouterConfig{Name: "R"},
	}
	got := string(EncodeDeviceConfiguration(1, dc))
	want := `<DEVICE_CONFIGURATION TYPE="ADD" IP_ADDRESS="10.0.0.1" DEVICE_TYPE="ROUTER" MTID="1">` +
		`<CONFIGURATION NAME="R"></CONFIGURATION>` +
		`</DEVICE_CONFIGURATION>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestEncodeDeviceConfiguration_GenericNoProtocol(t *testing.T) {
	// Generic body without <PROTOCOL_CONFIGURATION> → exercises the nil
	// protocol branch and the no-PROTOCOL_SPECIFIC path.
	dc := &DeviceConfiguration{
		Type:       DeviceConfigAdd,
		IPAddress:  "10.0.0.2",
		DeviceType: ConfigDeviceGeneric,
		Generic:    &GenericConfig{Name: "G"},
	}
	got := string(EncodeDeviceConfiguration(1, dc))
	want := `<DEVICE_CONFIGURATION TYPE="ADD" IP_ADDRESS="10.0.0.2" DEVICE_TYPE="GENERIC" MTID="1">` +
		`<CONFIGURATION NAME="G"></CONFIGURATION>` +
		`</DEVICE_CONFIGURATION>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestEncodeDeviceConfiguration_RouterFullSerial(t *testing.T) {
	// Exercise every <SERIAL> + <ROUTER> attribute branch (p22 tables).
	dc := &DeviceConfiguration{
		Type:       DeviceConfigModify,
		IPAddress:  "10.10.20.1",
		DeviceName: "R",
		DeviceType: ConfigDeviceRouter,
		Router: &RouterConfig{
			Device:      "Dev",
			Name:        "R New",
			Type:        "13",
			SecondaryIP: "10.10.20.2",
			Serial: &SerialConfig{
				Baud: "9600", Parity: "NONE", DataBits: "8",
				StopBitsIP: "1", EnableDSOM: "1",
			},
			Router: &RouterParams{
				MaxLevel: "4", MaxSource: "64", MaxDest: "64",
				IPPort: "9990", MasterServer: "1",
			},
		},
	}
	got := string(EncodeDeviceConfiguration(9, dc))
	want := `<DEVICE_CONFIGURATION TYPE="MODIFY" IP_ADDRESS="10.10.20.1" DEVICE_NAME="R" DEVICE_TYPE="ROUTER" MTID="9">` +
		`<CONFIGURATION DEVICE="Dev" NAME="R New" TYPE="13" SECONDARY_IP="10.10.20.2">` +
		`<SERIAL BAUD="9600" PARITY="NONE" DATA_BITS="8" STOP_BITS_IP="1" ENABLE_DSOM="1"/>` +
		`<ROUTER MAX_LEVEL="4" MAX_SOURCE="64" MAX_DEST="64" IP_PORT="9990" MASTER_SERVER="1"/>` +
		`</CONFIGURATION>` +
		`</DEVICE_CONFIGURATION>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestEncodeDeviceConfiguration_ProtocolConfigFullAttrs(t *testing.T) {
	// Exercise VIRTUALISE + SECONDARY_IP branches of PROTOCOL_CONFIGURATION.
	dc := &DeviceConfiguration{
		Type:       DeviceConfigAdd,
		IPAddress:  "10.0.0.1",
		DeviceType: ConfigDeviceGeneric,
		Generic: &GenericConfig{
			Device: "D",
			Protocol: &ProtocolConfiguration{
				ConnectionType: ConnWebsocketServer,
				PortNumber:     "80",
				Timeout:        "1000",
				PollPeriod:     "500",
				Virtualise:     "1",
				SecondaryIP:    "10.0.0.2",
			},
		},
	}
	got := string(EncodeDeviceConfiguration(1, dc))
	if !strings.Contains(got, `CONNECTION_TYPE="WEBSOCKET_SERVER" PORT_NUMBER="80" TIMEOUT="1000" POLL_PERIOD="500" VIRTUALISE="1" SECONDARY_IP="10.0.0.2"`) {
		t.Fatalf("protocol attrs not all emitted: %s", got)
	}
}

func TestEncodeDeviceConfiguration_PanelWithIDAndInner(t *testing.T) {
	// Exercise PANEL_ID branch + non-empty PANEL_CONFIG inner.
	dc := &DeviceConfiguration{
		Type:       DeviceConfigAdd,
		IPAddress:  "10.10.40.1",
		DeviceType: ConfigDevicePanel,
		Panel: &PanelConfig{
			Device:           "CCP",
			CPF:              "X.cpf",
			PanelID:          "12",
			PanelType:        "CCP-1601",
			PanelConfigInner: `<KEY ID="1"/>`,
		},
	}
	got := string(EncodeDeviceConfiguration(1, dc))
	want := `<DEVICE_CONFIGURATION TYPE="ADD" IP_ADDRESS="10.10.40.1" DEVICE_TYPE="PANEL" MTID="1">` +
		`<CONFIGURATION DEVICE="CCP">` +
		`<PROTOCOL_SPECIFIC CPF="X.cpf" PANEL_ID="12" PANEL_TYPE="CCP-1601">` +
		`<PANEL_CONFIG><KEY ID="1"/></PANEL_CONFIG>` +
		`</PROTOCOL_SPECIFIC>` +
		`</CONFIGURATION>` +
		`</DEVICE_CONFIGURATION>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

// ----------------------------------------------------------------------
// §4.5 RESULT reply decode (pp20-23).
// ----------------------------------------------------------------------

func TestDecodeDeviceConfigResult_Accepted(t *testing.T) {
	// §4.5.1.1 p20 RX worked example.
	wire := `<DEVICE_CONFIGURATION TYPE="ADD" IP_ADDRESS="10.10.10.1" DEVICE_TYPE="GENERIC">` +
		`<RESULT VALUE="ACCEPTED"/>` +
		`</DEVICE_CONFIGURATION>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind != KindDeviceConfigResult {
		t.Fatalf("kind: got %s want device_config_result", f.Kind)
	}
	r := f.DeviceConfig
	if r.Type != "ADD" || r.IPAddress != "10.10.10.1" || r.DeviceType != "GENERIC" {
		t.Errorf("envelope: %+v", r)
	}
	if r.Value != "ACCEPTED" || !r.Accepted {
		t.Errorf("result: %+v", r)
	}
}

func TestDecodeDeviceConfigResult_Failed(t *testing.T) {
	// §4.5.1.1 p20: <RESULT VALUE="FAILED"/>.
	wire := `<DEVICE_CONFIGURATION TYPE="ADD" IP_ADDRESS="10.10.10.1" DEVICE_TYPE="GENERIC">` +
		`<RESULT VALUE="FAILED"/>` +
		`</DEVICE_CONFIGURATION>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	if f.DeviceConfig.Accepted || f.DeviceConfig.Value != "FAILED" {
		t.Fatalf("result: %+v", f.DeviceConfig)
	}
}

func TestDecodeDeviceConfigResult_NoResultChild(t *testing.T) {
	// Defensive: envelope with no <RESULT> child — Value empty, not accepted.
	wire := `<DEVICE_CONFIGURATION TYPE="REMOVE" IP="10.0.0.1" DEVICE_TYPE="SNMP"/>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	if f.DeviceConfig.IPAddress != "10.0.0.1" || f.DeviceConfig.Value != "" || f.DeviceConfig.Accepted {
		t.Fatalf("result: %+v", f.DeviceConfig)
	}
}

// ----------------------------------------------------------------------
// §5.1.2 / §5.1.3 lock-change RX detail (p24).
// ----------------------------------------------------------------------

func TestDecodeRoutingChange_SrceLock_ValueChild(t *testing.T) {
	// §5.1.2 p24: SRCE_LOCK detail in a <VALUE …/> child.
	wire := `<ROUTING_CHANGE TYPE="SRCE_LOCK" DEVICE_NAME="MTX1" DEVICE_TYPE="ROUTER" SRCE_ID="1" LEVEL_NAME="UHD">` +
		`<VALUE AVAILABLE="1" LOCK_STATE="LOCKED" LOCKED_BY="Admin" UNLOCK_TIME="" UNLOCK_DATE=""/>` +
		`</ROUTING_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	lk := f.Routing.Lock
	if lk == nil {
		t.Fatal("Lock nil")
	}
	if !lk.Available || lk.LockState != LockLocked || lk.LockedBy != "Admin" {
		t.Errorf("Lock = %+v", lk)
	}
}

func TestDecodeRoutingChange_DestLock_LockChild(t *testing.T) {
	// §5.1.3 p24: DEST_LOCK detail in a <LOCK …/> child.
	wire := `<ROUTING_CHANGE TYPE="DEST_LOCK" DEVICE_NAME="MTX1" DEVICE_TYPE="ROUTER" DEST_NAME="MON-1" LEVEL_ID="2">` +
		`<LOCK AVAILABLE="1" LOCK_STATE="LOCKED" LOCKED_BY="Admin" UNLOCK_TIME="" UNLOCK_DATE=""/>` +
		`</ROUTING_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	lk := f.Routing.Lock
	if lk == nil {
		t.Fatal("Lock nil")
	}
	if !lk.Available || lk.LockState != LockLocked || lk.LockedBy != "Admin" {
		t.Errorf("Lock = %+v", lk)
	}
}

// ----------------------------------------------------------------------
// §5.1.4 Level Mnemonic RX: ORIGINAL_MNE + RM_DEFAULT_DEVICE (p25).
// ----------------------------------------------------------------------

func TestDecodeRoutingChange_LevelMne_Rich(t *testing.T) {
	// §5.1.4 p25 worked example.
	wire := `<ROUTING_CHANGE TYPE="LEVEL_MNE" DEVICE_NAME="MTX1" IP_ADDRESS="192.168.0.1" DEVICE_TYPE="ROUTER" LEVEL_ID="2">` +
		`<MNE AVAILABLE="1" MNEMONIC="HD" ORIGINAL_MNE="VIDEO_HD"/>` +
		`<ALT_MNE_1 MNEMONIC="V_HD" AVAILABLE="1"/>` +
		`<ALT_MNE_3 MNEMONIC="VHD" AVAILABLE="1"/>` +
		`<ALT_MNE_4 AVAILABLE="0"/>` +
		`<RM_DEFAULT_DEVICE DEVICE_NAME="Hybrid Router" DEVICE_TYPE="ROUTER" LEVEL_ID="1"/>` +
		`</ROUTING_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	r := f.Routing
	if r.Mnemonic != "HD" || r.OriginalMne != "VIDEO_HD" {
		t.Errorf("mne: mnemonic=%q original=%q", r.Mnemonic, r.OriginalMne)
	}
	if r.Mnemonics[1] != "V_HD" || r.Mnemonics[3] != "VHD" {
		t.Errorf("alt mnemonics: %+v", r.Mnemonics)
	}
	// ALT_MNE_4 AVAILABLE="0" must be dropped.
	if _, ok := r.Mnemonics[4]; ok {
		t.Errorf("alt_mne_4 (available=0) should be dropped: %+v", r.Mnemonics)
	}
	if r.RMDefaultDevice == nil {
		t.Fatal("RMDefaultDevice nil")
	}
	if r.RMDefaultDevice.DeviceName != "Hybrid Router" || r.RMDefaultDevice.DeviceType != "ROUTER" || r.RMDefaultDevice.LevelID != "1" {
		t.Errorf("RMDefaultDevice = %+v", r.RMDefaultDevice)
	}
}

// ----------------------------------------------------------------------
// §5.1.5 Source Mnemonic RX: ASSOCIATIONS with SRCE_ID + RM_* (p25-26).
// ----------------------------------------------------------------------

func TestDecodeRoutingChange_SrceMne_Associations(t *testing.T) {
	// §5.1.5 pp25-26 worked example.
	wire := `<ROUTING_CHANGE TYPE="SRCE_MNE" DEVICE_NAME="MTX1" DEVICE_TYPE="ROUTER" SRCE_NAME="CAM-1" LEVEL_ID="1">` +
		`<MNE AVAILABLE="1" MNEMONIC="CAM-1" ORIGINAL_MNE="SRCE-1"/>` +
		`<ALT_MNE_1 MNEMONIC="Camera-1" AVAILABLE="1"/>` +
		`<ALT_MNE_2 MNEMONIC="Harry" AVAILABLE="1"/>` +
		`<ASSOCIATIONS>` +
		`<ASSOCIATION_1 SRCE_ID="1" RM_DEVICE_NAME="Hybrid Router" RM_DEVICE_TYPE="ROUTER" RM_LEVEL_ID="1" RM_REVERSED="0" RM_TAGS="Tag1" RM_IP_UID=""/>` +
		`<ASSOCIATION_35 SRCE_ID="2" RM_DEVICE_NAME="Audio Router" RM_DEVICE_TYPE="ROUTER" RM_LEVEL_ID="13" RM_REVERSED="0" RM_TAGS="Tag2" RM_IP_UID=""/>` +
		`</ASSOCIATIONS>` +
		`</ROUTING_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	r := f.Routing
	if r.OriginalMne != "SRCE-1" {
		t.Errorf("original_mne = %q", r.OriginalMne)
	}
	if len(r.Associations) != 2 {
		t.Fatalf("associations len = %d want 2", len(r.Associations))
	}
	a0 := r.Associations[0]
	if a0.Index != 1 || a0.SrceID != "1" || a0.RMDeviceName != "Hybrid Router" || a0.RMLevelID != "1" || a0.RMTags != "Tag1" {
		t.Errorf("Associations[0] = %+v", a0)
	}
	a1 := r.Associations[1]
	if a1.Index != 35 || a1.SrceID != "2" || a1.RMDeviceName != "Audio Router" || a1.RMLevelID != "13" || a1.RMTags != "Tag2" {
		t.Errorf("Associations[1] = %+v", a1)
	}
}

func TestDecodeRoutingChange_DestMne_Associations(t *testing.T) {
	// §5.1.6 p26: DEST_ID-keyed associations (no RM_TAGS in the example).
	wire := `<ROUTING_CHANGE TYPE="DEST_MNE" DEVICE_NAME="MTX1" DEVICE_TYPE="ROUTER" DEST_ID="1" LEVEL_ID="2">` +
		`<MNE AVAILABLE="1" MNEMONIC="MON-1" ORIGINAL_MNE="DEST-1"/>` +
		`<ALT_MNE_1 MNEMONIC="Harrys Desk" AVAILABLE="1"/>` +
		`<ASSOCIATIONS>` +
		`<ASSOCIATION_1 DEST_ID="1" RM_DEVICE_NAME="Hybrid Router" RM_DEVICE_TYPE="ROUTER" RM_LEVEL_ID="1" RM_REVERSED="0" RM_IP_UID=""/>` +
		`</ASSOCIATIONS>` +
		`</ROUTING_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	r := f.Routing
	if len(r.Associations) != 1 {
		t.Fatalf("associations len = %d want 1", len(r.Associations))
	}
	a := r.Associations[0]
	if a.Index != 1 || a.DestID != "1" || a.SrceID != "" || a.RMDeviceName != "Hybrid Router" {
		t.Errorf("Associations[0] = %+v", a)
	}
}

// ----------------------------------------------------------------------
// §5.1.7 / §5.1.8 Routemaster Tags RX: pipe-delimited LIST (p27).
// ----------------------------------------------------------------------

func TestDecodeRoutingChange_SrceTags_Pipe(t *testing.T) {
	// §5.1.7 p27: <TAGS AVAILABLE="1" LIST="Tag1|Tag2"/> — PIPE delimiter.
	wire := `<ROUTING_CHANGE TYPE="RM_SRCE_TAGS" SRCE_ID="1" LEVEL_ID="1">` +
		`<TAGS AVAILABLE="1" LIST="Tag1|Tag2"/>` +
		`</ROUTING_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	r := f.Routing
	if !r.TagsAvailable {
		t.Error("TagsAvailable should be true")
	}
	if !equalSlice(r.TagList, []string{"Tag1", "Tag2"}) {
		t.Errorf("TagList = %v want [Tag1 Tag2]", r.TagList)
	}
}

func TestDecodeRoutingChange_DestTags_Pipe_NoAvailable(t *testing.T) {
	// §5.1.8 p27: <TAGS LIST="Tag1|Tag2"/> — no AVAILABLE attr in example.
	wire := `<ROUTING_CHANGE TYPE="RM_DEST_TAGS" DEST_ID="1" LEVEL_ID="1">` +
		`<TAGS LIST="Tag1|Tag2"/>` +
		`</ROUTING_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	if f.Routing.TagsAvailable {
		t.Error("TagsAvailable should be false (attr absent)")
	}
	if !equalSlice(f.Routing.TagList, []string{"Tag1", "Tag2"}) {
		t.Errorf("TagList = %v", f.Routing.TagList)
	}
}

// ----------------------------------------------------------------------
// §5.2.2 Category item optional GROUP attr (p28).
// ----------------------------------------------------------------------

func TestDecodeCategoryChange_ItemGroup(t *testing.T) {
	// §5.2.2 p28 note: items may carry a GROUP attribute.
	wire := `<CATEGORY_CHANGE TYPE="CATEGORY_DETAILS" CATEGORY="EVS">` +
		`<DETAILS AVAILABLE="1" LABEL="EVS Sources" DESCRIPTION="All EVS Sources"/>` +
		`<ITEMS>` +
		`<ITEM_1 TYPE="SALVO" VALUE="Line up" GROUP="Multiviewer 1"/>` +
		`</ITEMS>` +
		`</CATEGORY_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	items := f.Category.Details.Items
	if len(items) != 1 {
		t.Fatalf("items len = %d want 1", len(items))
	}
	if items[0].Group != "Multiviewer 1" || items[0].Type != "SALVO" || items[0].Value != "Line up" {
		t.Errorf("item = %+v", items[0])
	}
}

// ----------------------------------------------------------------------
// §5.3.3 Salvo Instance Details enriched (p28).
// ----------------------------------------------------------------------

func TestDecodeSalvoChange_InstanceDetails_Rich(t *testing.T) {
	// §5.3.3 p28 worked example.
	wire := `<SALVO_CHANGE TYPE="INSTANCE_DETAILS" GROUP="Multiviewer 1" INSTANCE="Line up">` +
		`<DETAILS AVAILABLE="1" DESCRIPTION="The layout for line up" ACTIVE="1" DATE="02/07/2017" TIME="17:33:22:12"/>` +
		`</SALVO_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	d := f.Salvo.InstanceDetails
	if d == nil {
		t.Fatal("InstanceDetails nil")
	}
	if !d.Available || d.Description != "The layout for line up" || !d.Active {
		t.Errorf("details = %+v", d)
	}
	if d.Date != "02/07/2017" || d.Time != "17:33:22:12" {
		t.Errorf("date/time = %q / %q", d.Date, d.Time)
	}
}

// ----------------------------------------------------------------------
// §5.4.3 Device Value rich OBJECT_VALUE (p29).
// ----------------------------------------------------------------------

func TestDecodeDeviceChange_Value_RichObjectValue(t *testing.T) {
	// §5.4.3 p29 scalar worked example.
	wire := `<DEVICE_CHANGE TYPE="VALUE" IP_ADDRESS="10.1.1.1" SUB_DEVICE="0" OBJECT="Status.Connected">` +
		`<OBJECT_VALUE OBJECT="Status.Connected" VALUE="1" AVAILABLE="1" DATA_TYPE="INTEGER" READABLE="1" WRITABLE="0" UNITS="" LABEL="" DEFAULT="0" ENUM_LIST="On,Off"/>` +
		`</DEVICE_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Device.ObjectValues) != 1 {
		t.Fatalf("ObjectValues len = %d want 1", len(f.Device.ObjectValues))
	}
	ov := f.Device.ObjectValues[0]
	if ov.Object != "Status.Connected" || ov.Value != "1" || !ov.Available {
		t.Errorf("ov core = %+v", ov)
	}
	if ov.DataType != "INTEGER" || !ov.Readable || ov.Writable {
		t.Errorf("ov flags = %+v", ov)
	}
	if ov.Default != "0" || !equalSlice(ov.EnumList, []string{"On", "Off"}) {
		t.Errorf("ov default/enum = %q / %v", ov.Default, ov.EnumList)
	}
	// Backwards-compat accessor points at first entry.
	if f.Device.ObjectValue == nil || f.Device.ObjectValue.Object != "Status.Connected" {
		t.Errorf("ObjectValue accessor = %+v", f.Device.ObjectValue)
	}
}

func TestDecodeDeviceChange_Value_GroupMultipleObjectValues(t *testing.T) {
	// §5.4.3 p29 group worked example — multiple OBJECT_VALUE children.
	wire := `<DEVICE_CHANGE TYPE="VALUE" IP_ADDRESS="10.1.1.1" SUB_DEVICE="0" OBJECT="Status">` +
		`<OBJECT_VALUE OBJECT="Status.Connected" VALUE="1" AVAILABLE="1" DATA_TYPE="INTEGER" READABLE="1" WRITABLE="0" UNITS="" LABEL="" DEFAULT="0" ENUM_LIST="On,Off"/>` +
		`<OBJECT_VALUE OBJECT="Status.Version" VALUE="1.0.0.1" AVAILABLE="1" DATA_TYPE="STRING" READABLE="1" WRITABLE="0" UNITS="" LABEL="" DEFAULT="" ENUM_LIST=""/>` +
		`</DEVICE_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Device.ObjectValues) != 2 {
		t.Fatalf("ObjectValues len = %d want 2", len(f.Device.ObjectValues))
	}
	if f.Device.ObjectValues[1].Object != "Status.Version" || f.Device.ObjectValues[1].DataType != "STRING" {
		t.Errorf("ObjectValues[1] = %+v", f.Device.ObjectValues[1])
	}
}

func TestDecodeDeviceChange_Value_WildcardTable(t *testing.T) {
	// §5.4.3 p29 wildcard table path Devices.[*] worked example.
	wire := `<DEVICE_CHANGE TYPE="VALUE" IP_ADDRESS="10.1.1.1" SUB_DEVICE="0" OBJECT="Devices.[*]">` +
		`<OBJECT_VALUE OBJECT="Name" VALUE="Device1" AVAILABLE="1" DATA_TYPE="STRING" READABLE="1" WRITABLE="0" UNITS="" LABEL="" DEFAULT="" ENUM_LIST=""/>` +
		`<OBJECT_VALUE OBJECT="Firmware Version" VALUE="1.0.0.2" AVAILABLE="1" DATA_TYPE="STRING" READABLE="1" WRITABLE="0" UNITS="" LABEL="" DEFAULT="" ENUM_LIST=""/>` +
		`</DEVICE_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	if f.Device.Object != "Devices.[*]" {
		t.Errorf("Object = %q", f.Device.Object)
	}
	if len(f.Device.ObjectValues) != 2 || f.Device.ObjectValues[1].Object != "Firmware Version" {
		t.Errorf("ObjectValues = %+v", f.Device.ObjectValues)
	}
}

// ----------------------------------------------------------------------
// §5.5.1 Data Store obtain TX + reply (p30) — PATH/NAME ambiguity.
// ----------------------------------------------------------------------

func TestEncodeDatastoreObtain_EmitsPATH(t *testing.T) {
	// §5.5.1 p30 TX example uses PATH=. We emit PATH (+ optional MTID).
	items := []SubItem{
		&DatastoreChange{Path: `Global Data Files\Index.xml`, MTID: "1"},
	}
	got := string(EncodeObtain(1, items))
	want := `<OBTAIN MTID="1"><DATASTORE_CHANGE PATH="Global Data Files\Index.xml" MTID="1"/></OBTAIN>`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestDecodeDatastoreObtain_AcceptsPATHandNAME(t *testing.T) {
	// Decode must accept either spelling (ambiguity resolution).
	for _, attr := range []string{"PATH", "NAME"} {
		wire := `<DATASTORE_CHANGE ` + attr + `="store\x.xml" TYPE="ATTRIBUTE"/>`
		f, err := Decode([]byte(wire))
		if err != nil {
			t.Fatalf("%s: %v", attr, err)
		}
		if f.Datastore.Path != `store\x.xml` {
			t.Errorf("%s: Path = %q", attr, f.Datastore.Path)
		}
		if f.Datastore.Name != `store\x.xml` {
			t.Errorf("%s: Name(alias) = %q", attr, f.Datastore.Name)
		}
	}
}

func TestDecodeDatastore_ReplyWithDataBody(t *testing.T) {
	// §5.5.1 p30 reply: <DATASTORE_CHANGE TYPE="FILE" AVAILABLE="1">
	// <DATA>…</DATA></DATASTORE_CHANGE>.
	wire := `<DATASTORE_CHANGE TYPE="FILE" AVAILABLE="1">` +
		`<DATA><PRESETS><PRESET_1 NAME="Active"/></PRESETS></DATA>` +
		`</DATASTORE_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	d := f.Datastore
	if d.Type != "FILE" || !d.Available {
		t.Errorf("type/available = %q / %v", d.Type, d.Available)
	}
	if d.Terminator {
		t.Error("reply with DATA body should NOT be a terminator")
	}
	// Body re-rendered (case-folded). Structure + values preserved.
	if !strings.Contains(d.Data, `presets`) || !strings.Contains(d.Data, `name="Active"`) {
		t.Errorf("Data = %q", d.Data)
	}
}

func TestDecodeDatastore_BareTerminator(t *testing.T) {
	// §5.5.1 p30: the <DATASTORE_CHANGE MTID="1"/> terminator that
	// follows the DATA reply.
	f, err := Decode([]byte(`<DATASTORE_CHANGE MTID="1"/>`))
	if err != nil {
		t.Fatal(err)
	}
	d := f.Datastore
	if !d.Terminator {
		t.Errorf("should be terminator: %+v", d)
	}
	if d.MTID != "1" || d.Type != "" {
		t.Errorf("terminator = %+v", d)
	}
}

func TestDecodeDatastore_ChangeEvent(t *testing.T) {
	// §5.5.2 p31 worked example.
	wire := `<DATASTORE_CHANGE NAME="Global Data Files\Index.xml" TYPE="ATTRIBUTE">` +
		`<ELEMENT ATTRIBUTE="Attribute" PATH="Data\Element" VALUE="Value" AVAILABLE="1" CHILD_COUNT="0"/>` +
		`</DATASTORE_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	d := f.Datastore
	if d.Path != `Global Data Files\Index.xml` || d.Type != "ATTRIBUTE" {
		t.Errorf("event envelope = %+v", d)
	}
	if d.Terminator {
		t.Error("change event should not be a terminator")
	}
	if d.Element == nil {
		t.Fatal("Element nil")
	}
	el := d.Element
	if el.Attribute != "Attribute" || el.Path != `Data\Element` || el.Value != "Value" || !el.Available || el.ChildCount != "0" {
		t.Errorf("Element = %+v", el)
	}
}

// ----------------------------------------------------------------------
// 0v13 regression: the original encoder/decoder behaviour is unchanged.
// ----------------------------------------------------------------------

func TestDatastoreSubscribe_0v16_PATH_overCSV(t *testing.T) {
	// Ensure TAGS pipe split and category CSV split stay independent:
	// a comma inside a tag value is NOT a delimiter.
	wire := `<ROUTING_CHANGE TYPE="RM_SRCE_TAGS" SRCE_ID="1" LEVEL_ID="1"><TAGS LIST="Tag,A|Tag B"/></ROUTING_CHANGE>`
	f, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	if !equalSlice(f.Routing.TagList, []string{"Tag,A", "Tag B"}) {
		t.Errorf("TagList = %v want [Tag,A Tag B]", f.Routing.TagList)
	}
}
