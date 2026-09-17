package consumer

import (
	"context"
	"fmt"
	"net"

	"dhs/internal/amwa/codec/is05"
	"dhs/internal/amwa/codec/spec"
	"dhs/internal/amwa/session/connection"
)

// SetSenderRequest configures a Sender's transport over IS-05.
//
// This is the half of IS-05 that makes a device actually emit. A real
// EVS Neuron ships with every Sender master_enable=true, a real
// source_ip on each ST 2022-7 leg, and destination_ip 0.0.0.0 — enabled
// and pointed nowhere. Until a destination is assigned, connecting a
// Receiver to it moves no media, and the Sender's SDP says as much
// (`c=IN IP4 0.0.0.0/32`).
type SetSenderRequest struct {
	// SenderID is the IS-04 Sender UUID. Required.
	SenderID string

	// DestinationIPs is one address per transport leg, in the order the
	// device lists them.
	//
	// ST 2022-7 senders have two legs on two separate networks, and the
	// two MUST NOT share a group — that is the whole point of seamless
	// protection. So this takes a list rather than one value applied to
	// both, and refuses a count that does not match the device.
	DestinationIPs []string

	// DestinationPorts is optional, one per leg. Empty leaves whatever
	// the device already has.
	DestinationPorts []int

	// MasterEnable, when non-nil, is set alongside the transport
	// parameters.
	MasterEnable *bool

	// Mode defaults to activate_immediate. Scheduled modes need When.
	Mode is05.ActivationMode
	When string

	// DryRun resolves and prints without sending. Assigning a multicast
	// group makes a device start emitting onto a live network; being
	// able to read the exact body first is the difference between a
	// planned change and a hopeful one.
	DryRun bool
}

// SetSenderResult reports what the device did, as the device tells it.
type SetSenderResult struct {
	SenderID     string
	Endpoint     string
	MasterEnable bool
	Legs         []LegState

	DryRun  bool
	Patch   map[string]any
	Current []LegState
}

// LegState is one transport leg's addressing, flattened for display.
type LegState struct {
	SourceIP        string
	DestinationIP   string
	DestinationPort int
	RTPEnabled      bool
}

// SetSender assigns transport parameters to a Sender and activates.
//
// IS-05 PATCH is a MERGE, so only the keys named here are sent; every
// other transport parameter the device holds is left alone. Sending a
// full transport_params array built from our own struct would push zero
// values into fields the caller never mentioned.
func (c *Controller) SetSender(ctx context.Context, req SetSenderRequest) (*SetSenderResult, error) {
	if req.SenderID == "" {
		return nil, fmt.Errorf("nmos set: sender id is required")
	}
	if len(req.DestinationIPs) == 0 && len(req.DestinationPorts) == 0 && req.MasterEnable == nil {
		return nil, fmt.Errorf("nmos set: nothing to change — pass --destination, " +
			"--port or --enable/--disable")
	}
	mode := req.Mode
	if mode == "" {
		mode = is05.ActivationModeImmediate
	}
	if !is05.IsValidActivationMode(mode) {
		return nil, fmt.Errorf("nmos set: %q is not an IS-05 activation mode", mode)
	}
	if mode != is05.ActivationModeImmediate && req.When == "" {
		return nil, fmt.Errorf("nmos set: %s needs --when <secs>:<nanos>", mode)
	}
	for _, ip := range req.DestinationIPs {
		if ip == "" {
			continue
		}
		if net.ParseIP(ip) == nil {
			return nil, fmt.Errorf("nmos set: %q is not an IP address", ip)
		}
	}

	snap, _ := c.Walk(ctx)
	href, err := c.senderConnectionHref(snap, req.SenderID)
	if err != nil {
		return nil, err
	}
	cl, err := connection.NewClient(href)
	if err != nil {
		return nil, err
	}

	// Read the device's ACTIVE state first, for two reasons: it tells us
	// how many legs there are, and it is what the operator is about to
	// overwrite.
	active, err := cl.ActiveSender(ctx, req.SenderID)
	if err != nil {
		return nil, fmt.Errorf("nmos set: read sender %s: %w", req.SenderID, err)
	}
	patch, err := buildSenderPatch(len(active.TransportParams), mode, req)
	if err != nil {
		return nil, err
	}

	res := &SetSenderResult{
		SenderID: req.SenderID,
		Endpoint: cl.Base,
		Current:  flattenLegs(active.TransportParams),
	}

	if req.DryRun {
		res.DryRun = true
		res.Patch = patch
		res.MasterEnable = active.MasterEnable
		res.Legs = res.Current
		return res, nil
	}

	staged, err := cl.PatchSender(ctx, req.SenderID, patch)
	if err != nil {
		return nil, err
	}
	res.MasterEnable = staged.MasterEnable
	res.Legs = flattenLegs(staged.TransportParams)

	// A device that accepted the stage but kept 0.0.0.0 is still
	// emitting nowhere. Say so rather than reporting success.
	for i, leg := range res.Legs {
		if i < len(req.DestinationIPs) && req.DestinationIPs[i] != "" &&
			(leg.DestinationIP == "" || leg.DestinationIP == "0.0.0.0") {
			c.fire(spec.SeverityError, "nmos_is05_destination_ignored",
				fmt.Sprintf("sender %s leg %d: asked for destination_ip %s, device "+
					"reports %q; nothing will be emitted on this leg",
					req.SenderID, i, req.DestinationIPs[i], leg.DestinationIP),
				req.SenderID)
		}
	}
	return res, nil
}

// buildSenderPatch turns a request into the IS-05 merge body for a
// sender with `legs` transport legs.
//
// The transport_params array is always full length even when only one
// leg changes, because IS-05 matches legs POSITIONALLY: a shorter array
// would be read as describing the first N legs, silently re-addressing
// the wrong one. Legs with nothing to change carry an empty object,
// which merges to a no-op.
//
// Pure, so the leg arithmetic is testable without a device — the live
// path is proven separately against real hardware.
func buildSenderPatch(legs int, mode is05.ActivationMode, req SetSenderRequest) (map[string]any, error) {
	if legs == 0 {
		return nil, fmt.Errorf("nmos set: sender %s reports no transport legs", req.SenderID)
	}
	// A trailing EMPTY slot means "leave that leg alone" — and a leg
	// the device does not have needs no leaving-alone. Trimming lets
	// `--leg red` (always rendered as a two-slot list) drive a
	// single-leg sender; a NON-empty value beyond the device's legs
	// still errors below, because that is a real addressing mistake.
	for len(req.DestinationIPs) > legs && req.DestinationIPs[len(req.DestinationIPs)-1] == "" {
		req.DestinationIPs = req.DestinationIPs[:len(req.DestinationIPs)-1]
	}
	for len(req.DestinationPorts) > legs && req.DestinationPorts[len(req.DestinationPorts)-1] == 0 {
		req.DestinationPorts = req.DestinationPorts[:len(req.DestinationPorts)-1]
	}
	if n := len(req.DestinationIPs); n > 0 && n != legs {
		return nil, fmt.Errorf("nmos set: sender %s has %d transport leg(s), you gave "+
			"%d --destination value(s); give one per leg (ST 2022-7 legs must not "+
			"share a group)", req.SenderID, legs, n)
	}
	if n := len(req.DestinationPorts); n > 0 && n != legs {
		return nil, fmt.Errorf("nmos set: sender %s has %d transport leg(s), you gave "+
			"%d --port value(s); give one per leg", req.SenderID, legs, n)
	}

	params := make([]map[string]any, legs)
	for i := range params {
		leg := map[string]any{}
		if i < len(req.DestinationIPs) && req.DestinationIPs[i] != "" {
			leg["destination_ip"] = req.DestinationIPs[i]
		}
		if i < len(req.DestinationPorts) && req.DestinationPorts[i] != 0 {
			leg["destination_port"] = req.DestinationPorts[i]
		}
		params[i] = leg
	}

	patch := map[string]any{
		"transport_params": params,
		"activation":       activationBody(mode, req.When),
	}
	if req.MasterEnable != nil {
		patch["master_enable"] = *req.MasterEnable
	}
	return patch, nil
}

// flattenLegs pulls the four fields an operator actually reads out of
// the untyped transport_params maps.
func flattenLegs(params []is05.TransportParams) []LegState {
	out := make([]LegState, 0, len(params))
	for _, p := range params {
		out = append(out, LegState{
			SourceIP:        stringParam(p, "source_ip"),
			DestinationIP:   stringParam(p, "destination_ip"),
			DestinationPort: intParam(p, "destination_port"),
			RTPEnabled:      boolParam(p, "rtp_enabled"),
		})
	}
	return out
}

func stringParam(p is05.TransportParams, key string) string {
	if v, ok := p[key].(string); ok {
		return v
	}
	return ""
}

func intParam(p is05.TransportParams, key string) int {
	switch v := p[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func boolParam(p is05.TransportParams, key string) bool {
	v, _ := p[key].(bool)
	return v
}
