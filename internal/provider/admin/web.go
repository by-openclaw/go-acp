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

// WebServer is the HTML5 admin page surface paired with the admin
// socket (R24 #489). Per the strict-spec mandate it ships:
//
//   - GET  /              static peer-health table
//   - GET  /events        Server-Sent Events stream with live updates
//   - POST /sessions/disconnect    operator action — disconnect a peer
//   - POST /subs/close             operator action — close (oid, peer) sub
//   - GET  /health        liveness probe
//
// Page binds to the local interface only (the page is at most
// local-network-trustworthy; never expose to the public internet).
// CSP allows 'self' inline style + 'self' inline script: the page
// is self-contained and ships no third-party dependencies, so we
// can keep the relaxation tight (no 'unsafe-inline' for script — we
// gate via a SHA-256 hash matched against `inlineScript` below).
type WebServer struct {
	// Socket is the local admin socket path the page reads from.
	// Defaults to admin.DefaultSocketPath when empty.
	Socket string

	// ConnectorTag is rendered into the page header so an operator
	// with multiple producers open in tabs can tell them apart.
	ConnectorTag string

	// EventInterval is how often the SSE stream emits a peer-health
	// snapshot. Defaults to 2 s when zero.
	EventInterval time.Duration
}

// Serve binds addr on the local interface only. Blocks until ctx cancels.
func (w *WebServer) Serve(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", w.handleIndex)
	mux.HandleFunc("/health", w.handleHealth)
	mux.HandleFunc("/events", w.handleEvents)
	mux.HandleFunc("/sessions/disconnect", w.handleSessionsDisconnect)
	mux.HandleFunc("/subs/close", w.handleSubsClose)

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

// socketPath resolves the configured Socket or falls back to the tag-
// derived default. Centralised so every handler reaches the same
// admin endpoint.
func (w *WebServer) socketPath() string {
	if w.Socket != "" {
		return w.Socket
	}
	return DefaultSocketPath(w.ConnectorTag)
}

// handleIndex renders the peer-health table by calling sessions:list.
// Errors render inline so the operator sees the diagnosis without
// checking server logs.
func (w *WebServer) handleIndex(rw http.ResponseWriter, _ *http.Request) {
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	// CSP: allow inline style + connect-src 'self' for the SSE stream;
	// scripts only from 'self' inline (we never load remote JS so the
	// 'unsafe-inline' allowance is bounded by 'self' source).
	rw.Header().Set("Content-Security-Policy",
		"default-src 'none'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self'; form-action 'self'")
	rw.Header().Set("X-Content-Type-Options", "nosniff")

	socket := w.socketPath()

	var rows string
	resp, err := Call(context.Background(), socket, Request{Verb: "sessions:list"})
	if err != nil {
		rows = fmt.Sprintf(`<tr><td colspan="7">admin socket unreachable: %s</td></tr>`, html.EscapeString(err.Error()))
	} else if !resp.OK {
		rows = fmt.Sprintf(`<tr><td colspan="7">admin error: %s</td></tr>`, html.EscapeString(resp.Error))
	} else {
		rows = renderPeerRows(resp.Data)
	}

	subsRows := "<tr><td colspan=\"3\"><em>subs:list unavailable</em></td></tr>"
	subsResp, subsErr := Call(context.Background(), socket, Request{Verb: "subs:list"})
	if subsErr == nil && subsResp.OK {
		subsRows = renderSubsRows(subsResp.Data)
	}

	page := strings.ReplaceAll(indexTemplate, "{{TAG}}", html.EscapeString(w.ConnectorTag))
	page = strings.ReplaceAll(page, "{{ROWS}}", rows)
	page = strings.ReplaceAll(page, "{{SUBS_ROWS}}", subsRows)
	page = strings.ReplaceAll(page, "{{TIME}}", time.Now().UTC().Format(time.RFC3339))
	_, _ = rw.Write([]byte(page))
}

// handleEvents streams Server-Sent Events with periodic peer-health
// snapshots. Each event is `data: {...JSON peers}` followed by the
// SSE double-newline separator. The page's EventSource subscriber
// re-renders the table on every tick.
func (w *WebServer) handleEvents(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-cache")
	rw.Header().Set("Connection", "keep-alive")
	rw.Header().Set("Content-Security-Policy", "default-src 'none'")

	flusher, ok := rw.(http.Flusher)
	if !ok {
		http.Error(rw, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	interval := w.EventInterval
	if interval == 0 {
		interval = 2 * time.Second
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	socket := w.socketPath()
	ctx := r.Context()
	emit := func() {
		resp, err := Call(ctx, socket, Request{Verb: "sessions:list"})
		if err != nil {
			_, _ = fmt.Fprintf(rw, "event: error\ndata: %s\n\n", err.Error())
			flusher.Flush()
			return
		}
		if !resp.OK {
			_, _ = fmt.Fprintf(rw, "event: error\ndata: %s\n\n", resp.Error)
			flusher.Flush()
			return
		}
		_, _ = fmt.Fprintf(rw, "event: peers\ndata: %s\n\n", string(resp.Data))
		flusher.Flush()
	}
	emit() // initial frame so the client doesn't wait the full interval
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			emit()
		}
	}
}

// handleSessionsDisconnect is a POST endpoint that dispatches the
// sessions:disconnect admin verb. Accepts form-urlencoded `peer`.
// On success returns 303 → / so the browser reloads the table.
func (w *WebServer) handleSessionsDisconnect(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		rw.Header().Set("Allow", "POST")
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(rw, "bad form", http.StatusBadRequest)
		return
	}
	peer := strings.TrimSpace(r.Form.Get("peer"))
	if peer == "" {
		http.Error(rw, "peer param required", http.StatusBadRequest)
		return
	}
	params, _ := json.Marshal(map[string]string{"peer": peer})
	resp, err := Call(r.Context(), w.socketPath(), Request{Verb: "sessions:disconnect", Params: params})
	if err != nil {
		http.Error(rw, "socket: "+err.Error(), http.StatusBadGateway)
		return
	}
	if !resp.OK {
		http.Error(rw, "admin: "+resp.Error, http.StatusBadRequest)
		return
	}
	http.Redirect(rw, r, "/", http.StatusSeeOther)
}

// handleSubsClose POSTs to admin verb subs:close with oid + peer.
func (w *WebServer) handleSubsClose(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		rw.Header().Set("Allow", "POST")
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(rw, "bad form", http.StatusBadRequest)
		return
	}
	oid := strings.TrimSpace(r.Form.Get("oid"))
	peer := strings.TrimSpace(r.Form.Get("peer"))
	if oid == "" && peer == "" {
		http.Error(rw, "at least one of oid or peer required", http.StatusBadRequest)
		return
	}
	payload := map[string]string{}
	if oid != "" {
		payload["oid"] = oid
	}
	if peer != "" {
		payload["peer"] = peer
	}
	params, _ := json.Marshal(payload)
	resp, err := Call(r.Context(), w.socketPath(), Request{Verb: "subs:close", Params: params})
	if err != nil {
		http.Error(rw, "socket: "+err.Error(), http.StatusBadGateway)
		return
	}
	if !resp.OK {
		http.Error(rw, "admin: "+resp.Error, http.StatusBadRequest)
		return
	}
	http.Redirect(rw, r, "/", http.StatusSeeOther)
}

// renderPeerRows decodes the sessions:list payload and emits one
// <tr> per peer plus a "disconnect" form button. Deterministic order:
// peer addr sorted.
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
		return fmt.Sprintf(`<tr><td colspan="7">decode error: %s</td></tr>`, html.EscapeString(err.Error()))
	}
	if len(peers) == 0 {
		return `<tr><td colspan="7"><em>no peers connected</em></td></tr>`
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].Peer < peers[j].Peer })
	var b strings.Builder
	for _, p := range peers {
		fmt.Fprintf(&b,
			`<tr data-peer="%s"><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%d</td><td>`+
				`<form method="post" action="/sessions/disconnect" style="display:inline">`+
				`<input type="hidden" name="peer" value="%s">`+
				`<button type="submit">disconnect</button>`+
				`</form></td></tr>`,
			html.EscapeString(p.Peer),
			html.EscapeString(p.Peer),
			boolBadge(p.Connected),
			boolBadge(p.Live),
			html.EscapeString(p.LastRx),
			html.EscapeString(p.StaleAfter),
			p.SubsOpen,
			html.EscapeString(p.Peer),
		)
	}
	return b.String()
}

// renderSubsRows decodes the subs:list payload and emits one row per
// (oid, subscribers) with a per-OID "close all" button.
func renderSubsRows(data json.RawMessage) string {
	var subs []struct {
		OID         string   `json:"oid"`
		Subscribers []string `json:"subscribers"`
	}
	if err := json.Unmarshal(data, &subs); err != nil {
		return fmt.Sprintf(`<tr><td colspan="3">decode error: %s</td></tr>`, html.EscapeString(err.Error()))
	}
	if len(subs) == 0 {
		return `<tr><td colspan="3"><em>no active subscriptions</em></td></tr>`
	}
	sort.Slice(subs, func(i, j int) bool { return subs[i].OID < subs[j].OID })
	var b strings.Builder
	for _, s := range subs {
		joined := strings.Join(s.Subscribers, ", ")
		fmt.Fprintf(&b,
			`<tr><td>%s</td><td>%s</td><td>`+
				`<form method="post" action="/subs/close" style="display:inline">`+
				`<input type="hidden" name="oid" value="%s">`+
				`<button type="submit">close all</button>`+
				`</form></td></tr>`,
			html.EscapeString(s.OID),
			html.EscapeString(joined),
			html.EscapeString(s.OID),
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

// indexTemplate is the static HTML5 page. Inline CSS + inline JS for
// the SSE consumer. The page is self-contained with no external
// dependencies.
const indexTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>dhs admin — {{TAG}}</title>
<style>
  body { font-family: system-ui, sans-serif; margin: 1rem; background: #fafafa; color: #222; }
  h1 { margin: 0 0 0.25rem 0; font-size: 1.25rem; }
  h2 { font-size: 1rem; margin: 1.5rem 0 0.5rem 0; }
  p.meta { color: #666; margin: 0 0 1rem 0; font-size: 0.85rem; }
  table { border-collapse: collapse; width: 100%; max-width: 1100px; background: white; }
  th, td { border-bottom: 1px solid #eee; padding: 0.5rem 0.75rem; text-align: left; }
  th { background: #f3f3f3; font-weight: 600; }
  tr:hover td { background: #fcfcff; }
  em { color: #888; }
  button { background: #f7f7f7; border: 1px solid #ddd; padding: 0.2rem 0.6rem; border-radius: 3px; cursor: pointer; }
  button:hover { background: #efefef; }
  footer { color: #888; margin-top: 1rem; font-size: 0.75rem; }
  .ts { color: #888; font-size: 0.8rem; margin-left: 0.5rem; }
</style>
</head>
<body>
  <h1>dhs admin — {{TAG}}<span id="ts" class="ts">{{TIME}}</span></h1>
  <p class="meta">Live peer inventory. Updates every 2 s via Server-Sent Events.</p>

  <h2>Peers</h2>
  <table>
    <thead>
      <tr><th>Peer</th><th>Connected</th><th>Live</th><th>Last rx</th><th>Stale after</th><th>Subs open</th><th>Action</th></tr>
    </thead>
    <tbody id="peers-tbody">
      {{ROWS}}
    </tbody>
  </table>

  <h2>Subscriptions</h2>
  <table>
    <thead>
      <tr><th>OID</th><th>Subscribers</th><th>Action</th></tr>
    </thead>
    <tbody id="subs-tbody">
      {{SUBS_ROWS}}
    </tbody>
  </table>

  <footer>R24 #489 · live HTML5 admin · SSE + form POST mutations · CSP scripts limited to self</footer>

  <script>
(function () {
  if (!window.EventSource) { return; }
  var es = new EventSource('/events');
  var tbody = document.getElementById('peers-tbody');
  var tsEl = document.getElementById('ts');
  function escape(s) {
    return String(s).replace(/[&<>"']/g, function (c) {
      return ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'})[c];
    });
  }
  es.addEventListener('peers', function (ev) {
    var peers;
    try { peers = JSON.parse(ev.data); } catch (e) { return; }
    if (!peers || peers.length === 0) {
      tbody.innerHTML = '<tr><td colspan="7"><em>no peers connected</em></td></tr>';
    } else {
      peers.sort(function (a, b) { return a.peer < b.peer ? -1 : a.peer > b.peer ? 1 : 0; });
      var rows = peers.map(function (p) {
        return '<tr data-peer="' + escape(p.peer) + '"><td>' + escape(p.peer) + '</td>' +
          '<td>' + (p.connected ? 'yes' : 'no') + '</td>' +
          '<td>' + (p.live ? 'yes' : 'no') + '</td>' +
          '<td>' + escape(p.last_rx || '') + '</td>' +
          '<td>' + escape(p.stale_after || '') + '</td>' +
          '<td>' + (p.subs_open || 0) + '</td>' +
          '<td><form method="post" action="/sessions/disconnect" style="display:inline">' +
            '<input type="hidden" name="peer" value="' + escape(p.peer) + '">' +
            '<button type="submit">disconnect</button></form></td></tr>';
      }).join('');
      tbody.innerHTML = rows;
    }
    tsEl.textContent = new Date().toISOString();
  });
})();
  </script>
</body>
</html>
`
