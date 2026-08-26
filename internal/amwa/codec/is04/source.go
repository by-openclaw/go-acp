package is04

import (
	"fmt"

	"dhs/internal/amwa/codec/spec"
)

// Source is the IS-04 v1.3 Source resource. Combines source_core
// (caps + device_id + parents + clock_name + grain_rate) with the
// format discriminator (`format` URN) and format-specific extras.
//
// JSON Schemas:
//   - https://specs.amwa.tv/is-04/releases/v1.3.3/APIs/schemas/with-refs/source_core.html
//   - source_audio.json / source_video.json / source_data.json / source_generic.json
//
// Format-specific fields land here as optional pointers; Validate
// enforces the per-format required-set when Format is set.
type Source struct {
	ResourceCore

	Caps      map[string]any `json:"caps"`
	DeviceID  string         `json:"device_id"`
	Parents   []string       `json:"parents"`
	ClockName *string        `json:"clock_name"` // string OR null
	GrainRate *GrainRate     `json:"grain_rate,omitempty"`
	Format    string         `json:"format"` // URN

	// Audio-specific (source_audio): channels[].symbol + label.
	Channels []SourceAudioChannel `json:"channels,omitempty"`

	// Event-tally specific (source_data + IS-07): event_type URN.
	EventType string `json:"event_type,omitempty"`
}

// GrainRate is the numerator/denominator pair shared by Source + Flow.
type GrainRate struct {
	Numerator   int `json:"numerator"`
	Denominator int `json:"denominator,omitempty"` // default 1
}

// SourceAudioChannel mirrors source_audio.json channels[].
type SourceAudioChannel struct {
	Label  string `json:"label"`
	Symbol string `json:"symbol,omitempty"`
}

// Validate enforces source_core + per-format rules.
func (s *Source) Validate() error {
	errs := validateCore(&s.ResourceCore, "source")

	if s.Caps == nil {
		errs = append(errs, "source.caps: required (may be empty object)")
	}
	if s.DeviceID == "" || !IsValidUUID(s.DeviceID) {
		errs = append(errs, fmt.Sprintf("source.device_id %q: must match UUID v1-5", s.DeviceID))
	}
	if s.Parents == nil {
		errs = append(errs, "source.parents: required (may be empty array)")
	} else {
		for i, id := range s.Parents {
			if !IsValidUUID(id) {
				errs = append(errs, fmt.Sprintf("source.parents[%d] %q: not a UUID", i, id))
			}
		}
	}
	// clock_name is `["string", "null"]` and required by the schema —
	// the field must appear in the JSON. Go cannot distinguish an
	// absent field from a present-but-null one without a custom
	// UnmarshalJSON, so we accept both shapes on decode and rely on
	// the encoder (which always emits the field, including as null
	// when nil) to keep the wire spec-compliant.
	if s.GrainRate != nil && s.GrainRate.Numerator <= 0 {
		errs = append(errs, fmt.Sprintf("source.grain_rate.numerator=%d: must be positive", s.GrainRate.Numerator))
	}
	if s.GrainRate != nil && s.GrainRate.Denominator < 0 {
		errs = append(errs, fmt.Sprintf("source.grain_rate.denominator=%d: must be non-negative", s.GrainRate.Denominator))
	}

	if s.Format != "" && !IsValidFormatURN(s.Format) {
		errs = append(errs, fmt.Sprintf("source.format %q: must be a known NMOS format URN or non-NMOS URI", s.Format))
	}
	if s.Format == FormatAudio {
		// canonical Validate is lenient when `channels` is absent —
		// v1.0 audio Source has no `channels` field at all (added in
		// v1.1). Per-element validation runs only when channels are
		// present. Strict per-version presence (require channels at
		// v1.1+) lives in
		// `internal/amwa/registry/store.go validateRegistrationPresenceVersioned`.
		for i, ch := range s.Channels {
			if ch.Label == "" {
				errs = append(errs, fmt.Sprintf("source.channels[%d].label: required", i))
			}
		}
	}

	return joinErrs("is04 source validation failed", errs)
}

// DecodeSource parses + validates a Source payload.
func DecodeSource(raw []byte) (*Source, error) {
	return DecodeSourceReporting(raw, APIVersion, nil)
}

// DecodeSourceReporting parses a Source payload and validates it against the
// canonical rules, which track the latest IS-04 minor.
//
// A per-minor codec wants [ParseSource] instead: a v1.0 payload judged by
// v1.3 rules is failed for missing fields that minor never had.
func DecodeSourceReporting(raw []byte, apiVer string, rep spec.Reporter) (*Source, error) {
	s, err := ParseSource(raw, apiVer, rep)
	if err != nil {
		return nil, err
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return s, nil
}

// ParseSource decodes a Source served on an apiVer tree WITHOUT applying any
// minor's validation rules. Two classes of deviation are absorbed and
// reported rather than raised: a field IS-04 defines nowhere (see
// absorb.go) and a field it did not define until after apiVer (see
// [Since]). The caller then validates against the minor it asked for.
func ParseSource(raw []byte, apiVer string, rep spec.Reporter) (*Source, error) {
	var s Source
	if err := decodeAbsorbing(raw, &s, "source", apiVer, rep); err != nil {
		return nil, err
	}
	AbsorbLaterThan(raw, "source", apiVer, rep)
	return &s, nil
}
