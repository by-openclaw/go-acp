package query_test

// A Registry that guards its Query API (IS-10 / BCP-003-02) guards
// the subscription request and the subscription socket alike. The
// client carries the token on both, and does not go out without one
// when it was told to carry it.

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/session/query"
)

var errNoToken = errors.New("authorization server unreachable")

func TestSubscribeAndWatchCarryTheToken(t *testing.T) {
	var subAuth, wsAuth atomic.Value
	var srv *httptest.Server
	srv = httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if strings.HasSuffix(r.URL.Path, "/subscriptions") {
			subAuth.Store(r.Header.Get("Authorization"))
			w.WriteHeader(stdhttp.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"s","ws_href":"` + strings.Replace(srv.URL, "http://", "ws://", 1) + `/ws"}`))
			return
		}
		// The socket: the handshake's header is all this test reads.
		wsAuth.Store(r.Header.Get("Authorization"))
		w.WriteHeader(stdhttp.StatusUnauthorized)
	}))
	defer srv.Close()

	c, err := query.NewClient(srv.URL, v13(t))
	if err != nil {
		t.Fatal(err)
	}
	token := func(context.Context) (string, error) { return "tok-1", nil }
	c.HTTP.TokenSource = token
	sub, err := c.Subscribe(context.Background(), query.SubscribeRequest{ResourcePath: "/nodes"})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if got, _ := subAuth.Load().(string); got != "Bearer tok-1" {
		t.Errorf("subscription request Authorization = %q, want Bearer tok-1", got)
	}
	// The server refuses the upgrade after reading the header: Watch
	// reports the dial, and the header it sent is what is checked.
	err = query.Watch(context.Background(), sub.WSHref, func(*is04.Grain) error { return nil },
		query.WatchOptions{TokenSource: token})
	if err == nil {
		t.Error("Watch against a refused upgrade returned nil")
	}
	if got, _ := wsAuth.Load().(string); got != "Bearer tok-1" {
		t.Errorf("socket handshake Authorization = %q, want Bearer tok-1", got)
	}
}

func TestSubscribeAndWatchDoNotGoOutWithoutTheToken(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(stdhttp.ResponseWriter, *stdhttp.Request) { hits.Add(1) }))
	defer srv.Close()

	c, err := query.NewClient(srv.URL, v13(t))
	if err != nil {
		t.Fatal(err)
	}
	failing := func(context.Context) (string, error) { return "", errNoToken }
	c.HTTP.TokenSource = failing
	if _, err := c.Subscribe(context.Background(), query.SubscribeRequest{ResourcePath: "/nodes"}); !errors.Is(err, errNoToken) {
		t.Errorf("Subscribe = %v, want the token error", err)
	}
	ws := strings.Replace(srv.URL, "http://", "ws://", 1) + "/ws"
	err = query.Watch(context.Background(), ws, func(*is04.Grain) error { return nil },
		query.WatchOptions{TokenSource: failing})
	if !errors.Is(err, errNoToken) {
		t.Errorf("Watch = %v, want the token error", err)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("%d requests reached the registry without a token, want 0", n)
	}
}
