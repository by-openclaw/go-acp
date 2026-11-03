package consumer

import (
	"context"
	"fmt"

	"dhs/internal/rrcs/codec"
)

// Registration names the endpoint RRCS must call. There is no address in
// it: RRCS sends its events to "http://[ip-address of calling
// computer]:[TCPPort]/[URLPath]" (§8.15.1), so the source address of the
// registration request is where the events go.
type Registration struct {
	// Port is the TCP port our Listener is bound to.
	Port int
	// Path is the URL path of our Listener. Empty means DefaultPath.
	Path string
	// Pipelining lets RRCS send notifications without waiting for the
	// answer to the previous one (AllowHttpPipelining).
	Pipelining bool
}

func (r Registration) check() error {
	if r.Port < 1 || r.Port > 65535 {
		return fmt.Errorf("rrcs: registration port %d out of range", r.Port)
	}
	if !ValidPath(r.Path) {
		return fmt.Errorf("rrcs: registration path %q: ASCII without control characters or spaces only (§8.15.1)", r.Path)
	}
	return nil
}

// Register asks RRCS for every event (§8.15.1). AllowSystemMulticall is
// always sent as false: "RRCS does not support the 'system.multicall'
// yet".
func (c *Client) Register(ctx context.Context, r Registration) (Reply, error) {
	if err := r.check(); err != nil {
		return Reply{}, err
	}
	return c.Call(ctx, "RegisterForAllEvents",
		codec.Int(int32(r.Port)),
		codec.String(NormalizePath(r.Path)),
		codec.Bool(r.Pipelining),
		codec.Bool(false),
	)
}

// Unregister removes the registration made with the same port and path
// (§8.15.2).
func (c *Client) Unregister(ctx context.Context, r Registration) (Reply, error) {
	if err := r.check(); err != nil {
		return Reply{}, err
	}
	return c.Call(ctx, "UnregisterForAllEvents",
		codec.Int(int32(r.Port)),
		codec.String(NormalizePath(r.Path)),
	)
}

// IsRegistered asks whether RRCS still sends its events to our endpoint
// (§8.8 IsRegisteredForAllEvents). RRCS drops a registration silently
// when a GetAlive stays unanswered, so §9.9.1 has the control system ask
// periodically and register again on false.
func (c *Client) IsRegistered(ctx context.Context, r Registration) (bool, error) {
	if err := r.check(); err != nil {
		return false, err
	}
	reply, err := c.Call(ctx, "IsRegisteredForAllEvents",
		codec.Int(int32(r.Port)),
		codec.String(NormalizePath(r.Path)),
	)
	if err != nil {
		return false, err
	}
	v, ok := reply.Value.Field("IsRegistered")
	if !ok {
		return false, fmt.Errorf("%w: IsRegisteredForAllEvents: no member %q", codec.ErrMalformed, "IsRegistered")
	}
	return v.AsBool()
}
