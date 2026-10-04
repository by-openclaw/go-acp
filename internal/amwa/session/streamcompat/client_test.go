package streamcompat

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is11"
)

const (
	inputBody = `{"id":"in1","version":"1:0","label":"HDMI 1","description":"input","tags":{},
	  "base_edid_support":true,"connected":true,"edid_support":true,
	  "status":{"state":"signal_present"},"device_id":"dev1"}`
	outputBody = `{"id":"out1","version":"1:0","label":"HDMI out","description":"output","tags":{},
	  "connected":true,"edid_support":true,"status":{"state":"signal_present"},"device_id":"dev1"}`
	activeBody = `{"constraint_sets":[{"urn:x-nmos:cap:format:frame_width":{"enum":[1920]}}]}`
)

// device serves the Stream Compatibility API of one Device.
type device struct {
	srv     *httptest.Server
	put     [][]byte // bodies PUT, in order
	putType []string
	deleted []string
	refuse  int               // when set, every request answers this status
	answers map[string]string // path -> body, replacing the default
}

func newDevice(t *testing.T) *device {
	t.Helper()
	d := &device{answers: map[string]string{}}
	d.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if d.refuse != 0 {
			w.WriteHeader(d.refuse)
			_, _ = io.WriteString(w, `{"code":400,"error":"Invalid constraints","debug":"frame_width 99 is not supported"}`)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/x-nmos/streamcompatibility/v1.0/")
		if body, ok := d.answers[rest]; ok && r.Method == http.MethodGet {
			_, _ = io.WriteString(w, body)
			return
		}
		switch r.Method + " " + rest {
		case "GET senders/":
			_, _ = io.WriteString(w, `["s1/","s2/"]`)
		case "GET senders/s1/inputs/":
			_, _ = io.WriteString(w, `["in1/"]`)
		case "GET senders/s1/status/":
			_, _ = io.WriteString(w, `{"state":"constrained"}`)
		case "GET receivers/r1/status/":
			_, _ = io.WriteString(w, `{"state":"non_compliant_stream","debug":"frame rate 50 not in caps"}`)
		case "GET inputs/in1/properties/":
			_, _ = io.WriteString(w, inputBody)
		case "GET outputs/out1/properties/":
			_, _ = io.WriteString(w, outputBody)
		case "GET senders/s1/constraints/active/":
			_, _ = io.WriteString(w, activeBody)
		case "GET senders/s1/constraints/supported/":
			_, _ = io.WriteString(w, `{"parameter_constraints":["urn:x-nmos:cap:format:frame_width"]}`)
		case "PUT senders/s1/constraints/active/", "PUT inputs/in1/edid/base/":
			body, _ := io.ReadAll(r.Body)
			d.put = append(d.put, body)
			d.putType = append(d.putType, r.Header.Get("Content-Type"))
			if strings.HasPrefix(rest, "senders/") {
				_, _ = w.Write(body)
			} else {
				w.WriteHeader(http.StatusNoContent)
			}
		case "DELETE senders/s1/constraints/active/", "DELETE inputs/in1/edid/base/":
			d.deleted = append(d.deleted, rest)
			w.WriteHeader(http.StatusNoContent)
		case "GET inputs/in1/edid/base/", "GET inputs/in1/edid/effective/", "GET outputs/out1/edid/":
			_, _ = w.Write([]byte{0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x00})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(d.srv.Close)
	return d
}

func (d *device) client(t *testing.T) *Client {
	t.Helper()
	c, err := NewClient(d.srv.URL + "/x-nmos/streamcompatibility/v1.0/")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewClientReadsTheVersionFromTheControlHref(t *testing.T) {
	c, err := NewClient("http://10.6.255.102:3000/x-nmos/streamcompatibility/v1.0/")
	if err != nil {
		t.Fatal(err)
	}
	if c.APIVer != "v1.0" || c.Base != "http://10.6.255.102:3000/x-nmos/streamcompatibility/v1.0" {
		t.Errorf("client = %+v", c)
	}
	for _, bad := range []string{
		"http://bad host/x-nmos/streamcompatibility/v1.0",
		"/x-nmos/streamcompatibility/v1.0",
		"http://10.6.255.102:3000/x-nmos/streamcompatibility",
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

	ids, err := c.List(ctx, KindSenders)
	if err != nil || len(ids) != 2 || ids[0] != "s1" {
		t.Errorf("senders = %v, %v — want the ids without their trailing slash", ids, err)
	}
	ids, err = c.List(ctx, "/senders/s1/inputs/")
	if err != nil || len(ids) != 1 || ids[0] != "in1" {
		t.Errorf("a sender's inputs = %v, %v", ids, err)
	}

	st, err := c.Status(ctx, KindSenders, "s1")
	if err != nil || st.State != is11.SenderConstrained {
		t.Errorf("sender status = %+v, %v", st, err)
	}
	st, err = c.Status(ctx, KindReceivers, "r1")
	if err != nil || st.State != is11.ReceiverNonCompliantStream || st.Debug == "" {
		t.Errorf("receiver status = %+v, %v — the Device's reason must come through", st, err)
	}

	in, err := c.Input(ctx, "in1")
	if err != nil || !in.BaseEDIDSupport || in.Status.State != is11.InputSignalPresent {
		t.Errorf("input = %+v, %v", in, err)
	}
	out, err := c.Output(ctx, "out1")
	if err != nil || !out.Connected {
		t.Errorf("output = %+v, %v", out, err)
	}

	ac, err := c.ActiveConstraints(ctx, "s1")
	if err != nil || len(ac.ConstraintSets) != 1 {
		t.Errorf("active constraints = %+v, %v", ac, err)
	}
	sc, err := c.SupportedConstraints(ctx, "s1")
	if err != nil || len(sc.ParameterConstraints) != 1 {
		t.Errorf("supported constraints = %+v, %v", sc, err)
	}

	for _, which := range []string{EDIDBase, EDIDEffective} {
		edid, err := c.EDID(ctx, KindInputs, "in1", which)
		if err != nil || len(edid) != 8 {
			t.Errorf("input %s EDID = %x, %v", which, edid, err)
		}
	}
	if edid, err := c.EDID(ctx, KindOutputs, "out1", ""); err != nil || len(edid) != 8 {
		t.Errorf("output EDID = %x, %v", edid, err)
	}
}

func TestClientConstrainsAndReleasesASender(t *testing.T) {
	d := newDevice(t)
	c := d.client(t)
	ctx := context.Background()

	want := is11.ActiveConstraints{ConstraintSets: []is11.ConstraintSet{
		{"urn:x-nmos:cap:format:frame_width": map[string]any{"enum": []any{float64(1920)}}},
	}}
	got, err := c.PutActiveConstraints(ctx, "s1", want)
	if err != nil || len(got.ConstraintSets) != 1 {
		t.Fatalf("put = %+v, %v", got, err)
	}
	if d.putType[0] != "application/json" || !bytes.Contains(d.put[0], []byte("frame_width")) {
		t.Errorf("the Device was sent %s as %s", d.put[0], d.putType[0])
	}
	if err := c.DeleteActiveConstraints(ctx, "s1"); err != nil {
		t.Fatal(err)
	}

	edid := []byte{0x00, 0xff, 0x01}
	if err := c.PutBaseEDID(ctx, "in1", edid); err != nil {
		t.Fatal(err)
	}
	if d.putType[1] != "application/octet-stream" || !bytes.Equal(d.put[1], edid) {
		t.Errorf("Base EDID sent as %s: %x", d.putType[1], d.put[1])
	}
	if err := c.DeleteBaseEDID(ctx, "in1"); err != nil {
		t.Fatal(err)
	}
	if len(d.deleted) != 2 {
		t.Errorf("deleted %v", d.deleted)
	}

	// A constraint set the codec refuses never leaves.
	if _, err := c.PutActiveConstraints(ctx, "s1", is11.ActiveConstraints{}); err == nil {
		t.Error("active constraints with no constraint_sets must be refused before they are sent")
	}
	if len(d.put) != 2 {
		t.Errorf("the refused body reached the Device")
	}
}

func TestARefusalCarriesTheDevicesOwnWords(t *testing.T) {
	d := newDevice(t)
	d.refuse = http.StatusBadRequest
	c := d.client(t)
	ctx := context.Background()

	_, err := c.PutActiveConstraints(ctx, "s1", is11.ActiveConstraints{ConstraintSets: []is11.ConstraintSet{}})
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusBadRequest || !strings.Contains(se.Error(), "frame_width 99 is not supported") {
		t.Errorf("refusal = %v", err)
	}
	for name, call := range map[string]func() error{
		"list":      func() error { _, err := c.List(ctx, KindSenders); return err },
		"status":    func() error { _, err := c.Status(ctx, KindSenders, "s1"); return err },
		"input":     func() error { _, err := c.Input(ctx, "in1"); return err },
		"output":    func() error { _, err := c.Output(ctx, "out1"); return err },
		"active":    func() error { _, err := c.ActiveConstraints(ctx, "s1"); return err },
		"supported": func() error { _, err := c.SupportedConstraints(ctx, "s1"); return err },
		"release":   func() error { return c.DeleteActiveConstraints(ctx, "s1") },
		"edid":      func() error { _, err := c.EDID(ctx, KindInputs, "in1", EDIDBase); return err },
		"put edid":  func() error { return c.PutBaseEDID(ctx, "in1", []byte{0}) },
		"del edid":  func() error { return c.DeleteBaseEDID(ctx, "in1") },
	} {
		if err := call(); err == nil {
			t.Errorf("%s: a refused request must fail", name)
		}
	}

	if got := (&StatusError{What: "senders/", Code: 404}).Error(); got != "nmos/streamcompat: senders/: HTTP 404" {
		t.Errorf("bodyless error = %q", got)
	}
	if got := (&StatusError{What: "x", Code: 500, Body: strings.Repeat("x", 400)}).Error(); !strings.HasSuffix(got, "…") {
		t.Errorf("long error not cut: %d bytes", len(got))
	}
}

func TestAnAnswerThatIsNotWhatIS11DefinesIsRefused(t *testing.T) {
	d := newDevice(t)
	c := d.client(t)
	ctx := context.Background()

	d.answers["senders/"] = `{"not":"a list"}`
	if _, err := c.List(ctx, KindSenders); err == nil {
		t.Error("a list that is not an array must be refused")
	}
	d.answers["senders/s1/status/"] = `<html>`
	if _, err := c.Status(ctx, KindSenders, "s1"); err == nil {
		t.Error("a status that is not JSON must be refused")
	}
	d.answers["senders/s1/status/"] = `{"state":"compliant_stream"}` // a receiver's state on a sender
	if _, err := c.Status(ctx, KindSenders, "s1"); err == nil {
		t.Error("a state outside the sender enum must be refused")
	}
	d.answers["senders/s1/constraints/supported/"] = `[]`
	if _, err := c.SupportedConstraints(ctx, "s1"); err == nil {
		t.Error("supported constraints that are not an object must be refused")
	}
}

func TestAClientWithNoDeviceSaysSo(t *testing.T) {
	d := newDevice(t)
	c := d.client(t)
	d.srv.Close()
	if _, err := c.List(context.Background(), KindSenders); err == nil || !strings.Contains(err.Error(), "nmos/streamcompat: senders/") {
		t.Errorf("unreachable Device: %v", err)
	}
	if _, err := c.do(context.Background(), "bad method", "senders/", "", nil); err == nil {
		t.Error("an unbuildable request must fail")
	}
}
