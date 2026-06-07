package acp1

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
)

// fetchObjectMeta does a single getObject (MCODE=5) for one (slot,
// group, id) and decodes the reply into a consumer.Object + its
// fine-grained ACP1 ObjectType. Used by SetValue + ValidateValue when
// the slot tree was not walked — the per-row equivalent of Walker.Walk
// minus the recursion. Cost = 1 wire roundtrip per call.
//
// Mirrors ACP2's fetchObjectMeta pattern. Enables walk-free import
// for ACP1 (refs #421).
func (p *Plugin) fetchObjectMeta(ctx context.Context, slot int, group codec.ObjGroup, id byte) (consumer.Object, codec.ObjectType, error) {
	p.mu.Lock()
	c := p.client
	p.mu.Unlock()
	if c == nil {
		return consumer.Object{}, 0, consumer.ErrNotConnected
	}
	req := &codec.Message{
		MType:    codec.MTypeRequest,
		MAddr:    byte(slot),
		MCode:    byte(codec.MethodGetObject),
		ObjGroup: group,
		ObjID:    id,
	}
	reply, err := c.Do(ctx, req)
	if err != nil {
		return consumer.Object{}, 0, err
	}
	if reply.IsError() {
		return consumer.Object{}, 0, reply.ErrCode()
	}
	dec, err := codec.DecodeObject(reply.Value)
	if err != nil {
		return consumer.Object{}, 0, fmt.Errorf("acp1: decode getObject reply: %w", err)
	}
	obj := toProtocolObject(dec, slot, group, id)
	return obj, dec.Type, nil
}

// ValidateValue is the offline pre-flight check for one (req, val) pair.
// Implements consumer.ValueValidator — the import / dry-run path uses
// it to surface bad obj-ids, enum-out-of-options, and basic type
// mismatches before any state-changing wire send.
//
// Pipeline (mirrors SetValue minus the setValue send):
//  1. Resolve req → (group, id) via the local resolve(). Label-only
//     calls with no cached tree fail fast as ErrValidationFailed (cannot
//     resolve without a walk).
//  2. fetchObjectMeta to get the object's type + range constraints.
//     ACP1 ObjectErr stat=17 → ErrObjectNotFound.
//  3. Type-check val against the object's ACPType. Enum out of range,
//     numeric out of [Min, Max], IPv4 unparseable → ErrValidationFailed.
//  4. Write-access check: access bit 1 must be set. Otherwise
//     ErrValidationFailed (matches ACP1 stat=19 if the device were to be
//     asked).
//
// Returns nil when the SetValue is safe to attempt; otherwise wraps
// consumer.ErrObjectNotFound or consumer.ErrValidationFailed.
func (p *Plugin) ValidateValue(ctx context.Context, req consumer.ValueRequest, val consumer.Value) error {
	p.mu.Lock()
	c := p.client
	cache := p.trees
	p.mu.Unlock()
	if c == nil {
		return consumer.ErrNotConnected
	}

	var tree *SlotTree
	if cache != nil {
		tree, _ = cache.Get(req.Slot)
	}

	group, id, err := resolve(req, tree)
	if err != nil {
		return fmt.Errorf("%w: %v", consumer.ErrValidationFailed, err)
	}

	// Try the cached tree first to avoid a roundtrip when the operator
	// already walked the slot.
	obj, acpType, found := findObject(tree, group, id)
	if !found {
		var ferr error
		obj, acpType, ferr = p.fetchObjectMeta(ctx, req.Slot, group, id)
		if ferr != nil {
			var oerr codec.ObjectErr
			if errors.As(ferr, &oerr) && oerr.Code == codec.OErrInstanceNoExist {
				return fmt.Errorf("%w: group=%s id=%d", consumer.ErrObjectNotFound, group, id)
			}
			if errors.As(ferr, &oerr) && oerr.Code == codec.OErrGroupNoExist {
				return fmt.Errorf("%w: group=%s does not exist", consumer.ErrObjectNotFound, group)
			}
			return fmt.Errorf("%w: meta fetch failed: %v", consumer.ErrValidationFailed, ferr)
		}
	}

	return validateValueAgainstType(acpType, obj, val)
}

// validateValueAgainstType is the type-check half of ValidateValue,
// extracted so tests can drive it without setting up a Plugin + client.
//
// Returns nil if val is plausibly settable on obj; otherwise wraps
// consumer.ErrValidationFailed with a human-readable cause.
func validateValueAgainstType(t codec.ObjectType, obj consumer.Object, val consumer.Value) error {
	// Raw-bytes escape hatch — caller knows the wire shape, trust them.
	if len(val.Raw) > 0 && val.Str == "" && val.Int == 0 && val.Float == 0 && val.Uint == 0 {
		return nil
	}

	// Write-access check (access bit 1). Read-only objects fail here
	// rather than wasting a roundtrip to learn the device's no-write
	// reply.
	if obj.Access&0x02 == 0 {
		return fmt.Errorf("%w: object is read-only", consumer.ErrValidationFailed)
	}

	switch t {
	case codec.TypeInteger, codec.TypeLong:
		// KindUnknown means the upstream CSV parser couldn't parse the
		// cell as the declared kind — a strong signal for "bad type".
		if val.Kind == consumer.KindUnknown {
			return fmt.Errorf("%w: integer object got unparseable value", consumer.ErrValidationFailed)
		}
		if val.Kind == consumer.KindString && val.Str != "" {
			return fmt.Errorf("%w: integer object got string value %q", consumer.ErrValidationFailed, val.Str)
		}
		v := val.Int
		// Cross-kind tolerance: KindUint / KindFloat callers can still
		// resolve to a valid int. Float gets truncated.
		if val.Kind == consumer.KindUint {
			v = int64(val.Uint)
		}
		if val.Kind == consumer.KindFloat {
			v = int64(val.Float)
		}
		if mn, ok := obj.Min.(int64); ok && v < mn {
			return fmt.Errorf("%w: value %d below Min %d", consumer.ErrValidationFailed, v, mn)
		}
		if mx, ok := obj.Max.(int64); ok && v > mx {
			return fmt.Errorf("%w: value %d above Max %d", consumer.ErrValidationFailed, v, mx)
		}
		return nil

	case codec.TypeByte:
		if val.Kind == consumer.KindUnknown {
			return fmt.Errorf("%w: byte object got unparseable value", consumer.ErrValidationFailed)
		}
		var u uint64
		switch val.Kind {
		case consumer.KindUint, consumer.KindEnum:
			u = val.Uint
		case consumer.KindInt:
			if val.Int < 0 {
				return fmt.Errorf("%w: byte object got negative value %d", consumer.ErrValidationFailed, val.Int)
			}
			u = uint64(val.Int)
		default:
			return fmt.Errorf("%w: byte object got non-numeric kind %v", consumer.ErrValidationFailed, val.Kind)
		}
		if u > 255 {
			return fmt.Errorf("%w: byte value %d out of range 0..255", consumer.ErrValidationFailed, u)
		}
		return nil

	case codec.TypeFloat:
		if val.Kind == consumer.KindUnknown {
			return fmt.Errorf("%w: float object got unparseable value", consumer.ErrValidationFailed)
		}
		f := val.Float
		switch val.Kind {
		case consumer.KindInt:
			f = float64(val.Int)
		case consumer.KindUint:
			f = float64(val.Uint)
		case consumer.KindString:
			if val.Str == "" {
				return fmt.Errorf("%w: float object got empty value", consumer.ErrValidationFailed)
			}
			return fmt.Errorf("%w: float object got string value %q", consumer.ErrValidationFailed, val.Str)
		}
		if mn, ok := obj.Min.(float64); ok && f < mn {
			return fmt.Errorf("%w: value %v below Min %v", consumer.ErrValidationFailed, f, mn)
		}
		if mx, ok := obj.Max.(float64); ok && f > mx {
			return fmt.Errorf("%w: value %v above Max %v", consumer.ErrValidationFailed, f, mx)
		}
		return nil

	case codec.TypeIPAddr:
		// Accept either parsed IPAddr (already validated upstream) or a
		// dotted string we can split.
		if val.IPAddr != [4]byte{} {
			return nil
		}
		if val.Str == "" {
			return fmt.Errorf("%w: ipv4 object got empty value", consumer.ErrValidationFailed)
		}
		parts := strings.Split(val.Str, ".")
		if len(parts) != 4 {
			return fmt.Errorf("%w: ipv4 value %q malformed", consumer.ErrValidationFailed, val.Str)
		}
		for _, p := range parts {
			n, err := strconv.Atoi(p)
			if err != nil || n < 0 || n > 255 {
				return fmt.Errorf("%w: ipv4 value %q malformed", consumer.ErrValidationFailed, val.Str)
			}
		}
		return nil

	case codec.TypeEnum:
		// Delegate to the same resolver the encoder uses (coerceEnum) so the
		// pre-flight check accepts EXACTLY what SetValue would accept: a known
		// label OR a numeric index in range. A non-empty value that is neither
		// (e.g. a mistyped option name) is rejected here as a client-side
		// validation error (exit 2) instead of failing later at encode time
		// (exit 1). Previously this branch treated an unmatched non-numeric
		// label as index 0 and wrongly accepted it.
		if _, err := coerceEnum(obj.EnumItems, val); err != nil {
			return fmt.Errorf("%w: %v", consumer.ErrValidationFailed, err)
		}
		return nil

	case codec.TypeString:
		// Strings accept any val.Str. Length check uses MaxLen when set.
		if obj.MaxLen > 0 && len(val.Str) > obj.MaxLen {
			return fmt.Errorf("%w: string length %d exceeds MaxLen %d",
				consumer.ErrValidationFailed, len(val.Str), obj.MaxLen)
		}
		return nil

	case codec.TypeAlarm, codec.TypeFile, codec.TypeFrame, codec.TypeRoot:
		// Set on these is not a normal use case — let the device reply
		// drive the outcome. Don't pre-flight reject.
		return nil
	}
	// Unknown type — let the device decide.
	return nil
}

// Compile-time check that the plugin satisfies consumer.ValueValidator.
var _ consumer.ValueValidator = (*Plugin)(nil)
