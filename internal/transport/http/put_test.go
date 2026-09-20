package http

import (
	"context"
	"errors"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhs/internal/metrics"
)

// PutJSON is the write half of read-modify-write. Every branch is
// driven here: the body that cannot be marshalled, the URL that cannot
// be built, the token that cannot be obtained, the peer that does not
// answer, the body that is cut short, the body that is too big, the
// three shapes of a decodable answer, and the metrics count.

func putServer(t *testing.T, h stdhttp.HandlerFunc) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts
}

func TestPutJSON_RoundTripDecodesAndCounts(t *testing.T) {
	var gotMethod, gotCT, gotBody string
	ts := putServer(t, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		b := make([]byte, 64)
		n, _ := r.Body.Read(b)
		gotBody = string(b[:n])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"n":3}`))
	})
	met := metrics.NewConnector()
	c := &Client{HTTP: ts.Client(), MaxBody: DefaultMaxBody, Metrics: met}

	var dst struct {
		OK bool `json:"ok"`
		N  int  `json:"n"`
	}
	status, err := c.PutJSON(context.Background(), ts.URL+"/x", map[string]int{"a": 1}, &dst)
	if err != nil || status != 200 {
		t.Fatalf("PutJSON = %d, %v", status, err)
	}
	if gotMethod != stdhttp.MethodPut || gotCT != "application/json" || gotBody != `{"a":1}` {
		t.Errorf("request: method=%s ct=%s body=%s", gotMethod, gotCT, gotBody)
	}
	if !dst.OK || dst.N != 3 {
		t.Errorf("decoded %+v", dst)
	}
	if s := met.Snapshot(); s.TxFrames != 1 || s.RxFrames != 1 || s.TxBytes != 7 {
		t.Errorf("metrics tx=%d/%dB rx=%d — a PUT must be counted like a GET", s.TxFrames, s.TxBytes, s.RxFrames)
	}
}

func TestPutJSON_StatusOnlyWhenDstNilOrBodyEmpty(t *testing.T) {
	ts := putServer(t, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusBadRequest) // an answered write is not a transport error
	})
	c := &Client{HTTP: ts.Client()} // MaxBody 0 → DefaultMaxBody
	status, err := c.PutJSON(context.Background(), ts.URL, 1, nil)
	if err != nil || status != 400 {
		t.Fatalf("dst=nil: %d, %v", status, err)
	}
	var dst map[string]any
	status, err = c.PutJSON(context.Background(), ts.URL, 1, &dst)
	if err != nil || status != 400 || dst != nil {
		t.Fatalf("empty body with dst: %d, %v, %v", status, err, dst)
	}
}

func TestPutJSON_ErrorBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("marshal", func(t *testing.T) {
		c := &Client{HTTP: stdhttp.DefaultClient}
		if _, err := c.PutJSON(ctx, "http://127.0.0.1/", make(chan int), nil); err == nil || !strings.Contains(err.Error(), "marshal") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("build request", func(t *testing.T) {
		c := &Client{HTTP: stdhttp.DefaultClient}
		if _, err := c.PutJSON(ctx, "http://bad host/\x7f", 1, nil); err == nil || !strings.Contains(err.Error(), "build PUT") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("token source", func(t *testing.T) {
		boom := errors.New("vault down")
		c := &Client{HTTP: stdhttp.DefaultClient, TokenSource: func(context.Context) (string, error) { return "", boom }}
		if _, err := c.PutJSON(ctx, "http://127.0.0.1/", 1, nil); !errors.Is(err, boom) {
			t.Errorf("err = %v, want the token error surfaced", err)
		}
	})
	t.Run("transport", func(t *testing.T) {
		ts := putServer(t, func(stdhttp.ResponseWriter, *stdhttp.Request) {})
		url := ts.URL
		ts.Close() // nobody listens any more
		c := &Client{HTTP: stdhttp.DefaultClient}
		if _, err := c.PutJSON(ctx, url, 1, nil); err == nil || !strings.Contains(err.Error(), "PUT "+url) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("short body", func(t *testing.T) {
		// Promise 40 bytes, send 4, hang up: the read fails mid-body.
		ts := putServer(t, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			conn, _, err := w.(stdhttp.Hijacker).Hijack()
			if err != nil {
				t.Fatalf("hijack: %v", err)
			}
			_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 40\r\nContent-Type: application/json\r\n\r\n{\"a\""))
			_ = conn.(*net.TCPConn).Close()
		})
		c := &Client{HTTP: ts.Client()}
		status, err := c.PutJSON(ctx, ts.URL, 1, nil)
		if err == nil || !strings.Contains(err.Error(), "read PUT body") || status != 200 {
			t.Errorf("status=%d err=%v", status, err)
		}
	})
	t.Run("oversize", func(t *testing.T) {
		ts := putServer(t, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", 64)))
		})
		c := &Client{HTTP: ts.Client(), MaxBody: 16}
		if _, err := c.PutJSON(ctx, ts.URL, 1, nil); err == nil || !strings.Contains(err.Error(), "exceeds 16 bytes") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("decode", func(t *testing.T) {
		ts := putServer(t, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			_, _ = w.Write([]byte(`{"unexpected":1}`))
		})
		c := &Client{HTTP: ts.Client()}
		var dst struct {
			Known int `json:"known"`
		}
		if _, err := c.PutJSON(ctx, ts.URL, 1, &dst); err == nil || !strings.Contains(err.Error(), "decode PUT response") {
			t.Errorf("err = %v — an unknown field must be refused, not dropped", err)
		}
	})
}
