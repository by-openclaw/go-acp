package is09

import (
	"strings"
	"testing"
)

// specExample is the verbatim payload from
// https://specs.amwa.tv/is-09/releases/v1.0.0/examples/global-get-200.html.
// Used as the canonical happy-path round-trip test.
const specExample = `{
  "id": "3b8be755-08ff-452b-b217-c9151eb21193",
  "version": "1441700172:318426300",
  "label": "ZBQ System",
  "description": "System Global Information for ZBQ",
  "tags": {},
  "is04": {
    "heartbeat_interval": 8
  },
  "ptp": {
    "announce_receipt_timeout": 2,
    "domain_number": 57
  },
  "syslogv2": {
    "hostname": "biglogger.ebu.ch",
    "port": 3477
  }
}`

func validGlobal() Global {
	return Global{
		ID:          "3b8be755-08ff-452b-b217-c9151eb21193",
		Version:     "1441700172:318426300",
		Label:       "ZBQ System",
		Description: "System Global Information for ZBQ",
		Tags:        map[string][]string{},
		IS04:        IS04Config{HeartbeatInterval: 8},
		PTP:         PTPConfig{AnnounceReceiptTimeout: 2, DomainNumber: 57},
	}
}

func TestDecodeSpecExample(t *testing.T) {
	g, err := Decode([]byte(specExample))
	if err != nil {
		t.Fatalf("decode spec example: %v", err)
	}
	if g.ID != "3b8be755-08ff-452b-b217-c9151eb21193" {
		t.Fatalf("ID = %q", g.ID)
	}
	if g.IS04.HeartbeatInterval != 8 {
		t.Fatalf("heartbeat_interval = %d", g.IS04.HeartbeatInterval)
	}
	if g.PTP.DomainNumber != 57 {
		t.Fatalf("ptp.domain_number = %d", g.PTP.DomainNumber)
	}
	if g.SyslogV2 == nil || g.SyslogV2.Hostname != "biglogger.ebu.ch" || g.SyslogV2.Port != 3477 {
		t.Fatalf("syslogv2 = %+v", g.SyslogV2)
	}
	if g.Syslog != nil {
		t.Fatalf("syslog should be absent (omitempty), got %+v", g.Syslog)
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	in := validGlobal()
	in.SyslogV2 = &SyslogConfig{Hostname: "biglogger.ebu.ch", Port: 3477}

	wire, err := in.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := Decode(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.IS04.HeartbeatInterval != in.IS04.HeartbeatInterval {
		t.Fatalf("heartbeat_interval mismatch")
	}
	if out.SyslogV2 == nil || *out.SyslogV2 != *in.SyslogV2 {
		t.Fatalf("syslogv2 mismatch: %+v vs %+v", out.SyslogV2, in.SyslogV2)
	}
}

func TestDecodeRejectsUnknownKey(t *testing.T) {
	withExtra := strings.Replace(specExample,
		`"tags": {}`,
		`"tags": {}, "rogue_field": "should be rejected"`, 1)
	_, err := Decode([]byte(withExtra))
	if err == nil {
		t.Fatal("expected unknown-key rejection")
	}
	if !strings.Contains(err.Error(), "rogue_field") {
		t.Fatalf("error should name the rogue key: %v", err)
	}
}

func TestDecodeRejectsTrailingJSON(t *testing.T) {
	_, err := Decode([]byte(specExample + `{}`))
	if err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("expected trailing-content rejection, got %v", err)
	}
}

func TestValidateRequiredMissing(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Global)
		want   string
	}{
		{"id absent", func(g *Global) { g.ID = "" }, "id"},
		{"version absent", func(g *Global) { g.Version = "" }, "version"},
		{"label absent", func(g *Global) { g.Label = "" }, "label"},
		{"description absent", func(g *Global) { g.Description = "" }, "description"},
		{"tags nil", func(g *Global) { g.Tags = nil }, "tags"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := validGlobal()
			tc.mutate(&g)
			err := g.Validate()
			if err == nil {
				t.Fatalf("expected validation error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q should mention %q", err, tc.want)
			}
		})
	}
}

func TestValidateIDPattern(t *testing.T) {
	g := validGlobal()
	g.ID = "not-a-uuid"
	if err := g.Validate(); err == nil {
		t.Fatal("expected uuid pattern rejection")
	}
	// Valid UUID v4
	g.ID = "f47ac10b-58cc-4372-a567-0e02b2c3d479"
	if err := g.Validate(); err != nil {
		t.Fatalf("v4 uuid should be accepted: %v", err)
	}
	// v6+ not in the allowed set [1-5]
	g.ID = "f47ac10b-58cc-6372-a567-0e02b2c3d479"
	if err := g.Validate(); err == nil {
		t.Fatal("v6 uuid must be rejected by spec pattern [1-5]")
	}
}

func TestValidateVersionPattern(t *testing.T) {
	g := validGlobal()
	for _, v := range []string{"abc", "123:", ":456", "123:abc"} {
		g.Version = v
		if err := g.Validate(); err == nil {
			t.Fatalf("version %q should be rejected", v)
		}
	}
	g.Version = "0:0"
	if err := g.Validate(); err != nil {
		t.Fatalf("0:0 should be valid: %v", err)
	}
}

func TestValidateRangeBounds(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Global)
		want   string
	}{
		{"heartbeat too low", func(g *Global) { g.IS04.HeartbeatInterval = 0 }, "heartbeat_interval"},
		{"heartbeat too high", func(g *Global) { g.IS04.HeartbeatInterval = 1001 }, "heartbeat_interval"},
		{"announce too low", func(g *Global) { g.PTP.AnnounceReceiptTimeout = 1 }, "announce_receipt_timeout"},
		{"announce too high", func(g *Global) { g.PTP.AnnounceReceiptTimeout = 11 }, "announce_receipt_timeout"},
		{"domain too low", func(g *Global) { g.PTP.DomainNumber = -1 }, "domain_number"},
		{"domain too high", func(g *Global) { g.PTP.DomainNumber = 128 }, "domain_number"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := validGlobal()
			tc.mutate(&g)
			err := g.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %s error, got %v", tc.want, err)
			}
		})
	}
}

func TestValidateBoundary(t *testing.T) {
	g := validGlobal()
	g.IS04.HeartbeatInterval = 1
	g.PTP.AnnounceReceiptTimeout = 2
	g.PTP.DomainNumber = 0
	if err := g.Validate(); err != nil {
		t.Fatalf("low boundaries should pass: %v", err)
	}
	g.IS04.HeartbeatInterval = 1000
	g.PTP.AnnounceReceiptTimeout = 10
	g.PTP.DomainNumber = 127
	if err := g.Validate(); err != nil {
		t.Fatalf("high boundaries should pass: %v", err)
	}
}

func TestValidateSyslogBlocks(t *testing.T) {
	g := validGlobal()
	g.SyslogV2 = &SyslogConfig{Hostname: "biglogger.ebu.ch", Port: SyslogV2DefaultPort}
	g.Syslog = &SyslogConfig{Hostname: "10.0.0.1", Port: SyslogV1DefaultPort}
	if err := g.Validate(); err != nil {
		t.Fatalf("valid syslog blocks rejected: %v", err)
	}

	// Empty hostname + zero port = valid (both fields optional).
	g.SyslogV2 = &SyslogConfig{}
	if err := g.Validate(); err != nil {
		t.Fatalf("empty syslogv2 block rejected: %v", err)
	}

	// Out-of-range port
	g.SyslogV2 = &SyslogConfig{Port: 70000}
	if err := g.Validate(); err == nil || !strings.Contains(err.Error(), "syslogv2.port") {
		t.Fatalf("expected port range error, got %v", err)
	}

	// Bogus hostname
	g.SyslogV2 = &SyslogConfig{Hostname: "not_a_valid_host"}
	if err := g.Validate(); err == nil || !strings.Contains(err.Error(), "hostname") {
		t.Fatalf("expected hostname format error, got %v", err)
	}

	// IPv6 should pass
	g.SyslogV2 = &SyslogConfig{Hostname: "2001:db8::1"}
	if err := g.Validate(); err != nil {
		t.Fatalf("ipv6 hostname should pass: %v", err)
	}
}

func TestIndexBodyAndDecode(t *testing.T) {
	idx := IndexBody()
	if len(idx) != 1 || idx[0] != IndexValue {
		t.Fatalf("IndexBody = %v", idx)
	}
	out, err := DecodeIndex([]byte(`["global/"]`))
	if err != nil {
		t.Fatalf("DecodeIndex: %v", err)
	}
	if len(out) != 1 || out[0] != IndexValue {
		t.Fatalf("DecodeIndex result: %v", out)
	}
	for _, bad := range []string{`[]`, `["global/","other/"]`, `["other/"]`, `[1]`} {
		if _, err := DecodeIndex([]byte(bad)); err == nil {
			t.Fatalf("DecodeIndex(%q) should fail", bad)
		}
	}
}
