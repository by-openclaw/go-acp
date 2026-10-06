package consumer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/rrcs/codec"
	"dhs/internal/wiretrace"
)

// fakeRRCS answers each method with the value its handler returns, the
// way a gateway does: HTTP POST, text/xml, one value per answer.
type fakeRRCS struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	calls   []codec.Call
	headers []http.Header
	answer  func(call codec.Call) (codec.Value, *codec.Fault)
}

func newFakeRRCS(t *testing.T, answer func(codec.Call) (codec.Value, *codec.Fault)) *fakeRRCS {
	t.Helper()
	f := &fakeRRCS{t: t, answer: answer}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeRRCS) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	call, err := codec.DecodeCall(body)
	if err != nil {
		f.t.Errorf("fake RRCS: request not decodable: %v", err)
		http.Error(w, "bad", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.headers = append(f.headers, r.Header.Clone())
	f.mu.Unlock()

	v, fault := f.answer(call)
	var doc []byte
	if fault != nil {
		doc = codec.EncodeFault(fault.Code, fault.String)
	} else if doc, err = codec.EncodeResponse(v); err != nil {
		f.t.Errorf("fake RRCS: %v", err)
	}
	w.Header().Set("Content-Type", "text/xml")
	_, _ = w.Write(doc)
}

func (f *fakeRRCS) addr() string { return strings.TrimPrefix(f.srv.URL, "http://") }

func (f *fakeRRCS) client(t *testing.T, cfg Config) *Client {
	t.Helper()
	cfg.Addr = f.addr()
	if cfg.Seed == 0 {
		cfg.Seed = 2817190 // the next key is the §11.1 key C0002817191
	}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// key returns the transaction key of a request.
func key(call codec.Call) codec.Value {
	if len(call.Params) == 0 {
		return codec.String("")
	}
	return call.Params[0]
}

func TestHostPort(t *testing.T) {
	good := map[string]string{
		"10.1.2.3":        "10.1.2.3:8193",
		"10.1.2.3:9000":   "10.1.2.3:9000",
		"rrcs.example":    "rrcs.example:8193",
		"[fd00::1]":       "[fd00::1]:8193",
		"[fd00::1]:8194":  "[fd00::1]:8194",
		"rrcs.example:80": "rrcs.example:80",
	}
	for in, want := range good {
		if got, err := hostPort(in); err != nil || got != want {
			t.Errorf("hostPort(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", ":8193", "host:0", "host:70000", "host:abc"} {
		if _, err := hostPort(bad); err == nil {
			t.Errorf("hostPort(%q): no error", bad)
		}
	}
}

func TestNewClientDefaultsAndRefusals(t *testing.T) {
	c, err := NewClient(Config{Addr: "10.1.2.3"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.Peer() != "10.1.2.3:8193" || c.timeout != DefaultTimeout || c.prefix != DefaultPrefix || c.seq.Load() == 0 {
		t.Errorf("defaults: %+v", c)
	}
	if _, err := NewClient(Config{Addr: ""}); err == nil {
		t.Error("empty address accepted")
	}
	if _, err := NewClient(Config{Addr: "h", Prefix: codec.RRCSPrefix}); err == nil {
		t.Error("the prefix RRCS keeps for itself was accepted")
	}
}

// The request for §11.1 leaves as the specification prints it, with the
// headers §5.5 demands.
func TestCallSendsSpecRequest(t *testing.T) {
	var sent []byte
	f := newFakeRRCS(t, func(call codec.Call) (codec.Value, *codec.Fault) {
		return codec.Array(key(call), codec.Int(0)), nil
	})
	c := f.client(t, Config{Tap: func(dir wiretrace.Direction, peer string, doc []byte) {
		if dir == wiretrace.DirectionTx {
			sent = bytes.Clone(doc)
		}
	}})
	reply, err := c.Call(context.Background(), "SetXp",
		codec.Int(1), codec.Int(2), codec.Int(19), codec.Int(1), codec.Int(5), codec.Int(3))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	want := `<?xml version="1.0"?><methodCall><methodName>SetXp</methodName><params>` +
		`<param><value><string>C0002817191</string></value></param>` +
		`<param><value><int>1</int></value></param><param><value><int>2</int></value></param>` +
		`<param><value><int>19</int></value></param><param><value><int>1</int></value></param>` +
		`<param><value><int>5</int></value></param><param><value><int>3</int></value></param>` +
		`</params></methodCall>`
	if string(sent) != want {
		t.Errorf("sent %s\nwant %s", sent, want)
	}
	if reply.Key != "C0002817191" || reply.Echo != reply.Key || !reply.HasCode || reply.Code != codec.CodeSuccess {
		t.Errorf("reply %+v", reply)
	}
	h := f.headers[0]
	if h.Get("Content-Type") != "text/xml" || h.Get("User-Agent") == "" || h.Get("Content-Length") != strconv.Itoa(len(want)) {
		t.Errorf("headers %v", h)
	}
}

// Every answer shape the specification prints is read without loss.
func TestReplyShapes(t *testing.T) {
	const k = "C0002817191"
	port := codec.Struct(codec.Member{Name: "Label", Value: codec.String("CAM 1")})
	tests := []struct {
		name    string
		answer  codec.Value
		echo    string
		hasCode bool
		payload codec.Value
	}{
		{"§11.1 key and code", codec.Array(codec.String(k), codec.Int(0)), k, true, codec.Array()},
		{"§8.8 GetVersion", codec.Array(codec.String(k), codec.Int(0), codec.String("9.0.1")), k, true, codec.Array(codec.String("9.0.1"))},
		{"§8.8 GetAllPorts, no code", codec.Array(codec.String(k), codec.Array(port)), k, false, codec.Array(codec.Array(port))},
		{"§8.8 IsConnectedToArtist", codec.Struct(codec.Member{Name: "IsConnected", Value: codec.Bool(true)}, codec.Member{Name: "TransKey", Value: codec.String(k)}),
			k, false, codec.Struct(codec.Member{Name: "IsConnected", Value: codec.Bool(true)})},
		{"§8.7 struct with code", codec.Struct(codec.Member{Name: "ErrorCode", Value: codec.Int(0)}, codec.Member{Name: "TransKey", Value: codec.String(k)}, codec.Member{Name: "port count", Value: codec.Int(2)}),
			k, true, codec.Struct(codec.Member{Name: "port count", Value: codec.Int(2)})},
		{"§8.15.1 bare key", codec.String(k), k, false, codec.Array()},
		{"array without a key", codec.Array(codec.Int(7)), "", false, codec.Array(codec.Int(7))},
		{"empty array", codec.Array(), "", false, codec.Array()},
		{"scalar", codec.Int(7), "", false, codec.Int(7)},
		{"text that is no key", codec.String("9.0.1"), "", false, codec.String("9.0.1")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeRRCS(t, func(codec.Call) (codec.Value, *codec.Fault) { return tc.answer, nil })
			reply, err := f.client(t, Config{}).Call(context.Background(), "Any")
			if err != nil {
				t.Fatalf("Call: %v", err)
			}
			if reply.Echo != tc.echo || reply.HasCode != tc.hasCode {
				t.Errorf("echo %q code %v, want %q %v", reply.Echo, reply.HasCode, tc.echo, tc.hasCode)
			}
			got := reply.Payload()
			if got.Kind != tc.payload.Kind || len(got.Items) != len(tc.payload.Items) || len(got.Members) != len(tc.payload.Members) {
				t.Errorf("payload %+v, want %+v", got, tc.payload)
			}
			if len(got.Items)+len(got.Members) > 0 && !reflect.DeepEqual(got, tc.payload) {
				t.Errorf("payload %+v, want %+v", got, tc.payload)
			}
		})
	}
}

func TestCallErrors(t *testing.T) {
	ctx := context.Background()
	t.Run("error code", func(t *testing.T) {
		f := newFakeRRCS(t, func(call codec.Call) (codec.Value, *codec.Fault) {
			return codec.Array(key(call), codec.Int(13)), nil
		})
		reply, err := f.client(t, Config{}).Call(ctx, "SetXp")
		if !errors.Is(err, &codec.CodeError{Code: codec.CodeGatewayStandby}) {
			t.Errorf("got %v, want gateway standby", err)
		}
		if reply.Code != codec.CodeGatewayStandby {
			t.Errorf("the reply was not returned with the error: %+v", reply)
		}
		if !strings.HasPrefix(err.Error(), "SetXp: rrcs: ") {
			t.Errorf("wording: %q", err.Error())
		}
	})
	t.Run("fault", func(t *testing.T) {
		f := newFakeRRCS(t, func(codec.Call) (codec.Value, *codec.Fault) {
			return codec.Value{}, &codec.Fault{Code: 4, String: "Too many parameters."}
		})
		_, err := f.client(t, Config{}).Call(ctx, "GetObjectList")
		var fault *codec.Fault
		if !errors.As(err, &fault) || fault.Code != 4 {
			t.Errorf("got %v, want fault 4", err)
		}
	})
	t.Run("foreign key", func(t *testing.T) {
		f := newFakeRRCS(t, func(codec.Call) (codec.Value, *codec.Fault) {
			return codec.Array(codec.String("C9999999999"), codec.Int(0)), nil
		})
		if _, err := f.client(t, Config{}).Call(ctx, "GetState"); !errors.Is(err, ErrTransKey) {
			t.Errorf("got %v, want ErrTransKey", err)
		}
	})
	t.Run("not XML-RPC", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("<html>hello</html>"))
		}))
		defer srv.Close()
		c, _ := NewClient(Config{Addr: strings.TrimPrefix(srv.URL, "http://")})
		if _, err := c.Call(ctx, "GetState"); !errors.Is(err, codec.ErrMalformed) {
			t.Errorf("got %v, want ErrMalformed", err)
		}
	})
	t.Run("HTTP status", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		defer srv.Close()
		c, _ := NewClient(Config{Addr: strings.TrimPrefix(srv.URL, "http://")})
		if _, err := c.Call(ctx, "GetState"); !errors.Is(err, ErrHTTP) {
			t.Errorf("got %v, want ErrHTTP", err)
		}
	})
	t.Run("nothing listening", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := ln.Addr().String()
		_ = ln.Close()
		c, _ := NewClient(Config{Addr: addr, Timeout: time.Second})
		if _, err := c.Call(ctx, "GetState"); err == nil {
			t.Error("no error")
		}
	})
	t.Run("timeout", func(t *testing.T) {
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
		defer srv.Close()
		defer close(release)
		c, _ := NewClient(Config{Addr: strings.TrimPrefix(srv.URL, "http://"), Timeout: 50 * time.Millisecond})
		if _, err := c.Call(ctx, "GetAllPorts"); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("got %v, want a deadline error", err)
		}
	})
	t.Run("bad method name", func(t *testing.T) {
		c, _ := NewClient(Config{Addr: "127.0.0.1"})
		if _, err := c.Call(ctx, "Get State"); !errors.Is(err, codec.ErrMalformed) {
			t.Errorf("got %v, want ErrMalformed", err)
		}
	})
}

func TestCallNoKey(t *testing.T) {
	f := newFakeRRCS(t, func(codec.Call) (codec.Value, *codec.Fault) { return codec.Array(), nil })
	reply, err := f.client(t, Config{}).CallNoKey(context.Background(), "GetAlive")
	if err != nil || reply.Key != "" || len(f.calls[0].Params) != 0 {
		t.Errorf("reply %+v, %v, sent %+v", reply, err, f.calls[0])
	}
}

// Keys differ from one request to the next.
func TestKeysAdvance(t *testing.T) {
	f := newFakeRRCS(t, func(call codec.Call) (codec.Value, *codec.Fault) {
		return codec.Array(key(call), codec.Int(0)), nil
	})
	c := f.client(t, Config{Prefix: 'D'})
	a, _ := c.Call(context.Background(), "GetState")
	b, _ := c.Call(context.Background(), "GetState")
	if a.Key != "D0002817191" || b.Key != "D0002817192" {
		t.Errorf("keys %q %q", a.Key, b.Key)
	}
}

func TestRegistration(t *testing.T) {
	ctx := context.Background()
	registered := true
	f := newFakeRRCS(t, func(call codec.Call) (codec.Value, *codec.Fault) {
		switch call.Method {
		case "IsRegisteredForAllEvents":
			return codec.Struct(codec.Member{Name: "IsRegistered", Value: codec.Bool(registered)}, codec.Member{Name: "TransKey", Value: key(call)}), nil
		case "Broken":
			return codec.Struct(codec.Member{Name: "TransKey", Value: key(call)}), nil
		}
		return key(call), nil
	})
	c := f.client(t, Config{})
	reg := Registration{Port: 8195, Path: "notification", Pipelining: true}

	if _, err := c.Register(ctx, reg); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// §8.15.1: TransKey, TCPPort, URLPath, AllowHttpPipelining, AllowSystemMulticall.
	want := []codec.Value{codec.String("C0002817191"), codec.Int(8195), codec.String("/notification"), codec.Bool(true), codec.Bool(false)}
	if got := f.calls[0]; got.Method != "RegisterForAllEvents" || !reflect.DeepEqual(got.Params, want) {
		t.Errorf("sent %+v", got)
	}

	if ok, err := c.IsRegistered(ctx, reg); err != nil || !ok {
		t.Errorf("IsRegistered = %v, %v", ok, err)
	}
	registered = false
	if ok, err := c.IsRegistered(ctx, reg); err != nil || ok {
		t.Errorf("IsRegistered = %v, %v", ok, err)
	}

	if _, err := c.Unregister(ctx, reg); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	// §8.15.2: TransKey, TCPPort, URLPath.
	last := f.calls[len(f.calls)-1]
	if last.Method != "UnregisterForAllEvents" || len(last.Params) != 3 || last.Params[1].Int != 8195 || last.Params[2].Str != "/notification" {
		t.Errorf("sent %+v", last)
	}

	for _, bad := range []Registration{{Port: 0}, {Port: 70000}, {Port: 80, Path: "a b"}, {Port: 80, Path: "café"}} {
		if _, err := c.Register(ctx, bad); err == nil {
			t.Errorf("Register(%+v): no error", bad)
		}
		if _, err := c.Unregister(ctx, bad); err == nil {
			t.Errorf("Unregister(%+v): no error", bad)
		}
		if _, err := c.IsRegistered(ctx, bad); err == nil {
			t.Errorf("IsRegistered(%+v): no error", bad)
		}
	}
}

func TestIsRegisteredRefusesOddAnswers(t *testing.T) {
	ctx := context.Background()
	answers := map[string]codec.Value{
		"no member":  codec.Struct(codec.Member{Name: "Other", Value: codec.Bool(true)}),
		"wrong type": codec.Struct(codec.Member{Name: "IsRegistered", Value: codec.Int(1)}),
	}
	for name, v := range answers {
		f := newFakeRRCS(t, func(codec.Call) (codec.Value, *codec.Fault) { return v, nil })
		if _, err := f.client(t, Config{}).IsRegistered(ctx, Registration{Port: 80}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	f := newFakeRRCS(t, func(call codec.Call) (codec.Value, *codec.Fault) {
		return codec.Array(key(call), codec.Int(11)), nil
	})
	if _, err := f.client(t, Config{}).IsRegistered(ctx, Registration{Port: 80}); !errors.Is(err, &codec.CodeError{Code: codec.ErrorCode(11)}) {
		t.Errorf("got %v, want code 11", err)
	}
}

func TestPaths(t *testing.T) {
	for in, want := range map[string]string{"": "/RPC2", "RPC2": "/RPC2", "/x/y": "/x/y", "notification": "/notification"} {
		if got := NormalizePath(in); got != want {
			t.Errorf("NormalizePath(%q) = %q, want %q", in, got, want)
		}
	}
	for _, good := range []string{"", "/RPC2", "a-b_c.d~e"} {
		if !ValidPath(good) {
			t.Errorf("ValidPath(%q) = false", good)
		}
	}
	for _, bad := range []string{"a b", "a\tb", "a\x7f", "é"} {
		if ValidPath(bad) {
			t.Errorf("ValidPath(%q) = true", bad)
		}
	}
}

// post sends doc to the listener the way RRCS does.
func post(t *testing.T, url, doc string) (int, string) {
	t.Helper()
	resp, err := http.Post(url, "text/xml", strings.NewReader(doc))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// The §11.2 notification gets the §11.2 answer.
func TestListenerSpecNotification(t *testing.T) {
	var got []Event
	var taps []wiretrace.Direction
	at := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	l := &Listener{
		OnEvent: func(e Event) { got = append(got, e) },
		Tap:     func(dir wiretrace.Direction, _ string, _ []byte) { taps = append(taps, dir) },
		Now:     func() time.Time { return at },
	}
	srv := httptest.NewServer(l)
	defer srv.Close()

	request := `<?xml version="1.0"?>
<methodCall>
     <methodName>UpstreamFailed</methodName>
     <params>
          <param>
               <value><string>R1947584733</string></value>
          </param>
          <param>
               <value><int>2</int></value></param>
          </params>
</methodCall>`
	status, body := post(t, srv.URL+"/RPC2", request)
	want := `<?xml version="1.0"?><methodResponse><params><param><value><array><data>` +
		`<value><string>R1947584733</string></value><value><int>0</int></value>` +
		`</data></array></value></param></params></methodResponse>`
	if status != http.StatusOK || body != want {
		t.Errorf("status %d body %s\nwant %s", status, body, want)
	}
	if len(got) != 1 || got[0].Method != "UpstreamFailed" || got[0].TransKey != "R1947584733" ||
		len(got[0].Params) != 2 || got[0].Params[1].Int != 2 || !got[0].Time.Equal(at) || got[0].Remote == "" {
		t.Errorf("event %+v", got)
	}
	if l.Events() != 1 || l.Alives() != 0 {
		t.Errorf("counters %d %d", l.Events(), l.Alives())
	}
	if !reflect.DeepEqual(taps, []wiretrace.Direction{wiretrace.DirectionRx, wiretrace.DirectionTx}) {
		t.Errorf("taps %v", taps)
	}
}

// §5.5: the answer carries its Content-Length and is not chunked.
func TestListenerAnswerHasContentLength(t *testing.T) {
	srv := httptest.NewServer(&Listener{})
	defer srv.Close()
	for _, doc := range []string{
		`<methodCall><methodName>ConnectArtistRestored</methodName><params><param><value><string>R0000000000</string></value></param><param><value><string>Working</string></value></param></params></methodCall>`,
		`garbage`,
	} {
		resp, err := http.Post(srv.URL+"/RPC2", "text/xml", strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.ContentLength != int64(len(body)) || len(resp.TransferEncoding) != 0 {
			t.Errorf("Content-Length %d, body %d bytes, transfer encoding %v", resp.ContentLength, len(body), resp.TransferEncoding)
		}
	}
}

// §9.8: GetAlive has no parameter and must be answered.
func TestListenerGetAlive(t *testing.T) {
	l := &Listener{Path: "notification"}
	srv := httptest.NewServer(l)
	defer srv.Close()
	status, body := post(t, srv.URL+"/notification", `<?xml version="1.0"?><methodCall><methodName>GetAlive</methodName></methodCall>`)
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	resp, err := codec.DecodeResponse([]byte(body))
	if err != nil || resp.Fault != nil {
		t.Fatalf("answer %s: %v", body, err)
	}
	if l.Alives() != 1 || l.Events() != 0 {
		t.Errorf("counters %d %d", l.Events(), l.Alives())
	}
}

func TestListenerRefusals(t *testing.T) {
	var rejected []error
	l := &Listener{OnReject: func(_ string, err error) { rejected = append(rejected, err) }}
	srv := httptest.NewServer(l)
	defer srv.Close()

	if status, _ := post(t, srv.URL+"/other", "<methodCall/>"); status != http.StatusNotFound {
		t.Errorf("wrong path: status %d", status)
	}
	resp, err := http.Get(srv.URL + "/RPC2")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET: status %d", resp.StatusCode)
	}

	status, body := post(t, srv.URL+"/RPC2", "not xml at all")
	answer, derr := codec.DecodeResponse([]byte(body))
	if status != http.StatusOK || derr != nil || answer.Fault == nil {
		t.Errorf("malformed request: status %d body %s", status, body)
	}
	if len(rejected) != 1 || !errors.Is(rejected[0], codec.ErrMalformed) {
		t.Errorf("rejected %v", rejected)
	}
	if l.Events() != 0 {
		t.Errorf("a malformed request was counted as an event")
	}
	// The zero value, without handlers, still answers.
	plain := httptest.NewServer(&Listener{})
	defer plain.Close()
	if status, _ := post(t, plain.URL+"/RPC2", "garbage"); status != http.StatusOK {
		t.Errorf("zero value: status %d", status)
	}
	if status, _ := post(t, plain.URL+"/RPC2", `<methodCall><methodName>ConfigurationChange</methodName><params><param><value>R0000000001</value></param></params></methodCall>`); status != http.StatusOK {
		t.Errorf("zero value: status %d", status)
	}
}

// The whole loop: we register, the gateway calls back, we answer.
func TestRegisterThenReceive(t *testing.T) {
	events := make(chan Event, 4)
	l := &Listener{OnEvent: func(e Event) { events <- e }}
	ours := httptest.NewServer(l)
	defer ours.Close()
	_, portText, _ := net.SplitHostPort(strings.TrimPrefix(ours.URL, "http://"))
	port, _ := strconv.Atoi(portText)

	f := newFakeRRCS(t, func(call codec.Call) (codec.Value, *codec.Fault) {
		if call.Method == "RegisterForAllEvents" {
			url := "http://127.0.0.1:" + strconv.Itoa(int(call.Params[1].Int)) + call.Params[2].Str
			for _, method := range []string{"ConfigurationChange", "GetAlive"} {
				var params []codec.Value
				if method != "GetAlive" {
					params = append(params, codec.String("R0000000001"))
				}
				doc, _ := codec.EncodeCall(method, params...)
				resp, err := http.Post(url, "text/xml", bytes.NewReader(doc))
				if err != nil {
					t.Errorf("callback: %v", err)
					continue
				}
				_ = resp.Body.Close()
			}
		}
		return key(call), nil
	})
	if _, err := f.client(t, Config{}).Register(context.Background(), Registration{Port: port}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	for _, want := range []string{"ConfigurationChange", "GetAlive"} {
		select {
		case e := <-events:
			if e.Method != want {
				t.Errorf("got %s, want %s", e.Method, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no %s", want)
		}
	}
	if l.Events() != 1 || l.Alives() != 1 {
		t.Errorf("counters %d %d", l.Events(), l.Alives())
	}
}

// §8.12: TransKey, TCPPort, URLPath, Node, Port, EventInfo.
func TestPanelSpy(t *testing.T) {
	f := newFakeRRCS(t, func(call codec.Call) (codec.Value, *codec.Fault) { return key(call), nil })
	c := f.client(t, Config{})
	reg := Registration{Port: 8195}
	if _, err := c.PanelSpy(context.Background(), reg, 61, 1026, true); err != nil {
		t.Fatalf("PanelSpy: %v", err)
	}
	got := f.calls[0]
	if got.Method != "ChangePanelSpyRegistry" || len(got.Params) != 6 || got.Params[1].Int != 8195 ||
		got.Params[2].Str != "/RPC2" || got.Params[3].Int != 61 || got.Params[4].Int != 1026 {
		t.Fatalf("sent %+v", got)
	}
	for _, name := range []string{"RotateEventsOn", "KeyEventsOn", "FuncKeyEventsOn", "NumKeyEventsOn"} {
		if v, ok := got.Params[5].Field(name); !ok || !v.Bool {
			t.Errorf("EventInfo.%s = %+v, %v", name, v, ok)
		}
	}
	if _, err := c.PanelSpy(context.Background(), reg, 61, 1026, false); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.calls[1].Params[5].Field("KeyEventsOn"); v.Bool {
		t.Error("off was sent as on")
	}
	if _, err := c.PanelSpy(context.Background(), reg, -1, 0, true); !errors.Is(err, codec.ErrRange) {
		t.Errorf("negative node: %v", err)
	}
	if _, err := c.PanelSpy(context.Background(), Registration{}, 1, 1, true); err == nil {
		t.Error("an empty registration was accepted")
	}
}
