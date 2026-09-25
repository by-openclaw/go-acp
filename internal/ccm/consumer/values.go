package consumer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dhs/internal/ccm/codec"
	dhsc "dhs/internal/consumer"
	"dhs/internal/consumer/monitor"
)

// Reading one value, and watching many.
//
// A path here is the REST path plus the field path inside the
// resource, so resolving one means splitting it back into "the
// resource to GET" and "the field to take out of it". The spec is what
// draws that line: the longest prefix of the path that the spec
// declares as a GET is the resource, and the rest is the field. No
// walk is needed for it — which is what PathNative promises — so
// `get --path` on a cold connection is one round trip.

// GetValue reads one object by path.
func (p *Plugin) GetValue(ctx context.Context, req dhsc.ValueRequest) (dhsc.Value, error) {
	client, spec, err := p.session()
	if err != nil {
		return dhsc.Value{}, err
	}
	resource, field, err := split(spec, req.Path)
	if err != nil {
		return dhsc.Value{}, err
	}
	body, err := client.get(ctx, resource)
	if err != nil {
		return dhsc.Value{}, fmt.Errorf("ccm: get %s: %w", resource, err)
	}
	p.RecordRx()

	writable := false
	if tpl, ok := spec.TemplateFor(resource); ok {
		writable = spec.Writable(tpl)
	}
	for _, o := range flatten(resource, body, writable) {
		if strings.EqualFold(strings.Join(o.Path, "."), strings.Join(append(segments(resource), field...), ".")) {
			return o.Value, nil
		}
	}
	if len(field) == 0 {
		return dhsc.Value{}, fmt.Errorf("ccm: %s is a resource, not a value — name a field inside it", resource)
	}
	return dhsc.Value{}, fmt.Errorf("ccm: %s has no field %q", resource, strings.Join(field, "."))
}

// PathNative reports that this connector resolves a path without
// walking. The REST path IS the path, and the spec says which prefixes
// are resources, so nothing has to be discovered first.
func (p *Plugin) PathNative() bool { return true }

// split divides a dotted or slashed path into the resource to GET and
// the field path inside it.
//
// The longest declared GET prefix wins, so
// "io.sdi.<uuid>.name" splits at "/io/sdi/<uuid>" and not at "/io/sdi"
// — the deeper resource is the one that actually carries the field.
func split(spec *codec.Spec, path string) (string, []string, error) {
	raw := strings.TrimSpace(path)
	if raw == "" {
		return "", nil, fmt.Errorf("ccm: no path given")
	}
	// Accept both shapes an operator types: the REST one and the
	// dotted one every other connector uses.
	raw = strings.ReplaceAll(raw, ".", "/")
	segs := strings.Split(strings.Trim(raw, "/"), "/")

	for i := len(segs); i > 0; i-- {
		candidate := "/" + strings.Join(segs[:i], "/")
		if tpl, ok := spec.TemplateFor(candidate); ok && spec.Readable(tpl) {
			return candidate, segs[i:], nil
		}
	}
	return "", nil, fmt.Errorf("ccm: no resource in this device's api.yml covers %q", path)
}

// SetPollInterval sets how often a watch re-reads each object.
func (p *Plugin) SetPollInterval(d time.Duration) {
	if d <= 0 {
		return
	}
	p.mu.Lock()
	p.interval = d
	p.mu.Unlock()
}

// Subscribe watches objects by polling.
//
// REST only, by decision (../CLAUDE.md): there is no notification
// channel on this device, so a watch that waited for one would watch
// nothing. Same machinery as every other polled connector (ADR-0030
// through pollwatch).
func (p *Plugin) Subscribe(req dhsc.ValueRequest, fn dhsc.EventFunc) error {
	return p.poller.Subscribe(req, fn)
}

// Unsubscribe stops a watch.
func (p *Plugin) Unsubscribe(req dhsc.ValueRequest) error { return p.poller.Unsubscribe(req) }

// pollProfileFor builds the poll plan from the model, narrowed to the
// request's --path scope.
//
// Scoping matters here for the same reason it does on an agent: this
// device publishes thousands of leaves across hundreds of resources,
// and a watch that polled all of them would be a traffic source rather
// than a monitor.
func (p *Plugin) pollProfileFor(ctx context.Context, req dhsc.ValueRequest) (*monitor.Profile, error) {
	p.mu.Lock()
	tree, interval := p.tree, p.interval
	p.mu.Unlock()

	if len(tree) == 0 {
		var err error
		if tree, err = p.Walk(ctx, 0); err != nil {
			return nil, err
		}
	}

	filter := strings.ReplaceAll(strings.TrimSpace(req.Path), ".", "/")
	filter = strings.Trim(filter, "/")

	onChange := false
	prof := &monitor.Profile{
		Defaults: monitor.Defaults{Interval: monitor.Duration(interval), OnChange: true},
	}
	for _, o := range tree {
		path := strings.Join(o.Path, "/")
		if filter != "" && path != filter && !strings.HasPrefix(path, filter+"/") {
			continue
		}
		prof.Entries = append(prof.Entries, monitor.Entry{
			Path:     strings.Join(o.Path, "."),
			Slot:     0,
			Interval: monitor.Duration(interval),
			OnChange: &onChange,
		})
	}
	if len(prof.Entries) == 0 {
		return nil, fmt.Errorf("ccm: nothing to poll under %q (%d object(s) in the model)", req.Path, len(tree))
	}
	return prof, nil
}
