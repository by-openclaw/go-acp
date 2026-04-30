package is04

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// RegistrationRequest is the body of POST /x-nmos/registration/v1.x/resource.
// `Type` is the singular resource type (`node`, `device`, …); `Data` is
// the resource object itself, raw-JSON until the caller decodes it
// based on Type.
type RegistrationRequest struct {
	Type ResourceType    `json:"type"`
	Data json.RawMessage `json:"data"`
}

// EncodeRegistration builds the registration body for the given
// resource. The caller is responsible for validating `data` first;
// this helper just wraps it in the spec envelope.
func EncodeRegistration(t ResourceType, data any) ([]byte, error) {
	if !IsValidResourceType(string(t)) {
		return nil, fmt.Errorf("is04: invalid resource type %q", t)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("is04: marshal %s: %w", t, err)
	}
	body, err := json.Marshal(RegistrationRequest{Type: t, Data: raw})
	if err != nil {
		return nil, fmt.Errorf("is04: marshal registration envelope: %w", err)
	}
	return body, nil
}

// DecodeRegistration parses the envelope without yet decoding Data.
func DecodeRegistration(raw []byte) (*RegistrationRequest, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var r RegistrationRequest
	if err := d.Decode(&r); err != nil {
		return nil, fmt.Errorf("is04: decode registration: %w", err)
	}
	if !IsValidResourceType(string(r.Type)) {
		return nil, fmt.Errorf("is04: registration.type %q: not a valid resource type", r.Type)
	}
	return &r, nil
}

// HealthResponse mirrors the spec body returned by POST + GET on
// `/health/nodes/{nodeId}` — `{ "health": "<unix-seconds>" }`.
type HealthResponse struct {
	Health string `json:"health"`
}
