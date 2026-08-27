package consumer

import (
	"context"
	"fmt"
	"strings"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
	"dhs/internal/amwa/codec/spec"
	"dhs/internal/amwa/session/connection"
)

// ConnectRequest asks the Controller to route one Sender to one
// Receiver.
type ConnectRequest struct {
	// SenderID is the IS-04 Sender to route. Empty means DISCONNECT:
	// IS-05 models "stop receiving" as staging sender_id=null with
	// master_enable=false, not as a separate verb.
	SenderID string

	// ReceiverID is the IS-04 Receiver to drive. Required.
	ReceiverID string

	// Mode defaults to activate_immediate. Scheduled modes need When.
	Mode is05.ActivationMode

	// When is the TAI timestamp ("<secs>:<nanos>") for a scheduled
	// activation — an offset for _relative, an absolute instant for
	// _absolute. Ignored for activate_immediate.
	When string

	// DryRun resolves and reports everything without sending the PATCH.
	//
	// Routing moves real signal on real hardware, and IS-05 gives no
	// undo. Being able to see the endpoint, the body and the receiver's
	// current state first is the difference between a safe change and a
	// hopeful one.
	DryRun bool
}

// ConnectResult reports what actually happened, as the Device tells it
// — not what we asked for.
type ConnectResult struct {
	ReceiverID   string
	SenderID     *string
	MasterEnable bool
	Mode         is05.ActivationMode
	ActivationAt string
	Endpoint     string // the IS-05 base the route went through
	SDPBytes     int    // 0 when the sender served no transport file

	// Set only for a dry run: what WOULD have been sent, and what the
	// receiver is doing right now and would have lost.
	DryRun              bool
	Patch               map[string]any
	CurrentSenderID     *string
	CurrentMasterEnable bool
}

// Connect routes a Sender to a Receiver over IS-05.
//
// The endpoint is DISCOVERED, never guessed: IS-04 advertises each
// Device's Connection API in its `controls` array under
// urn:x-nmos:control:sr-ctrl/vX.Y. Real devices serve IS-05 on a
// different port from their Node API, so constructing the URL from the
// Node's host would work in the lab and fail in a plant.
//
// The sequence is the one IS-05 §"Connection Management" describes:
// fetch the Sender's transport file, stage it on the Receiver together
// with sender_id and master_enable, and activate. Staging without
// master_enable=true is the classic silent failure — everything reports
// success and no signal moves.
func (c *Controller) Connect(ctx context.Context, req ConnectRequest) (*ConnectResult, error) {
	if req.ReceiverID == "" {
		return nil, fmt.Errorf("nmos connect: receiver id is required")
	}
	mode := req.Mode
	if mode == "" {
		mode = is05.ActivationModeImmediate
	}
	if !is05.IsValidActivationMode(mode) {
		return nil, fmt.Errorf("nmos connect: %q is not an IS-05 activation mode", mode)
	}
	if mode != is05.ActivationModeImmediate && req.When == "" {
		return nil, fmt.Errorf("nmos connect: %s needs --when <secs>:<nanos>", mode)
	}

	snap, _ := c.Walk(ctx)

	href, err := c.connectionHref(snap, req.ReceiverID)
	if err != nil {
		return nil, err
	}
	cl, err := connection.NewClient(href)
	if err != nil {
		return nil, err
	}

	patch := map[string]any{
		"master_enable": req.SenderID != "",
		"activation":    activationBody(mode, req.When),
	}
	res := &ConnectResult{
		ReceiverID: req.ReceiverID,
		Mode:       mode,
		Endpoint:   cl.Base,
	}

	if req.SenderID == "" {
		// Disconnect. sender_id must be explicitly null — omitting it
		// would leave the existing one in place, because PATCH merges.
		patch["sender_id"] = nil
	} else {
		patch["sender_id"] = req.SenderID
		sdp, err := cl.TransportFile(ctx, req.SenderID)
		switch {
		case err != nil:
			// Not fatal. A Sender on a non-RTP transport has no SDP,
			// and some devices simply do not serve one. The Receiver
			// can still be pointed at the Sender by id; we record the
			// gap rather than refusing to route.
			c.fire(spec.SeverityWarn, "nmos_is05_no_transport_file",
				fmt.Sprintf("sender %s served no transport file: %v", req.SenderID, err),
				req.SenderID)
		case strings.TrimSpace(sdp) == "":
			c.fire(spec.SeverityWarn, "nmos_is05_empty_transport_file",
				fmt.Sprintf("sender %s served an empty transport file", req.SenderID),
				req.SenderID)
		default:
			patch["transport_file"] = map[string]any{
				"data": sdp,
				"type": "application/sdp",
			}
			res.SDPBytes = len(sdp)
		}
	}

	if req.DryRun {
		res.DryRun = true
		res.Patch = patch
		// Read the receiver's ACTIVE state, not its staged state: what
		// the operator is about to overwrite is what the device is
		// currently doing.
		if active, err := cl.ActiveReceiver(ctx, req.ReceiverID); err == nil {
			res.CurrentSenderID = active.SenderID
			res.CurrentMasterEnable = active.MasterEnable
		} else {
			c.fire(spec.SeverityWarn, "nmos_is05_active_unreadable",
				fmt.Sprintf("receiver %s active state unreadable: %v", req.ReceiverID, err),
				req.ReceiverID)
		}
		return res, nil
	}

	staged, err := cl.PatchReceiver(ctx, req.ReceiverID, patch)
	if err != nil {
		return nil, err
	}
	res.SenderID = staged.SenderID
	res.MasterEnable = staged.MasterEnable
	if staged.Activation.RequestedTime != nil {
		res.ActivationAt = *staged.Activation.RequestedTime
	}

	// A device that accepted the stage but silently dropped
	// master_enable routes nothing. Say so rather than reporting
	// success.
	if req.SenderID != "" && !staged.MasterEnable {
		c.fire(spec.SeverityError, "nmos_is05_master_enable_ignored",
			fmt.Sprintf("receiver %s accepted the stage but reports master_enable=false; "+
				"no signal will flow", req.ReceiverID), req.ReceiverID)
	}
	return res, nil
}

// connectionHref finds the IS-05 endpoint for whichever Device owns the
// named Receiver.
func (c *Controller) connectionHref(snap *CatalogueSnapshot, receiverID string) (string, error) {
	deviceID := ""
	for _, r := range snap.Receivers {
		if r.ID == receiverID {
			deviceID = r.DeviceID
			break
		}
	}
	if deviceID == "" {
		return "", fmt.Errorf("nmos connect: no receiver %s in this catalogue "+
			"(walk it first to see what is there)", receiverID)
	}
	for _, d := range snap.Devices {
		if d.ID != deviceID {
			continue
		}
		if href := pickConnectionControl(d.Controls); href != "" {
			return href, nil
		}
		return "", fmt.Errorf("nmos connect: device %s advertises no "+
			"urn:x-nmos:control:sr-ctrl control, so it has no IS-05 endpoint "+
			"to route through", deviceID)
	}
	return "", fmt.Errorf("nmos connect: receiver %s names device %s, which is "+
		"not in this catalogue", receiverID, deviceID)
}

// pickConnectionControl selects the highest sr-ctrl version advertised.
// Controls are unordered, and a Device commonly lists several minors.
func pickConnectionControl(controls []is04.DeviceControl) string {
	const prefix = "urn:x-nmos:control:sr-ctrl/"
	best, bestVer := "", ""
	for _, ctl := range controls {
		if !strings.HasPrefix(ctl.Type, prefix) {
			continue
		}
		ver := strings.TrimPrefix(ctl.Type, prefix)
		if best == "" || ver > bestVer {
			best, bestVer = ctl.Href, ver
		}
	}
	return best
}

// activationBody builds the IS-05 activation object. Only the scheduled
// modes carry a requested_time; sending one with activate_immediate is
// a schema violation.
func activationBody(mode is05.ActivationMode, when string) map[string]any {
	body := map[string]any{"mode": string(mode)}
	if mode != is05.ActivationModeImmediate && when != "" {
		body["requested_time"] = when
	}
	return body
}
