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
// resource using a canonical (latest-minor) shape. Used by tests and
// where per-version downcast is irrelevant. Production registration
// MUST go through EncodeRegistrationVersioned with the wire api_ver's
// codec — otherwise the registry stores a v1.3-shaped resource while
// the Node API serves a v1.0-shaped response under /x-nmos/node/v1.0/,
// and AMWA test_04 fails Node-API-vs-registry coherence on every
// minor < v1.3.
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

// EncodeRegistrationVersioned builds the registration body using the
// per-api_ver codec for the resource. The Data field of the envelope
// is the codec's wire shape — fields the older minor doesn't define
// are stripped (Node.interfaces[].attached_network_device for v1.2,
// Receiver.caps.constraint_sets for v1.2, the entire `interfaces` /
// `clocks` / `api` arrays for v1.0, etc.).
//
// This is what production registration MUST use. Without it, the
// registry stores the canonical (v1.3) shape but the Node API serves
// the per-version shape — AMWA test_04 / test_07-11 fail Node-API
// vs registry coherence on every minor < v1.3.
func EncodeRegistrationVersioned(c Codec, t ResourceType, data any) ([]byte, error) {
	if c == nil {
		return EncodeRegistration(t, data)
	}
	if !IsValidResourceType(string(t)) {
		return nil, fmt.Errorf("is04: invalid resource type %q", t)
	}
	var raw []byte
	var err error
	switch t {
	case ResourceNode:
		v, ok := data.(*Node)
		if !ok {
			return nil, fmt.Errorf("is04: EncodeRegistrationVersioned %s: data is %T, want *Node", t, data)
		}
		raw, err = c.EncodeNode(*v)
	case ResourceDevice:
		v, ok := data.(*Device)
		if !ok {
			return nil, fmt.Errorf("is04: EncodeRegistrationVersioned %s: data is %T, want *Device", t, data)
		}
		raw, err = c.EncodeDevice(*v)
	case ResourceSource:
		v, ok := data.(*Source)
		if !ok {
			return nil, fmt.Errorf("is04: EncodeRegistrationVersioned %s: data is %T, want *Source", t, data)
		}
		raw, err = c.EncodeSource(*v)
	case ResourceFlow:
		v, ok := data.(*Flow)
		if !ok {
			return nil, fmt.Errorf("is04: EncodeRegistrationVersioned %s: data is %T, want *Flow", t, data)
		}
		raw, err = c.EncodeFlow(*v)
	case ResourceSender:
		v, ok := data.(*Sender)
		if !ok {
			return nil, fmt.Errorf("is04: EncodeRegistrationVersioned %s: data is %T, want *Sender", t, data)
		}
		raw, err = c.EncodeSender(*v)
	case ResourceReceiver:
		v, ok := data.(*Receiver)
		if !ok {
			return nil, fmt.Errorf("is04: EncodeRegistrationVersioned %s: data is %T, want *Receiver", t, data)
		}
		raw, err = c.EncodeReceiver(*v)
	default:
		return nil, fmt.Errorf("is04: invalid resource type %q", t)
	}
	if err != nil {
		return nil, fmt.Errorf("is04: codec encode %s: %w", t, err)
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
