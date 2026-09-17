package http

import (
	"context"
	"errors"
	stdhttp "net/http"
	"strings"
	"testing"
)

// The body a caller decodes itself comes back verbatim — including a
// content type that is not JSON at all, which is the whole reason GetBytes
// exists (a Neuron serves its OpenAPI document as YAML).
func TestGetBytesReturnsRawBody(t *testing.T) {
	const yaml = "openapi: 3.0.0\ninfo:\n  title: neuron\n"
	srv := jsonServer(t, func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte(yaml))
	})

	body, err := NewClient().GetBytes(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("GetBytes: %v", err)
	}
	if string(body) != yaml {
		t.Errorf("body = %q, want %q", body, yaml)
	}
}

// The reason this is not an io.ReadAll at the call site.
func TestGetBytesHonoursMaxBody(t *testing.T) {
	srv := jsonServer(t, okJSON(strings.Repeat("a", 4096)))
	c := NewClient()
	c.MaxBody = 16

	_, err := c.GetBytes(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("err = %v, want an exceeds-cap error", err)
	}
}

func TestGetBytesZeroMaxBodyUsesDefault(t *testing.T) {
	srv := jsonServer(t, okJSON(`hello`))
	c := &Client{HTTP: &stdhttp.Client{}}

	body, err := c.GetBytes(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("GetBytes: %v", err)
	}
	if string(body) != "hello" {
		t.Errorf("body = %q", body)
	}
}

// A non-200 carries the status AND the peer's explanation — a device that
// says why it refused is more useful than a bare code.
func TestGetBytesNon200CarriesTheBody(t *testing.T) {
	srv := jsonServer(t, func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusNotFound)
		_, _ = w.Write([]byte("no such resource"))
	})

	_, err := NewClient().GetBytes(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("GetBytes on a 404 returned nil")
	}
	if !strings.Contains(err.Error(), "HTTP 404") ||
		!strings.Contains(err.Error(), "no such resource") {
		t.Errorf("err = %v, want the status and the peer's message", err)
	}
}

func TestGetBytesTokenSourceError(t *testing.T) {
	srv := jsonServer(t, okJSON(`{}`))
	c := NewClient()
	c.TokenSource = func(context.Context) (string, error) {
		return "", errors.New("no token")
	}

	if _, err := c.GetBytes(context.Background(), srv.URL); err == nil ||
		!strings.Contains(err.Error(), "obtain access token") {
		t.Errorf("err = %v, want an access-token error", err)
	}
}

func TestGetBytesRejectsUnbuildableURL(t *testing.T) {
	_, err := NewClient().GetBytes(context.Background(), "http://exa\nmple.com")
	if err == nil || !strings.Contains(err.Error(), "build request") {
		t.Errorf("err = %v, want a build-request error", err)
	}
}

func TestGetBytesReportsTransportFailure(t *testing.T) {
	srv := jsonServer(t, okJSON(`{}`))
	url := srv.URL
	srv.Close()

	if _, err := NewClient().GetBytes(context.Background(), url); err == nil {
		t.Error("GetBytes to a closed server returned nil")
	}
}

func TestGetBytesReportsReadFailure(t *testing.T) {
	srv := jsonServer(t, func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.Header().Set("Content-Length", "64")
		_, _ = w.Write([]byte("short"))
	})

	_, err := NewClient().GetBytes(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "read body") {
		t.Errorf("err = %v, want a read-body error", err)
	}
}
