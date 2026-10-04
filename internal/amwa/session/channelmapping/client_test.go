package channelmapping

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is08"
)

// Bodies as the IS-08 v1.0 schemas shape them (codec/is08/testdata).
const (
	ioBody = `{
	  "inputs":  {"in1": {"properties": {"name": "SDI 1", "description": "embedded"},
	                      "parent": {"id": null, "type": null},
	                      "channels": [{"label": "L"}, {"label": "R"}],
	                      "caps": {"reordering": true, "block_size": 1}}},
	  "outputs": {"out1": {"properties": {"name": "Tx 1", "description": "2110-30"},
	                       "source_id": null,
	                       "channels": [{"label": "1"}, {"label": "2"}],
	                       "caps": {"routable_inputs": ["in1", null]}}}
	}`
	activeBody = `{
	  "activation": {"mode": null, "requested_time": null, "activation_time": "1700000000:0"},
	  "map": {"out1": {"0": {"input": "in1", "channel_index": 0},
	                   "1": {"input": null, "channel_index": null}}}
	}`
	immediate = `{"act-1": {"activation": {"mode": "activate_immediate", "requested_time": null, "activation_time": "1700000001:0"},
	                        "action": {"out1": {"0": {"input": "in1", "channel_index": 1}}}}}`
	scheduled = `{"act-2": {"activation": {"mode": "activate_scheduled_relative", "requested_time": "2:0", "activation_time": "1700000003:0"},
	                        "action": {"out1": {"1": {"input": "in1", "channel_index": 0}}}}}`
)

// device serves the Channel Mapping API of one Device and records what
// it was asked.
type device struct {
	srv      *httptest.Server
	posted   []is08.MapActivationRequest
	deleted  []string
	schedule bool   // answer POST with 202 and a scheduled activation
	refuse   int    // when set, every request answers this status
	answer   string // when set, replaces the POST answer
}

func newDevice(t *testing.T) *device {
	t.Helper()
	d := &device{}
	d.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if d.refuse != 0 {
			w.WriteHeader(d.refuse)
			_, _ = io.WriteString(w, `{"code":400,"error":"input in9 is not routable to output out1","debug":null}`)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/x-nmos/channelmapping/v1.0/")
		switch {
		case r.Method == http.MethodGet && rest == "io":
			_, _ = io.WriteString(w, ioBody)
		case r.Method == http.MethodGet && rest == "map/active":
			_, _ = io.WriteString(w, activeBody)
		case r.Method == http.MethodGet && rest == "map/activations":
			_, _ = io.WriteString(w, scheduled)
		case r.Method == http.MethodPost && rest == "map/activations":
			if got := r.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("POST Content-Type = %q", got)
			}
			var req is08.MapActivationRequest
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &req); err != nil {
				t.Errorf("POST body: %v", err)
			}
			d.posted = append(d.posted, req)
			switch {
			case d.answer != "":
				_, _ = io.WriteString(w, d.answer)
			case d.schedule:
				w.WriteHeader(http.StatusAccepted)
				_, _ = io.WriteString(w, scheduled)
			default:
				_, _ = io.WriteString(w, immediate)
			}
		case r.Method == http.MethodDelete && strings.HasPrefix(rest, "map/activations/"):
			d.deleted = append(d.deleted, strings.TrimPrefix(rest, "map/activations/"))
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(d.srv.Close)
	return d
}

func (d *device) client(t *testing.T) *Client {
	t.Helper()
	c, err := NewClient(d.srv.URL + "/x-nmos/channelmapping/v1.0/")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewClientReadsTheVersionFromTheControlHref(t *testing.T) {
	c, err := NewClient("http://10.6.255.103:3000/x-nmos/channelmapping/v1.0/")
	if err != nil {
		t.Fatal(err)
	}
	if c.APIVer != "v1.0" || c.Base != "http://10.6.255.103:3000/x-nmos/channelmapping/v1.0" {
		t.Errorf("client = %+v", c)
	}
	for _, bad := range []string{
		"http://bad host/x-nmos/channelmapping/v1.0",     // not a URL
		"/x-nmos/channelmapping/v1.0",                    // not absolute
		"http://10.6.255.103:3000/x-nmos/channelmapping", // no version
	} {
		if _, err := NewClient(bad); err == nil {
			t.Errorf("NewClient(%q) must be refused", bad)
		}
	}
}

func TestClientReadsTheDeviceAsItIs(t *testing.T) {
	d := newDevice(t)
	c := d.client(t)
	ctx := context.Background()

	inout, err := c.IO(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(inout.Inputs["in1"].Channels) != 2 || !inout.Inputs["in1"].Caps.Reordering {
		t.Errorf("io = %+v", inout)
	}

	active, err := c.Active(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := active.Map["out1"]["0"]; got.Input == nil || *got.Input != "in1" || *got.ChannelIndex != 0 {
		t.Errorf("active map out1/0 = %+v", got)
	}
	if got := active.Map["out1"]["1"]; got.Input != nil {
		t.Errorf("active map out1/1 must read as unrouted, got %+v", got)
	}

	pending, err := c.Activations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pending["act-2"]; !ok || len(pending) != 1 {
		t.Errorf("activations = %+v", pending)
	}
}

func route(output, channel, input string, index int) is08.MapActivationRequest {
	return is08.MapActivationRequest{
		Activation: is08.Activation{Mode: is08.ActivationModeImmediate},
		Action:     is08.MapEntries{output: {channel: {Input: &input, ChannelIndex: &index}}},
	}
}

func TestActivateSaysWhetherTheMapWasAppliedOrQueued(t *testing.T) {
	d := newDevice(t)
	c := d.client(t)
	ctx := context.Background()

	id, resp, queued, err := c.Activate(ctx, route("out1", "0", "in1", 1))
	if err != nil {
		t.Fatal(err)
	}
	if id != "act-1" || queued || resp.Activation.ActivationTime == nil {
		t.Errorf("immediate: id=%q queued=%v resp=%+v", id, queued, resp)
	}
	if len(d.posted) != 1 || *d.posted[0].Action["out1"]["0"].ChannelIndex != 1 {
		t.Errorf("the Device was sent %+v", d.posted)
	}

	d.schedule = true
	id, _, queued, err = c.Activate(ctx, route("out1", "1", "in1", 0))
	if err != nil {
		t.Fatal(err)
	}
	if id != "act-2" || !queued {
		t.Errorf("scheduled: id=%q queued=%v", id, queued)
	}

	if err := c.Cancel(ctx, "act-2"); err != nil {
		t.Fatal(err)
	}
	if len(d.deleted) != 1 || d.deleted[0] != "act-2" {
		t.Errorf("cancelled %v", d.deleted)
	}
}

func TestActivateRefusesWhatItCannotSendOrRead(t *testing.T) {
	d := newDevice(t)
	c := d.client(t)
	ctx := context.Background()

	// A request the codec refuses never leaves.
	if _, _, _, err := c.Activate(ctx, is08.MapActivationRequest{Activation: is08.Activation{Mode: "sometime"}}); err == nil {
		t.Error("an activation mode IS-08 does not define must be refused before it is sent")
	}
	if len(d.posted) != 0 {
		t.Errorf("the refused request reached the Device: %+v", d.posted)
	}

	for name, answer := range map[string]string{
		"not JSON":          `<html>`,
		"two activations":   `{"a": {"activation": {"mode": null, "requested_time": null, "activation_time": null}, "action": {}}, "b": {"activation": {"mode": null, "requested_time": null, "activation_time": null}, "action": {}}}`,
		"an invalid answer": `{"a": {"activation": {"mode": "sometime", "requested_time": null, "activation_time": null}, "action": {}}}`,
	} {
		d.answer = answer
		if _, _, _, err := c.Activate(ctx, route("out1", "0", "in1", 1)); err == nil {
			t.Errorf("%s: the answer must be refused", name)
		}
	}
}

func TestARefusalCarriesTheDevicesOwnWords(t *testing.T) {
	d := newDevice(t)
	d.refuse = http.StatusBadRequest
	c := d.client(t)
	ctx := context.Background()

	_, _, _, err := c.Activate(ctx, route("out1", "0", "in9", 0))
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusBadRequest || !strings.Contains(se.Error(), "not routable") {
		t.Errorf("refusal = %v", err)
	}
	if _, err := c.IO(ctx); err == nil {
		t.Error("a refused read must fail")
	}
	if _, err := c.Active(ctx); err == nil {
		t.Error("a refused read must fail")
	}
	if _, err := c.Activations(ctx); err == nil {
		t.Error("a refused read must fail")
	}
	if err := c.Cancel(ctx, "act-2"); err == nil {
		t.Error("a refused cancel must fail")
	}

	// A status with no body, and one with far too much of it.
	if got := (&StatusError{What: "io", Code: 404}).Error(); got != "nmos/channelmapping: io: HTTP 404" {
		t.Errorf("bodyless error = %q", got)
	}
	if got := (&StatusError{What: "io", Code: 500, Body: strings.Repeat("x", 400)}).Error(); !strings.HasSuffix(got, "…") || len(got) > 360 {
		t.Errorf("long error not cut: %d bytes", len(got))
	}
}

func TestAClientWithNoDeviceSaysSo(t *testing.T) {
	d := newDevice(t)
	c := d.client(t)
	d.srv.Close()
	if _, err := c.IO(context.Background()); err == nil || !strings.Contains(err.Error(), "nmos/channelmapping: io") {
		t.Errorf("unreachable Device: %v", err)
	}

	// A request that cannot even be built.
	c.Base = "http://ok"
	if _, _, err := c.do(context.Background(), "bad method", "io", nil); err == nil {
		t.Error("an unbuildable request must fail")
	}
}
