package export

import (
	"context"
	"strings"
	"testing"

	"dhs/internal/consumer"
)

// Every cell the import refuses, and the ones it takes, per kind.
func TestCellProblem(t *testing.T) {
	items := []string{"Off", "On", "Auto"}
	cases := []struct {
		kind      consumer.ValueKind
		val, name string
		want      string // "" = accepted; else a fragment of the reason
	}{
		{consumer.KindBool, "true", "", ""},
		{consumer.KindBool, "FALSE", "", ""},
		{consumer.KindBool, "1", "", ""},
		{consumer.KindBool, "0", "", ""},
		{consumer.KindBool, "2", "", "not a boolean"},
		{consumer.KindBool, "ture", "", "not a boolean"},
		{consumer.KindBool, "t", "", "not a boolean"},
		{consumer.KindBool, "", "", "no value"},
		{consumer.KindInt, "-4", "", ""},
		{consumer.KindInt, "4.5", "", "not a whole number"},
		{consumer.KindUint, "-1", "", "0 or more"},
		{consumer.KindUint, "7", "", ""},
		{consumer.KindFloat, "1e3", "", ""},
		{consumer.KindFloat, "1,5", "", "not a number"},
		{consumer.KindFloat, "", "", "no value"},
		{consumer.KindEnum, "1", "On", ""},
		{consumer.KindEnum, "1", "", ""},
		{consumer.KindEnum, "", "Auto", ""},
		{consumer.KindEnum, "1", "Auto", `enum value 1 is "On" but the name says "Auto"`},
		{consumer.KindEnum, "3", "", "enum value 3 is none of Off|On|Auto"},
		{consumer.KindEnum, "-1", "", "none of"},
		{consumer.KindEnum, "x", "", "not an enum value"},
		{consumer.KindEnum, "", "on", `differs only in case`},
		{consumer.KindEnum, "", "Manual", "is not one of Off|On|Auto"},
		{consumer.KindEnum, "0", "Of", "is not one of"},
		{consumer.KindIPAddr, "239.1.0.1", "", ""},
		{consumer.KindIPAddr, "10.6.250.300", "", "not an IP address"},
		{consumer.KindIPAddr, "010.6.250.1", "", "not an IP address"},
		{consumer.KindIPAddr, "fd00::1", "", "is IPv6"},
		{consumer.KindIPAddr, "::ffff:10.0.0.1", "", "written as IPv6"},
		{consumer.KindIPAddr, "", "", "no value"},
		{consumer.KindString, "", "", ""},
	}
	for _, c := range cases {
		got := cellProblem(c.kind, c.val, c.name, items, nil)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%v %q/%q → %q, want %q", c.kind, c.val, c.name, got, c.want)
		}
	}
	if got := cellProblem(consumer.KindEnum, "200", "", nil, nil); got != "" {
		t.Errorf("an index with no item list: %q", got)
	}
	if got := cellProblem(consumer.KindEnum, "256", "", nil, nil); !strings.Contains(got, "outside 0..255") {
		t.Errorf("256 with no item list: %q", got)
	}
}

// A JSON snapshot carries the enum index already parsed: it is still
// held to the item list.
func TestValueProblemEnumFromJSON(t *testing.T) {
	obj := consumer.Object{Kind: consumer.KindEnum, EnumItems: []string{"Off", "On"}, Value: consumer.Value{Kind: consumer.KindEnum, Enum: 5}}
	if got := valueProblem(obj); !strings.Contains(got, "enum value 5 is none of Off|On") {
		t.Errorf("%q", got)
	}
	obj.Value.Enum = 1
	if got := valueProblem(obj); got != "" {
		t.Errorf("index 1: %q", got)
	}
}

// A device that numbers its items (ACP2 options, an Ember+ enumMap):
// the file carries "id=name", the value is an id, and the write goes by
// the item's name.
func TestEnumWithDeviceIDs(t *testing.T) {
	items, ids := []string{"Off", "On"}, []int64{7, 8}
	for _, c := range []struct{ val, name, want string }{
		{"7", "Off", ""}, {"8", "", ""}, {"", "On", ""},
		{"0", "", "enum value 0 is none of 7=Off|8=On"},
		{"7", "On", `enum value 7 is "Off" but the name says "On"`},
	} {
		got := cellProblem(consumer.KindEnum, c.val, c.name, items, ids)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%q/%q → %q, want %q", c.val, c.name, got, c.want)
		}
	}
	// The export writes the device's id of the named item, not the
	// one-byte index (715 would come out as 203).
	if got := enumValue(consumer.Object{EnumItems: []string{"Off", "60 s"}, EnumIDs: []int64{710, 715}, Value: consumer.Value{Kind: consumer.KindEnum, Enum: 203, Str: "60 s"}}); got != 715 {
		t.Errorf("enumValue = %d, want 715", got)
	}
	if got := enumValue(consumer.Object{EnumItems: items, Value: consumer.Value{Kind: consumer.KindEnum, Enum: 1, Str: "On"}}); got != 1 {
		t.Errorf("positional enumValue = %d", got)
	}
	cells := enumCells(consumer.Object{EnumItems: items, EnumIDs: ids})
	if strings.Join(cells, "|") != "7=Off|8=On" {
		t.Fatalf("cells %v", cells)
	}
	if n, i := splitEnumCells(cells); strings.Join(n, "|") != "Off|On" || len(i) != 2 || i[1] != 8 {
		t.Errorf("split %v %v", n, i)
	}
	if n, i := splitEnumCells([]string{"Off", "x=On"}); i != nil || n[1] != "x=On" {
		t.Errorf("plain names %v %v", n, i)
	}
	csv := "slot,path,id,label,kind,access,value,value_name,enum_items\n" +
		"1,a.r,1,r,enum,RW-,203,60 s,7=Off|203=60 s\n" +
		"1,a.s,2,s,enum,RW-,,Off,7=Off|203=60 s\n"
	snap, err := ReadCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	p := &recordingPlugin{}
	rep, _ := Apply(context.Background(), p, snap, false)
	if len(rep.Invalid) != 0 || len(p.sent) != 2 || p.sent[0].Str != "60 s" || p.sent[0].Enum != 203 || p.sent[1].Str != "Off" || p.sent[1].Enum != 7 {
		t.Fatalf("invalid %v sent %+v", rep.Invalid, p.sent)
	}
	obj := consumer.Object{Kind: consumer.KindEnum, EnumItems: items, EnumIDs: ids, Value: consumer.Value{Kind: consumer.KindEnum, Str: "on"}}
	if got := valueProblem(obj); !strings.Contains(got, "differs only in case") {
		t.Errorf("by name: %q", got)
	}
}

func TestIPClass(t *testing.T) {
	for ip, want := range map[[4]byte]string{
		{0, 0, 0, 0}: "unspecified", {255, 255, 255, 255}: "broadcast", {127, 0, 0, 1}: "loopback",
		{232, 1, 1, 1}: "multicast (SSM)", {239, 1, 0, 1}: "ipv4 multicast", {169, 254, 1, 1}: "link-local",
		{10, 6, 250, 1}: "private unicast", {8, 8, 8, 8}: "public unicast",
	} {
		if got := ipClass(ip); !strings.HasSuffix(got, want) {
			t.Errorf("%v → %q, want %q", ip, got, want)
		}
	}
}

// A CSV with good and bad rows: the good ones go out, the bad ones are
// named and never sent; an enum given by name alone goes out as its index.
func TestApplySkipsInvalidRows(t *testing.T) {
	csv := "slot,path,id,label,kind,access,value,value_name,enum_items,max_len\n" +
		"1,a.mode,1,mode,enum,RW-,,Auto,Off|On|Auto,\n" +
		"1,a.flag,2,flag,bool,RW-,ture,,,\n" +
		"1,a.name,3,name,string,RW-,abcdef,,,4\n" +
		"1,a.ip,4,ip,ipaddr,RW-,239.1.0.1,,,\n" +
		"1,a.gw,5,gw,ipaddr,RW-,10.0.0.256,,,\n" +
		"1,a.sel,6,sel,enum,RW-,1,Auto,Off|On|Auto,\n" +
		"1,a.ro,7,ro,bool,R--,maybe,,,\n"
	snap, err := ReadCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	p := &recordingPlugin{}
	rep, err := Apply(context.Background(), p, snap, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.sent) != 2 || p.sent[0].Enum != 2 || p.sent[1].IPAddr != [4]byte{239, 1, 0, 1} {
		t.Fatalf("sent %+v", p.sent)
	}
	if len(rep.Invalid) != 4 || len(rep.IPs) != 1 || !strings.Contains(rep.IPs[0], "239.1.0.1 ipv4 multicast") {
		t.Fatalf("invalid %q ips %q", rep.Invalid, rep.IPs)
	}
	for i, want := range []string{"a.flag", "a.name: 6 characters, max_len 4", "a.gw", "a.sel"} {
		if !strings.Contains(rep.Invalid[i], want) {
			t.Errorf("invalid[%d] = %q, want %q", i, rep.Invalid[i], want)
		}
	}
}
