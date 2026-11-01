package virtuoso

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	dhsc "dhs/internal/consumer"
	"dhs/internal/virtuoso/codec"
)

// GetValue reads one path with one GET and no walk. A path that names an
// object answers its JSON text.
func (p *Plugin) GetValue(ctx context.Context, req dhsc.ValueRequest) (dhsc.Value, error) {
	slot, path, err := p.resolve(req)
	if err != nil {
		return dhsc.Value{}, err
	}
	raw, err := p.read(ctx, slot, path)
	if err != nil {
		return dhsc.Value{}, err
	}
	return p.value(decode(raw), p.leafNode(slot, path)), nil
}

func (p *Plugin) read(ctx context.Context, slot int, path []string) (json.RawMessage, error) {
	r, err := p.session()
	if err != nil {
		return nil, err
	}
	env, err := r.do(ctx, "GET", codec.DataTarget(slot, path))
	if err != nil {
		var he *HTTPError
		if errors.As(err, &he) && he.Status == 404 {
			return nil, fmt.Errorf("%w: virtuoso: slot %d %s: %v", dhsc.ErrObjectNotFound, slot, codec.JoinPath(path), err)
		}
		return nil, err
	}
	raw, ok := env.Lookup(path)
	if !ok {
		p.ComplianceProfile().Note(NodeNotInAnswer)
		return nil, fmt.Errorf("%w: virtuoso: slot %d: the answer does not carry %s", dhsc.ErrObjectNotFound, slot, codec.JoinPath(path))
	}
	return raw, nil
}

// decode reads a piece of an envelope that already parsed whole.
func decode(raw json.RawMessage) any {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	_ = dec.Decode(&v)
	return v
}

// leafNode is the field's schema when the session already holds it. A get
// never fetches the schema: it only enriches a value the walk has seen.
func (p *Plugin) leafNode(slot int, path []string) *codec.Node {
	s := p.loadedSchema(slot)
	if s == nil {
		return nil
	}
	t, err := s.Find(path)
	if err != nil || !t.Node.IsLeaf() {
		return nil
	}
	return t.Node
}

// value types a decoded JSON value. With a schema node an enum shows its
// label; without one the JSON type decides.
func (p *Plugin) value(v any, n *codec.Node) dhsc.Value {
	switch x := v.(type) {
	case nil:
		return dhsc.Value{Kind: dhsc.KindString}
	case bool:
		return dhsc.Value{Kind: dhsc.KindBool, Bool: x}
	case string:
		return dhsc.Value{Kind: dhsc.KindString, Str: x}
	case json.Number:
		if n != nil && isLabelled(n) {
			if i, ok := enumIndex(n, x); ok {
				return dhsc.Value{Kind: dhsc.KindEnum, Enum: uint8(i), Str: n.Labels[i]}
			}
			p.ComplianceProfile().Note(ValueOutsideEnum)
		}
		return numberValue(x)
	}
	text, _ := json.Marshal(v)
	return dhsc.Value{Kind: dhsc.KindString, Str: string(text)}
}

func enumIndex(n *codec.Node, v json.Number) (int, bool) {
	for i, raw := range n.Enum {
		if string(raw) == v.String() {
			return i, true
		}
	}
	return 0, false
}

func numberValue(n json.Number) dhsc.Value {
	if u, err := strconv.ParseUint(n.String(), 10, 64); err == nil {
		return dhsc.Value{Kind: dhsc.KindUint, Uint: u}
	}
	if i, err := n.Int64(); err == nil {
		return dhsc.Value{Kind: dhsc.KindInt, Int: i}
	}
	f, _ := n.Float64()
	return dhsc.Value{Kind: dhsc.KindFloat, Float: f}
}

// SetValue changes one field with the documented write: PATCH on its
// object with the field as a query attribute ("Changing a single value").
// The schema decides first: an unknown path, a method, an object, a key or
// a read-only field is refused with nothing sent. The result is the value
// read back; the status of the PATCH alone is not trusted.
func (p *Plugin) SetValue(ctx context.Context, req dhsc.ValueRequest, val dhsc.Value) (dhsc.Value, error) {
	slot, path, err := p.resolve(req)
	if err != nil {
		return dhsc.Value{}, err
	}
	r, err := p.session()
	if err != nil {
		return dhsc.Value{}, err
	}
	wire, err := p.writable(ctx, r, slot, path, val)
	if err != nil {
		return dhsc.Value{}, err
	}
	if _, err := r.do(ctx, "PATCH", codec.PatchTarget(slot, path, wire)); err != nil {
		return dhsc.Value{}, fmt.Errorf("%w: %v", dhsc.ErrWriteRejected, err)
	}
	p.forget(slot)

	raw, err := p.read(ctx, slot, path)
	if err != nil {
		return dhsc.Value{}, fmt.Errorf("virtuoso: PATCH %s accepted; read-back failed: %w", codec.JoinPath(path), err)
	}
	got := decode(raw)
	back := p.value(got, p.leafNode(slot, path))
	if !codec.SameValue(wire, got) {
		return back, fmt.Errorf("%w: virtuoso: %s wrote %s, reads back %s", dhsc.ErrWriteCoerced, codec.JoinPath(path), wire, raw)
	}
	return back, nil
}

func (p *Plugin) writable(ctx context.Context, r *rest, slot int, path []string, val dhsc.Value) (string, error) {
	schema, err := p.schemaFor(ctx, r, slot)
	if err != nil {
		return "", absent(slot, err)
	}
	t, err := schema.Find(path)
	if err != nil {
		if errors.Is(err, codec.ErrNotInSchema) {
			return "", fmt.Errorf("%w: %v", dhsc.ErrObjectNotFound, err)
		}
		return "", err
	}
	field := codec.JoinPath(path)
	switch {
	case t.Node.RPC:
		return "", &dhsc.ValidationError{Field: field, Reason: "an RPC method, not a value; this connector invokes none"}
	case !t.Node.IsLeaf():
		return "", &dhsc.ValidationError{Field: field, Reason: "an object, not a field"}
	case t.Key:
		return "", &dhsc.ValidationError{Field: field, Reason: "a collection key cannot be changed by PATCH"}
	case t.Node.ReadOnly:
		return "", &dhsc.ValidationError{Field: field, Reason: "read-only in the device schema"}
	}
	text, err := valueText(val)
	if err != nil {
		return "", &dhsc.ValidationError{Field: field, Reason: err.Error()}
	}
	wire, err := codec.EncodeAttr(t.Node, text)
	switch {
	case errors.Is(err, codec.ErrBelowMinimum):
		return "", fmt.Errorf("%w: %s: %v", dhsc.ErrOutOfRangeLow, field, err)
	case errors.Is(err, codec.ErrAboveMaximum):
		return "", fmt.Errorf("%w: %s: %v", dhsc.ErrOutOfRangeHigh, field, err)
	case errors.Is(err, codec.ErrNotInEnum):
		return "", fmt.Errorf("%w: %s: %v", dhsc.ErrInvalidEnumLabel, field, err)
	case err != nil:
		return "", &dhsc.ValidationError{Field: field, Reason: err.Error()}
	}
	return wire, nil
}

// forget drops the walked tree of a slot after a write.
func (p *Plugin) forget(slot int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.trees, slot)
}

// resolve turns a request into a slot and a path: Path first (no walk
// needed), then Label against the walked tree. Slot -1, the neutral
// "every slot" of a watch, means the manager.
func (p *Plugin) resolve(req dhsc.ValueRequest) (int, []string, error) {
	slot := max(req.Slot, 0)
	if !codec.ValidSlot(slot) {
		return 0, nil, errSlotRange(req.Slot)
	}
	if path := codec.SplitPath(req.Path); len(path) > 0 {
		return slot, path, nil
	}
	if req.Label == "" {
		return 0, nil, errors.New("virtuoso: --path or --label is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var hits [][]string
	for _, o := range p.trees[slot] {
		if o.Label == req.Label && (req.Group == "" || o.Group == req.Group || strings.HasSuffix(o.Group, "/"+req.Group)) {
			hits = append(hits, o.Path)
		}
	}
	switch len(hits) {
	case 1:
		return slot, hits[0], nil
	case 0:
		return 0, nil, fmt.Errorf("%w: virtuoso: label %q (walk first, or give --path)", dhsc.ErrUnknownLabel, req.Label)
	}
	return 0, nil, fmt.Errorf("virtuoso: label %q is ambiguous (%d paths, e.g. %s): give --path", req.Label, len(hits), codec.JoinPath(hits[0]))
}

// valueText is the operator's value as text. --raw has no meaning on a
// REST field and is refused.
func valueText(v dhsc.Value) (string, error) {
	switch v.Kind {
	case dhsc.KindRaw:
		return "", errors.New("--raw has no meaning here: give --value")
	case dhsc.KindBool:
		return strconv.FormatBool(v.Bool), nil
	case dhsc.KindInt:
		return strconv.FormatInt(v.Int, 10), nil
	case dhsc.KindUint:
		return strconv.FormatUint(v.Uint, 10), nil
	case dhsc.KindFloat:
		return strconv.FormatFloat(v.Float, 'f', -1, 64), nil
	}
	return v.Str, nil
}
