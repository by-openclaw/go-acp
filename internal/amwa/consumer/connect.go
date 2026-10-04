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

	// SenderNode is the Sender's own Node (http://host:port) for the
	// peer-to-peer case where the Sender lives on another device than
	// the Receiver and no Registry knows either: the Controller walks
	// that Node too and fetches the transport file from ITS IS-05,
	// which is the only place the SDP exists. Ignored when the Sender
	// is already in the catalogue.
	SenderNode string

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
// success and no signal moves. MXL legs are the one exception: no
// transport file is fetched or staged (see mxlLeg).
func (c *Controller) Connect(ctx context.Context, req ConnectRequest) (*ConnectResult, error) {
	mode, err := checkConnectRequest(req)
	if err != nil {
		return nil, err
	}

	snap, _ := c.Walk(ctx)

	rt, err := c.resolveRoute(ctx, snap, req, mode, map[string]*senderSource{})
	if err != nil {
		return nil, err
	}
	res := rt.result()

	if req.DryRun {
		c.describeDryRun(ctx, rt, res)
		return res, nil
	}

	staged, err := rt.client.PatchReceiver(ctx, req.ReceiverID, rt.patch)
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

// checkConnectRequest is the pre-flight every route passes before
// anything is walked: a receiver to drive, a real activation mode, and
// a time when the mode is a scheduled one. It returns the mode with
// the default applied.
func checkConnectRequest(req ConnectRequest) (is05.ActivationMode, error) {
	if req.ReceiverID == "" {
		return "", fmt.Errorf("nmos connect: receiver id is required")
	}
	mode := req.Mode
	if mode == "" {
		mode = is05.ActivationModeImmediate
	}
	if !is05.IsValidActivationMode(mode) {
		return "", fmt.Errorf("nmos connect: %q is not an IS-05 activation mode", mode)
	}
	if mode != is05.ActivationModeImmediate && req.When == "" {
		return "", fmt.Errorf("nmos connect: %s needs --when <secs>:<nanos>", mode)
	}
	return mode, nil
}

// route is one resolved request: the IS-05 client for the Receiver's
// Device and the body to stage on it. Connect sends it as a PATCH,
// ConnectBulk as one entry of a bulk POST — the body is the same.
type route struct {
	req      ConnectRequest
	mode     is05.ActivationMode
	client   *connection.Client
	patch    map[string]any
	sdpBytes int
}

// result starts the report every route gets, sent or not.
func (rt *route) result() *ConnectResult {
	return &ConnectResult{
		ReceiverID: rt.req.ReceiverID,
		Mode:       rt.mode,
		Endpoint:   rt.client.Base,
		SDPBytes:   rt.sdpBytes,
	}
}

// senderSource is where a Sender that this catalogue never saw lives:
// its own Node, walked once, and the Controller to reach it through.
type senderSource struct {
	snap *CatalogueSnapshot
	ctrl *Controller
}

// resolveRoute turns a request into the PATCH body and the endpoint
// it goes to. foreign caches the Sender Nodes walked on the way, so a
// salvo of routes from one foreign Node walks it once.
func (c *Controller) resolveRoute(ctx context.Context, snap *CatalogueSnapshot, req ConnectRequest, mode is05.ActivationMode, foreign map[string]*senderSource) (*route, error) {
	// The Sender may live on a Node this catalogue never saw.
	senderSnap, senderCtrl := snap, c
	if req.SenderID != "" && req.SenderNode != "" && !hasSender(snap, req.SenderID) {
		src, known := foreign[req.SenderNode]
		if !known {
			sc, err := NewController(ctx, ControllerOptions{
				Logger: c.logger, Deps: c.opts.Deps, Reporter: c.reporter,
				NodeURL: req.SenderNode, APIVer: c.opts.APIVer,
			})
			if err != nil {
				return nil, fmt.Errorf("nmos connect: sender node %s: %w", req.SenderNode, err)
			}
			ss, _ := sc.Walk(ctx)
			src = &senderSource{snap: ss, ctrl: sc}
			foreign[req.SenderNode] = src
		}
		if !hasSender(src.snap, req.SenderID) {
			return nil, fmt.Errorf("nmos connect: sender %s is on neither this catalogue nor %s", req.SenderID, req.SenderNode)
		}
		senderSnap, senderCtrl = src.snap, src.ctrl
	}

	href, err := c.connectionHref(snap, req.ReceiverID)
	if err != nil {
		return nil, err
	}
	cl, err := connection.NewClient(href)
	if err != nil {
		return nil, err
	}

	rt := &route{
		req:    req,
		mode:   mode,
		client: cl,
		patch: map[string]any{
			"master_enable": req.SenderID != "",
			"activation":    activationBody(mode, req.When),
		},
	}

	if req.SenderID == "" {
		// Disconnect. sender_id must be explicitly null — omitting it
		// would leave the existing one in place, because PATCH merges.
		rt.patch["sender_id"] = nil
	} else if mxlLeg(snap, req.SenderID, req.ReceiverID) {
		// BCP-007-03: an MXL connection carries NO transport file —
		// the receiver locates the flow through the MXL runtime, not
		// an SDP, and the spec's Controller suite (BCP-007-03-02
		// test_03) scores a PATCH that attaches one as non-conformant.
		// Deliberately not even requesting /transportfile: an MXL
		// sender has none to serve.
		rt.patch["sender_id"] = req.SenderID
	} else {
		rt.patch["sender_id"] = req.SenderID
		// The transport file is served by the Sender's own IS-05 — its
		// Device's sr-ctrl control, which is the Receiver's only when
		// both sit on one device. Asking the Receiver's IS-05 for a
		// foreign Sender's SDP is what staged "{}" on a FusioN6 (#1114).
		tf := cl
		if shref, err := senderCtrl.senderConnectionHref(senderSnap, req.SenderID); err == nil {
			if scl, err := connection.NewClient(shref); err == nil {
				tf = scl
			}
		}
		sdp, err := tf.TransportFile(ctx, req.SenderID)
		switch {
		case err != nil:
			// Not fatal. A Sender on a non-RTP transport has no SDP,
			// and some devices simply do not serve one. The Receiver
			// can still be pointed at the Sender by id; we record the
			// gap rather than refusing to route.
			c.fire(spec.SeverityWarn, "nmos_is05_no_transport_file",
				fmt.Sprintf("sender %s served no transport file: %v", req.SenderID, err),
				req.SenderID)
		case strings.TrimSpace(sdp) == "" || strings.TrimSpace(sdp) == "{}":
			// "{}" is what a Node answers for a Sender it does not own:
			// not an SDP, never staged.
			c.fire(spec.SeverityWarn, "nmos_is05_empty_transport_file",
				fmt.Sprintf("sender %s served an empty transport file", req.SenderID),
				req.SenderID)
		default:
			rt.patch["transport_file"] = map[string]any{
				"data": sdp,
				"type": "application/sdp",
			}
			rt.sdpBytes = len(sdp)
		}
	}
	return rt, nil
}

// describeDryRun fills the dry-run half of a result: what would have
// been sent, and what the receiver is doing right now and would lose.
func (c *Controller) describeDryRun(ctx context.Context, rt *route, res *ConnectResult) {
	res.DryRun = true
	res.Patch = rt.patch
	// Read the receiver's ACTIVE state, not its staged state: what
	// the operator is about to overwrite is what the device is
	// currently doing.
	if active, err := rt.client.ActiveReceiver(ctx, rt.req.ReceiverID); err == nil {
		res.CurrentSenderID = active.SenderID
		res.CurrentMasterEnable = active.MasterEnable
	} else {
		c.fire(spec.SeverityWarn, "nmos_is05_active_unreadable",
			fmt.Sprintf("receiver %s active state unreadable: %v", rt.req.ReceiverID, err),
			rt.req.ReceiverID)
	}
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
	return c.hrefForDevice(snap, deviceID, receiverID, "receiver")
}

// hasSender reports whether the catalogue lists the Sender.
func hasSender(snap *CatalogueSnapshot, id string) bool {
	for _, s := range snap.Senders {
		if s.ID == id {
			return true
		}
	}
	return false
}

// senderConnectionHref is the Sender-side twin. Same rule: the endpoint
// comes from the owning Device's IS-04 `controls`, never from a guess.
func (c *Controller) senderConnectionHref(snap *CatalogueSnapshot, senderID string) (string, error) {
	deviceID := ""
	for _, s := range snap.Senders {
		if s.ID == senderID {
			deviceID = s.DeviceID
			break
		}
	}
	if deviceID == "" {
		return "", fmt.Errorf("nmos: no sender %s in this catalogue "+
			"(walk it first to see what is there)", senderID)
	}
	return c.hrefForDevice(snap, deviceID, senderID, "sender")
}

func (c *Controller) hrefForDevice(snap *CatalogueSnapshot, deviceID, resourceID, kind string) (string, error) {
	for _, d := range snap.Devices {
		if d.ID != deviceID {
			continue
		}
		if href := pickConnectionControl(d.Controls); href != "" {
			return href, nil
		}
		return "", fmt.Errorf("nmos: device %s advertises no "+
			"urn:x-nmos:control:sr-ctrl control, so it has no IS-05 endpoint "+
			"to drive", deviceID)
	}
	return "", fmt.Errorf("nmos: %s %s names device %s, which is "+
		"not in this catalogue", kind, resourceID, deviceID)
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

// mxlLeg reports whether either end of the requested connection rides
// urn:x-nmos:transport:mxl (or a dotted subclassification), looked up
// in the snapshot already fetched for control-href resolution. BCP-007-03
// forbids a transport file on MXL connections, so Connect branches on it.
func mxlLeg(snap *CatalogueSnapshot, senderID, receiverID string) bool {
	isMXL := func(t string) bool {
		return t == is04.TransportMXL || strings.HasPrefix(t, is04.TransportMXL+".")
	}
	for _, r := range snap.Receivers {
		if r.ID == receiverID && isMXL(r.Transport) {
			return true
		}
	}
	for _, s := range snap.Senders {
		if s.ID == senderID && isMXL(s.Transport) {
			return true
		}
	}
	return false
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
