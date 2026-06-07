package registry

import (
	"encoding/json"

	codec "dhs/internal/amwa/codec/dnssd"
	"dhs/internal/amwa/codec/is04"
)

// pickRegistryServices returns the DNS-SD service-type names a Registry
// must advertise to satisfy every minor of IS-04 it claims to support.
//
// Spec rationale:
//   - IS-04 v1.2 renamed the Registration service from
//     `_nmos-registration._tcp` to `_nmos-register._tcp`.
//   - Per AMWA `IS0402Test.test_01` (and any peer Node still on those
//     minors) v1.0/v1.1/v1.2 clients browse the LEGACY name; v1.3+
//     clients browse the modern name.
//   - The Query face never had a legacy alias — `_nmos-query._tcp` is
//     the only name across every minor.
//
// So a registry that supports v1.0/v1.1/v1.2 alongside v1.3 MUST
// advertise on both register service-type names. Mirror of the
// consumer-side `RegistryWatcher` browse fix (#193). See
// root CLAUDE.md "AMWA NMOS strict".
func pickRegistryServices(apiVers []string) []string {
	out := []string{codec.ServiceRegister, codec.ServiceQuery}
	for _, v := range apiVers {
		if v == "v1.0" || v == "v1.1" || v == "v1.2" {
			out = append(out, codec.ServiceRegisterLegacy)
			break
		}
	}
	return out
}

// jsonUnmarshal aliases encoding/json.Unmarshal so callers don't have
// to import json directly — keeps every Registry handler-side file
// importing only `acp/...` packages.
func jsonUnmarshal(data []byte, dst any) error { return json.Unmarshal(data, dst) }

// singularFromPlural maps the IS-04 collection name in a URL
// (`devices`, `sources`, …) back to the singular ResourceType.
func singularFromPlural(p string) (is04.ResourceType, bool) {
	switch p {
	case "nodes":
		return is04.ResourceNode, true
	case "devices":
		return is04.ResourceDevice, true
	case "sources":
		return is04.ResourceSource, true
	case "flows":
		return is04.ResourceFlow, true
	case "senders":
		return is04.ResourceSender, true
	case "receivers":
		return is04.ResourceReceiver, true
	}
	return "", false
}


// getResource fetches one resource by (type, id). Returns the typed
// value and a boolean ok.
func getResource(s *Store, t is04.ResourceType, id string) (any, bool) {
	switch t {
	case is04.ResourceNode:
		v, err := s.GetNode(id)
		return v, err == nil
	case is04.ResourceDevice:
		v, err := s.GetDevice(id)
		return v, err == nil
	case is04.ResourceSource:
		v, err := s.GetSource(id)
		return v, err == nil
	case is04.ResourceFlow:
		v, err := s.GetFlow(id)
		return v, err == nil
	case is04.ResourceSender:
		v, err := s.GetSender(id)
		return v, err == nil
	case is04.ResourceReceiver:
		v, err := s.GetReceiver(id)
		return v, err == nil
	}
	return nil, false
}

