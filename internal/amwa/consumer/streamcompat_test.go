package consumer

// The IS-11 half of the Controller: the endpoint comes from the owning
// Device's stream-compat control, constraints a Sender does not say it
// supports are refused before they are sent, and the result is what the
// Device reports afterwards.

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is11"
)

// compatDeviceAPI is the Stream Compatibility API of one Device with
// one Sender (fed by one Input) and one Receiver (driving one Output).
type compatDeviceAPI struct {
	sender, receiver string
	active           string // the sender's active constraints body
	put              []string
	released         int
	fail             string // a path suffix that answers 500
}

func (d *compatDeviceAPI) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rest := r.URL.Path[strings.Index(r.URL.Path, "/v1.0/")+len("/v1.0/"):]
		if d.fail != "" && strings.HasSuffix(rest, d.fail) {
			http.Error(w, "device fault", http.StatusInternalServerError)
			return
		}
		s, rc := "senders/"+d.sender+"/", "receivers/"+d.receiver+"/"
		switch r.Method + " " + rest {
		case "GET " + s + "status/":
			state := is11.SenderUnconstrained
			if d.active != `{"constraint_sets":[]}` {
				state = is11.SenderConstrained
			}
			_, _ = io.WriteString(w, `{"state":"`+state+`"}`)
		case "GET " + s + "constraints/active/":
			_, _ = io.WriteString(w, d.active)
		case "GET " + s + "constraints/supported/":
			_, _ = io.WriteString(w, `{"parameter_constraints":["urn:x-nmos:cap:format:frame_width","urn:x-nmos:cap:format:grain_rate"]}`)
		case "PUT " + s + "constraints/active/":
			body, _ := io.ReadAll(r.Body)
			d.put = append(d.put, string(body))
			d.active = string(body)
			_, _ = w.Write(body)
		case "DELETE " + s + "constraints/active/":
			d.released++
			d.active = `{"constraint_sets":[]}`
			w.WriteHeader(http.StatusNoContent)
		case "GET " + s + "inputs/":
			_, _ = io.WriteString(w, `["in1/"]`)
		case "GET inputs/in1/properties/":
			_, _ = io.WriteString(w, `{"id":"in1","version":"1:0","label":"SDI 1","description":"input","tags":{},
			  "base_edid_support":false,"connected":true,"edid_support":false,
			  "status":{"state":"signal_present"},"device_id":"dev1"}`)
		case "GET " + rc + "status/":
			_, _ = io.WriteString(w, `{"state":"non_compliant_stream","debug":"grain_rate 50/1 is not in caps"}`)
		case "GET " + rc + "outputs/":
			_, _ = io.WriteString(w, `["out1/"]`)
		case "GET outputs/out1/properties/":
			_, _ = io.WriteString(w, `{"id":"out1","version":"1:0","label":"SDI out","description":"output","tags":{},
			  "connected":true,"edid_support":false,"status":{"state":"signal_present"},"device_id":"dev1"}`)
		default:
			http.NotFound(w, r)
		}
	}
}

// compatHarness is a catalogue with one Device that advertises IS-11,
// one Sender and one Receiver on it, and the API behind them.
func compatHarness(t *testing.T) (*harness, *compatDeviceAPI) {
	t.Helper()
	h := newHarness(t)
	d := &compatDeviceAPI{sender: uuidN(11), receiver: uuidN(12), active: `{"constraint_sets":[]}`}
	h.is11 = d.handler()
	dev := deviceWith(uuidN(1), "")
	dev.Controls = []is04.DeviceControl{{
		Href: strings.Replace(h.controlHref, "/x-nmos/connection/v1.1", "/x-nmos/streamcompatibility/v1.0", 1),
		Type: "urn:x-nmos:control:stream-compat/v1.0",
	}}
	h.cat.devices = []is04.Device{dev}
	h.cat.senders = []is04.Sender{senderOn(d.sender, "cam", dev.ID, is04.TransportRTP)}
	h.cat.receivers = []is04.Receiver{receiverOn(d.receiver, dev.ID, is04.TransportRTP)}
	return h, d
}

func frameWidth(n float64) *is11.ActiveConstraints {
	return &is11.ActiveConstraints{ConstraintSets: []is11.ConstraintSet{{
		"urn:x-nmos:cap:meta:label":         "HD only",
		"urn:x-nmos:cap:format:frame_width": map[string]any{"enum": []any{n}},
	}}}
}

func TestCompatReadsASenderAndAReceiver(t *testing.T) {
	h, d := compatHarness(t)
	ctx := context.Background()

	res, err := h.ctrl.Compat(ctx, CompatRequest{SenderID: d.sender})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(res.Endpoint, "/x-nmos/streamcompatibility/v1.0") ||
		res.Status.State != is11.SenderUnconstrained || len(res.Supported) != 2 ||
		len(res.Inputs) != 1 || res.Inputs[0].Label != "SDI 1" || res.Changed {
		t.Errorf("sender read = %+v", res)
	}

	res, err = h.ctrl.Compat(ctx, CompatRequest{ReceiverID: d.receiver})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status.State != is11.ReceiverNonCompliantStream || !strings.Contains(res.Status.Debug, "grain_rate") ||
		len(res.Outputs) != 1 {
		t.Errorf("receiver read = %+v — the Device's reason must come through", res)
	}
}

func TestCompatConstrainsAndReleasesASender(t *testing.T) {
	h, d := compatHarness(t)
	ctx := context.Background()

	res, err := h.ctrl.Compat(ctx, CompatRequest{SenderID: d.sender, Constraints: frameWidth(1920)})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Status.State != is11.SenderConstrained || len(res.Active.ConstraintSets) != 1 {
		t.Errorf("after the PUT = %+v", res)
	}
	if len(d.put) != 1 || !strings.Contains(d.put[0], "frame_width") {
		t.Errorf("the Device was sent %v", d.put)
	}

	res, err = h.ctrl.Compat(ctx, CompatRequest{SenderID: d.sender, Release: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Status.State != is11.SenderUnconstrained || d.released != 1 {
		t.Errorf("after the release = %+v (released %d)", res, d.released)
	}
}

func TestCompatDryRunChangesNothing(t *testing.T) {
	h, d := compatHarness(t)
	ctx := context.Background()

	for _, req := range []CompatRequest{
		{SenderID: d.sender, Constraints: frameWidth(1920), DryRun: true},
		{SenderID: d.sender, Release: true, DryRun: true},
	} {
		res, err := h.ctrl.Compat(ctx, req)
		if err != nil || res.Changed || !res.DryRun {
			t.Errorf("dry run = %+v, %v", res, err)
		}
	}
	if len(d.put) != 0 || d.released != 0 {
		t.Errorf("a dry run reached the Device: put %v, released %d", d.put, d.released)
	}
}

// A constraint the Sender does not say it supports is refused before
// it is sent, naming what it does support.
func TestCompatRefusesAConstraintTheSenderDoesNotSupport(t *testing.T) {
	h, d := compatHarness(t)

	ac := &is11.ActiveConstraints{ConstraintSets: []is11.ConstraintSet{
		{"urn:x-nmos:cap:format:color_sampling": map[string]any{"enum": []any{"YCbCr-4:2:2"}}},
		{"urn:x-nmos:cap:format:color_sampling": map[string]any{"enum": []any{"RGB"}}, "urn:x-nmos:cap:format:frame_width": map[string]any{"enum": []any{float64(1920)}}},
	}}
	_, err := h.ctrl.Compat(context.Background(), CompatRequest{SenderID: d.sender, Constraints: ac})
	if err == nil || !strings.Contains(err.Error(), "color_sampling") || !strings.Contains(err.Error(), "frame_width") {
		t.Errorf("refusal = %v — it must name the unsupported URN once and what the Sender supports", err)
	}
	if strings.Count(err.Error(), "color_sampling") != 1 {
		t.Errorf("the unsupported URN must be named once: %v", err)
	}
	if len(d.put) != 0 {
		t.Error("a refused constraint set reached the Device")
	}
}

func TestCompatRequestIsCheckedBeforeAnythingIsWalked(t *testing.T) {
	h, d := compatHarness(t)
	ctx := context.Background()

	for name, req := range map[string]CompatRequest{
		"nothing named":             {},
		"both named":                {SenderID: d.sender, ReceiverID: d.receiver},
		"constraints on a receiver": {ReceiverID: d.receiver, Constraints: frameWidth(1920)},
		"release on a receiver":     {ReceiverID: d.receiver, Release: true},
		"set and release":           {SenderID: d.sender, Constraints: frameWidth(1920), Release: true},
		"a sender not listed":       {SenderID: uuidN(99)},
		"a receiver not listed":     {ReceiverID: uuidN(99)},
	} {
		if _, err := h.ctrl.Compat(ctx, req); err == nil {
			t.Errorf("%s: must be refused", name)
		}
	}

	// A Device with no stream-compat control has no IS-11 endpoint; one
	// whose control href carries no version cannot be driven.
	h.cat.devices[0].Controls = []is04.DeviceControl{}
	if _, err := h.ctrl.Compat(ctx, CompatRequest{SenderID: d.sender}); err == nil ||
		!strings.Contains(err.Error(), "stream-compat") {
		t.Errorf("a Device without IS-11: %v", err)
	}
	h.cat.devices[0].Controls = []is04.DeviceControl{{Href: "http://h/x-nmos/streamcompatibility", Type: "urn:x-nmos:control:stream-compat/v1.0"}}
	if _, err := h.ctrl.Compat(ctx, CompatRequest{SenderID: d.sender}); err == nil {
		t.Error("a control href with no version must be refused")
	}
}

// Every call to the Device can fail; each failure is the caller's to
// see, in the Device's words.
func TestCompatCarriesTheDevicesFailures(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		fail string
		req  func(d *compatDeviceAPI) CompatRequest
	}{
		{"constraints/supported/", func(d *compatDeviceAPI) CompatRequest { return CompatRequest{SenderID: d.sender} }},
		{"status/", func(d *compatDeviceAPI) CompatRequest { return CompatRequest{SenderID: d.sender} }},
		{"constraints/active/", func(d *compatDeviceAPI) CompatRequest { return CompatRequest{SenderID: d.sender} }},
		{"inputs/", func(d *compatDeviceAPI) CompatRequest { return CompatRequest{SenderID: d.sender} }},
		{"inputs/in1/properties/", func(d *compatDeviceAPI) CompatRequest { return CompatRequest{SenderID: d.sender} }},
		{"constraints/active/", func(d *compatDeviceAPI) CompatRequest {
			return CompatRequest{SenderID: d.sender, Constraints: frameWidth(1920)}
		}},
		{"constraints/active/", func(d *compatDeviceAPI) CompatRequest {
			return CompatRequest{SenderID: d.sender, Release: true}
		}},
		{"status/", func(d *compatDeviceAPI) CompatRequest { return CompatRequest{ReceiverID: d.receiver} }},
		{"outputs/", func(d *compatDeviceAPI) CompatRequest { return CompatRequest{ReceiverID: d.receiver} }},
		{"outputs/out1/properties/", func(d *compatDeviceAPI) CompatRequest { return CompatRequest{ReceiverID: d.receiver} }},
	} {
		h, d := compatHarness(t)
		d.fail = tc.fail
		if _, err := h.ctrl.Compat(ctx, tc.req(d)); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
			t.Errorf("%s failing: %v", tc.fail, err)
		}
	}
}
