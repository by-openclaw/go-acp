package prism

import (
	"context"
	"encoding/json"
	"errors"
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
	"dhs/internal/plugin"
	"dhs/internal/prism/codec"
	"dhs/internal/transport"
)

// fake is a PRISM built from the page's URL rules: GET /api/<name>[/scope]
// answers a stored raw body, POST stores the body it got. Anything not
// stored answers 404. Bodies are synthetic.
type fake struct {
	mu     sync.Mutex
	get    map[string]string // "name/scope?query" -> body
	status map[string]int    // same key -> HTTP status
	posts  map[string]string // "name/scope" -> POST body
	calls  []string
	auth   []string
	apply  func(key, body string) // what a POST does to the state
}

func newFake(t *testing.T) (*fake, *httptest.Server) {
	t.Helper()
	f := &fake{get: map[string]string{
		"software_version": `{"object":"4.3.1"}`,
	}, status: map[string]int{}, posts: map[string]string{}}
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	return f, s
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.TrimPrefix(r.URL.Path, "/api/")
	if r.URL.RawQuery != "" {
		key += "?" + r.URL.RawQuery
	}
	f.calls = append(f.calls, r.Method+" "+key)
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	if st, ok := f.status[key]; ok {
		w.WriteHeader(st)
		_, _ = io.WriteString(w, f.get[key])
		return
	}
	if r.Method == "POST" {
		b, _ := io.ReadAll(r.Body)
		f.posts[key] = string(b)
		if f.apply != nil {
			f.apply(key, string(b))
		}
		return
	}
	body, ok := f.get[key]
	if !ok {
		w.WriteHeader(404)
		_, _ = io.WriteString(w, "not found")
		return
	}
	_, _ = io.WriteString(w, body)
}

func hostPort(t *testing.T, s *httptest.Server) (string, int) {
	t.Helper()
	h, ps, err := net.SplitHostPort(strings.TrimPrefix(s.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(ps)
	return h, n
}

func connected(t *testing.T, s *httptest.Server, o Options) *Plugin {
	t.Helper()
	p := (&Factory{}).New(plugin.Deps{}).(*Plugin)
	p.SetOptions(o)
	h, port := hostPort(t, s)
	if err := p.Connect(context.Background(), h, port); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Disconnect() })
	return p
}

func TestMeta(t *testing.T) {
	m := (&Factory{}).Meta()
	if m.Name != "prism" || m.DefaultPort != 9000 {
		t.Fatalf("%+v", m)
	}
	if got, err := dhsc.Get("prism"); err != nil || got == nil {
		t.Fatalf("not registered: %v", err)
	}
	if !(&Plugin{}).PathNative() {
		t.Fatal("not path native")
	}
}

func TestConnectFailures(t *testing.T) {
	f, s := newFake(t)
	p := (&Factory{}).New(plugin.Deps{}).(*Plugin)
	h, port := hostPort(t, s)

	f.status["software_version"] = 401
	p.SetOptions(Options{})
	err := p.Connect(context.Background(), h, port)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("401: %v", err)
	}
	if _, err := p.session(); err == nil {
		t.Fatal("session survives a failed connect")
	}

	s.Close()
	err = p.Connect(context.Background(), h, port)
	if err == nil || !strings.Contains(err.Error(), "try <host>:80") {
		t.Fatalf("closed: %v", err)
	}

	p.catErr = errors.New("catalogue broken")
	if err := p.Connect(context.Background(), h, 0); err == nil || err.Error() != "catalogue broken" {
		t.Fatalf("catErr: %v", err)
	}
}

func TestDefaultPortAndNotConnected(t *testing.T) {
	p := (&Factory{}).New(plugin.Deps{}).(*Plugin)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	p.SetOptions(Options{})
	if err := p.Connect(ctx, "127.0.0.1", 0); err == nil || !strings.Contains(err.Error(), ":9000") {
		t.Fatalf("port: %v", err)
	}
	if _, err := p.GetDeviceInfo(ctx); !errors.Is(err, dhsc.ErrNotConnected) {
		t.Fatal(err)
	}
	if _, err := p.GetSlotInfo(ctx, 0); !errors.Is(err, dhsc.ErrNotConnected) {
		t.Fatal(err)
	}
	if _, err := p.Walk(ctx, 0); !errors.Is(err, dhsc.ErrNotConnected) {
		t.Fatal(err)
	}
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "software_version"}); !errors.Is(err, dhsc.ErrNotConnected) {
		t.Fatal(err)
	}
	if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "host_name"}, dhsc.Value{Str: "x"}); !errors.Is(err, dhsc.ErrNotConnected) {
		t.Fatal(err)
	}
}

func TestOptionsFromEnvironment(t *testing.T) {
	f, s := newFake(t)
	t.Setenv(EnvPass, "secret")
	t.Setenv(EnvScalar, "bare")
	t.Setenv(EnvAnswerWait, "2s")
	t.Setenv(EnvAllowActions, "1")
	p := (&Factory{}).New(plugin.Deps{}).(*Plugin)
	h, port := hostPort(t, s)
	if err := p.Connect(context.Background(), h, port); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Disconnect() }()
	r, _ := p.session()
	if p.scalar != codec.ScalarBare || !p.allow || r.wait != 2*time.Second {
		t.Fatalf("scalar=%v allow=%v wait=%v", p.scalar, p.allow, r.wait)
	}
	// The page's script: -H Authorization: Basic <password>.
	if f.auth[0] != "Basic secret" {
		t.Fatalf("auth = %q", f.auth[0])
	}

	t.Setenv(EnvScalar, "list")
	if err := p.Connect(context.Background(), h, port); err == nil {
		t.Fatal("bad scalar accepted")
	}
	t.Setenv(EnvScalar, "")
	t.Setenv(EnvAnswerWait, "soon")
	if err := p.Connect(context.Background(), h, port); err == nil {
		t.Fatal("bad wait accepted")
	}
}

func TestInfo(t *testing.T) {
	f, s := newFake(t)
	f.get["serial_number"] = `{"object":"SYN0001"}`
	f.get["hardware_version_info/slot0"] = `{"object":{"rev":"A"}}`
	f.get["ip_signal_status/IP1"] = `{"object":{"errors":[]}}`
	f.status["ptp_status"] = 500
	f.get["ptp_status"] = "boom"
	p := connected(t, s, Options{})
	ctx := context.Background()

	info, err := p.GetDeviceInfo(ctx)
	if err != nil || info.NumSlots != 1 {
		t.Fatalf("%+v %v", info, err)
	}
	si, err := p.GetSlotInfo(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if si.Identity["software_version"] != "4.3.1" || si.Identity["serial_number"] != "SYN0001" ||
		si.Identity["hardware_version_info/slot0"] != `{"rev":"A"}` {
		t.Fatalf("%v", si.Identity)
	}
	if _, ok := si.Identity["ptp_status"]; ok || !strings.Contains(si.Identity["ptp_status.error"], "HTTP 500 boom") {
		t.Fatalf("failure not separate: %v", si.Identity)
	}
	if _, err := p.GetSlotInfo(ctx, 1); err == nil {
		t.Fatal("slot 1")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := p.GetSlotInfo(cancelled, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
}

// A verb's --timeout is one answer's wait, whatever number of exchanges
// the verb takes: an answer asked for after the verb's own deadline has
// passed still gets a wait of its own. Decided on the deadlines, not on
// how fast a runner sleeps: the check starts when the verb's context has
// ended (an event), and compares two fixed instants.
func TestInfoWaitIsPerAnswer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	verbEnds, _ := ctx.Deadline()
	next := answers(ctx)
	<-ctx.Done()
	c, stop := next()
	defer stop()
	own, ok := c.Deadline()
	if !ok || !own.After(verbEnds) {
		t.Fatalf("an answer after the verb's deadline waits until %v (has deadline %v), the verb ended at %v", own, ok, verbEnds)
	}

	// And the verb itself: every identity exchange answers, none is cut
	// by the exchanges before it.
	f, s := newFake(t)
	for _, call := range identityCalls() {
		f.get[call] = `{"object":"x"}`
	}
	p := connected(t, s, Options{})
	vctx, vcancel := context.WithTimeout(context.Background(), time.Minute)
	defer vcancel()
	si, err := p.GetSlotInfo(vctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range si.Identity {
		if strings.HasSuffix(k, ".error") {
			t.Fatalf("%s: %s", k, v)
		}
	}
}

func TestAnswersFollowCtrlC(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	next := answers(ctx)
	c, stop := next()
	defer stop()
	cancel()
	<-c.Done()
	if !errors.Is(c.Err(), context.Canceled) {
		t.Fatal(c.Err())
	}
}

func TestAnswerWaitBindsAWalkExchange(t *testing.T) {
	release := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "software_version") {
			_, _ = io.WriteString(w, `{"object":"4"}`)
			return
		}
		<-release
	}))
	defer s.Close()
	defer close(release)
	// Connect with the default wait: only the stalled walk exchange is the
	// behaviour under test, not how fast the runner opens its first socket.
	p := connected(t, s, Options{})
	p.rest.wait = 20 * time.Millisecond
	objs, err := p.WalkUnder(context.Background(), "ptp_status")
	if err != nil || len(objs) != 1 || objs[0].Meta["error"] == nil {
		t.Fatalf("%v %v", objs, err)
	}
}

func TestGetValue(t *testing.T) {
	f, s := newFake(t)
	f.get["waveform_gain/tile2"] = `{"object":[4]}`
	f.get["audio_program_level"] = `{"object":[-20.5]}`
	f.get["system_status"] = `{"object":{"fans":{"state":"ok"}}}`
	f.get["system_status?field=object.fans.state"] = `"ok"`
	f.get["system_ready"] = `{"object":true}`
	f.get["inputConfigure?input=3"] = `{"object":{"name":"x"}}`
	f.get["ip_signal_status/config0"] = `{"object":{}}`
	f.get["eye_histogram_data/slot0"] = `{"object":[1e999]}`
	p := connected(t, s, Options{})
	ctx := context.Background()

	for _, tc := range []struct {
		path string
		want dhsc.Value
	}{
		{"waveform_gain/tile2", dhsc.Value{Kind: dhsc.KindInt, Int: 4}},
		{"waveform_gain.tile2", dhsc.Value{Kind: dhsc.KindInt, Int: 4}},
		{"audio_program_level", dhsc.Value{Kind: dhsc.KindFloat, Float: -20.5}},
		{"software_version", dhsc.Value{Kind: dhsc.KindString, Str: "4.3.1"}},
		{"system_status", dhsc.Value{Kind: dhsc.KindString, Str: `{"fans":{"state":"ok"}}`}},
		{"system_status?field=object.fans.state", dhsc.Value{Kind: dhsc.KindString, Str: "ok"}},
		{"system_ready", dhsc.Value{Kind: dhsc.KindBool, Bool: true}},
		{"inputConfigure?input=3", dhsc.Value{Kind: dhsc.KindString, Str: `{"name":"x"}`}},
		{"ip_signal_status/config0", dhsc.Value{Kind: dhsc.KindString, Str: `{}`}},
		{"eye_histogram_data/slot0", dhsc.Value{Kind: dhsc.KindString, Str: "1e999"}},
	} {
		got, err := p.GetValue(ctx, dhsc.ValueRequest{Path: tc.path, Slot: -1})
		if err != nil {
			t.Errorf("%s: %v", tc.path, err)
			continue
		}
		if got.Kind != tc.want.Kind || got.Int != tc.want.Int || got.Float != tc.want.Float ||
			got.Str != tc.want.Str || got.Bool != tc.want.Bool {
			t.Errorf("%s = %+v", tc.path, got)
		}
	}
	if got, err := p.GetValue(ctx, dhsc.ValueRequest{Label: "software_version"}); err != nil || got.Str != "4.3.1" {
		t.Errorf("label: %v %v", got, err)
	}
	if got, err := p.GetValue(ctx, dhsc.ValueRequest{Group: "waveform_gain", Label: "tile2"}); err != nil || got.Int != 4 {
		t.Errorf("group+label: %v %v", got, err)
	}
	if strings.Contains(strings.Join(f.calls, " "), "waveform_gain/tile1") {
		t.Error("a get read more than it was asked")
	}
}

func TestGetValueRefused(t *testing.T) {
	f, s := newFake(t)
	f.status["ptp_status"] = 403
	p := connected(t, s, Options{})
	ctx := context.Background()
	calls := len(f.calls)

	var verr *dhsc.ValidationError
	for _, path := range []string{"waveform_mode", "system_reboot", "loudness_load_preset", "x?field=nope", ""} {
		if _, err := p.GetValue(ctx, dhsc.ValueRequest{Path: path}); !errors.As(err, &verr) {
			t.Errorf("%q: %v", path, err)
		}
	}
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "no_such_call"}); !errors.Is(err, dhsc.ErrUnknownLabel) {
		t.Errorf("unknown: %v", err)
	}
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "ptp_status", Slot: 1}); err == nil {
		t.Error("slot 1")
	}
	if len(f.calls) != calls {
		t.Errorf("a refused request reached the wire: %v", f.calls[calls:])
	}
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "ptp_status"}); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("403: %v", err)
	}
	var ref *RefusedError
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "gamut_measurements"}); !errors.As(err, &ref) || ref.Status != 404 {
		t.Errorf("404: %v", err)
	}
}

// A 2xx that is no value must never come back as one.
func TestGetValueNeverReturnsAFailureAsValue(t *testing.T) {
	f, s := newFake(t)
	f.get["ptp_status"] = `<html>sign in</html>`
	f.get["system_status"] = `{"error":"busy"}`
	f.get["gamut_measurements"] = `{"object":null}`
	f.get["timecode_current"] = ``
	p := connected(t, s, Options{})
	for _, path := range []string{"ptp_status", "system_status", "gamut_measurements", "timecode_current"} {
		if v, err := p.GetValue(context.Background(), dhsc.ValueRequest{Path: path}); err == nil {
			t.Errorf("%s answered %+v", path, v)
		}
	}
	snap := p.ComplianceProfile().Snapshot()
	if snap[codec.DevNotJSON] != 1 || snap[codec.DevErrorIn2xx] != 1 {
		t.Fatalf("%v", snap)
	}
}

func TestSetReadsBack(t *testing.T) {
	f, s := newFake(t)
	f.get["waveform_mode/tile1"] = `{"object":["WAVEFORM_MODE_YPBPR"]}`
	f.apply = func(key, body string) {
		var b struct {
			Object []string `json:"object"`
		}
		_ = json.Unmarshal([]byte(body), &b)
		f.get[key] = `{"object":["` + b.Object[0] + `"]}`
	}
	p := connected(t, s, Options{})
	got, err := p.SetValue(context.Background(), dhsc.ValueRequest{Path: "waveform_mode/tile1"}, dhsc.Value{Str: "WAVEFORM_MODE_RGB"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Str != "WAVEFORM_MODE_RGB" {
		t.Fatalf("read-back = %+v", got)
	}
	if f.posts["waveform_mode/tile1"] != `{"object":["WAVEFORM_MODE_RGB"]}` {
		t.Fatalf("body = %s", f.posts["waveform_mode/tile1"])
	}
	if n := len(p.ComplianceProfile().Snapshot()); n != 0 {
		t.Fatalf("events: %v", p.ComplianceProfile().Snapshot())
	}
}

func TestSetRefusedBeforeTheWire(t *testing.T) {
	f, s := newFake(t)
	p := connected(t, s, Options{})
	ctx := context.Background()
	calls := len(f.calls)
	var verr *dhsc.ValidationError
	for _, tc := range []struct{ path, value string }{
		{"waveform_mode/tile1", "WAVEFORM_MODE_NOPE"},
		{"waveform_mode", "WAVEFORM_MODE_RGB"},
		{"software_version", "1"},
		{"system_reboot", "{}"},
		{"serial_number", "X"},
		{"data_logger_db_new", "1"},
		{"inputConfigure", `{"data":{}}`},
		{"waveform_mode/tile1?field=object", "WAVEFORM_MODE_RGB"},
	} {
		if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: tc.path}, dhsc.Value{Str: tc.value}); !errors.As(err, &verr) {
			t.Errorf("%s=%s: %v", tc.path, tc.value, err)
		}
	}
	if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "no_such"}, dhsc.Value{Str: "1"}); !errors.Is(err, dhsc.ErrUnknownLabel) {
		t.Errorf("unknown: %v", err)
	}
	if len(f.calls) != calls {
		t.Fatalf("reached the wire: %v", f.calls[calls:])
	}
}

func TestSetGuardOpen(t *testing.T) {
	f, s := newFake(t)
	f.apply = func(key, body string) { f.get[key] = `{"object":"X"}` }
	p := connected(t, s, Options{AllowActions: true})
	got, err := p.SetValue(context.Background(), dhsc.ValueRequest{Path: "serial_number"}, dhsc.Value{Str: "X"})
	if err != nil || got.Str != "X" {
		t.Fatalf("%+v %v", got, err)
	}
	// system_reboot is POSTable but never read: sent, nothing confirmed.
	got, err = p.SetValue(context.Background(), dhsc.ValueRequest{Path: "system_reboot"}, dhsc.Value{Str: "{}"})
	if err != nil || !strings.Contains(got.Str, "no read-back") {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestSetPostOnlySaysNothingIsConfirmed(t *testing.T) {
	f, s := newFake(t)
	p := connected(t, s, Options{})
	got, err := p.SetValue(context.Background(), dhsc.ValueRequest{Path: "loudness_load_preset"}, dhsc.Value{Str: "LOUDNESS_PRESET_EBU_R128_2014"})
	if err != nil || !strings.Contains(got.Str, "no read-back") {
		t.Fatalf("%+v %v", got, err)
	}
	if f.posts["loudness_load_preset"] != `{"object":["LOUDNESS_PRESET_EBU_R128_2014"]}` {
		t.Fatalf("body %s", f.posts["loudness_load_preset"])
	}
}

func TestSetInputConfigureReadsBackThatInput(t *testing.T) {
	f, s := newFake(t)
	f.apply = func(key, body string) { f.get["inputConfigure?input=2"] = `{"object":{"name":"y"}}` }
	p := connected(t, s, Options{})
	body := `{"input":2,"data":{"inputType":"SDI","name":"y","port":0}}`
	got, err := p.SetValue(context.Background(), dhsc.ValueRequest{Path: "inputConfigure"}, dhsc.Value{Str: body})
	if err != nil || got.Str != `{"name":"y"}` {
		t.Fatalf("%+v %v", got, err)
	}
	if f.posts["inputConfigure"] != body {
		t.Fatalf("body %s", f.posts["inputConfigure"])
	}
}

func TestSetFailures(t *testing.T) {
	f, s := newFake(t)
	p := connected(t, s, Options{})
	ctx := context.Background()

	f.status["host_name"] = 403
	if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "host_name"}, dhsc.Value{Str: "a"}); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("403: %v", err)
	}
	if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "current_time"}, dhsc.Value{Str: "a"}); err == nil ||
		!strings.Contains(err.Error(), "read-back failed") {
		t.Errorf("read-back: %v", err)
	}
	f.apply = func(key, body string) {}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/snmp_trap_community", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"error":"read only"}`)
	})
	mux.Handle("/", f)
	s.Config.Handler = mux
	if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "snmp_trap_community"}, dhsc.Value{Str: "a"}); err == nil ||
		!strings.Contains(err.Error(), "read only") {
		t.Errorf("error in 200: %v", err)
	}
	if p.ComplianceProfile().Snapshot()[codec.DevErrorIn2xx] != 1 {
		t.Error("error in 200 not counted")
	}
}

func TestSetReadbackDiffers(t *testing.T) {
	f, s := newFake(t)
	f.get["host_name"] = `{"object":"other"}`
	p := connected(t, s, Options{})
	got, err := p.SetValue(context.Background(), dhsc.ValueRequest{Path: "host_name"}, dhsc.Value{Str: "mine"})
	if err != nil || got.Str != "other" {
		t.Fatalf("%+v %v", got, err)
	}
	if p.ComplianceProfile().Snapshot()[codec.DevReadbackDiffers] != 1 {
		t.Fatal("not counted")
	}
}

func TestSameValue(t *testing.T) {
	cat, _ := codec.Load()
	ints, _ := cat.Lookup("waveform_gain")
	enum, _ := cat.Lookup("waveform_mode")
	obj, _ := cat.Lookup("audio_silence_alarm")
	str, _ := cat.Lookup("host_name")
	reply := func(text string) codec.Reply { return codec.Reply{Text: text, Single: text} }
	for _, tc := range []struct {
		e    codec.Entry
		sent string
		got  codec.Reply
		want bool
	}{
		{obj, "{}", reply("x"), true},
		{ints, "4,5", codec.Reply{Text: "[4,5]"}, true},
		{ints, "4", reply("4.0"), true},
		{ints, "4", reply("5"), false},
		{enum, "WAVEFORM_MODE_RGB", reply("1"), true},
		{enum, "1", reply("WAVEFORM_MODE_RGB"), true},
		{enum, "WAVEFORM_MODE_RGB", reply("WAVEFORM_MODE_YRGB"), false},
		{str, "abc", reply("abc"), true},
		{str, "1", reply("abc"), false},
		{str, "abc", reply("abd"), false},
	} {
		if got := sameValue(tc.e, tc.sent, tc.got); got != tc.want {
			t.Errorf("%s %s vs %s = %v", tc.e.Name, tc.sent, tc.got.Text, got)
		}
	}
}

func TestValueText(t *testing.T) {
	for _, tc := range []struct {
		v    dhsc.Value
		want string
	}{
		{dhsc.Value{Kind: dhsc.KindBool, Bool: true}, "1"},
		{dhsc.Value{Kind: dhsc.KindBool}, "0"},
		{dhsc.Value{Kind: dhsc.KindInt, Int: -3}, "-3"},
		{dhsc.Value{Kind: dhsc.KindUint, Uint: 3}, "3"},
		{dhsc.Value{Kind: dhsc.KindFloat, Float: 1.5}, "1.5"},
		{dhsc.Value{Str: "x"}, "x"},
		{dhsc.Value{Kind: dhsc.KindRaw, Raw: []byte("y")}, "y"},
	} {
		if got := valueText(tc.v); got != tc.want {
			t.Errorf("%+v = %q", tc.v, got)
		}
	}
}

func TestWalkGoesOnPastRefusals(t *testing.T) {
	f, s := newFake(t)
	f.get["waveform_gain/tile1"] = `{"object":[4]}`
	f.get["waveform_gain/tile3"] = `{"object":[5]}`
	f.status["serial_number"] = 403
	f.get["system_status"] = `<html>sign in</html>`
	f.get["input_status"] = `{"object":{"primary":true}}`
	f.get["input_status/config2"] = `{"object":{}}`
	p := connected(t, s, Options{})
	ctx := context.Background()
	objs, err := p.Walk(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]dhsc.Object{}
	for i, o := range objs {
		if o.ID != i {
			t.Fatalf("id %d at %d", o.ID, i)
		}
		by[o.Meta["call"].(string)] = o
	}

	if v := by["waveform_gain/tile3"].Value; v.Int != 5 {
		t.Fatalf("a read past the refusals is lost: %+v", v)
	}
	for _, call := range []string{"waveform_gain/tile2", "serial_number", "system_status", "nmos_api_version"} {
		o, ok := by[call]
		if !ok || !o.Value.IsZero() || o.Meta["error"] == nil {
			t.Errorf("%s: %+v", call, o)
		}
	}
	if !by["input_status"].Value.IsZero() == false || by["input_status/config2"].Value.IsZero() {
		t.Error("the optional scope reads both forms")
	}

	o := by["waveform_mode/tile4"]
	if o.Group != "waveform_mode" || o.Label != "tile4" || len(o.Path) != 2 || o.Access != 3 ||
		o.Kind != dhsc.KindEnum || len(o.EnumItems) != 3 {
		t.Errorf("%+v", o)
	}
	if by["inputConfigure?input=5"].Label != "input=5" {
		t.Errorf("%+v", by["inputConfigure?input=5"])
	}
	if o := by["loudness_load_preset"]; o.Access != 2 || o.Meta["skipped"] == nil {
		t.Errorf("%+v", o)
	}
	if o := by["system_reboot"]; o.Meta["skipped"] == nil {
		t.Errorf("%+v", o)
	}

	for _, c := range f.calls {
		for _, never := range []string{"system_reboot", "system_poweroff", "help", "downloadScreenshot",
			"data_logger_db_diag_init", "fan_diag", "eye_calibration_save", "cable_calibration_save"} {
			if c == "GET "+never {
				t.Fatalf("walk read %s", never)
			}
		}
		if strings.HasPrefix(c, "POST") {
			t.Fatalf("walk wrote: %s", c)
		}
	}
	if by["ptp_status"].Kind != dhsc.KindString || by["audio_program_level"].Kind != dhsc.KindFloat ||
		by["fan_speed"].Kind != dhsc.KindInt {
		t.Error("kinds")
	}
	if _, err := p.Walk(ctx, 1); err == nil {
		t.Error("slot 1")
	}
}

func TestWalkStopsWhenTheInstrumentIsGone(t *testing.T) {
	f, s := newFake(t)
	f.get["anc_session_control"] = `{"object":[1]}`
	p := connected(t, s, Options{AnswerWait: time.Second})
	s.CloseClientConnections()
	s.Close()
	objs, err := p.Walk(context.Background(), 0)
	if err == nil || !strings.Contains(err.Error(), "transport failures in a row") {
		t.Fatalf("%v", err)
	}
	if len(objs) != deadInARow {
		t.Fatalf("%d objects", len(objs))
	}
}

func TestWalkCancelled(t *testing.T) {
	_, s := newFake(t)
	p := connected(t, s, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Walk(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestWalkUnder(t *testing.T) {
	f, s := newFake(t)
	f.get["waveform_gain/tile1"] = `{"object":[1]}`
	p := connected(t, s, Options{})
	ctx := context.Background()
	objs, err := p.WalkUnder(ctx, "waveform_gain")
	if err != nil || len(objs) != 8 {
		t.Fatalf("%d %v", len(objs), err)
	}
	objs, err = p.WalkUnder(ctx, "waveform_gain.tile1")
	if err != nil || len(objs) != 1 || objs[0].Value.Int != 1 {
		t.Fatalf("%v %v", objs, err)
	}
	if _, err := p.WalkUnder(ctx, "no_such"); !errors.Is(err, dhsc.ErrUnknownLabel) {
		t.Fatal(err)
	}
}

func TestPollProfile(t *testing.T) {
	_, s := newFake(t)
	p := connected(t, s, Options{})
	ctx := context.Background()
	prof, err := p.pollProfileFor(ctx, dhsc.ValueRequest{})
	if err != nil || len(prof.Entries) != 5 {
		t.Fatalf("%+v %v", prof, err)
	}
	prof, err = p.pollProfileFor(ctx, dhsc.ValueRequest{Path: "waveform_gain,ptp_status,system_status?field=object.a"})
	if err != nil || len(prof.Entries) != 10 || prof.Entries[9].Path != "system_status?field=object.a" {
		t.Fatalf("%+v %v", prof, err)
	}
	if _, err := p.pollProfileFor(ctx, dhsc.ValueRequest{Path: "no_such"}); err == nil {
		t.Error("unknown call")
	}
	var verr *dhsc.ValidationError
	if _, err := p.pollProfileFor(ctx, dhsc.ValueRequest{Path: "system_reboot"}); !errors.As(err, &verr) {
		t.Errorf("no-read: %v", err)
	}
}

func TestWatchPolls(t *testing.T) {
	f, s := newFake(t)
	f.get["ptp_status"] = `{"object":{"state":"locked"}}`
	p := connected(t, s, Options{})
	p.SetPollInterval(10 * time.Millisecond)
	req := dhsc.ValueRequest{Slot: -1, Path: "ptp_status"}
	events := make(chan dhsc.Event, 16)
	if err := p.Subscribe(req, func(e dhsc.Event) { events <- e }); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Unsubscribe(req) }()
	first := <-events
	if first.Value.Str != `{"state":"locked"}` {
		t.Fatalf("%+v", first)
	}
	f.mu.Lock()
	f.get["ptp_status"] = `{"object":{"state":"free run"}}`
	f.mu.Unlock()
	if second := <-events; second.Value.Str != `{"state":"free run"}` {
		t.Fatalf("%+v", second)
	}
}

func TestDisconnectReportsDeviations(t *testing.T) {
	f, s := newFake(t)
	f.get["ptp_status"] = `{"state":"locked"}`
	p := connected(t, s, Options{})
	if _, err := p.GetValue(context.Background(), dhsc.ValueRequest{Path: "ptp_status"}); err != nil {
		t.Fatal(err)
	}
	if p.ComplianceProfile().Snapshot()[codec.DevUnwrapped] != 1 {
		t.Fatal("not counted")
	}
	if err := p.Disconnect(); err != nil {
		t.Fatal(err)
	}
}

func TestRecorderSeesTheExchange(t *testing.T) {
	_, s := newFake(t)
	p := (&Factory{}).New(plugin.Deps{}).(*Plugin)
	p.SetOptions(Options{})
	rec, err := transport.NewRecorder(t.TempDir() + "/r.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rec.Close() }()
	p.SetRecorder(rec)
	h, port := hostPort(t, s)
	if err := p.Connect(context.Background(), h, port); err != nil {
		t.Fatal(err)
	}
	if m := p.Metrics().Snapshot(); m.TxFrames == 0 {
		t.Fatalf("metrics: %+v", m)
	}
	_ = p.Disconnect()
}

func TestIdentityCallsAreReadable(t *testing.T) {
	p := (&Factory{}).New(plugin.Deps{}).(*Plugin)
	for _, call := range identityCalls() {
		e, tg, err := p.resolve(dhsc.ValueRequest{Path: call})
		if err != nil {
			t.Fatalf("%s: %v", call, err)
		}
		if err := e.Check(tg, false); err != nil {
			t.Fatalf("%s: %v", call, err)
		}
	}
}

func TestValidationKeepsOtherErrors(t *testing.T) {
	plain := errors.New("plain")
	if validation(plain) != plain {
		t.Fatal("rewrapped")
	}
}

func TestRefusedErrorText(t *testing.T) {
	long := strings.Repeat("x", 200)
	e := &RefusedError{Method: "GET", Call: "/api/x", Status: 500, Body: snippet([]byte(long))}
	if !strings.HasSuffix(e.Error(), "…") || errors.Is(e, ErrUnauthorized) {
		t.Fatal(e)
	}
}

// A caller that has already cancelled stops the next answer at once.
func TestAnswersAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	cancel()
	answer, done := answers(ctx)()
	defer done()
	if answer.Err() == nil {
		t.Fatal("the answer outlives a cancelled caller")
	}
}
