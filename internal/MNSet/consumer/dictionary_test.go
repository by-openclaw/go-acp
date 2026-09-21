package mnset

import (
	"context"
	"strings"
	"testing"

	"dhs/internal/consumer"
)

// The dictionary is evidence, not guesswork: every entry names a
// source, and the walk shows the result in unit / min / max / enum /
// value_name — the columns that were empty.
func TestDictionaryEntriesNameTheirSource(t *testing.T) {
	d, formats, err := parseDictionary(fusion6Dictionary, videoFormatsJSON)
	if err != nil || d.Model != "FusioN6" || len(d.Entries) == 0 || len(formats) < 50 {
		t.Fatalf("dictionary = %s, %d entries, %d formats, %v", d.Model, len(d.Entries), len(formats), err)
	}
	if _, _, err := parseDictionary([]byte("x"), videoFormatsJSON); err == nil || !strings.Contains(err.Error(), "embedded dictionary") {
		t.Errorf("broken dictionary err = %v", err)
	}
	if _, _, err := parseDictionary(fusion6Dictionary, []byte("x")); err == nil || !strings.Contains(err.Error(), "embedded video formats") {
		t.Errorf("broken formats err = %v", err)
	}
	// A broken embed leaves the objects bare instead of failing the walk.
	saved := fusion6Dictionary
	fusion6Dictionary = []byte("{")
	objs := []consumer.Object{{Path: []string{"self", "system", "core_temp"}, Value: consumer.Value{Kind: consumer.KindInt, Int: 63}}}
	annotate(objs)
	fusion6Dictionary = saved
	if objs[0].Unit != "" {
		t.Errorf("annotate with a broken embed must not annotate: %+v", objs[0])
	}
	for _, e := range d.Entries {
		if e.Source == "" || e.Match == "" {
			t.Errorf("entry %+v without match or source", e)
		}
	}
	for _, s := range d.Siblings {
		if s.Source == "" || s.MinFrom == "" || s.MaxFrom == "" {
			t.Errorf("sibling rule %+v incomplete", s)
		}
	}
}

func TestMatchPath(t *testing.T) {
	cases := []struct {
		pat  string
		path string
		want bool
	}{
		{"*.dst_udp_port", "flows.abc.network.dst_udp_port", false}, // '*' is ONE segment
		{"**.dst_udp_port", "flows.abc.network.dst_udp_port", true}, // '**' any number of leading ones
		{"**.dst_udp_port", "dst_udp_port", true},
		{"**.network.dst_udp_port", "dst_udp_port", false},
		{"flows.*.network.dst_udp_port", "flows.abc.network.dst_udp_port", true},
		{"port.*.sfp_ddm_info.*.current", "port.3.sfp_ddm_info.temperature.current", true},
		{"port.*.sfp_ddm_info.*.current", "port.3.sfp_ddm_info.temperature.high_alarm", false},
		{"refclk.mode", "refclk.mode", true},
		{"refclk.mode", "refclk", false},
	}
	for _, c := range cases {
		if got := matchPath(c.pat, strings.Split(c.path, ".")); got != c.want {
			t.Errorf("matchPath(%q, %q) = %v", c.pat, c.path, got)
		}
	}
}

func TestWalkAnnotatesUnitsRangesEnumsAndFormatNames(t *testing.T) {
	m := newModule(t)
	m.docs["port/3"] = `{"detected_sfp_part_number":"GSS","detected_sfp_serial_number":"M1","host_pinout":"RT","link":"up","speed":"25Gbps",
	  "sfp_ddm_info":{"temperature":{"current":36.5,"low_alarm":-20,"high_alarm":85},"vcc":{"current":3.312,"low_alarm":3,"high_alarm":3.6},"rx_power":{"current":"n/a","low_alarm":0,"high_alarm":0}}}`
	m.docs["self/system"] = `{"core_temp":63,"smpte_network":{"2022-7":{"class":"d"}}}`
	m.docs["flows/fee338d3"] = `{"id":"fee338d3","name":"rx","format":{"format_code_t_scan":4,"format_code_p_scan":4,"format_code_mode":16,"format_code_format":0,"format_code_rate":9216,"format_code_sampling":8192},"network":{"dst_ip_addr":"239.0.1.2","dst_udp_port":20000,"pkt_cnt":"7"}}`
	// the real module spells the codes as digit strings
	m.docs["flows/fee338d3-sec"] = `{"id":"sec","format":{"format_code_t_scan":"0","format_code_p_scan":"0","format_code_mode":"0","format_code_format":"0","format_code_rate":"5120","format_code_sampling":"0"},"network":{"dst_ip_addr":"239.132.3.134"}}`
	m.docs["flows/orphan"] = `{"id":"orphan","format":{"format_code_t_scan":1,"format_code_p_scan":2,"format_code_mode":3,"format_code_format":4,"format_code_rate":5,"format_code_sampling":6},"network":{"dst_ip_addr":"0.0.0.0"}}`
	m.docs["flows"] = `["fee338d3/","fee338d3-sec/","orphan/","partial/","missing/","boolcode/"]`
	m.docs["flows/missing"] = `{"id":"missing","format":{"format_code_rate":9216},"network":{}}`
	m.docs["flows/boolcode"] = `{"id":"boolcode","format":{"format_code_t_scan":true,"format_code_p_scan":0,"format_code_mode":0,"format_code_format":0,"format_code_rate":9216,"format_code_sampling":0},"network":{}}`
	m.docs["flows/partial"] = `{"id":"partial","format":{"format_code_rate":9216,"format_code_t_scan":"x","format_code_p_scan":true,"format_code_mode":0,"format_code_format":0,"format_code_sampling":0},"network":{}}`
	p := connected(t, m)
	objs, err := p.Walk(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]consumer.Object{}
	for _, o := range objs {
		byPath[strings.Join(o.Path, ".")] = o
	}
	// unit + module-published thresholds
	o := byPath["port.3.sfp_ddm_info.temperature.current"]
	if o.Unit != "°C" || o.Min != float64(-20) || o.Max != float64(85) {
		t.Errorf("temperature = unit %q min %v max %v", o.Unit, o.Min, o.Max)
	}
	if o := byPath["port.3.sfp_ddm_info.vcc.current"]; o.Unit != "V" || o.Min != float64(3) || o.Max != 3.6 {
		t.Errorf("vcc = unit %q min %v max %v", o.Unit, o.Min, o.Max)
	}
	if o := byPath["port.3.sfp_ddm_info.rx_power.current"]; o.Min != nil || o.Max != nil {
		t.Errorf("a non-numeric current must not take thresholds: %v %v", o.Min, o.Max)
	}
	// validator range from MN SET
	if o := byPath["flows.fee338d3.network.dst_udp_port"]; o.Min != float64(1) || o.Max != float64(65534) {
		t.Errorf("port range = %v..%v", o.Min, o.Max)
	}
	// enum with value name
	o = byPath["self.system.smpte_network.2022-7.class"]
	if strings.Join(o.EnumItems, "|") != "a=Class A (10 ms)|b=Class B (50 ms)|d=Class D (150 µs)" || o.Meta["value_name"] != "Class D (150 µs)" || o.Meta["description"] == "" {
		t.Errorf("class = %v %v", o.EnumItems, o.Meta)
	}
	// unit with description + source
	if o := byPath["flows.fee338d3.network.pkt_cnt"]; o.Unit != "packets" || o.Meta["source"] == "" {
		t.Errorf("pkt_cnt = %q %v", o.Unit, o.Meta)
	}
	// format tuple → name on all six, list on rate
	for _, c := range []string{"format_code_t_scan", "format_code_rate", "format_code_sampling"} {
		if o := byPath["flows.fee338d3.format."+c]; o.Meta["value_name"] != "3GA 1080p50" {
			t.Errorf("%s value_name = %v", c, o.Meta["value_name"])
		}
	}
	if o := byPath["flows.fee338d3-sec.format.format_code_rate"]; o.Meta["value_name"] != "1080i50" || len(o.EnumItems) < 50 {
		t.Errorf("sec format = %v, %d names", o.Meta["value_name"], len(o.EnumItems))
	}
	if o := byPath["flows.orphan.format.format_code_rate"]; o.Meta["value_name"] != "(not a listed format)" {
		t.Errorf("orphan format = %v", o.Meta["value_name"])
	}
	for _, f := range []string{"partial", "missing", "boolcode"} {
		if o := byPath["flows."+f+".format.format_code_rate"]; o.Meta["value_name"] != nil || len(o.EnumItems) < 50 {
			t.Errorf("%s tuple must get the list but no name: %v", f, o.Meta)
		}
	}
}
