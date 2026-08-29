//go:build linux

package dnssd

import (
	"net"
	"testing"
)

// TestAnnounceIfaceIndexes pins the interface filter that keeps our
// Avahi announce off the loopback interface. Regression guard for the
// live defect where AVAHI_IF_UNSPEC let the daemon publish a
// 127.0.0.1 A record for our services and a peer (Cerebrum) resolved
// the registry to itself — docs/cerebrum-interop.md, root-cause
// section.
func TestAnnounceIfaceIndexes(t *testing.T) {
	mk := func(idx int, flags net.Flags) net.Interface {
		return net.Interface{Index: idx, Name: "if" + string(rune('0'+idx)), Flags: flags}
	}

	cases := []struct {
		name string
		in   []net.Interface
		want []int32
	}{
		{
			name: "loopback excluded",
			in: []net.Interface{
				mk(1, net.FlagUp|net.FlagLoopback|net.FlagMulticast),
				mk(2, net.FlagUp|net.FlagMulticast),
			},
			want: []int32{2},
		},
		{
			name: "down interface excluded",
			in: []net.Interface{
				mk(2, net.FlagMulticast),
				mk(3, net.FlagUp|net.FlagMulticast),
			},
			want: []int32{3},
		},
		{
			name: "non-multicast excluded",
			in: []net.Interface{
				mk(2, net.FlagUp),
				mk(3, net.FlagUp|net.FlagMulticast),
			},
			want: []int32{3},
		},
		{
			name: "multiple qualifying kept in order",
			in: []net.Interface{
				mk(1, net.FlagUp|net.FlagLoopback|net.FlagMulticast),
				mk(2, net.FlagUp|net.FlagMulticast),
				mk(4, net.FlagUp|net.FlagMulticast),
			},
			want: []int32{2, 4},
		},
		{
			name: "nothing qualifies yields empty (caller falls back to Unspec)",
			in: []net.Interface{
				mk(1, net.FlagUp|net.FlagLoopback|net.FlagMulticast),
				mk(2, net.FlagMulticast),
			},
			want: nil,
		},
		{
			name: "empty input",
			in:   nil,
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := announceIfaceIndexes(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("index %d: got %v, want %v", i, got, tc.want)
				}
			}
		})
	}
}
