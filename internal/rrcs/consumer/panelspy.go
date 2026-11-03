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
	MethodPanelSpyStateChange = "PanelSpyStateChange"
	// MethodPanelSpyStateChanged is the name a real RRCS 9.0 sends; the
	// specification prints it without the final d (§9.9.2).
	MethodPanelSpyStateChanged = "PanelSpyStateChanged"
	MethodPanelSpyRotateEvent  = "PanelSpyRotateEvent"
	MethodPanelSpyKeyEvent     = "PanelSpyKeyEvent"
	MethodPanelSpyFuncKeyEvent = "PanelSpyFuncKeyEvent"
	MethodPanelSpyNumKeyEvent  = "PanelSpyNumKeyEvent"
)

// PanelSpyKinds are the kinds of panel spy notification, in the order the
// request carries them (§8.12).
var PanelSpyKinds = []string{"Rotate", "Key", "FuncKey", "NumKey"}

// PanelSpy turns the four kinds of panel spy notification on or off for
// one panel (§8.12 ChangePanelSpyRegistry): keys, function keys, numeric
// keys and rotary encoders. r is the registration the notifications go
// to; "the method fails, if RegisterForAllEvents has not been called".
//
// kinds names the notifications to switch: "Key", "Rotate", "FuncKey",
// "NumKey"; none means all four. A kind that is left out is not touched
// ("if not present, the notification feature is nor turned on neither
// turned off"). A real RRCS 9.0 reports an error state for every kind a
// panel type does not have — a smart panel has no function or numeric
// keys — so asking only for what the panel has keeps the answers clean.
func (c *Client) PanelSpy(ctx context.Context, r Registration, node, port int, on bool, kinds ...string) (Reply, error) {
	if err := r.check(); err != nil {
		return Reply{}, err
	}
	if node < 0 || port < 0 {
		return Reply{}, fmt.Errorf("rrcs: panel spy: node %d port %d: %w", node, port, codec.ErrRange)
	}
	if len(kinds) == 0 {
		kinds = PanelSpyKinds
	}
	members := make([]codec.Member, 0, len(kinds))
	for _, want := range PanelSpyKinds {
		for _, k := range kinds {
			if k == want {
				members = append(members, codec.Member{Name: want + "EventsOn", Value: codec.Bool(on)})
			}
		}
	}
	if len(members) == 0 {
		return Reply{}, fmt.Errorf("rrcs: panel spy: no known kind in %v: %w", kinds, codec.ErrRange)
	}
	info := codec.Struct(members...)
	return c.Call(ctx, "ChangePanelSpyRegistry",
		codec.Int(int32(r.Port)),
		codec.String(NormalizePath(r.Path)),
		codec.Int(int32(node)),
		codec.Int(int32(port)),
		info,
	)
}
