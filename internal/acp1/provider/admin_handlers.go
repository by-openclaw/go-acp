package acp1

import (
	"context"
	"fmt"

	"dhs/internal/acp1/codec"
)

func init() {
	registerAdminHandler("ping", handlePing)
	registerAdminHandler("slot.load", handleSlotLoad)
	registerAdminHandler("slot.unload", handleSlotUnload)
	registerAdminHandler("slot.state", handleSlotState)
	registerAdminHandler("value.set", handleValueSet)
	registerAdminHandler("value.get", handleValueGet)
	registerAdminHandler("reload", handleReload)
}

// handlePing is a liveness probe. Used by tests + the CLI's `admin
// ping` to confirm a running provider before issuing real verbs.
func handlePing(_ context.Context, s *server, _ map[string]any) (*AdminResponse, error) {
	return &AdminResponse{
		Status: "ok",
		Result: map[string]any{
			"slots": s.tree.numSlots(),
		},
	}, nil
}

// handleSlotLoad is reserved for the DM-library loader (#260). For now
// it returns a clear "not implemented yet" diagnostic so the verb is
// reachable from the CLI but the actual schema-load wiring waits.
func handleSlotLoad(_ context.Context, _ *server, _ map[string]any) (*AdminResponse, error) {
	return &AdminResponse{
		Status:  "error",
		Message: "slot.load: DM-library loader lands in #260; verb is plumbed but not yet wired",
	}, nil
}

// handleSlotUnload mirrors handleSlotLoad — full implementation lands
// in #260 alongside the loader.
func handleSlotUnload(_ context.Context, _ *server, _ map[string]any) (*AdminResponse, error) {
	return &AdminResponse{
		Status:  "error",
		Message: "slot.unload: DM-library loader lands in #260",
	}, nil
}

// handleSlotState mutates the rack-controller's frame-status array for
// one slot. The full state-machine cascade (no_card → powerup → boot
// → present with realistic timings) lands in #259; this handler does
// the immediate-jump variant per the issue's `slot state * → error`
// shortcut.
func handleSlotState(_ context.Context, s *server, args map[string]any) (*AdminResponse, error) {
	slot, err := readIntArg(args, "slot")
	if err != nil {
		return nil, err
	}
	stateStr, err := readStringArg(args, "state")
	if err != nil {
		return nil, err
	}
	state, err := parseSlotStatus(stateStr)
	if err != nil {
		return nil, err
	}
	if err := s.setSlotStatus(uint8(slot), state); err != nil {
		return nil, err
	}
	return &AdminResponse{
		Result: map[string]any{
			"slot":  slot,
			"state": stateStr,
		},
	}, nil
}

// handleValueSet drives a SetValue through the API path. The path
// format mirrors the existing s.SetValue contract:
// "1.<slot+1>.<group_num>.<id>".
func handleValueSet(ctx context.Context, s *server, args map[string]any) (*AdminResponse, error) {
	path, err := readStringArg(args, "path")
	if err != nil {
		return nil, err
	}
	value, ok := args["value"]
	if !ok {
		return nil, fmt.Errorf("value.set: missing arg %q", "value")
	}
	got, err := s.SetValue(ctx, path, value)
	if err != nil {
		return nil, err
	}
	return &AdminResponse{
		Result: map[string]any{
			"path":  path,
			"value": got,
		},
	}, nil
}

// handleValueGet reads one object value through the wire dispatcher.
// Synthesises a getValue request and returns the decoded value bytes
// as base64-equivalent (raw bytes) so callers can post-process.
func handleValueGet(_ context.Context, s *server, args map[string]any) (*AdminResponse, error) {
	slot, err := readIntArg(args, "slot")
	if err != nil {
		return nil, err
	}
	groupNum, err := readIntArg(args, "group")
	if err != nil {
		return nil, err
	}
	id, err := readIntArg(args, "id")
	if err != nil {
		return nil, err
	}

	req := &codec.Message{
		MType:    codec.MTypeRequest,
		MAddr:    uint8(slot),
		MCode:    byte(codec.MethodGetValue),
		ObjGroup: codec.ObjGroup(groupNum),
		ObjID:    uint8(id),
	}
	rep, _ := s.handleRequest(req)
	if rep == nil {
		return nil, fmt.Errorf("value.get: no reply (object missing or method unsupported)")
	}
	if rep.IsError() {
		return nil, rep.ErrCode()
	}
	return &AdminResponse{
		Result: map[string]any{
			"slot":  slot,
			"group": groupNum,
			"id":    id,
			"raw":   rep.Value,
		},
	}, nil
}

// handleReload re-reads the served tree from disk. Implemented as a
// stub for now — the file path is not stored on the server. Wiring
// happens alongside #259 when the producer CLI passes its --tree path
// down for hot-reload.
func handleReload(_ context.Context, _ *server, _ map[string]any) (*AdminResponse, error) {
	return &AdminResponse{
		Status:  "error",
		Message: "reload: tree-path bookkeeping lands alongside #259",
	}, nil
}

// ---- helpers -----------------------------------------------------------

func readIntArg(args map[string]any, key string) (int, error) {
	raw, ok := args[key]
	if !ok {
		return 0, fmt.Errorf("missing arg %q", key)
	}
	switch v := raw.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	}
	return 0, fmt.Errorf("arg %q: want number, got %T", key, raw)
}

func readStringArg(args map[string]any, key string) (string, error) {
	raw, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing arg %q", key)
	}
	s, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("arg %q: want string, got %T", key, raw)
	}
	return s, nil
}
