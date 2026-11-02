package prism

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	dhsc "dhs/internal/consumer"
	"dhs/internal/consumer/monitor"
	"dhs/internal/prism/codec"
)

// GetValue reads one call: name, name/scope, optional ?field=.
func (p *Plugin) GetValue(ctx context.Context, req dhsc.ValueRequest) (dhsc.Value, error) {
	e, t, err := p.resolve(req)
	if err != nil {
		return dhsc.Value{}, err
	}
	if err := e.Check(t, false); err != nil {
		return dhsc.Value{}, validation(err)
	}
	reply, err := p.read(ctx, e, t)
	if err != nil {
		return dhsc.Value{}, err
	}
	return toValue(reply), nil
}

// read is one GET: the answer decoded, its deviations counted.
func (p *Plugin) read(ctx context.Context, e codec.Entry, t codec.Target) (codec.Reply, error) {
	r, err := p.session()
	if err != nil {
		return codec.Reply{}, err
	}
	raw, err := r.exchange(ctx, "GET", t.URLPath(), nil)
	if err != nil {
		return codec.Reply{}, err
	}
	p.RecordRx()
	reply, err := e.DecodeReply(raw, p.scalarForm(), t.Field != "")
	for _, d := range reply.Deviations {
		p.ComplianceProfile().Note(d)
	}
	if err != nil {
		return reply, fmt.Errorf("prism: GET %s: %w", t, err)
	}
	return reply, nil
}

func (p *Plugin) scalarForm() codec.ScalarForm {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.scalar
}

// SetValue posts one value, then reads the call back and returns what
// the instrument holds. A POST-only call has nothing to read back, and
// says so in the value it returns.
func (p *Plugin) SetValue(ctx context.Context, req dhsc.ValueRequest, val dhsc.Value) (dhsc.Value, error) {
	e, t, err := p.resolve(req)
	if err != nil {
		return dhsc.Value{}, err
	}
	if err := e.Check(t, true); err != nil {
		return dhsc.Value{}, validation(err)
	}
	r, err := p.session()
	if err != nil {
		return dhsc.Value{}, err
	}
	p.mu.Lock()
	allow := p.allow
	p.mu.Unlock()
	if e.Guarded && !allow {
		return dhsc.Value{}, &dhsc.ValidationError{Field: e.Name,
			Reason: "guarded: this call stops the instrument, destroys data or rewrites its identity; set " + EnvAllowActions + "=1 to send it"}
	}
	text := valueText(val)
	body, err := e.EncodeBody(text, p.scalarForm())
	if err != nil {
		return dhsc.Value{}, validation(err)
	}
	back := t
	if e.Scope == codec.ScopeInput {
		if back.Input, err = codec.InputOf(body); err != nil {
			return dhsc.Value{}, validation(err)
		}
	}

	next := answers(ctx)
	c, cancel := next()
	defer cancel()
	raw, err := r.exchange(c, "POST", t.URLPath(), body)
	if err != nil {
		return dhsc.Value{}, err
	}
	p.RecordRx()
	if err := codec.PostFailed(raw); err != nil {
		p.ComplianceProfile().Note(codec.DevErrorIn2xx)
		return dhsc.Value{}, fmt.Errorf("prism: POST %s: %w", t, err)
	}
	if e.Check(back, false) != nil {
		p.deps.Logger.Warn("prism: nothing to read back, the value is sent but not confirmed", "call", t.String())
		return dhsc.Value{Kind: dhsc.KindString, Str: text + " (sent, no read-back on this call)"}, nil
	}

	c2, cancel2 := next()
	defer cancel2()
	reply, err := p.read(c2, e, back)
	if err != nil {
		return dhsc.Value{}, fmt.Errorf("prism: POST %s=%s sent; read-back failed: %w", t, text, err)
	}
	if !sameValue(e, text, reply) {
		p.ComplianceProfile().Note(codec.DevReadbackDiffers)
		p.deps.Logger.Warn("prism: read-back differs from the value written", "call", t.String(), "wrote", text, "read", reply.Text)
	}
	return toValue(reply), nil
}

// sameValue compares what can be compared: an option name against the
// number a read answers cannot be, the page gives no numbers for them.
func sameValue(e codec.Entry, sent string, got codec.Reply) bool {
	if e.Type == codec.TypeObject || got.Single == nil {
		return true
	}
	a, errA := strconv.ParseFloat(sent, 64)
	b, errB := strconv.ParseFloat(got.Text, 64)
	if errA == nil && errB == nil {
		return a == b
	}
	if errA == nil || errB == nil {
		return e.Type != codec.TypeString && len(e.Options) > 0
	}
	return sent == got.Text
}

func toValue(r codec.Reply) dhsc.Value {
	switch x := r.Single.(type) {
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return dhsc.Value{Kind: dhsc.KindInt, Int: n}
		}
		if f, err := x.Float64(); err == nil {
			return dhsc.Value{Kind: dhsc.KindFloat, Float: f}
		}
	case bool:
		return dhsc.Value{Kind: dhsc.KindBool, Bool: x}
	}
	return dhsc.Value{Kind: dhsc.KindString, Str: r.Text}
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
	}
	if v.Str != "" {
		return v.Str
	}
	return string(v.Raw)
}

// resolve turns a request into a call of the catalogue. The walk names
// scoped objects Group=call, Label=scope.
func (p *Plugin) resolve(req dhsc.ValueRequest) (codec.Entry, codec.Target, error) {
	if req.Slot > 0 { // -1 = "every slot" (neutral watch)
		return codec.Entry{}, codec.Target{}, fmt.Errorf("prism: slot %d: an instrument has slot 0 only", req.Slot)
	}
	s := req.Path
	if s == "" {
		s = req.Label
		if req.Group != "" {
			s = req.Group + "/" + s
		}
	}
	t, err := codec.ParseTarget(s)
	if err != nil {
		return codec.Entry{}, codec.Target{}, validation(err)
	}
	e, ok := p.cat.Lookup(t.Name)
	if !ok {
		return codec.Entry{}, codec.Target{}, fmt.Errorf("%w: prism: %q is not a call of the page", dhsc.ErrUnknownLabel, t.Name)
	}
	return e, t, nil
}

func validation(err error) error {
	var inv *codec.Invalid
	if errors.As(err, &inv) {
		return &dhsc.ValidationError{Field: inv.Field, Reason: inv.Reason}
	}
	return err
}

// Subscribe watches calls by polling: the page documents no push channel.
func (p *Plugin) Subscribe(req dhsc.ValueRequest, fn dhsc.EventFunc) error {
	return p.poller.Subscribe(req, fn)
}

// Unsubscribe stops a watch.
func (p *Plugin) Unsubscribe(req dhsc.ValueRequest) error {
	return p.poller.Unsubscribe(req)
}

// pollProfileFor plans a watch. --path takes comma-separated calls; a
// call that needs a scope, named with none, polls every scope. With no path it
// polls the health calls, not the whole catalogue: a watch must not be a
// traffic source on a working instrument.
func (p *Plugin) pollProfileFor(_ context.Context, req dhsc.ValueRequest) (*monitor.Profile, error) {
	paths := []string{"system_status", "input_status", "ptp_status", "ip_signal_status/IP1", "ip_signal_status/IP2"}
	if req.Path != "" {
		paths = strings.Split(req.Path, ",")
	}
	p.mu.Lock()
	interval := p.interval
	p.mu.Unlock()
	prof := &monitor.Profile{Defaults: monitor.Defaults{Interval: monitor.Duration(interval), OnChange: true}}
	for _, path := range paths {
		e, t, err := p.resolve(dhsc.ValueRequest{Path: path})
		if err != nil {
			return nil, err
		}
		targets := []codec.Target{t}
		if t.Scope == "" && t.Input == "" && e.Scope != codec.ScopeNone && !e.ScopeOptional {
			targets = e.Targets()
		}
		for _, one := range targets {
			if err := e.Check(one, false); err != nil {
				return nil, validation(err)
			}
			prof.Entries = append(prof.Entries, monitor.Entry{Path: one.String()})
		}
	}
	return prof, nil
}
