package http

import (
	"context"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhs/internal/transport"
)

type roundTripFunc func(*stdhttp.Request) (*stdhttp.Response, error)

func (f roundTripFunc) RoundTrip(r *stdhttp.Request) (*stdhttp.Response, error) { return f(r) }

func TestExchangeReturnsANon2xxAnswerWithItsBody(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method != stdhttp.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		w.WriteHeader(stdhttp.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"gone"}`)
	}))
	defer srv.Close()

	status, body, err := NewClient().Exchange(context.Background(), stdhttp.MethodDelete, srv.URL+"/x", nil, nil)
	if err != nil {
		t.Fatalf("a 404 is an answer, not an error: %v", err)
	}
	if status != 404 || string(body) != `{"error":"gone"}` {
		t.Errorf("status %d body %s", status, body)
	}
}

func TestExchangeWritesHeaderNamesAsGivenAndDeclaresAZeroLength(t *testing.T) {
	var gotKeys []string
	var gotLen int64 = -1
	c := NewClient()
	c.HTTP.Transport = roundTripFunc(func(r *stdhttp.Request) (*stdhttp.Response, error) {
		for k := range r.Header {
			gotKeys = append(gotKeys, k)
		}
		gotLen = r.ContentLength
		return &stdhttp.Response{StatusCode: 200, Header: stdhttp.Header{}, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})

	if _, _, err := c.Exchange(context.Background(), stdhttp.MethodPost, "http://peer/x", map[string]string{"reqid": "7"}, nil); err != nil {
		t.Fatal(err)
	}
	if len(gotKeys) != 1 || gotKeys[0] != "reqid" {
		t.Errorf("header names sent = %v, want exactly [reqid]", gotKeys)
	}
	if gotLen != 0 {
		t.Errorf("Content-Length = %d, want 0 for a request without a body", gotLen)
	}
}

func TestExchangeRefusesABodyOverTheCap(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 64))
	}))
	defer srv.Close()

	c := NewClient()
	c.MaxBody = 16
	if _, _, err := c.Exchange(context.Background(), stdhttp.MethodGet, srv.URL, nil, nil); err == nil {
		t.Error("a 64-byte answer passed a 16-byte cap")
	}
}

func TestNewTLSClientCarriesThePosture(t *testing.T) {
	c, err := NewTLSClient(transport.TLSOptions{Enable: true, Insecure: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := c.HTTP.Transport.(*stdhttp.Transport)
	if !ok || tr.TLSClientConfig == nil || !tr.TLSClientConfig.InsecureSkipVerify || tr.TLSClientConfig.MinVersion != transport.MinTLSVersion {
		t.Errorf("transport = %+v", c.HTTP.Transport)
	}
	if c.HTTP.Timeout != DefaultTimeout {
		t.Errorf("timeout = %s, want the default", c.HTTP.Timeout)
	}

	plain, err := NewTLSClient(transport.TLSOptions{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if tr := plain.HTTP.Transport.(*stdhttp.Transport); tr.TLSClientConfig != nil {
		t.Error("a client without TLS carries a TLS config")
	}

	if _, err := NewTLSClient(transport.TLSOptions{Enable: true, CAFile: "does-not-exist.pem"}, 0); err == nil {
		t.Error("an unreadable CA file was accepted")
	}
}
