package http

// The arms the moved tests did not reach. Before the move these were
// covered only indirectly, from amwa/registry and amwa/provider tests —
// which meant the transport could not be proven on its own. It can now.

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// jsonServer answers every request with status/body/headers from fn.
func jsonServer(t *testing.T, fn func(w stdhttp.ResponseWriter, r *stdhttp.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(stdhttp.HandlerFunc(fn))
	t.Cleanup(srv.Close)
	return srv
}

func okJSON(body string) func(stdhttp.ResponseWriter, *stdhttp.Request) {
	return func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

// --- applyAuth ------------------------------------------------------------

func TestClientAttachesBearerToken(t *testing.T) {
	var got string
	srv := jsonServer(t, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		got = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	c := NewClient()
	c.TokenSource = func(context.Context) (string, error) { return "tok123", nil }

	var dst map[string]any
	if err := c.GetJSON(context.Background(), srv.URL, &dst); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if got != "Bearer tok123" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer tok123")
	}
}

// A token that cannot be obtained aborts the call: sending the request
// unauthenticated would only trade a 401 for a less useful error.
func TestClientTokenSourceErrorAbortsEveryVerb(t *testing.T) {
	srv := jsonServer(t, okJSON(`{}`))
	c := NewClient()
	c.TokenSource = func(context.Context) (string, error) {
		return "", errors.New("no token")
	}
	ctx := context.Background()
	var dst map[string]any

	if err := c.GetJSON(ctx, srv.URL, &dst); err == nil ||
		!strings.Contains(err.Error(), "obtain access token") {
		t.Errorf("GetJSON err = %v, want an access-token error", err)
	}
	if _, err := c.GetJSONPage(ctx, srv.URL, &dst); err == nil ||
		!strings.Contains(err.Error(), "obtain access token") {
		t.Errorf("GetJSONPage err = %v, want an access-token error", err)
	}
	if _, _, err := c.GetJSONPageLinks(ctx, srv.URL, &dst); err == nil ||
		!strings.Contains(err.Error(), "obtain access token") {
		t.Errorf("GetJSONPageLinks err = %v, want an access-token error", err)
	}
	if _, err := c.PostJSON(ctx, srv.URL, map[string]any{}, &dst); err == nil ||
		!strings.Contains(err.Error(), "obtain access token") {
		t.Errorf("PostJSON err = %v, want an access-token error", err)
	}
}

// --- argument + request-building errors -----------------------------------

func TestClientRejectsNilDst(t *testing.T) {
	c := NewClient()
	ctx := context.Background()
	if err := c.GetJSON(ctx, "http://example.invalid", nil); err == nil {
		t.Error("GetJSON(nil dst) returned nil")
	}
	if _, err := c.GetJSONPage(ctx, "http://example.invalid", nil); err == nil {
		t.Error("GetJSONPage(nil dst) returned nil")
	}
}

// A URL the stdlib refuses to turn into a request — the build-request arm,
// distinct from a transport failure.
func TestClientRejectsUnbuildableURL(t *testing.T) {
	const bad = "http://exa\nmple.com"
	c := NewClient()
	ctx := context.Background()
	var dst map[string]any

	if err := c.GetJSON(ctx, bad, &dst); err == nil ||
		!strings.Contains(err.Error(), "build request") {
		t.Errorf("GetJSON err = %v, want a build-request error", err)
	}
	if _, err := c.GetJSONPage(ctx, bad, &dst); err == nil ||
		!strings.Contains(err.Error(), "build request") {
		t.Errorf("GetJSONPage err = %v, want a build-request error", err)
	}
	if _, _, err := c.GetJSONPageLinks(ctx, bad, &dst); err == nil ||
		!strings.Contains(err.Error(), "build request") {
		t.Errorf("GetJSONPageLinks err = %v, want a build-request error", err)
	}
	if _, err := c.PostJSON(ctx, bad, map[string]any{}, &dst); err == nil ||
		!strings.Contains(err.Error(), "build POST") {
		t.Errorf("PostJSON err = %v, want a build-POST error", err)
	}
}

// The peer is unreachable: the Do() arm.
func TestClientReportsTransportFailure(t *testing.T) {
	srv := jsonServer(t, okJSON(`{}`))
	url := srv.URL
	srv.Close() // nothing is listening now

	c := NewClient()
	ctx := context.Background()
	var dst map[string]any

	if err := c.GetJSON(ctx, url, &dst); err == nil {
		t.Error("GetJSON to a closed server returned nil")
	}
	if _, err := c.GetJSONPage(ctx, url, &dst); err == nil {
		t.Error("GetJSONPage to a closed server returned nil")
	}
	if _, _, err := c.GetJSONPageLinks(ctx, url, &dst); err == nil {
		t.Error("GetJSONPageLinks to a closed server returned nil")
	}
	if _, err := c.PostJSON(ctx, url, map[string]any{}, &dst); err == nil {
		t.Error("PostJSON to a closed server returned nil")
	}
}

// --- GetJSON decode arms --------------------------------------------------

func TestGetJSONRejectsUnknownFieldsAndTrailingContent(t *testing.T) {
	type want struct {
		A int `json:"a"`
	}
	tests := []struct {
		name, body, wantErr string
	}{
		{"unknown field", `{"a":1,"b":2}`, "decode body"},
		{"trailing content", `{"a":1}{"a":2}`, "trailing JSON content"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := jsonServer(t, okJSON(tc.body))
			var dst want
			err := NewClient().GetJSON(context.Background(), srv.URL, &dst)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

// MaxBody <= 0 falls back to DefaultMaxBody rather than rejecting
// everything — a zero-valued struct literal must still work.
func TestGetJSONZeroMaxBodyUsesDefault(t *testing.T) {
	srv := jsonServer(t, okJSON(`{"a":1}`))
	c := &Client{HTTP: &stdhttp.Client{}} // MaxBody left at 0
	var dst struct {
		A int `json:"a"`
	}
	if err := c.GetJSON(context.Background(), srv.URL, &dst); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if dst.A != 1 {
		t.Errorf("A = %d, want 1", dst.A)
	}
}

// --- GetJSONPage / GetJSONPageLinks --------------------------------------

func TestGetJSONPageFollowsLinkNext(t *testing.T) {
	srv := jsonServer(t, func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Add("Link", `<http://next/page>; rel="next"`)
		w.Header().Add("Link", `<http://prev/page>; rel="prev"`)
		_, _ = w.Write([]byte(`[{"id":"a"}]`))
	})
	var dst []map[string]string
	next, err := NewClient().GetJSONPage(context.Background(), srv.URL, &dst)
	if err != nil {
		t.Fatalf("GetJSONPage: %v", err)
	}
	if next != "http://next/page" {
		t.Errorf("next = %q", next)
	}
	if len(dst) != 1 || dst[0]["id"] != "a" {
		t.Errorf("dst = %v", dst)
	}
}

// IS-04 §6.1.6: collections are newest-first, so a client walking the whole
// thing follows PREV. Both cursors must come back.
func TestGetJSONPageLinksReturnsBothCursors(t *testing.T) {
	srv := jsonServer(t, func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Add("Link", `<http://newer>; rel="next", <http://older>; rel="prev"`)
		_, _ = w.Write([]byte(`[]`))
	})
	var dst []map[string]string
	next, prev, err := NewClient().GetJSONPageLinks(context.Background(), srv.URL, &dst)
	if err != nil {
		t.Fatalf("GetJSONPageLinks: %v", err)
	}
	if next != "http://newer" || prev != "http://older" {
		t.Errorf("next = %q, prev = %q", next, prev)
	}
}

func TestGetJSONPageNon200(t *testing.T) {
	srv := jsonServer(t, func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusServiceUnavailable)
		_, _ = w.Write([]byte("busy"))
	})
	var dst []map[string]string
	ctx := context.Background()
	if _, err := NewClient().GetJSONPage(ctx, srv.URL, &dst); err == nil ||
		!strings.Contains(err.Error(), "HTTP 503") {
		t.Errorf("GetJSONPage err = %v, want HTTP 503", err)
	}
	if _, _, err := NewClient().GetJSONPageLinks(ctx, srv.URL, &dst); err == nil ||
		!strings.Contains(err.Error(), "HTTP 503") {
		t.Errorf("GetJSONPageLinks err = %v, want HTTP 503", err)
	}
}

func TestGetJSONPageDecodeError(t *testing.T) {
	srv := jsonServer(t, okJSON(`not json`))
	var dst []map[string]string
	ctx := context.Background()
	if _, err := NewClient().GetJSONPage(ctx, srv.URL, &dst); err == nil ||
		!strings.Contains(err.Error(), "decode body") {
		t.Errorf("GetJSONPage err = %v, want a decode error", err)
	}
	if _, _, err := NewClient().GetJSONPageLinks(ctx, srv.URL, &dst); err == nil ||
		!strings.Contains(err.Error(), "decode") {
		t.Errorf("GetJSONPageLinks err = %v, want a decode error", err)
	}
}

// The cap applies to every verb, not just GetJSON — an oversized page or
// POST response must be refused rather than read into memory.
func TestBodyCapAppliesToEveryVerb(t *testing.T) {
	srv := jsonServer(t, okJSON(strings.Repeat("a", 4096)))
	mk := func() *Client {
		c := NewClient()
		c.MaxBody = 16
		return c
	}
	ctx := context.Background()
	var dst map[string]any

	if _, err := mk().GetJSONPage(ctx, srv.URL, &dst); err == nil ||
		!strings.Contains(err.Error(), "exceeds") {
		t.Errorf("GetJSONPage err = %v, want an exceeds-cap error", err)
	}
	if _, _, err := mk().GetJSONPageLinks(ctx, srv.URL, &dst); err == nil ||
		!strings.Contains(err.Error(), "exceeds") {
		t.Errorf("GetJSONPageLinks err = %v, want an exceeds-cap error", err)
	}
	if _, err := mk().PostJSON(ctx, srv.URL, map[string]any{}, &dst); err == nil ||
		!strings.Contains(err.Error(), "exceeds") {
		t.Errorf("PostJSON err = %v, want an exceeds-cap error", err)
	}
}

func TestPageVerbsZeroMaxBodyUsesDefault(t *testing.T) {
	srv := jsonServer(t, okJSON(`{"a":1}`))
	ctx := context.Background()
	var dst map[string]any

	c := &Client{HTTP: &stdhttp.Client{}}
	if _, err := c.GetJSONPage(ctx, srv.URL, &dst); err != nil {
		t.Errorf("GetJSONPage: %v", err)
	}
	c = &Client{HTTP: &stdhttp.Client{}}
	if _, _, err := c.GetJSONPageLinks(ctx, srv.URL, &dst); err != nil {
		t.Errorf("GetJSONPageLinks: %v", err)
	}
	c = &Client{HTTP: &stdhttp.Client{}}
	if _, err := c.PostJSON(ctx, srv.URL, map[string]any{}, &dst); err != nil {
		t.Errorf("PostJSON: %v", err)
	}
}

// --- PostJSON -------------------------------------------------------------

func TestPostJSONRoundTrip(t *testing.T) {
	var gotBody, gotCT string
	srv := jsonServer(t, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		b := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(b)
		gotBody, gotCT = string(b), r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(stdhttp.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"x"}`))
	})

	var dst struct {
		ID string `json:"id"`
	}
	status, err := NewClient().PostJSON(context.Background(), srv.URL,
		map[string]string{"label": "cam1"}, &dst)
	if err != nil {
		t.Fatalf("PostJSON: %v", err)
	}
	if status != stdhttp.StatusCreated {
		t.Errorf("status = %d, want 201", status)
	}
	if dst.ID != "x" {
		t.Errorf("dst.ID = %q, want x", dst.ID)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q", gotCT)
	}
	if !strings.Contains(gotBody, `"label":"cam1"`) {
		t.Errorf("body = %q", gotBody)
	}
}

// A non-2xx is a STATUS, not an error: the caller decides whether 200 or
// 201 is acceptable (IS-04 §4.0 treats them differently).
func TestPostJSONNon2xxIsNotAnError(t *testing.T) {
	srv := jsonServer(t, func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusConflict)
	})
	status, err := NewClient().PostJSON(context.Background(), srv.URL, map[string]any{}, nil)
	if err != nil {
		t.Fatalf("PostJSON returned an error for 409: %v", err)
	}
	if status != stdhttp.StatusConflict {
		t.Errorf("status = %d, want 409", status)
	}
}

func TestPostJSONEmptyBodyWithDst(t *testing.T) {
	srv := jsonServer(t, func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusOK)
	})
	var dst map[string]any
	status, err := NewClient().PostJSON(context.Background(), srv.URL, map[string]any{}, &dst)
	if err != nil {
		t.Fatalf("PostJSON: %v", err)
	}
	if status != stdhttp.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
}

func TestPostJSONUnmarshalableSource(t *testing.T) {
	_, err := NewClient().PostJSON(context.Background(), "http://example.invalid",
		make(chan int), nil)
	if err == nil || !strings.Contains(err.Error(), "marshal body") {
		t.Errorf("err = %v, want a marshal error", err)
	}
}

func TestPostJSONDecodeError(t *testing.T) {
	srv := jsonServer(t, okJSON(`{"unexpected":1}`))
	var dst struct {
		Known string `json:"known"`
	}
	_, err := NewClient().PostJSON(context.Background(), srv.URL, map[string]any{}, &dst)
	if err == nil || !strings.Contains(err.Error(), "decode POST response") {
		t.Errorf("err = %v, want a decode error", err)
	}
}

// --- linkRel edge cases ---------------------------------------------------

func TestLinkRelIgnoresMalformedEntries(t *testing.T) {
	tests := []struct {
		name string
		hdrs []string
		want string
	}{
		{"no semicolon", []string{`<http://a>`}, ""},
		{"wrong rel", []string{`<http://a>; rel="last"`}, ""},
		{"single quotes accepted", []string{`<http://a>; rel='next'`}, "http://a"},
		{"none at all", nil, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := linkRel(tc.hdrs, "next"); got != tc.want {
				t.Errorf("linkRel = %q, want %q", got, tc.want)
			}
		})
	}
}

// A body that fails mid-read — the read-error arm, distinct from the cap.
func TestBodyReadFailure(t *testing.T) {
	srv := jsonServer(t, func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "64")
		_, _ = w.Write([]byte(`{"a":`))
		// Handler returns with fewer bytes than promised: the client sees
		// an unexpected EOF while reading.
	})
	ctx := context.Background()
	var dst map[string]any

	if err := NewClient().GetJSON(ctx, srv.URL, &dst); err == nil ||
		!strings.Contains(err.Error(), "read body") {
		t.Errorf("GetJSON err = %v, want a read-body error", err)
	}
	if _, err := NewClient().GetJSONPage(ctx, srv.URL, &dst); err == nil ||
		!strings.Contains(err.Error(), "read body") {
		t.Errorf("GetJSONPage err = %v, want a read-body error", err)
	}
	if _, _, err := NewClient().GetJSONPageLinks(ctx, srv.URL, &dst); err == nil ||
		!strings.Contains(err.Error(), "read body") {
		t.Errorf("GetJSONPageLinks err = %v, want a read-body error", err)
	}
	if _, err := NewClient().PostJSON(ctx, srv.URL, map[string]any{}, &dst); err == nil ||
		!strings.Contains(err.Error(), "read POST body") {
		t.Errorf("PostJSON err = %v, want a read-body error", err)
	}
}
