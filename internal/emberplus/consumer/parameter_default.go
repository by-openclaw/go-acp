package emberplus

import (
	"context"
	"fmt"

	"dhs/internal/consumer"
)

// ParameterDefault looks up the Glow Parameter's declared Default value
// (spec p.86 ParameterContents[12]) for the supplied request path/label
// and returns it as a consumer.Value.
//
// Returns:
//
//   - (Value, true, nil)  — Default is declared on the wire and was
//     converted to a typed consumer.Value matching the parameter's
//     current value kind.
//   - (Value{}, false, nil) — Parameter exists but has no Default
//     declared (the common case for many providers). Callers map this
//     to validation:no-default-declared.
//   - (Value{}, false, err) — request did not resolve to a known
//     parameter, or the Default's Go type does not match a representable
//     consumer.Value kind.
//
// Used by R14 #475 `set --ensure absent`: reset a Parameter to its
// declared default. Other callers (UI surfaces that want to show
// "factory" alongside "current") can use it too.
func (p *Plugin) ParameterDefault(ctx context.Context, req consumer.ValueRequest) (consumer.Value, bool, error) {
	if err := p.ensureWalked(ctx, "parameter-default"); err != nil {
		return consumer.Value{}, false, err
	}
	_, entry := p.findEntry(req)
	if entry == nil || entry.glowParam == nil {
		return consumer.Value{}, false, fmt.Errorf("emberplus: parameter not found for default lookup")
	}
	if entry.glowParam.Default == nil {
		return consumer.Value{}, false, nil
	}

	// Convert Glow's `any` payload to a typed consumer.Value. The Glow
	// decoder emits Go primitives (int64, float64, bool, string) via
	// decodeAnyValue (codec/glow/decoder.go) — match them here. The
	// resulting Kind tracks the parameter's current Value.Kind so a
	// SetValue with the returned default round-trips cleanly through
	// coerceStringToTyped + applyParameterConstraints.
	v := consumer.Value{Kind: entry.obj.Value.Kind}
	switch x := entry.glowParam.Default.(type) {
	case int64:
		v.Int = x
	case int:
		v.Int = int64(x)
	case int32:
		v.Int = int64(x)
	case uint64:
		v.Uint = x
	case uint:
		v.Uint = uint64(x)
	case float64:
		v.Float = x
	case float32:
		v.Float = float64(x)
	case bool:
		v.Bool = x
	case string:
		v.Str = x
	default:
		return consumer.Value{}, false,
			fmt.Errorf("emberplus: parameter Default has unsupported Go type %T", x)
	}
	return v, true, nil
}
