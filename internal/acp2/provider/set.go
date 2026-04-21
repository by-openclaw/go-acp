package acp2

import (
	"encoding/binary"
	"fmt"
	"math"

	iacp2 "acp/internal/acp2/consumer"
)

// applySet mutates e per an incoming set_property request and returns
// the post-state Property (ready for the reply + the announce). The
// caller holds no locks; this function takes tree.mu's write lock.
//
// Numeric values are clamped to the entry's declared [min, max] when
// those constraints are present. Enum writes are rejected with
// ErrInvalidValue when the index is out of range. String writes are
// truncated to pid=6 max_length.
func (s *server) applySet(e *entry, in *iacp2.Property) (iacp2.Property, iacp2.ACP2ErrStatus, error) {
	if e.access&0x02 == 0 {
		return iacp2.Property{}, iacp2.ErrNoAccess, fmt.Errorf("no write access")
	}
	if in.PID != iacp2.PIDValue {
		// MVP only accepts writes to pid=8 (value). announce_delay
		// (pid=4) and event_prio (pid=17) are spec-writable too but
		// out of MVP scope.
		return iacp2.Property{}, iacp2.ErrInvalidPID, fmt.Errorf("only pid=8 is writable in MVP")
	}

	s.tree.mu.Lock()
	defer s.tree.mu.Unlock()

	switch e.objType {
	case iacp2.ObjTypeNumber:
		return s.applySetNumber(e, in)
	case iacp2.ObjTypeEnum:
		return s.applySetEnum(e, in)
	case iacp2.ObjTypeIPv4:
		return s.applySetIPv4(e, in)
	case iacp2.ObjTypeString:
		return s.applySetString(e, in)
	}
	return iacp2.Property{}, iacp2.ErrInvalidPID, fmt.Errorf("set on unsupported object type %d", e.objType)
}

// applySetNumber decodes the incoming numeric bytes per the entry's
// NumberType, clamps to declared min/max, and persists to
// canonical.Parameter.Value.
func (s *server) applySetNumber(e *entry, in *iacp2.Property) (iacp2.Property, iacp2.ACP2ErrStatus, error) {
	nt := e.numType
	iv, uv, fv, err := iacp2.DecodeNumericValue(nt, in.Data)
	if err != nil {
		return iacp2.Property{}, iacp2.ErrInvalidValue, err
	}

	switch nt {
	case iacp2.NumTypeS8, iacp2.NumTypeS16, iacp2.NumTypeS32, iacp2.NumTypeS64:
		if minV, ok := intConstraint(e.param.Minimum); ok && iv < minV {
			iv = minV
		}
		if maxV, ok := intConstraint(e.param.Maximum); ok && iv > maxV {
			iv = maxV
		}
		e.param.Value = iv
		data, err := iacp2.EncodeNumericValue(nt, iv, 0, 0)
		if err != nil {
			return iacp2.Property{}, iacp2.ErrInvalidValue, err
		}
		return numericProp(iacp2.PIDValue, nt, data), 0, nil

	case iacp2.NumTypeU8, iacp2.NumTypeU16, iacp2.NumTypeU32, iacp2.NumTypeU64, iacp2.NumTypePreset:
		if minV, ok := uintConstraint(e.param.Minimum); ok && uv < minV {
			uv = minV
		}
		if maxV, ok := uintConstraint(e.param.Maximum); ok && uv > maxV {
			uv = maxV
		}
		e.param.Value = uv
		data, err := iacp2.EncodeNumericValue(nt, 0, uv, 0)
		if err != nil {
			return iacp2.Property{}, iacp2.ErrInvalidValue, err
		}
		return numericProp(iacp2.PIDValue, nt, data), 0, nil

	case iacp2.NumTypeFloat:
		if minV, ok := floatConstraint(e.param.Minimum); ok && fv < minV {
			fv = minV
		}
		if maxV, ok := floatConstraint(e.param.Maximum); ok && fv > maxV {
			fv = maxV
		}
		e.param.Value = fv
		data, err := iacp2.EncodeNumericValue(nt, 0, 0, fv)
		if err != nil {
			return iacp2.Property{}, iacp2.ErrInvalidValue, err
		}
		return numericProp(iacp2.PIDValue, nt, data), 0, nil
	}
	return iacp2.Property{}, iacp2.ErrInvalidValue, fmt.Errorf("number_type %d not writable", nt)
}

func (s *server) applySetEnum(e *entry, in *iacp2.Property) (iacp2.Property, iacp2.ACP2ErrStatus, error) {
	if len(in.Data) < 4 {
		return iacp2.Property{}, iacp2.ErrInvalidValue, fmt.Errorf("enum needs u32")
	}
	idx := binary.BigEndian.Uint32(in.Data[0:4])
	opts := enumOptions(e.param)
	if int(idx) >= len(opts) {
		return iacp2.Property{}, iacp2.ErrInvalidValue, fmt.Errorf("enum index %d >= %d", idx, len(opts))
	}
	e.param.Value = int64(idx)
	return numericProp(iacp2.PIDValue, iacp2.NumTypeU32, u32Data(idx)), 0, nil
}

func (s *server) applySetIPv4(e *entry, in *iacp2.Property) (iacp2.Property, iacp2.ACP2ErrStatus, error) {
	if len(in.Data) < 4 {
		return iacp2.Property{}, iacp2.ErrInvalidValue, fmt.Errorf("ipv4 needs 4 bytes")
	}
	d := in.Data
	e.param.Value = fmt.Sprintf("%d.%d.%d.%d", d[0], d[1], d[2], d[3])
	return numericProp(iacp2.PIDValue, iacp2.NumTypeIPv4, d[:4]), 0, nil
}

func (s *server) applySetString(e *entry, in *iacp2.Property) (iacp2.Property, iacp2.ACP2ErrStatus, error) {
	raw := in.Data
	if n := len(raw); n > 0 && raw[n-1] == 0 {
		raw = raw[:n-1]
	}
	str := string(raw)
	if ml := maxLenHint(e.param); ml > 0 && uint16(len(str)) > ml {
		str = str[:ml]
	}
	e.param.Value = str
	return iacp2.MakeStringProperty(iacp2.PIDValue, str), 0, nil
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
