package mnset

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"dhs/internal/consumer"
)

func f64(v float64) *float64 { return &v }

// Every kind and format a write can carry: what reaches the module is
// the module's own spelling, and what the module would wrap, clamp or
// take as given is refused here first. The cases that matter most are
// the ones the FusioN6 does not check itself (probe 2026-09-28): a bad
// IPv4 octet, DSCP 64, payload type 128.
func TestNormalizeSpellsForTheModuleAndRefusesWhatItWouldMangle(t *testing.T) {
	uuid := "59d52e04-fbff-1040-938b-40a36ba2100c"
	cases := []struct {
		name string
		t    fieldType
		in   consumer.Value
		want string // the spelling sent, when accepted
		err  string // a substring of the refusal
		is   error  // the sentinel the refusal wraps, when there is one
	}{
		{"an untyped field passes as asked", fieldType{}, consumer.Value{Str: "anything"}, "anything", "", nil},
		{"a read-only field is refused", fieldType{readOnly: true, kind: "int"}, consumer.Value{Str: "1"}, "", "read-only", nil},

		{"bool from true", fieldType{kind: "bool"}, consumer.Value{Str: "true"}, "1", "", nil},
		{"bool from off", fieldType{kind: "bool"}, consumer.Value{Str: " off "}, "0", "", nil},
		{"bool from a typed bool", fieldType{kind: "bool"}, consumer.Value{Kind: consumer.KindBool, Bool: true}, "1", "", nil},
		{"bool from nonsense", fieldType{kind: "bool"}, consumer.Value{Str: "2"}, "", "not 0/1", consumer.ErrValidationFailed},
		{"an action is a bool", fieldType{kind: "action"}, consumer.Value{Str: "yes"}, "1", "", nil},

		{"int from text", fieldType{kind: "int"}, consumer.Value{Str: " 20000 "}, "20000", "", nil},
		{"int from a typed int", fieldType{kind: "int"}, consumer.Value{Kind: consumer.KindInt, Int: 5}, "5", "", nil},
		{"int from words", fieldType{kind: "int"}, consumer.Value{Str: "high"}, "", "not a whole number", consumer.ErrValidationFailed},
		{"DSCP 64, which the module accepts", fieldType{kind: "int", min: f64(0), max: f64(63)}, consumer.Value{Str: "64"}, "", "64 > 63", consumer.ErrOutOfRangeHigh},
		{"below the floor", fieldType{kind: "int", min: f64(1), max: f64(16)}, consumer.Value{Str: "0"}, "", "0 < 1", consumer.ErrOutOfRangeLow},
		{"uint from a typed uint", fieldType{kind: "uint"}, consumer.Value{Kind: consumer.KindUint, Uint: 4294967295}, "4294967295", "", nil},
		{"uint refuses a negative", fieldType{kind: "uint"}, consumer.Value{Str: "-1"}, "", "negative", consumer.ErrValidationFailed},

		{"enum by its wire value", fieldType{kind: "enum", values: []string{"BT709", "BT2020"}}, consumer.Value{Str: "BT2020"}, "BT2020", "", nil},
		{"enum by its label, any case", fieldType{kind: "enum", values: []string{"freerun", "locked"}, labels: map[string]string{"freerun": "Free running source"}}, consumer.Value{Str: "free RUNNING source"}, "freerun", "", nil},
		{"enum refuses what the module refuses", fieldType{kind: "enum", values: []string{"SDR", "PQ", "HLG"}}, consumer.Value{Str: "LINEAR"}, "", "not one of SDR, PQ, HLG", consumer.ErrValidationFailed},

		{"ip canonical", fieldType{kind: "ip"}, consumer.Value{Str: "239.1.0.1"}, "239.1.0.1", "", nil},
		{"ip from a typed address", fieldType{kind: "ip"}, consumer.Value{Kind: consumer.KindIPAddr, IPAddr: [4]byte{10, 6, 40, 54}}, "10.6.40.54", "", nil},
		{"the octet the module would wrap to 231", fieldType{kind: "ip"}, consumer.Value{Str: "999.1.1.1"}, "", "not an IPv4 address", consumer.ErrValidationFailed},

		{"mac normalised", fieldType{format: "mac"}, consumer.Value{Str: "01-00-5E-01-00-04"}, "01:00:5e:01:00:04", "", nil},
		{"mac refused", fieldType{format: "mac"}, consumer.Value{Str: "zz:zz"}, "", "not a MAC", consumer.ErrValidationFailed},
		{"uuid", fieldType{format: "uuid"}, consumer.Value{Str: uuid}, uuid, "", nil},
		{"uuid refused", fieldType{format: "uuid"}, consumer.Value{Str: "59d52e04"}, "", "not a UUID", consumer.ErrValidationFailed},
		{"cidr", fieldType{format: "cidr"}, consumer.Value{Str: "192.168.39.230/24"}, "192.168.39.230/24", "", nil},
		{"cidr refuses IPv6", fieldType{format: "cidr"}, consumer.Value{Str: "fe80::1/64"}, "", "not an IPv4 prefix", consumer.ErrValidationFailed},
		{"hex32", fieldType{format: "hex32"}, consumer.Value{Str: "0x85062001"}, "0x85062001", "", nil},
		{"hex32 refused", fieldType{format: "hex32"}, consumer.Value{Str: "12345"}, "", "0x followed by 8 hex", consumer.ErrValidationFailed},
		{"an empty audio slot", fieldType{format: "audio_map"}, consumer.Value{Str: ":0:0"}, ":0:0", "", nil},
		{"an audio slot", fieldType{format: "audio_map"}, consumer.Value{Str: uuid + ":2:1"}, uuid + ":2:1", "", nil},
		{"audio slot refused", fieldType{format: "audio_map"}, consumer.Value{Str: "garbage"}, "", "audio flow uuid", consumer.ErrValidationFailed},
		{"hostname", fieldType{format: "hostname"}, consumer.Value{Str: "emsfp-a2-10-0c"}, "emsfp-a2-10-0c", "", nil},
		{"hostname refused", fieldType{format: "hostname"}, consumer.Value{Str: "bad_name!"}, "", "not a host name", consumer.ErrValidationFailed},
	}
	for _, c := range cases {
		got, err := normalize("f", c.t, c.in)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%s: err = %v, want %q", c.name, err, c.err)
			}
			if c.is != nil && !errors.Is(err, c.is) {
				t.Errorf("%s: %v does not wrap %v", c.name, err, c.is)
			}
			continue
		}
		if err != nil || got.Str != c.want {
			t.Errorf("%s: got %q, %v; want %q", c.name, got.Str, err, c.want)
		}
	}
}

// A walked leaf shows what it means and keeps what the module wrote.
func TestApplyKindTypesTheLeafAndKeepsTheWireSpelling(t *testing.T) {
	leaf := func(v consumer.Value) consumer.Object {
		return consumer.Object{Value: v, Kind: v.Kind, Access: accessRead | accessWrite}
	}
	str := func(s string) consumer.Value { return consumer.Value{Kind: consumer.KindString, Str: s} }

	o := leaf(str("1"))
	applyKind(&o, fieldType{kind: "bool", note: "n", format: "f"})
	if o.Kind != consumer.KindBool || !o.Value.Bool || o.Value.Str != "1" || o.Meta["note"] != "n" || o.Meta["format"] != "f" {
		t.Errorf("bool = %+v", o)
	}
	o = leaf(consumer.Value{Kind: consumer.KindBool, Bool: true})
	applyKind(&o, fieldType{kind: "action"})
	if o.Kind != consumer.KindBool || !o.Value.Bool || o.Meta["action"] != "true" {
		t.Errorf("a JSON bool stays a bool = %+v", o)
	}
	o = leaf(str("maybe"))
	applyKind(&o, fieldType{kind: "bool"})
	if o.Kind != consumer.KindString {
		t.Errorf("a value that is not 0/1 is not forced into a bool: %+v", o)
	}
	o = leaf(str("20000"))
	applyKind(&o, fieldType{kind: "int", readOnly: true})
	if o.Kind != consumer.KindInt || o.Value.Int != 20000 || o.Value.Str != "20000" || o.Access&accessWrite != 0 {
		t.Errorf("int = %+v", o)
	}
	o = leaf(str("x"))
	applyKind(&o, fieldType{kind: "int"})
	if o.Kind != consumer.KindString {
		t.Errorf("int garbage = %+v", o)
	}
	o = leaf(consumer.Value{Kind: consumer.KindInt, Int: 5})
	applyKind(&o, fieldType{kind: "uint"})
	if o.Kind != consumer.KindUint || o.Value.Uint != 5 {
		t.Errorf("uint from a JSON number = %+v", o)
	}
	o = leaf(str("x"))
	applyKind(&o, fieldType{kind: "uint"})
	if o.Kind != consumer.KindString {
		t.Errorf("uint garbage = %+v", o)
	}
	o = leaf(str("239.1.0.4"))
	applyKind(&o, fieldType{kind: "ip"})
	if o.Kind != consumer.KindIPAddr || o.Value.IPAddr != [4]byte{239, 1, 0, 4} || o.Value.Str != "239.1.0.4" {
		t.Errorf("ip = %+v", o)
	}
	o = leaf(str("not-an-ip"))
	applyKind(&o, fieldType{kind: "ip"})
	if o.Kind != consumer.KindString {
		t.Errorf("ip garbage = %+v", o)
	}
	o = leaf(str("dash-30"))
	applyKind(&o, fieldType{kind: "enum", values: []string{"dash-30", "dash-31"}, labels: map[string]string{"dash-30": "Uncompressed"}})
	if strings.Join(o.EnumItems, "|") != "dash-30=Uncompressed|dash-31" || o.Meta["value_name"] != "Uncompressed" || o.Kind != consumer.KindString {
		t.Errorf("enum = %+v", o)
	}
	// A float leaf is read back in the module's own spelling.
	if wireText(consumer.Value{Kind: consumer.KindFloat, Float: 1.5, Str: "1.50"}) != "1.50" ||
		wireText(consumer.Value{Kind: consumer.KindFloat, Float: 1.5}) != "1.5" {
		t.Error("float spelling")
	}
}

// Entries merge in file order: a later one refines an earlier one, and
// the older code→name tables read as enums.
func TestTypeOfMergesEveryMatchingEntry(t *testing.T) {
	d := dictionary{Entries: []dictEntry{
		{Match: "**.port", Kind: "int", Min: f64(0), Max: f64(65535), Note: "first"},
		{Match: "flows.*.network.port", Format: "x", Note: "second", Access: "R", Values: []string{"a"}, Labels: map[string]string{"a": "A"}},
		{Match: "self.class", Enum: map[string]string{"d": "Class D", "a": "Class A"}},
		{Match: "nothing.else", Kind: "bool"},
	}}
	got := d.typeOf([]string{"flows", "u", "network", "port"})
	if got.kind != "int" || got.format != "x" || got.note != "second" || !got.readOnly || *got.min != 0 || *got.max != 65535 || got.values[0] != "a" || got.labels["a"] != "A" {
		t.Errorf("merged = %+v", got)
	}
	cls := d.typeOf([]string{"self", "class"})
	if cls.kind != "enum" || strings.Join(cls.values, ",") != "a,d" || cls.labels["d"] != "Class D" {
		t.Errorf("code table = %+v", cls)
	}
}

// Get and a set's read-back answer in the kind a walk shows.
func TestGetAnswersInTheDictionaryKind(t *testing.T) {
	m := newModule(t)
	p := connected(t, m)
	v, err := p.GetValue(context.Background(), consumer.ValueRequest{Path: "flows.fee338d3.network.1.dst_ip_addr"})
	if err != nil || v.Kind != consumer.KindIPAddr || v.IPAddr != [4]byte{239, 0, 1, 3} {
		t.Errorf("get = %+v, %v", v, err)
	}
	// A value the module would wrap never reaches it.
	_, err = p.SetValue(context.Background(), consumer.ValueRequest{Path: "flows.fee338d3.network.1.dst_ip_addr"}, consumer.Value{Str: "999.1.1.1"})
	if !errors.Is(err, consumer.ErrValidationFailed) {
		t.Errorf("wrapped octet = %v", err)
	}
	if m.puts["flows/fee338d3"] != "" {
		t.Error("a refused value must not be PUT")
	}
}

// A set is a connect, a resolve, a PUT and a read-back: it must not be
// cut at the CLI's 1 s default after the PUT has already gone out.
func TestMinOpTimeoutCoversAWholeSet(t *testing.T) {
	if d := (&Plugin{}).MinOpTimeout(); d < 10*time.Second {
		t.Errorf("MinOpTimeout = %s: a FusioN6 set needs ~2 s even when healthy", d)
	}
}

// The NMOS registration settings are one writable document under the
// read-only diag tree; nothing else under diag becomes writable.
func TestOnlyTheNMOSDocumentUnderDiagIsWritable(t *testing.T) {
	for url, want := range map[string]bool{
		"self/diag/nmos":   true,
		"self/diag/refclk": false,
		"self/diag/common": false,
		"self/diag":        false,
		"self/ipconfig":    true,
		"self/license":     false,
		"flows/fee338d3":   true,
		"telemetry/node":   false,
		"receivers":        false,
		"senders":          false,
	} {
		if got := isWritable(url); got != want {
			t.Errorf("isWritable(%q) = %v, want %v", url, got, want)
		}
	}
	for s, ok := range map[string]bool{
		"10.6.250.5:8080": true, "0.0.0.0:0": true,
		"10.6.250.5": false, "999.1.1.1:80": false, "10.6.250.5:70000": false, "host:80": false,
	} {
		_, err := normalize("r", fieldType{format: "hostport"}, consumer.Value{Str: s})
		if (err == nil) != ok {
			t.Errorf("hostport %q: err = %v, want ok=%v", s, err, ok)
		}
	}
}

// TestTheDictionaryTypesTheProvisioningLeaves pins the entries the
// provisioning set relies on (2026-10-01 review): the PTP clock inputs
// under refclk/<uuid>, the three VPID sources, the SDI bit-rate family,
// the syslog switches, and the two leaves that stay read-only because a
// write over in-band would cut the access path.
func TestTheDictionaryTypesTheProvisioningLeaves(t *testing.T) {
	d := dict()
	uuid := "f2807dac-985d-11e5-8994-feff819cdc9f"
	typ := func(p string) fieldType { return d.typeOf(strings.Split(p, ".")) }

	if ft := typ("refclk." + uuid + ".domain_num"); ft.kind != "int" || ft.min == nil || *ft.min != 0 || ft.max == nil || *ft.max != 127 || ft.readOnly {
		t.Errorf("refclk.<uuid>.domain_num = %+v", ft)
	}
	if ft := typ("refclk." + uuid + ".dscp"); ft.kind != "int" || ft.max == nil || *ft.max != 63 {
		t.Errorf("refclk.<uuid>.dscp = %+v", ft)
	}
	if ft := typ("refclk." + uuid + ".vlan_id"); ft.kind != "int" || ft.max == nil || *ft.max != 4095 {
		t.Errorf("refclk.<uuid>.vlan_id = %+v", ft)
	}
	if ft := typ("refclk." + uuid + ".grandmaster_id"); !ft.readOnly {
		t.Errorf("refclk.<uuid>.grandmaster_id must be read-only: %+v", ft)
	}
	// The parent's own leaves keep their own entries.
	if ft := typ("refclk.delay_req"); ft.readOnly {
		t.Errorf("refclk.delay_req = %+v", ft)
	}

	if ft := typ("sdi_output.x.vpid.source"); ft.kind != "enum" || strings.Join(ft.values, ",") != "regenerated,source,override" {
		t.Errorf("vpid.source = %+v", ft)
	}
	if v, err := normalize("sdi_output.x.vpid.source", typ("sdi_output.x.vpid.source"), consumer.Value{Str: "Source"}); err != nil || requestText(v) != "source" {
		t.Errorf("vpid.source by label = %+v, %v", v, err)
	}
	if ft := typ("sdi.configuration.operating_bit_rate"); ft.kind != "enum" || strings.Join(ft.values, ",") != "fractional,integer,auto" {
		t.Errorf("operating_bit_rate = %+v", ft)
	}
	if ft := typ("self.syslog.monitoring.decap.frame_repeat"); ft.kind != "bool" || ft.readOnly {
		t.Errorf("syslog monitoring switch = %+v", ft)
	}
	if ft := typ("self.interfaces.e1.vlan"); ft.kind != "int" || ft.max == nil || *ft.max != 4095 {
		t.Errorf("interfaces vlan = %+v", ft)
	}
	if ft := typ("self.interfaces.e2.dhcp"); ft.kind != "bool" {
		t.Errorf("interfaces dhcp = %+v", ft)
	}
	if ft := typ("self.system.access_control.media.device_management"); !ft.readOnly {
		t.Errorf("REST over media must be read-only over in-band: %+v", ft)
	}
	if ft := typ("self.diag.dns.lookup.host"); !ft.readOnly {
		t.Errorf("self.diag.dns is status: %+v", ft)
	}
}

// TestCoerceKeepsATypedBool: a Value that arrives already typed as a bool
// (an importer, not the CLI's string) is written as the bool it is,
// whatever the leaf currently holds.
func TestCoerceKeepsATypedBool(t *testing.T) {
	for _, existing := range []any{true, "1", 1.0} {
		got, err := coerce(existing, consumer.Value{Kind: consumer.KindBool, Bool: false})
		if err != nil || got != false {
			t.Errorf("coerce(%v, typed false) = %v, %v", existing, got, err)
		}
	}
}
