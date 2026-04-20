package emberplus

import (
	"context"
	"log/slog"
	"math"
	"time"

	"acp/internal/export/canonical"
	"acp/internal/protocol/emberplus/ber"
	"acp/internal/protocol/emberplus/glow"
)

// streamEntry holds the per-parameter state needed to emit StreamEntry
// values each tick. Type is captured at tree-load time so the streamer
// doesn't have to re-look-up the Parameter on every tick.
type streamEntry struct {
	id      int64  // stream identifier (wire-visible to consumers)
	oid     string // source parameter OID, for debug
	kind    string // canonical parameter type (real / integer / boolean)
	min     float64
	max     float64
}

// collectStreams walks the loaded tree for Parameters with a non-nil
// StreamIdentifier. Each becomes a streamEntry the broadcaster emits
// periodically.
func (s *server) collectStreams() []streamEntry {
	var out []streamEntry
	if s.tree == nil {
		return nil
	}
	var walk func(el canonical.Element)
	walk = func(el canonical.Element) {
		if el == nil {
			return
		}
		if p, ok := el.(*canonical.Parameter); ok && p.StreamIdentifier != nil {
			e := streamEntry{
				id:   *p.StreamIdentifier,
				oid:  p.OID,
				kind: p.Type,
			}
			if v, ok := asFloat64(p.Minimum); ok {
				e.min = v
			} else {
				e.min = -60
			}
			if v, ok := asFloat64(p.Maximum); ok {
				e.max = v
			} else {
				e.max = 0
			}
			out = append(out, e)
		}
		for _, c := range el.Common().Children {
			walk(c)
		}
	}
	walk(s.tree.root)
	return out
}

// streamTick computes sine-wave values for a caller-filtered subset of
// entries and encodes them as one Root{StreamCollection}. Returns nil
// if the subset is empty so the caller can skip send() entirely.
//
// Wire shape (spec p.93):
//
//	Root [APP 0] → StreamCollection [APP 6] → [CTX 0] → StreamEntry [APP 5]
//	  StreamEntry { [0] streamIdentifier Integer32, [1] streamValue Value }
func streamTick(entries []streamEntry, t time.Time) []byte {
	if len(entries) == 0 {
		return nil
	}
	items := make([]ber.TLV, 0, len(entries))
	phase := float64(t.UnixMilli()) / 500.0 // 2-second period
	for _, e := range entries {
		amp := (e.max - e.min) / 2
		mid := (e.max + e.min) / 2
		raw := mid + amp*math.Sin(phase+float64(e.id))
		var val ber.TLV
		switch e.kind {
		case canonical.ParamReal:
			val = ber.Real(raw)
		case canonical.ParamInteger:
			val = ber.Integer(int64(math.Round(raw)))
		case canonical.ParamBoolean:
			val = ber.Boolean(raw > mid)
		default:
			val = ber.Real(raw)
		}
		entry := ber.AppConstructed(glow.TagStreamEntry,
			ber.ContextConstructed(glow.StreamEntryIdentifier, ber.Integer(e.id)),
			ber.ContextConstructed(glow.StreamEntryValue, val),
		)
		items = append(items, ber.ContextConstructed(0, entry))
	}
	coll := ber.AppConstructed(glow.TagStreamCollection, items...)
	root := ber.AppConstructed(glow.TagRoot, coll)
	return ber.EncodeTLV(root)
}

// runStreamer fires StreamCollection frames every interval — but only
// to sessions that have explicitly Subscribed to at least one stream
// parameter. Each subscriber receives a StreamCollection containing
// only the entries for the parameters they watch.
//
// This avoids broadcasting meter noise to consumers that aren't
// interested — which matters at scale: a rack with 200 meters at 10 Hz
// would otherwise push ~40 KB/s at every passive observer, starving
// the link for other traffic like crosspoint tallies.
func (s *server) runStreamer(ctx context.Context, interval time.Duration) {
	entries := s.collectStreams()
	if len(entries) == 0 {
		return
	}
	s.logger.Info("streamer started",
		slog.Int("stream_count", len(entries)),
		slog.Duration("interval", interval))

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopped:
			return
		case t := <-ticker.C:
			s.fanoutStreams(entries, t)
		}
	}
}

// fanoutStreams builds a per-session StreamCollection restricted to the
// entries that session has Subscribed to. Sessions with no relevant
// subscriptions are skipped entirely.
func (s *server) fanoutStreams(entries []streamEntry, t time.Time) {
	// Snapshot sessions + their subs under the server lock, then do the
	// (potentially slow) per-session encode + send outside it.
	type sessFilter struct {
		sess *session
		want []streamEntry
	}
	// sess.subs is protected by server.mu — single lock covers the whole
	// snapshot.
	s.mu.Lock()
	var work []sessFilter
	for sess := range s.sessions {
		var want []streamEntry
		for _, e := range entries {
			if _, ok := sess.subs[e.oid]; ok {
				want = append(want, e)
			}
		}
		if len(want) > 0 {
			work = append(work, sessFilter{sess: sess, want: want})
		}
	}
	s.mu.Unlock()

	for _, w := range work {
		payload := streamTick(w.want, t)
		if payload != nil {
			w.sess.send(payload)
		}
	}
}
