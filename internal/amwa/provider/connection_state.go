// Layer-3 IS-05 Connection API provider — connection state.
//
// A Node that serves only IS-04 can be discovered and never connected
// to. IS-05 is what turns a resource graph into something a controller
// can route: every Sender and Receiver gains a staged endpoint that
// accepts a PATCH, and an active endpoint reflecting what is actually
// running.
//
// The state machine is the whole specification in miniature (IS-05
// §4.2, §5): PATCH writes STAGED, activation promotes staged to
// ACTIVE, and nothing on the wire moves until master_enable is true.
// Getting that separation wrong is the classic implementation bug —
// a device that applies a PATCH immediately looks like it works right
// up until a controller stages a route it means to activate later.

package provider

import (
	"fmt"
	"sync"
	"time"

	"dhs/internal/amwa/codec/is05"
)

// connectionEndpoint is one Sender or Receiver's connection state.
type connectionEndpoint struct {
	// staged is what a controller has written but not yet activated.
	staged is05.StagedSender
	// active is what the endpoint is actually doing.
	active is05.StagedSender
	// constraints is the per-leg parameter envelope this endpoint
	// accepts. IS-05 requires the endpoint to publish it so a
	// controller can validate BEFORE staging (§4.2 constraints).
	constraints []map[string]any
	// transportFile is the SDP an RTP Sender serves. Receivers carry
	// theirs inside the staged PATCH instead.
	transportFile string
	transportType string
	// scheduled holds a pending timed activation, if any.
	scheduled *time.Time
}

// connectionStore holds every endpoint's connection state, keyed by
// IS-04 resource id.
//
// One mutex over the whole store rather than per endpoint: a bulk
// PATCH has to be all-or-nothing across endpoints (§5 bulk), and
// per-endpoint locks cannot give that without an ordering protocol
// nobody needs at this scale.
type connectionStore struct {
	mu        sync.RWMutex
	senders   map[string]*connectionEndpoint
	receivers map[string]*connectionEndpoint
	// now is injectable so scheduled-activation tests do not sleep.
	now func() time.Time
}

func newConnectionStore() *connectionStore {
	return &connectionStore{
		senders:   map[string]*connectionEndpoint{},
		receivers: map[string]*connectionEndpoint{},
		now:       time.Now,
	}
}

// seedFromBundle gives every Sender and Receiver in the IS-04 bundle a
// connection endpoint.
//
// IS-05 §4.1 is explicit that the two APIs describe the SAME resources:
// "the ids used in the Connection API MUST match those in the Node
// API". A sender present in one and absent from the other is the
// dangling reference that makes a controller drop the branch.
func (s *connectionStore) seedFromBundle(cfg *NodeConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range cfg.Senders {
		snd := &cfg.Senders[i]
		s.senders[snd.ID] = newEndpointForTransport(snd.Transport, true)
	}
	for i := range cfg.Receivers {
		rcv := &cfg.Receivers[i]
		s.receivers[rcv.ID] = newEndpointForTransport(rcv.Transport, false)
	}
}

// newEndpointForTransport builds the starting state for one endpoint.
//
// A fresh endpoint is master_enable=false with one leg of unset
// parameters. That is deliberate and matches a real device out of the
// box: nothing is transmitting, and a controller must stage and
// activate before anything does.
func newEndpointForTransport(transport string, isSender bool) *connectionEndpoint {
	legs := []is05.TransportParams{defaultLegParams(transport, isSender)}
	e := &connectionEndpoint{
		staged: is05.StagedSender{
			// master_enable false: a fresh endpoint transmits nothing
			// until a controller stages and activates. That matches a
			// real device out of the box.
			MasterEnableField: is05.MasterEnableField{MasterEnable: false},
			TransportParams:   legs,
			Activation:        is05.Activation{},
		},
		constraints:   []map[string]any{constraintsForTransport(transport, isSender)},
		transportType: transport,
	}
	e.active = cloneStaged(e.staged)
	return e
}

// defaultLegParams is the unset-but-present parameter set for one leg.
//
// IS-05 uses the string "auto" for "the device chooses", which is NOT
// the same as absent: absent means the parameter does not apply to
// this transport, while "auto" means it applies and the device will
// pick. Conflating them is why a receiver can look configured and
// receive nothing.
func defaultLegParams(transport string, isSender bool) is05.TransportParams {
	p := is05.TransportParams{}
	switch {
	case isRTP(transport):
		p["rtp_enabled"] = false
		if isSender {
			p["source_ip"] = "auto"
			p["destination_ip"] = "auto"
			p["source_port"] = "auto"
			p["destination_port"] = "auto"
			p["fec_enabled"] = false
			p["rtcp_enabled"] = false
		} else {
			p["source_ip"] = nil
			p["interface_ip"] = "auto"
			p["destination_port"] = "auto"
			p["fec_enabled"] = false
			p["rtcp_enabled"] = false
			p["multicast_ip"] = nil
		}
	case transport == transportWebSocketURN:
		if isSender {
			p["connection_uri"] = "auto"
			p["connection_authorization"] = false
		} else {
			p["connection_uri"] = nil
			p["connection_authorization"] = false
		}
	case transport == transportMQTTURN:
		p["destination_host"] = "auto"
		p["destination_port"] = "auto"
		p["broker_topic"] = nil
		p["broker_protocol"] = "auto"
		p["broker_authorization"] = false
		p["connection_status_broker_topic"] = nil
	default:
		// A transport we do not model in detail still gets a leg, so
		// the endpoint is addressable and a controller can read its
		// (empty) constraints rather than 404.
	}
	return p
}

// constraintsForTransport publishes what each parameter accepts.
//
// The constraints endpoint is not decoration: a controller validates
// against it before staging, and an endpoint that publishes an empty
// constraint set is telling controllers it accepts anything — which is
// worse than publishing a narrow one.
func constraintsForTransport(transport string, isSender bool) map[string]any {
	c := map[string]any{}
	switch {
	case isRTP(transport):
		c["rtp_enabled"] = map[string]any{}
		c["destination_port"] = map[string]any{
			"minimum": 5000, "maximum": 49151,
		}
		c["fec_enabled"] = map[string]any{}
		c["rtcp_enabled"] = map[string]any{}
		if isSender {
			c["source_ip"] = map[string]any{"enum": []string{"auto"}}
			c["destination_ip"] = map[string]any{}
			c["source_port"] = map[string]any{"minimum": 5000, "maximum": 49151}
		} else {
			c["interface_ip"] = map[string]any{"enum": []string{"auto"}}
			c["multicast_ip"] = map[string]any{}
			c["source_ip"] = map[string]any{}
		}
	case transport == transportWebSocketURN:
		c["connection_uri"] = map[string]any{}
		c["connection_authorization"] = map[string]any{}
	case transport == transportMQTTURN:
		c["destination_host"] = map[string]any{}
		c["destination_port"] = map[string]any{"minimum": 1, "maximum": 65535}
		c["broker_topic"] = map[string]any{}
		c["broker_protocol"] = map[string]any{}
		c["broker_authorization"] = map[string]any{}
	}
	return c
}

const (
	transportWebSocketURN = "urn:x-nmos:transport:websocket"
	transportMQTTURN      = "urn:x-nmos:transport:mqtt"
)

func isRTP(t string) bool {
	return t == "urn:x-nmos:transport:rtp" ||
		t == "urn:x-nmos:transport:rtp.mcast" ||
		t == "urn:x-nmos:transport:rtp.ucast"
}


// cloneStaged deep-copies the parts that a caller could otherwise
// mutate through a shared map. Legs are maps, so a shallow copy of the
// slice would let a later PATCH rewrite the ACTIVE state in place —
// the endpoint would report whatever was last staged as though it had
// been activated.
func cloneStaged(s is05.StagedSender) is05.StagedSender {
	out := s
	out.TransportParams = make([]is05.TransportParams, len(s.TransportParams))
	for i, leg := range s.TransportParams {
		cp := make(is05.TransportParams, len(leg))
		for k, v := range leg {
			cp[k] = v
		}
		out.TransportParams[i] = cp
	}
	if s.TransportFile != nil {
		tf := *s.TransportFile
		out.TransportFile = &tf
	}
	return out
}

// get returns the endpoint for an id, or an error naming which
// collection was searched.
func (s *connectionStore) get(kind, id string) (*connectionEndpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.senders
	if kind == "receivers" {
		m = s.receivers
	}
	e, ok := m[id]
	if !ok {
		return nil, fmt.Errorf("no %s with id %q", kind, id)
	}
	return e, nil
}

// ids lists the endpoint ids of one collection.
func (s *connectionStore) ids(kind string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.senders
	if kind == "receivers" {
		m = s.receivers
	}
	out := make([]string, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	return out
}
