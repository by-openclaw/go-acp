package registry

// The mirror as an OAuth client of its source (#1312): a source
// Registry that guards its Query API answers 401 to anything without
// a Bearer token — the subscription request, the subscription socket
// and the REST reads alike. With SourceAuthURL the mirror obtains a
// token by client_credentials and carries it on all three.

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// guardedSource wraps a source handler with the Bearer gate a guarded
// Query API applies, and keeps what it saw.
type guardedSource struct {
	mu      sync.Mutex
	token   string
	refused int             // requests without the token
	granted map[string]bool // "subscribe" / "ws" / "rest" reached with it
}

func (g *guardedSource) wrap(next stdhttp.Handler) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		g.mu.Lock()
		ok := r.Header.Get("Authorization") == "Bearer "+g.token
		if ok {
			switch {
			case strings.HasPrefix(r.URL.Path, "/ws/"):
				g.granted["ws"] = true
			case strings.HasSuffix(r.URL.Path, "/subscriptions"):
				g.granted["subscribe"] = true
			default:
				g.granted["rest"] = true
			}
		} else {
			g.refused++
		}
		g.mu.Unlock()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="NMOS", error="invalid_request"`)
			w.WriteHeader(stdhttp.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// mockTokenAS is an Authorization Server with a token endpoint: it
// grants `token` to the one client it knows, for the scope it is asked.
type mockTokenAS struct {
	mu     sync.Mutex
	scopes []string // scope of every grant
	denied int      // token requests with the wrong credentials
}

func (a *mockTokenAS) serve(t *testing.T, id, secret, token string) *httptest.Server {
	t.Helper()
	mux := stdhttp.NewServeMux()
	var ts *httptest.Server
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                           ts.URL,
			"authorization_endpoint":           ts.URL + "/authorize",
			"token_endpoint":                   ts.URL + "/token",
			"jwks_uri":                         ts.URL + "/jwks",
			"registration_endpoint":            ts.URL + "/register",
			"response_types_supported":         []string{"code"},
			"code_challenge_methods_supported": []string{"S256", "plain"},
			"grant_types_supported":            []string{"authorization_code", "client_credentials"},
		})
	})
	mux.HandleFunc("/token", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		u, p, ok := r.BasicAuth()
		_ = r.ParseForm()
		a.mu.Lock()
		defer a.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if !ok || u != id || p != secret || r.PostForm.Get("grant_type") != "client_credentials" {
			a.denied++
			w.WriteHeader(stdhttp.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
		a.scopes = append(a.scopes, r.PostForm.Get("scope"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": token, "token_type": "Bearer", "expires_in": 3600,
			"scope": r.PostForm.Get("scope"),
		})
	})
	ts = httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func sourceAuthFrames() map[string][][]byte {
	return map[string][][]byte{
		"nodes":   {grainFrame("nodes", "n1", "", `{"id":"n1","label":"src-node"}`)},
		"devices": {grainFrame("devices", "d1", "", `{"id":"d1","node_id":"n1"}`)},
	}
}

// TestMirrorReadsAGuardedSourceWithItsToken: every source leg carries
// the token the Authorization Server granted for the `query` scope,
// nothing is asked of the source without it, and the catalogue lands.
func TestMirrorReadsAGuardedSourceWithItsToken(t *testing.T) {
	as := &mockTokenAS{}
	asSrv := as.serve(t, "mirror-client", "s3cret", "tok-query-1")

	plant := &fakePlant{}
	target := httptest.NewServer(plant.targetHandler())
	defer target.Close()
	guard := &guardedSource{token: "tok-query-1", granted: map[string]bool{}}
	var src *httptest.Server
	src = httptest.NewServer(guard.wrap(plant.sourceHandler(t, func() string { return src.URL }, sourceAuthFrames())))
	defer src.Close()

	m, err := NewMirror(MirrorOptions{
		Source: src.URL, Target: target.URL, APIVer: "v1.3",
		SourceAuthURL: asSrv.URL, SourceAuthClientID: "mirror-client", SourceAuthClientSecret: "s3cret",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = m.Run(ctx) }()

	waitFor(t, 5*time.Second, func() bool {
		plant.mu.Lock()
		defer plant.mu.Unlock()
		joined := strings.Join(plant.posts, ",")
		return strings.Contains(joined, "node:n1") && strings.Contains(joined, "device:d1")
	}, "node and device at the target, read from the guarded source")
	// A row seen at a lower minor is looked up above by REST: wait for
	// that leg too, it runs behind the socket.
	waitFor(t, 5*time.Second, func() bool {
		guard.mu.Lock()
		defer guard.mu.Unlock()
		return guard.granted["subscribe"] && guard.granted["ws"] && guard.granted["rest"]
	}, "the subscription request, its socket and a REST read, each with the token")

	guard.mu.Lock()
	refused := guard.refused
	guard.mu.Unlock()
	if refused != 0 {
		t.Errorf("%d source requests went out without the token, want 0", refused)
	}
	as.mu.Lock()
	defer as.mu.Unlock()
	if as.denied != 0 || len(as.scopes) == 0 {
		t.Fatalf("token endpoint: %d denied, grants %v — want one grant, none denied", as.denied, as.scopes)
	}
	for _, s := range as.scopes {
		if s != "query" {
			t.Errorf("token asked for scope %q, want query", s)
		}
	}
	if len(as.scopes) != 1 {
		t.Errorf("%d token requests for one unexpired token, want 1", len(as.scopes))
	}
}

// TestMirrorWithoutATokenGetsNothingFromAGuardedSource pins the other
// side: no SourceAuthURL, a guarded source, nothing forwarded — the
// mirror keeps asking and the audit trail says why.
func TestMirrorWithoutATokenGetsNothingFromAGuardedSource(t *testing.T) {
	plant := &fakePlant{}
	target := httptest.NewServer(plant.targetHandler())
	defer target.Close()
	guard := &guardedSource{token: "tok-query-1", granted: map[string]bool{}}
	var src *httptest.Server
	src = httptest.NewServer(guard.wrap(plant.sourceHandler(t, func() string { return src.URL }, sourceAuthFrames())))
	defer src.Close()

	m, err := NewMirror(MirrorOptions{Source: src.URL, Target: target.URL, APIVer: "v1.3"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = m.Run(ctx) }()

	waitFor(t, 5*time.Second, func() bool {
		guard.mu.Lock()
		defer guard.mu.Unlock()
		return guard.refused > 0
	}, "the guarded source refusing a tokenless subscription")
	plant.mu.Lock()
	defer plant.mu.Unlock()
	if len(plant.posts) != 0 {
		t.Errorf("target received %v from a source that answered 401", plant.posts)
	}
	if st := m.Stats(); st.Forwarded != 0 {
		t.Errorf("forwarded = %d, want 0", st.Forwarded)
	}
}

// TestNewMirrorSourceAuthNeedsItsClient: a server without a client, or
// a client without a server, is a configuration that cannot work.
func TestNewMirrorSourceAuthNeedsItsClient(t *testing.T) {
	base := MirrorOptions{Source: "http://a:1", Target: "http://b:1"}
	for name, o := range map[string]MirrorOptions{
		"url only":       {SourceAuthURL: "http://as:3"},
		"no secret":      {SourceAuthURL: "http://as:3", SourceAuthClientID: "c"},
		"no id":          {SourceAuthURL: "http://as:3", SourceAuthClientSecret: "s"},
		"client, no url": {SourceAuthClientID: "c", SourceAuthClientSecret: "s"},
	} {
		o.Source, o.Target = base.Source, base.Target
		if _, err := NewMirror(o); err == nil {
			t.Errorf("%s: accepted, want an error", name)
		}
	}
	base.SourceAuthURL, base.SourceAuthClientID, base.SourceAuthClientSecret = "http://as:3", "c", "s"
	if _, err := NewMirror(base); err != nil {
		t.Errorf("complete source authorization rejected: %v", err)
	}
}
