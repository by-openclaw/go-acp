package registry

// The mirror's AUDIT surface — the reason a mirror exists beyond
// bridging: it sits between an external Registry (a Cerebrum-side
// one, a vendor appliance) and the plant, and every behaviour of that
// external party worth an argument later is recorded as a fact now.
//
// Two faces:
//
//   - an append-only JSONL audit log (--audit-log): one object per
//     observation — refused forwards with the target's own words,
//     evictions, parent-ordering rejections, WS drops. The file is
//     the evidence trail for "your registry did X at T";
//   - a status endpoint (--status-addr, /status.json): live counters,
//     per-collection cache sizes (the mirror's authoritative copy —
//     parity against either registry is one GET away) and the recent
//     audit ring, machine-checkable by amwa-validate-mirror.yml.

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	stdhttp "net/http"
	"os"
	"sync"
	"time"
)

// AuditEvent is one observation about the mirrored path.
type AuditEvent struct {
	TS     string         `json:"ts"`
	Kind   string         `json:"kind"`
	Detail map[string]any `json:"detail,omitempty"`
}

// auditRingSize bounds the in-memory tail served by /status.json.
const auditRingSize = 64

// DefaultAuditMaxBytes is the size at which the audit log is rotated
// when the operator sets nothing. One previous generation is kept, so
// the trail never holds more than twice this on disk.
const DefaultAuditMaxBytes = 64 << 20

// A burst of identical observations is one fact, not a million: the
// first is written, the ones that follow within auditRepeatWindow of
// each other are counted, and the count is written as one "repeated"
// line — when the burst ends, and every auditRepeatFlush repeats while
// it lasts, so a crash loses at most that many. A target refusing
// every child of an evicted node wrote 2.7 million identical lines in
// five hours before this (#1311).
const (
	auditRepeatWindow = 5 * time.Second
	auditRepeatFlush  = 1000
)

type auditor struct {
	mu   sync.Mutex
	f    *os.File
	path string
	size int64 // bytes in the current generation
	max  int64 // rotate beyond this; 0 = never
	ring []AuditEvent
	now  func() time.Time

	// The last distinct observation and the repeats of it not yet
	// written.
	lastKey  string
	lastKind string
	lastAt   time.Time
	repeats  int
}

// newAuditor opens the JSONL sink; an empty path keeps only the ring.
// maxBytes 0 means DefaultAuditMaxBytes.
func newAuditor(path string, maxBytes int64) (*auditor, error) {
	if maxBytes == 0 {
		maxBytes = DefaultAuditMaxBytes
	}
	a := &auditor{path: path, max: maxBytes, now: time.Now}
	if path != "" {
		if err := a.open(); err != nil {
			return nil, fmt.Errorf("registry/mirror: audit log: %w", err)
		}
	}
	return a, nil
}

// open appends to the current generation and learns its size.
func (a *auditor) open() error {
	var size int64
	if st, err := os.Stat(a.path); err == nil {
		size = st.Size()
	}
	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	a.f, a.size = f, size
	return nil
}

func (a *auditor) event(kind string, detail map[string]any) {
	if a == nil {
		return
	}
	key := auditKey(kind, detail)
	a.mu.Lock()
	defer a.mu.Unlock()
	at := a.now()
	if key != "" && key == a.lastKey && at.Sub(a.lastAt) <= auditRepeatWindow {
		a.repeats++
		a.lastAt = at
		if a.repeats >= auditRepeatFlush {
			a.flushRepeats()
		}
		return
	}
	a.flushRepeats()
	a.lastKey, a.lastKind, a.lastAt = key, kind, at
	a.record(AuditEvent{TS: stamp(at), Kind: kind, Detail: detail})
}

// auditKey identifies an observation by what it says, not when. Empty
// when the detail cannot be encoded: such an event is never a repeat.
func auditKey(kind string, detail map[string]any) string {
	raw, err := json.Marshal(detail) // map keys are encoded sorted
	if err != nil {
		return ""
	}
	return kind + "\x00" + string(raw)
}

func stamp(at time.Time) string { return at.UTC().Format(time.RFC3339Nano) }

// flushRepeats writes the repeats counted since the last line, stamped
// with the time of the latest one. Caller holds mu.
func (a *auditor) flushRepeats() {
	if a.repeats == 0 {
		return
	}
	a.record(a.repeated())
	a.repeats = 0
}

// repeated is the line that stands for the repeats not yet written.
func (a *auditor) repeated() AuditEvent {
	return AuditEvent{TS: stamp(a.lastAt), Kind: "repeated",
		Detail: map[string]any{"kind": a.lastKind, "times": a.repeats}}
}

// record puts one line in the ring and in the file, rotating the file
// first when the line would take it past its cap. Caller holds mu.
func (a *auditor) record(ev AuditEvent) {
	a.ring = append(a.ring, ev)
	if len(a.ring) > auditRingSize {
		a.ring = a.ring[len(a.ring)-auditRingSize:]
	}
	if a.f == nil {
		return
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		return
	}
	line := append(raw, '\n')
	if a.max > 0 && a.size > 0 && a.size+int64(len(line)) > a.max {
		a.rotate()
		if a.f == nil {
			return
		}
	}
	n, _ := a.f.Write(line)
	a.size += int64(n)
}

// rotate moves the current generation to <path>.1, replacing the one
// before it, and starts a new file. A trail that cannot be rotated
// keeps growing where it is rather than losing what it holds; one that
// cannot be reopened leaves the ring only. Caller holds mu.
func (a *auditor) rotate() {
	_ = a.f.Close()
	a.f = nil
	if err := os.Rename(a.path, a.path+".1"); err != nil {
		a.max = 0 // do not try again at every line
	}
	_ = a.open()
}

// recent returns the tail of the trail, the repeats still being
// counted included.
func (a *auditor) recent() []AuditEvent {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := append([]AuditEvent(nil), a.ring...)
	if a.repeats > 0 {
		out = append(out, a.repeated())
	}
	return out
}

func (a *auditor) close() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.flushRepeats()
	if a.f != nil {
		_ = a.f.Close()
		a.f = nil
	}
}

// mirrorStatus is the /status.json document.
type mirrorStatus struct {
	Source      string         `json:"source"`
	Target      string         `json:"target"`
	APIVer      string         `json:"api_ver"`
	UptimeSec   int64          `json:"uptime_sec"`
	Stats       MirrorStats    `json:"stats"`
	CacheCounts map[string]int `json:"cache_counts"`
	// ServeAddr is the served read-only Query face's bound address
	// (--serve, mirror_serve.go); absent when serving is disabled.
	ServeAddr string `json:"serve_addr,omitempty"`
	// ServeAuth reports whether the served face is armed with the
	// BCP-003-02 Bearer gate (--auth-url, issue #946). A pointer so a
	// disarmed-but-serving mirror reports an explicit false while a
	// mirror with no served face omits the key — same presence rule
	// as serve_addr.
	ServeAuth *bool `json:"serve_auth,omitempty"`
	// ServeTLS reports whether the served face speaks HTTPS/WSS only
	// (--serve-tls-cert/-key, issue #948). Same pointer presence rule
	// as serve_auth.
	ServeTLS    *bool        `json:"serve_tls,omitempty"`
	RecentAudit []AuditEvent `json:"recent_audit"`
}

// serveStatus runs the status endpoint until ctx ends.
func (m *Mirror) serveStatus(ctx context.Context, addr string) {
	mux := stdhttp.NewServeMux()
	mux.HandleFunc("/status.json", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		m.mu.Lock()
		counts := make(map[string]int, len(m.cache))
		for topic, docs := range m.cache {
			counts[topic] = len(docs)
		}
		st := mirrorStatus{
			Source:      m.opts.Source,
			Target:      m.opts.Target,
			APIVer:      m.opts.APIVer,
			UptimeSec:   int64(time.Since(m.started).Seconds()),
			Stats:       m.stats,
			CacheCounts: counts,
		}
		if m.serve != nil {
			st.ServeAddr = m.serve.addr
			armed := m.opts.ServeAuthURL != ""
			st.ServeAuth = &armed
			secured := m.opts.ServeTLSCert != ""
			st.ServeTLS = &secured
		}
		m.mu.Unlock()
		st.RecentAudit = m.audit.recent()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(st)
	})
	srv := &stdhttp.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		m.logger.Warn("registry/mirror: status endpoint failed", "addr", addr, "err", err)
		return
	}
	m.serveUntil(ctx, srv, ln, "status endpoint")
}

// serveUntil runs one HTTP face until ctx ends, then shuts it down.
// A server that stops for any other reason is reported: the operator
// asked for this face, and it going away silently is how a mirror
// ends up looking healthy while answering nothing.
func (m *Mirror) serveUntil(ctx context.Context, srv *stdhttp.Server, ln net.Listener, what string) {
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	if err := srv.Serve(ln); err != nil && err != stdhttp.ErrServerClosed {
		m.logger.Warn("registry/mirror: "+what+" failed", "addr", ln.Addr().String(), "err", err)
	}
}
