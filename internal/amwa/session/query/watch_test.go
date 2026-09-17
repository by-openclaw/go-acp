package query_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
	_ "dhs/internal/amwa/codec/is04/v13" // register the v1.3 codec
	"dhs/internal/amwa/session/query"
)

func v13(t *testing.T) is04.Codec {
	t.Helper()
	c, ok := is04.Get("v1.3")
	if !ok {
		t.Fatal("is04 v1.3 codec not registered")
	}
	return c
}

func TestSubscribePostsSpecShape(t *testing.T) {
	var gotPath, gotMethod, gotCT string
	var gotBody map[string]any

	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		gotPath, gotMethod, gotCT = r.URL.Path, r.Method, r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(stdhttp.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"sub-1","ws_href":"ws://example.invalid/x-nmos/query/v1.3/subscriptions/sub-1/ws",
		  "resource_path":"/senders/","params":{},"persist":true,"max_update_rate_ms":100,"secure":false}`))
	}))
	defer srv.Close()

	c, err := query.NewClient(srv.URL, v13(t))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	sub, err := c.Subscribe(context.Background(), query.SubscribeRequest{
		ResourcePath:  "/senders/",
		Params:        map[string]string{"label": "VTX-01"},
		Persist:       true,
		MaxUpdateRate: 100,
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if gotMethod != stdhttp.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if want := "/x-nmos/query/v1.3/subscriptions"; gotPath != want {
		t.Errorf("path = %s, want %s", gotPath, want)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q, want application/json", gotCT)
	}
	// The four keys IS-04 requires on a subscription request.
	for _, k := range []string{"max_update_rate_ms", "resource_path", "params", "persist"} {
		if _, ok := gotBody[k]; !ok {
			t.Errorf("request body missing %q: %v", k, gotBody)
		}
	}
	if got := gotBody["resource_path"]; got != "/senders" {
		t.Errorf("resource_path = %v, want /senders (spec form, no trailing slash)", got)
	}
	if sub.ID != "sub-1" || !strings.HasSuffix(sub.WSHref, "/ws") {
		t.Errorf("subscription = %+v", sub)
	}
}

func TestSubscribeAddsLeadingSlash(t *testing.T) {
	var got string
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		var body map[string]any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		got, _ = body["resource_path"].(string)
		w.WriteHeader(stdhttp.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"s","ws_href":"ws://x/ws"}`))
	}))
	defer srv.Close()

	c, _ := query.NewClient(srv.URL, v13(t))
	if _, err := c.Subscribe(context.Background(), query.SubscribeRequest{ResourcePath: "senders/"}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if got != "/senders" {
		t.Errorf("resource_path = %q, want /senders (leading slash added, trailing removed)", got)
	}
}

func TestSubscribeAcceptsExistingSubscription(t *testing.T) {
	// A Registry may answer 200 with an existing subscription rather than 201.
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusOK)
		_, _ = w.Write([]byte(`{"id":"existing","ws_href":"ws://x/ws"}`))
	}))
	defer srv.Close()

	c, _ := query.NewClient(srv.URL, v13(t))
	sub, err := c.Subscribe(context.Background(), query.SubscribeRequest{ResourcePath: "/nodes/"})
	if err != nil {
		t.Fatalf("200 must be accepted: %v", err)
	}
	if sub.ID != "existing" {
		t.Errorf("id = %q, want existing", sub.ID)
	}
}

func TestSubscribeRejects(t *testing.T) {
	t.Run("no resource path", func(t *testing.T) {
		c, _ := query.NewClient("http://example.invalid", v13(t))
		if _, err := c.Subscribe(context.Background(), query.SubscribeRequest{}); err == nil {
			t.Error("empty resource_path must be rejected")
		}
	})

	t.Run("no ws_href", func(t *testing.T) {
		srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
			w.WriteHeader(stdhttp.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"s","resource_path":"/nodes/"}`))
		}))
		defer srv.Close()
		c, _ := query.NewClient(srv.URL, v13(t))
		_, err := c.Subscribe(context.Background(), query.SubscribeRequest{ResourcePath: "/nodes/"})
		if !errors.Is(err, query.ErrNoWSHref) {
			t.Errorf("err = %v, want ErrNoWSHref — a subscription with no endpoint is useless", err)
		}
	})

	t.Run("server error", func(t *testing.T) {
		srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
			w.WriteHeader(stdhttp.StatusInternalServerError)
		}))
		defer srv.Close()
		c, _ := query.NewClient(srv.URL, v13(t))
		if _, err := c.Subscribe(context.Background(), query.SubscribeRequest{ResourcePath: "/nodes/"}); err == nil {
			t.Error("500 must be an error")
		}
	})

	t.Run("malformed body", func(t *testing.T) {
		srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
			w.WriteHeader(stdhttp.StatusCreated)
			_, _ = w.Write([]byte(`{`))
		}))
		defer srv.Close()
		c, _ := query.NewClient(srv.URL, v13(t))
		if _, err := c.Subscribe(context.Background(), query.SubscribeRequest{ResourcePath: "/nodes/"}); err == nil {
			t.Error("undecodable body must be an error")
		}
	})
}

func TestWatchRejectsBadInput(t *testing.T) {
	if err := query.Watch(context.Background(), "", func(*is04.Grain) error { return nil }, query.WatchOptions{}); !errors.Is(err, query.ErrNoWSHref) {
		t.Errorf("empty ws_href: err = %v, want ErrNoWSHref", err)
	}
	if err := query.Watch(context.Background(), "ws://example.invalid/ws", nil, query.WatchOptions{}); err == nil {
		t.Error("nil GrainFunc must be rejected")
	}
}

func TestWatchDialFailureIsReported(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := query.Watch(ctx, "ws://127.0.0.1:1/ws", func(*is04.Grain) error { return nil }, query.WatchOptions{})
	if err == nil {
		t.Fatal("dialling a closed port must fail")
	}
	if !strings.Contains(err.Error(), "dial") {
		t.Errorf("err = %v, want it to name the dial failure", err)
	}
}
