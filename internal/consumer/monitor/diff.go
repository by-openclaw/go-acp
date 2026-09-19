package monitor

import (
	"bytes"
	"fmt"
	"net"

	"dhs/internal/consumer"
)

// valueEqual reports whether two decoded values are the same, comparing
// only the field the Kind selects. Change detection is built on it.
func valueEqual(a, b consumer.Value) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case consumer.KindBool:
		return a.Bool == b.Bool
	case consumer.KindInt:
		return a.Int == b.Int
	case consumer.KindUint:
		return a.Uint == b.Uint
	case consumer.KindFloat:
		return a.Float == b.Float
	case consumer.KindEnum:
		return a.Enum == b.Enum
	case consumer.KindString:
		return a.Str == b.Str
	case consumer.KindIPAddr:
		return a.IPAddr == b.IPAddr
	default:
		return bytes.Equal(a.Raw, b.Raw)
	}
}

// valueString renders a value for a FieldChange Old/New column without
// requiring the caller to know its Kind.
func valueString(v consumer.Value) string {
	switch v.Kind {
	case consumer.KindBool:
		return fmt.Sprintf("%t", v.Bool)
	case consumer.KindInt:
		return fmt.Sprintf("%d", v.Int)
	case consumer.KindUint:
		return fmt.Sprintf("%d", v.Uint)
	case consumer.KindFloat:
		return fmt.Sprintf("%g", v.Float)
	case consumer.KindEnum:
		return fmt.Sprintf("%d", v.Enum)
	case consumer.KindString:
		return v.Str
	case consumer.KindIPAddr:
		return net.IP(v.IPAddr[:]).String()
	default:
		if len(v.Raw) == 0 {
			return ""
		}
		return fmt.Sprintf("%x", v.Raw)
	}
}
