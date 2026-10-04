package consumer

import (
	"context"
	"fmt"

	"dhs/internal/amwa/codec/is11"
	"dhs/internal/amwa/session/streamcompat"
)

// CompatRequest asks the Controller to read the stream compatibility of
// one Sender or one Receiver (IS-11), or to constrain a Sender.
type CompatRequest struct {
	// SenderID or ReceiverID names the resource; exactly one.
	SenderID   string
	ReceiverID string

	// Constraints, when set, are PUT as the Sender's active constraints
	// (an empty list is legal: it leaves the Sender unconstrained while
	// still stating so explicitly).
	Constraints *is11.ActiveConstraints

	// Release removes the Sender's active constraints.
	Release bool

	// DryRun resolves and reads everything, and changes nothing.
	DryRun bool
}

// CompatResult reports what the Device says, after any change.
type CompatResult struct {
	Endpoint string // the stream-compat href resolved from IS-04

	SenderID   string
	ReceiverID string
	Status     is11.Status

	// For a Sender: what it is held to, what it can be held to, and the
	// Inputs feeding it.
	Active    is11.ActiveConstraints
	Supported []string
	Inputs    []is11.Input

	// For a Receiver: the Outputs it drives.
	Outputs []is11.Output

	DryRun  bool
	Changed bool // constraints were PUT or released
}

// Compat is the IS-11 half of the Controller role.
//
// A Receiver whose status reads non_compliant_stream, or a Sender in
// active_constraints_violation, is a route that carries nothing an
// operator can use — and IS-05 says nothing about it. This reads the
// status with the Device's own reason, and lets a Sender be held to
// what the far end can take.
//
// The endpoint comes from the owning Device's IS-04 `controls`
// (urn:x-nmos:control:stream-compat). Constraints are checked against
// the parameter constraints the Sender says it supports before they are
// sent, and what the Device applies is read back.
func (c *Controller) Compat(ctx context.Context, req CompatRequest) (*CompatResult, error) {
	if err := checkCompatRequest(req); err != nil {
		return nil, err
	}
	snap, _ := c.Walk(ctx)
	deviceID, err := compatDevice(snap, req)
	if err != nil {
		return nil, err
	}
	href := ""
	for _, d := range snap.Devices {
		if d.ID == deviceID {
			href = pickControl(d.Controls, "urn:x-nmos:control:stream-compat/")
		}
	}
	if href == "" {
		return nil, fmt.Errorf("nmos compat: device %s advertises no "+
			"urn:x-nmos:control:stream-compat control, so it has no IS-11 endpoint to read", deviceID)
	}
	cl, err := streamcompat.NewClient(href)
	if err != nil {
		return nil, err
	}
	res := &CompatResult{Endpoint: cl.Base, SenderID: req.SenderID, ReceiverID: req.ReceiverID, DryRun: req.DryRun}

	if req.ReceiverID != "" {
		return res, c.readReceiverCompat(ctx, cl, res)
	}

	supported, err := cl.SupportedConstraints(ctx, req.SenderID)
	if err != nil {
		return nil, err
	}
	res.Supported = supported.ParameterConstraints
	if req.Constraints != nil {
		if unknown := unsupportedConstraints(*req.Constraints, res.Supported); len(unknown) > 0 {
			return nil, fmt.Errorf("nmos compat: sender %s does not support constraining %v "+
				"(it supports %v)", req.SenderID, unknown, res.Supported)
		}
	}
	if !req.DryRun {
		switch {
		case req.Release:
			if err := cl.DeleteActiveConstraints(ctx, req.SenderID); err != nil {
				return nil, err
			}
			res.Changed = true
		case req.Constraints != nil:
			if _, err := cl.PutActiveConstraints(ctx, req.SenderID, *req.Constraints); err != nil {
				return nil, err
			}
			res.Changed = true
		}
	}
	return res, c.readSenderCompat(ctx, cl, res)
}

func checkCompatRequest(req CompatRequest) error {
	switch {
	case (req.SenderID == "") == (req.ReceiverID == ""):
		return fmt.Errorf("nmos compat: name one sender or one receiver " +
			"(run `dhs consumer nmos walk -l` to list them)")
	case req.ReceiverID != "" && (req.Constraints != nil || req.Release):
		return fmt.Errorf("nmos compat: constraints are a Sender's; a Receiver states what it takes in its IS-04 caps")
	case req.Constraints != nil && req.Release:
		return fmt.Errorf("nmos compat: setting constraints and releasing them are two requests")
	}
	return nil
}

// compatDevice names the Device that owns the resource.
func compatDevice(snap *CatalogueSnapshot, req CompatRequest) (string, error) {
	for _, s := range snap.Senders {
		if req.SenderID != "" && s.ID == req.SenderID {
			return s.DeviceID, nil
		}
	}
	for _, r := range snap.Receivers {
		if req.ReceiverID != "" && r.ID == req.ReceiverID {
			return r.DeviceID, nil
		}
	}
	return "", fmt.Errorf("nmos compat: no sender %q / receiver %q in this catalogue "+
		"(walk it first to see what is there)", req.SenderID, req.ReceiverID)
}

// unsupportedConstraints lists the capability URNs in the constraint
// sets that the Sender does not say it supports. Meta keys
// (urn:x-nmos:cap:meta:*) describe the set, not the stream.
func unsupportedConstraints(ac is11.ActiveConstraints, supported []string) []string {
	known := map[string]bool{}
	for _, urn := range supported {
		known[urn] = true
	}
	var unknown []string
	seen := map[string]bool{}
	for _, set := range ac.ConstraintSets {
		for urn := range set {
			const meta = "urn:x-nmos:cap:meta:"
			if len(urn) >= len(meta) && urn[:len(meta)] == meta {
				continue
			}
			if !known[urn] && !seen[urn] {
				seen[urn] = true
				unknown = append(unknown, urn)
			}
		}
	}
	return unknown
}

func (c *Controller) readSenderCompat(ctx context.Context, cl *streamcompat.Client, res *CompatResult) error {
	var err error
	if res.Status, err = cl.Status(ctx, streamcompat.KindSenders, res.SenderID); err != nil {
		return err
	}
	if res.Active, err = cl.ActiveConstraints(ctx, res.SenderID); err != nil {
		return err
	}
	ids, err := cl.List(ctx, "senders/"+res.SenderID+"/inputs")
	if err != nil {
		return err
	}
	for _, id := range ids {
		in, err := cl.Input(ctx, id)
		if err != nil {
			return err
		}
		res.Inputs = append(res.Inputs, in)
	}
	return nil
}

func (c *Controller) readReceiverCompat(ctx context.Context, cl *streamcompat.Client, res *CompatResult) error {
	var err error
	if res.Status, err = cl.Status(ctx, streamcompat.KindReceivers, res.ReceiverID); err != nil {
		return err
	}
	ids, err := cl.List(ctx, "receivers/"+res.ReceiverID+"/outputs")
	if err != nil {
		return err
	}
	for _, id := range ids {
		out, err := cl.Output(ctx, id)
		if err != nil {
			return err
		}
		res.Outputs = append(res.Outputs, out)
	}
	return nil
}
