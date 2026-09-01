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

type auditor struct {
	mu   sync.Mutex
	f    *os.File
	ring []AuditEvent
}

// newAuditor opens the JSONL sink; an empty path keeps only the ring.
func newAuditor(path string) (*auditor, error) {
	a := &auditor{}
	if path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, fmt.Errorf("registry/mirror: audit log: %w", err)
		}
		a.f = f
	}
	return a, nil
}

func (a *auditor) event(kind string, detail map[string]any) {
	if a == nil {
		return
	}
	ev := AuditEvent{TS: time.Now().UTC().Format(time.RFC3339Nano), Kind: kind, Detail: detail}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ring = append(a.ring, ev)
	if len(a.ring) > auditRingSize {
		a.ring = a.ring[len(a.ring)-auditRingSize:]
	}
	if a.f != nil {
		if raw, err := json.Marshal(ev); err == nil {
			_, _ = a.f.Write(append(raw, '\n'))
		}
	}
}

func (a *auditor) recent() []AuditEvent {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]AuditEvent(nil), a.ring...)
}

func (a *auditor) close() {
	if a == nil || a.f == nil {
		return
	}
	_ = a.f.Close()
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
	ServeAddr   string       `json:"serve_addr,omitempty"`
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
		}
		m.mu.Unlock()
		st.RecentAudit = m.audit.recent()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(st)
	})
	srv := &stdhttp.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && err != stdhttp.ErrServerClosed {
		m.logger.Warn("registry/mirror: status endpoint failed", "addr", addr, "err", err)
	}
}
