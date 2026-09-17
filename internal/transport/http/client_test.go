package http

import (
	"context"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
