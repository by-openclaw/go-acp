package consumer

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"dhs/internal/clock"
	dhsc "dhs/internal/consumer"
	"dhs/internal/plugin"
)

// The neutral face, driven against a Neuron that answers the way the
// real one does — including the two shapes that caught this connector
// out on the live BRIDGE:
//
//   - a collection that returns the FULL body of every member, beside
//     a member resource that returns one of them again. Reading both
//     puts every field in the model twice, once with the collection's
//     access bits and once with the member's;
//   - a field carrying binary (the real device puts a JPEG thumbnail
//     in every video channel), which is a picture of the video and not
//     a property of the device.

const fakeAPIYML = `openapi: '3.1.2'
paths:
  /v1/self:
    get:
      operationId: 'GetSelf'
  /v1/misc/reference:
    get:
      operationId: 'GetReference'
    put:
      operationId: 'SetReference'
  /v1/misc/reference/status:
    get:
      operationId: 'GetReferenceStatus'
  /v1/misc/luts:
    get:
      operationId: 'GetLuts'
  /v1/processing/video/channels:
    get:
      operationId: 'GetChannels'
  /v1/processing/video/channels/{uuid}:
    get:
      operationId: 'GetChannel'
    put:
      operationId: 'SetChannel'
  /v1/processing/video/channels/{id}:
    get:
      operationId: 'GetChannelById'
    put:
      operationId: 'SetChannelById'
  /v1/io/ip/senders/video:
    get:
      operationId: 'GetVideoSenders'
  /v1/io/ip/receivers/video:
    get:
      operationId: 'GetVideoReceivers'
  /v1/gone:
    get:
      operationId: 'GetSomethingNotFitted'
`

// channelBody is one video channel, with a binary thumbnail like the
// real device's.
const channelBody = `{"channel":"B2","uuid":"c-1","inputSelection":"Main","lock":true,` +
	`"delay":{"frameDelay":0,"ioDelay":19.25},"thumbnail":"ÿØÿàBINARY\u0000JPEG"}`

func fakeNeuron(t *testing.T) *httptest.Server {
	t.Helper()
	routes := map[string]string{
		"/self":                          `{"app":{"productName":"BRIDGE","productVersion":"7.0.3","modelVersion":17}}`,
		"/docs/api.yml":                  fakeAPIYML,
		"/misc/reference":                `{"ptp":{"domain":77,"priority1":248}}`,
		"/misc/reference/status":         `{"media":{"refLocked":true}}`,
		"/misc/luts":                     `{"lut_3d":[],"upload_status":""}`,
		"/processing/video/channels":     `[` + channelBody + `]`,
		"/processing/video/channels/c-1": channelBody,
		"/io/ip/senders/video":           `[{"uuid":"s-1","name":"tx","enabled":true}]`,
		"/io/ip/receivers/video":         `[]`,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			// /gone is declared by the spec and not served — an option
			// this build does not have.
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// connectedPlugin points a plugin at the fake device.
func connectedPlugin(t *testing.T) *Plugin {
	t.Helper()
	srv := fakeNeuron(t)
	host, portStr, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	// The fake speaks http and the client derives an https base, so
	// the connector is pointed at the test server for this test.
	restore := dialClient
	dialClient = func(string) *Client { return testClient(srv) }
	t.Cleanup(func() { dialClient = restore })

	f := &Factory{}
	p, ok := f.New(plugin.Deps{Logger: quiet(), Clock: clock.System()}).(*Plugin)
	if !ok {
		t.Fatal("the factory must build a *Plugin")
	}
	if err := p.Connect(context.Background(), host, port); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = p.Disconnect() })
	return p
}

func TestFactoryRegistersTheProtocol(t *testing.T) {
	f := &Factory{}
	if m := f.Meta(); m.Name != Name || m.DefaultPort != DefaultPort || m.Description == "" {
		t.Errorf("meta = %+v", m)
	}
	if _, err := dhsc.Get(Name); err != nil {
		t.Errorf("%s is not in the registry: %v", Name, err)
	}
}

func TestTheDeviceAnswersTheNeutralVerbs(t *testing.T) {
	p := connectedPlugin(t)
	ctx := context.Background()

	info, err := p.GetDeviceInfo(ctx)
	if err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}
	if info.NumSlots != 1 || info.DtdVersion != "7.0.3" {
		t.Errorf("info = %+v", info)
	}

	si, err := p.GetSlotInfo(ctx, 0)
	if err != nil || si.Status != dhsc.SlotPresent || !si.IsOnline {
		t.Fatalf("slot 0 = %+v, %v", si, err)
	}
	if si, _ := p.GetSlotInfo(ctx, 3); si.Status != dhsc.SlotNoCard {
		t.Errorf("slot 3 = %+v — a bridge is one box", si)
	}

	id, err := p.IdentityProbe(ctx, 0)
	if err != nil || id != "BRIDGE@7.0.3" {
		t.Errorf("identity = %q, %v", id, err)
	}
	if !p.PathNative() {
		t.Error("a REST path IS a path — no walk should be needed to resolve one")
	}
	if p.MinOpTimeout() < time.Minute {
		t.Error("a walk here is hundreds of round trips; the floor must reflect that")
	}
}

func TestTheModelComesFromTheSpecNotTheTreeShape(t *testing.T) {
	p := connectedPlugin(t)
	objs, err := p.Walk(context.Background(), 0)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	byPath := map[string]dhsc.Object{}
	for _, o := range objs {
		byPath[strings.Join(o.Path, ".")] = o
	}

	// /misc/luts is declared by the spec and listed by no parent — the
	// exact resource a tree walk cannot find.
	if _, ok := byPath["misc.luts.upload_status"]; !ok {
		t.Error("a resource only the spec declares must still be in the model")
	}
	// A sub-resource under an object, which a walk also stops short of.
	if _, ok := byPath["misc.reference.status.media.refLocked"]; !ok {
		t.Error("a /status sub-resource must be in the model")
	}
	// A parameterised member, expanded with the id the device returned.
	if _, ok := byPath["processing.video.channels.c-1.inputSelection"]; !ok {
		t.Errorf("the member resource is missing: %v", keysWithPrefix(byPath, "processing"))
	}
	// A declared resource this build does not serve is skipped, not fatal.
	if _, ok := byPath["gone"]; ok {
		t.Error("a 404 must not become an object")
	}
	if slot1, err := p.Walk(context.Background(), 1); err != nil || slot1 != nil {
		t.Errorf("slot 1 = %v, %v — a bridge has one slot", slot1, err)
	}
}

func TestAccessBitsComeFromTheSpecsPUTs(t *testing.T) {
	// Nothing in a GET response says what may be written. An operator
	// who cannot tell a setting from a reading will try to write a
	// reading.
	p := connectedPlugin(t)
	objs, err := p.Walk(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]dhsc.Object{}
	for _, o := range objs {
		byPath[strings.Join(o.Path, ".")] = o
	}

	writable, ok := byPath["misc.reference.ptp.domain"]
	if !ok {
		t.Fatal("misc.reference.ptp.domain missing")
	}
	if writable.Access&accessWrite == 0 {
		t.Error("/misc/reference declares PUT — its fields are writable")
	}
	readOnly, ok := byPath["misc.reference.status.media.refLocked"]
	if !ok {
		t.Fatal("the status object is missing")
	}
	if readOnly.Access&accessWrite != 0 {
		t.Error("/misc/reference/status declares no PUT — it is a reading")
	}
}

func TestACollectionAndItsMembersDoNotBothBecomeObjects(t *testing.T) {
	// The bug this caught on the live BRIDGE: the collection returns
	// every channel in full and the member returns one of them again,
	// so every field appeared twice — once read-only (the collection
	// has no PUT) and once writable (the member does).
	p := connectedPlugin(t)
	objs, err := p.Walk(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, o := range objs {
		seen[strings.Join(o.Path, ".")]++
	}
	for path, n := range seen {
		if n > 1 {
			t.Errorf("%s is in the model %d times", path, n)
		}
	}
	// And the surviving copy is the member's, with the member's access.
	o, ok := seen["processing.video.channels.c-1.channel"]
	if !ok || o != 1 {
		t.Fatalf("the member's objects are missing (%d)", o)
	}
}

func TestBinaryValuesAreDescribedRatherThanStored(t *testing.T) {
	// The real device answers every video channel with a JPEG
	// thumbnail. A device model is what an operator reads and a
	// firmware diff compares; tens of kilobytes of binary per channel
	// is neither.
	p := connectedPlugin(t)
	objs, err := p.Walk(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range objs {
		if strings.HasSuffix(strings.Join(o.Path, "."), "thumbnail") {
			if !strings.HasPrefix(o.Value.Str, "<binary,") {
				t.Errorf("thumbnail kept verbatim: %q", o.Value.Str)
			}
			if o.MaxLen == 0 {
				t.Error("the size must survive even when the bytes do not")
			}
			return
		}
	}
	t.Error("no thumbnail in the model — the fixture should have one")
}

func keysWithPrefix(m map[string]dhsc.Object, prefix string) []string {
	var out []string
	for k := range m {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out
}

// quiet is a logger that says nothing, for tests that assert on
// behaviour rather than on output.
func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestGetValueResolvesAPathWithoutWalking(t *testing.T) {
	// PathNative's promise: `get --path` on a cold connection is one
	// round trip, because the spec already says which prefix is the
	// resource. No walk has run in this test.
	p := connectedPlugin(t)
	ctx := context.Background()

	v, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "misc.reference.ptp.domain"})
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if v.Kind != dhsc.KindInt || v.Int != 77 {
		t.Errorf("value = %+v", v)
	}
	// The REST spelling works too — an operator types what they see.
	if v2, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "/misc/reference/ptp/domain"}); err != nil || v2.Int != 77 {
		t.Errorf("slashed path = %+v, %v", v2, err)
	}
	// A deeper resource wins over a shallower one, so a field of
	// /misc/reference/status is not looked for in /misc/reference.
	if v3, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "misc.reference.status.media.refLocked"}); err != nil ||
		v3.Kind != dhsc.KindBool || !v3.Bool {
		t.Errorf("status field = %+v, %v", v3, err)
	}
}

func TestGetValueSaysWhichHalfOfThePathIsWrong(t *testing.T) {
	p := connectedPlugin(t)
	ctx := context.Background()

	cases := []struct{ path, want string }{
		{"", "no path"},
		{"misc.nonsense.field", "no resource"},
		{"misc.reference.nosuchfield", "has no field"},
		{"misc.reference", "is a resource, not a value"},
	}
	for _, c := range cases {
		_, err := p.GetValue(ctx, dhsc.ValueRequest{Path: c.path})
		if err == nil {
			t.Errorf("GetValue(%q) must fail", c.path)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("GetValue(%q) = %v, want it to say %q", c.path, err, c.want)
		}
	}
}

func TestSetSaysWhetherTheDeviceWouldAcceptItAtAll(t *testing.T) {
	// The connector does not write yet, and the two refusals are
	// different answers: "this device will never accept that" is a
	// fact from its own api.yml, and an operator should not have to
	// try it to find out.
	p := connectedPlugin(t)
	ctx := context.Background()

	_, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "misc.reference.status.media.refLocked"},
		dhsc.Value{Kind: dhsc.KindBool, Bool: false})
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Errorf("a status must be refused as read-only: %v", err)
	}
	_, err = p.SetValue(ctx, dhsc.ValueRequest{Path: "misc.reference.ptp.domain"},
		dhsc.Value{Kind: dhsc.KindInt, Int: 1})
	if err == nil || !strings.Contains(err.Error(), "does not write yet") {
		t.Errorf("a writable object must say the verb is missing, not that the object is: %v", err)
	}
	if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "no.such.thing"}, dhsc.Value{}); err == nil {
		t.Error("an unknown path must fail before anything else")
	}
}

func TestTheWatchPollsTheModelItWalked(t *testing.T) {
	p := connectedPlugin(t)
	ctx := context.Background()

	p.SetPollInterval(0) // ignored: a cadence has to be positive
	p.SetPollInterval(2 * time.Second)

	prof, err := p.pollProfileFor(ctx, dhsc.ValueRequest{Path: "misc.reference"})
	if err != nil {
		t.Fatalf("pollProfileFor: %v", err)
	}
	if len(prof.Entries) == 0 {
		t.Fatal("the scope matched nothing")
	}
	for _, e := range prof.Entries {
		if !strings.HasPrefix(e.Path, "misc.reference") {
			t.Errorf("%s is outside the scope", e.Path)
		}
		if time.Duration(e.Interval) != 2*time.Second {
			t.Errorf("interval = %v", time.Duration(e.Interval))
		}
	}
	// A scope that matches nothing says so, with what it did have.
	if _, err := p.pollProfileFor(ctx, dhsc.ValueRequest{Path: "nothing.here"}); err == nil ||
		!strings.Contains(err.Error(), "nothing to poll") {
		t.Errorf("= %v", err)
	}
	// Subscribe/Unsubscribe go through the shared poller.
	req := dhsc.ValueRequest{Path: "misc.reference"}
	if err := p.Subscribe(req, func(dhsc.Event) {}); err != nil {
		t.Errorf("Subscribe: %v", err)
	}
	if err := p.Unsubscribe(req); err != nil {
		t.Errorf("Unsubscribe: %v", err)
	}
}

func TestEveryVerbRefusesBeforeConnect(t *testing.T) {
	// A connector that answered from an empty model would report a
	// device as healthy that nobody has talked to.
	f := &Factory{}
	p, _ := f.New(plugin.Deps{Logger: quiet(), Clock: clock.System()}).(*Plugin)
	ctx := context.Background()

	if _, err := p.GetDeviceInfo(ctx); err == nil {
		t.Error("GetDeviceInfo")
	}
	if _, err := p.GetSlotInfo(ctx, 0); err == nil {
		t.Error("GetSlotInfo")
	}
	if _, err := p.IdentityProbe(ctx, 0); err == nil {
		t.Error("IdentityProbe")
	}
	if _, err := p.Walk(ctx, 0); err == nil {
		t.Error("Walk")
	}
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "misc.reference"}); err == nil {
		t.Error("GetValue")
	}
	if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "misc.reference"}, dhsc.Value{}); err == nil {
		t.Error("SetValue")
	}
	// And Disconnect on a connector that never connected is not an
	// error — stopping something that is not running is done.
	if err := p.Disconnect(); err != nil {
		t.Errorf("Disconnect: %v", err)
	}
}

func TestConnectRefusesADeviceThatCannotDescribeItself(t *testing.T) {
	// The spec is fetched AT connect and not on demand, so a device
	// that cannot produce one fails where an operator is looking
	// rather than inside a walk an hour later.
	routes := map[string]string{
		"/self": `{"app":{"productName":"BRIDGE","productVersion":"7.0.3"}}`,
		// no /docs/api.yml
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	restore := dialClient
	dialClient = func(string) *Client { return testClient(srv) }
	defer func() { dialClient = restore }()

	f := &Factory{}
	p, _ := f.New(plugin.Deps{Logger: quiet(), Clock: clock.System()}).(*Plugin)
	err := p.Connect(context.Background(), "127.0.0.1", 0)
	if err == nil || !strings.Contains(err.Error(), "api.yml") {
		t.Errorf("= %v, want the missing spec named", err)
	}

	// And one that serves a document with no paths in it.
	routes["/docs/api.yml"] = "openapi: '3.1.2'\ninfo:\n  title: 'x'\n"
	if err := p.Connect(context.Background(), "127.0.0.1", 0); err == nil {
		t.Error("a document with no paths must not produce a session")
	}

	// A device that does not answer /self at all is refused first.
	delete(routes, "/self")
	if err := p.Connect(context.Background(), "127.0.0.1", 0); err == nil {
		t.Error("a device that does not answer /self is not a device")
	}
}

func TestIdentityNeedsBothHalves(t *testing.T) {
	// Model@SwRev is what a DM and an alarm template are keyed by
	// (ADR-0022). Half of it is not an identity.
	routes := map[string]string{
		"/self":         `{"app":{"productName":"BRIDGE"}}`, // no version
		"/docs/api.yml": fakeAPIYML,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	restore := dialClient
	dialClient = func(string) *Client { return testClient(srv) }
	defer func() { dialClient = restore }()

	f := &Factory{}
	p, _ := f.New(plugin.Deps{Logger: quiet(), Clock: clock.System()}).(*Plugin)
	if err := p.Connect(context.Background(), "127.0.0.1", 0); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := p.IdentityProbe(context.Background(), 0); err == nil {
		t.Error("a device that names no firmware has no identity")
	}
}

// newFirmwareNeuron serves the API WITHOUT the version segment, the
// way the firmware on the shuffler does: /api/self, /api/docs/api.yml.
// The lab BRIDGE (7.0.3) serves /api/v1. One connector, both fleets.
func newFirmwareNeuron(t *testing.T) *httptest.Server {
	t.Helper()
	routes := map[string]string{
		"/api/self":                      `{"app":{"productName":"SHUFFLER","productVersion":"8.0.0"}}`,
		"/api/docs/api.yml":              fakeAPIYML,
		"/api/misc/reference":            `{"ptp":{"domain":42}}`,
		"/api/misc/luts":                 `{"upload_status":""}`,
		"/api/io/ip/senders/video":       `[]`,
		"/api/io/ip/receivers/video":     `[]`,
		"/api/processing/video/channels": `[]`,
		"/api/misc/reference/status":     `{"media":{"refLocked":false}}`,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestTheConnectorFindsTheBaseTheFirmwareServes(t *testing.T) {
	srv := newFirmwareNeuron(t)
	hostport := strings.TrimPrefix(srv.URL, "http://")

	// Built the way production builds it, except for the scheme: the
	// candidate list is what is under test, not TLS.
	c := &Client{host: hostport, http: testClient(srv).http, base: "http://" + hostport + APIBases[0]}
	if err := c.Resolve(context.Background()); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got, want := c.Base(), "http://"+hostport+"/api"; got != want {
		t.Errorf("base = %q, want %q — the version segment is gone on this firmware", got, want)
	}

	// And a device that answers neither says which ones were tried.
	dead := &Client{host: "127.0.0.1:1", http: testClient(srv).http,
		base: "http://127.0.0.1:1" + APIBases[0]}
	err := dead.Resolve(context.Background())
	if err == nil {
		t.Fatal("a device that answers nothing must not resolve")
	}
	for _, want := range []string{"/api/v1", "/api", "--api-base"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to mention %q", err, want)
		}
	}
}

func TestAnExplicitBaseIsNotProbed(t *testing.T) {
	// A deployment behind a proxy prefix is one no probe would guess,
	// so a named base is taken as given and costs no round trip.
	c := New(Options{Host: "neuron.invalid", APIBase: "/gateway/neuron/api/"})
	want := "https://neuron.invalid/gateway/neuron/api"
	if got := c.Base(); got != want {
		t.Errorf("base = %q, want %q (trailing slash trimmed)", got, want)
	}
	// Resolve is a no-op on it: no candidate list, no request.
	if err := c.Resolve(context.Background()); err != nil {
		t.Errorf("Resolve on an explicit base = %v", err)
	}
	if c.Base() != want {
		t.Errorf("Resolve changed an explicit base to %q", c.Base())
	}
}

func TestABaseTypedWithoutItsLeadingSlashStillWorks(t *testing.T) {
	// What somebody types when reading the URL off a browser. Without
	// the slash the host and the path run together and the error is
	// about DNS, which sends an operator looking in the wrong place.
	for _, in := range []string{"api", "/api", "api/", "  /api/  "} {
		c := New(Options{Host: "neuron.invalid", APIBase: in})
		if got, want := c.Base(), "https://neuron.invalid/api"; got != want {
			t.Errorf("APIBase(%q) -> %q, want %q", in, got, want)
		}
	}
	// And an empty one is still empty: that means "ask the device",
	// not "the root".
	c := New(Options{Host: "neuron.invalid", APIBase: "   "})
	if got := c.Base(); got != "https://neuron.invalid"+APIBases[0] {
		t.Errorf("empty base -> %q, want the first candidate", got)
	}
	if c.resolved {
		t.Error("an empty base must stay unresolved so Resolve probes")
	}
}
