package is04

import "fmt"

// BCP-005-02 (NMOS Support for IPMX/HKEP) consistency rules between a
// Sender's `hkep` attribute, its SDP, and the
// urn:x-nmos:cap:transport:hkep capability.

// HKEPCapValues extracts the boolean enum of the hkep transport
// capability from a caps map (as it appears in caps.constraint_sets[]
// or a sender's caps). Returns nil when the capability is absent.
func HKEPCapValues(caps map[string]any) []bool {
	return boolCapValues(caps, HKEPCapabilityURN)
}

// capAllows reports whether a boolean enum permits value v.
func capAllows(values []bool, v bool) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}

// ValidateHKEPConsistency enforces the BCP-005-02 "Consistency" rules
// for a Sender: the hkep attribute (present ⇒ matches the SDP) must
// agree with a declared urn:x-nmos:cap:transport:hkep capability.
//
//   - cap allows only true  ⇒ hkep MUST be true
//   - cap allows only false ⇒ hkep MUST be false
//   - cap allows both       ⇒ either is valid
//   - cap absent            ⇒ no constraint
//
// hkep==nil means the Sender does not implement BCP-005-02; that is
// only a conflict when a capability restricts to a single value.
func ValidateHKEPConsistency(hkep *bool, capValues []bool) error {
	if len(capValues) == 0 {
		return nil
	}
	allowsTrue := capAllows(capValues, true)
	allowsFalse := capAllows(capValues, false)

	switch {
	case allowsTrue && allowsFalse:
		return nil // both permitted
	case allowsTrue: // only true
		if hkep == nil || !*hkep {
			return fmt.Errorf("is04: hkep capability allows only true but the sender's hkep attribute is %s", boolPtrStr(hkep))
		}
	case allowsFalse: // only false
		if hkep != nil && *hkep {
			return fmt.Errorf("is04: hkep capability allows only false but the sender's hkep attribute is true")
		}
	}
	return nil
}

func boolPtrStr(b *bool) string {
	if b == nil {
		return "absent"
	}
	if *b {
		return "true"
	}
	return "false"
}
