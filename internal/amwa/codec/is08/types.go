package is08

import (
	"regexp"
)

// ActivationMode is the discriminator on the activation sub-object —
// one of three explicit values (no empty-string equivalent like
// IS-05; IS-08 always names the mode).
//
// Spec: APIs/schemas/activation-schema.json `mode` enum.
type ActivationMode string

// Recognised activation modes per IS-08 v1.0.1.
const (
	ActivationModeImmediate         ActivationMode = "activate_immediate"
	ActivationModeScheduledRelative ActivationMode = "activate_scheduled_relative"
	ActivationModeScheduledAbsolute ActivationMode = "activate_scheduled_absolute"
)

// IsValidActivationMode is true when m is one of the three spec
// values. Empty is rejected — IS-08 requests always name a mode.
func IsValidActivationMode(m ActivationMode) bool {
	switch m {
	case ActivationModeImmediate,
		ActivationModeScheduledRelative,
		ActivationModeScheduledAbsolute:
		return true
	}
	return false
}

// Activation is the request-side activation sub-object — `mode`
// required + `requested_time` optional (anyOf string/null).
// Spec: APIs/schemas/activation-schema.json.
type Activation struct {
	Mode          ActivationMode `json:"mode"`
	RequestedTime *string        `json:"requested_time,omitempty"`
}

// ActivationResponse is the response-side activation block — every
// field required and nullable. `mode` is null when no activation
// is scheduled; `activation_time` is set by the server.
// Spec: APIs/schemas/activation-response-schema.json.
type ActivationResponse struct {
	Mode           *ActivationMode `json:"mode"`
	RequestedTime  *string         `json:"requested_time"`
	ActivationTime *string         `json:"activation_time"`
}

// MapEntry is the per-channel route — either both fields populated
// (routed) or both null (unrouted). Spec:
// APIs/schemas/map-entries-schema.json.
type MapEntry struct {
	Input        *string `json:"input"`
	ChannelIndex *int    `json:"channel_index"`
}

// MapEntries is the per-output channel-routing dictionary:
//
//	{ outputID: { "0": MapEntry, "1": MapEntry, ... } }
//
// Channel index keys are stringified non-negative integers.
type MapEntries = map[string]map[string]MapEntry

// MapActive is the body of GET /map/active{,/{outputID}}. The
// activation block is response-shaped (server-populated).
// Spec: APIs/schemas/map-active-response-schema.json.
type MapActive struct {
	Activation ActivationResponse `json:"activation"`
	Map        MapEntries         `json:"map"`
}

// (A MapStaged alias lived here, documented against a GET /map/staged
// endpoint IS-08 v1.0 does not define — staging is an IS-05 concept;
// IS-08 changes are POSTed to /map/activations. Removed with zero
// users, issue #857.)

// MapActivationRequest is the body of POST /map/activations.
// Spec: APIs/schemas/map-activations-post-request-schema.json.
type MapActivationRequest struct {
	Activation Activation `json:"activation"`
	Action     MapEntries `json:"action"`
}

// MapActivationResponse is the per-activation entry on
// GET /map/activations{,/{id}} and POST response.
// Spec: APIs/schemas/map-activations-activation-get-response-schema.json.
type MapActivationResponse struct {
	ID         string             `json:"id,omitempty"`
	Activation ActivationResponse `json:"activation"`
	Action     MapEntries         `json:"action"`
}

// InputProperties is the body of GET /inputs/{id}/properties.
type InputProperties struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// OutputProperties mirrors InputProperties for outputs.
type OutputProperties = InputProperties

// InputCaps is the body of GET /inputs/{id}/caps. Reordering ==
// whether the input can be re-ordered freely; BlockSize == hardware
// granularity.
type InputCaps struct {
	Reordering bool `json:"reordering"`
	BlockSize  int  `json:"block_size"`
}

// OutputCaps is the body of GET /outputs/{id}/caps. RoutableInputs
// is nil = unrestricted; non-nil entries with a null element ==
// "unrouted is allowed".
type OutputCaps struct {
	RoutableInputs []*string `json:"routable_inputs"`
}

// InputParent is the body of GET /inputs/{id}/parent. Both fields
// nullable; Type ∈ {"source", "receiver", null}.
type InputParent struct {
	ID   *string `json:"id"`
	Type *string `json:"type"`
}

// Channel is one element of GET /inputs/{id}/channels and
// /outputs/{id}/channels.
type Channel struct {
	Label string `json:"label"`
}

// Input is the inner record of IO.Inputs[id]. Every property is
// optional on the `/map/io` aggregate view; the dedicated
// per-property endpoints are authoritative.
type Input struct {
	Properties *InputProperties `json:"properties,omitempty"`
	Parent     *InputParent     `json:"parent,omitempty"`
	Channels   []Channel        `json:"channels,omitempty"`
	Caps       *InputCaps       `json:"caps,omitempty"`
}

// Output is the inner record of IO.Outputs[id].
type Output struct {
	Properties *OutputProperties `json:"properties,omitempty"`
	SourceID   *string           `json:"source_id,omitempty"`
	Channels   []Channel         `json:"channels,omitempty"`
	Caps       *OutputCaps       `json:"caps,omitempty"`
}

// IO is the body of GET /map/io — the full inputs + outputs view.
type IO struct {
	Inputs  map[string]Input  `json:"inputs"`
	Outputs map[string]Output `json:"outputs"`
}

// ErrorBody is the standard 4xx/5xx response shape.
type ErrorBody struct {
	Code  int    `json:"code"`
	Error string `json:"error"`
	Debug string `json:"debug,omitempty"`
}

// idPattern matches the IS-08 input/output identifier grammar
// (alphanumeric + hyphen + underscore).
var idPattern = regexp.MustCompile(`^[a-zA-Z0-9\-_]+$`)

// chanPattern matches the channel-index keys in MapEntries — `0` or
// `1`-prefixed digits.
var chanPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

// taiPattern matches the standard NMOS `<sec>:<nsec>` TAI form.
var taiPattern = regexp.MustCompile(`^[0-9]+:[0-9]+$`)

// uuidPattern matches the IS-04 UUID grammar reused by IS-08
// source/parent identifiers.
var uuidPattern = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)
