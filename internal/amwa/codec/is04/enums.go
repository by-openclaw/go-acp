package is04

import (
	"strconv"
	"strings"
)

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

// Transport URNs.
//
// The authoritative list is the NMOS Parameter Registers' Transports
// register, NOT the IS-04 or IS-05 spec text: from IS-05 v1.2 onwards
// "additional transport types and associated schemas are defined in
// the Transports register" (IS-05 v1.2.0 Upgrade Path). A transport
// can therefore appear without any spec being revised, which is
// exactly how ndi and usb were missing here — nothing in a spec
// document changed to announce them.
//
// Verified against the register 2026-08-26.
const (
	TransportRTP       = "urn:x-nmos:transport:rtp"
	TransportRTPMcast  = "urn:x-nmos:transport:rtp.mcast"
	TransportRTPUcast  = "urn:x-nmos:transport:rtp.ucast"
	TransportDASH      = "urn:x-nmos:transport:dash"
	TransportWebSocket = "urn:x-nmos:transport:websocket"
	TransportMQTT      = "urn:x-nmos:transport:mqtt"
	// Registered against IS-05 v1.2 / BCP-007-01 (NDI) and
	// BCP-007-02 (USB).
	TransportNDI = "urn:x-nmos:transport:ndi"
	TransportUSB = "urn:x-nmos:transport:usb"
	// Registered against BCP-007-03 (NMOS With MXL). Requires IS-04
	// v1.3+ and IS-05 v1.2+ per the spec.
	TransportMXL = "urn:x-nmos:transport:mxl"
)

// transportMinIS05 is the earliest IS-05 minor each transport may
// appear on.
//
// The gate is required, not cosmetic: IS-05's Upgrade Path says an
// earlier API version "MUST NOT list any Senders or Receivers which
// make use of this new transport type". A v1.1 tree offering an NDI
// sender is non-conformant even though the URN is perfectly valid on
// v1.2.
var transportMinIS05 = map[string]string{
	TransportRTP:       "v1.0",
	TransportRTPMcast:  "v1.0",
	TransportRTPUcast:  "v1.0",
	TransportDASH:      "v1.0",
	TransportWebSocket: "v1.1",
	TransportMQTT:      "v1.1",
	TransportNDI:       "v1.2",
	TransportUSB:       "v1.2",
	TransportMXL:       "v1.2",
}

// transportMinIS04 is the earliest IS-04 minor each transport may
// appear on. It is NOT the same table as transportMinIS05, and the
// difference is not a rounding error.
//
// IS-05 accepted websocket and mqtt from v1.1; IS-04's sender.json did
// not list them until v1.3. Verified against the AMWA schemas at each
// tag (2026-08-26): v1.0's transport enum is exactly
// {rtp, rtp.ucast, rtp.mcast, dash}, and v1.1 and v1.2 both reject
// websocket with "not valid under any of the given schemas".
//
// So a Node serving IS-04 v1.2 CANNOT describe its own WebSocket event
// sender, however valid that sender is on IS-05 v1.2. The two specs
// version independently and a single table for both quietly asserts
// they do not.
var transportMinIS04 = map[string]string{
	TransportRTP:       "v1.0",
	TransportRTPMcast:  "v1.0",
	TransportRTPUcast:  "v1.0",
	TransportDASH:      "v1.0",
	TransportWebSocket: "v1.3",
	TransportMQTT:      "v1.3",
	TransportNDI:       "v1.3",
	TransportUSB:       "v1.3",
	TransportMXL:       "v1.3",
}

// IsTransportAtIS04 reports whether u may appear in an IS-04 tree
// served at apiVer.
//
// A transport outside the NMOS namespace is a vendor transport, which
// every minor permits — the version gate applies only to URNs AMWA
// itself defines.
func IsTransportAtIS04(u, apiVer string) bool {
	if !IsValidTransportURN(u) {
		return false
	}
	min, known := transportMinIS04[u]
	if !known {
		return true
	}
	return compareAPIVer(apiVer, min) >= 0
}

// IsNMOSTransport reports whether u is a registered NMOS transport
// URN, at any version.
func IsNMOSTransport(u string) bool {
	_, ok := transportMinIS05[u]
	return ok
}

// IsNMOSTransportAt reports whether u is a registered NMOS transport
// that may appear on the given IS-05 wire minor ("v1.0", "v1.1", …).
//
// An unknown minor is treated as the newest: a caller that cannot say
// which version it is speaking should not have its transports silently
// narrowed.
func IsNMOSTransportAt(u, is05APIVer string) bool {
	min, ok := transportMinIS05[u]
	if !ok {
		return false
	}
	return compareAPIVer(is05APIVer, min) >= 0
}

// NMOSTransportsAt lists the transports valid on one IS-05 minor, in
// register order.
func NMOSTransportsAt(is05APIVer string) []string {
	ordered := []string{
		TransportRTP, TransportRTPMcast, TransportRTPUcast, TransportDASH,
		TransportWebSocket, TransportMQTT, TransportNDI, TransportUSB,
		TransportMXL,
	}
	out := make([]string, 0, len(ordered))
	for _, t := range ordered {
		if IsNMOSTransportAt(t, is05APIVer) {
			out = append(out, t)
		}
	}
	return out
}

// compareAPIVer orders "vMAJOR.MINOR" strings. An unparseable version
// sorts ABOVE everything, so the unknown-minor case above degrades to
// "allow", never to "silently drop".
func compareAPIVer(a, b string) int {
	amaj, amin, aok := splitAPIVer(a)
	bmaj, bmin, bok := splitAPIVer(b)
	switch {
	case !aok:
		return 1
	case !bok:
		return -1
	case amaj != bmaj:
		if amaj < bmaj {
			return -1
		}
		return 1
	case amin != bmin:
		if amin < bmin {
			return -1
		}
		return 1
	}
	return 0
}

func splitAPIVer(v string) (maj, min int, ok bool) {
	if !strings.HasPrefix(v, "v") {
		return 0, 0, false
	}
	dot := strings.IndexByte(v, '.')
	if dot < 0 {
		return 0, 0, false
	}
	maj, err := strconv.Atoi(v[1:dot])
	if err != nil {
		return 0, 0, false
	}
	min, err = strconv.Atoi(v[dot+1:])
	if err != nil {
		return 0, 0, false
	}
	return maj, min, true
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
