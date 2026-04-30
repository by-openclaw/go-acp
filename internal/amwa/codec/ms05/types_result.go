package ms05

import "encoding/json"

// NcMethodResult is the base shape of every method invocation
// result. Every concrete variant carries `status`; non-error variants
// add a typed `value` alongside.
//
// Spec: datatypes/NcMethodResult.json.
type NcMethodResult struct {
	Status NcMethodStatus `json:"status"`
}

// NcMethodResultError is the failure variant — adds a human-readable
// error message.
type NcMethodResultError struct {
	Status       NcMethodStatus `json:"status"`
	ErrorMessage string         `json:"errorMessage"`
}

// NcMethodResultPropertyValue carries a typed property read result.
// Value is left as raw JSON so callers can unmarshal per the
// property's declared NcDatatypeDescriptor.
type NcMethodResultPropertyValue struct {
	Status NcMethodStatus  `json:"status"`
	Value  json.RawMessage `json:"value"`
}

// NcMethodResultId carries a sequence-index return (e.g. from
// AddSequenceItem).
type NcMethodResultId struct {
	Status NcMethodStatus `json:"status"`
	Value  NcId           `json:"value"`
}

// NcMethodResultLength carries a sequence-length return.
type NcMethodResultLength struct {
	Status NcMethodStatus `json:"status"`
	Value  uint32         `json:"value"`
}

// NcMethodResultClassDescriptor wraps a NcClassDescriptor return —
// used by ClassManager.GetClassDescriptor.
type NcMethodResultClassDescriptor struct {
	Status NcMethodStatus      `json:"status"`
	Value  NcClassDescriptor   `json:"value"`
}

// NcMethodResultDatatypeDescriptor wraps a NcDatatypeDescriptor
// return — used by ClassManager.GetDatatype.
type NcMethodResultDatatypeDescriptor struct {
	Status NcMethodStatus       `json:"status"`
	Value  NcDatatypeDescriptor `json:"value"`
}

// NcMethodResultBlockMemberDescriptors wraps the GetMembers /
// FindMembersByPath return.
type NcMethodResultBlockMemberDescriptors struct {
	Status NcMethodStatus            `json:"status"`
	Value  []NcBlockMemberDescriptor `json:"value"`
}
