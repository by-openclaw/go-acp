package mib

import (
	"testing"

	"dhs/internal/snmp/codec"
)

func TestETVAlarmTrapName(t *testing.T) {
	rx1290 := codec.MustParseOID("1.3.6.1.4.1.1773.1.3.200") // product enterprise
	tt1260 := codec.MustParseOID("1.3.6.1.4.1.1773.1.3.200")
	snell := codec.MustParseOID("1.3.6.1.4.1.7995")

	cases := []struct {
		name       string
		enterprise codec.OID
		generic    codec.GenericTrap
		specific   int
		want       string
		ok         bool
	}{
		{"major", rx1290, codec.EnterpriseSpecific, 5, "alarmMajor", true},
		{"minor", rx1290, codec.EnterpriseSpecific, 4, "alarmMinor", true},
		{"critical", rx1290, codec.EnterpriseSpecific, 6, "alarmCritical", true},
		{"normal", rx1290, codec.EnterpriseSpecific, 1, "alarmNormal", true},
		{"eventNotify", tt1260, codec.EnterpriseSpecific, 101, "eventNotify", true},
		{"not enterprise-specific", rx1290, codec.ColdStart, 5, "", false},
		{"non-ETV enterprise", snell, codec.EnterpriseSpecific, 5, "", false},
		{"unknown specific", rx1290, codec.EnterpriseSpecific, 999, "", false},
		{"negative specific", rx1290, codec.EnterpriseSpecific, -1, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ETVAlarmTrapName(c.enterprise, c.generic, c.specific)
			if ok != c.ok || got != c.want {
				t.Errorf("ETVAlarmTrapName = (%q, %v), want (%q, %v)", got, ok, c.want, c.ok)
			}
		})
	}
}
