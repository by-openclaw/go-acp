package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"sort"
	"strings"
	"time"
)

// WebServer is the read-only HTML5 page surface paired with the
// admin socket (R24 #489). Static HTML5, zero JS, zero third-party
// deps, CSP blocks all scripts. Lives on a SEPARATE port from the
// wire listener so a misconfigured admin page never affects the
// protocol path.
//
// Pragmatic v1 renders a single page: the peer-health table sourced
// from the same `sessions:list` admin verb. Hot reload + per-feature
// toggles roll in via R25 v1.5.
type WebServer struct {
	// Socket is the local admin socket path the page reads from.
	// Defaults to admin.DefaultSocketPath when empty.
	Socket string

	// ConnectorTag is rendered into the page header so an operator
	// with multiple producers open in tabs can tell them apart.
	ConnectorTag string
}

// Serve binds addr on the local interface only (the page is local-
// network-trustworthy at best; never expose to the public internet).
// Blocks until ctx cancels.
func (w *WebServer) Serve(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", w.handleIndex)
	mux.HandleFunc("/health", w.handleHealth)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	return srv.ListenAndServe()
}

func (w *WebServer) handleHealth(rw http.ResponseWriter, _ *http.Request) {
	rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
	rw.Header().Set("Content-Security-Policy", "default-src 'none'")
	_, _ = rw.Write([]byte("ok"))
}

// handleIndex renders the peer-health table by calling sessions:list
// against the local admin socket. Errors render inline so the
// operator sees the diagnosis without checking server logs.
func (w *WebServer) handleIndex(rw http.ResponseWriter, _ *http.Request) {
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	// CSP locks the page down: no scripts, no third-party fetches.
	rw.Header().Set("Content-Security-Policy",
		"default-src 'none'; style-src 'self' 'unsafe-inline'; img-src 'self'")
	rw.Header().Set("X-Content-Type-Options", "nosniff")

	socket := w.Socket
	if socket == "" {
		socket = DefaultSocketPath(w.ConnectorTag)
	}

	var rows string
	resp, err := Call(context.Background(), socket, Request{Verb: "sessions:list"})
	if err != nil {
		rows = fmt.Sprintf(`<tr><td colspan="6">admin socket unreachable: %s</td></tr>`, html.EscapeString(err.Error()))
	} else if !resp.OK {
		rows = fmt.Sprintf(`<tr><td colspan="6">admin error: %s</td></tr>`, html.EscapeString(resp.Error))
	} else {
		rows = renderPeerRows(resp.Data)
	}

	page := strings.ReplaceAll(indexTemplate, "{{TAG}}", html.EscapeString(w.ConnectorTag))
	page = strings.ReplaceAll(page, "{{ROWS}}", rows)
	page = strings.ReplaceAll(page, "{{TIME}}", time.Now().UTC().Format(time.RFC3339))
	_, _ = rw.Write([]byte(page))
}

// renderPeerRows decodes the sessions:list payload and emits one
// <tr> per peer. Deterministic order: peer addr sorted.
func renderPeerRows(data json.RawMessage) string {
	var peers []struct {
		Peer       string `json:"peer"`
		Connected  bool   `json:"connected"`
		Live       bool   `json:"live"`
		LastRx     string `json:"last_rx"`
		StaleAfter string `json:"stale_after"`
		SubsOpen   int    `json:"subs_open"`
	}
	if err := json.Unmarshal(data, &peers); err != nil {
		return fmt.Sprintf(`<tr><td colspan="6">decode error: %s</td></tr>`, html.EscapeString(err.Error()))
	}
	if len(peers) == 0 {
		return `<tr><td colspan="6"><em>no peers connected</em></td></tr>`
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].Peer < peers[j].Peer })
	var b strings.Builder
	for _, p := range peers {
		fmt.Fprintf(&b,
			`<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%d</td></tr>`,
			html.EscapeString(p.Peer),
			boolBadge(p.Connected),
			boolBadge(p.Live),
			html.EscapeString(p.LastRx),
			html.EscapeString(p.StaleAfter),
			p.SubsOpen,
		)
	}
	return b.String()
}

func boolBadge(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// indexTemplate is the static HTML5 page. Zero JS, inline CSS only
// (CSP allows 'unsafe-inline' for style only). One table; refresh
// via browser reload. Per feedback_admin_web_minimal.
const indexTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>dhs admin — {{TAG}}</title>
<style>
  body { font-family: system-ui, sans-serif; margin: 1rem; background: #fafafa; color: #222; }
  h1 { margin: 0 0 0.25rem 0; font-size: 1.25rem; }
  p.meta { color: #666; margin: 0 0 1rem 0; font-size: 0.85rem; }
  table { border-collapse: collapse; width: 100%; max-width: 920px; background: white; }
  th, td { border-bottom: 1px solid #eee; padding: 0.5rem 0.75rem; text-align: left; }
  th { background: #f3f3f3; font-weight: 600; }
  tr:hover td { background: #fcfcff; }
  em { color: #888; }
  footer { color: #888; margin-top: 1rem; font-size: 0.75rem; }
</style>
</head>
<body>
  <h1>dhs admin — {{TAG}}</h1>
  <p class="meta">Read-only peer inventory. Generated {{TIME}}. Reload to refresh.</p>
  <table>
    <thead>
      <tr><th>Peer</th><th>Connected</th><th>Live</th><th>Last rx</th><th>Stale after</th><th>Subs open</th></tr>
    </thead>
    <tbody>
      {{ROWS}}
    </tbody>
  </table>
  <footer>R24 #489 v1 · static HTML5 · zero JS · CSP blocks scripts</footer>
</body>
</html>
`
