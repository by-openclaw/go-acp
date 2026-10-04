package consumer

// The check a controller owes a route before it sends it.
//
// IS-05 does not make a Device verify that it can decode what it is
// pointed at — a Receiver accepts a sender_id that exists nowhere, a
// JPEG XS stream on a raw-only input, a 2160p flow past its 1080
// ceiling, and reports the stage as a success. BCP-004-01 exists so
// the CONTROLLER can tell: the Receiver declares what it takes, IS-04
// states what the Sender emits, and the comparison needs nothing else.
//
// Only what is DECLARED and KNOWN can refuse a route. A Receiver that
// declares no media types or no constraint sets has constrained
// nothing; a parameter the Flow does not state is unknown, not wrong.
// The check exists to stop a route that cannot work, never to demand
// metadata a device does not publish.

import (
	"fmt"
	"strings"

	"dhs/internal/amwa/codec/bcp/bcp00401"
	"dhs/internal/amwa/codec/is04"
)

// routeProblems returns why senderID cannot feed receiverID, one line
// per reason, or nil when nothing declared stands against the route.
// snap holds the Receiver; senderSnap the Sender, its Flow and Source
// (the same snapshot unless the Sender lives on another Node).
func routeProblems(snap, senderSnap *CatalogueSnapshot, senderID, receiverID string) []string {
	var receiver *is04.Receiver
	for i := range snap.Receivers {
		if snap.Receivers[i].ID == receiverID {
			receiver = &snap.Receivers[i]
			break
		}
	}
	if receiver == nil {
		return nil // resolveRoute reports an unknown Receiver in its own words
	}
	var sender *is04.Sender
	for i := range senderSnap.Senders {
		if senderSnap.Senders[i].ID == senderID {
			sender = &senderSnap.Senders[i]
			break
		}
	}
	if sender == nil {
		return []string{fmt.Sprintf("sender %s is not in the catalogue "+
			"(walk it to see what is there, or name its Node with --sender-node)", senderID)}
	}

	var problems []string
	if !transportsCompatible(receiver.Transport, sender.Transport) {
		problems = append(problems, fmt.Sprintf("the receiver takes %s, the sender emits %s",
			shortURN(receiver.Transport), shortURN(sender.Transport)))
	}

	// A Sender with no Flow is configured to emit nothing yet; there is
	// no stream to compare, and that is not a capability mismatch.
	if sender.FlowID == nil {
		return problems
	}
	var flow *is04.Flow
	for i := range senderSnap.Flows {
		if senderSnap.Flows[i].ID == *sender.FlowID {
			flow = &senderSnap.Flows[i]
			break
		}
	}
	if flow == nil {
		return problems
	}
	var source *is04.Source
	for i := range senderSnap.Sources {
		if senderSnap.Sources[i].ID == flow.SourceID {
			source = &senderSnap.Sources[i]
			break
		}
	}

	if receiver.Format != "" && flow.Format != "" && receiver.Format != flow.Format {
		problems = append(problems, fmt.Sprintf("the receiver is %s, the flow is %s",
			shortURN(receiver.Format), shortURN(flow.Format)))
	}
	if len(receiver.Caps.MediaTypes) > 0 && flow.MediaType != "" {
		member := false
		for _, mt := range receiver.Caps.MediaTypes {
			if strings.EqualFold(mt, flow.MediaType) {
				member = true
				break
			}
		}
		if !member {
			problems = append(problems, fmt.Sprintf("the flow is %s, the receiver takes %s",
				flow.MediaType, strings.Join(receiver.Caps.MediaTypes, ", ")))
		}
	}
	if ok, declared, why := bcp00401.AnySetSatisfied(bcp00401.FlowParams(flow, source), receiver.Caps.ConstraintSets); declared && !ok {
		problems = append(problems, "no constraint set of the receiver admits the stream — "+strings.Join(why, "; "))
	}
	return problems
}

// transportsCompatible compares two IS-04 transport URNs. The base
// transport must be the same; a dotted subclassification (rtp.mcast,
// rtp.ucast) narrows it, so two different subclassifications do not
// meet, while the bare base on either end takes any of its own.
func transportsCompatible(receiver, sender string) bool {
	rBase, rSub := splitTransport(receiver)
	sBase, sSub := splitTransport(sender)
	if rBase != sBase {
		return false
	}
	return rSub == "" || sSub == "" || rSub == sSub
}

// splitTransport separates "urn:x-nmos:transport:rtp.mcast" into its
// base URN and its subclassification ("mcast"; "" when there is none).
func splitTransport(urn string) (base, sub string) {
	const prefix = "urn:x-nmos:transport:"
	name, ok := strings.CutPrefix(urn, prefix)
	if !ok {
		return urn, ""
	}
	name, sub, _ = strings.Cut(name, ".")
	return prefix + name, sub
}

// shortURN drops the x-nmos namespace from a transport or format URN
// for a message an operator reads.
func shortURN(urn string) string {
	for _, p := range []string{"urn:x-nmos:transport:", "urn:x-nmos:format:"} {
		if s, ok := strings.CutPrefix(urn, p); ok {
			return s
		}
	}
	return urn
}
