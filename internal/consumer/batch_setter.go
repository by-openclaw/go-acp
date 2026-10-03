package consumer

import "context"

// BatchSetter is the optional contract a Protocol implementation MAY
// satisfy to write several values in one device operation.
//
// Some values only make sense together: a static address and its
// gateway must land in the same document, or the device refuses each
// alone as inconsistent with the other (the FusioN answers 400 to a
// static_ip outside its static_gateway's subnet). A REST connector
// writing one whole document per field cannot express that; one that
// groups the fields by the document they live in can.
//
// reqs and vals are parallel. The answer is parallel to them: what the
// device holds after the write. A plugin that cannot write one of the
// fields fails before anything is sent for that document.
//
//	if b, ok := plug.(consumer.BatchSetter); ok { b.SetValues(...) }
type BatchSetter interface {
	SetValues(ctx context.Context, reqs []ValueRequest, vals []Value) ([]Value, error)
}
