package consumer

// The Neuron REST client, against a device that answers, one that
// answers with less than the whole tree, and one that answers with
// something a decoder cannot read. The rule throughout is the repo's:
// a deviation is recorded and the walk continues, because a partial
// Neuron is still worth what it did say.

import (
	"context"
	"crypto/tls"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dhs/internal/ccm/codec"
	"dhs/internal/transport"
)

// neuron is a fake Neuron REST API: each path answers with whatever
// the test put there, and a path with nothing behind it 404s the way
// a firmware without that tree does.
type neuron struct {
	ts    *httptest.Server
	paths map[string]string
	hits  map[string]int
}

func newNeuron(t *testing.T) *neuron {
	t.Helper()
	n := &neuron{paths: map[string]string{}, hits: map[string]int{}}
	n.ts = httptest.NewTLSServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		n.hits[r.URL.Path]++
		body, ok := n.paths[r.URL.Path]
		if !ok {
			stdhttp.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(n.ts.Close)
	return n
}

// host is the address the client should be pointed at.
func (n *neuron) host() string { return strings.TrimPrefix(n.ts.URL, "https://") }

// clientFor points a client at the fake device. The fake serves a
// self-signed certificate, which is exactly what the real one does.
func clientFor(t *testing.T, n *neuron) *Client {
	t.Helper()
	return New(Options{Host: n.host()})
}

// A device that serves the whole tree yields its identity and every
// stream, keyed by the UUID that lines up with the plant's NMOS
// registry and the ACP2 view of the same box.
func TestWalkReadsTheWholeTree(t *testing.T) {
	n := newNeuron(t)
	n.paths["/api/v1/self"] = `{"app":{"productName":"Neuron","productVersion":"6.7.4","modelVersion":3}}`
	n.paths["/api/v1/io/ip/senders/video"] = `[
		{"uuid":"11111111-1111-1111-1111-111111111111","name":"cam-1","enable":true,
		 "legs":[{"ip":"239.1.1.1","port":5004,"mac":"leg-a"}],
		 "nmos":{"groupHint":"cam-1:video","label":"Camera 1"}}
	]`
	n.paths["/api/v1/io/ip/senders/audio"] = `[]`
	n.paths["/api/v1/io/ip/senders/data"] = `[]`
	n.paths["/api/v1/io/ip/receivers/video"] = `[
		{"uuid":"22222222-2222-2222-2222-222222222222","name":"mon-1","enable":false,"nmos":{}}
	]`
	n.paths["/api/v1/io/ip/receivers/audio"] = `[]`
	n.paths["/api/v1/io/ip/receivers/data"] = `[]`

	dev, deviations, err := clientFor(t, n).Walk(context.Background())
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(deviations) != 0 {
		t.Errorf("a device serving the whole tree deviates in nothing: %v", deviations)
	}
	if dev.ProductName != "Neuron" || dev.ModelVersion != 3 {
		t.Errorf("identity = %+v", dev)
	}
	if len(dev.Streams) != 2 {
		t.Fatalf("streams = %+v, want the sender and the receiver", dev.Streams)
	}
	s, ok := dev.Stream("11111111-1111-1111-1111-111111111111")
	if !ok {
		t.Fatal("the sender must be keyed by its UUID")
	}
	if s.Kind != codec.KindSender || s.Essence != codec.EssenceVideo {
		t.Errorf("the stream must carry the tree it came from: %+v", s)
	}
	if len(s.Legs) != 1 || s.Legs[0].StreamID != "leg-a" {
		t.Errorf("legs = %+v", s.Legs)
	}
}

// A tree the firmware does not serve is recorded and the walk
// continues: a partial Neuron still yields what it has, and an
// operator reading the deviations knows exactly which tree was
// missing.
func TestWalkRecordsTreesTheDeviceDoesNotServe(t *testing.T) {
	n := newNeuron(t)
	n.paths["/api/v1/self"] = `{"app":{"productName":"Neuron"}}`
	n.paths["/api/v1/io/ip/senders/video"] = `[
		{"uuid":"11111111-1111-1111-1111-111111111111","name":"cam-1","nmos":{}}
	]`
	// Every other tree 404s.

	dev, deviations, err := clientFor(t, n).Walk(context.Background())
	if err != nil {
		t.Fatalf("a partial device must still walk: %v", err)
	}
	if len(dev.Streams) != 1 {
		t.Errorf("streams = %+v, want the one tree that answered", dev.Streams)
	}
	if len(deviations) != 5 {
		t.Fatalf("deviations = %v, want one per tree that did not answer", deviations)
	}
	for _, d := range deviations {
		if !strings.Contains(d, "/io/ip/") {
			t.Errorf("a deviation must name the path: %q", d)
		}
	}
}

// A tree that answers with something that is not a stream array is a
// deviation too, not a failed walk.
func TestWalkRecordsATreeItCannotDecode(t *testing.T) {
	n := newNeuron(t)
	n.paths["/api/v1/self"] = `{"app":{"productName":"Neuron"}}`
	n.paths["/api/v1/io/ip/senders/video"] = `{"not":"an array"}`
	n.paths["/api/v1/io/ip/senders/audio"] = `[]`
	n.paths["/api/v1/io/ip/senders/data"] = `[]`
	n.paths["/api/v1/io/ip/receivers/video"] = `[]`
	n.paths["/api/v1/io/ip/receivers/audio"] = `[]`
	n.paths["/api/v1/io/ip/receivers/data"] = `[]`

	_, deviations, err := clientFor(t, n).Walk(context.Background())
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(deviations) != 1 || !strings.Contains(deviations[0], "decode") {
		t.Fatalf("deviations = %v, want the undecodable tree reported", deviations)
	}
}

// A stream with no UUID cannot be keyed, and a device that reports one
// is telling us about a resource nothing else in the plant can refer
// to. It is skipped with the reason attached rather than dropped.
func TestWalkReportsAStreamWithNoUUID(t *testing.T) {
	n := newNeuron(t)
	n.paths["/api/v1/self"] = `{"app":{"productName":"Neuron"}}`
	n.paths["/api/v1/io/ip/senders/video"] = `[
		{"name":"nameless","nmos":{}},
		{"uuid":"11111111-1111-1111-1111-111111111111","name":"cam-1","nmos":{}}
	]`
	for _, p := range []string{"senders/audio", "senders/data", "receivers/video", "receivers/audio", "receivers/data"} {
		n.paths["/api/v1/io/ip/"+p] = `[]`
	}

	dev, deviations, err := clientFor(t, n).Walk(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(dev.Streams) != 1 {
		t.Errorf("streams = %+v, want only the one that can be keyed", dev.Streams)
	}
	if len(deviations) != 1 || !strings.Contains(deviations[0], "no uuid") {
		t.Fatalf("deviations = %v, want the nameless stream reported", deviations)
	}
}

// A device that will not answer /self at all cannot be walked: there
// is no identity to hang the streams on, and a Device with none would
// be indistinguishable from a device nobody asked about.
func TestWalkRefusesADeviceWithNoIdentity(t *testing.T) {
	n := newNeuron(t) // nothing served at all

	if _, _, err := clientFor(t, n).Walk(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "/self") {
		t.Fatalf("= %v, want the /self failure reported", err)
	}

	n.paths["/api/v1/self"] = `{`
	if _, _, err := clientFor(t, n).Walk(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "self") {
		t.Fatalf("a /self that will not decode = %v", err)
	}
}

// The OpenAPI document is the device's own statement of its contract,
// and the artifact to diff across firmware upgrades.
func TestFetchSpec(t *testing.T) {
	n := newNeuron(t)
	n.paths["/api/v1/docs/api.yml"] = "openapi: 3.1.0\n"

	got, err := clientFor(t, n).FetchSpec(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), "openapi:") {
		t.Errorf("= %q", got)
	}

	// A device that does not publish one says so rather than handing
	// back an empty contract.
	delete(n.paths, "/api/v1/docs/api.yml")
	if _, err := clientFor(t, n).FetchSpec(context.Background()); err == nil {
		t.Error("a device with no spec must be reported")
	}
}

// The default posture is skip-verify, because the media-plane device
// is self-signed by design. VerifyTLS opts back in, and then the same
// device is refused.
func TestTLSPosture(t *testing.T) {
	n := newNeuron(t)
	n.paths["/api/v1/self"] = `{"app":{"productName":"Neuron"}}`

	verifying := New(Options{Host: n.host(), VerifyTLS: true})
	if _, _, err := verifying.Walk(context.Background()); err == nil {
		t.Error("a verifying client must refuse a self-signed device")
	}
}

// A timeout the caller did not set is one the client picks: a Neuron
// that accepts the connection and then says nothing must not hold a
// walk open forever.
func TestDefaultTimeout(t *testing.T) {
	c := New(Options{Host: "neuron.invalid"})
	if got := c.http.HTTP.Timeout; got != 8*time.Second {
		t.Errorf("= %v, want the default", got)
	}
	if got := New(Options{Host: "neuron.invalid", Timeout: time.Second}).http.HTTP.Timeout; got != time.Second {
		t.Errorf("= %v, want the caller's", got)
	}
}

// A TLS posture that could not be built leaves net/http to apply its
// own defaults, which verify — the safe answer if this client ever
// grows a configuration that can fail.
func TestTLSPostureThatCannotBeBuilt(t *testing.T) {
	prev := tlsClientConfig
	tlsClientConfig = func(transport.TLSOptions) (*tls.Config, error) {
		return nil, errors.New("refused")
	}
	t.Cleanup(func() { tlsClientConfig = prev })

	c := New(Options{Host: "neuron.invalid"})
	tr, ok := c.http.HTTP.Transport.(*stdhttp.Transport)
	if !ok {
		t.Fatalf("transport = %T", c.http.HTTP.Transport)
	}
	if tr.TLSClientConfig != nil {
		t.Error("a posture that could not be built must not be installed")
	}
}
