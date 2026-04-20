package acp1

import (
	"encoding/binary"
	"fmt"
	"math"

	"acp/internal/export/canonical"
	iacp1 "acp/internal/protocol/acp1"
)

// applyMutation dispatches the four mutating methods (setValue,
// setIncValue, setDecValue, setDefValue) for one entry and returns the
// post-state value bytes the reply + announcement must carry.
//
// The caller holds no locks; this function takes tree.mu's write lock
// so the canonical.Parameter.Value update is atomic with the encoded
// bytes we return.
//
// Spec behaviour (p.28):
//   - setValue: accept incoming bytes, clamp to [min,max].
//   - setIncValue: value += step, clamped to max.
//   - setDecValue: value -= step, clamped to min.
//   - setDefValue: value = default.
//
// The returned bytes are the SAME format as encodeValue's output — they
// are what getValue would return *after* the change. The ACP1 spec
// requires the reply to carry the confirmed-stored value, not the
// requested one.
func (s *server) applyMutation(e *entry, method iacp1.Method, incoming []byte) ([]byte, error) {
	s.tree.mu.Lock()
	defer s.tree.mu.Unlock()

	switch e.acpType {
	case iacp1.TypeInteger:
		return s.mutateInteger(e, method, incoming)
	case iacp1.TypeLong:
		return s.mutateLong(e, method, incoming)
	case iacp1.TypeByte:
		return s.mutateByte(e, method, incoming)
	case iacp1.TypeFloat:
		return s.mutateFloat(e, method, incoming)
	case iacp1.TypeIPAddr:
		return s.mutateIPAddr(e, method, incoming)
	case iacp1.TypeEnum:
		return s.mutateEnum(e, method, incoming)
	case iacp1.TypeString:
		return s.mutateString(e, method, incoming)
	}
	return nil, fmt.Errorf("applyMutation: unsupported type %d", e.acpType)
}

// ----------------------------------------------------------------- Integer

func (s *server) mutateInteger(e *entry, m iacp1.Method, incoming []byte) ([]byte, error) {
	p := e.param
	cur, err := asInt16(p.Value, "value")
	if err != nil {
		return nil, err
	}
	step, _ := asInt16Opt(p.Step, "step", 1)
	minV, _ := asInt16Opt(p.Minimum, "minimum", math.MinInt16)
	maxV, _ := asInt16Opt(p.Maximum, "maximum", math.MaxInt16)
	def, _ := asInt16Opt(p.Default, "default", 0)

	var next int32 // widen so increment/decrement can overflow cleanly
	switch m {
	case iacp1.MethodSetValue:
		if len(incoming) < 2 {
			return nil, fmt.Errorf("setValue integer: need 2 bytes, got %d", len(incoming))
		}
		next = int32(int16(binary.BigEndian.Uint16(incoming)))
	case iacp1.MethodSetIncValue:
		next = int32(cur) + int32(step)
	case iacp1.MethodSetDecValue:
		next = int32(cur) - int32(step)
	case iacp1.MethodSetDefValue:
		next = int32(def)
	default:
		return nil, fmt.Errorf("unexpected method %d", m)
	}

	clamped := clampInt32(next, int32(minV), int32(maxV))
	p.Value = int64(clamped)
	return writeI16(int16(clamped)), nil
}

// ----------------------------------------------------------------- Long

func (s *server) mutateLong(e *entry, m iacp1.Method, incoming []byte) ([]byte, error) {
	p := e.param
	cur, err := asInt32(p.Value, "value")
	if err != nil {
		return nil, err
	}
	step, _ := asInt32Opt(p.Step, "step", 1)
	minV, _ := asInt32Opt(p.Minimum, "minimum", math.MinInt32)
	maxV, _ := asInt32Opt(p.Maximum, "maximum", math.MaxInt32)
	def, _ := asInt32Opt(p.Default, "default", 0)

	var next int64
	switch m {
	case iacp1.MethodSetValue:
		if len(incoming) < 4 {
			return nil, fmt.Errorf("setValue long: need 4 bytes, got %d", len(incoming))
		}
		next = int64(int32(binary.BigEndian.Uint32(incoming)))
	case iacp1.MethodSetIncValue:
		next = int64(cur) + int64(step)
	case iacp1.MethodSetDecValue:
		next = int64(cur) - int64(step)
	case iacp1.MethodSetDefValue:
		next = int64(def)
	default:
		return nil, fmt.Errorf("unexpected method %d", m)
	}

	clamped := clampInt64(next, int64(minV), int64(maxV))
	p.Value = clamped
	return writeI32(int32(clamped)), nil
}

// ----------------------------------------------------------------- Byte

func (s *server) mutateByte(e *entry, m iacp1.Method, incoming []byte) ([]byte, error) {
	p := e.param
	cur, err := asUint8(p.Value, "value")
	if err != nil {
		return nil, err
	}
	step, _ := asUint8Opt(p.Step, "step", 1)
	minV, _ := asUint8Opt(p.Minimum, "minimum", 0)
	maxV, _ := asUint8Opt(p.Maximum, "maximum", math.MaxUint8)
	def, _ := asUint8Opt(p.Default, "default", 0)

	var next int32
	switch m {
	case iacp1.MethodSetValue:
		if len(incoming) < 1 {
			return nil, fmt.Errorf("setValue byte: need 1 byte, got 0")
		}
		next = int32(incoming[0])
	case iacp1.MethodSetIncValue:
		next = int32(cur) + int32(step)
	case iacp1.MethodSetDecValue:
		next = int32(cur) - int32(step)
	case iacp1.MethodSetDefValue:
		next = int32(def)
	default:
		return nil, fmt.Errorf("unexpected method %d", m)
	}

	clamped := uint8(clampInt32(next, int32(minV), int32(maxV)))
	p.Value = int64(clamped)
	return []byte{clamped}, nil
}

// ----------------------------------------------------------------- Float

func (s *server) mutateFloat(e *entry, m iacp1.Method, incoming []byte) ([]byte, error) {
	p := e.param
	cur, err := asFloat32(p.Value, "value")
	if err != nil {
		return nil, err
	}
	step, _ := asFloat32Opt(p.Step, "step", 1)
	minV, _ := asFloat32Opt(p.Minimum, "minimum", -math.MaxFloat32)
	maxV, _ := asFloat32Opt(p.Maximum, "maximum", math.MaxFloat32)
	def, _ := asFloat32Opt(p.Default, "default", 0)

	var next float64
	switch m {
	case iacp1.MethodSetValue:
		if len(incoming) < 4 {
			return nil, fmt.Errorf("setValue float: need 4 bytes, got %d", len(incoming))
		}
		next = float64(math.Float32frombits(binary.BigEndian.Uint32(incoming)))
	case iacp1.MethodSetIncValue:
		next = float64(cur) + float64(step)
	case iacp1.MethodSetDecValue:
		next = float64(cur) - float64(step)
	case iacp1.MethodSetDefValue:
		next = float64(def)
	default:
		return nil, fmt.Errorf("unexpected method %d", m)
	}

	clamped := clampFloat64(next, float64(minV), float64(maxV))
	p.Value = clamped
	return writeF32(float32(clamped)), nil
}

// ----------------------------------------------------------------- IPAddr

func (s *server) mutateIPAddr(e *entry, m iacp1.Method, incoming []byte) ([]byte, error) {
	p := e.param
	cur, err := ipv4ToUint32(p.Value)
	if err != nil {
		return nil, err
	}
	step, _ := ipv4ToUint32Opt(p.Step, 0)
	minV, _ := ipv4ToUint32Opt(p.Minimum, 0)
	maxV, _ := ipv4ToUint32Opt(p.Maximum, math.MaxUint32)
	def, _ := ipv4ToUint32Opt(p.Default, 0)

	var next uint64 // widen so overflow maths stays clean
	switch m {
	case iacp1.MethodSetValue:
		if len(incoming) < 4 {
			return nil, fmt.Errorf("setValue ipaddr: need 4 bytes, got %d", len(incoming))
		}
		next = uint64(binary.BigEndian.Uint32(incoming))
	case iacp1.MethodSetIncValue:
		next = uint64(cur) + uint64(step)
	case iacp1.MethodSetDecValue:
		if uint64(cur) < uint64(step) {
			next = 0
		} else {
			next = uint64(cur) - uint64(step)
		}
	case iacp1.MethodSetDefValue:
		next = uint64(def)
	default:
		return nil, fmt.Errorf("unexpected method %d", m)
	}

	if next > uint64(maxV) {
		next = uint64(maxV)
	}
	if next < uint64(minV) {
		next = uint64(minV)
	}
	clamped := uint32(next)
	p.Value = uint32ToDottedQuad(clamped)
	return writeU32(clamped), nil
}

// ----------------------------------------------------------------- Enum

func (s *server) mutateEnum(e *entry, m iacp1.Method, incoming []byte) ([]byte, error) {
	p := e.param
	items := enumItems(p)
	if len(items) == 0 {
		return nil, fmt.Errorf("enum %q has no items", p.Identifier)
	}
	maxIdx := uint8(len(items) - 1)

	var next uint8
	switch m {
	case iacp1.MethodSetValue:
		if len(incoming) < 1 {
			return nil, fmt.Errorf("setValue enum: need 1 byte, got 0")
		}
		next = incoming[0]
		if next > maxIdx {
			// Spec p.29 "invalid value" is ObjectErrCode 5 on ACP2;
			// ACP1's closest fit is OErrIllegalForType. Caller handles.
			return nil, fmt.Errorf("enum %q: index %d > max %d", p.Identifier, next, maxIdx)
		}
	case iacp1.MethodSetDefValue:
		def, _ := asUint8Opt(p.Default, "default", 0)
		next = def
		if next > maxIdx {
			next = 0
		}
	default:
		return nil, fmt.Errorf("unexpected method %d for enum", m)
	}

	p.Value = int64(next)
	return []byte{next}, nil
}

// ----------------------------------------------------------------- String

func (s *server) mutateString(e *entry, m iacp1.Method, incoming []byte) ([]byte, error) {
	if m != iacp1.MethodSetValue {
		return nil, fmt.Errorf("string %q: only setValue supported", e.param.Identifier)
	}
	// incoming is NUL-terminated on the wire. Strip the terminator if
	// present so our Go string carries only the content.
	raw := incoming
	if n := len(raw); n > 0 && raw[n-1] == 0 {
		raw = raw[:n-1]
	}
	maxLen := stringMaxLen(e.param)
	s2 := string(raw)
	if maxLen > 0 && len(s2) > int(maxLen) {
		s2 = s2[:maxLen]
	}
	e.param.Value = s2
	return writeCStr(s2), nil
}

// ----------------------------------------------------------------- helpers

func clampInt32(v, minV, maxV int32) int32 {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func clampInt64(v, minV, maxV int64) int64 {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func clampFloat64(v, minV, maxV float64) float64 {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func uint32ToDottedQuad(v uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		(v>>24)&0xff, (v>>16)&0xff, (v>>8)&0xff, v&0xff)
}

// assignFromAny coerces an `any` value (from the canonical JSON or from
// the provider.Provider.SetValue API path) to the correct native Go
// type for the entry and serialises it as ACP1 setValue bytes.
// Returns a DescribedError so the caller can map to the proper MCODE.
//
// Used by server.SetValue (API-driven writes) to route through the same
// mutation pipeline as the wire handlers.
func (s *server) encodeIncomingFromAny(e *entry, val any) ([]byte, error) {
	switch e.acpType {
	case iacp1.TypeInteger:
		v, err := asInt16(val, "value")
		if err != nil {
			return nil, err
		}
		return writeI16(v), nil
	case iacp1.TypeLong:
		v, err := asInt32(val, "value")
		if err != nil {
			return nil, err
		}
		return writeI32(v), nil
	case iacp1.TypeByte:
		v, err := asUint8(val, "value")
		if err != nil {
			return nil, err
		}
		return []byte{v}, nil
	case iacp1.TypeFloat:
		v, err := asFloat32(val, "value")
		if err != nil {
			return nil, err
		}
		return writeF32(v), nil
	case iacp1.TypeIPAddr:
		v, err := ipv4ToUint32(val)
		if err != nil {
			return nil, err
		}
		return writeU32(v), nil
	case iacp1.TypeEnum:
		v, err := asUint8(val, "value")
		if err != nil {
			return nil, err
		}
		return []byte{v}, nil
	case iacp1.TypeString:
		v, err := asString(val, "value")
		if err != nil {
			return nil, err
		}
		return writeCStr(v), nil
	}
	return nil, fmt.Errorf("encodeIncomingFromAny: unsupported type %d", e.acpType)
}

// convertStoredValue reads the mutated canonical.Parameter.Value back
// into a type-faithful `any` for the API-path return value. Used by
// server.SetValue after applyMutation.
func convertStoredValue(p *canonical.Parameter) any { return p.Value }
