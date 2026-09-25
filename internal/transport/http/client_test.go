package http

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/transport"
)

func TestClientGetJSONHappyPath(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"hello":"world"}`)
	}))
	defer srv.Close()

	c := NewClient()
	var got map[string]string
	if err := c.GetJSON(context.Background(), srv.URL, &got); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if got["hello"] != "world" {
		t.Fatalf("body decoded as %v", got)
	}
}

func TestClientGetJSONRejectsNonJSONContentType(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, `{"hello":"world"}`)
	}))
	defer srv.Close()
	c := NewClient()
	var got map[string]string
	err := c.GetJSON(context.Background(), srv.URL, &got)
	if err == nil || !strings.Contains(err.Error(), "Content-Type") {
		t.Fatalf("expected Content-Type rejection, got %v", err)
	}
}

func TestClientGetJSONAcceptsCharsetSuffix(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{"hello":"world"}`)
	}))
	defer srv.Close()
	c := NewClient()
	var got map[string]string
	if err := c.GetJSON(context.Background(), srv.URL, &got); err != nil {
		t.Fatalf("charset suffix should be accepted: %v", err)
	}
}

func TestClientGetJSONHonoursMaxBody(t *testing.T) {
	big := strings.Repeat("x", 1024)
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"x":"`+big+`"}`)
	}))
	defer srv.Close()
	c := NewClient()
	c.MaxBody = 64
	var got map[string]string
	err := c.GetJSON(context.Background(), srv.URL, &got)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected body-cap rejection, got %v", err)
	}
}

func TestClientGetJSONNon200(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusNotFound)
		_, _ = io.WriteString(w, `Not Found`)
	}))
	defer srv.Close()
	c := NewClient()
	var got map[string]string
	err := c.GetJSON(context.Background(), srv.URL, &got)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected status-bearing error, got %v", err)
	}
}

func TestTheExchangeIsCapturedBothWays(t *testing.T) {
	// A REST connector has no frames of its own, so without this its
	// capture file is a header and nothing else — and a wire nobody
	// can replay cannot meet ADR-0025 #6.
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hello":"module"}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	rec, err := transport.NewRecorder(filepath.Join(dir, "raw.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{HTTP: srv.Client(), MaxBody: DefaultMaxBody, Recorder: rec, Proto: "mnset"}
	if _, err := c.GetBytes(context.Background(), srv.URL+"/self/information"); err != nil {
		t.Fatalf("GetBytes: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "raw.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var tx, rx int
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var r struct {
			Proto string `json:"proto"`
			Dir   string `json:"dir"`
			Hex   string `json:"hex"`
		}
		if err := json.Unmarshal([]byte(line), &r); err != nil || r.Dir == "" {
			continue // the header line
		}
		if r.Proto != "mnset" {
			t.Errorf("proto = %q", r.Proto)
		}
		raw, err := hex.DecodeString(r.Hex)
		if err != nil {
			t.Fatalf("hex: %v", err)
		}
		switch r.Dir {
		case "tx":
			tx++
			if !strings.Contains(string(raw), "GET /self/information") {
				t.Errorf("the request went out as %q", raw)
			}
		case "rx":
			rx++
			if !strings.Contains(string(raw), `{"hello":"module"}`) {
				t.Errorf("the reply's body is not in the capture: %q", raw)
			}
		}
	}
	if tx != 1 || rx != 1 {
		t.Errorf("captured %d request(s) and %d reply(ies)", tx, rx)
	}
}

func TestCaptureIsOptionalAndNamed(t *testing.T) {
	// No recorder: nothing is captured and nothing fails.
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), MaxBody: DefaultMaxBody}
	if _, err := c.GetBytes(context.Background(), srv.URL); err != nil {
		t.Fatalf("GetBytes without a recorder: %v", err)
	}
	// An unnamed connector still reads as something in the file.
	if got := (&Client{}).protoName(); got != "http" {
		t.Errorf("protoName = %q", got)
	}
	if got := (&Client{Proto: "ccm"}).protoName(); got != "ccm" {
		t.Errorf("protoName = %q", got)
	}
}
