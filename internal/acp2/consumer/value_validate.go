package acp2

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"dhs/internal/acp2/codec"
	"dhs/internal/protocol"
)

// ValidateValue is the offline pre-flight check for one (req, val) pair.
// Implements protocol.ValueValidator — the import / dry-run path uses
// it to surface bad obj-ids, enum-out-of-options, and basic type
// mismatches before any state-changing wire send.
//
// Distinct from Plugin.Validate (the ADR-0021 trace decoder).
//
// Pipeline (mirrors SetValue minus the set_property send):
//  1. Look up obj-id in the walked-tree cache; on miss, single
//     get_object. Device stat=1 → ErrObjectNotFound.
//  2. Type-check val against the object's objType / numType.
//  3. Enum / preset: val.Enum (or val.Str) must be in options.
//  4. IPv4: val.IPAddr non-zero OR val.Str parseable.
//
// Returns nil when the SetValue is safe to attempt; otherwise wraps
// protocol.ErrObjectNotFound or protocol.ErrValidationFailed.
func (p *Plugin) ValidateValue(ctx context.Context, req protocol.ValueRequest, val protocol.Value) error {
	p.mu.Lock()
	s := p.session
	p.mu.Unlock()
	if s == nil {
		return protocol.ErrNotConnected
	}

	tree, _ := p.trees.Get(req.Slot)

	objID, objType, numType, obj, err := p.resolveRequest(req, tree)
	if err != nil {
		return fmt.Errorf("%w: %v", protocol.ErrValidationFailed, err)
	}

	// No tree → fetch metadata via single get_object. ACP2Error stat=1
	// is the device telling us the obj-id is invalid.
	if objType == 0 && tree == nil {
		var miniTree *WalkedTree
		objType, numType, miniTree, err = p.fetchObjectMeta(ctx, s, uint8(req.Slot), objID)
		if err != nil {
			var acpErr *codec.ACP2Error
			if errors.As(err, &acpErr) && acpErr.Status == codec.ErrInvalidObjID {
				return fmt.Errorf("%w: obj-id %d", protocol.ErrObjectNotFound, objID)
			}
			return fmt.Errorf("%w: meta fetch failed: %v", protocol.ErrValidationFailed, err)
		}
		tree = miniTree
		if len(miniTree.Objects) > 0 {
			obj = &miniTree.Objects[0]
		}
	}

	// Build reverse enum map (label → wire index) for enum resolution.
	var reverseEnum map[string]uint32
	if tree != nil && obj != nil {
		for i, tobj := range tree.Objects {
			if tobj.ID == obj.ID && tree.OptionsMaps[i] != nil {
				reverseEnum = make(map[string]uint32, len(tree.OptionsMaps[i]))
				for wireIdx, label := range tree.OptionsMaps[i] {
					reverseEnum[label] = wireIdx
				}
				break
			}
		}
	}

	return validateValueAgainstType(objType, numType, obj, val, reverseEnum)
}

// validateValueAgainstType is the type-check half of Validate, separated
// so tests can drive it without setting up a Plugin + session.
func validateValueAgainstType(objType codec.ACP2ObjType, numType codec.NumberType, obj *protocol.Object, val protocol.Value, reverseEnum map[string]uint32) error {
	_ = numType
	// Raw bytes escape hatch — caller knows the wire shape, trust them.
	if len(val.Raw) > 0 && val.Str == "" && val.Int == 0 && val.Float == 0 && val.Uint == 0 {
		return nil
	}

	switch objType {
	case codec.ObjTypeNumber:
		// KindUnknown means the CSV row's value didn't parse as the
		// declared kind — that's the upstream signal for a bad type cell.
		if val.Kind == protocol.KindUnknown {
			return fmt.Errorf("%w: numeric object got unparseable value", protocol.ErrValidationFailed)
		}
		// A claimed numeric row with KindString and a non-empty Str means
		// the CSV had a non-numeric in the value column.
		if val.Kind == protocol.KindString && val.Str != "" {
			return fmt.Errorf("%w: numeric object got string value %q", protocol.ErrValidationFailed, val.Str)
		}
		return nil

	case codec.ObjTypeEnum, codec.ObjTypePreset:
		// Accept a Str that reverse-maps to a valid wire idx.
		if val.Str != "" && reverseEnum != nil {
			if _, ok := reverseEnum[val.Str]; ok {
				return nil
			}
		}
		// Accept val.Enum if it's a known wire idx.
		if reverseEnum != nil {
			wireIdx := uint32(val.Enum)
			for _, idx := range reverseEnum {
				if idx == wireIdx {
					return nil
				}
			}
			return fmt.Errorf("%w: enum value %d (label %q) not in options", protocol.ErrValidationFailed, val.Enum, val.Str)
		}
		// No options map captured — best-effort accept; device will catch.
		return nil

	case codec.ObjTypeIPv4:
		if val.IPAddr != [4]byte{} {
			return nil
		}
		if val.Str == "" {
			return fmt.Errorf("%w: ipv4 object got empty value", protocol.ErrValidationFailed)
		}
		parts := strings.Split(val.Str, ".")
		if len(parts) != 4 {
			return fmt.Errorf("%w: ipv4 value %q malformed", protocol.ErrValidationFailed, val.Str)
		}
		for _, p := range parts {
			n, err := strconv.Atoi(p)
			if err != nil || n < 0 || n > 255 {
				return fmt.Errorf("%w: ipv4 value %q malformed", protocol.ErrValidationFailed, val.Str)
			}
		}
		return nil

	case codec.ObjTypeString:
		// String objects accept any val.Str. Empty is allowed.
		return nil
	}

	// Node containers and unknown types — nothing actionable, let device handle.
	return nil
}

// Compile-time check that the plugin satisfies protocol.ValueValidator.
var _ protocol.ValueValidator = (*Plugin)(nil)
