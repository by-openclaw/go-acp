package registry

import (
	"encoding/json"

	"acp/internal/amwa/codec/is04"
)

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

// listResource returns every resource of the given type as `[]any`
// (so the HTTP layer can JSON-encode it without per-type plumbing).
func listResource(s *Store, t is04.ResourceType) any {
	switch t {
	case is04.ResourceNode:
		return s.ListNodes()
	case is04.ResourceDevice:
		return s.ListDevices()
	case is04.ResourceSource:
		return s.ListSources()
	case is04.ResourceFlow:
		return s.ListFlows()
	case is04.ResourceSender:
		return s.ListSenders()
	case is04.ResourceReceiver:
		return s.ListReceivers()
	}
	return []any{}
}
