package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/rrcs/codec"
)

// rrcsFake is a stand-in gateway for the verb tests.
type rrcsFake struct {
	srv    *httptest.Server
	mu     sync.Mutex
	seen   []string
	answer func(call codec.Call) (codec.Value, bool)
}

func newRRCSFake(t *testing.T, answer func(codec.Call) (codec.Value, bool)) *rrcsFake {
	t.Helper()
	f := &rrcsFake{answer: answer}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		call, err := codec.DecodeCall(body)
		if err != nil {
			t.Errorf("fake gateway: %v", err)
			return
		}
		f.mu.Lock()
		f.seen = append(f.seen, call.Method)
		f.mu.Unlock()
		v, ok := f.answer(call)
		doc := codec.EncodeFault(1, "no such method")
		if ok {
			doc, _ = codec.EncodeResponse(v)
		}
		_, _ = w.Write(doc)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *rrcsFake) addr() string { return strings.TrimPrefix(f.srv.URL, "http://") }

func (f *rrcsFake) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.seen...)
}

// captureStdout runs fn and returns what it printed.
func rrcsStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string)
	go func() {
		var b bytes.Buffer
		_, _ = io.Copy(&b, r)
		done <- b.String()
	}()
	runErr := fn()
	_ = w.Close()
	os.Stdout = old
	return <-done, runErr
}

func rrcsStatusAnswer(call codec.Call) (codec.Value, bool) {
	k := call.Params[0]
	switch call.Method {
	case "GetVersion":
		return codec.Array(k, codec.Int(0), codec.String("9.0.1")), true
	case "GetState":
		return codec.Array(k, codec.Int(0), codec.String("Working")), true
	case "IsConnectedToArtist":
		return codec.Struct(codec.Member{Name: "IsConnected", Value: codec.Bool(true)}, codec.Member{Name: "TransKey", Value: k}), true
	case "GetAllPorts":
		port := codec.Struct(codec.Member{Name: "Label", Value: codec.String("CAM 1")})
		return codec.Array(k, codec.Array(port, port, port)), true
	case "GetAllNodes":
		return codec.Array(k, codec.Int(11)), true // Artist not connected
	}
	return codec.Value{}, false
}

func TestRRCSInfo(t *testing.T) {
	f := newRRCSFake(t, rrcsStatusAnswer)
	out, err := rrcsStdout(t, func() error {
		return runRRCS(context.Background(), []string{"info", f.addr()})
	})
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	for _, want := range []string{`GetVersion`, `["9.0.1"]`, `["Working"]`, `{"IsConnected":true}`, `GetConfigurationID     FAILED`} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if got := strings.Join(f.methods(), ","); got != strings.Join(rrcsInfoMethods, ",") {
		t.Errorf("methods sent: %s", got)
	}
}

func TestRRCSDiscoverJSONAndCapture(t *testing.T) {
	f := newRRCSFake(t, rrcsStatusAnswer)
	capture := filepath.Join(t.TempDir(), "sub", "discover.jsonl")
	out, err := rrcsStdout(t, func() error {
		return runRRCS(context.Background(), []string{"discover", f.addr(), "--output", "json", "--capture", capture})
	})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	var doc struct {
		Verb    string
		Results []struct {
			Method  string
			OK      bool
			Error   string
			Payload any
		}
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if doc.Verb != "discover" || len(doc.Results) != len(rrcsDiscoverMethods) {
		t.Fatalf("document %+v", doc)
	}
	for _, r := range doc.Results {
		switch r.Method {
		case "GetAllPorts":
			if !r.OK || !strings.Contains(out, `"Label":"CAM 1"`) {
				t.Errorf("GetAllPorts: %+v", r)
			}
		case "GetAllNodes":
			if r.OK || !strings.Contains(r.Error, "code 11") {
				t.Errorf("GetAllNodes: %+v", r)
			}
		default:
			if r.OK || !strings.Contains(r.Error, "fault") {
				t.Errorf("%s: %+v", r.Method, r)
			}
		}
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	// One meta line, then a request and an answer per method.
	if len(lines) != 1+2*len(rrcsDiscoverMethods) || !strings.Contains(lines[0], `"meta"`) {
		t.Errorf("capture has %d lines, first: %s", len(lines), lines[0])
	}
}

func TestRRCSDiscoverText(t *testing.T) {
	f := newRRCSFake(t, rrcsStatusAnswer)
	out, err := rrcsStdout(t, func() error {
		return runRRCS(context.Background(), []string{"discover", f.addr()})
	})
	if err != nil || !strings.Contains(out, "GetAllPorts            ok") || !strings.Contains(out, "3 entries") {
		t.Errorf("%v\n%s", err, out)
	}
}

func TestRRCSRefusals(t *testing.T) {
	ctx := context.Background()
	var val *consumer.ValidationError
	for name, args := range map[string][]string{
		"unknown verb":   {"frobnicate", "h"},
		"no host":        {"info"},
		"two hosts":      {"info", "a", "b"},
		"bad output":     {"info", "h", "--output", "xml"},
		"bad port":       {"info", "h:99999"},
		"watch no host":  {"watch"},
		"watch bad path": {"watch", "h", "--path", "a b", "--listen", "127.0.0.1:0"},
		"watch bad mode": {"watch", "h", "--alive", "loud"},
	} {
		if err := runRRCS(ctx, args); !errors.As(err, &val) {
			t.Errorf("%s: got %v, want a validation error", name, err)
		}
	}
	// Nothing answers: info must fail, not print an empty success.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	_ = ln.Close()
	_, err := rrcsStdout(t, func() error {
		return runRRCS(ctx, []string{"info", addr, "--timeout", "500ms"})
	})
	if err == nil || !strings.Contains(err.Error(), "no request was answered") {
		t.Errorf("dead gateway: %v", err)
	}
	if _, err := rrcsStdout(t, func() error { return runRRCS(ctx, nil) }); err != nil {
		t.Errorf("help: %v", err)
	}
	if err := runRRCS(ctx, []string{"watch", "h", "--listen", "256.0.0.1:1"}); err == nil {
		t.Error("unusable listen address accepted")
	}
}

func TestRRCSSize(t *testing.T) {
	tests := map[string]codec.Value{
		"2 entries":  codec.Array(codec.Array(codec.Int(1), codec.Int(2))),
		"2 elements": codec.Array(codec.Int(1), codec.Int(2)),
		"1 members":  codec.Struct(codec.Member{Name: "a", Value: codec.Int(1)}),
		"1 value":    codec.String("x"),
	}
	for want, v := range tests {
		if got := rrcsSize(v); got != want {
			t.Errorf("rrcsSize = %q, want %q", got, want)
		}
	}
	all := codec.Array(codec.Double(1.5), codec.Base64([]byte("hi")), codec.DateTime("19980717T14:08:55"))
	if got := rrcsCompact(all); got != `[1.5,"aGk=","19980717T14:08:55"]` {
		t.Errorf("rrcsCompact = %s", got)
	}
}

// watch registers, prints what the gateway sends, answers its ping, asks
// whether it is still registered, registers again when it is not, and
// unregisters when stopped.
func TestRRCSWatch(t *testing.T) {
	probe, _ := net.Listen("tcp", "127.0.0.1:0")
	listen := probe.Addr().String()
	_ = probe.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var once sync.Once
	checks := 0
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		k := call.Params[0]
		switch call.Method {
		case "RegisterForAllEvents":
			once.Do(func() {
				url := "http://127.0.0.1:" + strconv.Itoa(int(call.Params[1].Int)) + call.Params[2].Str
				go func() {
					xp, _ := codec.EncodeCall("CrosspointChange", codec.String("R0000000001"), codec.Int(1))
					alive, _ := codec.EncodeCall("GetAlive")
					for _, doc := range [][]byte{xp, alive, []byte("garbage")} {
						resp, err := http.Post(url, "text/xml", bytes.NewReader(doc))
						if err != nil {
							t.Errorf("callback: %v", err)
							return
						}
						_ = resp.Body.Close()
					}
				}()
			})
			return k, true
		case "IsRegisteredForAllEvents":
			checks++
			if checks == 2 {
				time.AfterFunc(50*time.Millisecond, cancel)
			}
			return codec.Struct(codec.Member{Name: "IsRegistered", Value: codec.Bool(checks > 1)}, codec.Member{Name: "TransKey", Value: k}), true
		case "UnregisterForAllEvents":
			return k, true
		}
		return codec.Value{}, false
	})
	out, err := rrcsStdout(t, func() error {
		return runRRCS(ctx, []string{"watch", f.addr(), "--listen", listen, "--check", "100ms", "--alive", "show", "--output", "json"})
	})
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if !strings.Contains(out, `"method":"CrosspointChange"`) || !strings.Contains(out, `"trans_key":"R0000000001"`) ||
		!strings.Contains(out, `"params":[1]`) || !strings.Contains(out, `"method":"GetAlive"`) {
		t.Errorf("output:\n%s", out)
	}
	got := strings.Join(f.methods(), ",")
	want := "RegisterForAllEvents,IsRegisteredForAllEvents,RegisterForAllEvents,IsRegisteredForAllEvents,UnregisterForAllEvents"
	if got != want {
		t.Errorf("methods %s\nwant    %s", got, want)
	}
}

// A gateway that refuses the registration ends watch with its error.
func TestRRCSWatchRegistrationRefused(t *testing.T) {
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		return codec.Array(call.Params[0], codec.Int(13)), true
	})
	err := runRRCS(context.Background(), []string{"watch", f.addr(), "--listen", "127.0.0.1:0"})
	if !errors.Is(err, &codec.CodeError{Code: codec.CodeGatewayStandby}) {
		t.Errorf("got %v, want gateway standby", err)
	}
}

// Text output, pings counted and not shown.
func TestRRCSWatchText(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		if call.Method == "RegisterForAllEvents" {
			url := "http://127.0.0.1:" + strconv.Itoa(int(call.Params[1].Int)) + call.Params[2].Str
			go func() {
				for _, m := range []string{"GetAlive", "ConfigurationChange"} {
					var params []codec.Value
					if m != "GetAlive" {
						params = append(params, codec.String("R0000000002"))
					}
					doc, _ := codec.EncodeCall(m, params...)
					if resp, err := http.Post(url, "text/xml", bytes.NewReader(doc)); err == nil {
						_ = resp.Body.Close()
					}
				}
				cancel()
			}()
		}
		return call.Params[0], true
	})
	out, err := rrcsStdout(t, func() error {
		return runRRCS(ctx, []string{"watch", f.addr(), "--listen", "127.0.0.1:0", "--check", "0"})
	})
	if err != nil || !strings.Contains(out, "ConfigurationChange") || strings.Contains(out, "GetAlive") {
		t.Errorf("%v\n%s", err, out)
	}
}
