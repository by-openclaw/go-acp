package emberplus

import (
	"acp/internal/export/canonical"
	"acp/internal/protocol/emberplus/ber"
	"acp/internal/protocol/emberplus/glow"
)

// encodeQualifiedFunction emits a [APPLICATION 20] QualifiedFunction. Only
// path + contents are emitted; children are not used for demo functions.
//
// Spec p.91 FunctionContents SET — ascending CTX-tag order:
//
//	0 identifier, 1 description, 2 arguments (TupleDescription), 3 result,
//	4 templateReference.
func (s *server) encodeQualifiedFunction(e *entry, f *canonical.Function) ber.TLV {
	var kids []ber.TLV
	kids = append(kids,
		ber.ContextConstructed(glow.FuncContentIdentifier, ber.UTF8(f.Identifier))) // [0]
	if f.Description != nil && *f.Description != "" {
		kids = append(kids,
			ber.ContextConstructed(glow.FuncContentDescription, ber.UTF8(*f.Description))) // [1]
	}
	if len(f.Arguments) > 0 {
		kids = append(kids,
			ber.ContextConstructed(glow.FuncContentArguments, encodeTupleDescription(f.Arguments))) // [2]
	}
	if len(f.Result) > 0 {
		kids = append(kids,
			ber.ContextConstructed(glow.FuncContentResult, encodeTupleDescription(f.Result))) // [3]
	}
	contents := ber.Set(kids...)
	return ber.AppConstructed(glow.TagQualifiedFunction,
		ber.ContextConstructed(glow.QFuncPath, ber.RelOID(encodeRelativeOID(e.oidParts))),
		ber.ContextConstructed(glow.QFuncContents, contents),
	)
}

// encodeTupleDescription emits a SEQUENCE OF [0] TupleItemDescription
// (spec p.91). Each item is [APP 21] SEQUENCE { [0] type, [1] name }.
func encodeTupleDescription(items []canonical.TupleItem) ber.TLV {
	out := make([]ber.TLV, 0, len(items))
	for _, t := range items {
		ti := encodeTupleItem(t)
		out = append(out, ber.ContextConstructed(0, ti))
	}
	return ber.Sequence(out...)
}

func encodeTupleItem(t canonical.TupleItem) ber.TLV {
	typeConst, ok := paramTypeConst(t.Type)
	if !ok {
		typeConst = glow.ParamTypeNull
	}
	fields := []ber.TLV{
		ber.ContextConstructed(glow.TupleType, ber.Integer(typeConst)),
	}
	if t.Name != "" {
		fields = append(fields,
			ber.ContextConstructed(glow.TupleName, ber.UTF8(t.Name)))
	}
	return ber.AppConstructed(glow.TagTupleItemDescription, fields...)
}

// encodeInvocationResult builds a top-level Root { InvocationResult } frame.
// Consumers receive this as the response to Command{Invoke, Invocation}.
//
// Spec p.92:
//
//	InvocationResult ::= [APPLICATION 23] SEQUENCE {
//	  invocationId [0] Integer32,
//	  success      [1] BOOLEAN OPTIONAL,  -- default true
//	  result       [2] Tuple    OPTIONAL
//	}
func (s *server) encodeInvocationResult(invID int32, success bool, result []any) []byte {
	fields := []ber.TLV{
		ber.ContextConstructed(glow.InvResInvocationID, ber.Integer(int64(invID))),
	}
	// Spec: "True or omitted if no errors". Emit only when false.
	if !success {
		fields = append(fields,
			ber.ContextConstructed(glow.InvResSuccess, ber.Boolean(false)))
	}
	if len(result) > 0 {
		fields = append(fields,
			ber.ContextConstructed(glow.InvResResult, encodeTupleValues(result)))
	}
	ir := ber.AppConstructed(glow.TagInvocationResult, fields...)
	root := ber.AppConstructed(glow.TagRoot, ir)
	return ber.EncodeTLV(root)
}

// encodeTupleValues turns a Go tuple into the wire Tuple shape
// (SEQUENCE OF [0] Value). Each value uses its Go type to pick the BER
// primitive — int/int64 → INTEGER, float → REAL, string → UTF8,
// bool → BOOLEAN, []byte → OCTET STRING, nil → NULL.
func encodeTupleValues(values []any) ber.TLV {
	items := make([]ber.TLV, 0, len(values))
	for _, v := range values {
		items = append(items, ber.ContextConstructed(0, encodeTupleValue(v)))
	}
	return ber.Sequence(items...)
}

func encodeTupleValue(v any) ber.TLV {
	switch t := v.(type) {
	case nil:
		return ber.Primitive(ber.ClassUniversal, ber.TagNull, nil)
	case bool:
		return ber.Boolean(t)
	case int:
		return ber.Integer(int64(t))
	case int32:
		return ber.Integer(int64(t))
	case int64:
		return ber.Integer(t)
	case float32:
		return ber.Real(float64(t))
	case float64:
		return ber.Real(t)
	case string:
		return ber.UTF8(t)
	case []byte:
		return ber.OctetStr(t)
	}
	// Fallback: NULL for unsupported types so the wire stays legal.
	return ber.Primitive(ber.ClassUniversal, ber.TagNull, nil)
}
