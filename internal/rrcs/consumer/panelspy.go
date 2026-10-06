package consumer

import (
	"context"
	"fmt"

	"dhs/internal/rrcs/codec"
)

// Panel spy notifications (§9.9.2 – §9.9.6). A control system receives
// them for a panel after two steps: RegisterForAllEvents, then
// ChangePanelSpyRegistry for that panel (§9.9.1).
const (
	MethodPanelSpyStateChange  = "PanelSpyStateChange"
	MethodPanelSpyRotateEvent  = "PanelSpyRotateEvent"
	MethodPanelSpyKeyEvent     = "PanelSpyKeyEvent"
	MethodPanelSpyFuncKeyEvent = "PanelSpyFuncKeyEvent"
	MethodPanelSpyNumKeyEvent  = "PanelSpyNumKeyEvent"
)

// PanelSpy turns the four kinds of panel spy notification on or off for
// one panel (§8.12 ChangePanelSpyRegistry): keys, function keys, numeric
// keys and rotary encoders. r is the registration the notifications go
// to; "the method fails, if RegisterForAllEvents has not been called".
func (c *Client) PanelSpy(ctx context.Context, r Registration, node, port int, on bool) (Reply, error) {
	if err := r.check(); err != nil {
		return Reply{}, err
	}
	if node < 0 || port < 0 {
		return Reply{}, fmt.Errorf("rrcs: panel spy: node %d port %d: %w", node, port, codec.ErrRange)
	}
	info := codec.Struct(
		codec.Member{Name: "RotateEventsOn", Value: codec.Bool(on)},
		codec.Member{Name: "KeyEventsOn", Value: codec.Bool(on)},
		codec.Member{Name: "FuncKeyEventsOn", Value: codec.Bool(on)},
		codec.Member{Name: "NumKeyEventsOn", Value: codec.Bool(on)},
	)
	return c.Call(ctx, "ChangePanelSpyRegistry",
		codec.Int(int32(r.Port)),
		codec.String(NormalizePath(r.Path)),
		codec.Int(int32(node)),
		codec.Int(int32(port)),
		info,
	)
}
