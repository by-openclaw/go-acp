package is04

import "strings"

// APIVersion is the IS-04 wire version this codec implements.
const APIVersion = "v1.3"

// ResourceType enumerates the six IS-04 resource collections. Used in
// the Registration envelope (`{type, data}`) and as the resource-type
// path segment under `/x-nmos/registration/.../resource/{type}/{id}`.
//
// Spec-strict: the registration envelope uses the singular form
// (`node`, `device`, `source`, `flow`, `sender`, `receiver`) while
// the Node API path collections are plural (`/sources`, `/devices`,
// etc.). Helpers below convert between forms.
type ResourceType string

const (
	ResourceNode     ResourceType = "node"
	ResourceDevice   ResourceType = "device"
	ResourceSource   ResourceType = "source"
	ResourceFlow     ResourceType = "flow"
	ResourceSender   ResourceType = "sender"
	ResourceReceiver ResourceType = "receiver"
)

// AllResourceTypes lists the six resource types in registration order
// — Nodes first because every other resource refers back to them.
var AllResourceTypes = []ResourceType{
	ResourceNode, ResourceDevice, ResourceSource, ResourceFlow,
	ResourceSender, ResourceReceiver,
}

// Plural returns the IS-04 collection name (`sources`, `devices`, …).
func (r ResourceType) Plural() string {
	switch r {
	case ResourceNode:
		return "nodes"
	default:
		return string(r) + "s"
	}
}

// IsValidResourceType reports whether s is one of the six allowed
// types in the registration envelope.
func IsValidResourceType(s string) bool {
	switch ResourceType(s) {
	case ResourceNode, ResourceDevice, ResourceSource, ResourceFlow, ResourceSender, ResourceReceiver:
		return true
	}
	return false
}

// Format URNs (per IS-04 §3.6 / source_*.json). Senders/Receivers carry
// a `format` field whose value MUST start with `urn:x-nmos:format:` for
// NMOS-defined formats; vendor extensions use a different URN root.
const (
	FormatAudio = "urn:x-nmos:format:audio"
	FormatVideo = "urn:x-nmos:format:video"
	FormatData  = "urn:x-nmos:format:data"
	FormatMux   = "urn:x-nmos:format:mux"
)

// IsNMOSFormat reports whether u is one of the four NMOS-defined
// format URNs.
func IsNMOSFormat(u string) bool {
	switch u {
	case FormatAudio, FormatVideo, FormatData, FormatMux:
		return true
	}
	return false
}

// IsValidFormatURN enforces the spec rule: either a known NMOS format
// URN, or a non-NMOS URI (vendor extension). It rejects URNs that
// claim `urn:x-nmos:format:` but use an unknown sub-token.
func IsValidFormatURN(u string) bool {
	if u == "" {
		return false
	}
	if strings.HasPrefix(u, "urn:x-nmos:format:") {
		return IsNMOSFormat(u)
	}
	// Vendor extensions: any URI not under the urn:x-nmos: namespace.
	return !strings.HasPrefix(u, "urn:x-nmos:")
}

// Transport URNs (per IS-04 §3.7 / sender / receiver_core).
const (
	TransportRTP          = "urn:x-nmos:transport:rtp"
	TransportRTPMcast     = "urn:x-nmos:transport:rtp.mcast"
	TransportRTPUcast     = "urn:x-nmos:transport:rtp.ucast"
	TransportDASH         = "urn:x-nmos:transport:dash"
	TransportWebSocket    = "urn:x-nmos:transport:websocket"
	TransportMQTT         = "urn:x-nmos:transport:mqtt"
)

// IsNMOSTransport reports whether u is one of the six NMOS-defined
// transport URNs.
func IsNMOSTransport(u string) bool {
	switch u {
	case TransportRTP, TransportRTPMcast, TransportRTPUcast,
		TransportDASH, TransportWebSocket, TransportMQTT:
		return true
	}
	return false
}

// IsValidTransportURN matches the same shape rule as IsValidFormatURN.
func IsValidTransportURN(u string) bool {
	if u == "" {
		return false
	}
	if strings.HasPrefix(u, "urn:x-nmos:transport:") {
		return IsNMOSTransport(u)
	}
	return !strings.HasPrefix(u, "urn:x-nmos:")
}

// IsValidDeviceTypeURN per device.json: either starts with
// `urn:x-nmos:device:`, or is a non-`urn:x-nmos:` URI.
func IsValidDeviceTypeURN(u string) bool {
	if u == "" {
		return false
	}
	if strings.HasPrefix(u, "urn:x-nmos:device:") {
		return true
	}
	return !strings.HasPrefix(u, "urn:x-nmos:")
}

// HTTPProtocol enumerates the api_proto / endpoints[].protocol values.
type HTTPProtocol string

const (
	HTTPProto  HTTPProtocol = "http"
	HTTPSProto HTTPProtocol = "https"
)

// IsValidHTTPProtocol reports whether p is "http" or "https".
func IsValidHTTPProtocol(p string) bool {
	return p == string(HTTPProto) || p == string(HTTPSProto)
}
