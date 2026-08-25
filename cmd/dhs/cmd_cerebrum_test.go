package main

import (
	"context"
	"io"
	"os"
	"time"
	"strings"
	"testing"

	"dhs/internal/cerebrum-nb/codec"
)

// These are flag-validation / dispatch smoke tests for the cerebrum-nb CLI.
// They never touch the network: every assertion is on an error that fires
// at parse/validation time, before connectAndLogin dials. There is no live
// Cerebrum peer and no vendor emulator in this environment, so the wire
// behaviour of these verbs is covered by the consumer package's fake-WS
// tests, not here.

func TestCerebrumUnknownVerb(t *testing.T) {
	err := runCerebrum(context.Background(), []string{"bogus-verb"})
	if err == nil || !strings.Contains(err.Error(), "unknown verb") {
		t.Fatalf("want unknown verb error, got %v", err)
	}
}

func TestCerebrumHelpNoArgs(t *testing.T) {
	if err := runCerebrum(context.Background(), nil); err != nil {
		t.Fatalf("help (no args) returned error: %v", err)
	}
	if err := runCerebrum(context.Background(), []string{"-h"}); err != nil {
		t.Fatalf("help (-h) returned error: %v", err)
	}
}

// TestCerebrumWriteVerbsValidateFlags pins the pre-dial flag validation for
// every new §4 write verb. Each case must error before any network I/O.
func TestCerebrumWriteVerbsValidateFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		// lock / unlock: bad --kind, then missing srce/dest.
		{"lock-bad-kind", []string{"lock", "h", "--kind", "WAT", "--dest", "1"}, "unknown --kind"},
		{"lock-no-addr", []string{"lock", "h", "--kind", "DEST_LOCK"}, "--srce or --dest is required"},
		{"unlock-bad-kind", []string{"unlock", "h", "--kind", "WAT"}, "unknown --kind"},
		// set-mnemonic: bad kind + missing mnemonic.
		{"mne-bad-kind", []string{"set-mnemonic", "h", "--kind", "WAT", "--mnemonic", "X"}, "unknown --kind"},
		{"mne-no-text", []string{"set-mnemonic", "h", "--kind", "DEST_MNE"}, "--mnemonic is required"},
		// set-tags: bad kind + missing tags.
		{"tags-bad-kind", []string{"set-tags", "h", "--kind", "WAT", "--tags", "a"}, "unknown --kind"},
		{"tags-no-tags", []string{"set-tags", "h", "--kind", "RM_DEST_TAGS"}, "--tags is required"},
		// salvo: missing/unknown op, missing group, rename without new-name.
		{"salvo-no-op", []string{"salvo", "h", "--group", "G"}, "--op is required"},
		{"salvo-bad-op", []string{"salvo", "h", "--op", "frob", "--group", "G"}, "unknown --op"},
		{"salvo-no-group", []string{"salvo", "h", "--op", "run"}, "--group is required"},
		{"salvo-rename-no-name", []string{"salvo", "h", "--op", "rename", "--group", "G"}, "--new-name is required"},
		{"salvo-desc-no-text", []string{"salvo", "h", "--op", "description", "--group", "G"}, "--description is required"},
		// category: missing/unknown op, missing category, per-op required attrs
		// (§4.2 table), bad §3.3 item-type.
		{"cat-no-op", []string{"category", "h", "--category", "C"}, "--op is required"},
		{"cat-bad-op", []string{"category", "h", "--op", "frob", "--category", "C"}, "unknown --op"},
		{"cat-no-category", []string{"category", "h", "--op", "create"}, "--category is required"},
		{"cat-create-no-name", []string{"category", "h", "--op", "create", "--category", "C"}, "--name is required"},
		{"cat-modify-missing", []string{"category", "h", "--op", "modify", "--category", "C", "--index", "1"}, "--index, --item-type and --value are required"},
		{"cat-modify-all-missing", []string{"category", "h", "--op", "modify-all", "--category", "C"}, "--item-type and --value are required"},
		{"cat-modify-desc-missing", []string{"category", "h", "--op", "modify-desc", "--category", "C"}, "--description is required"},
		{"cat-delete-item-missing", []string{"category", "h", "--op", "delete-item", "--category", "C"}, "--index is required"},
		{"cat-bad-item-type", []string{"category", "h", "--op", "modify-all", "--category", "C", "--item-type", "WAT", "--value", "V"}, "unknown --item-type"},
		// set-value: missing required addressing.
		{"setval-missing", []string{"set-value", "h", "--device", "D"}, "are required"},
		// obtain-datastore: missing --name.
		{"ds-no-name", []string{"obtain-datastore", "h"}, "--name is required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := runCerebrum(context.Background(), c.args)
			if err == nil {
				t.Fatalf("args %v returned nil, want %q", c.args, c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("args %v error = %q, want substring %q", c.args, err.Error(), c.want)
			}
		})
	}
}

// TestCerebrumWriteVerbsRequireHost: with valid flags but no host argument,
// the verb must fail with "missing host" before dialing.
func TestCerebrumWriteVerbsRequireHost(t *testing.T) {
	cases := [][]string{
		{"lock", "--kind", "DEST_LOCK", "--dest", "1", "--level", "1"},
		{"unlock", "--kind", "DEST_LOCK", "--dest", "1", "--level", "1"},
		{"set-mnemonic", "--kind", "DEST_MNE", "--dest", "1", "--mnemonic", "X"},
		{"set-tags", "--kind", "RM_DEST_TAGS", "--dest", "1", "--tags", "a"},
		{"salvo", "--op", "run", "--group", "G"},
		{"category", "--op", "create", "--category", "C", "--name", "N"},
		{"set-value", "--device", "D", "--sub-device", "S", "--object", "O", "--value", "V"},
		{"obtain-datastore", "--name", "p"},
	}
	for _, args := range cases {
		t.Run(args[0], func(t *testing.T) {
			err := runCerebrum(context.Background(), args)
			if err == nil || !strings.Contains(err.Error(), "missing host") {
				t.Fatalf("verb %q: want missing-host error, got %v", args[0], err)
			}
		})
	}
}

// TestWriteVerbPortReachesDial pins the dropped-connection-flags fix: the §4
// write verbs (and obtain-datastore) parse the shared connection flags in
// their own FlagSet, and those values MUST reach the dialer. Before the fix
// they re-parsed only the leftover host, silently dialing the default port.
// Port 1 on loopback refuses instantly; the dial error must name it.
func TestWriteVerbPortReachesDial(t *testing.T) {
	cases := [][]string{
		{"salvo", "--op", "run", "--group", "G", "--user", "u", "--pass", "p", "--port", "1", "--timeout", "500ms", "127.0.0.1"},
		{"obtain-datastore", "--name", "x", "--port", "1", "--timeout", "500ms", "127.0.0.1"},
	}
	for _, args := range cases {
		t.Run(args[0], func(t *testing.T) {
			err := runCerebrum(context.Background(), args)
			if err == nil || !strings.Contains(err.Error(), ":1") {
				t.Fatalf("verb %q: want dial error naming port 1, got %v", args[0], err)
			}
		})
	}
}

// TestSalvoOpType / TestCategoryOpType pin the op -> wire TYPE maps.
func TestSalvoOpType(t *testing.T) {
	cases := map[string]string{"run": "RUN", "save": "SAVE", "rename": "RENAME", "description": "DESCRIPTION", "delete": "DELETE"}
	for in, want := range cases {
		got, err := salvoOpType(in)
		if err != nil || got != want {
			t.Errorf("salvoOpType(%q) = %q,%v want %q", in, got, err, want)
		}
	}
	if _, err := salvoOpType("nope"); err == nil {
		t.Error("salvoOpType(nope) should error")
	}
}

func TestCategoryOpType(t *testing.T) {
	cases := map[string]string{
		"create": "CREATE", "modify": "MODIFY_ITEM", "modify-all": "MODIFY_ALL",
		"modify-desc": "MODIFY_DESC", "delete": "DELETE", "delete-item": "DELETE_ITEM",
	}
	for in, want := range cases {
		got, err := categoryOpType(in)
		if err != nil || got != want {
			t.Errorf("categoryOpType(%q) = %q,%v want %q", in, got, err, want)
		}
	}
	if _, err := categoryOpType("nope"); err == nil {
		t.Error("categoryOpType(nope) should error")
	}
}

func TestRequireKind(t *testing.T) {
	if err := requireKind("dest_lock", "lock", "SRCE_LOCK", "DEST_LOCK"); err != nil {
		t.Fatalf("case-insensitive match failed: %v", err)
	}
	if err := requireKind("bogus", "lock", "SRCE_LOCK"); err == nil {
		t.Fatal("requireKind(bogus) should error")
	}
}

// TestCerebrumDeviceConfigValidateFlags pins the pre-dial validation for the
// 0v16 device-config verb. Each case must error before any network I/O.
func TestCerebrumDeviceConfigValidateFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no-op", []string{"device-config"}, "missing operation"},
		{"bad-op", []string{"device-config", "frob", "--ip", "1.2.3.4"}, "unknown operation"},
		{"add-no-ip", []string{"device-config", "add", "--device-type", "snmp", "h"}, "--ip is required"},
		{"add-no-device-type", []string{"device-config", "add", "--ip", "1.2.3.4", "h"}, "--device-type is required"},
		{"add-bad-device-type", []string{"device-config", "add", "--ip", "1.2.3.4", "--device-type", "frob", "h"}, "unknown --device-type"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := runCerebrum(context.Background(), c.args)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("args %v error = %v, want substring %q", c.args, err, c.want)
			}
		})
	}
}

// TestCerebrumDeviceConfigRequireHost: valid flags but no host → missing-host
// error (REMOVE skips the body, so this also covers the no-body path).
func TestCerebrumDeviceConfigRequireHost(t *testing.T) {
	cases := [][]string{
		{"device-config", "add", "--ip", "1.2.3.4", "--device-type", "generic", "--conn-type", "TCP", "--port-number", "9000", "--name", "G1"},
		{"device-config", "add", "--ip", "1.2.3.4", "--device-type", "panel", "--cpf", "x", "--panel-type", "T"},
		{"device-config", "add", "--ip", "1.2.3.4", "--device-type", "router", "--baud", "9600", "--max-level", "4"},
		{"device-config", "add", "--ip", "1.2.3.4", "--device-type", "snmp", "--name", "S1", "--snmp-port", "161"},
		{"device-config", "remove", "--ip", "1.2.3.4"},
	}
	for _, args := range cases {
		t.Run(args[2], func(t *testing.T) {
			err := runCerebrum(context.Background(), args)
			if err == nil || !strings.Contains(err.Error(), "missing host") {
				t.Fatalf("args %v: want missing-host, got %v", args, err)
			}
		})
	}
}

// TestLockModeFlagRejectsBad: an unknown --mode on lock errors before dial.
func TestLockModeFlagRejectsBad(t *testing.T) {
	err := runCerebrum(context.Background(), []string{"lock", "h", "--kind", "DEST_LOCK", "--dest", "1", "--mode", "frob"})
	if err == nil || !strings.Contains(err.Error(), "unknown --mode") {
		t.Fatalf("want unknown --mode error, got %v", err)
	}
}

func TestLockModeValue(t *testing.T) {
	cases := map[string]codec.LockKind{
		"unlocked":       codec.LockUnlocked,
		"locked":         codec.LockLocked,
		"protected":      codec.LockProtected,
		"locked_path":    codec.LockLockedPath,
		"locked-path":    codec.LockLockedPath,
		"protected_path": codec.LockProtectedPath,
		"protected-path": codec.LockProtectedPath,
		"PROTECTED":      codec.LockProtected,
	}
	for in, want := range cases {
		got, err := lockModeValue(in)
		if err != nil || got != want {
			t.Errorf("lockModeValue(%q) = %q,%v want %q", in, got, err, want)
		}
	}
	if _, err := lockModeValue("nope"); err == nil {
		t.Error("lockModeValue(nope) should error")
	}
}

func TestDeviceConfigOpType(t *testing.T) {
	cases := map[string]codec.DeviceConfigType{
		"add": codec.DeviceConfigAdd, "modify": codec.DeviceConfigModify, "remove": codec.DeviceConfigRemove,
		"ADD": codec.DeviceConfigAdd,
	}
	for in, want := range cases {
		got, err := deviceConfigOpType(in)
		if err != nil || got != want {
			t.Errorf("deviceConfigOpType(%q) = %q,%v want %q", in, got, err, want)
		}
	}
	if _, err := deviceConfigOpType("frob"); err == nil {
		t.Error("deviceConfigOpType(frob) should error")
	}
}

// TestBuildDeviceConfigBody pins the flag->codec-struct mapping for every
// device type, including the optional sub-struct gating (protocol / serial /
// router params only attached when their flags are set).
func TestBuildDeviceConfigBody(t *testing.T) {
	t.Run("generic-with-protocol", func(t *testing.T) {
		dc := &codec.DeviceConfiguration{}
		dt, err := buildDeviceConfigBody(dc, "generic", deviceConfigBodyFlags{
			device: "DRV", name: "G1", connType: "TCP", port: "9000",
		})
		if err != nil || dt != codec.ConfigDeviceGeneric {
			t.Fatalf("dt=%q err=%v", dt, err)
		}
		if dc.Generic == nil || dc.Generic.Protocol == nil || dc.Generic.Protocol.PortNumber != "9000" {
			t.Fatalf("generic body = %+v", dc.Generic)
		}
	})
	t.Run("generic-no-protocol", func(t *testing.T) {
		dc := &codec.DeviceConfiguration{}
		_, err := buildDeviceConfigBody(dc, "generic", deviceConfigBodyFlags{device: "DRV"})
		if err != nil || dc.Generic == nil || dc.Generic.Protocol != nil {
			t.Fatalf("expected nil protocol, got %+v err=%v", dc.Generic, err)
		}
	})
	t.Run("panel", func(t *testing.T) {
		dc := &codec.DeviceConfiguration{}
		dt, err := buildDeviceConfigBody(dc, "panel", deviceConfigBodyFlags{cpf: "c", panelID: "5", panelType: "T"})
		if err != nil || dt != codec.ConfigDevicePanel || dc.Panel == nil || dc.Panel.CPF != "c" {
			t.Fatalf("panel body = %+v dt=%q err=%v", dc.Panel, dt, err)
		}
	})
	t.Run("router-full", func(t *testing.T) {
		dc := &codec.DeviceConfiguration{}
		dt, err := buildDeviceConfigBody(dc, "router", deviceConfigBodyFlags{
			routerType: "5", baud: "9600", parity: "N", maxLevel: "4", maxSource: "1024", maxDest: "1024",
		})
		if err != nil || dt != codec.ConfigDeviceRouter {
			t.Fatalf("dt=%q err=%v", dt, err)
		}
		if dc.Router == nil || dc.Router.Serial == nil || dc.Router.Router == nil {
			t.Fatalf("router body = %+v", dc.Router)
		}
	})
	t.Run("router-no-subs", func(t *testing.T) {
		dc := &codec.DeviceConfiguration{}
		_, err := buildDeviceConfigBody(dc, "router", deviceConfigBodyFlags{routerType: "5"})
		if err != nil || dc.Router == nil || dc.Router.Serial != nil || dc.Router.Router != nil {
			t.Fatalf("expected bare router, got %+v err=%v", dc.Router, err)
		}
	})
	t.Run("snmp", func(t *testing.T) {
		dc := &codec.DeviceConfiguration{}
		dt, err := buildDeviceConfigBody(dc, "snmp", deviceConfigBodyFlags{snmpName: "S1", snmpPort: "161"})
		if err != nil || dt != codec.ConfigDeviceSNMP || dc.SNMP == nil || dc.SNMP.Port != "161" {
			t.Fatalf("snmp body = %+v dt=%q err=%v", dc.SNMP, dt, err)
		}
	})
	t.Run("empty-type", func(t *testing.T) {
		if _, err := buildDeviceConfigBody(&codec.DeviceConfiguration{}, "", deviceConfigBodyFlags{}); err == nil {
			t.Fatal("empty device-type should error")
		}
	})
	t.Run("bad-type", func(t *testing.T) {
		if _, err := buildDeviceConfigBody(&codec.DeviceConfiguration{}, "frob", deviceConfigBodyFlags{}); err == nil {
			t.Fatal("bad device-type should error")
		}
	})
}

func TestRouteTargetFromFlags(t *testing.T) {
	def := routeTargetFromFlags("0.0.0.0", "")
	if def.IPAddress != "0.0.0.0" || def.DeviceType != codec.DeviceType("ROUTER") {
		t.Fatalf("default target = %+v", def)
	}
	byName := routeTargetFromFlags("0.0.0.0", "MYDEV")
	if byName.DeviceName != "MYDEV" || byName.IPAddress != "" {
		t.Fatalf("device-name target = %+v", byName)
	}
	byIP := routeTargetFromFlags("10.1.2.3", "")
	if byIP.IPAddress != "10.1.2.3" {
		t.Fatalf("router IP target = %+v", byIP)
	}
}

// TestNamePaddingIsVisible pins the fix for a live-session trap.
//
// --by-name matches DEVICE_NAME byte-for-byte and Cerebrum pads some
// names and not others. Printed bare, "NOC" and "NOC " look identical,
// so an operator copies what the terminal showed, the obtain NACKs 10,
// and the error blames the obtain. On 2026-08-25 that cost an hour:
// every by-name call against the NMOS NOC device failed while the same
// call by IP succeeded, and the whole difference was one trailing
// space.
func TestNamePaddingIsVisible(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"trailing space is shown", "Cerebrum NMOS NOC ", `"Cerebrum NMOS NOC "`},
		{"leading space is shown", " CONVERT", `" CONVERT"`},
		{"tab is shown", "CONVERT\t", `"CONVERT\t"`},
		// An unpadded name stays bare. Quoting every name would make
		// the quotes decoration; the point is that quotes MEAN
		// "this string has edges you cannot see".
		{"unpadded stays bare", "Cerebrum NMOS NOC", "Cerebrum NMOS NOC"},
		{"inner spaces are not padding", "bm-n-nncvt-001", "bm-n-nncvt-001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := quoteIfPadded(tc.in); got != tc.want {
				t.Errorf("quoteIfPadded(%q) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
	// displayName keeps its empty-string dash and gains the same
	// visibility for padded values.
	if got := displayName(""); got != "-" {
		t.Errorf(`displayName("") = %q, want "-"`, got)
	}
	if got := displayName("NOC "); got != `"NOC "` {
		t.Errorf(`displayName("NOC ") = %s, want quoted`, got)
	}
}

// TestByNameHintNamesTheSuspect: the server refuses an unknown
// DEVICE_NAME with the same code it uses for a bad object path, so the
// hint is the only thing that points at whitespace.
func TestByNameHintNamesTheSuspect(t *testing.T) {
	nack := &codec.NackError{ID: codec.NackOneOrMoreObtainsInvalid}

	// Padded name: say so, and say what to try.
	got := byNameHint(nack, "Cerebrum NMOS NOC ", true)
	if !strings.Contains(got.Error(), "padded") || !strings.Contains(got.Error(), `"Cerebrum NMOS NOC"`) {
		t.Errorf("padded-name hint should name the trimmed form, got %q", got)
	}
	// The wire's own words must survive - a hint that displaces the
	// real error is worse than no hint.
	if !strings.Contains(got.Error(), "nack") {
		t.Errorf("hint replaced the underlying error: %q", got)
	}

	// Unpadded name: still hint, because the name the operator copied
	// may have been padded at the source.
	got = byNameHint(nack, "Cerebrum NMOS NOC", true)
	if !strings.Contains(got.Error(), "byte-for-byte") {
		t.Errorf("unpadded by-name hint missing: %q", got)
	}

	// By IP: the name is not in play, so no hint.
	if got := byNameHint(nack, "10.44.55.56", false); got != error(nack) {
		t.Errorf("by-IP should pass the error through unchanged, got %q", got)
	}
	// A different NACK is not a naming problem.
	other := &codec.NackError{ID: codec.NackNoLicenceAvailable}
	if got := byNameHint(other, "NOC ", true); got != error(other) {
		t.Errorf("unrelated NACK should pass through unchanged, got %q", got)
	}
	if byNameHint(nil, "NOC ", true) != nil {
		t.Error("nil error must stay nil")
	}
}
// TestWatchObjectListGrammar: --object accepts one path, a
// ';'-separated list (the same grammar --path already uses in
// export/extract), or "GROUP.*" for every leaf under a group.
//
// Only the non-expanding forms are exercised here; ".*" needs a live
// obtain and is covered by the consumer package's fake-WS tests.
func TestWatchObjectListGrammar(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want []string
	}{
		{"single path", "Nodes.[u].SubID", []string{"Nodes.[u].SubID"}},
		{"list", "Nodes.[u].SubID;Nodes.[u].Connected", []string{"Nodes.[u].SubID", "Nodes.[u].Connected"}},
		{"whitespace around separators is ignored", " a ; b ", []string{"a", "b"}},
		{"empty entries are dropped", "a;;b;", []string{"a", "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cerebrumWatchObjects(nil, 0, "dev", true, "0", tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("got %v, want %v", got, tc.want)
					break
				}
			}
		})
	}
	// A list of nothing is a usage error, not an empty subscription.
	if _, err := cerebrumWatchObjects(nil, 0, "dev", true, "0", " ; ; "); err == nil {
		t.Error("an all-empty --object should be refused")
	}
}
// TestCerebrumDMRowMatchesGenericWatch pins the Tree/DM layout so an
// NB watch reads like `dhs watch` on acp1/acp2. One grammar everywhere
// is the point of the Tree/DM template (docs/protocols/verbs.md 12b);
// an operator comparing a CONVERT over acp2 against the same card over
// Cerebrum should be diffing VALUES, not re-learning a layout.
func TestCerebrumDMRowMatchesGenericWatch(t *testing.T) {
	ts := time.Date(2026, 8, 26, 1, 2, 3, 0, time.UTC)
	capture := func(fn func()) string {
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		fn()
		_ = w.Close()
		os.Stdout = old
		var sb strings.Builder
		_, _ = io.Copy(&sb, r)
		return sb.String()
	}

	// A readable value carries its access bits and type.
	got := capture(func() {
		cerebrumDMRow(ts, codec.DeviceObjectValue{
			Object: "Nodes.[abc].SubID", Value: "1000",
			Available: true, Readable: true, Writable: true, DataType: "INTEGER",
		})
	})
	for _, want := range []string{"01:02:03", "SubID", "RW-", "integer", "1000"} {
		if !strings.Contains(got, want) {
			t.Errorf("row missing %q:\n%s", want, got)
		}
	}

	// available=0 is a CHILD GROUP arriving inside a VALUE response,
	// not an object with an empty value — rendering it as the latter
	// reads as "this object is empty" instead of "this is a folder".
	got = capture(func() {
		cerebrumDMRow(ts, codec.DeviceObjectValue{Object: "Nodes.[abc].Interfaces.[eno1]"})
	})
	if !strings.Contains(got, "group") || !strings.Contains(got, "<children>") {
		t.Errorf("child group should render as a group row:\n%s", got)
	}

	// Units ride with the value, as in the generic watch.
	got = capture(func() {
		cerebrumDMRow(ts, codec.DeviceObjectValue{
			Object: "a.Delay", Value: "5.0", Available: true, Readable: true, Units: "ms",
		})
	})
	if !strings.Contains(got, "5.0 ms") {
		t.Errorf("units should follow the value:\n%s", got)
	}
}

// TestTruncatePathKeepsBothEnds: Cerebrum paths are front-loaded with a
// group and a bracketed UUID, so head-truncation leaves every row with
// an identical 52-character prefix and hides the segment that differs.
func TestTruncatePathKeepsBothEnds(t *testing.T) {
	long := "Nodes.[ab469b7c-0100-1000-a000-3ceceffd5b65].SDP_Resend_Mode"
	got := truncatePath(long, 40)
	if len([]rune(got)) != 40 {
		t.Errorf("truncatePath returned %d runes, want 40: %q", len([]rune(got)), got)
	}
	if !strings.HasPrefix(got, "Nodes.") {
		t.Errorf("head lost: %q", got)
	}
	if !strings.HasSuffix(got, "SDP_Resend_Mode") {
		t.Errorf("tail lost — the tail IS the object name: %q", got)
	}
	// Short enough already: untouched.
	if got := truncatePath("a.b", 40); got != "a.b" {
		t.Errorf("short path was modified: %q", got)
	}
}