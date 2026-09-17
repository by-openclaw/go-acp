package consumer

// BCP-007-03: an MXL connection carries NO transport file. mxlLeg is
// the branch Connect takes to skip the /transportfile fetch entirely
// (BCP-007-03-02 test_03 scores a PATCH that attaches one as
// non-conformant).

import (
	"testing"

	"dhs/internal/amwa/codec/is04"
)

func TestMXLLeg(t *testing.T) {
	snap := &CatalogueSnapshot{
		Senders: []is04.Sender{
			{ResourceCore: is04.ResourceCore{ID: "snd-mxl"}, Transport: is04.TransportMXL},
			{ResourceCore: is04.ResourceCore{ID: "snd-rtp"}, Transport: "urn:x-nmos:transport:rtp.mcast"},
		},
		Receivers: []is04.Receiver{
			{ResourceCore: is04.ResourceCore{ID: "rcv-mxl"}, Transport: is04.TransportMXL},
			{ResourceCore: is04.ResourceCore{ID: "rcv-rtp"}, Transport: "urn:x-nmos:transport:rtp"},
		},
	}
	cases := []struct {
		name         string
		sender, recv string
		want         bool
	}{
		{"both MXL", "snd-mxl", "rcv-mxl", true},
		{"receiver MXL only", "snd-rtp", "rcv-mxl", true},
		{"sender MXL only", "snd-mxl", "rcv-rtp", true},
		{"neither", "snd-rtp", "rcv-rtp", false},
		{"unknown ids", "nope", "nada", false},
	}
	for _, tc := range cases {
		if got := mxlLeg(snap, tc.sender, tc.recv); got != tc.want {
			t.Errorf("%s: mxlLeg = %v, want %v", tc.name, got, tc.want)
		}
	}
}
