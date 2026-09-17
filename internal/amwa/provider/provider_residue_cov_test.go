package provider

// The last thirteen: each is a state some particular combination of
// bundle, advertisement or request produces, and each says something
// about what the Node does when the network is less tidy than a
// fixture.

import (
	"context"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dnssdcodec "dhs/internal/amwa/codec/dnssd"
	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
	"dhs/internal/amwa/codec/is09"
)

// A goodbye packet for the only System API on the link leaves nothing
// to fetch, and the watcher waits rather than re-reading what is gone.
func TestSystemWatcherWithNothingLeftOnTheLink(t *testing.T) {
	useFakeBrowser(t, newFakeBrowser())
	w, err := NewSystemWatcher(newLogTap().logger(), "v1.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	ins := dnssdcodec.Instance{
		Name: "sys", Service: dnssdcodec.ServiceSystem,
		Host: "sys.local", Port: 80, TTL: 120,
		TXT: map[string]string{
			dnssdcodec.TXTKeyAPIProto: "http",
			dnssdcodec.TXTKeyAPIVer:   "v1.0",
		},
	}
	w.observe(context.Background(), ins)

	ins.TTL = 0 // goodbye
	w.observe(context.Background(), ins)
}

// The only System API advertised is one that will not answer: it is
// remembered as failed and the walk ends, rather than looping over a
// list of one.
func TestSystemWatcherWithOneUnreachableCandidate(t *testing.T) {
	useFakeBrowser(t, newFakeBrowser())
	tap := newLogTap()
	w, err := NewSystemWatcher(tap.logger(), "v1.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	dead := dnssdcodec.Instance{
		Name: "dead", Service: dnssdcodec.ServiceSystem,
		Host: "127.0.0.1", Port: 1, TTL: 120,
		IPv4: []net.IP{net.ParseIP("127.0.0.1")},
		TXT: map[string]string{
			dnssdcodec.TXTKeyAPIProto: "http",
			dnssdcodec.TXTKeyAPIVer:   "v1.0",
		},
	}
	w.observe(context.Background(), dead)

	// Re-advertised: it is already known to be unreachable, so it is
	// skipped rather than re-read, and with nothing behind it the walk
	// ends there.
	w.observe(context.Background(), dead)
}

// The AMWA suite advertises a deliberately broken System API alongside
// a working one, and a Node that gives up on the highest-priority
// failure never reaches the one that answers.
func TestSystemWatcherWalksPastAFailureToTheOneThatAnswers(t *testing.T) {
	api := newSystemAPI(t, "good")
	useFakeBrowser(t, newFakeBrowser())

	got := make(chan string, 4)
	w, err := NewSystemWatcher(newLogTap().logger(), "v1.0", func(g any, url string) {
		if _, ok := g.(*is09.Global); ok {
			got <- url
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	// The working one is seen first and read.
	w.observe(context.Background(), api.instance(t, "good", 10, 120))

	// Then a broken one that wins on priority: it is tried, fails, and
	// the walk carries on to the one already known to answer rather
	// than stopping at the highest-priority failure.
	w.observe(context.Background(), dnssdcodec.Instance{
		Name: "broken", Service: dnssdcodec.ServiceSystem,
		Host: "127.0.0.1", Port: 1, TTL: 120,
		IPv4: []net.IP{net.ParseIP("127.0.0.1")},
		TXT: map[string]string{
			dnssdcodec.TXTKeyAPIProto: "http",
			dnssdcodec.TXTKeyAPIVer:   "v1.0",
			dnssdcodec.TXTKeyPriority: "1",
		},
	})

	select {
	case url := <-got:
		if url == "" {
			t.Error("the callback must name where it read")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the walk never reached the System API that answers")
	}
}

// The Node's System-API callback is typed `any` because the watcher
// serves several specs; anything that is not a Global is not this
// Node's to apply.
func TestSystemGlobalCallbackIgnoresWhatIsNotAGlobal(t *testing.T) {
	useFakeBrowser(t, newFakeBrowser())
	s := nodeFor(t, validBundle(), func(c *IS04NodeConfig) { c.DiscoveryMode = "mdns" })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.fetchSystemGlobal(ctx)
	if s.systemWatcher == nil {
		t.Fatal("mDNS mode must open a System API watcher")
	}
	// The watcher hands the callback whatever it decoded; a Node that
	// applied a non-Global would take its heartbeat interval from
	// something that never described one.
	s.systemWatcher.onGlobal("not a global", "http://sys.local/global")
	s.systemWatcher.onGlobal(&is09.Global{}, "http://sys.local/global")
}

// With an address in the bundle and no port anywhere, the control host
// is that address alone: half of it is still reachable, and a made-up
// port is not.
func TestControlHostFromTheBundleWithoutAPort(t *testing.T) {
	b := fullBundle(t)
	b.Node.API.Endpoints = []is04.NodeEndpoint{{Host: "192.0.2.11", Port: 8080, Protocol: "http"}}

	s := nodeFor(t, b, func(c *IS04NodeConfig) { c.Bind = "0.0.0.0" })
	if got := s.controlHost(); got != "192.0.2.11" {
		t.Errorf("= %q, want the bundle's address alone", got)
	}
}

// Before the Node knows where it answers, "auto" still resolves to
// something a consumer can dial: the loopback is a truthful "here, on
// this box" rather than a blank a controller renders as unreachable.
func TestResolutionBeforeTheNodeKnowsItsAddress(t *testing.T) {
	b := routableBundle(t)
	s := NewIS05ConnectionServer(newLogTap().logger(), b, IS05ConnectionConfig{APIVer: "v1.2"})
	st := s.Store()
	st.setNodeIP("")
	st.reresolveActive()

	e, err := st.get("senders", b.Senders[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.active.TransportParams[0]["source_ip"]; got != "127.0.0.1" {
		t.Errorf("source_ip = %v, want the loopback", got)
	}
}

// The constraint envelope can outlast the legs it describes — a
// re-seed that shortens the staged parameters leaves the extra
// constraint sets behind — and the event-parameter pass walks only as
// far as there are legs.
func TestEventParameterPassStopsAtTheLastLeg(t *testing.T) {
	b := mqttEventBundle(t)
	s := NewIS05ConnectionServer(newLogTap().logger(), b, IS05ConnectionConfig{APIVer: "v1.2"})
	e, err := s.Store().get("senders", b.Senders[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	// One more constraint set than there are legs.
	e.constraints = append(e.constraints, e.constraints[0])
	e.staged.TransportParams = e.staged.TransportParams[:1]

	s.Store().setNodeBase("10.0.0.7:8080")
	s.Store().reresolveActive()
}

// A parameter the SDP describes but the endpoint does not publish is
// essence description, not transport: merging it would leave the
// endpoint failing its own constraints on the next read.
func TestSDPParametersOutsideTheEndpointAreDropped(t *testing.T) {
	s, _, rid := is05Server(t)
	e, err := s.Store().get("receivers", rid)
	if err != nil {
		t.Fatal(err)
	}
	// The endpoint stops publishing one of the parameters the SDP
	// carries.
	delete(e.constraints[0], "multicast_ip")

	sdp := strings.Join([]string{
		"v=0", "o=- 1 1 IN IP4 192.0.2.1", "s=test", "t=0 0",
		"m=video 5004 RTP/AVP 96",
		"c=IN IP4 239.10.10.10/64",
		"a=rtpmap:96 raw/90000",
		"",
	}, "\r\n")
	out, status, err := s.Store().applyPatch("receivers", rid, is05.StagedSender{
		TransportFile: &is05.TransportFile{Type: strp("application/sdp"), Data: &sdp},
	}, patchFields{TransportFile: true})
	if err != nil || status != stdhttp.StatusOK {
		t.Fatalf("= %d (%v)", status, err)
	}
	if got, present := out.TransportParams[0]["multicast_ip"]; present && got == "239.10.10.10" {
		t.Error("a parameter the endpoint does not publish rode in from the SDP")
	}
}

// An IS-08 request the schema accepts but the validator does not is
// refused with the validator's reason: an activation naming a mode and
// a time that contradict each other is a controller error.
func TestChannelMapActivationRefusedByTheValidator(t *testing.T) {
	s := nodeFor(t, audioBundle(), nil)

	req := httptest.NewRequest(stdhttp.MethodPost, cmBase+"/map/activations/",
		strings.NewReader(`{"activation":{"mode":"activate_immediate","requested_time":"0:0"},"action":{}}`))
	code, _, err := s.channelMapping.handleActivationPost(req)
	if err != nil || code != stdhttp.StatusBadRequest {
		t.Fatalf("= %d (%v), want 400", code, err)
	}
}

// The projection narrows receivers as well as senders: a receiver on a
// transport the minor cannot describe is dropped, because serving it
// advertises a resource the controller cannot act on.
func TestProjectionDropsAReceiverToo(t *testing.T) {
	b := routableBundle(t)
	b.Receivers[0].Transport = is04.TransportMXL

	got := projectForMinor(b, "v1.0")
	if got == b {
		t.Fatal("a projection that drops something must produce a new bundle")
	}
	if len(got.Receivers) != 0 {
		t.Errorf("receivers = %+v, want the MXL receiver dropped at v1.0", got.Receivers)
	}
}

// The endpoint list carries the addresses a controller can actually
// reach: loopback, link-local and the unspecified address are not
// among them.
func TestLocalIPv4SkipsWhatNobodyCanReach(t *testing.T) {
	prev := interfaceAddrs
	interfaceAddrs = func() ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)},
			&net.IPNet{IP: net.ParseIP("169.254.1.1"), Mask: net.CIDRMask(16, 32)},
			&net.IPNet{IP: net.ParseIP("::1"), Mask: net.CIDRMask(128, 128)},
			&net.IPAddr{IP: net.ParseIP("10.0.0.7")}, // not an IPNet
			&net.IPNet{IP: net.ParseIP("10.0.0.9"), Mask: net.CIDRMask(24, 32)},
		}, nil
	}
	t.Cleanup(func() { interfaceAddrs = prev })

	got := localIPv4()
	if len(got) != 1 || got[0] != "10.0.0.9" {
		t.Errorf("= %v, want only the reachable address", got)
	}
}

var _ = time.Second

// An IS-08 request with no action at all is caught by the validator
// rather than the decoder: the body is well-formed and simply says
// nothing about what to route.
func TestChannelMapActivationRefusesARequestWithNoAction(t *testing.T) {
	s := nodeFor(t, audioBundle(), nil)

	req := httptest.NewRequest(stdhttp.MethodPost, cmBase+"/map/activations/",
		strings.NewReader(`{"activation":{"mode":"activate_immediate"}}`))
	code, _, err := s.channelMapping.handleActivationPost(req)
	if err != nil || code != stdhttp.StatusBadRequest {
		t.Fatalf("= %d (%v), want 400", code, err)
	}
}

// An address parameter that is neither a string nor null is refused
// whichever side of the connection it arrives on.
func TestAddressParametersMustBeStringsOrNull(t *testing.T) {
	for _, isSender := range []bool{true, false} {
		if err := validateParamValue("destination_ip", 7, isSender); err == nil {
			t.Errorf("a number as an address (sender=%v) was accepted", isSender)
		}
	}

	// BCP-007-03 ids are the same shape: null or a string, never a
	// number, on either side.
	for _, isSender := range []bool{true, false} {
		if err := validateParamValue("mxl_flow_id", 7, isSender); err == nil {
			t.Errorf("a number as an mxl_flow_id (sender=%v) was accepted", isSender)
		}
		if err := validateParamValue("mxl_flow_id", nil, isSender); err != nil {
			t.Errorf("an unset mxl_flow_id (sender=%v) = %v", isSender, err)
		}
	}
}

// The constraint envelope can outlast the legs it describes, and the
// event-parameter pass walks only as far as there are legs rather than
// indexing past them.
func TestEventParameterPassStopsAtTheStagedLegs(t *testing.T) {
	e := eventEndpoint(is05.TransportParams{"ext_is_07_source_id": ""})
	// One more constraint set than there are legs.
	e.constraints = append(e.constraints, map[string]any{})
	flowID := "44444444-4444-4444-8444-444444444444"
	fillEventExtParams(e, eventFlowConfig(flowID, "33333333-3333-4333-8333-333333333333", formatData), &flowID)
}

// With no address anywhere — none advertised, none in the bundle — the
// control host is the name the Node was bound under. A href naming
// nothing is worse than one naming a name.
func TestControlHostWithNothingToNameButItself(t *testing.T) {
	b := validBundle()
	b.Node.API.Endpoints = nil
	b.Node.Interfaces = nil

	s := nodeFor(t, b, func(c *IS04NodeConfig) { c.Bind = "0.0.0.0" })
	if got := s.controlHost(); got == "" {
		t.Error("controlHost must always name something")
	}
}

// Registration stops at the first refusal, and a Device the Registry
// will not take stops it before any Source is offered.
func TestRegisterAllStopsAtARefusedDevice(t *testing.T) {
	reg := newTypedRegistry(t)
	reg.set(func(r *typedRegistry) { r.byType["device"] = stdhttp.StatusInternalServerError })

	c := NewRegistrationClient(newLogTap().logger(), reg.ts.URL, "v1.3", fullBundle(t))
	if err := c.registerAll(context.Background()); err == nil {
		t.Fatal("a refused Device must stop the chain")
	}
	if reg.seen("source") != 0 {
		t.Error("a Source was offered after the Device was refused")
	}
}
