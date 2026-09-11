package provider

import (
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
)

// eventEndpoint builds one endpoint with the given staged legs, its
// constraints mirrored leg for leg.
func eventEndpoint(legs ...is05.TransportParams) *connectionEndpoint {
	e := &connectionEndpoint{}
	for _, leg := range legs {
		staged := is05.TransportParams{}
		active := is05.TransportParams{}
		for k, v := range leg {
			staged[k] = v
			active[k] = v
		}
		e.staged.TransportParams = append(e.staged.TransportParams, staged)
		e.active.TransportParams = append(e.active.TransportParams, active)
		e.constraints = append(e.constraints, map[string]any{})
	}
	return e
}

// eventFlowConfig is a bundle carrying one flow of the given format.
func eventFlowConfig(flowID, sourceID, format string) *NodeConfig {
	return &NodeConfig{Flows: []is04.Flow{{
		ResourceCore: is04.ResourceCore{ID: flowID},
		SourceID:     sourceID,
		Format:       format,
	}}}
}

// The IS-07 extension parameters are filled only for an endpoint whose
// Flow is an event flow — a WebSocket sender is not automatically an
// event sender, because IS-12 uses WebSocket too.
func TestFillEventExtParams(t *testing.T) {
	const (
		flowID   = "44444444-4444-4444-8444-444444444444"
		sourceID = "33333333-3333-4333-8333-333333333333"
	)

	// A WebSocket event leg gets both keys, and the constraint set
	// grows to match exactly — the suite compares the two sets
	// (IS-05-01 test_09).
	e := eventEndpoint(is05.TransportParams{"ext_is_07_source_id": ""})
	fid := flowID
	fillEventExtParams(e, eventFlowConfig(flowID, sourceID, formatData), &fid)
	if e.eventSourceID != sourceID {
		t.Errorf("the endpoint must remember its event source: %q", e.eventSourceID)
	}
	if e.staged.TransportParams[0]["ext_is_07_source_id"] != sourceID {
		t.Errorf("staged leg = %v", e.staged.TransportParams[0])
	}
	if _, ok := e.staged.TransportParams[0]["ext_is_07_rest_api_url"]; !ok {
		t.Error("a WS event leg stages the REST URL too")
	}
	for _, key := range []string{"ext_is_07_source_id", "ext_is_07_rest_api_url"} {
		if _, ok := e.constraints[0][key]; !ok {
			t.Errorf("the constraint set must carry %s", key)
		}
	}

	// An MQTT leg carries only the REST re-sync URL.
	mqttLeg := eventEndpoint(is05.TransportParams{"broker_topic": "x/y"})
	fillEventExtParams(mqttLeg, eventFlowConfig(flowID, sourceID, formatData), &fid)
	if _, ok := mqttLeg.staged.TransportParams[0]["ext_is_07_source_id"]; ok {
		t.Error("an MQTT leg does not stage the source id")
	}
	if _, ok := mqttLeg.staged.TransportParams[0]["ext_is_07_rest_api_url"]; !ok {
		t.Error("an MQTT leg stages the REST URL")
	}

	// A leg that stages neither key is left alone.
	plain := eventEndpoint(is05.TransportParams{"destination_port": 5004})
	fillEventExtParams(plain, eventFlowConfig(flowID, sourceID, formatData), &fid)
	if len(plain.staged.TransportParams[0]) != 1 {
		t.Errorf("a non-event leg must be untouched: %v", plain.staged.TransportParams[0])
	}

	// Every way of not being an event endpoint leaves it untouched.
	empty := ""
	other := "55555555-5555-4555-8555-555555555555"
	for name, tc := range map[string]struct {
		cfg    *NodeConfig
		flowID *string
	}{
		"no flow at all":   {eventFlowConfig(flowID, sourceID, formatData), nil},
		"an empty flow id": {eventFlowConfig(flowID, sourceID, formatData), &empty},
		"a flow that is not an event flow": {
			eventFlowConfig(flowID, sourceID, is04.FormatVideo), &fid,
		},
		"a flow the bundle does not carry": {
			eventFlowConfig(other, sourceID, formatData), &fid,
		},
		"an event flow with no source": {
			eventFlowConfig(flowID, "", formatData), &fid,
		},
	} {
		t.Run(name, func(t *testing.T) {
			ep := eventEndpoint(is05.TransportParams{"ext_is_07_source_id": ""})
			fillEventExtParams(ep, tc.cfg, tc.flowID)
			if ep.eventSourceID != "" {
				t.Errorf("eventSourceID = %q, want none", ep.eventSourceID)
			}
			if ep.staged.TransportParams[0]["ext_is_07_source_id"] != "" {
				t.Errorf("staged leg = %v, want it untouched", ep.staged.TransportParams[0])
			}
		})
	}
}

// An IS-07 face that never opened a publisher has no subscribers to
// tear down — closing it is a no-op, not a nil dereference.
func TestEventsCloseWithoutAPublisher(t *testing.T) {
	s := &IS07EventsServer{}
	if err := s.Close(); err != nil {
		t.Errorf("Close = %v", err)
	}
	if got := s.Versions(); got != nil {
		t.Errorf("Versions = %v, want none", got)
	}
}
