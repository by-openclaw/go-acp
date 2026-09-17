package is14

import (
	"bytes"
	"encoding/json"
	"fmt"

	"dhs/internal/amwa/codec/ms05"
)

// RestoreMode is the numeric NcRestoreMode enum from the
// device-configuration feature set. The spec's own examples pin the
// values: bulkProperties-put-request.json carries 0 for a Modify
// restore and bulkProperties-put-rebuild-request.json carries 1.
type RestoreMode int

const (
	RestoreModeModify  RestoreMode = 0
	RestoreModeRebuild RestoreMode = 1
)

// ValidateRestoreMode rejects values outside the published enum.
func ValidateRestoreMode(m RestoreMode) error {
	if m != RestoreModeModify && m != RestoreModeRebuild {
		return fmt.Errorf("is14: restoreMode %d: not a member of NcRestoreMode (0 Modify, 1 Rebuild)", m)
	}
	return nil
}

// Notice types for NcPropertyRestoreNotice.noticeType. The spec
// examples use 300 for every "Property is readonly" warning; 400 is
// the error variant per the feature set.
const (
	NoticeWarning = 300
	NoticeError   = 400
)

// NcRestoreValidationStatus values — the ONLY statuses a per-object
// restore verdict may carry (the feature set's enum is {200, 400,
// 404, 500}; general NcMethodStatus codes like 417 are rejected by
// the response schema).
const (
	RestoreValidationOk          ms05.NcMethodStatus = 200
	RestoreValidationFailed      ms05.NcMethodStatus = 400
	RestoreValidationNotFound    ms05.NcMethodStatus = 404
	RestoreValidationDeviceError ms05.NcMethodStatus = 500
)

// PropertyHolder is one property inside an object's backup entry
// (feature set NcPropertyHolder). Descriptor is nullable — the
// includeDescriptors=false form of a backup sets it to null.
type PropertyHolder struct {
	ID         ms05.NcPropertyId          `json:"id"`
	Descriptor *ms05.NcPropertyDescriptor `json:"descriptor"`
	Value      any                        `json:"value"`
}

// ObjectPropertiesHolder is one role path's entry in a backup data
// set (feature set NcObjectPropertiesHolder). Path is the role path
// as an array of roles; DependencyPaths and AllowedMembersClasses are
// required members that may be empty.
type ObjectPropertiesHolder struct {
	Path                  []string         `json:"path"`
	DependencyPaths       [][]string       `json:"dependencyPaths"`
	AllowedMembersClasses []ms05.NcClassId `json:"allowedMembersClasses"`
	Values                []PropertyHolder `json:"values"`
	IsRebuildable         bool             `json:"isRebuildable"`
}

// BulkPropertiesHolder is a whole backup data set (feature set
// NcBulkPropertiesHolder). ValidationFingerprint is nullable and
// vendor-opaque; a device's own backups always carry one.
type BulkPropertiesHolder struct {
	ValidationFingerprint *string                  `json:"validationFingerprint"`
	Values                []ObjectPropertiesHolder `json:"values"`
}

// PropertyRestoreNotice is one per-property note in a restore
// validation (feature set NcPropertyRestoreNotice).
type PropertyRestoreNotice struct {
	ID            ms05.NcPropertyId `json:"id"`
	Name          string            `json:"name"`
	NoticeType    int               `json:"noticeType"`
	NoticeMessage string            `json:"noticeMessage"`
}

// ObjectPropertiesSetValidation is one role path's verdict from a
// restore or restore-validation (feature set
// NcObjectPropertiesSetValidation). Status mirrors NcMethodStatus
// numerics (200 Ok / 400 Failed / 404 NotFound / 500 DeviceError).
type ObjectPropertiesSetValidation struct {
	Path          []string                `json:"path"`
	Status        ms05.NcMethodStatus     `json:"status"`
	Notices       []PropertyRestoreNotice `json:"notices"`
	StatusMessage *string                 `json:"statusMessage"`
}

// ResultBulkPropertiesHolder wraps a backup response
// (NcMethodResultBulkPropertiesHolder).
type ResultBulkPropertiesHolder struct {
	Status ms05.NcMethodStatus  `json:"status"`
	Value  BulkPropertiesHolder `json:"value"`
}

// ResultObjectPropertiesSetValidation wraps a restore / validate
// response (NcMethodResultObjectPropertiesSetValidation).
type ResultObjectPropertiesSetValidation struct {
	Status ms05.NcMethodStatus             `json:"status"`
	Value  []ObjectPropertiesSetValidation `json:"value"`
}

// ---- request bodies (the three the RAML defines) ----

// PropertyValuePutRequest is the body of
// PUT /rolePaths/{rolePath}/properties/{propertyId}/value
// (property-value-put-request.json: `value` required, any type —
// including null, which is why the raw bytes are kept).
type PropertyValuePutRequest struct {
	Value json.RawMessage `json:"value"`
}

// MethodPatchRequest is the body of
// PATCH /rolePaths/{rolePath}/methods/{methodId}
// (method-patch-request.json: `arguments` required, an object; empty
// object for parameterless methods).
type MethodPatchRequest struct {
	Arguments json.RawMessage `json:"arguments"`
}

// BulkPropertiesSetArgs is the `arguments` object shared by the
// bulkProperties PUT (SetPropertiesByPath) and PATCH
// (ValidateSetPropertiesByPath) bodies. Pointers keep "absent"
// distinct from zero values: the docs say clients MUST include all
// three members.
type BulkPropertiesSetArgs struct {
	DataSet     *BulkPropertiesHolder `json:"dataSet"`
	Recurse     *bool                 `json:"recurse"`
	RestoreMode *RestoreMode          `json:"restoreMode"`
}

// BulkPropertiesSetRequest is the full PUT / PATCH body
// (bulkProperties-put-request.json / bulkProperties-patch-request.json).
type BulkPropertiesSetRequest struct {
	Arguments *BulkPropertiesSetArgs `json:"arguments"`
}

// ---- Encode / Decode (canonical, strict) ----

func encodeJSON(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }

func decodeStrict(raw []byte, dst any, what string) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return fmt.Errorf("is14: decode %s: %w", what, err)
	}
	if d.More() {
		return fmt.Errorf("is14: decode %s: trailing JSON", what)
	}
	return nil
}

// EncodeBulkPropertiesHolder marshals a backup data set.
func EncodeBulkPropertiesHolder(h BulkPropertiesHolder) ([]byte, error) {
	return encodeJSON(h)
}

// DecodeBulkPropertiesHolder parses a backup data set. Values must
// exist — a holder with no `values` member is not one.
func DecodeBulkPropertiesHolder(raw []byte) (BulkPropertiesHolder, error) {
	var h BulkPropertiesHolder
	if err := decodeStrict(raw, &h, "bulk properties holder"); err != nil {
		return BulkPropertiesHolder{}, err
	}
	if h.Values == nil {
		return BulkPropertiesHolder{}, fmt.Errorf("is14: bulk properties holder: values is required")
	}
	return h, nil
}

// DecodeBulkPropertiesSetRequest parses + validates a bulkProperties
// PUT / PATCH body: `arguments` with dataSet, recurse and restoreMode
// all present (API requests doc: clients MUST include all three), and
// the mode inside the published enum.
//
// Unknown members are TOLERATED: the request schemas never set
// additionalProperties=false, and the AMWA suite exercises that
// freedom (its validate bodies carry an extra top-level member —
// rejecting it failed 7 of the bulkProperties rounds).
func DecodeBulkPropertiesSetRequest(raw []byte) (BulkPropertiesSetRequest, error) {
	var r BulkPropertiesSetRequest
	if err := json.Unmarshal(raw, &r); err != nil {
		return BulkPropertiesSetRequest{}, fmt.Errorf("is14: decode bulkProperties set request: %w", err)
	}
	a := r.Arguments
	if a == nil {
		return BulkPropertiesSetRequest{}, fmt.Errorf("is14: bulkProperties set request: arguments is required")
	}
	if a.DataSet == nil {
		return BulkPropertiesSetRequest{}, fmt.Errorf("is14: bulkProperties set request: arguments.dataSet is required")
	}
	if a.DataSet.Values == nil {
		return BulkPropertiesSetRequest{}, fmt.Errorf("is14: bulkProperties set request: dataSet.values is required")
	}
	if a.Recurse == nil {
		return BulkPropertiesSetRequest{}, fmt.Errorf("is14: bulkProperties set request: arguments.recurse is required")
	}
	if a.RestoreMode == nil {
		return BulkPropertiesSetRequest{}, fmt.Errorf("is14: bulkProperties set request: arguments.restoreMode is required")
	}
	if err := ValidateRestoreMode(*a.RestoreMode); err != nil {
		return BulkPropertiesSetRequest{}, err
	}
	return r, nil
}

// DecodePropertyValuePutRequest parses a property PUT body. The
// `value` member must EXIST; its content may be any JSON including
// null, so existence is checked on the raw key set, not the decoded
// pointer. Unknown members pass — the schema does not forbid them.
func DecodePropertyValuePutRequest(raw []byte) (PropertyValuePutRequest, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return PropertyValuePutRequest{}, fmt.Errorf("is14: decode property value put request: %w", err)
	}
	v, ok := probe["value"]
	if !ok {
		return PropertyValuePutRequest{}, fmt.Errorf("is14: property value put request: value is required")
	}
	return PropertyValuePutRequest{Value: v}, nil
}

// DecodeMethodPatchRequest parses a method invocation body. The
// `arguments` member must exist and be a JSON object (empty for
// parameterless methods). Unknown members pass — the schema does not
// forbid them.
func DecodeMethodPatchRequest(raw []byte) (MethodPatchRequest, error) {
	var r MethodPatchRequest
	if err := json.Unmarshal(raw, &r); err != nil {
		return MethodPatchRequest{}, fmt.Errorf("is14: decode method patch request: %w", err)
	}
	if r.Arguments == nil {
		return MethodPatchRequest{}, fmt.Errorf("is14: method patch request: arguments is required")
	}
	trimmed := bytes.TrimSpace(r.Arguments)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return MethodPatchRequest{}, fmt.Errorf("is14: method patch request: arguments must be a JSON object")
	}
	return r, nil
}
