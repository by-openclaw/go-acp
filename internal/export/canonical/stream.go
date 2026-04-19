package canonical

// StreamFormat values — Ember+ spec §5.3. Used as Parameter.StreamDescriptor.Format.
//
// Documented in docs/protocols/elements/stream.md §StreamFormat values.
const (
	StreamUnsignedInt8    = 0
	StreamUnsignedInt16BE = 1
	StreamUnsignedInt16LE = 2
	StreamUnsignedInt32BE = 3
	StreamUnsignedInt32LE = 4
	StreamUnsignedInt64BE = 5
	StreamUnsignedInt64LE = 6
	StreamSignedInt8      = 7
	StreamSignedInt16BE   = 8
	StreamSignedInt16LE   = 9
	StreamSignedInt32BE   = 10
	StreamSignedInt32LE   = 11
	StreamIEEEFloat32BE   = 12
	StreamIEEEFloat32LE   = 13
	StreamIEEEFloat64BE   = 14
	StreamIEEEFloat64LE   = 15
)

// StreamFormatName returns the canonical string name for a
// StreamFormat id. Useful for log messages and compliance events.
func StreamFormatName(id int) string {
	switch id {
	case StreamUnsignedInt8:
		return "unsignedInt8"
	case StreamUnsignedInt16BE:
		return "unsignedInt16BE"
	case StreamUnsignedInt16LE:
		return "unsignedInt16LE"
	case StreamUnsignedInt32BE:
		return "unsignedInt32BE"
	case StreamUnsignedInt32LE:
		return "unsignedInt32LE"
	case StreamUnsignedInt64BE:
		return "unsignedInt64BE"
	case StreamUnsignedInt64LE:
		return "unsignedInt64LE"
	case StreamSignedInt8:
		return "signedInt8"
	case StreamSignedInt16BE:
		return "signedInt16BE"
	case StreamSignedInt16LE:
		return "signedInt16LE"
	case StreamSignedInt32BE:
		return "signedInt32BE"
	case StreamSignedInt32LE:
		return "signedInt32LE"
	case StreamIEEEFloat32BE:
		return "ieeeFloat32BE"
	case StreamIEEEFloat32LE:
		return "ieeeFloat32LE"
	case StreamIEEEFloat64BE:
		return "ieeeFloat64BE"
	case StreamIEEEFloat64LE:
		return "ieeeFloat64LE"
	default:
		return "unknown"
	}
}
