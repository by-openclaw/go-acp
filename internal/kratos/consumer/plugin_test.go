package kratos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	dhsc "dhs/internal/consumer"
	"dhs/internal/kratos/codec"
	"dhs/internal/plugin"
)

// fakeNGC2 answers the REST queries with rows shaped like the document's
// examples. With auth set it behaves as the medium-security model of
// §6.1: 401 on everything but security/login until the cookie is shown.
type fakeNGC2 struct {
	t    *testing.T
	auth bool

	mu    sync.Mutex
	cvt   map[string]string // UID → Value
	posts []string          // postMessage bodies
}

const fakeSystem = "5464a897ddbba8f452f11d305d653983"

func newFake(t *testing.T, auth bool) (*fakeNGC2, *httptest.Server) {
	f := &fakeNGC2{t: t, auth: auth, cvt: map[string]string{
		"cpiasd/antenna/motor/status/targetPlatAz":    "180000",
		"cpiasd/tracking/general/status/trackState":   "2",
		"cpiasd/antenna/srt/diagnostics/1":            "11",
		"cpiasd/antenna/srt/diagnostics/2":            "12",
		"cpiasd/antenna/motor/status/hiddenThing":     "0",
		"cpiasd/move/command/targetAz":                "0",
	}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeNGC2) envelope(w http.ResponseWriter, query string, rows any) {
	b, _ := json.Marshal(rows)
	_, _ = fmt.Fprintf(w, `{"customerId":"tbd","system":%q,"query":%q,"timeMs":"1573739995672","parameters":%s}`, fakeSystem, query, b)
}

func (f *fakeNGC2) serve(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimPrefix(r.URL.Path, "/")
	if f.auth && q != "security/login" {
		if c, err := r.Cookie("connect.sid"); err != nil || c.Value != "s1" {
			w.WriteHeader(401)
			return
		}
	}
	switch {
	case q == "security/login":
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["Operator"] != "operator" || body["Password"] != "pw" {
			w.WriteHeader(401)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "connect.sid", Value: "s1", Path: "/"})
		f.envelope(w, q, []map[string]any{{"Operator": "operator", "AccessLevel": 10, "language": "en"}})
	case q == "security/logout":
		f.envelope(w, q, []map[string]any{{}})
	case q == "consumer/boundaries":
		f.envelope(w, q, []map[string]any{{"customerId": "@DEFAULT", "UID": "cpiasd", "activate": 1, "Comment": "Tree root enables all", "hierarchy": 5}})
	case q == "consumer/svtList":
		f.envelope(w, q, []map[string]any{
			{"UID": "cpiasd/antenna/motor", "enName": "General Motorization", "NodeType": 2},
			{"UID": "cpiasd/antenna/motor/status/targetPlatAz", "enName": "Target platform azimuth", "NodeType": 0, "Units": "mdeg", "RangeMin": "0", "RangeMax": "360000", "unitScale": "1000", "volatility": 0},
			{"UID": "cpiasd/antenna/motor/status/hiddenThing", "enName": "Hidden", "NodeType": 0},
			{"UID": "cpiasd/tracking/general/status/trackState", "enName": "Track state", "NodeType": 0, "Units": "@NOUNITS", "RangeMin": "@NOMIN"},
			{"UID": "cpiasd/antenna/srt/diagnostics/#", "enName": "SRT diagnostic", "NodeType": 0},
			{"UID": "cpiasd/rf/beacon/status/level", "enName": "Beacon level", "NodeType": 0, "Units": "dBm", "volatility": -1},
			{"UID": "cpiasd/move/command/targetAz", "enName": "Target azimuth", "NodeType": 0},
		})
	case q == "consumer/cvtList":
		f.mu.Lock()
		var rows []map[string]any
		for uid, v := range f.cvt {
			rows = append(rows, map[string]any{"UID": uid, "Value": v, "GroupID": 0, "LastUTS": 0, "ConfigValue": "@INVALID", "configLastUTS": nil})
		}
		f.mu.Unlock()
		f.envelope(w, q, rows)
	case q == "consumer/enumerations":
		f.envelope(w, q, []map[string]any{
			{"UID": "cpiasd/tracking/general/status/trackState", "enumIndex": 0, "enDisplayText": "uninitialized"},
			{"UID": "cpiasd/tracking/general/status/trackState", "enumIndex": 1, "enDisplayText": "not present"},
			{"UID": "cpiasd/tracking/general/status/trackState", "enumIndex": 2, "enDisplayText": "not tracking"},
		})
	case q == "consumer/dependencies":
		f.envelope(w, q, []map[string]any{{"uid": "cpiasd/antenna/motor/status/hiddenThing", "isHidden": 1}})
	case strings.HasPrefix(q, "consumer/cvtItem/"):
		uid := "cpiasd/" + strings.TrimPrefix(q, "consumer/cvtItem/")
		f.mu.Lock()
		v, ok := f.cvt[uid]
		f.mu.Unlock()
		if !ok {
			f.envelope(w, q, []any{})
			return
		}
		f.envelope(w, q, []map[string]any{{"UID": uid, "Value": v, "GroupID": 0, "LastUTS": 0, "ConfigValue": "@INVALID", "configLastUTS": nil, "isPrivate": 1}})
	case q == "consumer/postMessage":
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(400)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var m codec.Message
		_ = json.Unmarshal(b, &m)
		f.mu.Lock()
		f.posts = append(f.posts, string(b))
		if m.MCII == codec.MCIISet {
			if i := strings.IndexByte(m.Message, ','); i > 0 {
				f.cvt[m.Message[:i]] = m.Message[i+1:]
			}
		}
		f.mu.Unlock()
		f.envelope(w, q, []any{})
	default:
		w.WriteHeader(404)
	}
}

func hostPort(t *testing.T, url string) (string, int) {
	h, p, err := net.SplitHostPort(strings.TrimPrefix(url, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(p)
	return h, n
}

func newPlugin(o Options) *Plugin {
	p := (&Factory{}).New(plugin.Deps{}).(*Plugin)
	p.settle = 0
	p.SetOptions(o)
	return p
}

func TestConnectAnonymousAndIdentity(t *testing.T) {
	_, srv := newFake(t, false)
	p := newPlugin(Options{})
	h, port := hostPort(t, srv.URL)
	ctx := context.Background()
	if err := p.Connect(ctx, h, port); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Disconnect() }()
	if p.Root() != "cpiasd" {
		t.Fatalf("root = %q", p.Root())
	}
	si, err := p.GetSlotInfo(ctx, 0)
	if err != nil || si.Identity["system"] != fakeSystem || !si.IsOnline {
		t.Fatalf("slot info %+v %v", si, err)
	}
	if _, err := p.GetSlotInfo(ctx, 1); err == nil {
		t.Fatal("slot 1 must be refused")
	}
	di, err := p.GetDeviceInfo(ctx)
	if err != nil || di.NumSlots != 1 || di.Port != port {
		t.Fatalf("device info %+v %v", di, err)
	}
}

func TestLoginModel(t *testing.T) {
	_, srv := newFake(t, true)
	h, port := hostPort(t, srv.URL)
	ctx := context.Background()

	if err := newPlugin(Options{}).Connect(ctx, h, port); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("no credentials on a login model: %v", err)
	}
	if err := newPlugin(Options{User: "operator", Pass: "bad"}).Connect(ctx, h, port); err == nil || !strings.Contains(err.Error(), "login refused") {
		t.Fatalf("bad password: %v", err)
	}
	p := newPlugin(Options{User: "operator", Pass: "pw"})
	if err := p.Connect(ctx, h, port); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Disconnect() }()
	v, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "antenna.motor.status.targetPlatAz"})
	if err != nil || v.Str != "180000" {
		t.Fatalf("get with session cookie: %+v %v", v, err)
	}
}

func TestWalkJoinsTables(t *testing.T) {
	_, srv := newFake(t, false)
	p := newPlugin(Options{})
	h, port := hostPort(t, srv.URL)
	ctx := context.Background()
	if err := p.Connect(ctx, h, port); err != nil {
		t.Fatal(err)
	}
	objs, err := p.Walk(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]dhsc.Object{}
	for _, o := range objs {
		by[strings.Join(o.Path, "/")] = o
	}
	if _, ok := by["cpiasd/antenna/motor"]; ok {
		t.Error("a NodeType 2 row is a node, not a value")
	}
	if _, ok := by["cpiasd/antenna/motor/status/hiddenThing"]; ok {
		t.Error("isHidden 1 must be left out")
	}
	az := by["cpiasd/antenna/motor/status/targetPlatAz"]
	if az.Value.Str != "180000" || az.Unit != "mdeg" || az.Min != "0" || az.Max != "360000" || az.Meta["unitScale"] != "1000" || az.Label != "targetPlatAz" || az.Group != "cpiasd/antenna/motor/status" {
		t.Errorf("targetPlatAz = %+v", az)
	}
	ts := by["cpiasd/tracking/general/status/trackState"]
	if ts.Kind != dhsc.KindEnum || len(ts.EnumItems) != 3 || ts.EnumItems[2] != "not tracking" || ts.Unit != "" || ts.Min != nil {
		t.Errorf("trackState = %+v", ts)
	}
	for _, i := range []string{"1", "2"} {
		d := by["cpiasd/antenna/srt/diagnostics/"+i]
		if d.Meta["svtUID"] != "cpiasd/antenna/srt/diagnostics/#" || d.Meta["name"] != "SRT diagnostic" {
			t.Errorf("array instance %s = %+v", i, d)
		}
	}
	beacon := by["cpiasd/rf/beacon/status/level"]
	if beacon.Meta["mqttOnly"] != true || beacon.Value.Str != "" {
		t.Errorf("volatility -1 = %+v", beacon)
	}
	if v, err := p.GetValue(ctx, dhsc.ValueRequest{Label: "trackState"}); err != nil || v.Str != "2" {
		t.Errorf("get by label: %+v %v", v, err)
	}
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "rf/beacon/status/level"}); err == nil || !strings.Contains(err.Error(), "read it with watch") {
		t.Errorf("MQTT-only get: %v", err)
	}
}

func TestGetSetCommand(t *testing.T) {
	f, srv := newFake(t, false)
	p := newPlugin(Options{})
	h, port := hostPort(t, srv.URL)
	ctx := context.Background()
	if err := p.Connect(ctx, h, port); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"antenna/motor/status/targetPlatAz", "cpiasd/antenna/motor/status/targetPlatAz", "antenna.motor.status.targetPlatAz"} {
		if v, err := p.GetValue(ctx, dhsc.ValueRequest{Path: path}); err != nil || v.Str != "180000" {
			t.Errorf("get %q: %+v %v", path, v, err)
		}
	}
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "antenna/motor/status/targetPlat"}); !errors.Is(err, dhsc.ErrUnknownLabel) {
		t.Errorf("empty set must be not-found: %v", err)
	}
	got, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "move.command.targetAz"}, dhsc.Value{Kind: dhsc.KindInt, Int: 120000})
	if err != nil || got.Str != "120000" {
		t.Fatalf("set read-back: %+v %v", got, err)
	}
	if _, err := p.Command(ctx, "stow"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Command(ctx, "POINT-AT-GLOBAL-SAT", "42"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	posts := append([]string(nil), f.posts...)
	f.mu.Unlock()
	want := []string{
		`{"MCII":1,"NumArgs":2,"Message":"cpiasd/move/command/targetAz,120000"}`,
		`{"MCII":2,"NumArgs":1,"Message":"STOW"}`,
		`{"MCII":2,"NumArgs":1,"Message":"POINT-AT-GLOBAL-SAT 42"}`,
	}
	if strings.Join(posts, "\n") != strings.Join(want, "\n") {
		t.Fatalf("posted bodies:\n%s\nwant:\n%s", strings.Join(posts, "\n"), strings.Join(want, "\n"))
	}
	env, err := p.Table(ctx, "consumer/boundaries")
	if err != nil || env.System != fakeSystem {
		t.Fatalf("table: %+v %v", env, err)
	}
}

// fakeBroker is enough MQTT 3.1.1 to accept one client, acknowledge its
// SUBSCRIBE / UNSUBSCRIBE and push PUBLISHes on demand.
type fakeBroker struct {
	ln      net.Listener
	mu      sync.Mutex
	conn    net.Conn
	user    string
	filters []string
	ready   chan struct{}
	subbed  chan string
}

func newBroker(t *testing.T) *fakeBroker {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	b := &fakeBroker{ln: ln, ready: make(chan struct{}), subbed: make(chan string, 8)}
	t.Cleanup(func() { _ = ln.Close() })
	go b.run()
	return b
}

func (b *fakeBroker) port() int { return b.ln.Addr().(*net.TCPAddr).Port }

func (b *fakeBroker) run() {
	c, err := b.ln.Accept()
	if err != nil {
		return
	}
	defer func() { _ = c.Close() }()
	pkt, err := codec.ReadPacket(c, 1<<16)
	if err != nil || pkt.Type != codec.PacketCONNECT {
		return
	}
	// Username: after protocol name (6), level (1), flags (1), keepalive (2), client id.
	body := pkt.Body
	flags := body[7]
	idLen := int(body[10])<<8 | int(body[11])
	rest := body[12+idLen:]
	if flags&0x80 != 0 {
		n := int(rest[0])<<8 | int(rest[1])
		b.mu.Lock()
		b.user = string(rest[2 : 2+n])
		b.mu.Unlock()
	}
	_, _ = c.Write([]byte{0x20, 0x02, 0x00, 0x00})
	b.mu.Lock()
	b.conn = c
	b.mu.Unlock()
	close(b.ready)
	for {
		pkt, err := codec.ReadPacket(c, 1<<16)
		if err != nil {
			return
		}
		switch pkt.Type {
		case codec.PacketSUBSCRIBE:
			n := int(pkt.Body[2])<<8 | int(pkt.Body[3])
			f := string(pkt.Body[4 : 4+n])
			b.mu.Lock()
			b.filters = append(b.filters, f)
			b.mu.Unlock()
			_, _ = c.Write([]byte{0x90, 0x03, pkt.Body[0], pkt.Body[1], 0x00})
			b.subbed <- f
		case codec.PacketUNSUBSCRIBE:
			_, _ = c.Write([]byte{0xB0, 0x02, pkt.Body[0], pkt.Body[1]})
			b.subbed <- "-"
		case codec.PacketPINGREQ:
			_, _ = c.Write([]byte{0xD0, 0x00})
		case codec.PacketDISCONNECT:
			return
		}
	}
}

func (b *fakeBroker) publish(topic, payload string) {
	body := append([]byte{byte(len(topic) >> 8), byte(len(topic))}, topic...)
	body = append(body, payload...)
	pkt := append(codec.AppendRemainingLength([]byte{0x30}, len(body)), body...)
	b.mu.Lock()
	_, _ = b.conn.Write(pkt)
	b.mu.Unlock()
}

func TestSubscribeOverMQTT(t *testing.T) {
	_, srv := newFake(t, true)
	br := newBroker(t)
	p := newPlugin(Options{User: "operator", Pass: "pw", MQTTPort: br.port()})
	h, port := hostPort(t, srv.URL)
	ctx := context.Background()
	if err := p.Connect(ctx, h, port); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Disconnect() }()
	if _, err := p.Walk(ctx, 0); err != nil {
		t.Fatal(err)
	}
	events := make(chan dhsc.Event, 8)
	if err := p.Subscribe(dhsc.ValueRequest{Path: "antenna/motor/status/targetPlatAz"}, func(e dhsc.Event) { events <- e }); err != nil {
		t.Fatal(err)
	}
	if f := <-br.subbed; f != "cpiasd/antenna/motor/status/targetPlatAz" {
		t.Fatalf("value filter = %q", f)
	}
	if err := p.Subscribe(dhsc.ValueRequest{Path: "antenna/srt/diagnostics/#"}, func(e dhsc.Event) { events <- e }); err != nil {
		t.Fatal(err)
	}
	if f := <-br.subbed; f != "cpiasd/antenna/srt/diagnostics/#" {
		t.Fatalf("array filter = %q", f)
	}
	br.mu.Lock()
	user := br.user
	br.mu.Unlock()
	if user != "operator" {
		t.Fatalf("broker login user = %q (§6.1: same username as REST)", user)
	}
	br.publish("cpiasd/antenna/motor/status/targetPlatAz", "181000")
	br.publish("cpiasd/antenna/srt/diagnostics/2", "13")
	br.publish("cpiasd/other/topic", "x") // matches no subscription
	for _, want := range []struct{ path, val, unit string }{
		{"cpiasd/antenna/motor/status/targetPlatAz", "181000", "mdeg"},
		{"cpiasd/antenna/srt/diagnostics/2", "13", ""},
	} {
		select {
		case e := <-events:
			if e.Path != want.path || e.Value.Str != want.val || e.Unit != want.unit {
				t.Fatalf("event %+v, want %+v", e, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("no event for %s", want.path)
		}
	}
	select {
	case e := <-events:
		t.Fatalf("unexpected event %+v", e)
	case <-time.After(200 * time.Millisecond):
	}
	if err := p.Unsubscribe(dhsc.ValueRequest{Path: "antenna/motor/status/targetPlatAz"}); err != nil {
		t.Fatal(err)
	}
	if f := <-br.subbed; f != "-" {
		t.Fatalf("unsubscribe not sent: %q", f)
	}
}

func TestFilterFor(t *testing.T) {
	_, srv := newFake(t, false)
	p := newPlugin(Options{})
	h, port := hostPort(t, srv.URL)
	if err := p.Connect(context.Background(), h, port); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Walk(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ path, want string }{
		{"", "cpiasd/#"},
		{"antenna/motor", "cpiasd/antenna/motor/#"},
		{"antenna.motor.status.targetPlatAz", "cpiasd/antenna/motor/status/targetPlatAz"},
		{"antenna/+/status/targetPlatAz/#", "cpiasd/antenna/+/status/targetPlatAz/#"},
		{"#", "#"},
	} {
		got, err := p.filterFor(dhsc.ValueRequest{Path: c.path})
		if err != nil || got != c.want {
			t.Errorf("filterFor(%q) = %q %v, want %q", c.path, got, err, c.want)
		}
	}
}

// The neutral watch asks with slot -1 ("every slot"); the IDU's one slot
// answers it. Slot 1 and above are refused.
func TestSlotMinusOneIsTheIDU(t *testing.T) {
	_, srv := newFake(t, false)
	p := newPlugin(Options{})
	h, port := hostPort(t, srv.URL)
	if err := p.Connect(context.Background(), h, port); err != nil {
		t.Fatal(err)
	}
	if f, err := p.filterFor(dhsc.ValueRequest{Slot: -1, Path: "antenna/motor"}); err != nil || f != "cpiasd/antenna/motor/#" {
		t.Fatalf("slot -1: %q %v", f, err)
	}
	if v, err := p.GetValue(context.Background(), dhsc.ValueRequest{Slot: -1, Path: "antenna/motor/status/targetPlatAz"}); err != nil || v.Str != "180000" {
		t.Fatalf("get slot -1: %+v %v", v, err)
	}
	if _, err := p.filterFor(dhsc.ValueRequest{Slot: 1}); err == nil {
		t.Fatal("slot 1 must be refused")
	}
}
