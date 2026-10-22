package kratos

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	dhsc "dhs/internal/consumer"
	"dhs/internal/kratos/codec"
)

// GetValue reads one value from the CVT (§5.15): GET
// consumer/cvtItem/<UID without its root>. --path takes the UID with
// or without the root, dotted or slashed; --label is resolved against
// the walked tree.
func (p *Plugin) GetValue(ctx context.Context, req dhsc.ValueRequest) (dhsc.Value, error) {
	uid, err := p.resolve(req)
	if err != nil {
		return dhsc.Value{}, err
	}
	row, err := p.ReadCVT(ctx, uid)
	if err != nil {
		return dhsc.Value{}, err
	}
	return dhsc.Value{Kind: dhsc.KindString, Str: string(row.Value), Raw: []byte(row.Value)}, nil
}

// ReadCVT returns the CVT row of one UID. The empty set (§5.15.4) is
// reported as not found, with the MQTT-only case named when the walked
// tree knows it.
func (p *Plugin) ReadCVT(ctx context.Context, uid string) (codec.CVTRow, error) {
	r, err := p.session()
	if err != nil {
		return codec.CVTRow{}, err
	}
	q := codec.QueryPath(uid)
	if q == "" {
		return codec.CVTRow{}, fmt.Errorf("kratos: %q is the tree root, not a value", uid)
	}
	env, err := r.get(ctx, "consumer/cvtItem/"+q)
	if err != nil {
		return codec.CVTRow{}, err
	}
	var rows []codec.CVTRow
	if _, err := env.Rows(&rows); err != nil {
		return codec.CVTRow{}, err
	}
	for _, row := range rows {
		if row.UID == uid {
			return row, nil
		}
	}
	if len(rows) == 1 {
		return rows[0], nil
	}
	p.mu.Lock()
	o, known := p.byUID[uid]
	p.mu.Unlock()
	if known && o.Meta["mqttOnly"] == true {
		return codec.CVTRow{}, fmt.Errorf("kratos: %s is not kept in the current value table (volatility -1): read it with watch (MQTT)", uid)
	}
	return codec.CVTRow{}, fmt.Errorf("%w: kratos: no current value for %s", dhsc.ErrUnknownLabel, uid)
}

// SetValue posts a SET message (§5.39): {"MCII":1,"NumArgs":2,
// "Message":"<UID>,<value>"}. The document defines no acknowledge, so
// the value is read back after a short settle and returned as the
// controller holds it.
func (p *Plugin) SetValue(ctx context.Context, req dhsc.ValueRequest, val dhsc.Value) (dhsc.Value, error) {
	uid, err := p.resolve(req)
	if err != nil {
		return dhsc.Value{}, err
	}
	text := valueText(val)
	msg, err := codec.SetMessage(uid, text)
	if err != nil {
		return dhsc.Value{}, err
	}
	if err := p.Post(ctx, msg); err != nil {
		return dhsc.Value{}, err
	}
	if err := sleepCtx(ctx, p.settle); err != nil {
		return dhsc.Value{}, err
	}
	row, err := p.ReadCVT(ctx, uid)
	if err != nil {
		return dhsc.Value{}, fmt.Errorf("kratos: SET %s=%s posted; read-back failed: %w", uid, text, err)
	}
	return dhsc.Value{Kind: dhsc.KindString, Str: string(row.Value), Raw: []byte(row.Value)}, nil
}

// Post sends one postMessage body (§5.39).
func (p *Plugin) Post(ctx context.Context, msg codec.Message) error {
	r, err := p.session()
	if err != nil {
		return err
	}
	_, err = r.post(ctx, "consumer/postMessage", msg.Encode())
	return err
}

// Command posts a command verb (§9). There is no acknowledge: the effect
// is seen in the values the verb changes and in the noteLog.
func (p *Plugin) Command(ctx context.Context, verb string, args ...string) (codec.Message, error) {
	msg, err := codec.CommandMessage(strings.ToUpper(verb), args...)
	if err != nil {
		return codec.Message{}, err
	}
	return msg, p.Post(ctx, msg)
}

// Table reads one REST table by query path (e.g. "consumer/positionLog",
// "consumer/alarmPointsList/antenna") and returns its envelope.
func (p *Plugin) Table(ctx context.Context, query string) (codec.Envelope, error) {
	r, err := p.session()
	if err != nil {
		return codec.Envelope{}, err
	}
	return r.get(ctx, query)
}

// resolve turns a request into a UID: Path first (no walk needed), then
// Label against the walked tree.
func (p *Plugin) resolve(req dhsc.ValueRequest) (string, error) {
	if req.Slot > 0 { // -1 = "every slot" (neutral watch); the IDU has slot 0 only
		return "", fmt.Errorf("kratos: slot %d: an NGC2 IDU has slot 0 only", req.Slot)
	}
	if req.Path != "" {
		return p.uid(req.Path), nil
	}
	if req.Label == "" {
		return "", fmt.Errorf("kratos: --path or --label is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var hits []string
	for uid, o := range p.byUID {
		if o.Label == req.Label && (req.Group == "" || o.Group == req.Group || strings.HasSuffix(o.Group, "/"+req.Group)) {
			hits = append(hits, uid)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return "", fmt.Errorf("%w: kratos: label %q (walk first, or give --path)", dhsc.ErrUnknownLabel, req.Label)
	}
	return "", fmt.Errorf("kratos: label %q is ambiguous (%d UIDs, e.g. %s) — give --path", req.Label, len(hits), hits[0])
}

func valueText(v dhsc.Value) string {
	switch v.Kind {
	case dhsc.KindBool:
		if v.Bool {
			return "1"
		}
		return "0"
	case dhsc.KindInt:
		return strconv.FormatInt(v.Int, 10)
	case dhsc.KindUint:
		return strconv.FormatUint(v.Uint, 10)
	case dhsc.KindFloat:
		return strconv.FormatFloat(v.Float, 'f', -1, 64)
	case dhsc.KindEnum:
		return strconv.Itoa(int(v.Enum))
	}
	if v.Str != "" {
		return v.Str
	}
	return string(v.Raw)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
