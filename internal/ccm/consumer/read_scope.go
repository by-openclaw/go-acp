package consumer

import (
	"context"
	"strings"

	"dhs/internal/ccm/codec"
	dhsc "dhs/internal/consumer"
)

// ReadScope serves every GetValue inside it from one GET per resource
// (consumer.ReadScoper). An import reads each row before writing it, and
// a Shuffler receiver alone carries a dozen fields: without the scope a
// 1 544-stream export was read back a dozen times over.
//
// The scope keeps each resource flattened, not only its body: a matrix
// state document holds 17 728 crosspoints and an export has a row for
// each, so flattening it again per row is a billion steps (measured: an
// hour of CPU on the SHUFFLE). A row is one lookup instead.
//
// Scopes nest; everything is dropped when the outermost one ends, so a
// later get, watch or long session always reads the device.
func (p *Plugin) ReadScope(context.Context) func() {
	p.mu.Lock()
	if p.reads == nil {
		p.reads = map[string][]byte{}
		p.readFields = map[string]map[string]dhsc.Value{}
	}
	p.readDepth++
	p.mu.Unlock()
	return func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.readDepth--; p.readDepth == 0 {
			p.reads, p.readFields = nil, nil
		}
	}
}

// readDoc GETs a resource, or hands back the copy a scope already holds.
func (p *Plugin) readDoc(ctx context.Context, client *Client, resource string) ([]byte, error) {
	p.mu.Lock()
	body, ok := p.reads[resource]
	p.mu.Unlock()
	if ok {
		return body, nil
	}
	body, err := client.get(ctx, resource)
	if err != nil {
		return nil, err
	}
	p.RecordRx()
	p.remember(resource, body)
	return body, nil
}

// fields is a resource's values by lower-cased dotted path, typed from
// the spec: flattened once per scope, every read outside one.
func (p *Plugin) fields(ctx context.Context, client *Client, spec *codec.Spec, resource string) (map[string]dhsc.Value, error) {
	p.mu.Lock()
	vals, ok := p.readFields[resource]
	p.mu.Unlock()
	if ok {
		return vals, nil
	}
	body, err := p.readDoc(ctx, client, resource)
	if err != nil {
		return nil, err
	}
	writable := false
	if tpl, ok := spec.TemplateFor(resource); ok {
		writable = spec.Writable(tpl)
	}
	objs := flatten(resource, body, writable)
	typeObjects(spec, resource, objs)
	vals = make(map[string]dhsc.Value, len(objs))
	for _, o := range objs {
		vals[strings.ToLower(strings.Join(o.Path, "."))] = o.Value
	}
	p.mu.Lock()
	if p.readFields != nil {
		p.readFields[resource] = vals
	}
	p.mu.Unlock()
	return vals, nil
}

// remember keeps what the device holds for a resource while a scope is
// open. A write calls it with its read-back, so a row read after the
// write sees the device as it is now.
func (p *Plugin) remember(resource string, body []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.reads != nil {
		p.reads[resource] = body
		delete(p.readFields, resource)
	}
}
