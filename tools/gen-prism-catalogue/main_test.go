package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const page = `<table id="APItable"><tbody>
<tr class="commonTableHeaderClass"><th>NAME</th><th>MODE</th><th>DATATYPE</th><th>DESCRIPTION</th></tr>
<tr><td>audio_ballistic</td><td><a href="http://host/api/audio_ballistic">GET</a> / POST</td><td>ints</td><td>Available Options: AUDIO_PPM_1, AUDIO_PPM_2, AUDIO_PPM_1.</td></tr>
<tr><td>waveform_mode</td><td><a href="http://host/api/waveform_mode/tile1">GET</a> / POST</td><td>ints</td><td>Requires a scope of  tile1 through tile8. Options: W_MODE_A, W_MODE_B</td></tr>
<tr><td>band</td><td><a href="http://host/api/band/tile1">GET</a> / POST</td><td>ints</td><td>Options: B_METER_[HIDE, SHOW]; Requires Tile Scope of tile1 through tile8</td></tr>
<tr><td>sweep</td><td><a href="http://host/api/sweep">GET</a> / POST</td><td>ints</td><td>Options:  0 for 1 line,  1 for 2 line</td></tr>
<tr><td>only_one</td><td>POST</td><td>ints</td><td>Use 0 for nothing</td></tr>
<tr><td>hmag</td><td><a href="http://host/api/hmag">GET</a> / POST</td><td>ints</td><td>Specify an array[2]: 0 for Disable, 1 for Enable.</td></tr>
<tr><td>unit</td><td>GET</td><td>ints</td><td>Units for FALSE_COLOR_MODE LUMA</td></tr>
<tr><td>loud</td><td><a href="http://host/api/loud">GET</a> / POST</td><td>floats</td><td>Range: 0 to -31</td></tr>
<tr><td>stop_mode</td><td><a href="http://host/api/stop_mode">GET</a> / POST</td><td>string</td><td>Available Options:  "Y"</td></tr>
<tr><td>input_status</td><td><a href="http://host/api/input_status/config0">GET</a></td><td>object</td><td>Requires Input Configuration scope of config0 through config5; an empty scope field would return the primary.</td></tr>
<tr><td>a_config</td><td><a href="http://host/api/a_config/config0">GET</a> / POST</td><td>ints</td><td>Config/Input Scope: config0 to config5</td></tr>
<tr><td>ip_config_v4</td><td>GET</td><td>object</td><td>Requires a scope of IP1 or IP2.</td></tr>
<tr><td>timing</td><td>GET</td><td>object</td><td>Requires a scope of IP1 - IP2, SDI1 - SDI4, SDI1-4, or EXTREF.</td></tr>
<tr><td>hw</td><td>GET</td><td>object</td><td>Requires a scope of slot 0 or 1.</td></tr>
<tr><td>hw0</td><td>GET</td><td>object</td><td>Requires a scope of slot0.</td></tr>
<tr><td>bright</td><td>GET</td><td>ints</td><td>This requires index scope: index1 and index2.</td></tr>
<tr><td>hpf</td><td>GET</td><td>ints</td><td>Requires a scope of index0</td></tr>
<tr><td>href_tile</td><td><a href="http://host/api/href_tile/tile1">GET</a></td><td>object</td><td>no word</td></tr>
<tr><td>href_config</td><td><a href="http://host/api/href_config/config0">GET</a></td><td>object</td><td>no word</td></tr>
<tr><td>href_port</td><td><a href="http://host/api/href_port/IP1">GET</a></td><td>object</td><td>no word</td></tr>
<tr><td>href_slot</td><td><a href="http://host/api/href_slot/slot0">GET</a></td><td>object</td><td>no word</td></tr>
<tr><td>inputConfigure</td><td><a href="http://host/api/inputConfigure?input=0">GET</a> / POST</td><td>object</td><td>JSON body</td></tr>
<tr><td>system_reboot</td><td><a href="http://host/api/system_reboot">GET</a> / POST</td><td>object</td><td>Reboot system</td></tr>
<tr><td>nested_call</td><td><a href="http://host/api/nested_call">GET</a> / POST</td><td>object</td><td><table><tbody><tr><th>Name</th></tr>
<tr><td>"name"</td><td>string</td><td>The name.</td><td>"x"</td></tr>
<tr><td>short</td><td>row</td></tr>
</tbody></table>`

func TestParsePage(t *testing.T) {
	var b bytes.Buffer
	writeTSV(&b, parsePage(page))
	want := []string{
		"audio_ballistic\tGET/POST\tints\t\t\tAUDIO_PPM_1|AUDIO_PPM_2",
		"waveform_mode\tGET/POST\tints\ttile\t\tW_MODE_A|W_MODE_B",
		"band\tGET/POST\tints\ttile\t\tB_METER_HIDE|B_METER_SHOW",
		"sweep\tGET/POST\tints\t\t\t0|1",
		"only_one\tPOST\tints\t\t\t",
		"hmag\tGET/POST\tints\t\t\t",
		"unit\tGET\tints\t\t\t",
		"loud\tGET/POST\tfloats\t\t\t",
		"stop_mode\tGET/POST\tstring\t\t\tY|RGB|YRGB",
		"input_status\tGET\tobject\tconfig\tscope-optional\t",
		"a_config\tGET/POST\tints\tconfig\t\t",
		"ip_config_v4\tGET\tobject\tport\t\t",
		"timing\tGET\tobject\ttiming\t\t",
		"hw\tGET\tobject\tslot\t\t",
		"hw0\tGET\tobject\tslot0\t\t",
		"bright\tGET\tints\tindex\t\t",
		"hpf\tGET\tints\tindex0\t\t",
		"href_tile\tGET\tobject\ttile\t\t",
		"href_config\tGET\tobject\tconfig\t\t",
		"href_port\tGET\tobject\tport\t\t",
		"href_slot\tGET\tobject\tslot0\t\t",
		"inputConfigure\tGET/POST\tobject\tinput\tbare-body\t",
		"system_reboot\tGET/POST\tobject\t\tno-read,guarded\t",
		"nested_call\tGET/POST\tobject\t\t\t",
	}
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")[2:]
	if len(lines) != len(want) {
		t.Fatalf("%d rows, want %d:\n%s", len(lines), len(want), b.String())
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestRun(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "page.html")
	if err := os.WriteFile(in, []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if rc := run([]string{"-page", in}, &out, &errs); rc != 0 || !strings.Contains(out.String(), "audio_ballistic") {
		t.Fatalf("stdout run: %d %s", rc, errs.String())
	}
	tsv := filepath.Join(dir, "out.tsv")
	if rc := run([]string{"-page", in, "-out", tsv}, &out, &errs); rc != 0 {
		t.Fatalf("file run: %d %s", rc, errs.String())
	}
	if _, err := os.Stat(tsv); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"no page":   {},
		"bad flag":  {"-nope"},
		"missing":   {"-page", filepath.Join(dir, "absent")},
		"bad write": {"-page", in, "-out", filepath.Join(dir, "no", "dir", "x")},
	} {
		if rc := run(args, &out, &errs); rc == 0 {
			t.Errorf("%s: rc 0", name)
		}
	}
}

func TestMain_ExitsWithRun(t *testing.T) {
	saved, args := osExit, os.Args
	defer func() { osExit, os.Args = saved, args }()
	got := -1
	osExit = func(c int) { got = c }
	os.Args = []string{"gen-prism-catalogue"}
	main()
	if got != 1 {
		t.Errorf("exit = %d", got)
	}
}
