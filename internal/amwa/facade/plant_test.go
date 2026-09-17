package facade

// Test plant for the facade: the registry the tool would have stood up.
//
// One httptest.Server stands in for every HTTP face a question can make
// the facade touch — the IS-04 Query API (its REST collections and, for
// v1.0, the WebSocket subscription whose SYNC grain IS the listing), the
// IS-05 Connection API the Device advertises in its controls, and the
// senders' SDP manifests. Folding them onto one server is safe because
// the facade reaches IS-05 and SDP only through hrefs the registry
// advertised, which the plant points back at itself.
//
// Resources are typed structs rendered through the real codecs, never
// hand-written JSON — a payload the Controller's decoder would reject
// cannot silently pass here.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
	v13 "dhs/internal/amwa/codec/is04/v13"
	"dhs/internal/amwa/codec/is05"
	"dhs/internal/amwa/consumer"
	httpsession "dhs/internal/amwa/session/http"
)

// Fixture ids. Valid RFC 4122 UUIDs (the codecs validate them); the
// prefix says which kind each one is.
const (
	nodeID  = "0a000000-0000-4000-8000-000000000001"
	dev1ID  = "0d000000-0000-4000-8000-000000000001" // advertises IS-05
	dev2ID  = "0d000000-0000-4000-8000-000000000002" // no controls
	srcID   = "5c000000-0000-4000-8000-000000000001"
	flowJID = "f1000000-0000-4000-8000-000000000001" // video/jxsv
	flowMID = "f1000000-0000-4000-8000-000000000002" // video/v210 on MXL
	sndAID  = "5e000000-0000-4000-8000-00000000000a" // RTP, jxsv, manifest served
	sndBID  = "5e000000-0000-4000-8000-00000000000b" // MXL
	sndCID  = "5e000000-0000-4000-8000-00000000000c" // spare, absent until a test adds it
	rcvAID  = "8e000000-0000-4000-8000-00000000000a" // dev1, active on sndA, version 100
	rcvBID  = "8e000000-0000-4000-8000-00000000000b" // dev1, idle
	rcvCID  = "8e000000-0000-4000-8000-00000000000c" // dev2 (no IS-05), MXL, active on sndB, version 50
	ghostID = "ee000000-0000-4000-8000-0000000000ee" // offered by the tool, never in the registry
)

type patchCall struct {
	receiverID string
	body       map[string]any
}

// plant is the fake registry + device the facade drives.
type plant struct {
	t     *testing.T
	srv   *httptest.Server
	codec is04.Codec

	mu        sync.Mutex
	nodes     []is04.Node
	devices   []is04.Device
	sources   []is04.Source
	flows     []is04.Flow
	senders   []is04.Sender
	receivers []is04.Receiver
	lists     map[string]int
	// onList runs under mu before the n-th (1-based) GET of a collection,
	// so a test can change the plant between one walk and the next the
	// way the tool's background thread does.
	onList func(plural string, n int)
	// subscriptionSenders / subscriptionReceivers is what the v1.0
	// WebSocket SYNC grain carries, independently of the REST list.
	subscriptionSenders   []is04.Sender
	subscriptionReceivers []is04.Receiver

	patches     chan patchCall
	patchStatus int
	sdp         map[string]string // manifest path -> SDP text
}

// newPlant starts the server on codec. The catalogue starts empty;
// stdPlant fills the shape the tests share.
func newPlant(t *testing.T, codec is04.Codec) *plant {
	t.Helper()
	p := &plant{t: t, codec: codec, lists: map[string]int{}, patches: make(chan patchCall, 16), sdp: map[string]string{}}
	p.srv = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.srv.Close)
	return p
}

// is05Href is the IS-05 control href a Device advertises to route back
// here.
func (p *plant) is05Href() string { return p.srv.URL + "/x-nmos/connection/v1.1" }

func (p *plant) serve(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case strings.HasPrefix(path, "/x-nmos/connection/"):
		p.serveIS05(w, r)
	case strings.HasPrefix(path, "/sdp/"):
		p.mu.Lock()
		sdp, ok := p.sdp[path]
		p.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/sdp")
		_, _ = io.WriteString(w, sdp)
	case strings.HasPrefix(path, "/ws/"):
		p.serveSubscriptionSocket(w, r, strings.TrimPrefix(path, "/ws/"))
	case strings.HasSuffix(path, "/subscriptions") && r.Method == http.MethodPost:
		var req struct {
			ResourcePath string `json:"resource_path"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		plural := strings.Trim(req.ResourcePath, "/")
		wsHref := strings.Replace(p.srv.URL, "http://", "ws://", 1) + "/ws/" + plural
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"sub-1","ws_href":"` + wsHref + `","resource_path":"/` + plural + `","params":{},` +
			`"persist":false,"max_update_rate_ms":100,"secure":false}`))
	case strings.Contains(path, "/x-nmos/query/"):
		plural := path[strings.LastIndex(path, "/")+1:]
		p.serveCollection(w, plural)
	default:
		http.Error(w, "unexpected "+path, http.StatusNotFound)
	}
}

// serveCollection renders one Query API collection through the codec.
func (p *plant) serveCollection(w http.ResponseWriter, plural string) {
	p.mu.Lock()
	p.lists[plural]++
	if p.onList != nil {
		p.onList(plural, p.lists[plural])
	}
	var elems [][]byte
	switch plural {
	case "nodes":
		for _, x := range p.nodes {
			elems = append(elems, p.encode(p.codec.EncodeNode(x)))
		}
	case "devices":
		for _, x := range p.devices {
			elems = append(elems, p.encode(p.codec.EncodeDevice(x)))
		}
	case "sources":
		for _, x := range p.sources {
			elems = append(elems, p.encode(p.codec.EncodeSource(x)))
		}
	case "flows":
		for _, x := range p.flows {
			elems = append(elems, p.encode(p.codec.EncodeFlow(x)))
		}
	case "senders":
		for _, x := range p.senders {
			elems = append(elems, p.encode(p.codec.EncodeSender(x)))
		}
	case "receivers":
		for _, x := range p.receivers {
			elems = append(elems, p.encode(p.codec.EncodeReceiver(x)))
		}
	default:
		p.mu.Unlock()
		http.Error(w, "unknown collection "+plural, http.StatusNotFound)
		return
	}
	p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("["))
	_, _ = w.Write(bytes.Join(elems, []byte(",")))
	_, _ = w.Write([]byte("]"))
}

// encode fails the test on a fixture the codec rejects. Errorf, not
// Fatalf: this runs on the server's goroutine, where FailNow is not
// allowed to stop the test.
func (p *plant) encode(b []byte, err error) []byte {
	p.t.Helper()
	if err != nil {
		p.t.Errorf("plant: encode fixture: %v", err)
		return []byte("null")
	}
	return b
}

// serveSubscriptionSocket pushes one IS-04 §5.2 SYNC grain for plural
// and holds the socket until the client's quiet-period timeout closes
// it — the shape the AMWA mock registry has at v1.0.
func (p *plant) serveSubscriptionSocket(w http.ResponseWriter, r *http.Request, plural string) {
	type row struct {
		Path string          `json:"path"`
		Post json.RawMessage `json:"post"`
	}
	var rows []row
	p.mu.Lock()
	switch plural {
	case "senders":
		for _, x := range p.subscriptionSenders {
			rows = append(rows, row{Path: x.ID, Post: p.encode(p.codec.EncodeSender(x))})
		}
	case "receivers":
		for _, x := range p.subscriptionReceivers {
			rows = append(rows, row{Path: x.ID, Post: p.encode(p.codec.EncodeReceiver(x))})
		}
	}
	p.mu.Unlock()
	grain, _ := json.Marshal(map[string]any{"grain": map[string]any{
		"type": "urn:x-nmos:format:data.event", "topic": "/" + plural + "/", "data": rows,
	}})
	ws, err := httpsession.AcceptWebSocket(w, r)
	if err != nil {
		return
	}
	defer func() { _ = ws.Close() }()
	if err := ws.SendText(grain); err != nil {
		return
	}
	for {
		if _, err := ws.ReadText(); err != nil {
			return
		}
	}
}

// serveIS05 is the Device's Connection API: transport files for its
// senders and the staged PATCH on its receivers, echoing the staged
// state back the way IS-05 requires.
func (p *plant) serveIS05(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/transportfile"):
		w.Header().Set("Content-Type", "application/sdp")
		_, _ = io.WriteString(w, "v=0\r\no=- 1 1 IN IP4 10.0.0.1\r\ns=plant\r\nt=0 0\r\nm=video 5004 RTP/AVP 96\r\n")
	case r.Method == http.MethodPatch && strings.HasSuffix(path, "/staged"):
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		segs := strings.Split(path, "/")
		p.patches <- patchCall{receiverID: segs[len(segs)-2], body: body}
		if p.patchStatus != 0 {
			http.Error(w, "device refused the stage", p.patchStatus)
			return
		}
		var sender *string
		if s, ok := body["sender_id"].(string); ok {
			sender = &s
		}
		master, _ := body["master_enable"].(bool)
		out, err := is05.EncodeStagedReceiver(is05.StagedReceiver{
			MasterEnableField: is05.MasterEnableField{MasterEnable: master},
			SenderID:          sender,
			Activation:        is05.Activation{Mode: is05.ActivationModeImmediate},
			TransportParams:   []is05.TransportParams{{}},
		})
		if err != nil {
			p.t.Errorf("plant: encode staged receiver: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	default:
		http.Error(w, "unexpected "+r.Method+" "+path, http.StatusNotFound)
	}
}

// controller is the Options.Controller factory for this plant at apiVer.
func (p *plant) controller(apiVer string) func(context.Context) (*consumer.Controller, error) {
	return func(ctx context.Context) (*consumer.Controller, error) {
		return consumer.NewController(ctx, consumer.ControllerOptions{
			Logger: quietLogger(), RegistryURL: p.srv.URL, APIVer: apiVer,
		})
	}
}

// listCount reports how many times a collection was GET — how many
// walks touched it.
func (p *plant) listCount(plural string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lists[plural]
}

// --- typed fixtures ---------------------------------------------------

func core(id, version, label string) is04.ResourceCore {
	return is04.ResourceCore{ID: id, Version: version, Label: label, Description: label, Tags: map[string][]string{}}
}

func deviceWith(id, href string) is04.Device {
	d := is04.Device{
		ResourceCore: core(id, "0:0", "dev"),
		Type:         "urn:x-nmos:device:generic",
		NodeID:       nodeID,
		Senders:      []string{},
		Receivers:    []string{},
		Controls:     []is04.DeviceControl{},
	}
	if href != "" {
		d.Controls = []is04.DeviceControl{{Href: href, Type: "urn:x-nmos:control:sr-ctrl/v1.1"}}
	}
	return d
}

func senderOn(id, deviceID, transport string, flowID *string, manifest *string) is04.Sender {
	return is04.Sender{
		ResourceCore:      core(id, "0:0", "snd"),
		FlowID:            flowID,
		Transport:         transport,
		DeviceID:          deviceID,
		ManifestHref:      manifest,
		InterfaceBindings: []string{"eth0"},
		Subscription:      is04.SenderSubscription{Active: false},
	}
}

func receiverOn(id, version, deviceID, transport string, caps is04.ReceiverCaps, senderID *string) is04.Receiver {
	return is04.Receiver{
		ResourceCore:      core(id, version, "rcv"),
		DeviceID:          deviceID,
		Transport:         transport,
		InterfaceBindings: []string{"eth0"},
		Format:            is04.FormatVideo,
		Caps:              caps,
		Subscription:      is04.ReceiverSubscription{SenderID: senderID, Active: senderID != nil},
	}
}

// stdPlant is the catalogue every decide test reads: one Node, an
// IS-05-capable Device (dev1) and one without controls (dev2), a JPEG XS
// RTP sender with a served manifest, an MXL sender, and three receivers
// of differing transport, capability, ownership and subscription state.
func stdPlant(t *testing.T) *plant {
	t.Helper()
	p := newPlant(t, v13.New())
	p.nodes = []is04.Node{{
		ResourceCore: core(nodeID, "0:0", "node"),
		Href:         "http://10.0.0.1/",
		Caps:         map[string]any{},
		API: is04.NodeAPI{Versions: []string{"v1.3"},
			Endpoints: []is04.NodeEndpoint{{Host: "10.0.0.1", Port: 80, Protocol: "http"}}},
		Services:   []is04.NodeService{},
		Clocks:     []is04.NodeClock{{Name: "clk0", RefType: "internal"}},
		Interfaces: []is04.NodeIface{},
	}}
	p.devices = []is04.Device{deviceWith(dev1ID, p.is05Href()), deviceWith(dev2ID, "")}
	p.sources = []is04.Source{{
		ResourceCore: core(srcID, "0:0", "src"), DeviceID: dev1ID, Format: is04.FormatVideo,
		Caps: map[string]any{}, Parents: []string{},
	}}
	p.flows = []is04.Flow{
		{ResourceCore: core(flowJID, "0:0", "jxsv"), SourceID: srcID, DeviceID: dev1ID, Parents: []string{},
			Format: is04.FormatVideo, MediaType: "video/jxsv", FrameWidth: 1920, FrameHeight: 1080,
			GrainRate: &is04.GrainRate{Numerator: 50, Denominator: 1}, Interlace: "progressive",
			ColorSpace: "BT709", TransferChar: "SDR",
			Components: []is04.FlowVideoComponent{{Name: "Y", Width: 1920, Height: 1080, BitDepth: 10}}},
		{ResourceCore: core(flowMID, "0:0", "v210"), SourceID: srcID, DeviceID: dev1ID, Parents: []string{},
			Format: is04.FormatVideo, MediaType: "video/v210", FrameWidth: 1920, FrameHeight: 1080,
			GrainRate: &is04.GrainRate{Numerator: 25, Denominator: 1}, Interlace: "progressive",
			ColorSpace: "BT709", TransferChar: "SDR",
			Components: []is04.FlowVideoComponent{{Name: "Y", Width: 1920, Height: 1080, BitDepth: 10}}},
	}
	// sndA's manifest: a 1080p50 4:2:2 JPEG XS SDP, the evidence the
	// TR-08 questions are judged on.
	p.sdp["/sdp/a.sdp"] = jxsvSDP(1920, 1080, "50", "YCbCr-4:2:2", "BT709", "SDR", "High444.12", "2k-1", 500000)
	p.senders = []is04.Sender{
		senderOn(sndAID, dev1ID, is04.TransportRTPMcast, strPtr(flowJID), strPtr(p.srv.URL+"/sdp/a.sdp")),
		senderOn(sndBID, dev1ID, is04.TransportMXL, strPtr(flowMID), nil),
	}
	jxsvCaps := is04.ReceiverCaps{MediaTypes: []string{"video/raw", "video/jxsv"}, Version: "0:0",
		ConstraintSets: []map[string]any{
			tr08ConstraintSet("YCbCr-4:2:2", 1920, 1080, 50, 1, "BT709", "SDR", "High444.12", "2k-1", 100000, 1000000),
		}}
	v210Caps := is04.ReceiverCaps{MediaTypes: []string{"video/v210"}, Version: "0:0",
		ConstraintSets: []map[string]any{{
			"urn:x-nmos:cap:meta:label":            "1080p25",
			"urn:x-nmos:cap:format:media_type":     map[string]any{"enum": []any{"video/v210"}},
			"urn:x-nmos:cap:format:frame_width":    map[string]any{"maximum": float64(1920)},
			"urn:x-nmos:cap:format:interlace_mode": map[string]any{"enum": []any{"progressive"}},
		}}}
	p.receivers = []is04.Receiver{
		receiverOn(rcvAID, "100:0", dev1ID, is04.TransportRTPMcast, jxsvCaps, strPtr(sndAID)),
		receiverOn(rcvBID, "200:0", dev1ID, is04.TransportRTPMcast, is04.ReceiverCaps{MediaTypes: []string{"video/raw"}}, nil),
		receiverOn(rcvCID, "50:0", dev2ID, is04.TransportMXL, v210Caps, strPtr(sndBID)),
	}
	return p
}

// --- facade construction ---------------------------------------------

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// lockedBuf is a log sink safe to read while the facade's answer
// goroutine is still writing.
type lockedBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// facadeFor builds a Server over factory, logging into the returned
// buffer so a test can assert on what the operator would read.
func facadeFor(t *testing.T, factory func(context.Context) (*consumer.Controller, error)) (*Server, *lockedBuf) {
	t.Helper()
	logs := &lockedBuf{}
	s, err := New(Options{
		Logger:     slog.New(slog.NewTextHandler(logs, nil)),
		Controller: factory,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, logs
}

// failingFactory is a Controller factory that cannot reach its registry.
func failingFactory(context.Context) (*consumer.Controller, error) {
	return nil, io.ErrUnexpectedEOF
}

// shortMonitors makes the monitor questions run in milliseconds for the
// duration of one test.
func shortMonitors(t *testing.T, interval, window int) {
	t.Helper()
	oldI, oldW := pollInterval, monitorWindow
	pollInterval = time.Duration(interval) * time.Millisecond
	monitorWindow = time.Duration(window) * time.Millisecond
	t.Cleanup(func() { pollInterval, monitorWindow = oldI, oldW })
}

// answers builds the tool's offered answers: answer id "ans-<n>" for
// the n-th resource id given.
func answers(ids ...string) []Answer {
	out := make([]Answer, len(ids))
	for i, id := range ids {
		out[i] = Answer{AnswerID: "ans-" + string(rune('a'+i)), Resource: Resource{ID: id, Label: id}}
	}
	return out
}
