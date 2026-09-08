package query_test

import (
	"context"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	amwahttp "dhs/internal/amwa/session/http"
	"dhs/internal/amwa/session/query"
)

// subServer combines the two halves ListViaSubscription needs: a POST
// /subscriptions endpoint whose ws_href points back at this same
// server's /ws upgrade endpoint, which then pushes the frames in `push`
// and closes. wsHrefOverride, when set, replaces the advertised ws_href
// (used to point the client at a dead port for the dial-failure case).
func subServer(t *testing.T, status int, wsHrefOverride string, push func(ws *amwahttp.WebSocket)) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if strings.HasSuffix(r.URL.Path, "/ws") {
			ws, err := amwahttp.AcceptWebSocket(w, r)
			if err != nil {
				return
			}
			if push != nil {
				push(ws)
			}
			return
		}
		// POST /subscriptions
		if status != stdhttp.StatusOK && status != stdhttp.StatusCreated {
			w.WriteHeader(status)
			return
		}
		href := wsHrefOverride
		if href == "" {
			href = "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, `{"id":"sub-1","ws_href":%q}`, href)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestListViaSubscriptionSnapshot: the SYNC snapshot delivered over the
// subscription WS is the listing. The reader must key by resource path,
// keep only rows whose post is a real object (a null post is a removal,
// not a member), ignore grains for other topics, and tolerate an
// undecodable frame — all exercised in one stream.
func TestListViaSubscriptionSnapshot(t *testing.T) {
	srv := subServer(t, stdhttp.StatusCreated, "", func(ws *amwahttp.WebSocket) {
		_ = ws.SendText([]byte(`not json at all`))                                                             // tolerated
		_ = ws.SendText([]byte(`{"grain":{"topic":"/receivers/","data":[{"path":"r1","post":{"id":"r1"}}]}}`)) // wrong topic
		_ = ws.SendText([]byte(`{"grain":{"topic":"/senders/","data":[` +
			`{"path":"s-1","post":{"id":"s-1"}},` + // kept
			`{"path":"s-2","post":null}]}}`)) // null post → skipped
		_ = ws.SendText([]byte(`{"grain":{"topic":"/senders/","data":[{"path":"s-1","post":{"id":"s-1"}}]}}`)) // dup path
		time.Sleep(50 * time.Millisecond)
		_ = ws.Close()
	})

	c, _ := query.NewClient(srv.URL, v13(t))
	got, err := c.ListViaSubscription(context.Background(), "senders")
	if err != nil {
		t.Fatalf("ListViaSubscription: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d senders, want 1 (dup deduped, null-post skipped, wrong topic ignored)", len(got))
	}
	if !strings.Contains(string(got[0]), "s-1") {
		t.Fatalf("unexpected snapshot member %s", got[0])
	}
}

// TestListViaSubscriptionHTTPError: a non-2xx to the subscription POST
// is fatal — there is no socket to fall back to.
func TestListViaSubscriptionHTTPError(t *testing.T) {
	srv := subServer(t, stdhttp.StatusInternalServerError, "", nil)

	c, _ := query.NewClient(srv.URL, v13(t))
	if _, err := c.ListViaSubscription(context.Background(), "senders"); err == nil ||
		!strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("err = %v, want an HTTP 500 subscribe error", err)
	}
}

// TestListViaSubscriptionNoWSHref: a subscription resource with no
// ws_href gives the client nothing to dial and is an error, not an empty
// listing.
func TestListViaSubscriptionNoWSHref(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(stdhttp.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"sub-1"}`))
	}))
	defer srv.Close()

	c, _ := query.NewClient(srv.URL, v13(t))
	if _, err := c.ListViaSubscription(context.Background(), "senders"); err == nil ||
		!strings.Contains(err.Error(), "no ws_href") {
		t.Fatalf("err = %v, want a no-ws_href error", err)
	}
}

// TestListViaSubscriptionPostTransportError: an unreachable Registry
// surfaces the POST transport failure.
func TestListViaSubscriptionPostTransportError(t *testing.T) {
	c, _ := query.NewClient("http://127.0.0.1:1", v13(t))
	if _, err := c.ListViaSubscription(context.Background(), "senders"); err == nil ||
		!strings.Contains(err.Error(), "subscribe senders") {
		t.Fatalf("err = %v, want a subscribe transport error", err)
	}
}

// TestListViaSubscriptionDialError: the POST succeeds but the advertised
// ws_href is undialable — the dial failure must be reported.
func TestListViaSubscriptionDialError(t *testing.T) {
	srv := subServer(t, stdhttp.StatusCreated, "ws://127.0.0.1:1/ws", nil)

	c, _ := query.NewClient(srv.URL, v13(t))
	if _, err := c.ListViaSubscription(context.Background(), "senders"); err == nil ||
		!strings.Contains(err.Error(), "dial") {
		t.Fatalf("err = %v, want a dial error", err)
	}
}
