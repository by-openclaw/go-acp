package acp2

import (
	"encoding/binary"
	"fmt"
	"math"

	"dhs/internal/acp2/codec"
	"dhs/internal/export/canonical"
)

// applySet mutates e per an incoming set_property request and returns
// the post-state Property (ready for the reply + the announce). The
// caller holds no locks; this function takes tree.mu's write lock.
//
// Numeric values are clamped to the entry's declared [min, max] when
// those constraints are present. Enum writes are rejected with
// ErrInvalidValue when the index is out of range. String writes are
// truncated to pid=6 max_length.
//
// Incoming property (set_property body, after obj-id + idx):
//
//	| Offset | Field | Width  | Notes                                     |
//	|--------|-------|--------|-------------------------------------------|
//	| 0      | pid   | u8     | must be 8 (PIDValue); otherwise InvalidPID|
//	| 1      | vtype | u8     | interpreted via e.numType, not in.VType   |
//	| 2-3    | plen  | u16 BE | 4 + len(in.Data)                          |
//	| 4..    | value | varies | decoded per objType by applySet* helpers  |
//
// Error mapping: no write access -> ErrNoAccess; pid != 8 -> ErrInvalidPID;
// numeric decode / enum range / type mismatch -> ErrInvalidValue.
// idx=0 is ACTIVE INDEX on preset children; never "first preset slot".
//
// Spec reference: acp2_protocol.pdf §set_property (func=3), §Error stat codes
func (s *server) applySet(e *entry, in *codec.Property) (codec.Property, codec.ACP2ErrStatus, error) {
	if e.access&0x02 == 0 {
		return codec.Property{}, codec.ErrNoAccess, fmt.Errorf("no write access")
	}
	if in.PID != codec.PIDValue {
		// MVP only accepts writes to pid=8 (value). announce_delay
		// (pid=4) and event_prio (pid=17) are spec-writable too but
		// out of MVP scope.
		return codec.Property{}, codec.ErrInvalidPID, fmt.Errorf("only pid=8 is writable in MVP")
	}

	s.tree.mu.Lock()
	defer s.tree.mu.Unlock()

	switch e.objType {
	case codec.ObjTypeNumber:
		return s.applySetNumber(e, in)
	case codec.ObjTypeEnum:
		return s.applySetEnum(e, in)
	case codec.ObjTypeIPv4:
		return s.applySetIPv4(e, in)
	case codec.ObjTypeString:
		return s.applySetString(e, in)
	}
	return codec.Property{}, codec.ErrInvalidPID, fmt.Errorf("set on unsupported object type %d", e.objType)
}

// applySetNumber decodes the incoming numeric bytes per the entry's
// NumberType, clamps to declared min/max, and persists to
// canonical.Parameter.Value.
//
// Reply property (and announce body) shape:
//
//	| Offset | Field | Width  | Notes                                     |
//	|--------|-------|--------|-------------------------------------------|
//	| 0      | pid   | u8     | 8 = value                                 |
//	| 1      | vtype | u8     | e.numType (S8..U64, Float)                |
//	| 2-3    | plen  | u16 BE | 4 + 4 (32-bit types) or 4 + 8 (S64/U64)   |
//	| 4..    | value | 4 or 8 | clamped to [Minimum, Maximum] when set    |
//
// Spec reference: acp2_protocol.pdf §Number Types, §set_property clamping
func (s *server) applySetNumber(e *entry, in *codec.Property) (codec.Property, codec.ACP2ErrStatus, error) {
	nt := e.numType
	iv, uv, fv, err := codec.DecodeNumericValue(nt, in.Data)
	if err != nil {
		return codec.Property{}, codec.ErrInvalidValue, err
	}

	switch nt {
	case codec.NumTypeS8, codec.NumTypeS16, codec.NumTypeS32, codec.NumTypeS64:
		if minV, ok := intConstraint(e.param.Minimum); ok && iv < minV {
			iv = minV
		}
		if maxV, ok := intConstraint(e.param.Maximum); ok && iv > maxV {
			iv = maxV
		}
		e.param.Value = iv
		data, err := codec.EncodeNumericValue(nt, iv, 0, 0)
		if err != nil {
			return codec.Property{}, codec.ErrInvalidValue, err
		}
		return numericProp(codec.PIDValue, nt, data), 0, nil

	case codec.NumTypeU8, codec.NumTypeU16, codec.NumTypeU32, codec.NumTypeU64, codec.NumTypePreset:
		if minV, ok := uintConstraint(e.param.Minimum); ok && uv < minV {
			uv = minV
		}
		if maxV, ok := uintConstraint(e.param.Maximum); ok && uv > maxV {
			uv = maxV
		}
		e.param.Value = uv
		data, err := codec.EncodeNumericValue(nt, 0, uv, 0)
		if err != nil {
			return codec.Property{}, codec.ErrInvalidValue, err
		}
		return numericProp(codec.PIDValue, nt, data), 0, nil

	case codec.NumTypeFloat:
		if minV, ok := floatConstraint(e.param.Minimum); ok && fv < minV {
			fv = minV
		}
		if maxV, ok := floatConstraint(e.param.Maximum); ok && fv > maxV {
			fv = maxV
		}
		e.param.Value = fv
		data, err := codec.EncodeNumericValue(nt, 0, 0, fv)
		if err != nil {
			return codec.Property{}, codec.ErrInvalidValue, err
		}
		return numericProp(codec.PIDValue, nt, data), 0, nil
	}
	return codec.Property{}, codec.ErrInvalidValue, fmt.Errorf("number_type %d not writable", nt)
}

// applySetEnum validates the incoming wire idx against the entry's
// EnumMap and persists it as int64 in canonical.Parameter.Value.
//
// Incoming / reply body:
//
//	| Offset | Field | Width  | Notes                                     |
//	|--------|-------|--------|-------------------------------------------|
//	| 0      | pid   | u8     | 8 = value                                 |
//	| 1      | vtype | u8     | reply uses NumTypeU32 (6)                 |
//	| 2-3    | plen  | u16 BE | 8                                         |
//	| 4-7    | idx   | u32 BE | option wire id; not in EnumMap -> Invalid |
//
// Real Neuron enums use sparse u32 wire ids (e.g. 641 "Main",
// 786 "Automatic"); the option's wire idx is stored in EnumMap.Value.
// The gate validates against the set of wire ids, NOT positional
// 0..N-1 (which would reject every valid Set when EnumMap is sparse).
//
// When the canonical lacks an EnumMap (legacy comma/newline
// Enumeration string), accept the positional 0..N-1 range as a
// fallback so legacy fixtures keep round-tripping.
//
// Spec reference: acp2_protocol.pdf §4.3.4 set property + §5.2.2 enum value
func (s *server) applySetEnum(e *entry, in *codec.Property) (codec.Property, codec.ACP2ErrStatus, error) {
	if len(in.Data) < 4 {
		return codec.Property{}, codec.ErrInvalidValue, fmt.Errorf("enum needs u32")
	}
	idx := binary.BigEndian.Uint32(in.Data[0:4])
	if !enumIdxValid(e.param, idx) {
		return codec.Property{}, codec.ErrInvalidValue,
			fmt.Errorf("enum idx %d not in EnumMap", idx)
	}
	e.param.Value = int64(idx)
	// Enum reply / announce uses vtype = NumTypePreset (9) per spec
	// §5.2.2 "Property value type". Real Neuron emits 09 09 00 08
	// + u32 BE on every Enum value reply (verified obj 17671/21127).
	// Returning NumTypeU32 (6) here would make the announce body
	// look like a Number to consumers — they'd then decode the body
	// via Number/Uint and lose the enum-label resolution path.
	return numericProp(codec.PIDValue, codec.NumTypePreset, u32Data(idx)), 0, nil
}

// enumIdxValid returns true when idx matches one of the wire ids
// declared on the canonical's EnumMap, or — if the canonical has no
// EnumMap — falls back to positional bounds against the legacy
// Enumeration string.
func enumIdxValid(p *canonical.Parameter, idx uint32) bool {
	if len(p.EnumMap) > 0 {
		for _, e := range p.EnumMap {
			if uint32(e.Value) == idx {
				return true
			}
		}
		return false
	}
	// Legacy fallback: no EnumMap, count newline/comma-separated labels
	// in the Enumeration string and accept idx in [0, N).
	if p.Enumeration != nil && *p.Enumeration != "" {
		raw := *p.Enumeration
		n := 1
		for _, c := range raw {
			if c == '\n' || c == ',' {
				n++
			}
		}
		return int(idx) < n
	}
	return false
}

// applySetIPv4 decodes four packed octets and stores them as a dotted-quad
// string on canonical.Parameter.Value.
//
// Incoming / reply body:
//
//	| Offset | Field | Width  | Notes                                     |
//	|--------|-------|--------|-------------------------------------------|
//	| 0      | pid   | u8     | 8 = value                                 |
//	| 1      | vtype | u8     | NumTypeIPv4 (10)                          |
//	| 2-3    | plen  | u16 BE | 8                                         |
//	| 4-7    | octets| 4      | d[0].d[1].d[2].d[3] big-endian packed     |
//
// Spec reference: acp2_protocol.pdf §Number Types (ipv4), §Wire Sizes
func (s *server) applySetIPv4(e *entry, in *codec.Property) (codec.Property, codec.ACP2ErrStatus, error) {
	if len(in.Data) < 4 {
		return codec.Property{}, codec.ErrInvalidValue, fmt.Errorf("ipv4 needs 4 bytes")
	}
	d := in.Data
	e.param.Value = fmt.Sprintf("%d.%d.%d.%d", d[0], d[1], d[2], d[3])
	return numericProp(codec.PIDValue, codec.NumTypeIPv4, d[:4]), 0, nil
}

// applySetString strips the trailing NUL, truncates to the per-entry
// pid=6 max_length hint, and persists the resulting UTF-8 string.
//
// Incoming / reply body:
//
//	| Offset | Field | Width    | Notes                                   |
//	|--------|-------|----------|-----------------------------------------|
//	| 0      | pid   | u8       | 8 = value                               |
//	| 1      | vtype | u8       | NumTypeString (11)                      |
//	| 2-3    | plen  | u16 BE   | 4 + len(utf8) + 1                       |
//	| 4..    | utf8  | len(raw) | UTF-8 bytes; truncated to maxLenHint    |
//	| end    | NUL   | 1        | 0x00 terminator                         |
//
// Spec reference: acp2_protocol.pdf §Wire Sizes (string), §5.4 pid=6
func (s *server) applySetString(e *entry, in *codec.Property) (codec.Property, codec.ACP2ErrStatus, error) {
	raw := in.Data
	if n := len(raw); n > 0 && raw[n-1] == 0 {
		raw = raw[:n-1]
	}
	str := string(raw)
	if ml := maxLenHint(e.param); ml > 0 && uint16(len(str)) > ml {
		str = str[:ml]
	}
	e.param.Value = str
	return codec.MakeStringProperty(codec.PIDValue, str), 0, nil
}

// intConstraint pulls an int64 from a canonical constraint field
// (Default / Min / Max / Step). Returns ok=false when the field is nil
// or non-integer.
func intConstraint(v any) (int64, bool) {
	if v == nil {
		return 0, false
	}
	n, err := asInt64(v, "constraint")
	if err != nil {
		return 0, false
	}
	return n, true
}

func uintConstraint(v any) (uint64, bool) {
	if v == nil {
		return 0, false
	}
	n, err := asUint64(v, "constraint")
	if err != nil {
		return 0, false
	}
	return n, true
}

func floatConstraint(v any) (float64, bool) {
	if v == nil {
		return 0, false
	}
	f, err := asFloat64(v, "constraint")
	if err != nil {
		return 0, false
	}
	if math.IsNaN(f) {
		return 0, false
	}
	return f, true
}
