// Layer-3 -- narrowing a resource bundle to what one IS-04 minor can
// describe.
//
// IS-04's Upgrade Path is blunt about this: an earlier API version
// "MUST NOT list any Senders or Receivers which make use of this new
// transport type". So a device with WebSocket event senders is a v1.3
// device. Asked to serve v1.2, it is not a v1.3 device with two
// endpoints hidden -- it is a SMALLER DEVICE, and every API has to
// agree about that.
//
// Agreeing is the whole point. IS-05 §4.1 requires the Connection API
// ids to match the Node API exactly, IS-07 sources are IS-04 Sources,
// and IS-08 inputs and outputs are derived from IS-04 Receivers and
// Sources. Narrowing only the Node API leaves the other three
// advertising resources IS-04 no longer lists, which is the dangling
// reference that makes a controller drop the branch -- and it is
// exactly what the tool reported: "Unable to find an IS-04 resource
// with ID ...".
//
// The projection therefore runs ONCE, before any API server is built,
// and everything downstream sees the same device.

package provider

import "dhs/internal/amwa/codec/is04"

// projectForMinor returns the subset of a bundle that IS-04 apiVer can
// describe. The input is not modified.
//
// Returns the bundle unchanged when nothing needs dropping, so the
// common case (a device whose resources all fit) costs one pass and no
// allocation.
func projectForMinor(bundle *NodeConfig, apiVer string) *NodeConfig {
	if bundle == nil {
		return nil
	}
	// What this minor cannot describe at all: a transport it does not
	// define (WebSocket and MQTT before v1.3), and a format it does not
	// define (mux before v1.1). A Source in such a format goes, its
	// Flows with it, and the Senders of those Flows. A v1.0 Node that
	// kept its mux Source could not encode it: its registration stopped
	// half-way and started again, for ever, and the Registry showed the
	// Node arriving and leaving every second (IS-04-02 at v1.0, test_31).
	goneSource := map[string]bool{}
	for i := range bundle.Sources {
		if !is04.IsFormatAtIS04(bundle.Sources[i].Format, apiVer) {
			goneSource[bundle.Sources[i].ID] = true
		}
	}
	goneFlow := map[string]bool{}
	for i := range bundle.Flows {
		f := &bundle.Flows[i]
		if goneSource[f.SourceID] || !is04.IsFormatAtIS04(f.Format, apiVer) {
			goneFlow[f.ID] = true
		}
	}
	keepSender := func(s *is04.Sender) bool {
		if s.FlowID != nil && goneFlow[*s.FlowID] {
			return false
		}
		return is04.IsTransportAtIS04(s.Transport, apiVer)
	}
	keepReceiver := func(r *is04.Receiver) bool {
		return is04.IsTransportAtIS04(r.Transport, apiVer) && is04.IsFormatAtIS04(r.Format, apiVer)
	}

	dropped := len(goneSource) > 0 || len(goneFlow) > 0
	if !dropped {
		for i := range bundle.Senders {
			if !keepSender(&bundle.Senders[i]) {
				dropped = true
				break
			}
		}
	}
	if !dropped {
		for i := range bundle.Receivers {
			if !keepReceiver(&bundle.Receivers[i]) {
				dropped = true
				break
			}
		}
	}
	if !dropped {
		return bundle
	}

	out := *bundle
	out.Senders = nil
	out.Receivers = nil
	keptSenders := map[string]bool{}
	keptReceivers := map[string]bool{}
	for i := range bundle.Senders {
		if keepSender(&bundle.Senders[i]) {
			out.Senders = append(out.Senders, bundle.Senders[i])
			keptSenders[bundle.Senders[i].ID] = true
		}
	}
	for i := range bundle.Receivers {
		if keepReceiver(&bundle.Receivers[i]) {
			out.Receivers = append(out.Receivers, bundle.Receivers[i])
			keptReceivers[bundle.Receivers[i].ID] = true
		}
	}

	// A Flow whose Senders have all just been dropped is not carried on
	// this version of the device, and a Source whose Flows have all gone
	// that way is not produced by it.
	//
	// The cascade matters: leaving the orphans behind would publish an
	// IS-07 event source whose only Sender has just been dropped, so a
	// controller could read the source's state over REST and have no
	// way to subscribe to it.
	//
	// It follows what the projection dropped, and nothing else. A Flow
	// no Sender ever sent, a Source no Flow ever encoded — an audio
	// input generated inside the device, which IS-08 routes — were never
	// reached through a Sender and do not leave with one. They used to:
	// at v1.2 the device lost such a Source, and the channel map that
	// routes it was rejected at start ("unknown input").
	sent := map[string]bool{}      // flows some Sender of the whole device sends
	stillSent := map[string]bool{} // flows a kept Sender sends
	for i := range bundle.Senders {
		if id := bundle.Senders[i].FlowID; id != nil && *id != "" {
			sent[*id] = true
			if keptSenders[bundle.Senders[i].ID] {
				stillSent[*id] = true
			}
		}
	}
	out.Flows = nil
	encoded := map[string]bool{}      // sources some Flow of the whole device encodes
	stillEncoded := map[string]bool{} // sources a kept Flow encodes
	for i := range bundle.Flows {
		f := &bundle.Flows[i]
		encoded[f.SourceID] = true
		if goneFlow[f.ID] || (sent[f.ID] && !stillSent[f.ID]) {
			continue
		}
		out.Flows = append(out.Flows, *f)
		stillEncoded[f.SourceID] = true
	}
	out.Sources = nil
	for i := range bundle.Sources {
		id := bundle.Sources[i].ID
		if goneSource[id] || (encoded[id] && !stillEncoded[id]) {
			continue
		}
		out.Sources = append(out.Sources, bundle.Sources[i])
	}

	// A Device that still lists a dropped id "references one or more
	// unknown Senders" -- the tool's words, and a fair description of
	// a device pointing at resources that are not there.
	out.Devices = make([]is04.Device, len(bundle.Devices))
	copy(out.Devices, bundle.Devices)
	for i := range out.Devices {
		out.Devices[i].Senders = filterIDs(out.Devices[i].Senders, keptSenders)
		out.Devices[i].Receivers = filterIDs(out.Devices[i].Receivers, keptReceivers)
	}
	return &out
}

func filterIDs(ids []string, keep map[string]bool) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if keep[id] {
			out = append(out, id)
		}
	}
	return out
}
