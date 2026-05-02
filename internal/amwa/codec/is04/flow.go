package is04

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Flow is the IS-04 v1.3 Flow resource. Combines flow_core
// (source_id + device_id + parents + grain_rate) with the format
// discriminator (`format` URN) and a free-form `media_type`.
//
// JSON Schema base:
//   https://specs.amwa.tv/is-04/releases/v1.3.3/APIs/schemas/with-refs/flow_core.html
//
// Per-format constraints (raw vs coded video, audio_raw, sdianc_data,
// json_data, mux) layer on top of this base; we hold the union of
// fields and validate the must-be-present-for-format set in Validate.
type Flow struct {
	ResourceCore

	SourceID  string     `json:"source_id"`
	DeviceID  string     `json:"device_id"`
	Parents   []string   `json:"parents"`
	GrainRate *GrainRate `json:"grain_rate,omitempty"`
	Format    string     `json:"format"`     // URN
	MediaType string     `json:"media_type,omitempty"`

	// Video-specific (flow_video / flow_video_raw / flow_video_coded):
	FrameWidth   int                  `json:"frame_width,omitempty"`
	FrameHeight  int                  `json:"frame_height,omitempty"`
	Interlace    string               `json:"interlace_mode,omitempty"`
	ColorSpace   string               `json:"colorspace,omitempty"`
	TransferChar string               `json:"transfer_characteristic,omitempty"`
	Components   []FlowVideoComponent `json:"components,omitempty"`

	// Audio-specific (flow_audio / flow_audio_raw / flow_audio_coded):
	SampleRate *GrainRate `json:"sample_rate,omitempty"`
	BitDepth   int        `json:"bit_depth,omitempty"`

	// Mux-specific (flow_mux):
	// (no extra required fields beyond flow_core in v1.3 — payload
	// described elsewhere via the corresponding Sender's manifest)

	// SDI-ANC specific (flow_sdianc_data):
	DIDSDID []FlowDIDSDID `json:"DID_SDID,omitempty"`

	// json_data + event_type subset (flow_json_data):
	EventType string `json:"event_type,omitempty"`
}

// FlowDIDSDID mirrors flow_sdianc_data.json DID_SDID[].
type FlowDIDSDID struct {
	DID  string `json:"DID"`  // hex string e.g. "0x60"
	SDID string `json:"SDID"` // hex string
}

// FlowVideoComponent mirrors flow_video_raw.json `components[]` —
// per-color-component dimensions for a raw video flow (Y / Cb / Cr,
// or A / Y / G / B / R / etc.). The schema requires every entry to
// carry name + width + height + bit_depth.
type FlowVideoComponent struct {
	Name     string `json:"name"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	BitDepth int    `json:"bit_depth"`
}

// Validate enforces flow_core + per-format rules.
func (f *Flow) Validate() error {
	errs := validateCore(&f.ResourceCore, "flow")

	if f.SourceID == "" || !IsValidUUID(f.SourceID) {
		errs = append(errs, fmt.Sprintf("flow.source_id %q: must match UUID v1-5", f.SourceID))
	}
	// device_id was added to flow in IS-04 v1.1 — accept its absence
	// on v1.0 wire bodies, validate the pattern when present.
	if f.DeviceID != "" && !IsValidUUID(f.DeviceID) {
		errs = append(errs, fmt.Sprintf("flow.device_id %q: must match UUID v1-5", f.DeviceID))
	}
	for i, id := range f.Parents {
		if !IsValidUUID(id) {
			errs = append(errs, fmt.Sprintf("flow.parents[%d] %q: not a UUID", i, id))
		}
	}
	if f.GrainRate != nil && f.GrainRate.Numerator <= 0 {
		errs = append(errs, fmt.Sprintf("flow.grain_rate.numerator=%d: must be positive", f.GrainRate.Numerator))
	}
	if f.Format != "" && !IsValidFormatURN(f.Format) {
		errs = append(errs, fmt.Sprintf("flow.format %q: must be a known NMOS format URN or non-NMOS URI", f.Format))
	}

	// Per-format checks. Only enforce when at least one format-specific
	// field is present — IS-04 v1.0 strips ALL of these from the wire
	// (the v1.0 Flow schema is essentially flow_core), so a v1.0
	// registration body must validate without per-format guards.
	switch f.Format {
	case FormatVideo:
		hasVideoFields := f.FrameWidth > 0 || f.FrameHeight > 0 ||
			f.Interlace != "" || f.ColorSpace != "" || f.TransferChar != "" ||
			len(f.Components) > 0 || f.MediaType != ""
		if hasVideoFields {
			if f.FrameWidth <= 0 {
				errs = append(errs, "flow.frame_width: required (>0) for video flows")
			}
			if f.FrameHeight <= 0 {
				errs = append(errs, "flow.frame_height: required (>0) for video flows")
			}
			switch f.Interlace {
			case "", "progressive", "interlaced_tff", "interlaced_bff", "interlaced_psf":
			default:
				errs = append(errs, fmt.Sprintf("flow.interlace_mode %q: must be one of {progressive, interlaced_tff, interlaced_bff, interlaced_psf}", f.Interlace))
			}
		}
	case FormatAudio:
		// IS-04 §3.2.4: every audio Flow MUST carry sample_rate
		// (positive numerator). The "only validate when other audio
		// fields present" relaxation was a v1.0 wire-shape overshoot —
		// per-version codec strip handles wire emission, but the
		// canonical struct always represents a complete Flow.
		if f.SampleRate == nil || f.SampleRate.Numerator <= 0 {
			errs = append(errs, "flow.sample_rate: required for audio flows (positive numerator)")
		}
	}

	return joinErrs("is04 flow validation failed", errs)
}

// DecodeFlow parses + validates a Flow payload.
func DecodeFlow(raw []byte) (*Flow, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var f Flow
	if err := d.Decode(&f); err != nil {
		return nil, fmt.Errorf("is04: decode flow: %w", err)
	}
	if d.More() {
		return nil, fmt.Errorf("is04: decode flow: trailing JSON content")
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}
