package emberplus

import (
	"fmt"

	"dhs/internal/emberplus/codec/ber"
	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/export/canonical"
)

// encodeQualifiedTemplate emits a QualifiedTemplate [APPLICATION 25]
// from a canonical TemplateEntry. The inner prototype element (Node,
// Parameter, Matrix or Function) is wrapped as the corresponding
// non-qualified APPLICATION tag per spec p.84 TemplateElement CHOICE.
//
//	QualifiedTemplate ::= [APPLICATION 25] SET {
//	  path        [0] RELATIVE-OID,
//	  element     [1] TemplateElement OPTIONAL,
//	  description [2] EmberString     OPTIONAL
//	}
//
//	TemplateElement ::= CHOICE {
//	  Parameter [APP 1]
//	  Node      [APP 3]
//	  Matrix    [APP 13]
//	  Function  [APP 19]
//	}
//
// Spec reference: Ember+ Documentation.pdf §Template + QualifiedTemplate p. 84.
func encodeQualifiedTemplate(te *canonical.TemplateEntry) (ber.TLV, error) {
	parts, err := parseOID(te.OID)
	if err != nil {
		return ber.TLV{}, fmt.Errorf("template OID %q: %w", te.OID, err)
	}
	fields := []ber.TLV{
		ber.ContextConstructed(glow.TemplatePath, ber.RelOID(encodeRelativeOID(parts))),
	}
	if te.Template != nil {
		inner, err := encodeTemplateElement(te.Template)
		if err != nil {
			return ber.TLV{}, fmt.Errorf("template %q element: %w", te.Identifier, err)
		}
		fields = append(fields,
			ber.ContextConstructed(glow.TemplateElementCtx, inner))
	}
	if te.Description != nil && *te.Description != "" {
		fields = append(fields,
			ber.ContextConstructed(glow.TemplateDescription, ber.UTF8(*te.Description)))
	}
	return ber.AppConstructed(glow.TagQualifiedTemplate, fields...), nil
}

// encodeTemplateElement dispatches the canonical prototype to the right
// non-qualified APP encoder. The prototype's Header.Number lands as the
// element's CTX[0] number per spec p.84.
func encodeTemplateElement(el canonical.Element) (ber.TLV, error) {
	hdr := el.Common()
	num := int32(hdr.Number)
	switch v := el.(type) {
	case *canonical.Node:
		return encodeNonQualNode(num, v), nil
	case *canonical.Parameter:
		return encodeNonQualParameter(num, v), nil
	case *canonical.Matrix:
		return encodeNonQualMatrix(num, v)
	case *canonical.Function:
		return encodeNonQualFunction(num, v), nil
	}
	return ber.TLV{}, fmt.Errorf("template element kind %q not supported", el.Kind())
}
