package consumer

import "context"

// DMRef names the device model a point belongs to (ADR-0037 P4): the
// file of ADR-0022, .cache/dm/<proto>/<Model@SwRev>.json, the template
// in it and the path of the point inside that template. It is a pointer
// into the model, never a copy of it.
//
// A plugin fills what it knows without a read. Addr is the address of
// the thing in the manifest (the keys the plugin writes there at
// extract); the command line completes Model, SwRev and Template from
// the manifest entry that matches it. Addr is never printed.
type DMRef struct {
	Model    string `json:"model,omitempty"`
	SwRev    string `json:"sw_rev,omitempty"`
	Template string `json:"template,omitempty"`
	Path     string `json:"path,omitempty"`

	Addr map[string]any `json:"-"`
}

// IsZero reports whether nothing is known.
func (r DMRef) IsZero() bool {
	return r.Model == "" && r.SwRev == "" && r.Template == "" && r.Path == ""
}

// Describer is the optional contract of a plugin that knows the schema
// of a point without a walk (ADR-0037 P4): what the generic get and set
// print beside the value — kind, access, range, default, unit, named
// values — and the device model it belongs to. A plugin whose paths are
// the device's own addresses (PathNative) is the one that needs it: it
// has no walked tree to look the point up in.
type Describer interface {
	Describe(ctx context.Context, req ValueRequest) (Object, DMRef, error)
}
