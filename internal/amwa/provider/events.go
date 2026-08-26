// Layer-3 IS-07 Event & Tally API provider -- HTTP surface.
//
// IS-07 carries the things that are not essence: a tally lamp, a GPI
// closure, a fader position, a temperature. The REST surface is
// deliberately tiny --
//
//	/x-nmos/events/{ver}/
//	  sources/                 GET
//	  sources/{id}/type/       GET
//	  sources/{id}/state/      GET
//
// -- because REST is not how IS-07 is consumed. A controller reads
// `type` once to learn what the source can say, reads `state` once to
// learn what it is saying now, and then subscribes over the WebSocket
// (or MQTT) named in the Sender's IS-05 transport params for
// everything after that. The REST endpoints exist so a client can
// bootstrap and re-sync, which is exactly why `state` must always
// return the CURRENT value and never a placeholder: a client that
// re-syncs against a stale REST read carries that staleness until the
// next change, which for a tally lamp may be hours.
//
// Sources here are IS-04 Sources of format `urn:x-nmos:format:data`
// carrying an `event_type`. That is the same rule IS-04 uses to mark a
// Source as an event source, so the two views cannot disagree.

package provider

import (
	"context"
	"log/slog"
	stdhttp "net/http"
	"sort"
	"sync"
	"time"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
	"dhs/internal/amwa/codec/is07"
	httpsession "dhs/internal/amwa/session/http"
)

// IS07EventsConfig configures the Event & Tally surface.
type IS07EventsConfig struct {
	// APIVer pins one wire minor, e.g. "v1.0". Empty mounts every
	// registered IS-07 codec.
	APIVer string
}

// eventSource is one IS-07 source: what it can say, and what it is
// saying now.
type eventSource struct {
	id        string
	flowID    string
	eventType string
	// typeDef is the `type` document -- the schema of this source's
	// values, as the spec's TypeBoolean / TypeNumber / TypeString
	// shape. Held as `any` because the four variants share no Go type
	// and the endpoint serves whichever one this source is.
	typeDef any
	// state is the most recent state message. Always populated: IS-07
	// §4.2 requires the REST endpoint to answer with the current
	// value, and a source with no value yet still has a defined
	// starting one.
	state any
}

// IS07EventsServer serves the Event & Tally API for one Node.
type IS07EventsServer struct {
	logger *slog.Logger
	vers   []string

	mu      sync.RWMutex
	sources map[string]*eventSource
	now     func() time.Time
}

// NewIS07EventsServer derives the event sources from an IS-04 bundle.
func NewIS07EventsServer(logger *slog.Logger, bundle *NodeConfig, cfg IS07EventsConfig) *IS07EventsServer {
	vers := is07.SupportedVersions()
	if cfg.APIVer != "" {
		vers = []string{cfg.APIVer}
	}
	s := &IS07EventsServer{
		logger:  logger,
		vers:    vers,
		sources: map[string]*eventSource{},
		now:     time.Now,
	}
	s.seedFromBundle(bundle)
	return s
}

// Versions lists the mounted IS-07 minors.
func (s *IS07EventsServer) Versions() []string { return s.vers }

const formatData = "urn:x-nmos:format:data"

func (s *IS07EventsServer) seedFromBundle(bundle *NodeConfig) {
	if bundle == nil {
		return
	}
	for i := range bundle.Sources {
		src := &bundle.Sources[i]
		if src.Format != formatData || src.EventType == "" {
			continue
		}
		es := &eventSource{id: src.ID, eventType: src.EventType}
		// The Flow carrying this source's messages, when there is one.
		// IS-07 identity carries flow_id so a consumer can tell two
		// encodings of one source apart.
		for j := range bundle.Flows {
			if bundle.Flows[j].SourceID == src.ID {
				es.flowID = bundle.Flows[j].ID
				break
			}
		}
		es.typeDef = defaultTypeDef(src.EventType, src.Label)
		es.state = s.initialState(es)
		s.sources[src.ID] = es
	}
}

// defaultTypeDef builds the `type` document for an event_type.
//
// The document says what the source CAN say, which is a different
// question from what it is saying -- and the one a controller needs
// before it can render a control for the source at all. Without it a
// tally is just a boolean with no labels and a fader is a number with
// no range.
func defaultTypeDef(eventType, label string) any {
	switch is07.CategoryOf(eventType) {
	case is07.EventCategoryBoolean:
		return is07.TypeBoolean{Type: "boolean"}
	case is07.EventCategoryNumber:
		return is07.TypeNumber{Type: "number"}
	case is07.EventCategoryString:
		return is07.TypeString{Type: "string"}
	default:
		// object, or an event_type we do not recognise. The spec has
		// no type document for object payloads beyond the type name,
		// and inventing a schema for an opaque payload would claim
		// knowledge we do not have.
		return map[string]any{"type": "object"}
	}
}

// initialState builds the starting state message for a source.
//
// Not an empty body and not a 404. IS-07 §4.2 makes `state` the
// current value, and every source has one from the moment it exists --
// a tally that has never been set is OFF, not unknown. A client
// bootstrapping against "unknown" has to guess, and it will guess
// wrong half the time.
func (s *IS07EventsServer) initialState(es *eventSource) any {
	common := is07.EventCommon{
		MessageType: is07.MessageTypeState,
		Identity:    is07.Identity{SourceID: es.id, FlowID: es.flowID},
		Timing:      is07.Timing{CreationTimestamp: is05.FormatTAINow(s.now())},
		EventType:   es.eventType,
	}
	switch is07.CategoryOf(es.eventType) {
	case is07.EventCategoryBoolean:
		return is07.EventBoolean{EventCommon: common}
	case is07.EventCategoryNumber:
		return is07.EventNumber{EventCommon: common, Payload: is07.Number{Scale: 1}}
	case is07.EventCategoryString:
		return is07.EventString{EventCommon: common}
	default:
		return is07.EventObject{EventCommon: common, Payload: is07.PayloadObject{}}
	}
}

// SetState replaces one source's current value and returns the message
// a WebSocket subscriber should receive.
//
// Exported because the value comes from OUTSIDE this API -- a GPI
// pin, a control surface, another dhs protocol's tally. The Events API
// owns how a value is published, never what it is.
func (s *IS07EventsServer) SetState(sourceID string, payload any) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	es, found := s.sources[sourceID]
	if !found {
		return nil, false
	}
	common := is07.EventCommon{
		MessageType: is07.MessageTypeState,
		Identity:    is07.Identity{SourceID: es.id, FlowID: es.flowID},
		Timing:      is07.Timing{CreationTimestamp: is05.FormatTAINow(s.now())},
		EventType:   es.eventType,
	}
	switch v := payload.(type) {
	case bool:
		es.state = is07.EventBoolean{EventCommon: common, Payload: is07.PayloadBoolean{Value: v}}
	case float64:
		es.state = is07.EventNumber{EventCommon: common, Payload: is07.Number{Value: v, Scale: 1}}
	case int:
		es.state = is07.EventNumber{EventCommon: common, Payload: is07.Number{Value: float64(v), Scale: 1}}
	case string:
		es.state = is07.EventString{EventCommon: common, Payload: is07.PayloadString{Value: v}}
	case map[string]any:
		es.state = is07.EventObject{EventCommon: common, Payload: v}
	default:
		return nil, false
	}
	return es.state, true
}

// Mount registers every Event & Tally route on srv.
func (s *IS07EventsServer) Mount(srv *httpsession.Server) {
	ok := func(v any) (int, any, error) { return stdhttp.StatusOK, v, nil }

	srv.Handle(stdhttp.MethodGet, "/x-nmos/events", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok(withSlashes(s.vers))
	})
	srv.Handle(stdhttp.MethodGet, "/x-nmos/events/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok(withSlashes(s.vers))
	})
	for _, ver := range s.vers {
		s.mountVersion(srv, "/x-nmos/events/"+ver)
	}
}

func (s *IS07EventsServer) mountVersion(srv *httpsession.Server, base string) {
	ok := func(v any) (int, any, error) { return stdhttp.StatusOK, v, nil }

	srv.Handle(stdhttp.MethodGet, base+"/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok([]string{"sources/"})
	})
	srv.Handle(stdhttp.MethodGet, base+"/sources/", func(context.Context, *stdhttp.Request) (int, any, error) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		ids := make([]string, 0, len(s.sources))
		for id := range s.sources {
			ids = append(ids, id+"/")
		}
		sort.Strings(ids)
		return ok(ids)
	})

	// One route per known source rather than a prefix match, so an
	// unknown id gets the router's 404 instead of an invented answer.
	s.mu.RLock()
	ids := make([]string, 0, len(s.sources))
	for id := range s.sources {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	sort.Strings(ids)

	for _, id := range ids {
		srcID := id
		p := base + "/sources/" + srcID
		srv.Handle(stdhttp.MethodGet, p+"/", func(context.Context, *stdhttp.Request) (int, any, error) {
			return ok([]string{"type/", "state/"})
		})
		srv.Handle(stdhttp.MethodGet, p+"/type/", func(context.Context, *stdhttp.Request) (int, any, error) {
			s.mu.RLock()
			defer s.mu.RUnlock()
			es, found := s.sources[srcID]
			if !found {
				return stdhttp.StatusNotFound, is07.ErrorBody{Code: 404, Error: "Unknown source", Debug: srcID}, nil
			}
			return ok(es.typeDef)
		})
		srv.Handle(stdhttp.MethodGet, p+"/state/", func(context.Context, *stdhttp.Request) (int, any, error) {
			s.mu.RLock()
			defer s.mu.RUnlock()
			es, found := s.sources[srcID]
			if !found {
				return stdhttp.StatusNotFound, is07.ErrorBody{Code: 404, Error: "Unknown source", Debug: srcID}, nil
			}
			return ok(es.state)
		})
	}
}

// controlTypeEvents is the control URN a Device uses to point at its
// Event & Tally API.
const controlTypeEvents = "urn:x-nmos:control:events/"

// attachEventsAPI mounts IS-07 and advertises it on every Device.
func (s *IS04NodeServer) attachEventsAPI(srv *httpsession.Server) {
	if s.events == nil {
		return
	}
	s.events.Mount(srv)
	host := s.controlHost()
	for i := range s.bundle.Devices {
		d := &s.bundle.Devices[i]
		for _, ver := range s.events.Versions() {
			ctrl := is04.DeviceControl{
				Type: controlTypeEvents + ver,
				Href: "http://" + host + "/x-nmos/events/" + ver + "/",
			}
			if !hasControl(d.Controls, ctrl.Type) {
				d.Controls = append(d.Controls, ctrl)
			}
		}
	}
}
