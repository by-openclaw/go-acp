package consumer

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is08"
	"dhs/internal/amwa/codec/spec"
	"dhs/internal/amwa/session/channelmapping"
)

// ChannelMapRequest asks the Controller to read, or change, the audio
// channel map of one Device (IS-08).
type ChannelMapRequest struct {
	// DeviceID is the IS-04 Device whose Channel Mapping API is driven.
	// Required: inputs and outputs are named per Device, not globally.
	DeviceID string

	// Routes are the output channels to set. Empty means read only.
	Routes []ChannelRoute

	// Mode defaults to activate_immediate. Scheduled modes need When.
	Mode is08.ActivationMode

	// When is the TAI timestamp ("<secs>:<nanos>") for a scheduled
	// activation. Ignored for activate_immediate.
	When string

	// Cancel is the id of a scheduled activation to withdraw before its
	// instant. It excludes Routes.
	Cancel string

	// DryRun resolves and checks everything, and sends nothing.
	DryRun bool
}

// ChannelRoute says what one channel of one output carries.
type ChannelRoute struct {
	Output  string // IS-08 output id
	Channel int    // channel index on that output

	// Input is the IS-08 input id feeding it; empty means unrouted
	// (IS-08's explicit "no input": the output channel carries nothing).
	Input        string
	InputChannel int
}

// ChannelMapResult reports what the Device holds and, after a change,
// what it says it did — not what was asked.
type ChannelMapResult struct {
	DeviceID string
	Endpoint string // the cm-ctrl href resolved from IS-04

	IO      is08.IO
	Active  is08.MapActive
	Pending map[string]is08.MapActivationResponse // scheduled, still waiting

	// Request is what was sent, or would be with DryRun.
	Request *is08.MapActivationRequest
	DryRun  bool

	// ActivationID is the activation the Device created; Scheduled says
	// it is waiting for its instant rather than applied.
	ActivationID string
	Scheduled    bool

	// Cancelled is the activation id withdrawn.
	Cancelled string
}

// ChannelMap is the IS-08 half of the Controller role: it reads a
// Device's channel map and, when Routes are given, re-maps it.
//
// The endpoint comes from the Device's IS-04 `controls`
// (urn:x-nmos:control:cm-ctrl), never from a host guess. Every route
// is checked against what the Device itself declares (its outputs,
// their channels, the inputs each output can take) before anything is
// sent: a re-map moves real audio and IS-08 has no undo. An immediate
// re-map is read back from /map/active and a Device that answered 200
// and did not apply it is reported.
func (c *Controller) ChannelMap(ctx context.Context, req ChannelMapRequest) (*ChannelMapResult, error) {
	mode, err := checkChannelMapRequest(req)
	if err != nil {
		return nil, err
	}

	snap, _ := c.Walk(ctx)
	href, err := channelMappingHref(snap, req.DeviceID)
	if err != nil {
		return nil, err
	}
	cl, err := channelmapping.NewClient(href)
	if err != nil {
		return nil, err
	}
	res := &ChannelMapResult{DeviceID: req.DeviceID, Endpoint: cl.Base, DryRun: req.DryRun}

	if req.Cancel != "" {
		if req.DryRun {
			return res, nil
		}
		if err := cl.Cancel(ctx, req.Cancel); err != nil {
			return nil, err
		}
		res.Cancelled = req.Cancel
		return res, nil
	}

	if res.IO, err = cl.IO(ctx); err != nil {
		return nil, err
	}
	if res.Active, err = cl.Active(ctx); err != nil {
		return nil, err
	}
	if len(req.Routes) == 0 {
		if res.Pending, err = cl.Activations(ctx); err != nil {
			return nil, err
		}
		return res, nil
	}

	if problems := channelRouteProblems(res.IO, req.Routes); len(problems) > 0 {
		return nil, fmt.Errorf("nmos map: device %s cannot take this map: %s",
			req.DeviceID, strings.Join(problems, "; "))
	}
	body := channelMapBody(mode, req.When, req.Routes)
	res.Request = &body
	if req.DryRun {
		return res, nil
	}

	id, _, scheduled, err := cl.Activate(ctx, body)
	if err != nil {
		return nil, err
	}
	res.ActivationID, res.Scheduled = id, scheduled
	if scheduled {
		return res, nil
	}

	// Applied, the Device says. Read it back: the map it reports is the
	// one the operator hears.
	if res.Active, err = cl.Active(ctx); err != nil {
		return nil, err
	}
	for _, r := range req.Routes {
		if !routeApplied(res.Active.Map, r) {
			c.fire(spec.SeverityError, "nmos_is08_map_not_applied",
				fmt.Sprintf("device %s accepted the map but /map/active does not show output %s channel %d %s",
					req.DeviceID, r.Output, r.Channel, routeTarget(r)), req.DeviceID)
		}
	}
	return res, nil
}

// checkChannelMapRequest is the pre-flight before anything is walked.
// It returns the mode with the default applied.
func checkChannelMapRequest(req ChannelMapRequest) (is08.ActivationMode, error) {
	if req.DeviceID == "" {
		return "", fmt.Errorf("nmos map: a device id is required " +
			"(run `dhs consumer nmos walk -l` to list them)")
	}
	if req.Cancel != "" && len(req.Routes) > 0 {
		return "", fmt.Errorf("nmos map: cancelling an activation and setting routes are two requests")
	}
	mode := req.Mode
	if mode == "" {
		mode = is08.ActivationModeImmediate
	}
	if !is08.IsValidActivationMode(mode) {
		return "", fmt.Errorf("nmos map: %q is not an IS-08 activation mode", mode)
	}
	if mode != is08.ActivationModeImmediate && req.When == "" {
		return "", fmt.Errorf("nmos map: %s needs a time (<secs>:<nanos>)", mode)
	}
	return mode, nil
}

// channelMappingHref finds the Device's Channel Mapping API in the
// catalogue: the highest cm-ctrl version it advertises.
func channelMappingHref(snap *CatalogueSnapshot, deviceID string) (string, error) {
	for _, d := range snap.Devices {
		if d.ID != deviceID {
			continue
		}
		if href := pickControl(d.Controls, "urn:x-nmos:control:cm-ctrl/"); href != "" {
			return href, nil
		}
		return "", fmt.Errorf("nmos map: device %s advertises no "+
			"urn:x-nmos:control:cm-ctrl control, so it has no IS-08 endpoint to drive", deviceID)
	}
	return "", fmt.Errorf("nmos map: no device %s in this catalogue "+
		"(walk it first to see what is there)", deviceID)
}

// pickControl selects the highest version of one control type a Device
// advertises. Controls are unordered, and a Device commonly lists
// several minors.
func pickControl(controls []is04.DeviceControl, prefix string) string {
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

// channelRouteProblems checks routes against what the Device declares:
// the output and its channel exist, the input and its channel exist,
// and the output can take that input (caps.routable_inputs — a null
// list means any input, a null entry means "unrouted" is allowed).
// Reordering and block-size rules are the Device's to enforce: it
// refuses with its own words, which the client carries back.
func channelRouteProblems(io is08.IO, routes []ChannelRoute) []string {
	var problems []string
	for _, r := range routes {
		out, ok := io.Outputs[r.Output]
		if !ok {
			problems = append(problems, fmt.Sprintf("no output %q", r.Output))
			continue
		}
		if r.Channel < 0 || r.Channel >= len(out.Channels) {
			problems = append(problems, fmt.Sprintf("output %s has %d channel(s), not a channel %d",
				r.Output, len(out.Channels), r.Channel))
			continue
		}
		if r.Input == "" {
			if !routable(out, nil) {
				problems = append(problems, fmt.Sprintf("output %s cannot be left unrouted", r.Output))
			}
			continue
		}
		in, ok := io.Inputs[r.Input]
		if !ok {
			problems = append(problems, fmt.Sprintf("no input %q", r.Input))
			continue
		}
		if r.InputChannel < 0 || r.InputChannel >= len(in.Channels) {
			problems = append(problems, fmt.Sprintf("input %s has %d channel(s), not a channel %d",
				r.Input, len(in.Channels), r.InputChannel))
			continue
		}
		if !routable(out, &r.Input) {
			problems = append(problems, fmt.Sprintf("output %s does not take input %s", r.Output, r.Input))
		}
	}
	return problems
}

// routable reports whether an output takes an input (nil = unrouted).
func routable(out is08.Output, input *string) bool {
	if out.Caps == nil || out.Caps.RoutableInputs == nil {
		return true
	}
	for _, allowed := range out.Caps.RoutableInputs {
		switch {
		case allowed == nil && input == nil:
			return true
		case allowed != nil && input != nil && *allowed == *input:
			return true
		}
	}
	return false
}

// channelMapBody builds the POST /map/activations body. Only the
// scheduled modes carry a requested_time.
func channelMapBody(mode is08.ActivationMode, when string, routes []ChannelRoute) is08.MapActivationRequest {
	body := is08.MapActivationRequest{
		Activation: is08.Activation{Mode: mode},
		Action:     is08.MapEntries{},
	}
	if mode != is08.ActivationModeImmediate {
		body.Activation.RequestedTime = &when
	}
	for _, r := range routes {
		if body.Action[r.Output] == nil {
			body.Action[r.Output] = map[string]is08.MapEntry{}
		}
		entry := is08.MapEntry{}
		if r.Input != "" {
			input, index := r.Input, r.InputChannel
			entry.Input, entry.ChannelIndex = &input, &index
		}
		body.Action[r.Output][strconv.Itoa(r.Channel)] = entry
	}
	return body
}

// routeApplied reports whether the active map shows the route.
func routeApplied(active is08.MapEntries, r ChannelRoute) bool {
	got, ok := active[r.Output][strconv.Itoa(r.Channel)]
	if !ok {
		return false
	}
	if r.Input == "" {
		return got.Input == nil
	}
	return got.Input != nil && *got.Input == r.Input &&
		got.ChannelIndex != nil && *got.ChannelIndex == r.InputChannel
}

// routeTarget words what a route asked for.
func routeTarget(r ChannelRoute) string {
	if r.Input == "" {
		return "unrouted"
	}
	return fmt.Sprintf("from input %s channel %d", r.Input, r.InputChannel)
}
