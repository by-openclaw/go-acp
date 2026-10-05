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

// The narrowing follows what it dropped. A Source no Flow ever encoded
// — an audio input generated inside the device, which IS-08 routes —
// was never reached through a Sender and does not leave with the
// WebSocket chain; nor does a Flow no Sender ever sent. Before, a v1.2
// device lost such a Source, and the channel map routing it was
// rejected at start: "unknown input".
func TestProjectKeepsWhatNoSenderEverCarried(t *testing.T) {
	full := wsBundle()
	dev := full.Devices[0].ID
	const input, idle, idleFlow = "aaaaaaaa-1111-4111-8111-111111111111", "bbbbbbbb-2222-4222-8222-222222222222", "cccccccc-3333-4333-8333-333333333333"
	src := func(id, label string) is04.Source {
		return is04.Source{
			ResourceCore: is04.ResourceCore{ID: id, Version: "0:0", Label: label, Tags: map[string][]string{}},
			DeviceID:     dev, Format: formatAudio,
		}
	}
	full.Sources = append(full.Sources, src(input, "generated input"), src(idle, "encoded, never sent"))
	full.Flows = append(full.Flows, is04.Flow{
		ResourceCore: is04.ResourceCore{ID: idleFlow, Version: "0:0", Label: "idle flow", Tags: map[string][]string{}},
		SourceID:     idle, DeviceID: dev, Format: formatAudio,
	})

	for _, ver := range []string{"v1.0", "v1.1", "v1.2"} {
		got := projectForMinor(full, ver)
		if got == full {
			t.Fatalf("%s: the WebSocket sender must be dropped", ver)
		}
		have := map[string]bool{}
		for _, s := range got.Sources {
			have[s.ID] = true
			if s.Format == formatData {
				t.Errorf("%s: the event source %s outlived its only sender", ver, s.ID)
			}
		}
		if !have[input] {
			t.Errorf("%s: the Source no Flow ever encoded was dropped with the WebSocket chain", ver)
		}
		if !have[idle] {
			t.Errorf("%s: the Source whose Flow no Sender ever sent was dropped", ver)
		}
		kept := false
		for _, f := range got.Flows {
			kept = kept || f.ID == idleFlow
		}
		if !kept {
			t.Errorf("%s: the Flow no Sender ever sent was dropped", ver)
		}
	}
}

// A format the minor does not define leaves with everything built on
// it. mux arrived with IS-04 v1.1: at v1.0 a mux Source, its Flow, the
// Sender of that Flow and a mux Receiver are not part of the device —
// a v1.0 Node that kept them could not encode its own Source, and its
// registration never completed. At v1.1 they are all there.
func TestProjectDropsTheMuxChainAtV10(t *testing.T) {
	full := tallyBundle()
	dev := full.Devices[0].ID
	const src, flow, snd, rcv = "aaaaaaaa-0000-4000-8000-00000000000a", "bbbbbbbb-0000-4000-8000-00000000000b",
		"cccccccc-0000-4000-8000-00000000000c", "dddddddd-0000-4000-8000-00000000000d"
	core := func(id, label string) is04.ResourceCore {
		return is04.ResourceCore{ID: id, Version: "0:0", Label: label, Tags: map[string][]string{}}
	}
	fid := flow
	full.Sources = append(full.Sources, is04.Source{ResourceCore: core(src, "mux source"), DeviceID: dev, Format: is04.FormatMux})
	full.Flows = append(full.Flows, is04.Flow{ResourceCore: core(flow, "mux flow"), SourceID: src, DeviceID: dev, Format: is04.FormatMux})
	full.Senders = append(full.Senders, is04.Sender{ResourceCore: core(snd, "mux sender"), FlowID: &fid,
		Transport: "urn:x-nmos:transport:rtp", DeviceID: dev, InterfaceBindings: []string{"eth0"}})
	full.Receivers = append(full.Receivers, is04.Receiver{ResourceCore: core(rcv, "mux receiver"),
		Transport: "urn:x-nmos:transport:rtp", DeviceID: dev, Format: is04.FormatMux, InterfaceBindings: []string{"eth0"}})
	full.Devices[0].Senders = append(full.Devices[0].Senders, snd)
	full.Devices[0].Receivers = append(full.Devices[0].Receivers, rcv)

	has := func(b *NodeConfig) map[string]bool {
		m := map[string]bool{}
		for _, x := range b.Sources {
			m[x.ID] = true
		}
		for _, x := range b.Flows {
			m[x.ID] = true
		}
		for _, x := range b.Senders {
			m[x.ID] = true
		}
		for _, x := range b.Receivers {
			m[x.ID] = true
		}
		for _, d := range b.Devices {
			for _, id := range append(append([]string{}, d.Senders...), d.Receivers...) {
				m["device lists "+id] = true
			}
		}
		return m
	}

	at10 := has(projectForMinor(full, "v1.0"))
	for _, id := range []string{src, flow, snd, rcv, "device lists " + snd, "device lists " + rcv} {
		if at10[id] {
			t.Errorf("v1.0 still carries %s — mux is not defined before v1.1", id)
		}
	}
	// What v1.0 does define stays.
	if n := len(projectForMinor(full, "v1.0").Sources); n != len(full.Sources)-1 {
		t.Errorf("v1.0 keeps %d of %d sources, want all but the mux one", n, len(full.Sources))
	}
	at11 := has(projectForMinor(full, "v1.1"))
	for _, id := range []string{src, flow, snd, rcv} {
		if !at11[id] {
			t.Errorf("v1.1 dropped %s — mux is defined from v1.1", id)
		}
	}
}
