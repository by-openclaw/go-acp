package is04

import (
	"fmt"

	"dhs/internal/amwa/codec/spec"
)

// Device is the IS-04 v1.3 Device resource.
//
// JSON Schema: https://specs.amwa.tv/is-04/releases/v1.3.3/APIs/schemas/with-refs/device.html
type Device struct {
	ResourceCore

	Type      string          `json:"type"`    // URN
	NodeID    string          `json:"node_id"` // UUID v1-5
	Senders   []string        `json:"senders"` // deprecated array of UUIDs
	Receivers []string        `json:"receivers"`
	Controls  []DeviceControl `json:"controls"`
}

// DeviceControl is one entry in Device.Controls — points at IS-05 /
// IS-07 / IS-08 / IS-12 sub-APIs.
type DeviceControl struct {
	Href          string `json:"href"`
	Type          string `json:"type"` // URN
	Authorization bool   `json:"authorization,omitempty"`
}

// Validate enforces device.json rules.
func (d *Device) Validate() error {
	errs := validateCore(&d.ResourceCore, "device")

	if d.Type == "" || !IsValidDeviceTypeURN(d.Type) {
		errs = append(errs, fmt.Sprintf("device.type %q: must be a non-NMOS URI or `urn:x-nmos:device:...`", d.Type))
	}
	if d.NodeID == "" || !IsValidUUID(d.NodeID) {
		errs = append(errs, fmt.Sprintf("device.node_id %q: must match RFC 4122 v1-v5 UUID", d.NodeID))
	}
	for i, id := range d.Senders {
		if !IsValidUUID(id) {
			errs = append(errs, fmt.Sprintf("device.senders[%d] %q: not a UUID", i, id))
		}
	}
	for i, id := range d.Receivers {
		if !IsValidUUID(id) {
			errs = append(errs, fmt.Sprintf("device.receivers[%d] %q: not a UUID", i, id))
		}
	}
	// `controls` landed in IS-04 v1.1 — when absent (a v1.0 wire body)
	// we accept the field's omission silently.
	for i, c := range d.Controls {
		if c.Href == "" {
			errs = append(errs, fmt.Sprintf("device.controls[%d].href: required", i))
		}
		if c.Type == "" {
			errs = append(errs, fmt.Sprintf("device.controls[%d].type: required (URN)", i))
		}
	}

	return joinErrs("is04 device validation failed", errs)
}

// DecodeDevice parses + validates a Device payload.
func DecodeDevice(raw []byte) (*Device, error) {
	return DecodeDeviceReporting(raw, APIVersion, nil)
}

// DecodeDeviceReporting parses a Device payload and validates it against the
// canonical rules, which track the latest IS-04 minor.
//
// A per-minor codec wants [ParseDevice] instead: a v1.0 payload judged by
// v1.3 rules is failed for missing fields that minor never had.
func DecodeDeviceReporting(raw []byte, apiVer string, rep spec.Reporter) (*Device, error) {
	dev, err := ParseDevice(raw, apiVer, rep)
	if err != nil {
		return nil, err
	}
	if err := dev.Validate(); err != nil {
		return nil, err
	}
	return dev, nil
}

// ParseDevice decodes a Device served on an apiVer tree WITHOUT applying any
// minor's validation rules. Two classes of deviation are absorbed and
// reported rather than raised: a field IS-04 defines nowhere (see
// absorb.go) and a field it did not define until after apiVer (see
// [Since]). The caller then validates against the minor it asked for.
func ParseDevice(raw []byte, apiVer string, rep spec.Reporter) (*Device, error) {
	var dev Device
	if err := decodeAbsorbing(raw, &dev, "device", apiVer, rep); err != nil {
		return nil, err
	}
	return &dev, nil
}
