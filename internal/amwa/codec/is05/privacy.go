package is05

import "fmt"

// BCP-005-03 (NMOS With IPMX/PEP) IS-05 extended transport parameters.
//
// PEP (the IPMX Privacy Encryption Protocol) negotiates media-plane
// encryption. The KEYS and the cipher itself are device media-plane and
// out of scope for a reference node; what IS-05 models — and what a
// Controller reads to wire two peers together — is the SET of extended
// transport parameters below, plus the rule that a Sender advertising no
// privacy pins protocol and mode to the "NULL" sentinel.
//
// The spec names the parameters with the `ext_` prefix IS-05 reserves
// for information a transport needs that IS-05 itself knows nothing
// about (§ transport parameters), so they layer onto ANY RTP leg
// without a new transport URN.
const (
	// Core parameters — every PEP-capable device carries these.
	ParamPrivacyProtocol     = "ext_privacy_protocol"
	ParamPrivacyMode         = "ext_privacy_mode"
	ParamPrivacyIV           = "ext_privacy_iv"
	ParamPrivacyKeyGenerator = "ext_privacy_key_generator"
	ParamPrivacyKeyVersion   = "ext_privacy_key_version"
	ParamPrivacyKeyID        = "ext_privacy_key_id"

	// ECDH parameters — only devices doing an ECDH key exchange carry
	// these. A device using a pre-shared key omits the whole trio.
	ParamPrivacyECDHSenderPubKey   = "ext_privacy_ecdh_sender_public_key"
	ParamPrivacyECDHReceiverPubKey = "ext_privacy_ecdh_receiver_public_key"
	ParamPrivacyECDHCurve          = "ext_privacy_ecdh_curve"
)

// PrivacyNull is the sentinel a PEP parameter carries when the feature
// is not engaged. It is a real value, not absence: a PEP-capable device
// advertises the parameters at all times and sets them to "NULL" when
// privacy is off, so a Controller can tell "encryption disabled" apart
// from "device does not implement PEP" (the latter omits the parameters
// entirely).
const PrivacyNull = "NULL"

// PrivacyProtocols is the closed set of ext_privacy_protocol values.
var PrivacyProtocols = []string{"RTP", "RTP_KV", "USB", "USB_KV", PrivacyNull}

// PrivacyECDHCurves is the closed set of ext_privacy_ecdh_curve values.
var PrivacyECDHCurves = []string{"secp256r1", "secp521r1", "25519", "448", PrivacyNull}

// IsValidPrivacyProtocol reports whether v is a spec ext_privacy_protocol.
func IsValidPrivacyProtocol(v string) bool { return inSet(PrivacyProtocols, v) }

// IsValidPrivacyECDHCurve reports whether v is a spec ext_privacy_ecdh_curve.
func IsValidPrivacyECDHCurve(v string) bool { return inSet(PrivacyECDHCurves, v) }

func inSet(set []string, v string) bool {
	for _, x := range set {
		if x == v {
			return true
		}
	}
	return false
}

// PrivacyLegParams builds the unset-but-present PEP parameter set for one
// leg — every value at the "NULL" sentinel, which is the state of a
// PEP-capable device with encryption disabled. Pass ecdh=true to include
// the ECDH trio (a device doing an ECDH key exchange); false for a
// pre-shared-key device, which omits it.
//
// The keys returned here MUST also be added to the leg's constraints
// object with the same names, or the endpoint's own PATCH validation
// rejects them as unknown (constraints and transport_params carry
// exactly the same key set).
func PrivacyLegParams(ecdh bool) TransportParams {
	p := TransportParams{
		ParamPrivacyProtocol:     PrivacyNull,
		ParamPrivacyMode:         PrivacyNull,
		ParamPrivacyIV:           PrivacyNull,
		ParamPrivacyKeyGenerator: PrivacyNull,
		ParamPrivacyKeyVersion:   PrivacyNull,
		ParamPrivacyKeyID:        PrivacyNull,
	}
	if ecdh {
		p[ParamPrivacyECDHSenderPubKey] = PrivacyNull
		p[ParamPrivacyECDHReceiverPubKey] = PrivacyNull
		p[ParamPrivacyECDHCurve] = PrivacyNull
	}
	return p
}

// PrivacyParamKeys lists the PEP parameter names present in a leg. Used
// to grow the constraints object alongside transport_params.
func PrivacyParamKeys(p TransportParams) []string {
	all := []string{
		ParamPrivacyProtocol, ParamPrivacyMode, ParamPrivacyIV,
		ParamPrivacyKeyGenerator, ParamPrivacyKeyVersion, ParamPrivacyKeyID,
		ParamPrivacyECDHSenderPubKey, ParamPrivacyECDHReceiverPubKey, ParamPrivacyECDHCurve,
	}
	out := []string{}
	for _, k := range all {
		if _, ok := p[k]; ok {
			out = append(out, k)
		}
	}
	return out
}

// ValidatePrivacyParams enforces the BCP-005-03 rules that bind the IS-05
// PEP parameters to the Sender's `privacy` attribute:
//
//   - ext_privacy_protocol, when present, MUST be a known protocol; the
//     same for ext_privacy_ecdh_curve.
//   - When the Sender's privacy attribute is false (encryption off),
//     ext_privacy_protocol AND ext_privacy_mode MUST be "NULL".
//   - When the Sender's privacy attribute is true, ext_privacy_protocol
//     MUST NOT be "NULL" — an enabled Sender that named no protocol is
//     self-contradictory.
//
// privacy==nil means the Sender does not implement BCP-005-03; the
// parameters are then only checked for enum validity, not for the
// on/off coupling.
func ValidatePrivacyParams(p TransportParams, privacy *bool) error {
	if p == nil {
		return nil
	}
	proto, hasProto := stringParam(p, ParamPrivacyProtocol)
	if hasProto && !IsValidPrivacyProtocol(proto) {
		return fmt.Errorf("is05: %s %q not one of %v", ParamPrivacyProtocol, proto, PrivacyProtocols)
	}
	if curve, ok := stringParam(p, ParamPrivacyECDHCurve); ok && !IsValidPrivacyECDHCurve(curve) {
		return fmt.Errorf("is05: %s %q not one of %v", ParamPrivacyECDHCurve, curve, PrivacyECDHCurves)
	}
	mode, hasMode := stringParam(p, ParamPrivacyMode)
	if privacy != nil && !*privacy {
		if hasProto && proto != PrivacyNull {
			return fmt.Errorf("is05: privacy attribute is false but %s is %q (must be %q)", ParamPrivacyProtocol, proto, PrivacyNull)
		}
		if hasMode && mode != PrivacyNull {
			return fmt.Errorf("is05: privacy attribute is false but %s is %q (must be %q)", ParamPrivacyMode, mode, PrivacyNull)
		}
	}
	if privacy != nil && *privacy && hasProto && proto == PrivacyNull {
		return fmt.Errorf("is05: privacy attribute is true but %s is %q", ParamPrivacyProtocol, PrivacyNull)
	}
	return nil
}

func stringParam(p TransportParams, key string) (string, bool) {
	v, ok := p[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
