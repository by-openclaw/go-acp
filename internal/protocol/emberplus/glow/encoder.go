// Glow encoder — builds BER payloads for all Ember+ Glow messages.
// Encoding pattern per Ember+ spec v2.50 (Lawo GmbH):
//   ApplicationTag → Context(0)=number/path → Context(1)=contents(SET) → Context(2)=children
//
// Reference: assets/emberplus/Ember+ Documentation.pdf
package glow

import (
	"acp/internal/protocol/emberplus/ber"
)

// --- Root messages ---
//
// Spec p.93: RootElementCollection ::= [APPLICATION 11] IMPLICIT
// SEQUENCE OF [0] RootElement. Every child of the root collection must
// sit inside a CONTEXT[0] wrapper. Lax providers (TinyEmber+ Router)
// accept the wrapper-free form; strict providers (smh, dufour-based,
// DHD) refuse it. wrapRoot centralises the correct encoding.

func wrapRoot(child ber.TLV) ber.TLV {
	return ber.AppConstructed(TagRoot,
		ber.AppConstructed(TagRootElementCollection,
			ber.ContextConstructed(0, child),
		),
	)
}

// EncodeGetDirectory builds a root-level GetDirectory command with the
// optional dirFieldMask set to All (-1). Spec p.31: mask values are
// Default(0), Identifier(1), Description(2), Tree(3), Value(4), All(-1),
// Sparse(-2, Glow 2.50+). All is the most common consumer choice — it
// asks the provider to return every property. Older providers
// (TinyEmber+ DTD 2.31) ignore the field; strict providers require it.
func EncodeGetDirectory() []byte {
	cmd := ber.AppConstructed(TagCommand,
		ber.ContextConstructed(CmdCtxNumber, ber.Integer(CmdGetDirectory)),
		ber.ContextConstructed(CmdCtxDirMask, ber.Integer(-1)),
	)
	return ber.EncodeTLV(wrapRoot(cmd))
}

// EncodeGetDirectoryFor builds a GetDirectory for a specific node path.
// Pattern: QualifiedNode(path) → children → ElementCollection → Context(0) → Command(32)
// dirFieldMask=All (-1) per spec p.31.
func EncodeGetDirectoryFor(path []int32) []byte {
	cmd := ber.AppConstructed(TagCommand,
		ber.ContextConstructed(CmdCtxNumber, ber.Integer(CmdGetDirectory)),
		ber.ContextConstructed(CmdCtxDirMask, ber.Integer(-1)),
	)
	node := ber.AppConstructed(TagQualifiedNode,
		ber.ContextConstructed(QNodePath, ber.RelOID(encodeRelativeOID(path))),
		ber.ContextConstructed(QNodeChildren,
			ber.AppConstructed(TagElementCollection,
				ber.ContextConstructed(0, cmd),
			),
		),
	)
	return ber.EncodeTLV(wrapRoot(node))
}

// --- Parameter operations ---

// EncodeSetValue builds a set-value for a parameter.
// Pattern: QualifiedParameter(path) → contents(SET(value))
func EncodeSetValue(path []int32, value interface{}) []byte {
	valTLV := encodeAnyValue(value)
	param := ber.AppConstructed(TagQualifiedParameter,
		ber.ContextConstructed(QParamPath, ber.RelOID(encodeRelativeOID(path))),
		ber.ContextConstructed(QParamContents,
			ber.Set(ber.ContextConstructed(ParamContentValue, valTLV)),
		),
	)
	return ber.EncodeTLV(wrapRoot(param))
}

// EncodeSubscribe builds a Subscribe command for a parameter path.
// Pattern: QualifiedParameter(path) → children → ElementCollection → Context(0) → Command(30)
func EncodeSubscribe(path []int32) []byte {
	cmd := ber.AppConstructed(TagCommand,
		ber.ContextConstructed(CmdCtxNumber, ber.Integer(CmdSubscribe)),
	)
	param := ber.AppConstructed(TagQualifiedParameter,
		ber.ContextConstructed(QParamPath, ber.RelOID(encodeRelativeOID(path))),
		ber.ContextConstructed(QParamChildren,
			ber.AppConstructed(TagElementCollection,
				ber.ContextConstructed(0, cmd),
			),
		),
	)
	return ber.EncodeTLV(wrapRoot(param))
}

// EncodeUnsubscribe builds an Unsubscribe command for a parameter path.
func EncodeUnsubscribe(path []int32) []byte {
	cmd := ber.AppConstructed(TagCommand,
		ber.ContextConstructed(CmdCtxNumber, ber.Integer(CmdUnsubscribe)),
	)
	param := ber.AppConstructed(TagQualifiedParameter,
		ber.ContextConstructed(QParamPath, ber.RelOID(encodeRelativeOID(path))),
		ber.ContextConstructed(QParamChildren,
			ber.AppConstructed(TagElementCollection,
				ber.ContextConstructed(0, cmd),
			),
		),
	)
	return ber.EncodeTLV(wrapRoot(param))
}

// --- Matrix operations ---

// EncodeMatrixConnect builds a matrix connection command.
// Pattern: QualifiedMatrix(path) → connections(SEQUENCE(Context(0)(Connection)))
// Spec p42: Connection inside ConnectionCollection.
func EncodeMatrixConnect(matrixPath []int32, target int32, sources []int32, operation int64) []byte {
	conn := ber.AppConstructed(TagConnection,
		ber.ContextConstructed(ConnTarget, ber.Integer(int64(target))),
		ber.ContextConstructed(ConnSources, ber.RelOID(encodeRelativeOID(sources))),
		ber.ContextConstructed(ConnOperation, ber.Integer(operation)),
	)
	matrix := ber.AppConstructed(TagQualifiedMatrix,
		ber.ContextConstructed(0, ber.RelOID(encodeRelativeOID(matrixPath))),
		ber.ContextConstructed(5, // connections
			ber.Sequence(
				ber.ContextConstructed(0, conn),
			),
		),
	)
	return ber.EncodeTLV(wrapRoot(matrix))
}

// EncodeMatrixGetDirectory builds a GetDirectory on a matrix to fetch connections.
// Spec p42: "As soon as a consumer issues a GetDirectory on a matrix, it implicitly
// subscribes to matrix connection changes."
func EncodeMatrixGetDirectory(matrixPath []int32) []byte {
	cmd := ber.AppConstructed(TagCommand,
		ber.ContextConstructed(CmdCtxNumber, ber.Integer(CmdGetDirectory)),
	)
	matrix := ber.AppConstructed(TagQualifiedMatrix,
		ber.ContextConstructed(0, ber.RelOID(encodeRelativeOID(matrixPath))),
		ber.ContextConstructed(2, // children
			ber.AppConstructed(TagElementCollection,
				ber.ContextConstructed(0, cmd),
			),
		),
	)
	return ber.EncodeTLV(wrapRoot(matrix))
}

// --- Function operations ---

// EncodeInvoke builds a function invocation message.
// Pattern: QualifiedFunction(path) → children → ElementCollection → Context(0) → Command(33) → Invocation
// Spec p50: Command(33) contains Invocation at Context(2).
func EncodeInvoke(funcPath []int32, invocationID int32, args []interface{}) []byte {
	// Each argument wrapped in Context(0) per Dufour ContentParameter.Encode(0, writer).
	var argTLVs []ber.TLV
	for _, arg := range args {
		argTLVs = append(argTLVs, ber.ContextConstructed(0, encodeAnyValue(arg)))
	}

	inv := ber.AppConstructed(TagInvocation,
		ber.ContextConstructed(InvInvocationID, ber.Integer(int64(invocationID))),
		ber.ContextConstructed(InvArguments, ber.Sequence(argTLVs...)),
	)
	cmd := ber.AppConstructed(TagCommand,
		ber.ContextConstructed(CmdCtxNumber, ber.Integer(CmdInvoke)),
		ber.ContextConstructed(CmdCtxInvocation, inv),
	)
	fn := ber.AppConstructed(TagQualifiedFunction,
		ber.ContextConstructed(0, ber.RelOID(encodeRelativeOID(funcPath))),
		ber.ContextConstructed(2, // children
			ber.AppConstructed(TagElementCollection,
				ber.ContextConstructed(0, cmd),
			),
		),
	)
	return ber.EncodeTLV(wrapRoot(fn))
}

// --- Helpers ---

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

// encodeAnyValue encodes a Go value to a BER TLV using the correct universal tag.
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
