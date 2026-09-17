package is04

import "fmt"

// BCP-005-03 (NMOS Support for IPMX/PEP) consistency rules between a
// Sender's `privacy` attribute, its SDP, and the
// urn:x-nmos:cap:transport:privacy capability.
//
// The privacy attribute is the NMOS-layer statement of whether a Sender
// is emitting PEP-encrypted media; the capability declares what a
// Receiver (or a Sender's own advertisement) will accept. The keys and
// cipher live in the IS-05 ext_privacy_* parameters + the SDP transport
// file; those are device media-plane and modeled in codec/is05.

// PrivacyCapValues extracts the boolean enum of the privacy transport
// capability from a caps map (as it appears in caps.constraint_sets[]
// or a sender's caps). Returns nil when the capability is absent.
//
// BCP-005-03 says the capability is a SINGULAR boolean — the enum holds
// exactly one of true/false, never both — but the extractor returns
// whatever the enum lists so ValidatePrivacyConsistency can report a
// two-valued enum rather than silently accepting it.
func PrivacyCapValues(caps map[string]any) []bool {
	return boolCapValues(caps, PrivacyCapabilityURN)
}

// boolCapValues pulls the boolean enum of one capability URN out of a
// caps map. Shared by the HKEP and PEP extractors.
func boolCapValues(caps map[string]any, urn string) []bool {
	cap, ok := caps[urn]
	if !ok {
		return nil
	}
	m, ok := cap.(map[string]any)
	if !ok {
		return nil
	}
	rawEnum, ok := m["enum"].([]any)
	if !ok {
		return nil
	}
	out := make([]bool, 0, len(rawEnum))
	for _, v := range rawEnum {
		if b, ok := v.(bool); ok {
			out = append(out, b)
		}
	}
	return out
}

// ValidatePrivacyConsistency enforces the BCP-005-03 "Consistency" rules
// for a Sender: the privacy attribute must agree with a declared
// urn:x-nmos:cap:transport:privacy capability.
//
//   - cap allows only true  ⇒ privacy MUST be true (and, per spec, the
//     SDP MUST carry a privacy attribute + IS-05 params other than NULL —
//     enforced on the params side by is05.ValidatePrivacyParams)
//   - cap allows only false ⇒ privacy MUST be false
//   - cap is two-valued     ⇒ rejected: the capability is singular
//   - cap absent            ⇒ no constraint
//
// privacy==nil means the Sender does not implement BCP-005-03; that is
// only a conflict when the capability restricts to a single value.
func ValidatePrivacyConsistency(privacy *bool, capValues []bool) error {
	if len(capValues) == 0 {
		return nil
	}
	allowsTrue := capAllows(capValues, true)
	allowsFalse := capAllows(capValues, false)

	switch {
	case allowsTrue && allowsFalse:
		return fmt.Errorf("is04: %s must be a singular boolean but its enum allows both true and false", PrivacyCapabilityURN)
	case allowsTrue: // only true
		if privacy == nil || !*privacy {
			return fmt.Errorf("is04: privacy capability allows only true but the sender's privacy attribute is %s", boolPtrStr(privacy))
		}
	case allowsFalse: // only false
		if privacy != nil && *privacy {
			return fmt.Errorf("is04: privacy capability allows only false but the sender's privacy attribute is true")
		}
	}
	return nil
}
