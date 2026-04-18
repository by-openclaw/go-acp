package glow

import (
	"acp/internal/protocol/emberplus/ber"
)

// EncodeGetDirectory builds the BER payload for a GetDirectory command
// at the root level. This is the first message a consumer sends.
// Per spec: dirFieldMask is optional. Omitting it = Default (all properties).
// Some older providers don't support dirFieldMask (Glow < 2.50).
func EncodeGetDirectory() []byte {
	cmd := ber.AppConstructed(TagCommand,
		ber.ContextConstructed(CmdNumber, ber.Integer(CmdGetDirectory)),
	)
	root := ber.AppConstructed(TagRootElementCollection, cmd)
	return ber.EncodeTLV(root)
}

// EncodeGetDirectoryFor builds a GetDirectory command for a specific
// node path. Used for lazy tree discovery.
func EncodeGetDirectoryFor(path []int32) []byte {
	cmd := ber.AppConstructed(TagCommand,
		ber.ContextConstructed(CmdNumber, ber.Integer(CmdGetDirectory)),
		ber.ContextConstructed(CmdDirMask, ber.Integer(-1)),
	)

	// Use QualifiedNode with RELATIVE-OID path for all depths.
	// This is what the Go emberlib uses: path as RELATIVE-OID inside Context(0).
	node := ber.AppConstructed(TagQualifiedNode,
		ber.ContextConstructed(QNodePath, ber.RelOID(encodeRelativeOID(path))),
		ber.ContextConstructed(QNodeChildren,
			ber.AppConstructed(TagElementCollection,
				ber.ContextConstructed(0, cmd),
			),
		),
	)
	root := ber.AppConstructed(TagRootElementCollection, node)
	return ber.EncodeTLV(root)
}

// EncodeSubscribe builds a Subscribe command for a parameter path.
func EncodeSubscribe(path []int32) []byte {
	cmd := ber.AppConstructed(TagCommand,
		ber.ContextConstructed(CmdNumber, ber.Integer(CmdSubscribe)),
	)
	param := ber.AppConstructed(TagQualifiedParameter,
		ber.ContextConstructed(QParamPath, ber.RelOID(encodeRelativeOID(path))),
		ber.ContextConstructed(QParamChildren,
			ber.AppConstructed(TagElementCollection, cmd),
		),
	)
	root := ber.AppConstructed(TagRootElementCollection, param)
	return ber.EncodeTLV(root)
}

// EncodeUnsubscribe builds an Unsubscribe command for a parameter path.
func EncodeUnsubscribe(path []int32) []byte {
	cmd := ber.AppConstructed(TagCommand,
		ber.ContextConstructed(CmdNumber, ber.Integer(CmdUnsubscribe)),
	)
	param := ber.AppConstructed(TagQualifiedParameter,
		ber.ContextConstructed(QParamPath, ber.RelOID(encodeRelativeOID(path))),
		ber.ContextConstructed(QParamChildren,
			ber.AppConstructed(TagElementCollection, cmd),
		),
	)
	root := ber.AppConstructed(TagRootElementCollection, param)
	return ber.EncodeTLV(root)
}

// EncodeSetValue builds a set-value message for a parameter.
// Per Glow DTD, contents is a SET wrapping the value context tag.
// Path must be CONTEXT[0] CONSTRUCTED wrapping RELATIVE-OID (same as QualifiedNode).
func EncodeSetValue(path []int32, value interface{}) []byte {
	valTLV := encodeAnyValue(value)
	param := ber.AppConstructed(TagQualifiedParameter,
		ber.ContextConstructed(QParamPath, ber.RelOID(encodeRelativeOID(path))),
		ber.ContextConstructed(QParamContents,
			ber.Set(ber.ContextConstructed(ParamContentValue, valTLV)),
		),
	)
	root := ber.AppConstructed(TagRootElementCollection, param)
	return ber.EncodeTLV(root)
}

// EncodeMatrixConnect builds a matrix connection command.
// Per Glow DTD: CONTEXT(5) → SEQUENCE → CONTEXT(0) → CONNECTION.
func EncodeMatrixConnect(matrixPath []int32, target int32, sources []int32, operation int64) []byte {
	conn := ber.AppConstructed(TagConnection,
		ber.ContextConstructed(ConnTarget, ber.Integer(int64(target))),
		ber.ContextConstructed(ConnSources, ber.RelOID(encodeRelativeOID(sources))),
		ber.ContextConstructed(ConnOperation, ber.Integer(operation)),
	)
	matrix := ber.AppConstructed(TagQualifiedMatrix,
		ber.ContextConstructed(0, ber.RelOID(encodeRelativeOID(matrixPath))),
		ber.ContextConstructed(5, // connections
			ber.Sequence(             // EMBER_SEQUENCE wrapper
				ber.ContextConstructed(0, conn), // per-connection CONTEXT(0) wrapper
			),
		),
	)
	root := ber.AppConstructed(TagRootElementCollection, matrix)
	return ber.EncodeTLV(root)
}

// EncodeInvoke builds a function invocation message.
// Per spec (p50): QualifiedFunction → children → Command(33) → Invocation.
// Arguments: CONTEXT[1] → SEQUENCE → CONTEXT[0] per argument value.
func EncodeInvoke(funcPath []int32, invocationID int32, args []interface{}) []byte {
	// Each argument wrapped in CONTEXT[0] per Dufour ContentParameter.Encode.
	var argTLVs []ber.TLV
	for _, arg := range args {
		argTLVs = append(argTLVs, ber.ContextConstructed(0, encodeAnyValue(arg)))
	}

	inv := ber.AppConstructed(TagInvocation,
		ber.ContextConstructed(InvInvocationID, ber.Integer(int64(invocationID))),
		ber.ContextConstructed(InvArguments, ber.Sequence(argTLVs...)),
	)
	cmd := ber.AppConstructed(TagCommand,
		ber.ContextConstructed(CmdNumber, ber.Integer(CmdInvoke)),
		ber.ContextConstructed(CmdInvID, inv),
	)
	fn := ber.AppConstructed(TagQualifiedFunction,
		ber.ContextConstructed(0, ber.RelOID(encodeRelativeOID(funcPath))),
		ber.ContextConstructed(2, // children
			ber.AppConstructed(TagElementCollection,
				ber.ContextConstructed(0, cmd), // per-element wrapper
			),
		),
	)
	root := ber.AppConstructed(TagRootElementCollection, fn)
	return ber.EncodeTLV(root)
}

// EncodeKeepAliveRequest builds a Glow-level keep-alive (just an empty root).
func EncodeKeepAliveRequest() []byte {
	root := ber.AppConstructed(TagRootElementCollection)
	return ber.EncodeTLV(root)
}

// --- helpers ---

// encodeRelativeOID encodes a path as RELATIVE-OID (base-128 per element).
func encodeRelativeOID(path []int32) []byte {
	var out []byte
	for _, v := range path {
		out = append(out, encodeBase128Int32(v)...)
	}
	return out
}

func encodeBase128Int32(v int32) []byte {
	if v < 0 {
		v = 0
	}
	uv := uint32(v)
	if uv < 128 {
		return []byte{byte(uv)}
	}
	var parts []byte
	for uv > 0 {
		parts = append([]byte{byte(uv & 0x7F)}, parts...)
		uv >>= 7
	}
	for i := 0; i < len(parts)-1; i++ {
		parts[i] |= 0x80
	}
	return parts
}

// encodeAnyValue encodes a Go value to a BER TLV.
func encodeAnyValue(v interface{}) ber.TLV {
	switch t := v.(type) {
	case int:
		return ber.Integer(int64(t))
	case int32:
		return ber.Integer(int64(t))
	case int64:
		return ber.Integer(t)
	case float64:
		return ber.Real(t)
	case float32:
		return ber.Real(float64(t))
	case string:
		return ber.UTF8(t)
	case bool:
		return ber.Boolean(t)
	case []byte:
		return ber.OctetStr(t)
	default:
		return ber.Integer(0)
	}
}
