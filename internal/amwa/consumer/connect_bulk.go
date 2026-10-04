package consumer

// IS-05 bulk routing — a salvo.
//
// The routing change a plant actually makes is rarely one receiver: a
// camera group, a multiviewer wall, a multicast replan that must land
// every receiver in one step. IS-05 has an endpoint for exactly that
// (POST /bulk/receivers): one request per Device, every entry resolved
// and activated by the Device together, a verdict per entry.
//
// Each route is resolved the way Connect resolves one — endpoint from
// the Device's sr-ctrl control, SDP from the Sender's own IS-05, an MXL
// leg without a transport file, a disconnect as sender_id null — so a
// route means the same thing whether it travels alone or in a salvo.

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"dhs/internal/amwa/codec/is05"
	"dhs/internal/amwa/codec/spec"
	"dhs/internal/amwa/session/connection"
)

// BulkRouteResult is one route's outcome in a salvo.
type BulkRouteResult struct {
	ConnectResult

	// Code is the status the Device gave this entry — what the single
	// PATCH would have answered. 0 when the Device gave no verdict
	// (the route was never sent, or its request failed as a whole);
	// Error then says why.
	Code  int
	Error string
}

// OK reports whether the Device applied the route. A dry-run route is
// never OK: nothing was sent.
func (r BulkRouteResult) OK() bool { return r.Code >= 200 && r.Code <= 299 }

// BulkConnectResult is a salvo's outcome, in request order.
type BulkConnectResult struct {
	Routes []BulkRouteResult

	// Requests is how many bulk POSTs went out — one per Connection
	// API endpoint the receivers span.
	Requests int
}

// Failed counts the routes the Devices did not apply.
func (r *BulkConnectResult) Failed() int {
	n := 0
	for _, rt := range r.Routes {
		if !rt.DryRun && !rt.OK() {
			n++
		}
	}
	return n
}

// ConnectBulk routes many Receivers in one request per Device.
//
// Nothing is sent unless every request passes the pre-flight Connect
// applies to one, and no receiver appears twice: a salvo that is half
// malformed is refused whole, before any signal moves. Once it is on
// the wire the outcome is per route — a Device may apply some entries
// and refuse others, and one Device failing does not stop the others'
// requests. dryRun resolves and reports everything without sending.
func (c *Controller) ConnectBulk(ctx context.Context, reqs []ConnectRequest, dryRun bool) (*BulkConnectResult, error) {
	if len(reqs) == 0 {
		return nil, fmt.Errorf("nmos connect: a bulk request needs at least one route")
	}
	modes := make([]is05.ActivationMode, len(reqs))
	seen := make(map[string]bool, len(reqs))
	for i, req := range reqs {
		mode, err := checkConnectRequest(req)
		if err != nil {
			return nil, fmt.Errorf("route %d: %w", i+1, err)
		}
		if seen[req.ReceiverID] {
			return nil, fmt.Errorf("route %d: receiver %s is routed twice in one request", i+1, req.ReceiverID)
		}
		seen[req.ReceiverID] = true
		modes[i] = mode
	}

	snap, _ := c.Walk(ctx)

	// Resolve every route before sending any: an unknown receiver in
	// route 7 must not be discovered after routes 1-6 moved signal.
	foreign := map[string]*senderSource{}
	routes := make([]*route, len(reqs))
	for i, req := range reqs {
		rt, err := c.resolveRoute(ctx, snap, req, modes[i], foreign)
		if err != nil {
			return nil, fmt.Errorf("route %d: %w", i+1, err)
		}
		routes[i] = rt
	}

	out := &BulkConnectResult{Routes: make([]BulkRouteResult, len(routes))}
	for i, rt := range routes {
		out.Routes[i] = BulkRouteResult{ConnectResult: *rt.result()}
	}
	if dryRun {
		for i, rt := range routes {
			c.describeDryRun(ctx, rt, &out.Routes[i].ConnectResult)
		}
		return out, nil
	}

	// One request per endpoint, endpoints in first-appearance order.
	var endpoints []string
	byEndpoint := map[string][]int{}
	for i, rt := range routes {
		base := rt.client.Base
		if _, known := byEndpoint[base]; !known {
			endpoints = append(endpoints, base)
		}
		byEndpoint[base] = append(byEndpoint[base], i)
	}
	for _, base := range endpoints {
		idx := byEndpoint[base]
		out.Requests++
		c.sendBulk(ctx, routes, idx, out)
	}
	return out, nil
}

// sendBulk posts one endpoint's entries and files the verdicts.
func (c *Controller) sendBulk(ctx context.Context, routes []*route, idx []int, out *BulkConnectResult) {
	cl := routes[idx[0]].client
	items := make([]is05.BulkItem, len(idx))
	for n, i := range idx {
		items[n] = is05.BulkItem{ID: routes[i].req.ReceiverID, Params: routes[i].patch}
	}

	results, deviations, err := cl.BulkReceivers(ctx, items)
	var status *connection.StatusError
	if errors.As(err, &status) && noSuchEndpoint(status.Code) {
		// IS-05 requires /bulk; a Device without it deviates. The
		// salvo still lands — entry by entry, on the single endpoint
		// the Device does serve — and the deviation is on the record.
		c.fire(spec.SeverityWarn, "nmos_is05_bulk_unsupported",
			fmt.Sprintf("%s answered HTTP %d to bulk/receivers; routing its %d receivers one by one",
				cl.Base, status.Code, len(idx)), cl.Base)
		for _, i := range idx {
			c.sendSingle(ctx, routes[i], &out.Routes[i])
		}
		return
	}
	if err != nil {
		c.fire(spec.SeverityError, "nmos_is05_bulk_failed",
			fmt.Sprintf("bulk request to %s failed: %v", cl.Base, err), cl.Base)
		for _, i := range idx {
			out.Routes[i].Error = err.Error()
		}
		return
	}
	for _, d := range deviations {
		c.fire(spec.SeverityWarn, "nmos_is05_bulk_response_deviation",
			fmt.Sprintf("%s: bulk response departs from bulk-response-schema.json: %s", cl.Base, d), cl.Base)
	}

	verdicts := make(map[string]is05.BulkResult, len(results))
	for _, r := range results {
		verdicts[r.ID] = r
	}
	for _, i := range idx {
		id := routes[i].req.ReceiverID
		v, answered := verdicts[id]
		if !answered {
			// The Device owes one verdict per entry. A missing one is
			// not a success and not a refusal — it is unknown, and
			// reported as that.
			out.Routes[i].Error = "the Device returned no verdict for this receiver"
			c.fire(spec.SeverityError, "nmos_is05_bulk_no_verdict",
				fmt.Sprintf("%s returned no verdict for receiver %s", cl.Base, id), id)
			continue
		}
		out.Routes[i].Code = v.Code
		out.Routes[i].Error = v.Error
		if !v.OK() {
			c.fire(spec.SeverityError, "nmos_is05_bulk_entry_refused",
				fmt.Sprintf("receiver %s: HTTP %d %s", id, v.Code, v.Error), id)
		}
	}
}

// sendSingle routes one entry through the single endpoint — the
// fallback for a Device that serves no bulk endpoint.
func (c *Controller) sendSingle(ctx context.Context, rt *route, res *BulkRouteResult) {
	staged, err := rt.client.PatchReceiver(ctx, rt.req.ReceiverID, rt.patch)
	if err != nil {
		res.Error = err.Error()
		var status *connection.StatusError
		if errors.As(err, &status) {
			res.Code = status.Code
		}
		return
	}
	res.Code = http.StatusOK
	res.SenderID = staged.SenderID
	res.MasterEnable = staged.MasterEnable
}

// noSuchEndpoint is the set of answers that mean "this Device does not
// serve bulk" rather than "this Device refused this request".
func noSuchEndpoint(code int) bool {
	return code == http.StatusNotFound || code == http.StatusMethodNotAllowed || code == http.StatusNotImplemented
}
