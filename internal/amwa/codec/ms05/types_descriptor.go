package ms05

import "encoding/json"

// NcDescriptor is the abstract base shape of every descriptor — a
// single optional `description` field is the only common shape per
// MS-05-02 §11. Concrete descriptors embed this and add their own
// fields.
//
// Spec: datatypes/NcDescriptor.json.
type NcDescriptor struct {
	Description *string `json:"description"`
}

// NcEnumItemDescriptor describes one entry of an enum datatype.
// Spec: datatypes/NcEnumItemDescriptor.json.
type NcEnumItemDescriptor struct {
	NcDescriptor
	Name  string `json:"name"`
	Value int    `json:"value"`
}

// NcFieldDescriptor describes one field of a struct datatype.
type NcFieldDescriptor struct {
	NcDescriptor
	Name       string  `json:"name"`
	TypeName   *string `json:"typeName"`
	IsNullable bool    `json:"isNullable"`
	IsSequence bool    `json:"isSequence"`
	// Constraints is the schema's polymorphic union — nil, or one of
	// the NcParameterConstraints variants (Number / String / base).
	Constraints any `json:"constraints"`
}

// NcParameterDescriptor describes one parameter of a method.
type NcParameterDescriptor struct {
	NcDescriptor
	Name        string  `json:"name"`
	TypeName    *string `json:"typeName"`
	IsNullable  bool    `json:"isNullable"`
	IsSequence  bool    `json:"isSequence"`
	Constraints any     `json:"constraints"`
}

// NcPropertyDescriptor describes one class property.
// Spec: datatypes/NcPropertyDescriptor.json (referenced by
// classes/*.json `properties[]`).
type NcPropertyDescriptor struct {
	NcDescriptor
	ID           NcPropertyId `json:"id"`
	Name         string       `json:"name"`
	TypeName     *string      `json:"typeName"`
	IsReadOnly   bool         `json:"isReadOnly"`
	IsNullable   bool         `json:"isNullable"`
	IsSequence   bool         `json:"isSequence"`
	IsDeprecated bool         `json:"isDeprecated"`
	Constraints  any          `json:"constraints"`
}

// NcMethodDescriptor describes one class method.
type NcMethodDescriptor struct {
	NcDescriptor
	ID             NcMethodId              `json:"id"`
	Name           string                  `json:"name"`
	ResultDatatype string                  `json:"resultDatatype"`
	Parameters     []NcParameterDescriptor `json:"parameters"`
	IsDeprecated   bool                    `json:"isDeprecated"`
}

// NcEventDescriptor describes one class event.
type NcEventDescriptor struct {
	NcDescriptor
	ID            NcEventId `json:"id"`
	Name          string    `json:"name"`
	EventDatatype string    `json:"eventDatatype"`
	IsDeprecated  bool      `json:"isDeprecated"`
}

// NcClassDescriptor is the meta-model description of a single class
// returned by ClassManager.GetClassDescriptor.
//
// Spec: datatypes/NcClassDescriptor.json + classes/*.json.
type NcClassDescriptor struct {
	NcDescriptor
	ClassID    NcClassId              `json:"classId"`
	Name       string                 `json:"name"`
	FixedRole  *string                `json:"fixedRole"`
	Properties []NcPropertyDescriptor `json:"properties"`
	Methods    []NcMethodDescriptor   `json:"methods"`
	Events     []NcEventDescriptor    `json:"events"`
}

// NcDatatypeDescriptor is the meta-model description of a datatype.
// The `type` field discriminates the four variants — which is which
// reading the codec relies on. Optional `fields` (Struct), `items`
// (Enum), and `parentType` (Typedef) populate per the variant.
//
// Spec: datatypes/NcDatatypeDescriptor*.json (oneOf union of
// Primitive / Typedef / Struct / Enum).
type NcDatatypeDescriptor struct {
	NcDescriptor
	Name string         `json:"name"`
	Type NcDatatypeType `json:"type"`

	// Struct-only (type = 2)
	Fields []NcFieldDescriptor `json:"fields,omitempty"`

	// Enum-only (type = 3)
	Items []NcEnumItemDescriptor `json:"items,omitempty"`

	// Typedef-only (type = 1) + Struct-only (when Struct subclasses
	// another Struct).
	ParentType *string `json:"parentType,omitempty"`

	// Typedef-only — true when the alias represents a sequence of
	// the parent type.
	IsSequence bool `json:"isSequence,omitempty"`

	Constraints any `json:"constraints,omitempty"`
}

// MarshalJSON emits exactly the wire shape the variant's schema
// fixes. Two rules the plain struct tags cannot express (and the
// AMWA IS-14-01 suite checks — 39 of its tests failed on the first
// omission):
//
//   - the base keys `constraints` (and `parentType` on Struct) are
//     REQUIRED even when null, so omitempty must not drop them;
//   - each variant carries ONLY its own extra keys — a `fields` key
//     on a Primitive would satisfy the wrong branch of the schema's
//     oneOf. Typedef always writes `isSequence`, including false.
func (d NcDatatypeDescriptor) MarshalJSON() ([]byte, error) {
	m := map[string]any{
		"description": d.Description,
		"name":        d.Name,
		"type":        d.Type,
		"constraints": d.Constraints,
	}
	switch d.Type {
	case NcDatatypeTypeTypedef:
		m["parentType"] = d.ParentType
		m["isSequence"] = d.IsSequence
	case NcDatatypeTypeStruct:
		fields := d.Fields
		if fields == nil {
			fields = []NcFieldDescriptor{}
		}
		m["fields"] = fields
		m["parentType"] = d.ParentType
	case NcDatatypeTypeEnum:
		items := d.Items
		if items == nil {
			items = []NcEnumItemDescriptor{}
		}
		m["items"] = items
	}
	return json.Marshal(m)
}

// NcBlockMemberDescriptor is the per-member entry returned by
// NcBlock.GetMembers.
//
// Spec: datatypes/NcBlockMemberDescriptor.json.
type NcBlockMemberDescriptor struct {
	NcDescriptor
	Role        string    `json:"role"`
	Oid         NcOid     `json:"oid"`
	ConstantOid bool      `json:"constantOid"`
	ClassID     NcClassId `json:"classId"`
	UserLabel   *string   `json:"userLabel"`
	Owner       NcOid     `json:"owner"`
}
