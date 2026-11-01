package codec

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

func load(t *testing.T) *Catalogue {
	t.Helper()
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func entry(t *testing.T, c *Catalogue, name string) Entry {
	t.Helper()
	e, ok := c.Lookup(name)
	if !ok {
		t.Fatalf("%s not in the catalogue", name)
	}
	return e
}

// Counts read from the page: 298 GET / POST, 132 GET, 13 POST.
func TestCatalogueLoadsEveryRow(t *testing.T) {
	c := load(t)
	if got := len(c.Entries()); got != 443 {
		t.Fatalf("rows = %d, want 443", got)
	}
	modes := map[Mode]int{}
	types := map[DataType]int{}
	for _, e := range c.Entries() {
		modes[e.Mode]++
		types[e.Type]++
	}
	if modes[ModeGetPost] != 298 || modes[ModeGet] != 132 || modes[ModePost] != 13 {
		t.Fatalf("modes = %v", modes)
	}
	for ty, n := range map[DataType]int{TypeInts: 213, TypeObject: 149, TypeString: 48, TypeFloats: 11,
		TypeDoubles: 10, TypeQuantity: 10, TypeSysio: 1, TypeNone: 1} {
		if types[ty] != n {
			t.Errorf("%s = %d, want %d", ty, types[ty], n)
		}
	}
}

func TestCatalogueRows(t *testing.T) {
	c := load(t)
	for _, tc := range []struct {
		name   string
		mode   Mode
		typ    DataType
		scope  Scope
		option string
	}{
		{"audio_ballistic", ModeGetPost, TypeInts, ScopeNone, "AUDIO_BALLISTIC_TRUE_PEAK"},
		{"waveform_mode", ModeGetPost, TypeInts, ScopeTile, "WAVEFORM_MODE_YRGB"},
		{"source_config_eotf", ModeGetPost, TypeInts, ScopeConfig, "IOSLAVE_SOURCE_CONFIG_EOTF_LOGC"},
		{"picture_false_color_band_meter", ModeGetPost, TypeInts, ScopeTile, "PICTURE_FALSE_COLOR_BAND_METER_SHOW"},
		{"tile_freeze_display_mode", ModeGetPost, TypeInts, ScopeNone, "TILE_FREEZE_DISPLAY_MODE_Both"},
		{"waveform_sweep", ModeGetPost, TypeInts, ScopeTile, "18"},
		{"stop_mode", ModeGetPost, TypeString, ScopeTile, "YRGB"},
		{"loudness_load_preset", ModePost, TypeInts, ScopeNone, "LOUDNESS_PRESET_ATSC_A85_2013"},
		{"ip_config_v4", ModeGet, TypeObject, ScopePort, ""},
		{"hardware_version_info", ModeGet, TypeObject, ScopeSlot, ""},
		{"input_hardware_info", ModeGet, TypeObject, ScopeSlot0, ""},
		{"display_brightness", ModeGetPost, TypeInts, ScopeIndex, ""},
		{"jitter_hpf", ModeGetPost, TypeInts, ScopeIndex0, "6"},
		{"timing_ref_measure_summary", ModeGet, TypeObject, ScopeTiming, ""},
		{"inputConfigure", ModeGetPost, TypeObject, ScopeInput, ""},
		{"multi_input_status", ModeGet, TypeObject, ScopeNone, ""},
		{"input_configure_update", ModeGetPost, TypeObject, ScopeConfig, ""},
		{"line_select_position", ModeGetPost, TypeObject, ScopeNone, ""},
		{"video_input", ModeGetPost, TypeSysio, ScopeNone, ""},
	} {
		e := entry(t, c, tc.name)
		if e.Mode != tc.mode || e.Type != tc.typ || e.Scope != tc.scope {
			t.Errorf("%s = %v %s %q", tc.name, e.Mode, e.Type, e.Scope)
		}
		if tc.option != "" && !slices.Contains(e.Options, tc.option) {
			t.Errorf("%s options %v lack %s", tc.name, e.Options, tc.option)
		}
	}
	if _, ok := c.Lookup("\"lineMode\""); ok {
		t.Error("a nested field row reached the catalogue")
	}
	if !entry(t, c, "input_status").ScopeOptional || !entry(t, c, "tile_freeze").ScopeOptional {
		t.Error("the two optional scopes are lost")
	}
	if len(entry(t, c, "tile_false_color_unit").Options) != 0 || len(entry(t, c, "waveform_hmag").Options) != 0 {
		t.Error("prose or an array[2] read as an enum")
	}
	for _, name := range []string{"system_reboot", "system_poweroff", "help", "downloadScreenshot", "data_logger_db_diag_init"} {
		if !entry(t, c, name).NoRead {
			t.Errorf("%s can be read", name)
		}
	}
	if !entry(t, c, "serial_number").Guarded || !entry(t, c, "data_logger_db_new").Guarded {
		t.Error("guard lost")
	}
}

func TestModeString(t *testing.T) {
	for m, want := range map[Mode]string{ModeGet: "GET", ModePost: "POST", ModeGetPost: "GET / POST"} {
		if m.String() != want {
			t.Errorf("%d = %s", m, m)
		}
	}
}

func TestParseCatalogueErrors(t *testing.T) {
	for name, tsv := range map[string]string{
		"columns":   "a\tGET\tints\n",
		"mode":      "a\tPUT\tints\t\t\t\n",
		"flag":      "a\tGET\tints\t\tnope\t\n",
		"duplicate": "a\tGET\tints\t\t\t\na\tGET\tints\t\t\t\n",
	} {
		if _, err := parseCatalogue(tsv); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// git on a Windows runner checks the TSV out with CRLF, and embed keeps it.
func TestCatalogueSurvivesCRLF(t *testing.T) {
	lf := strings.ReplaceAll(catalogueTSV, "\r\n", "\n")
	c, err := parseCatalogue(strings.ReplaceAll(lf, "\n", "\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	e := entry(t, c, "waveform_mode")
	if last := e.Options[len(e.Options)-1]; last != "WAVEFORM_MODE_YRGB" {
		t.Fatalf("last option %q", last)
	}
	if len(entry(t, c, "host_name").Options) != 0 {
		t.Fatal("an empty last column holds a carriage return")
	}
}

func TestScopeValues(t *testing.T) {
	for s, want := range map[Scope]string{
		ScopeTile:   "tile1,tile2,tile3,tile4,tile5,tile6,tile7,tile8",
		ScopeConfig: "config0,config1,config2,config3,config4,config5",
		ScopePort:   "IP1,IP2",
		ScopeSlot:   "slot0,slot1",
		ScopeSlot0:  "slot0",
		ScopeIndex:  "index1,index2",
		ScopeIndex0: "index0",
		ScopeTiming: "IP1,IP2,SDI1,SDI2,SDI3,SDI4,SDI1-4,EXTREF",
		ScopeInput:  "0,1,2,3,4,5",
		ScopeNone:   "",
	} {
		if got := strings.Join(s.Values(), ","); got != want {
			t.Errorf("%q = %s", s, got)
		}
	}
}

func TestTargets(t *testing.T) {
	c := load(t)
	got := func(name string) []string {
		var out []string
		for _, x := range entry(t, c, name).Targets() {
			out = append(out, x.String())
		}
		return out
	}
	if g := got("software_version"); !slices.Equal(g, []string{"software_version"}) {
		t.Error(g)
	}
	if g := got("input_status"); len(g) != 7 || g[0] != "input_status" || g[6] != "input_status/config5" {
		t.Error(g)
	}
	if g := got("inputConfigure"); len(g) != 6 || g[3] != "inputConfigure?input=3" {
		t.Error(g)
	}
}

// URL shapes from the page: localhost:9000/api/API_NAME/SCOPE and
// localhost:9000/api/API_NAME?field=object.parent1.parent2.target.
func TestParseTargetURL(t *testing.T) {
	for in, want := range map[string]string{
		"API_NAME":       "/api/API_NAME",
		"API_NAME/SCOPE": "/api/API_NAME/SCOPE",
		"API_NAME.SCOPE": "/api/API_NAME/SCOPE",
		"API_NAME?field=object.parent1.parent2.target": "/api/API_NAME?field=object.parent1.parent2.target",
		"API_NAME/tile1?field=object":                  "/api/API_NAME/tile1?field=object",
		"inputConfigure?input=0":                       "/api/inputConfigure?input=0",
		" timing_ref_measure_summary/SDI1-4 ":          "/api/timing_ref_measure_summary/SDI1-4",
	} {
		tg, err := ParseTarget(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got := tg.URLPath(); got != want {
			t.Errorf("%q = %s, want %s", in, got, want)
		}
	}
}

func TestParseTargetInvalid(t *testing.T) {
	for _, in := range []string{"", "/tile1", "x?field=parent.target", "x?other=1", "x?a=%zz"} {
		var inv *Invalid
		if _, err := ParseTarget(in); !errors.As(err, &inv) || inv.Error() == "" {
			t.Errorf("%q: %v", in, err)
		}
	}
}

func TestCheck(t *testing.T) {
	c := load(t)
	for _, tc := range []struct {
		name, target string
		write, bad   bool
	}{
		{"software_version", "software_version", false, false},
		{"software_version", "software_version", true, true},
		{"loudness_load_preset", "loudness_load_preset", false, true},
		{"system_reboot", "system_reboot", false, true},
		{"waveform_mode", "waveform_mode", false, true},
		{"waveform_mode", "waveform_mode", true, true},
		{"waveform_mode", "waveform_mode/tile1", false, false},
		{"waveform_mode", "waveform_mode/tile1?field=object", true, true},
		{"input_status", "input_status", false, false},
		{"inputConfigure", "inputConfigure", false, true},
		{"inputConfigure", "inputConfigure", true, false},
		{"inputConfigure", "inputConfigure?input=2", false, false},
		{"ip_signal_status", "ip_signal_status/config0", false, false},
	} {
		tg, err := ParseTarget(tc.target)
		if err != nil {
			t.Fatal(err)
		}
		if err := entry(t, c, tc.name).Check(tg, tc.write); (err != nil) != tc.bad {
			t.Errorf("%s write=%v: %v", tc.target, tc.write, err)
		}
	}
}

// Expected bodies from the page's own examples where it has them; the
// scalar shape is the reading noted on ScalarForm.
func TestEncodeBody(t *testing.T) {
	c := load(t)
	for _, tc := range []struct {
		name, value, want string
		form              ScalarForm
	}{
		{"audio_silence_alarm", `{"duration":{"value": 3}}`, `{"object":{"duration":{"value": 3}}}`, ScalarArray},
		{"audio_phase_alarm", `{"min":{"value": -20}}`, `{"object":{"min":{"value": -20}}}`, ScalarArray},
		{"ip_status_reset", `{"object":{}}`, `{"object":{}}`, ScalarArray},
		{"ip_status_reset", `{}`, `{"object":{}}`, ScalarArray},
		{"requested_display_mappings", `{"primary":"eDP1", "extended":"HDMI3"}`, `{"object":{"primary":"eDP1", "extended":"HDMI3"}}`, ScalarArray},
		{"activeInput", `{"input":2}`, `{"input":2}`, ScalarArray},
		{"inputConfigure", `{"input":0,"data":{"inputType":"SDI","name":"SDI-In 1","port":0}}`,
			`{"input":0,"data":{"inputType":"SDI","name":"SDI-In 1","port":0}}`, ScalarArray},
		{"extref_position", "0.4, 194.5 mV", `{"object":["0.4","194.5 mV"]}`, ScalarArray},
		{"camapp_vertical_position", "5 stops", `{"object":["5 stops"]}`, ScalarBare},
		{"extref_hmag", "1,0", `{"object":[1,0]}`, ScalarArray},
		{"extref_hmag", "1,0", `{"object":[1,0]}`, ScalarBare},
		{"audio_ballistic", "AUDIO_BALLISTIC_PPM_1", `{"object":["AUDIO_BALLISTIC_PPM_1"]}`, ScalarArray},
		{"audio_ballistic", "AUDIO_BALLISTIC_PPM_1", `{"object":"AUDIO_BALLISTIC_PPM_1"}`, ScalarBare},
		{"audio_ballistic", "2", `{"object":[2]}`, ScalarArray},
		{"waveform_sweep", "16", `{"object":[16]}`, ScalarArray},
		{"waveform_gain", "4", `{"object":4}`, ScalarBare},
		{"video_input", "3", `{"object":[3]}`, ScalarArray},
		{"audio_program_level", "-20.5", `{"object":[-20.5]}`, ScalarArray},
		{"waveform_variable", "1.25", `{"object":1.25}`, ScalarBare},
		{"audio_solo_mute_track", "L1", `{"object":"L1"}`, ScalarArray},
		{"ypbpr_channels", "", `{"object":""}`, ScalarArray},
		{"stop_mode", "RGB", `{"object":"RGB"}`, ScalarArray},
		{"host_name", "a<b", `{"object":"a<b"}`, ScalarArray},
		{"loadpreset", "factory", `{"object":"factory"}`, ScalarArray},
	} {
		got, err := entry(t, c, tc.name).EncodeBody(tc.value, tc.form)
		if err != nil {
			t.Errorf("%s %q: %v", tc.name, tc.value, err)
			continue
		}
		if string(got) != tc.want {
			t.Errorf("%s %q = %s, want %s", tc.name, tc.value, got, tc.want)
		}
	}
}

func TestEncodeBodyRefused(t *testing.T) {
	c := load(t)
	for _, tc := range []struct{ name, value string }{
		{"audio_ballistic", "AUDIO_BALLISTIC_PPM_9"},
		{"waveform_sweep", "4"},
		{"waveform_sweep", "parade"},
		{"stop_mode", "XYZ"},
		{"waveform_gain", "x"},
		{"waveform_gain", ""},
		{"audio_program_level", "loud"},
		{"audio_program_level", "+1"},
		{"audio_silence_alarm", "{not json"},
		{"help", "x"},
	} {
		var inv *Invalid
		if _, err := entry(t, c, tc.name).EncodeBody(tc.value, ScalarArray); !errors.As(err, &inv) {
			t.Errorf("%s %q: %v", tc.name, tc.value, err)
		}
	}
}

func TestParseScalarForm(t *testing.T) {
	for in, want := range map[string]ScalarForm{"": ScalarArray, "array": ScalarArray, "bare": ScalarBare} {
		if got, err := ParseScalarForm(in); err != nil || got != want {
			t.Errorf("%q = %v %v", in, got, err)
		}
	}
	if _, err := ParseScalarForm("list"); err == nil {
		t.Error("list accepted")
	}
}

func TestDecodeReply(t *testing.T) {
	c := load(t)
	for _, tc := range []struct {
		name, raw, text string
		form            ScalarForm
		fielded         bool
		dev             []string
	}{
		{name: "software_version", raw: `{"object":"4.3.1"}`, text: "4.3.1"},
		{name: "waveform_gain", raw: `{"object":[4]}`, text: "4"},
		{name: "waveform_gain", raw: `{"object":[4]}`, text: "4", form: ScalarBare, dev: []string{DevScalarForm}},
		{name: "waveform_gain", raw: `{"object":4}`, text: "4", dev: []string{DevScalarForm}},
		{name: "waveform_gain", raw: `{"object":4}`, text: "4", form: ScalarBare},
		{name: "waveform_mode", raw: `{"object":["WAVEFORM_MODE_RGB"]}`, text: "WAVEFORM_MODE_RGB"},
		{name: "waveform_gain", raw: `{"object":["x"]}`, text: "x", dev: []string{DevWrongType}},
		{name: "waveform_gain", raw: `{"object":[true]}`, text: "true", dev: []string{DevWrongType}},
		{name: "extref_hmag", raw: `{"object":[1,0]}`, text: "[1,0]"},
		{name: "waveform_position", raw: `{"object":["0.4","194.5 mV"]}`, text: `["0.4","194.5 mV"]`},
		{name: "waveform_position", raw: `{"object":[0.4]}`, text: "0.4", dev: []string{DevWrongType}},
		{name: "camapp_vertical_position", raw: `{"object":"5 stops"}`, text: "5 stops", dev: []string{DevScalarForm}},
		{name: "camapp_vertical_position", raw: `{"object":["5 stops"]}`, text: "5 stops", form: ScalarBare},
		{name: "audio_program_level", raw: `{"object":[-20.5]}`, text: "-20.5"},
		{name: "system_status", raw: `{"object":{"state":"ok"}}`, text: `{"state":"ok"}`},
		{name: "audiomixer_device_list", raw: `{"object":[{"name":"Headphones"}]}`, text: `{"name":"Headphones"}`},
		{name: "system_status", raw: `{"object":"ok"}`, text: "ok", dev: []string{DevWrongType}},
		{name: "software_version", raw: `{"object":[1,2]}`, text: "[1,2]", dev: []string{DevWrongType}},
		{name: "system_status", raw: `{"state":"ok"}`, text: `{"state":"ok"}`, dev: []string{DevUnwrapped}},
		{name: "system_status", raw: `{"state":true}`, text: `{"state":true}`, fielded: true},
		{name: "waveform_gain", raw: `[4]`, text: "4", dev: []string{DevUnwrapped}},
		{name: "system_status", raw: `17`, text: "17", fielded: true},
		{name: "system_status", raw: `{"object":true}`, text: "true", dev: []string{DevWrongType}},
		{name: "software_version", raw: "4.3.1 build 2", text: "4.3.1 build 2", dev: []string{DevNotJSON}},
	} {
		r, err := entry(t, c, tc.name).DecodeReply([]byte(tc.raw), tc.form, tc.fielded)
		if err != nil {
			t.Errorf("%s %s: %v", tc.name, tc.raw, err)
			continue
		}
		if r.Text != tc.text || !slices.Equal(r.Deviations, tc.dev) {
			t.Errorf("%s %s = %q %v, want %q %v", tc.name, tc.raw, r.Text, r.Deviations, tc.text, tc.dev)
		}
	}
}

func TestDecodeReplyFailures(t *testing.T) {
	c := load(t)
	for _, tc := range []struct {
		name, raw string
		want      error
		dev       string
	}{
		{"software_version", "   ", ErrNoValue, ""},
		{"software_version", `{"object":null}`, ErrNoValue, ""},
		{"software_version", "<html>login</html>", ErrNotAValue, DevNotJSON},
		{"waveform_gain", "four", ErrNotAValue, DevNotJSON},
		{"waveform_gain", `{"a":1} {"b":2}`, ErrNotAValue, DevNotJSON},
		{"waveform_gain", `{"error":"no such tile"}`, nil, DevErrorIn2xx},
	} {
		r, err := entry(t, c, tc.name).DecodeReply([]byte(tc.raw), ScalarArray, false)
		if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
			t.Errorf("%s: err = %v", tc.raw, err)
		}
		if tc.dev != "" && !slices.Contains(r.Deviations, tc.dev) {
			t.Errorf("%s: deviations %v", tc.raw, r.Deviations)
		}
	}
}

func TestSingle(t *testing.T) {
	c := load(t)
	r, err := entry(t, c, "waveform_gain").DecodeReply([]byte(`{"object":[4]}`), ScalarArray, false)
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := r.Single.(json.Number); !ok || n.String() != "4" {
		t.Errorf("single = %#v", r.Single)
	}
	if entry(t, c, "waveform_gain").Numeric() {
		t.Error("no options is not numeric")
	}
	if !entry(t, c, "waveform_sweep").Numeric() || entry(t, c, "waveform_mode").Numeric() {
		t.Error("numeric enum misread")
	}
}

func TestPostFailed(t *testing.T) {
	if err := PostFailed([]byte(`{"error":"denied"}`)); err == nil || err.Error() != `{"error":"denied"}` {
		t.Error(err)
	}
	for _, raw := range []string{"", "ok", `{"object":[1]}`, `[1]`} {
		if err := PostFailed([]byte(raw)); err != nil {
			t.Errorf("%q: %v", raw, err)
		}
	}
}

func TestInputOf(t *testing.T) {
	if got, err := InputOf([]byte(`{"input":3,"data":{}}`)); err != nil || got != "3" {
		t.Errorf("%q %v", got, err)
	}
	var inv *Invalid
	if _, err := InputOf([]byte(`{"data":{}}`)); !errors.As(err, &inv) {
		t.Error(err)
	}
}

func TestCompactKeepsWhatItCannotCompact(t *testing.T) {
	if got := compact([]byte("{not json")); got != "{not json" {
		t.Error(got)
	}
}
