package mnset

import (
	"context"
	"errors"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/plugin"
)

// A fake module shaped like the real one (FusioN6, fw 0x68cd783f, read
// 2026-09-20): the root and every collection answer a LISTING of
// "name/", items answer a document, the sdp items answer raw text. PUT
// replaces the document whole (the real one refuses partials), and a
// listed resource with nothing behind it 404s like a program type
// without that tree.
type module struct {
	ts    *httptest.Server
	docs  map[string]string
	puts  map[string]string
	fail  map[string]int  // resource → status to answer PUT with
	drop  map[string]bool // resource vanishes after a successful PUT (read-back fails)
	calls map[string]int
}

func newModule(t *testing.T) *module {
	t.Helper()
	m := &module{docs: map[string]string{}, puts: map[string]string{}, fail: map[string]int{}, drop: map[string]bool{}, calls: map[string]int{}}
	m.ts = httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		res := strings.TrimPrefix(r.URL.Path, apiPrefix)
		m.calls[r.Method+" "+res]++
		switch r.Method {
		case stdhttp.MethodGet:
			body, ok := m.docs[res]
			if !ok {
				stdhttp.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(body))
		case stdhttp.MethodPut:
			b, _ := io.ReadAll(r.Body)
			if st, bad := m.fail[res]; bad {
				w.WriteHeader(st)
				_, _ = w.Write([]byte(`{"code":400,"message":"key 'x' not found"}`))
				return
			}
			m.puts[res] = string(b)
			m.docs[res] = string(b)
			if m.drop[res] {
				delete(m.docs, res)
			}
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	t.Cleanup(m.ts.Close)
	m.docs[""] = `["self/","flows/","sources/","refclk/","telemetry/","sdp/","broken/"]`
	m.docs["self"] = `["information/","firmware/","ipconfig/","syslog/","protocols/","system/"]`
	m.docs["self/information"] = `{"type":"22 - ST2110 UHD Transceiver","base_type":"FusioN6","serial_number":"125061600012","current_version":"0x68cd783f"}`
	m.docs["self/firmware"] = `{"info":[{"slot":1,"desc":"MN-FusioN-6-B-APP-25-2110-SDI-2R6T-N","active":"yes"},{"slot":3,"desc":"GTW_2110_8ch","active":"no"}]}`
	m.docs["self/ipconfig"] = `{"hostname":"emsfp-a2-10-0c","ip_addr":"192.168.39.230","dhcp_enable":"1","port":"80"}`
	m.docs["self/syslog"] = `{"config":{"server":"10.6.250.101","port":514,"enable":true}}`
	m.docs["self/protocols"] = `{"sap_announcement_enable":"1","mdns_enable":"1"}`
	m.docs["self/system"] = `{"core_temp":63,"fan_speed":4724,"uptime":"0 days, 07:36:32","igmp":{"version":3}}`
	m.docs["flows"] = `["fee338d3/"]`
	m.docs["flows/fee338d3"] = `{"id":"fee338d3","network":[{"dst_ip_addr":"239.0.1.2","dst_udp_port":20000,"enable":1,"pkt_cnt":"0"},{"dst_ip_addr":"239.0.1.3","dst_udp_port":20000,"enable":1}]}`
	m.docs["refclk"] = `{"mode":"0","status":"3","locked_interface":"e1","delay_req":-3,"ratio":1.5,"nothing":null}`
	m.docs["telemetry"] = `["node/","warnings/"]`
	m.docs["telemetry/node"] = `{"health":"ok","interfaces":["e1","e2"]}`
	m.docs["telemetry/warnings"] = `[]`
	m.docs["sdp"] = `["fee338d3/"]`
	m.docs["sdp/fee338d3"] = "v=0\r\no=- 1 1 IN IP4 10.6.40.53\r\ns=CH1 VidTx\r\n"
	m.docs["broken"] = `{"a":`
	// "sources" is listed by the root but not served.
	// Channel naming: one decoder device with one receiver on two flows,
	// one SDI output, one clean switch — the shapes labelObjects reads.
	m.docs[""] = `["self/","flows/","sources/","refclk/","telemetry/","sdp/","broken/","devices/","receivers/","sdi_output/","clean_switch/"]`
	m.docs["devices"] = `[{"id":"dev8","label":"Device CH8","receivers":["rx0"],"node_id":"n"}]`
	m.docs["receivers"] = `[{"id":"rx0","device_id":"dev8","format":"video","flow_id":["fee338d3","fee338d3-sec"],"label":" "}]`
	m.docs["flows"] = `["fee338d3/","fee338d3-sec/","orphan/"]`
	m.docs["flows/fee338d3-sec"] = `{"id":"fee338d3-sec","name":"rx ch8 flow 0 sec","network":{"dst_ip_addr":"239.132.3.134","enable":1}}`
	m.docs["flows/orphan"] = `{"id":"orphan","network":{"dst_ip_addr":"0.0.0.0"}}`
	m.docs["sdi_output"] = `["out1/"]`
	m.docs["sdi_output/out1"] = `{"id":"out1","device_id":"dev8","label":"HDMI 4","color_bar":0}`
	m.docs["clean_switch"] = `["dev8/"]`
	m.docs["clean_switch/dev8"] = `{"clean_switch":{"mode":"disabled"}}`
	m.docs["flows/fee338d3"] = `{"id":"fee338d3","name":"rx ch8 flow 0 pri","network":[{"dst_ip_addr":"239.0.1.2","dst_udp_port":20000,"enable":1,"pkt_cnt":"0"},{"dst_ip_addr":"239.0.1.3","dst_udp_port":20000,"enable":1}]}`
	return m
}

func (m *module) hostPort(t *testing.T) (string, int) {
	t.Helper()
	u, err := url.Parse(m.ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(u.Port())
	return u.Hostname(), port
}

func connected(t *testing.T, m *module) *Plugin {
	t.Helper()
	p := (&Factory{}).New(plugin.Deps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).(*Plugin)
	host, port := m.hostPort(t)
	if err := p.Connect(context.Background(), host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = p.Disconnect() })
	return p
}

func TestFactoryMeta(t *testing.T) {
	meta := (&Factory{}).Meta()
	if meta.Name != "mnset" || meta.DefaultPort != 80 {
		t.Fatalf("meta = %+v", meta)
	}
	proto := (&Factory{}).New(plugin.Deps{}) // static type consumer.Protocol
	if pn, ok := proto.(consumer.PathNative); !ok || !pn.PathNative() {
		t.Error("the plugin must declare PathNative so the CLI never walks the module to answer a --path")
	}
}

func TestConnectReadsIdentityAndReportsHealth(t *testing.T) {
	m := newModule(t)
	p := connected(t, m)
	id, err := p.IdentityProbe(context.Background(), 0)
	if err != nil || id != "FusioN6@0x68cd783f" {
		t.Fatalf("identity = %q, %v", id, err)
	}
	if s := p.String(); !strings.HasSuffix(s, "FusioN6@0x68cd783f") {
		t.Errorf("String() = %q", s)
	}
	info, err := p.GetDeviceInfo(context.Background())
	if err != nil || info.NumSlots != 1 || info.Port == 0 {
		t.Fatalf("device info = %+v, %v", info, err)
	}
	si, err := p.GetSlotInfo(context.Background(), 0)
	if err != nil || si.Status != consumer.SlotPresent || !si.IsOnline {
		t.Fatalf("slot info = %+v, %v", si, err)
	}
	if _, err := p.GetSlotInfo(context.Background(), 1); !errors.Is(err, consumer.ErrObjectNotFound) {
		t.Errorf("slot 1 err = %v, want not-found", err)
	}
	// One GET was counted into the connector: health has a live rx stamp.
	if p.Metrics().Snapshot().RxFrames == 0 {
		t.Error("connect did not count into metrics")
	}
}

func TestConnectDefaultsPortAndFailsOnSilence(t *testing.T) {
	p := (&Factory{}).New(plugin.Deps{}).(*Plugin)
	p.Transport = &stdhttp.Transport{}
	err := p.Connect(context.Background(), "127.0.0.1", 1) // nothing listens on :1
	if err == nil || !strings.Contains(err.Error(), "mnset connect 127.0.0.1:1") {
		t.Fatalf("err = %v", err)
	}
	// A default port is substituted for 0 before dialling.
	err = p.Connect(context.Background(), "127.0.0.1", 0)
	if err == nil || !strings.Contains(err.Error(), ":80:") {
		t.Fatalf("err = %v, want default port 80 in the message", err)
	}
}

func TestIdentityOfFallsBackWhenFieldsMissing(t *testing.T) {
	if got := identityOf(map[string]any{}); got != "emsfp@unknown" {
		t.Errorf("identityOf(empty) = %q", got)
	}
	if got := identityOf("not a map"); got != "emsfp@unknown" {
		t.Errorf("identityOf(non-map) = %q", got)
	}
}

func TestNotConnectedEverywhere(t *testing.T) {
	p := (&Factory{}).New(plugin.Deps{}).(*Plugin)
	ctx := context.Background()
	if _, err := p.GetDeviceInfo(ctx); !errors.Is(err, consumer.ErrNotConnected) {
		t.Error("GetDeviceInfo")
	}
	if _, err := p.GetSlotInfo(ctx, 0); !errors.Is(err, consumer.ErrNotConnected) {
		t.Error("GetSlotInfo")
	}
	if _, err := p.IdentityProbe(ctx, 0); !errors.Is(err, consumer.ErrNotConnected) {
		t.Error("IdentityProbe")
	}
	if _, err := p.Walk(ctx, 0); !errors.Is(err, consumer.ErrNotConnected) {
		t.Error("Walk")
	}
	if _, err := p.GetValue(ctx, consumer.ValueRequest{Path: "flows.fee338d3.id"}); !errors.Is(err, consumer.ErrNotConnected) {
		t.Error("GetValue")
	}
	if _, err := p.SetValue(ctx, consumer.ValueRequest{Path: "flows.fee338d3.id"}, consumer.Value{}); !errors.Is(err, consumer.ErrNotConnected) {
		t.Error("SetValue")
	}
	if err := p.Disconnect(); err != nil {
		t.Error("Disconnect on a fresh plugin must be a no-op")
	}
}

func TestWalkDescendsListingsAndRecordsTheRest(t *testing.T) {
	m := newModule(t)
	p := connected(t, m)
	objs, err := p.Walk(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]consumer.Object{}
	for _, o := range objs {
		byPath[strings.Join(o.Path, ".")] = o
	}
	// An item under a collection, with the right kind and the write bit.
	o, ok := byPath["flows.fee338d3.network.1.dst_ip_addr"]
	if !ok || o.Kind != consumer.KindString || o.Value.Str != "239.0.1.3" || o.Access&accessWrite == 0 || o.Label != "CH8 · rx ch8 flow 0 pri · network.1.dst_ip_addr" {
		t.Errorf("flows leaf = %+v (found %v)", o, ok)
	}
	// Integer stays integer; float stays float; null is raw; bool is bool.
	if o := byPath["refclk.delay_req"]; o.Kind != consumer.KindInt || o.Value.Int != -3 {
		t.Errorf("int leaf = %+v", o)
	}
	if o := byPath["refclk.ratio"]; o.Kind != consumer.KindFloat || o.Value.Float != 1.5 {
		t.Errorf("float leaf = %+v", o)
	}
	if o := byPath["refclk.nothing"]; o.Kind != consumer.KindRaw {
		t.Errorf("null leaf = %+v", o)
	}
	if o := byPath["self.syslog.config.enable"]; o.Kind != consumer.KindBool || !o.Value.Bool || o.Access&accessWrite == 0 {
		t.Errorf("bool leaf = %+v", o)
	}
	// A read-only resource carries no write bit.
	if o := byPath["self.information.serial_number"]; o.Access&accessWrite != 0 {
		t.Errorf("information should be read-only: %+v", o)
	}
	// A nested listing is descended; a plain array inside a document is indexed.
	if o := byPath["telemetry.node.interfaces.1"]; o.Value.Str != "e2" {
		t.Errorf("nested listing leaf = %+v", o)
	}
	if _, ok := byPath["telemetry.0"]; ok {
		t.Error("a listing must be descended, not exported as a leaf")
	}
	// A text resource is one string leaf.
	if o := byPath["sdp.fee338d3"]; o.Kind != consumer.KindString || !strings.HasPrefix(o.Value.Str, "v=0") || o.Access&accessWrite != 0 {
		t.Errorf("sdp leaf = %+v", o)
	}
	// A listed resource the fake does not serve is a deviation, not a failure.
	devs := p.Deviations()
	if len(devs) != 1 || !strings.Contains(devs[0], "sources") {
		t.Errorf("deviations = %v, want exactly the unserved 'sources'", devs)
	}
	if _, err := p.Walk(context.Background(), 1); !errors.Is(err, consumer.ErrObjectNotFound) {
		t.Errorf("walk slot 1 err = %v", err)
	}
}

func TestWalkBoundsListingDepth(t *testing.T) {
	m := newModule(t)
	// A listing that lists itself forever.
	m.docs[""] = `["loop/"]`
	for i := 1; i < 12; i++ {
		m.docs[strings.TrimSuffix(strings.Repeat("loop/", i), "/")] = `["loop/"]`
	}
	p := connected(t, m)
	if _, err := p.Walk(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	devs := p.Deviations()
	if len(devs) != 1 || !strings.Contains(devs[0], "nested deeper than") {
		t.Errorf("deviations = %v", devs)
	}
}

func TestWalkStopsOnCancel(t *testing.T) {
	m := newModule(t)
	p := connected(t, m)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Walk(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	// Cancelled mid-descent: the listing loop must surface it too.
	ctx2, cancel2 := context.WithCancel(context.Background())
	inner := m.ts.Config.Handler
	m.ts.Config.Handler = stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if strings.HasSuffix(r.URL.Path, "/self") {
			cancel2()
		}
		inner.ServeHTTP(w, r)
	})
	if _, err := p.Walk(ctx2, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("mid-walk err = %v", err)
	}
}

func TestGetValueLiveAndErrors(t *testing.T) {
	m := newModule(t)
	p := connected(t, m)
	ctx := context.Background()
	v, err := p.GetValue(ctx, consumer.ValueRequest{Path: "self.ipconfig.hostname"})
	if err != nil || v.Str != "emsfp-a2-10-0c" {
		t.Fatalf("get = %+v, %v", v, err)
	}
	// The [n] form resolves the same as the dotted form.
	v, err = p.GetValue(ctx, consumer.ValueRequest{Path: "flows.fee338d3.network[0].dst_udp_port"})
	if err != nil || v.Int != 20000 {
		t.Fatalf("get [n] = %+v, %v", v, err)
	}
	// A text resource is its own value.
	v, err = p.GetValue(ctx, consumer.ValueRequest{Path: "sdp.fee338d3"})
	if err != nil || !strings.HasPrefix(v.Str, "v=0") {
		t.Fatalf("get sdp = %+v, %v", v, err)
	}
	cases := map[string]string{
		"":                           "not-found",
		"self":                       "is a list",
		"flows.0.network.9.x":        "not listed under /flows",
		"flows.fee338d3.network.9.x": "not-found",
		"self.ipconfig":              "is a node",
		"telemetry.node.interfaces":  "is a list",
		"nosuchresource.field":       "not listed under /",
		"sources.x":                  "GET sources",
		"broken.a":                   "not-found", // undecodable body is text; no field "a" inside text
		"sdp.fee338d3.deeper":        "not-found",
	}
	for path, want := range cases {
		_, err := p.GetValue(ctx, consumer.ValueRequest{Path: path})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("GetValue(%q) err = %v, want %q", path, err, want)
		}
	}
	// The root itself cannot be fetched: surfaced.
	delete(m.docs, "")
	if _, err := p.GetValue(ctx, consumer.ValueRequest{Path: "refclk.mode"}); err == nil || !strings.Contains(err.Error(), "GET :") {
		t.Errorf("root missing err = %v", err)
	}
}

func TestSetValueReadModifyWriteConfirms(t *testing.T) {
	m := newModule(t)
	p := connected(t, m)
	ctx := context.Background()

	// string leaf, from the CLI shape (only Str set); the PUT goes to the item URL
	got, err := p.SetValue(ctx, consumer.ValueRequest{Path: "flows.fee338d3.network.1.dst_ip_addr"}, consumer.Value{Str: "239.131.3.134"})
	if err != nil || got.Str != "239.131.3.134" {
		t.Fatalf("set string = %+v, %v", got, err)
	}
	put := m.puts["flows/fee338d3"]
	if !strings.Contains(put, `"dst_ip_addr":"239.131.3.134"`) || !strings.Contains(put, `"dst_udp_port":20000`) {
		t.Errorf("PUT must carry the whole document with ints intact: %s", put)
	}
	if _, wrong := m.puts["flows"]; wrong {
		t.Error("PUT must target the item, never the collection")
	}
	// numeric leaf from a string; stays a number in the PUT
	got, err = p.SetValue(ctx, consumer.ValueRequest{Path: "flows.fee338d3.network.0.dst_udp_port"}, consumer.Value{Str: "30000"})
	if err != nil || got.Int != 30000 {
		t.Fatalf("set number = %+v, %v", got, err)
	}
	if !strings.Contains(m.puts["flows/fee338d3"], `"dst_udp_port":30000`) {
		t.Errorf("number must not be quoted: %s", m.puts["flows/fee338d3"])
	}
	// numeric leaf from typed Values
	if got, err = p.SetValue(ctx, consumer.ValueRequest{Path: "refclk.delay_req"}, consumer.Value{Kind: consumer.KindInt, Int: -2}); err != nil || got.Int != -2 {
		t.Fatalf("set typed int = %+v, %v", got, err)
	}
	if got, err = p.SetValue(ctx, consumer.ValueRequest{Path: "refclk.ratio"}, consumer.Value{Kind: consumer.KindFloat, Float: 2.25}); err != nil || got.Float != 2.25 {
		t.Fatalf("set typed float = %+v, %v", got, err)
	}
	// bool leaf from a word and from a typed bool
	if got, err = p.SetValue(ctx, consumer.ValueRequest{Path: "self.syslog.config.enable"}, consumer.Value{Str: "off"}); err != nil || got.Bool {
		t.Fatalf("set bool word = %+v, %v", got, err)
	}
	if got, err = p.SetValue(ctx, consumer.ValueRequest{Path: "self.syslog.config.enable"}, consumer.Value{Kind: consumer.KindBool, Bool: true}); err != nil || !got.Bool {
		t.Fatalf("set typed bool = %+v, %v", got, err)
	}
	// null leaf accepts a string
	if got, err = p.SetValue(ctx, consumer.ValueRequest{Path: "refclk.nothing"}, consumer.Value{Str: "x"}); err != nil || got.Str != "x" {
		t.Fatalf("set null = %+v, %v", got, err)
	}
}

func TestSetValueErrors(t *testing.T) {
	m := newModule(t)
	p := connected(t, m)
	ctx := context.Background()
	writable["sdp"] = true // pretend, to reach the text-document refusal
	t.Cleanup(func() { delete(writable, "sdp") })
	cases := []struct {
		path, want string
		val        consumer.Value
	}{
		{"", "not-found", consumer.Value{Str: "x"}},
		{"self.information.serial_number", "read-only", consumer.Value{Str: "x"}},
		{"flows.fee338d3.network.9.dst_ip_addr", "not-found", consumer.Value{Str: "x"}},
		{"self.syslog.config.enable", "not a boolean", consumer.Value{Str: "maybe"}},
		{"flows.fee338d3.network.0.dst_udp_port", "not a number", consumer.Value{Str: "high"}},
		{"self.syslog.config", "not a settable scalar", consumer.Value{Str: "x"}},
		{"nosuch.field", "not listed under /", consumer.Value{Str: "x"}},
		{"sdp.fee338d3", "text document", consumer.Value{Str: "x"}},
	}
	for _, c := range cases {
		_, err := p.SetValue(ctx, consumer.ValueRequest{Path: c.path}, c.val)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("SetValue(%q) err = %v, want %q", c.path, err, c.want)
		}
	}
	// The module refuses the PUT: the device's message is surfaced.
	m.fail["refclk"] = 400
	if _, err := p.SetValue(ctx, consumer.ValueRequest{Path: "refclk.delay_req"}, consumer.Value{Str: "1"}); err == nil || !strings.Contains(err.Error(), "answered 400 key 'x' not found") {
		t.Errorf("PUT refusal err = %v", err)
	}
	delete(m.fail, "refclk")
}
