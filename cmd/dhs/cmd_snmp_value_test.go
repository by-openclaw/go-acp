package main

import (
	"bytes"
	"testing"

	"dhs/internal/snmp/codec"
)

// Octets that are not text are written as hex — net-snmp's x. An ATEME
// DR5000 keeps a trap destination as four raw bytes; "0A06FA65" given as
// text would be eight.
func TestSNMPSetValueTakesHexOctets(t *testing.T) {
	for _, raw := range []string{"0A06FA65", "0a:06:fa:65", "0a 06 fa 65", "0A-06-FA-65"} {
		for _, typ := range []string{"x", "hex", "X"} {
			v, err := parseSNMPValue(typ, raw)
			if err != nil {
				t.Errorf("%s %q: %v", typ, raw, err)
				continue
			}
			if v.Type != codec.TypeOctetString || !bytes.Equal(v.Bytes, []byte{0x0a, 0x06, 0xfa, 0x65}) {
				t.Errorf("%s %q = %v %x, want the four octets", typ, raw, v.Type, v.Bytes)
			}
		}
	}
	for _, bad := range []string{"", "zz", "0A0", "10.6.250.101"} {
		if _, err := parseSNMPValue("x", bad); err == nil {
			t.Errorf("%q was accepted as hex octets", bad)
		}
	}
	if _, err := parseSNMPValue("q", "1"); err == nil {
		t.Error("an unknown type letter was accepted")
	}
}

// A password on a command line is in shell history and ps, so a raw verb
// takes its community from the environment; the flag still wins.
func TestRawVerbsTakeTheCommunityFromTheEnvironment(t *testing.T) {
	t.Setenv("SNMP_COMMUNITY", "read-env")
	t.Setenv("SNMP_WRITE_COMMUNITY", "write-env")
	for _, tc := range []struct {
		name string
		f    snmpFlags
		want string
	}{
		{"a read", snmpFlags{}, "read-env"},
		{"a write", snmpFlags{write: true}, "write-env"},
		{"the flag on a read", snmpFlags{community: "flag"}, "flag"},
		{"the flag on a write", snmpFlags{community: "flag", write: true}, "flag"},
	} {
		if got := tc.f.communityEnv(); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
	t.Setenv("SNMP_WRITE_COMMUNITY", "")
	if got := (&snmpFlags{write: true}).communityEnv(); got != "" {
		t.Errorf("no write community must not borrow the read one: %q", got)
	}
}
