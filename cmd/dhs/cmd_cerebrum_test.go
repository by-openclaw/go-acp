package main

import (
	"context"
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
		// category: missing/unknown op, missing category.
		{"cat-no-op", []string{"category", "h", "--category", "C"}, "--op is required"},
		{"cat-bad-op", []string{"category", "h", "--op", "frob", "--category", "C"}, "unknown --op"},
		{"cat-no-category", []string{"category", "h", "--op", "create"}, "--category is required"},
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
		{"category", "--op", "create", "--category", "C"},
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
	cases := map[string]string{"create": "CREATE", "modify": "MODIFY_ITEM", "delete": "DELETE"}
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
