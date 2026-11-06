package consumer

import "context"

// ReadScoper is the optional contract a Protocol implementation MAY
// satisfy to answer many GetValue calls from one read of the device.
//
// An import compares every row with what the device holds. On a REST
// device one GET returns a whole document — a Shuffler receiver carries
// a dozen fields — so reading it again for each row multiplies the load
// by the number of fields, and on a 1 544-stream device that is the
// difference between seconds and minutes. Inside the scope a document is
// read once and its fields are served from that read; a write inside the
// scope drops the documents it touched, so a read after it is fresh.
//
// The scope is explicit and short: outside it every GetValue reads the
// device, so a watch or a long-lived session never sees a stale value.
//
//	if r, ok := plug.(consumer.ReadScoper); ok { defer r.ReadScope(ctx)() }
type ReadScoper interface {
	ReadScope(ctx context.Context) (end func())
}
