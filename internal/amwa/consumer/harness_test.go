package consumer

// Shared test scaffolding for the Controller's HTTP-driven verbs.
//
// One httptest.Server stands in for BOTH IS-04 faces a routing verb
// touches: the Query API it walks (to discover the owning Device's
// IS-05 endpoint) and the IS-05 Connection API it then drives. A real
// Device serves the two on different ports; folding them onto one test
// server is safe because the code reaches IS-05 only through the href
// IS-04 advertised, which the harness points back at itself.
//
// Resources are built as typed structs and rendered through the codec
// encoders, never hand-written JSON — a payload the decoder would later
// reject cannot silently pass here.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
	v13 "dhs/internal/amwa/codec/is04/v13"
	"dhs/internal/amwa/codec/spec"
	"dhs/internal/amwa/session/query"
)

// catalogue is the set of resources the harness serves under the Query
// API. Each slice is encoded on demand so a test can populate it after
// it knows the server URL (needed to fill a Device's IS-05 control
// href).
type catalogue struct {
	nodes     []is04.Node
	devices   []is04.Device
	sources   []is04.Source
	flows     []is04.Flow
	senders   []is04.Sender
	receivers []is04.Receiver
}

// harness bundles a Controller wired to a live test server with the
// knobs a test sets: the catalogue the Query API serves, the IS-05
// handler its Connection API dispatches to, and the reporter that
// captured every compliance event fired along the way.
type harness struct {
	ctrl        *Controller
	rep         *spec.SliceReporter
	controlHref string
	cat         *catalogue
	is05        http.HandlerFunc
	// failColl, when it matches a Query collection's plural (e.g.
	// "senders"), makes that one collection answer HTTP 500 so a test
	// can drive Walk's per-collection error arm.
	failColl string
}

// newHarness starts the server and returns a harness bound to it. The
// test fills h.cat and h.is05 before calling a verb; h.controlHref is
// the value a Device must advertise so a verb routes back here.
func newHarness(t *testing.T) *harness {
	t.Helper()
	codec := v13.New()
	h := &harness{rep: &spec.SliceReporter{}, cat: &catalogue{}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.Contains(p, "/x-nmos/connection/") {
			if h.is05 != nil {
				h.is05(w, r)
				return
			}
			http.Error(w, "no is05 handler wired", http.StatusNotFound)
			return
		}
		if h.failColl != "" && strings.HasSuffix(p, "/query/v1.3/"+h.failColl) {
			http.Error(w, "collection unavailable", http.StatusInternalServerError)
			return
		}
		switch {
		case strings.HasSuffix(p, "/query/v1.3/nodes"):
			writeNodes(t, w, codec, h.cat.nodes)
		case strings.HasSuffix(p, "/query/v1.3/devices"):
			writeDevices(t, w, codec, h.cat.devices)
		case strings.HasSuffix(p, "/query/v1.3/sources"):
			writeSources(t, w, codec, h.cat.sources)
		case strings.HasSuffix(p, "/query/v1.3/flows"):
			writeFlows(t, w, codec, h.cat.flows)
		case strings.HasSuffix(p, "/query/v1.3/senders"):
			writeSenders(t, w, codec, h.cat.senders)
		case strings.HasSuffix(p, "/query/v1.3/receivers"):
			writeReceivers(t, w, codec, h.cat.receivers)
		default:
			http.Error(w, "unexpected "+p, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	cl, err := query.NewClient(srv.URL, codec)
	if err != nil {
		t.Fatalf("query.NewClient: %v", err)
	}
	h.ctrl = &Controller{reporter: h.rep, client: cl}
	h.controlHref = srv.URL + "/x-nmos/connection/v1.1"
	return h
}

// --- typed fixtures ---------------------------------------------------

const testUUID = "3b8be755-08ff-452b-b217-c9151eb21193"

// senderIDN / receiverIDN yield distinct valid UUIDs so a catalogue can
// hold several resources without colliding on id.
func uuidN(n int) string {
	return "00000000-0000-4000-8000-" + padID(n)
}

func padID(n int) string {
	s := ""
	for i := 0; i < 12; i++ {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

// deviceWith builds a valid v1.3 Device advertising href as its
// sr-ctrl (IS-05) control, unless href is empty (then no control).
func deviceWith(id, href string) is04.Device {
	d := is04.Device{
		ResourceCore: is04.ResourceCore{ID: id, Version: "0:0", Label: "dev", Description: "dev", Tags: map[string][]string{}},
		Type:         "urn:x-nmos:device:generic",
		NodeID:       testUUID,
		Senders:      []string{},
		Receivers:    []string{},
		Controls:     []is04.DeviceControl{},
	}
	if href != "" {
		d.Controls = []is04.DeviceControl{{Href: href, Type: "urn:x-nmos:control:sr-ctrl/v1.1"}}
	}
	return d
}

// senderOn builds a valid v1.3 Sender owned by deviceID on transport.
func senderOn(id, label, deviceID, transport string) is04.Sender {
	mh := "http://h/x-nmos/node/v1.3/senders/" + id + "/manifest"
	flow := testUUID
	return is04.Sender{
		ResourceCore:      is04.ResourceCore{ID: id, Version: "0:0", Label: label, Description: "s", Tags: map[string][]string{}},
		FlowID:            &flow,
		Transport:         transport,
		DeviceID:          deviceID,
		ManifestHref:      &mh,
		InterfaceBindings: []string{"eth0"},
		Subscription:      is04.SenderSubscription{Active: false},
	}
}

// receiverOn builds a valid v1.3 Receiver owned by deviceID on transport.
func receiverOn(id, deviceID, transport string) is04.Receiver {
	return is04.Receiver{
		ResourceCore:      is04.ResourceCore{ID: id, Version: "0:0", Label: "rcv", Description: "r", Tags: map[string][]string{}},
		DeviceID:          deviceID,
		Transport:         transport,
		InterfaceBindings: []string{"eth0"},
		Format:            is04.FormatVideo,
		Caps:              is04.ReceiverCaps{MediaTypes: []string{"video/raw"}},
		Subscription:      is04.ReceiverSubscription{Active: false},
	}
}

// --- list encoders ----------------------------------------------------

func writeJSONArray(w http.ResponseWriter, elems [][]byte) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("["))
	for i, e := range elems {
		if i > 0 {
			_, _ = w.Write([]byte(","))
		}
		_, _ = w.Write(e)
	}
	_, _ = w.Write([]byte("]"))
}

func writeNodes(t *testing.T, w http.ResponseWriter, c is04.Codec, xs []is04.Node) {
	t.Helper()
	out := make([][]byte, len(xs))
	for i := range xs {
		b, err := c.EncodeNode(xs[i])
		if err != nil {
			t.Fatalf("encode node: %v", err)
		}
		out[i] = b
	}
	writeJSONArray(w, out)
}

func writeDevices(t *testing.T, w http.ResponseWriter, c is04.Codec, xs []is04.Device) {
	t.Helper()
	out := make([][]byte, len(xs))
	for i := range xs {
		b, err := c.EncodeDevice(xs[i])
		if err != nil {
			t.Fatalf("encode device: %v", err)
		}
		out[i] = b
	}
	writeJSONArray(w, out)
}

func writeSources(t *testing.T, w http.ResponseWriter, c is04.Codec, xs []is04.Source) {
	t.Helper()
	out := make([][]byte, len(xs))
	for i := range xs {
		b, err := c.EncodeSource(xs[i])
		if err != nil {
			t.Fatalf("encode source: %v", err)
		}
		out[i] = b
	}
	writeJSONArray(w, out)
}

func writeFlows(t *testing.T, w http.ResponseWriter, c is04.Codec, xs []is04.Flow) {
	t.Helper()
	out := make([][]byte, len(xs))
	for i := range xs {
		b, err := c.EncodeFlow(xs[i])
		if err != nil {
			t.Fatalf("encode flow: %v", err)
		}
		out[i] = b
	}
	writeJSONArray(w, out)
}

func writeSenders(t *testing.T, w http.ResponseWriter, c is04.Codec, xs []is04.Sender) {
	t.Helper()
	out := make([][]byte, len(xs))
	for i := range xs {
		b, err := c.EncodeSender(xs[i])
		if err != nil {
			t.Fatalf("encode sender: %v", err)
		}
		out[i] = b
	}
	writeJSONArray(w, out)
}

func writeReceivers(t *testing.T, w http.ResponseWriter, c is04.Codec, xs []is04.Receiver) {
	t.Helper()
	out := make([][]byte, len(xs))
	for i := range xs {
		b, err := c.EncodeReceiver(xs[i])
		if err != nil {
			t.Fatalf("encode receiver: %v", err)
		}
		out[i] = b
	}
	writeJSONArray(w, out)
}
