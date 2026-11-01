package consumer

import (
	"context"
	"maps"
	"strings"
	"testing"
	"time"

	dhsc "dhs/internal/consumer"
	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/mib"
	"dhs/internal/transport"
)

// answersWalk is answers plus GETNEXT and GETBULK over the same values, so
// the ENTITY-MIB row can be found.
func answersWalk(vals map[string]codec.Value) func(codec.Message) codec.Message {
	get := answers(vals)
	return func(req codec.Message) codec.Message {
		if req.PDU.Type != codec.PDUTypeGetBulk && req.PDU.Type != codec.PDUTypeGetNext {
			return get(req)
		}
		out := get(req)
		from := req.PDU.VarBinds[0].Name
		var best *codec.OID
		for k := range vals {
			o := codec.MustParseOID(k)
			if o.Compare(from) > 0 && (best == nil || o.Compare(*best) < 0) {
				best = &o
			}
		}
		if best == nil {
			out.PDU.VarBinds = []codec.VarBind{{Name: from, Value: codec.EndOfMIBView()}}
			return out
		}
		out.PDU.VarBinds = []codec.VarBind{{Name: *best, Value: vals[best.String()]}}
		return out
	}
}

func identified(t *testing.T, mibName string, vals map[string]codec.Value) *Plugin {
	t.Helper()
	p := scriptedPlugin(t, &scriptedNet{Net: transport.New(transport.Config{}), reply: answersWalk(vals)})
	p.SetMIB(mibName)
	if err := p.Connect(context.Background(), "127.0.0.1", 161); err != nil {
		t.Fatal(err)
	}
	return p
}

// slotIdentity is what `info` prints for the live device.
func slotIdentity(t *testing.T, p *Plugin) map[string]string {
	t.Helper()
	si, err := p.GetSlotInfo(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	return si.Identity
}

// A switch with no table of its own, the way an Arista reads under
// --mib standard.
func chassisDevice() map[string]codec.Value {
	row := func(col codec.OID) string { return col.Append(1).String() }
	return map[string]codec.Value{
		sysDescr.String():                          codec.String("Some OS version 4.34.3.1M running on a box"),
		sysObjectID.String():                       codec.ObjectID(codec.MustParseOID("1.3.6.1.4.1.30065.1.3011.7060")),
		mib.EntPhysicalClass.Append(1).String():    codec.Int(mib.EntPhysicalChassis),
		row(mib.EntPhysicalModelName):              codec.String("DCS-7060CX-32S"),
		row(mib.EntPhysicalSerialNum):              codec.String("SSJ1"),
		row(mib.EntPhysicalSoftwareRev):            codec.String("4.34.3.1M"),
		row(mib.EntPhysicalFirmwareRev):            codec.String("Aboot-1"),
		row(mib.EntPhysicalMfgName):                codec.String("Arista Networks"),
		mib.EntPhysicalClass.Append(0).String():    codec.Int(9),
		mib.EntPhysicalClass.Append(2).String():    codec.Int(9),
		codec.MustParseOID("1.3.6.1.9.9").String(): codec.Int(1),
	}
}

func TestAChassisRowNamesADeviceWithNoTable(t *testing.T) {
	p := identified(t, mib.BaseTable, chassisDevice())
	ctx := context.Background()
	if got, err := p.IdentityProbe(ctx, 0); err != nil || got != "DCS-7060CX-32S@4.34.3.1M" {
		t.Fatalf("%q, %v", got, err)
	}
	id := slotIdentity(t, p)
	if !strings.HasPrefix(id["constructor"], "Arista Networks") || id["pen"] != "30065" || id["serial"] != "SSJ1" ||
		id["firmware"] != "Aboot-1" || id["table"] != mib.BaseTable || id["sys_object_id"] == "" {
		t.Errorf("%v", id)
	}
	// What an operator does next.
	for _, want := range []string{"PEN 30065", "no MIB loaded", "by-protocol/mib", "https://mibbrowser.online/mibs/", "mibdb_search.php"} {
		if !strings.Contains(id["next"], want) {
			t.Errorf("next = %q, want %q", id["next"], want)
		}
	}
	// Slot 0 carries it, which is what `info` prints.
	si, err := p.GetSlotInfo(ctx, 0)
	if err != nil || si.Identity["model"] != "DCS-7060CX-32S" {
		t.Errorf("%+v, %v", si.Identity, err)
	}
}

// Read once a connection: the slot is asked often and the agent is not.
func TestTheIdentityIsReadOnce(t *testing.T) {
	vals := chassisDevice()
	p := identified(t, mib.BaseTable, vals)
	ctx := context.Background()
	if got, _ := p.IdentityProbe(ctx, 0); got == "" {
		t.Fatal("no identity")
	}
	vals[mib.EntPhysicalModelName.Append(1).String()] = codec.String("other")
	if got, _ := p.IdentityProbe(ctx, 0); got != "DCS-7060CX-32S@4.34.3.1M" {
		t.Errorf("read twice: %q", got)
	}
}

// With no enterprise number to look up, the row's own maker is the word.
func TestAChassisRowCanNameTheMaker(t *testing.T) {
	vals := chassisDevice()
	vals[sysObjectID.String()] = codec.ObjectID(codec.MustParseOID("1.3.6.1.2.1.1"))
	p := identified(t, mib.BaseTable, vals)
	id := slotIdentity(t, p)
	if id["constructor"] != "Arista Networks" || id["next"] != "" || id["pen"] != "" {
		t.Errorf("%v", id)
	}
}

// No software column: the firmware column stands in, a version is a version.
func TestAChassisWithNoSoftwareRevUsesItsFirmware(t *testing.T) {
	vals := chassisDevice()
	delete(vals, mib.EntPhysicalSoftwareRev.Append(1).String())
	p := identified(t, mib.BaseTable, vals)
	if got, _ := p.IdentityProbe(context.Background(), 0); got != "DCS-7060CX-32S@Aboot-1" {
		t.Errorf("%q", got)
	}
}

// The floor: every agent has a system group. sysObjectID is the model, and
// sysDescr gives the version when it says one in words.
func TestTheSystemGroupIsTheFloor(t *testing.T) {
	vals := map[string]codec.Value{
		sysDescr.String():    codec.String("Acme thing, Version 12.2(55)SE, RELEASE"),
		sysObjectID.String(): codec.ObjectID(codec.MustParseOID("1.3.6.1.4.1.9.1.516")),
	}
	p := identified(t, mib.BaseTable, vals)
	if got, _ := p.IdentityProbe(context.Background(), 0); got != "1.3.6.1.4.1.9.1.516@12.2(55)SE" {
		t.Errorf("%q", got)
	}
}

// A sysObjectID the table has a word for is named by that word.
func TestAProductRegistrationNamesTheModel(t *testing.T) {
	vals := map[string]codec.Value{
		sysDescr.String():    codec.String("Linux box 5.10"),
		sysObjectID.String(): codec.ObjectID(mib.DHSAgent),
	}
	p := identified(t, mib.BaseTable, vals)
	got, _ := p.IdentityProbe(context.Background(), 0)
	if got != "dhsAgent" {
		t.Errorf("%q", got)
	}
	if id := slotIdentity(t, p); id["next"] != "" {
		t.Errorf("our own table is loaded: %q", id["next"])
	}
}

// An enterprise number nobody registered still sends the operator looking.
func TestANumberNotInTheRegistryIsSaid(t *testing.T) {
	vals := map[string]codec.Value{
		sysDescr.String():    codec.String("x"),
		sysObjectID.String(): codec.ObjectID(codec.MustParseOID("1.3.6.1.4.1.4294967295.1")),
	}
	p := identified(t, mib.BaseTable, vals)
	if id := slotIdentity(t, p); !strings.Contains(id["next"], "not in the IANA registry") {
		t.Errorf("%v", id)
	}
}

// A device that said nothing for now is asked again.
func TestAnAgentThatDidNotAnswerIsNotRemembered(t *testing.T) {
	vals := chassisDevice()
	p := identified(t, mib.BaseTable, vals)
	if err := p.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if got := p.DeviceIdentity(context.Background()); got != nil {
		t.Errorf("no session = %v", got)
	}
}

func TestReadTextSaysWhetherTheAgentAnswered(t *testing.T) {
	a := newFilledAgent(1)
	addr := scriptedAgent(t, a.answer)
	s := dial(t, addr, Options{Version: codec.Version2c, Timeout: time.Second})
	if got, ok := readText(context.Background(), s, boundAfter); got != "" || !ok {
		t.Errorf("not text: %q %v", got, ok)
	}
	a.mu.Lock()
	a.failOn = boundAfter.String()
	a.mu.Unlock()
	if _, ok := readText(context.Background(), s, boundAfter); ok {
		t.Error("a refused read did not answer")
	}
	a.mu.Lock()
	a.failOn, a.v1 = "", true
	a.mu.Unlock()
	if _, ok := readText(context.Background(), s, boundAfter.Append(9)); !ok {
		t.Error("noSuchName is an answer")
	}
}

func TestAddressesAreShownAsAddresses(t *testing.T) {
	def := &mib.Object{Base: "OCTET STRING", TC: "InetAddress"}
	v := named(def, toNeutral(codec.Bytes([]byte{10, 6, 255, 114})))
	if v.Str != "10.6.255.114" {
		t.Errorf("%q", v.Str)
	}
	// A DisplayString that happens to be four bytes stays what it is.
	text := &mib.Object{Base: "OCTET STRING", TC: "DisplayString"}
	if v := named(text, toNeutral(codec.String("eth0"))); v.Str != "eth0" {
		t.Errorf("%q", v.Str)
	}
	if v := named(def, toNeutral(codec.Gauge32(3))); v.Uint != 3 {
		t.Errorf("%+v", v)
	}
}

func TestAnAddressIsWrittenAsItsOctets(t *testing.T) {
	p := identified(t, "ateme-dr5000", map[string]codec.Value{sysDescr.String(): codec.String("x")})
	o := codec.MustParseOID("1.3.6.1.4.1.27338.5.3.2.2.2.1.0") // ...InputIpAddress, InetAddress
	v, err := p.wireValue(o, dhsc.Value{Kind: dhsc.KindString, Str: "10.0.0.1"})
	if err != nil || v.Type != codec.TypeOctetString || string(v.Bytes) != "\x0a\x00\x00\x01" {
		t.Fatalf("%+v, %v", v, err)
	}
}

// A vendor table that says everything costs no standard read.
func TestAFullVendorIdentityReadsNoChassis(t *testing.T) {
	vals := map[string]codec.Value{
		sysDescr.String():           codec.String("x"),
		sysObjectID.String():        codec.ObjectID(codec.MustParseOID("1.3.6.1.4.1.27338.5.2.2")),
		"1.3.6.1.4.1.27338.5.2.2.0": codec.String("DR5000"),
		"1.3.6.1.4.1.27338.5.6.1.0": codec.String("1.3.1.1"),
	}
	p := identified(t, "", vals)
	id := &deviceIdentity{model: "m", software: "s", serial: "n"}
	p.fromChassis(context.Background(), nil, id)
	if id.firmware != "" {
		t.Errorf("%+v", id)
	}
	if got, _ := p.IdentityProbe(context.Background(), 0); got != "DR5000@1.3.1.1" {
		t.Errorf("%q", got)
	}
}

// Two boxes of one model on one firmware: what the DM keeps is the same,
// what is true of one box is not in it.
func TestTwoUnitsShareTheirDMIdentity(t *testing.T) {
	ctx := context.Background()
	first := chassisDevice()
	second := chassisDevice()
	second[mib.EntPhysicalSerialNum.Append(1).String()] = codec.String("SSJ2")
	second[mib.EntPhysicalFirmwareRev.Append(1).String()] = codec.String("Aboot-2")
	second[sysDescr.String()] = codec.String("Some OS version 4.34.3.1M on the other box")
	a, b := identified(t, mib.BaseTable, first), identified(t, mib.BaseTable, second)

	da, db := a.DeviceIdentity(ctx), b.DeviceIdentity(ctx)
	if !maps.Equal(da, db) {
		t.Errorf("%v\n%v", da, db)
	}
	for _, key := range []string{"serial", "firmware", "description", "next"} {
		if v, ok := da[key]; ok {
			t.Errorf("the DM carries %s = %q", key, v)
		}
	}
	if ua, ub := a.UnitIdentity(ctx), b.UnitIdentity(ctx); ua["serial"] != "SSJ1" || ub["serial"] != "SSJ2" || ua["firmware"] == ub["firmware"] {
		t.Errorf("%v\n%v", ua, ub)
	}
	// `info` still prints the serial for the live device.
	if si, _ := a.GetSlotInfo(ctx, 0); si.Identity["serial"] != "SSJ1" {
		t.Errorf("%v", si.Identity)
	}
}

func TestAnIdentityNeedsASession(t *testing.T) {
	p := identified(t, mib.BaseTable, chassisDevice())
	if err := p.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if got := p.UnitIdentity(context.Background()); got != nil {
		t.Errorf("%v", got)
	}
}
