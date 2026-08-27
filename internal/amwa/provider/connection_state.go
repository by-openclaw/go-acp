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
	// id is the IS-04 resource id, carried so an activation can name
	// itself to the promote hook without the store re-scanning its own
	// maps to find which key it just modified.
	id string
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
	// isSender selects the sender or receiver parameter rules when
	// resolving "auto" — the two differ (a sender picks a destination,
	// a receiver picks an interface).
	isSender bool
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
	// nodeIP is what "auto" resolves to for source_ip / interface_ip.
	// A controller reads ACTIVE to learn where a stream comes from, so
	// this has to be an address that is actually reachable, not a
	// placeholder.
	nodeIP string
	// nodeBase is host:port -- what a URL must name to be fetchable.
	// nodeIP alone is enough for a transport parameter typed as an
	// address, and not enough for one typed as a URL.
	nodeBase string
	// onPromote fires after every activation, inside the store lock,
	// and returns the transport file the endpoint should serve from
	// then on.
	//
	// The store cannot compute that itself: an SDP needs the IS-04
	// Flow, and reflecting an activation into IS-04's
	// `subscription.active` needs the IS-04 bundle. Both live a layer
	// up. A callback keeps the store ignorant of IS-04 rather than
	// giving it a bundle pointer it would then be tempted to read on
	// every request.
	onPromote func(kind, id string, active is05.StagedSender) string
}

func newConnectionStore() *connectionStore {
	return &connectionStore{
		senders:   map[string]*connectionEndpoint{},
		receivers: map[string]*connectionEndpoint{},
		now:       time.Now,
	}
}

// setNodeIP records the address "auto" resolves to.
func (s *connectionStore) setNodeIP(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodeIP = ip
}

// setNodeBase records the host:port URLs should name.
func (s *connectionStore) setNodeBase(hostPort string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodeBase = hostPort
}

// reresolveActive recomputes every endpoint's ACTIVE addresses against
// the current nodeIP.
//
// Called once the Node knows what address it actually answers on.
// Only endpoints that have never been activated are touched: an
// activated endpoint's ACTIVE state describes a stream that is running
// now, and rewriting its addresses out from under a controller would
// silently retarget live traffic.
func (s *connectionStore) reresolveActive() {
	s.mu.Lock()
	defer s.mu.Unlock()
	ip := s.nodeIP
	if ip == "" {
		ip = "127.0.0.1"
	}
	// The IS-07 REST base, now that the Node knows where it answers.
	// A consumer reads this to fetch a source's current value before
	// its first WebSocket message arrives, so it must carry the PORT
	// as well as the address.
	restBase, wsURI := "", ""
	if s.nodeBase != "" {
		restBase = "http://" + s.nodeBase + "/x-nmos/events/" + eventsWireVersion + "/"
		wsURI = "ws://" + s.nodeBase + "/x-nmos/events/" + eventsWireVersion + "/ws"
	}
	for _, set := range []map[string]*connectionEndpoint{s.senders, s.receivers} {
		for _, e := range set {
			// A sender's source_ip is seeded blank because the Node
			// does not know its own address until the endpoint list is
			// expanded. Filling it in STAGED as well as active is the
			// point of this pass: source_ip may not be "auto", so
			// there is no placeholder that would survive validation
			// until the first activation.
			if e.isSender {
				for i := range e.staged.TransportParams {
					if v, ok := e.staged.TransportParams[i]["source_ip"].(string); ok && v == "" {
						e.staged.TransportParams[i]["source_ip"] = ip
					}
				}
			}
			if restBase != "" {
				for i := range e.staged.TransportParams {
					p := e.staged.TransportParams[i]
					if v, ok := p["ext_is_07_rest_api_url"].(string); ok && v == "" {
						p["ext_is_07_rest_api_url"] = restBase
					}
					// A WebSocket sender with a null connection_uri
					// publishes a stream nothing can reach: IS-07 §5
					// makes this the ONLY address a consumer gets. The
					// socket exists from the moment the Node serves,
					// so there is no state in which "not yet known" is
					// the truthful answer.
					if _, carries := p["connection_uri"]; carries && p["connection_uri"] == nil {
						p["connection_uri"] = wsURI
					}
				}
			}
			if e.active.Activation.ActivationTime != nil {
				continue
			}
			e.active = cloneStaged(e.staged)
			for i := range e.active.TransportParams {
				resolveAuto(e.active.TransportParams[i], s.nodeIP, e.isSender, i)
			}
		}
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
		e := newEndpointForTransport(snd.Transport, true)
		e.id = snd.ID
		fillEventExtParams(e, cfg, snd.FlowID)
		s.senders[snd.ID] = e
	}
	for i := range cfg.Receivers {
		rcv := &cfg.Receivers[i]
		e := newEndpointForTransport(rcv.Transport, false)
		e.id = rcv.ID
		fillEventExtParams(e, cfg, nil)
		s.receivers[rcv.ID] = e
	}
}

// fillEventExtParams resolves the IS-07 extension parameters for an
// endpoint that carries events.
//
// Only endpoints whose Flow is an event flow get them. A WebSocket
// sender is not automatically an event sender -- IS-12 uses WebSocket
// too -- so the discriminator is the Flow's format, the same one IS-07
// uses to decide what an event source is.
func fillEventExtParams(e *connectionEndpoint, cfg *NodeConfig, flowID *string) {
	if flowID == nil || *flowID == "" {
		return
	}
	var sourceID string
	for i := range cfg.Flows {
		if cfg.Flows[i].ID != *flowID {
			continue
		}
		if cfg.Flows[i].Format != formatData {
			return
		}
		sourceID = cfg.Flows[i].SourceID
		break
	}
	if sourceID == "" {
		return
	}
	for i := range e.staged.TransportParams {
		if _, carries := e.staged.TransportParams[i]["ext_is_07_source_id"]; !carries {
			continue
		}
		e.staged.TransportParams[i]["ext_is_07_source_id"] = sourceID
		e.staged.TransportParams[i]["ext_is_07_rest_api_url"] = ""
		e.active.TransportParams[i]["ext_is_07_source_id"] = sourceID
		e.active.TransportParams[i]["ext_is_07_rest_api_url"] = ""
	}
	// The constraint set has to grow the same two keys or the
	// endpoint's own PATCH validation would reject them as unknown.
	for i := range e.constraints {
		if _, carries := e.constraints[i]["ext_is_07_source_id"]; !carries {
			e.constraints[i]["ext_is_07_source_id"] = map[string]any{}
			e.constraints[i]["ext_is_07_rest_api_url"] = map[string]any{}
		}
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
		isSender:      isSender,
	}
	// ACTIVE never contains "auto", not even before the first
	// activation: active describes what the device is DOING, and a
	// device that has decided nothing yet has still decided the
	// address it would use. test_11_01/test_12_01 check exactly this.
	e.active = cloneStaged(e.staged)
	for i := range e.active.TransportParams {
		resolveAuto(e.active.TransportParams[i], "", isSender, i)
	}
	return e
}

// defaultLegParams is the unset-but-present parameter set for one leg.
//
// The key set is not a choice. constraints-schema-rtp.json says
// "every transport parameter must have an entry" and sets
// additionalProperties:false, so transport_params and constraints must
// carry EXACTLY the same keys — a mismatch is what the tool reports as
// "Invalid combination of parameters on constraints endpoint".
//
// fec_* and rtcp_* are deliberately absent. They are optional, and
// declaring fec_enabled without the six fec parameters that give it
// meaning is precisely the invalid combination above. A device that
// does not do FEC should say nothing about FEC.
//
// "auto" means "the device will choose", which is NOT the same as
// absent: absent means the parameter does not apply to this transport.
// Conflating them is why a receiver can look configured and receive
// nothing.
func defaultLegParams(transport string, isSender bool) is05.TransportParams {
	p := is05.TransportParams{}
	switch {
	case isRTP(transport):
		p["rtp_enabled"] = false
		p["destination_port"] = "auto"
		// "auto" is NOT legal on every parameter, and the split is not
		// symmetric between the two roles.
		//
		// IS-05's RTP schemas allow the literal "auto" only where the
		// device genuinely gets to choose: a sender's destination and
		// ports, a receiver's interface. A sender's source_ip is an
		// enum of the addresses the device HAS, and a receiver's
		// source_ip is the far end's address or null -- neither may be
		// "auto", so seeding it there makes staged fail validation
		// against our own published constraints (IS-05-01 test_16).
		if isSender {
			p["source_ip"] = ""
			p["destination_ip"] = "auto"
			p["source_port"] = "auto"
		} else {
			p["source_ip"] = nil
			p["interface_ip"] = "auto"
			p["multicast_ip"] = nil
		}
	case transport == transportWebSocketURN:
		p["connection_uri"] = nil
		p["connection_authorization"] = false
		// IS-07 §5 extension parameters.
		//
		// `ext_` params are how IS-05 carries information a transport
		// needs that IS-05 itself knows nothing about. For events that
		// is which SOURCE the WebSocket carries and where to read its
		// current value over REST -- without them a consumer has a
		// socket it can open and no way to know what arrives on it.
		// The values are filled in per endpoint by seedFromBundle,
		// which is the only place that knows which source this sender
		// belongs to.
		p["ext_is_07_source_id"] = nil
		p["ext_is_07_rest_api_url"] = nil
	case transport == transportMQTTURN:
		p["destination_host"] = "auto"
		p["destination_port"] = "auto"
		p["broker_topic"] = nil
		p["broker_protocol"] = "auto"
		p["broker_authorization"] = "auto"
		p["connection_status_broker_topic"] = nil
	}
	return p
}

// constraintsForTransport publishes what each parameter accepts.
//
// One entry per transport parameter, same keys, no extras — see
// defaultLegParams. An empty object is a legal constraint and means
// "no dynamic restriction", which is honest: a constraint set that
// invents limits the device does not enforce is worse than one that
// admits it has none.
func constraintsForTransport(transport string, isSender bool) map[string]any {
	c := map[string]any{}
	for k := range defaultLegParams(transport, isSender) {
		// Every entry is an EMPTY constraint.
		//
		// A numeric minimum/maximum on a port looks more precise and
		// is wrong: the same parameter legally holds the string "auto"
		// before activation, and the tool validates STAGED against
		// these constraints — "'auto' is not valid under any of the
		// given schemas". Publishing a bound we cannot honour for
		// every legal value is worse than publishing none.
		c[k] = map[string]any{}
	}
	return c
}

// resolveAuto replaces every "auto" with the concrete value the device
// chose.
//
// IS-05 is explicit that ACTIVE parameters must not contain "auto":
// active describes what the device is DOING, and "the device will
// decide" is not something it can still be doing once activated. The
// AMWA rounds test_11_01 / test_12_01 exist for exactly this, and it
// is a real fault rather than a formality — a controller reads active
// to learn the multicast address it must join.
func resolveAuto(p is05.TransportParams, nodeIP string, isSender bool, index int) {
	if nodeIP == "" {
		nodeIP = "127.0.0.1"
	}
	for k, v := range p {
		s, ok := v.(string)
		if !ok || s != autoKeyword {
			continue
		}
		switch k {
		case "source_ip", "interface_ip":
			p[k] = nodeIP
		case "destination_ip":
			// A deterministic multicast group per leg. Leg 1 and leg 2
			// of a 2022-7 pair land in DIFFERENT /24s, because putting
			// both legs on one subnet defeats the redundancy - the
			// fault the plant audit found on 48 senders.
			p[k] = fmt.Sprintf("239.%d.1.1", 4+index*2)
		case "multicast_ip":
			// The group a receiver joins. Same per-leg spread as a
			// sender's destination_ip, and for the same reason: two
			// legs of a 2022-7 pair on one subnet is not redundancy.
			p[k] = fmt.Sprintf("239.%d.1.1", 4+index*2)
		case "source_port", "destination_port":
			p[k] = defaultRTPPort + index*2
		case "destination_host":
			p[k] = nodeIP
		case "broker_protocol":
			p[k] = "mqtt"
		case "broker_authorization", "connection_authorization":
			p[k] = false
		case "connection_uri":
			// The WebSocket a consumer connects to for this sender's
			// events. IS-07 §5 makes this the ONLY way a consumer
			// finds the stream, so "auto" has to resolve to a real
			// URL rather than being left for the operator.
			p[k] = "ws://" + nodeIP + "/x-nmos/events/" + eventsWireVersion + "/"
		default:
			// An "auto" on a parameter with no defined resolution is
			// left alone rather than guessed at: inventing a value
			// would be worse than reporting the device never resolved
			// it.
		}
	}
}

const (
	autoKeyword    = "auto"
	defaultRTPPort = 5004
)
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
