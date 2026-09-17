package consumer

// Label → UUID resolution for operator-facing verbs.
//
// IS-04 makes UUIDs the only stable identifier; labels are mutable
// and non-unique BY SPEC. An operator still thinks in labels, so the
// resolver exists — but ambiguity and absence are both hard errors
// that list what WAS found, never a guess: routing "the wrong Left"
// on a live plant is exactly the failure the UUID rule prevents.

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/session/connection"
)

// ResolveSenderByLabel maps a label to the ONE Sender carrying it.
func (c *Controller) ResolveSenderByLabel(ctx context.Context, label string) (string, error) {
	snap, _ := c.Walk(ctx)
	if snap == nil {
		return "", fmt.Errorf("nmos: catalogue walk returned nothing")
	}
	return matchSenderLabel(snap.Senders, label)
}

// matchSenderLabel is the pure matcher (testable without a device).
func matchSenderLabel(senders []is04.Sender, label string) (string, error) {
	var matches []is04.Sender
	for i := range senders {
		if senders[i].Label == label {
			matches = append(matches, senders[i])
		}
	}
	switch len(matches) {
	case 1:
		return matches[0].ID, nil
	case 0:
		known := make([]string, 0, len(senders))
		for i := range senders {
			if senders[i].Label != "" {
				known = append(known, senders[i].Label)
			}
		}
		sort.Strings(known)
		if len(known) > 12 {
			known = append(known[:12], "…")
		}
		return "", fmt.Errorf("nmos: no sender labelled %q; labels present: %s",
			label, strings.Join(known, ", "))
	default:
		ids := make([]string, len(matches))
		for i := range matches {
			ids[i] = matches[i].ID
		}
		return "", fmt.Errorf("nmos: label %q names %d senders (%s) — labels are "+
			"non-unique by spec, use --sender <uuid>", label, len(matches), strings.Join(ids, ", "))
	}
}

// SenderActiveLegs reads a Sender's ACTIVE addressing — the verify
// half of a retune: the device's own answer after activation.
func (c *Controller) SenderActiveLegs(ctx context.Context, senderID string) ([]LegState, bool, error) {
	snap, _ := c.Walk(ctx)
	href, err := c.senderConnectionHref(snap, senderID)
	if err != nil {
		return nil, false, err
	}
	cl, err := connection.NewClient(href)
	if err != nil {
		return nil, false, err
	}
	active, err := cl.ActiveSender(ctx, senderID)
	if err != nil {
		return nil, false, err
	}
	return flattenLegs(active.TransportParams), active.MasterEnable, nil
}
