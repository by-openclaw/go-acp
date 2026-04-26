package main

import (
	"reflect"
	"testing"
)

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
