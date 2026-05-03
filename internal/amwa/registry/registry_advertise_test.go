package registry

import (
	"reflect"
	"testing"

	codec "dhs/internal/amwa/codec/dnssd"
)

// TestPickRegistryServices_LegacyConditional pins the spec-strict rule
// that a Registry MUST advertise on the legacy
// `_nmos-registration._tcp` service type whenever it supports any of
// v1.0/v1.1/v1.2 (the minors that pre-date the rename in v1.3). AMWA
// `IS0402Test.test_01` for those minors browses the legacy name; v1.3+
// browses the modern name. Any Registry that drops the legacy advertise
// is invisible to v1.0/v1.1/v1.2 peers.
func TestPickRegistryServices_LegacyConditional(t *testing.T) {
	cases := []struct {
		name    string
		apiVers []string
		want    []string
	}{
		{
			name:    "v1.3-only — modern names only",
			apiVers: []string{"v1.3"},
			want:    []string{codec.ServiceRegister, codec.ServiceQuery},
		},
		{
			name:    "v1.2 alone — legacy required",
			apiVers: []string{"v1.2"},
			want:    []string{codec.ServiceRegister, codec.ServiceQuery, codec.ServiceRegisterLegacy},
		},
		{
			name:    "v1.0 alone — legacy required",
			apiVers: []string{"v1.0"},
			want:    []string{codec.ServiceRegister, codec.ServiceQuery, codec.ServiceRegisterLegacy},
		},
		{
			name:    "v1.1 alone — legacy required",
			apiVers: []string{"v1.1"},
			want:    []string{codec.ServiceRegister, codec.ServiceQuery, codec.ServiceRegisterLegacy},
		},
		{
			name:    "every supported minor — legacy required",
			apiVers: []string{"v1.0", "v1.1", "v1.2", "v1.3"},
			want:    []string{codec.ServiceRegister, codec.ServiceQuery, codec.ServiceRegisterLegacy},
		},
		{
			name:    "v1.2 + v1.3 — legacy required for v1.2 clients",
			apiVers: []string{"v1.2", "v1.3"},
			want:    []string{codec.ServiceRegister, codec.ServiceQuery, codec.ServiceRegisterLegacy},
		},
		{
			name:    "empty — modern names only (defensive)",
			apiVers: nil,
			want:    []string{codec.ServiceRegister, codec.ServiceQuery},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pickRegistryServices(tc.apiVers)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("pickRegistryServices(%v) = %v, want %v",
					tc.apiVers, got, tc.want)
			}
		})
	}
}
