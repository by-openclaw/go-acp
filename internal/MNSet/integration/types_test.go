//go:build integration

package mnset_integration

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The typed DM, proven against the module (#1185). The oracle is the
// device: a write the dictionary allows must round-trip on it, and a
// write the dictionary refuses must be refused by dhs BEFORE the wire —
// those are the values the FusioN6 would wrap (999.1.1.1 → 231.1.1.1)
// or take as given (DSCP 64, payload type 128).
//
// Only idle instances are written — a flow with no destination and no
// packets, and the SDI output of that flow's channel — and every write
// is put back to what it was.

// row is one export line, by column name.
type row map[string]string

func exportRows(t *testing.T, h string) []row {
	t.Helper()
	out := filepath.Join(t.TempDir(), "module.csv")
	mustRun(t, 5*time.Minute, "consumer", "mnset", "export", h, "--format", "csv", "--out", out)
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("FAIL-real: %v", err)
	}
	defer func() { _ = f.Close() }()
	recs, err := csv.NewReader(f).ReadAll()
	if err != nil || len(recs) < 2 {
		t.Fatalf("FAIL-real: export unreadable: %v", err)
	}
	var rows []row
	for _, r := range recs[1:] {
		m := row{}
		for i, col := range recs[0] {
			m[col] = r[i]
		}
		rows = append(rows, m)
	}
	return rows
}

var reFlow = regexp.MustCompile(`^flows\.([0-9a-f-]{36})\.`)

// idle finds an unassigned receive flow, an unassigned transmit flow and
// the SDI output of the receive flow's channel.
func idle(t *testing.T, rows []row) (rx, tx, sdiOut string) {
	t.Helper()
	type flow struct{ dst, pkts, label, ttl, kind string }
	flows := map[string]*flow{}
	for _, r := range rows {
		m := reFlow.FindStringSubmatch(r["path"])
		if m == nil {
			continue
		}
		f := flows[m[1]]
		if f == nil {
			f = &flow{}
			flows[m[1]] = f
		}
		switch strings.TrimPrefix(r["path"], "flows."+m[1]+".") {
		case "network.dst_ip_addr":
			f.dst = r["value"]
		case "network.pkt_cnt":
			f.pkts = r["value"]
		case "network.ttl":
			f.ttl = r["value"]
		case "format.format_type":
			f.label = r["label"]
			f.kind = r["value"]
		}
	}
	channel := ""
	for id, f := range flows {
		// Video flows only: the cases write video fields (range, TCS).
		if f.dst != "0.0.0.0" || f.kind != "video" {
			continue
		}
		switch {
		case rx == "" && f.ttl == "" && f.pkts == "0" && strings.Contains(f.label, "rx ch"):
			rx = id
			channel = strings.Fields(f.label)[0] // "CH3"
		case tx == "" && f.ttl != "":
			tx = id
		}
	}
	for _, r := range rows {
		if strings.HasPrefix(r["path"], "sdi_output.") && strings.HasSuffix(r["path"], ".label") &&
			strings.TrimPrefix(r["value"], "SDI ") == strings.TrimPrefix(channel, "CH") {
			sdiOut = strings.Split(r["path"], ".")[1]
		}
	}
	if rx == "" || tx == "" || sdiOut == "" {
		t.Skipf("no idle instance to write on (rx %q tx %q sdi_output %q) — every channel is in use", rx, tx, sdiOut)
	}
	return rx, tx, sdiOut
}

// current reads one leaf as the CLI prints it, quotes stripped.
func current(t *testing.T, h, path string) string {
	t.Helper()
	out := mustRun(t, time.Minute, "consumer", "mnset", "get", h, "--path", path)
	i := strings.LastIndex(out, "value = ")
	if i < 0 {
		t.Fatalf("FAIL-real: get %s printed no value:\n%s", path, out)
	}
	return strings.Trim(strings.TrimSpace(out[i+len("value = "):]), `"`)
}

func TestTheExportCarriesNoUntypedWritableValue(t *testing.T) {
	h := host(t)
	rows := exportRows(t, h)
	// The #1185 measure: a writable leaf whose value is a flag, a number
	// or an IPv4 address, still exported as a string.
	looks := regexp.MustCompile(`^(-?\d+|\d+\.\d+\.\d+\.\d+)$`)
	var untyped []string
	kinds := map[string]int{}
	for _, r := range rows {
		kinds[r["kind"]]++
		// A leaf with enum items is typed: an enum keeps the module's
		// word ("0" = 1 ms) and lists what it may be.
		if strings.Contains(r["access"], "W") && r["kind"] == "string" && r["enum_items"] == "" && looks.MatchString(r["value"]) {
			untyped = append(untyped, r["path"]+"="+r["value"])
		}
	}
	if len(untyped) > 0 {
		n := len(untyped)
		if n > 20 {
			untyped = untyped[:20]
		}
		t.Errorf("FAIL-real: %d writable leaves still typed string:\n%s", n, strings.Join(untyped, "\n"))
	}
	for _, k := range []string{"bool", "int", "ipaddr"} {
		if kinds[k] == 0 {
			t.Errorf("FAIL-real: no %s leaf in the export — the dictionary is not applied", k)
		}
	}
	t.Logf("kinds: %v", kinds)
}

func TestTypedWritesRoundTripOnIdleInstances(t *testing.T) {
	h := host(t)
	rx, tx, out := idle(t, exportRows(t, h))
	// also lists the fields the module rewrites itself as a side effect:
	// a new dst_ip_addr re-derives dst_mac, and setting the address back
	// does not put the MAC back.
	cases := []struct {
		path, value, printed string
		also                 []string
	}{
		{"flows." + rx + ".network.dst_udp_port", "20000", "20000", nil},
		{"flows." + rx + ".network.dst_ip_addr", "239.1.9.9", "239.1.9.9", []string{"flows." + rx + ".network.dst_mac"}},
		{"flows." + rx + ".network.pkt_filter_src_ip", "true", "true", nil},
		{"flows." + rx + ".format.range", "FULL", "FULL", nil},
		{"flows." + rx + ".format.format_colorimetry", "BT2100", "BT2100", nil},
		{"flows." + tx + ".network.ttl", "255", "255", nil},
		{"flows." + tx + ".network.dscp", "46", "46", nil},
		{"sdi_output." + out + ".input_signal_output_mode.loss_of_input", "black", "black", nil},
		{"sdi_output." + out + ".vpid.override_value", "0x85062002", "0x85062002", nil},
	}
	for _, c := range cases {
		func() {
			// Snapshot first, put back last — even when the case fails.
			paths := append([]string{c.path}, c.also...)
			before := map[string]string{}
			for _, p := range paths {
				before[p] = current(t, h, p)
			}
			defer func() {
				// The field first, then what the module derived from it:
				// while dst_ip_addr is multicast the module overrides a
				// hand-set dst_mac with the derived one.
				for _, p := range paths {
					if got, err := run(t, time.Minute, "consumer", "mnset", "set", h, "--path", p, "--value", before[p]); err != nil {
						t.Errorf("FAIL-real: restoring %s=%s: %v\n%s", p, before[p], err, got)
					}
					if after := current(t, h, p); after != before[p] {
						t.Errorf("FAIL-real: %s not restored: %s, was %s", p, after, before[p])
					}
				}
			}()
			got, err := run(t, time.Minute, "consumer", "mnset", "set", h, "--path", c.path, "--value", c.value)
			if err != nil || !strings.Contains(got, "confirmed value = ") || !strings.Contains(got, c.printed) {
				t.Errorf("FAIL-real: set %s=%s: %v\n%s", c.path, c.value, err, got)
			}
		}()
	}
}

func TestWritesTheModuleWouldMangleAreRefusedBeforeTheWire(t *testing.T) {
	h := host(t)
	rx, tx, out := idle(t, exportRows(t, h))
	cases := []struct{ path, value, why string }{
		{"flows." + rx + ".network.dst_ip_addr", "999.1.1.1", "the module stores 231.1.1.1"},
		{"flows." + tx + ".network.dscp", "64", "the module accepts DSCP 64"},
		{"flows." + tx + ".network.rtp_pt", "128", "the module accepts payload type 128"},
		{"flows." + rx + ".format.format_tcs", "LINEAR", "not a TCS the module takes"},
		{"sdi_output." + out + ".input_signal_output_mode.loss_of_input", "colorbar", "not a documented value"},
		{"flows." + rx + ".network.pkt_filter_dst_ip", "0", "fixed at 1 on a receiver"},
	}
	for _, c := range cases {
		before := current(t, h, c.path)
		got, err := run(t, time.Minute, "consumer", "mnset", "set", h, "--path", c.path, "--value", c.value)
		if err == nil {
			t.Errorf("FAIL-real: set %s=%s was accepted (%s):\n%s", c.path, c.value, c.why, got)
		} else if !strings.Contains(got, "validation") && !strings.Contains(got, "out-of-range") {
			t.Errorf("FAIL-real: set %s=%s failed, but not as a client-side refusal:\n%s", c.path, c.value, got)
		}
		if after := current(t, h, c.path); after != before {
			t.Errorf("FAIL-real: %s changed to %s after a refused write (was %s)", c.path, after, before)
		}
	}
}
