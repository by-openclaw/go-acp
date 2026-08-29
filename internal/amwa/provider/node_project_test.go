package provider

import (
	"testing"

	"dhs/internal/amwa/codec/is04"
)

// wsSender builds a WebSocket event sender.
func wsSender(id, flowID, deviceID string) is04.Sender {
	fid := flowID
	return is04.Sender{
		ResourceCore: is04.ResourceCore{
			ID: id, Version: "0:0", Label: "snd-ws",
			Description: "tally over websocket", Tags: map[string][]string{},
		},
		FlowID: &fid, Transport: is04.TransportWebSocket, DeviceID: deviceID,
		InterfaceBindings: []string{"eth0"},
	}
}

// wsBundle is the audio device plus a WebSocket event chain: a data
// Source, its Flow, and the Sender that carries it. WebSocket is not a
// valid IS-04 transport before v1.3.
func wsBundle() *NodeConfig {
	b := tallyBundle()
	dev := b.Devices[0].ID
	// The tally Source and Flow exist in tallyBundle but nothing sends
	// them; give them a WebSocket Sender so the projection has a full
	// chain to remove.
	fid := b.Flows[len(b.Flows)-1].ID
	b.Senders = append(b.Senders, wsSender("99999999-9999-4999-8999-999999999999", fid, dev))
	b.Devices[0].Senders = append(b.Devices[0].Senders, "99999999-9999-4999-8999-999999999999")
	return b
}

// TestProjectKeepsEverythingAtV13: v1.3 defines every transport this
// bundle uses, so nothing is dropped and the bundle is returned as-is.
func TestProjectKeepsEverythingAtV13(t *testing.T) {
	b := wsBundle()
	got := projectForMinor(b, "v1.3")
	if got != b {
		t.Error("v1.3 can describe every resource; the bundle should come back untouched")
	}
}

// TestProjectDropsWebSocketChainBelowV13: IS-04's Upgrade Path forbids
// an earlier version from listing a Sender using a transport it does
// not define -- and the Flow and Source behind it go too, or the Node
// publishes an event source nothing can subscribe to.
func TestProjectDropsWebSocketChainBelowV13(t *testing.T) {
	for _, ver := range []string{"v1.0", "v1.1", "v1.2"} {
		t.Run(ver, func(t *testing.T) {
			full := wsBundle()
			got := projectForMinor(full, ver)
			if got == full {
				t.Fatal("a WebSocket sender cannot be described at this minor; it must be dropped")
			}
			for _, s := range got.Senders {
				if s.Transport == "urn:x-nmos:transport:websocket" {
					t.Errorf("sender %s survived with a transport %s does not define", s.ID, ver)
				}
			}
			// The chain, not just the sender.
			for _, f := range got.Flows {
				if f.Format == formatData {
					t.Errorf("flow %s has no sender left to carry it", f.ID)
				}
			}
			for _, src := range got.Sources {
				if src.Format == formatData {
					t.Errorf("source %s has no flow left to encode it", src.ID)
				}
			}
			// And the Device must not point at what is gone.
			live := map[string]bool{}
			for _, s := range got.Senders {
				live[s.ID] = true
			}
			for _, d := range got.Devices {
				for _, id := range d.Senders {
					if !live[id] {
						t.Errorf("device %s still references dropped sender %s", d.ID, id)
					}
				}
			}
			// The audio RTP sender is untouched: rtp is valid at every
			// minor, so narrowing must not take the whole device with
			// it.
			if len(got.Senders) == 0 {
				t.Error("the RTP sender is valid at every minor and must survive")
			}
		})
	}
}

// TestProjectDoesNotMutateTheInput: the caller keeps the full bundle,
// and a projection that edited it in place would silently narrow every
// later reader too.
func TestProjectDoesNotMutateTheInput(t *testing.T) {
	full := wsBundle()
	before := len(full.Senders)
	beforeDev := len(full.Devices[0].Senders)
	_ = projectForMinor(full, "v1.0")
	if len(full.Senders) != before || len(full.Devices[0].Senders) != beforeDev {
		t.Error("projectForMinor modified the bundle it was given")
	}
}
