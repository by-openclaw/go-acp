package mnset

import (
	"context"
	"fmt"
	"strings"

	"dhs/internal/consumer"
)

// SetValues writes several fields, grouped by the document that carries
// them: each document is read once, every field assigned, the document
// PUT once, then read again for the confirmed values.
//
// The module refuses a field that is inconsistent with its neighbours —
// a static_ip outside the static_gateway's subnet answers 400 — so two
// fields of one document written one PUT at a time can each be refused
// while both together are accepted. This is the write for a values file
// (import), where that is the common case.
//
// All-or-nothing per document: a field that cannot be resolved,
// normalised or assigned fails the whole call before anything is sent.
// The answer is parallel to reqs — what the module holds now.
func (p *Plugin) SetValues(ctx context.Context, reqs []consumer.ValueRequest, vals []consumer.Value) ([]consumer.Value, error) {
	if len(reqs) != len(vals) {
		return nil, fmt.Errorf("mnset: %d paths but %d values", len(reqs), len(vals))
	}
	if len(reqs) == 0 {
		return nil, nil
	}

	type change struct {
		i    int
		leaf []string
		val  consumer.Value
	}
	byURL := map[string][]change{}
	docs := map[string]any{}
	clients := map[string]*client{}
	var order []string

	for i, req := range reqs {
		c, err := p.clientFor(req.Slot)
		if err != nil {
			return nil, err
		}
		// Resolve from the live module: the document about to be
		// written must be the one it holds now, not a cached copy.
		r, err := p.resolve(ctx, c, req.Path, false)
		if err != nil {
			return nil, err
		}
		if !isWritable(r.url) {
			return nil, fmt.Errorf("mnset: %s is read-only", r.url)
		}
		if _, isText := r.doc.(string); isText {
			return nil, fmt.Errorf("mnset: %s is a text document, not a settable field", r.url)
		}
		if _, ok := lookup(r.doc, r.leaf); !ok {
			return nil, fmt.Errorf("mnset: %q: %w", req.Path, consumer.ErrObjectNotFound)
		}
		val, err := normalize(req.Path, dict().typeOf(strings.Split(req.Path, ".")), vals[i])
		if err != nil {
			return nil, fmt.Errorf("mnset: %w", err)
		}
		key := fmt.Sprintf("%d %s", req.Slot, r.url)
		if _, seen := docs[key]; !seen {
			docs[key], clients[key] = r.doc, c
			order = append(order, key)
		}
		byURL[key] = append(byURL[key], change{i, r.leaf, val})
	}

	// Every field assigned on its document before any document is sent.
	for _, key := range order {
		for _, ch := range byURL[key] {
			if _, err := assign(docs[key], ch.leaf, ch.val); err != nil {
				return nil, fmt.Errorf("mnset: %w", err)
			}
		}
	}

	out := make([]consumer.Value, len(reqs))
	for _, key := range order {
		url := key[strings.IndexByte(key, ' ')+1:]
		c := clients[key]
		if err := c.put(ctx, url, docs[key]); err != nil {
			return nil, err
		}
		after, err := c.get(ctx, url)
		if err != nil {
			return nil, fmt.Errorf("mnset: %s written, but read-back failed: %w", url, err)
		}
		for _, ch := range byURL[key] {
			got, ok := lookup(after, ch.leaf)
			if !ok {
				return nil, fmt.Errorf("mnset: %q written, but gone on read-back: %w", reqs[ch.i].Path, consumer.ErrObjectNotFound)
			}
			out[ch.i] = typedValue(reqs[ch.i].Path, got)
		}
	}
	return out, nil
}
