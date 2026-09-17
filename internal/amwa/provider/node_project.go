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
	keepSender := func(s *is04.Sender) bool { return is04.IsTransportAtIS04(s.Transport, apiVer) }
	keepReceiver := func(r *is04.Receiver) bool { return is04.IsTransportAtIS04(r.Transport, apiVer) }

	dropped := false
	for i := range bundle.Senders {
		if !keepSender(&bundle.Senders[i]) {
			dropped = true
			break
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

	// A Flow nothing sends is not carried on this version of the
	// device, and a Source no Flow encodes is not produced by it.
	//
	// The cascade matters: leaving the orphans behind would publish an
	// IS-07 event source whose only Sender has just been dropped, so a
	// controller could read the source's state over REST and have no
	// way to subscribe to it.
	usedFlows := map[string]bool{}
	for i := range out.Senders {
		if id := out.Senders[i].FlowID; id != nil && *id != "" {
			usedFlows[*id] = true
		}
	}
	out.Flows = nil
	usedSources := map[string]bool{}
	for i := range bundle.Flows {
		if !usedFlows[bundle.Flows[i].ID] {
			continue
		}
		out.Flows = append(out.Flows, bundle.Flows[i])
		usedSources[bundle.Flows[i].SourceID] = true
	}
	out.Sources = nil
	for i := range bundle.Sources {
		if usedSources[bundle.Sources[i].ID] {
			out.Sources = append(out.Sources, bundle.Sources[i])
		}
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
