package audit

import (
	"strings"
	"testing"
)

// senderOn builds a connection bucket entry for one enabled sender leg.
func senderLeg(dst string, port any, over map[string]any) map[string]any {
	leg := map[string]any{"destination_ip": dst, "destination_port": port}
	for k, v := range over {
		leg[k] = v
	}
	return leg
}

// TestPlantMulticastCountsOnlyEmitters: a collision is two senders on
// the wire. A sender switched off, a leg with no destination, and a
// leg whose port is still "auto" put nothing on the wire, so none of
// them can collide — only the two that are actually emitting do.
func TestPlantMulticastCountsOnlyEmitters(t *testing.T) {
	const group = "233.252.0.20"
	bucket := map[string]any{
		"senders/off/active": map[string]any{
			"master_enable": false, "transport_params": []any{senderLeg(group, 20000, nil)},
		},
		"senders/nodest/active": map[string]any{
			"master_enable": true, "transport_params": []any{map[string]any{"destination_port": 20000}},
		},
		"senders/auto/active": map[string]any{
			"master_enable": true, "transport_params": []any{senderLeg(group, "auto", nil)},
		},
		"senders/one/active": map[string]any{
			"master_enable": true, "transport_params": []any{senderLeg(group, 20000, nil)},
		},
		"senders/two/active": map[string]any{
			"master_enable": true, "transport_params": []any{senderLeg(group, 20000, nil)},
		},
	}
	h := mk("node", map[string]map[string]map[string]any{"connection": {"v1.1": bucket}})

	fs := checkPlantMulticast([]*Harvest{h})
	f := has(t, fs, "NMOS-PLANT-MCAST-COLLISION")
	if len(fs) != 1 {
		t.Fatalf("want exactly one collision, got %v", codeList(fs))
	}
	if !strings.Contains(f.Detail, "2 senders emit") {
		t.Errorf("only the two emitting senders collide: %q", f.Detail)
	}
	for _, id := range []string{"off", "nodest", "auto"} {
		if strings.Contains(f.Detail, "sender/"+id+" ") {
			t.Errorf("a non-emitting sender %q was counted as an emitter: %q", id, f.Detail)
		}
	}
}

// TestPlantMulticastSameSenderTwiceIsNotACollision: a registry export
// captures every sender twice — once in the registry's catalogue and
// once by following the node. One emitter seen from two places is not
// two emitters.
func TestPlantMulticastSameSenderTwiceIsNotACollision(t *testing.T) {
	body := map[string]any{
		"master_enable": true, "transport_params": []any{senderLeg("233.252.0.20", 20000, nil)},
	}
	fromRegistry := mk("registry", active(sID, body))
	fromNode := mk("node", active(sID, body))
	if got := checkPlantMulticast([]*Harvest{fromRegistry, fromNode}); len(got) != 0 {
		t.Errorf("the same sender captured twice was reported as a collision: %v", codeList(got))
	}
}
