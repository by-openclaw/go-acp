package main

import (
	"reflect"
	"testing"

	"acp/internal/cerebrum-nb/codec"
)

// TestCerebrumRouterRowsFromList pins the list-routers builder against
// spec §3.1 DEVICE_TYPE rules. The route-master sentinel
// (`0.0.0.0/ROUTER`, role `aggregator`) is always row 0; physical
// ROUTER-class entries from the LIST snapshot follow with case
// insensitive matching on the BASE class (suffixes like `:N`
// preserved in the displayed DEVICE_TYPE).
func TestCerebrumRouterRowsFromList(t *testing.T) {
	cases := []struct {
		name string
		in   []codec.DeviceEntry
		want []routerRow
	}{
		{
			name: "empty list — only route-master",
			in:   nil,
			want: []routerRow{
				{IPAddress: "0.0.0.0", DeviceType: "ROUTER", DeviceName: "(route-master)", Role: "aggregator"},
			},
		},
		{
			name: "all DEVICE-class — only route-master",
			in: []codec.DeviceEntry{
				{IPAddress: "10.0.0.10", DeviceType: codec.DeviceType("DEVICE"), DeviceName: "Powercore"},
				{IPAddress: "10.0.0.11", DeviceType: codec.DeviceType("SNMP"), DeviceName: "Switch"},
			},
			want: []routerRow{
				{IPAddress: "0.0.0.0", DeviceType: "ROUTER", DeviceName: "(route-master)", Role: "aggregator"},
			},
		},
		{
			name: "one ROUTER row appended",
			in: []codec.DeviceEntry{
				{IPAddress: "10.0.0.55", DeviceType: codec.DeviceType("ROUTER"), DeviceName: "MTX1"},
			},
			want: []routerRow{
				{IPAddress: "0.0.0.0", DeviceType: "ROUTER", DeviceName: "(route-master)", Role: "aggregator"},
				{IPAddress: "10.0.0.55", DeviceType: "ROUTER", DeviceName: "MTX1", Role: "physical"},
			},
		},
		{
			name: "case-insensitive: Router (mixed) and router (lower) both qualify",
			in: []codec.DeviceEntry{
				{IPAddress: "10.0.0.55", DeviceType: codec.DeviceType("Router"), DeviceName: "Mixed"},
				{IPAddress: "10.0.0.56", DeviceType: codec.DeviceType("router"), DeviceName: "Lower"},
			},
			want: []routerRow{
				{IPAddress: "0.0.0.0", DeviceType: "ROUTER", DeviceName: "(route-master)", Role: "aggregator"},
				{IPAddress: "10.0.0.55", DeviceType: "Router", DeviceName: "Mixed", Role: "physical"},
				{IPAddress: "10.0.0.56", DeviceType: "router", DeviceName: "Lower", Role: "physical"},
			},
		},
		{
			name: "sub-device suffix: ROUTER:2 → role physical:2, full type preserved",
			in: []codec.DeviceEntry{
				{IPAddress: "10.0.0.55", DeviceType: codec.DeviceType("ROUTER:2"), DeviceName: "MTX1-Mat2"},
			},
			want: []routerRow{
				{IPAddress: "0.0.0.0", DeviceType: "ROUTER", DeviceName: "(route-master)", Role: "aggregator"},
				{IPAddress: "10.0.0.55", DeviceType: "ROUTER:2", DeviceName: "MTX1-Mat2", Role: "physical:2"},
			},
		},
		{
			name: "multi-INSTANCE entry: one DEVICE flattens out, ROUTER kept",
			in: []codec.DeviceEntry{
				{
					IPAddress:   "10.0.0.55",
					DeviceName:  "MultiClass",
					DeviceTypes: []codec.DeviceType{"DEVICE", "ROUTER"},
				},
			},
			want: []routerRow{
				{IPAddress: "0.0.0.0", DeviceType: "ROUTER", DeviceName: "(route-master)", Role: "aggregator"},
				{IPAddress: "10.0.0.55", DeviceType: "ROUTER", DeviceName: "MultiClass", Role: "physical"},
			},
		},
		{
			name: "empty class string skipped (defensive)",
			in: []codec.DeviceEntry{
				{IPAddress: "10.0.0.99", DeviceType: codec.DeviceType(""), DeviceName: "Bogus"},
			},
			want: []routerRow{
				{IPAddress: "0.0.0.0", DeviceType: "ROUTER", DeviceName: "(route-master)", Role: "aggregator"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := cerebrumRouterRowsFromList(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("rows mismatch\n  got  %+v\n  want %+v", got, c.want)
			}
		})
	}
}

func TestReorderFlagsFirst(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "flags after positional",
			in:   []string{"10.41.64.95", "--port", "4008", "--user", "u", "--pass", "p"},
			want: []string{"--port", "4008", "--user", "u", "--pass", "p", "10.41.64.95"},
		},
		{
			name: "flags already first",
			in:   []string{"--port", "4008", "10.41.64.95"},
			want: []string{"--port", "4008", "10.41.64.95"},
		},
		{
			name: "interleaved",
			in:   []string{"--port", "4008", "127.0.0.1", "--user", "u"},
			want: []string{"--port", "4008", "--user", "u", "127.0.0.1"},
		},
		{
			name: "bool flag",
			in:   []string{"127.0.0.1", "--tls", "--port", "443"},
			want: []string{"--tls", "--port", "443", "127.0.0.1"},
		},
		{
			name: "flag with =value",
			in:   []string{"127.0.0.1", "--port=4008"},
			want: []string{"--port=4008", "127.0.0.1"},
		},
		{
			name: "double-dash terminator preserves trailing positional",
			in:   []string{"--port", "4008", "--", "--literal-host"},
			want: []string{"--port", "4008", "--", "--literal-host"},
		},
		{
			name: "host with colon-port",
			in:   []string{"10.41.64.95:4008", "--user", "u"},
			want: []string{"--user", "u", "10.41.64.95:4008"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := reorderFlagsFirst(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("reorderFlagsFirst(%q)\n  got %q\n want %q", c.in, got, c.want)
			}
		})
	}
}
