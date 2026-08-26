// Layer-3 IS-08 Channel Mapping API provider -- HTTP surface.
//
// IS-08 answers a question IS-05 cannot: IS-05 connects one Sender to
// one Receiver as a whole stream, but a device that carries 16 audio
// channels needs to say WHICH input channel feeds WHICH output
// channel. That mapping is the whole of this API.
//
// The surface is small and fixed:
//
//	/x-nmos/channelmapping/{ver}/
//	  io/                              GET
//	  map/                             GET
//	  map/active/                      GET
//	  map/active/{outputID}/           GET
//	  map/activations/                 GET POST
//	  map/activations/{activationID}/  GET DELETE
//
// There are no separate /inputs or /outputs trees. Every per-input and
// per-output property -- name, parent, channel labels, caps -- is a
// sub-object of the single `io` aggregate. That is worth stating
// because the codec has an InputProperties type and it is tempting to
// give it a route of its own; no controller would ever fetch it.
//
// Staged/active works differently from IS-05 too. IS-05 stages onto
// the endpoint and activates in place; IS-08 POSTs a whole activation
// (action + activation) and gets back an id, so several scheduled
// re-maps can be queued at once and cancelled individually.

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	stdhttp "net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
	"dhs/internal/amwa/codec/is08"
	httpsession "dhs/internal/amwa/session/http"
)

// IS08ChannelMappingConfig configures the Channel Mapping surface.
type IS08ChannelMappingConfig struct {
	// APIVer pins one wire minor, e.g. "v1.0". Empty mounts every
	// registered IS-08 codec in parallel.
	APIVer string
}

// IS08ChannelMappingServer serves the Channel Mapping API for one Node.
type IS08ChannelMappingServer struct {
	logger *slog.Logger
	vers   []string

	mu sync.RWMutex
	io is08.IO
	// active is the mapping in force right now.
	active is08.MapActive
	// activations are queued or completed re-maps, keyed by the id the
	// server minted. A scheduled activation stays here until its time
	// arrives; an immediate one is recorded so a controller can read
	// back what it just did.
	activations map[string]is08.MapActivationResponse
	// nextID mints activation ids. Sequential rather than random
	// because a controller listing activations reads them in the order
	// it created them, and a UUID would hide that.
	nextID int
	now    func() time.Time
}

// NewIS08ChannelMappingServer derives the channel-mapping IO view from
// an IS-04 bundle.
func NewIS08ChannelMappingServer(logger *slog.Logger, bundle *NodeConfig, cfg IS08ChannelMappingConfig) *IS08ChannelMappingServer {
	vers := is08.SupportedVersions()
	if cfg.APIVer != "" {
		vers = []string{cfg.APIVer}
	}
	s := &IS08ChannelMappingServer{
		logger:      logger,
		vers:        vers,
		activations: map[string]is08.MapActivationResponse{},
		now:         time.Now,
	}
	s.io = deriveIO(bundle)
	s.active = is08.MapActive{
		// No activation has happened yet, so every field of the
		// response block is null. The schema types all three as
		// nullable for exactly this state.
		Activation: is08.ActivationResponse{},
		Map:        unroutedMap(s.io),
	}
	return s
}

// Versions lists the mounted IS-08 minors.
func (s *IS08ChannelMappingServer) Versions() []string { return s.vers }

// deriveIO builds the inputs and outputs view from the IS-04 resources.
//
// The mapping between the two models is fixed by IS-08 §3: an INPUT is
// something audio arrives on (a Receiver, or a Source the device
// generates internally) and an OUTPUT is something audio leaves on (a
// Source feeding a Sender). Deriving them rather than requiring the
// operator to restate the device in a second vocabulary keeps the two
// APIs from drifting apart -- which is the failure IS-08-02 exists to
// catch.
func deriveIO(bundle *NodeConfig) is08.IO {
	out := is08.IO{
		Inputs:  map[string]is08.Input{},
		Outputs: map[string]is08.Output{},
	}
	if bundle == nil {
		return out
	}

	for i := range bundle.Receivers {
		r := &bundle.Receivers[i]
		if r.Format != formatAudio {
			continue
		}
		id := r.ID
		typ := "receiver"
		rid := r.ID
		out.Inputs[id] = is08.Input{
			Properties: &is08.InputProperties{
				Name:        labelOr(r.Label, "input"),
				Description: r.Description,
			},
			Parent:   &is08.InputParent{ID: &rid, Type: &typ},
			Channels: channelsFor(nil),
			Caps: &is08.InputCaps{
				// Reordering is true and block_size is 1: this is a
				// software node, so any input channel can feed any
				// output channel with no hardware granularity. A real
				// device with an 8-channel DSP block would say 8 here,
				// and a controller would then only offer whole-block
				// moves.
				Reordering: true,
				BlockSize:  1,
			},
		}
	}

	for i := range bundle.Sources {
		src := &bundle.Sources[i]
		if src.Format != formatAudio {
			continue
		}
		// A Source with a Sender is an OUTPUT -- audio leaving the
		// device. One with no Sender is an INPUT: an internally
		// generated signal that can be routed onward but does not
		// arrive from the network.
		if senderForSource(bundle, src.ID) != nil {
			sid := src.ID
			out.Outputs[src.ID] = is08.Output{
				Properties: &is08.OutputProperties{
					Name:        labelOr(src.Label, "output"),
					Description: src.Description,
				},
				SourceID: &sid,
				Channels: channelsFor(src.Channels),
				Caps: &is08.OutputCaps{
					// nil routable_inputs would mean "unrestricted",
					// which is true but tells a controller nothing.
					// Listing every input plus the null entry says the
					// same thing explicitly AND records that leaving a
					// channel unrouted is allowed.
					RoutableInputs: routableInputs(out.Inputs),
				},
			}
			continue
		}
		id := src.ID
		typ := "source"
		out.Inputs[src.ID] = is08.Input{
			Properties: &is08.InputProperties{
				Name:        labelOr(src.Label, "input"),
				Description: src.Description,
			},
			Parent:   &is08.InputParent{ID: &id, Type: &typ},
			Channels: channelsFor(src.Channels),
			Caps:     &is08.InputCaps{Reordering: true, BlockSize: 1},
		}
	}

	// routable_inputs was computed before the source-backed inputs
	// existed, so fill it in once the input set is final.
	for id, o := range out.Outputs {
		if o.Caps != nil {
			o.Caps.RoutableInputs = routableInputs(out.Inputs)
			out.Outputs[id] = o
		}
	}
	return out
}

const formatAudio = "urn:x-nmos:format:audio"

// channelsFor turns an IS-04 channel list into IS-08 channel labels.
//
// An audio resource that declares no channels still has channels --
// IS-04 makes the list optional, IS-08 does not. Falling back to a
// stereo pair keeps such a device mappable instead of publishing an
// input nothing can be routed to.
func channelsFor(in []is04.SourceAudioChannel) []is08.Channel {
	if len(in) == 0 {
		return []is08.Channel{{Label: "Left"}, {Label: "Right"}}
	}
	out := make([]is08.Channel, 0, len(in))
	for _, c := range in {
		out = append(out, is08.Channel{Label: labelOr(c.Label, "Channel")})
	}
	return out
}

func routableInputs(inputs map[string]is08.Input) []*string {
	ids := make([]string, 0, len(inputs))
	for id := range inputs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*string, 0, len(ids)+1)
	for i := range ids {
		v := ids[i]
		out = append(out, &v)
	}
	// The null entry is not padding: it is how IS-08 spells "this
	// output may be left unrouted". Without it a controller must treat
	// unrouting as forbidden.
	out = append(out, nil)
	return out
}

func senderForSource(bundle *NodeConfig, sourceID string) *is04.Sender {
	for i := range bundle.Flows {
		if bundle.Flows[i].SourceID != sourceID {
			continue
		}
		fid := bundle.Flows[i].ID
		for j := range bundle.Senders {
			if bundle.Senders[j].FlowID != nil && *bundle.Senders[j].FlowID == fid {
				return &bundle.Senders[j]
			}
		}
	}
	return nil
}

func labelOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// unroutedMap builds the starting map: every output channel present
// and unrouted.
//
// Absent entries are not the same as unrouted ones. The map is keyed
// by output and channel index, and a controller reads it to render
// what exists; an output missing from the map reads as an output the
// device does not have.
func unroutedMap(io is08.IO) is08.MapEntries {
	m := is08.MapEntries{}
	for id, o := range io.Outputs {
		ch := map[string]is08.MapEntry{}
		for i := range o.Channels {
			ch[strconv.Itoa(i)] = is08.MapEntry{}
		}
		m[id] = ch
	}
	return m
}

// Mount registers every Channel Mapping route on srv.
func (s *IS08ChannelMappingServer) Mount(srv *httpsession.Server) {
	ok := func(v any) (int, any, error) { return stdhttp.StatusOK, v, nil }

	srv.Handle(stdhttp.MethodGet, "/x-nmos/channelmapping", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok(withSlashes(s.vers))
	})
	srv.Handle(stdhttp.MethodGet, "/x-nmos/channelmapping/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok(withSlashes(s.vers))
	})
	for _, ver := range s.vers {
		s.mountVersion(srv, "/x-nmos/channelmapping/"+ver)
	}
}

func (s *IS08ChannelMappingServer) mountVersion(srv *httpsession.Server, base string) {
	ok := func(v any) (int, any, error) { return stdhttp.StatusOK, v, nil }

	srv.Handle(stdhttp.MethodGet, base+"/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok([]string{"io/", "map/"})
	})
	srv.Handle(stdhttp.MethodGet, base+"/io/", func(context.Context, *stdhttp.Request) (int, any, error) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return ok(s.io)
	})
	srv.Handle(stdhttp.MethodGet, base+"/map/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok([]string{"active/", "activations/"})
	})
	srv.Handle(stdhttp.MethodGet, base+"/map/active/", func(context.Context, *stdhttp.Request) (int, any, error) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return ok(s.active)
	})

	// Per-output view. Registered per known output rather than as a
	// prefix so an unknown output id falls through to the router's own
	// 404 instead of being answered with an empty map.
	s.mu.RLock()
	outputs := make([]string, 0, len(s.io.Outputs))
	for id := range s.io.Outputs {
		outputs = append(outputs, id)
	}
	s.mu.RUnlock()
	sort.Strings(outputs)
	for _, id := range outputs {
		outID := id
		srv.Handle(stdhttp.MethodGet, base+"/map/active/"+outID+"/", func(context.Context, *stdhttp.Request) (int, any, error) {
			s.mu.RLock()
			defer s.mu.RUnlock()
			entries, found := s.active.Map[outID]
			if !found {
				return stdhttp.StatusNotFound, is08.ErrorBody{
					Code: 404, Error: "Unknown output", Debug: outID,
				}, nil
			}
			return ok(entries)
		})
	}

	srv.Handle(stdhttp.MethodGet, base+"/map/activations/", func(context.Context, *stdhttp.Request) (int, any, error) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		ids := make([]string, 0, len(s.activations))
		for id := range s.activations {
			ids = append(ids, id+"/")
		}
		sort.Strings(ids)
		return ok(ids)
	})
	srv.Handle(stdhttp.MethodPost, base+"/map/activations/", func(_ context.Context, r *stdhttp.Request) (int, any, error) {
		return s.handleActivationPost(r)
	})
	srv.HandlePrefix(base+"/map/activations/", stdhttp.MethodGet, func(_ context.Context, r *stdhttp.Request) (int, any, error) {
		return s.handleActivationGet(base, r)
	})
	srv.HandlePrefix(base+"/map/activations/", stdhttp.MethodDelete, func(_ context.Context, r *stdhttp.Request) (int, any, error) {
		return s.handleActivationDelete(base, r)
	})
}

// handleActivationPost applies or queues one re-map.
func (s *IS08ChannelMappingServer) handleActivationPost(r *stdhttp.Request) (int, any, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return stdhttp.StatusBadRequest, is08.ErrorBody{Code: 400, Error: "Unreadable body", Debug: err.Error()}, nil
	}
	req, err := is08.DecodeMapActivationRequest(raw)
	if err != nil {
		return stdhttp.StatusBadRequest, is08.ErrorBody{Code: 400, Error: "Invalid activation request", Debug: err.Error()}, nil
	}
	if err := is08.ValidateMapActivationRequest(req); err != nil {
		return stdhttp.StatusBadRequest, is08.ErrorBody{Code: 400, Error: "Invalid activation request", Debug: err.Error()}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Every output and channel named in the action must exist, and the
	// input it routes from must be routable to that output. A device
	// that accepts a route it cannot make reports a mapping that is
	// not happening, which is worse than refusing.
	if err := s.validateActionLocked(req.Action); err != nil {
		return stdhttp.StatusBadRequest, is08.ErrorBody{Code: 400, Error: "Invalid action", Debug: err.Error()}, nil
	}

	mode := req.Activation.Mode
	now := is05.FormatTAINow(s.now())
	resp := is08.MapActivationResponse{
		Activation: is08.ActivationResponse{
			Mode:          &mode,
			RequestedTime: req.Activation.RequestedTime,
		},
		Action: req.Action,
	}

	if mode == is08.ActivationModeImmediate {
		s.applyLocked(req.Action)
		resp.Activation.ActivationTime = &now
		s.active.Activation = resp.Activation
		// An immediate activation is not queued: it has already
		// happened, and IS-08 §5 answers 200 with the result rather
		// than 202 with an id to poll.
		return stdhttp.StatusOK, resp, nil
	}

	s.nextID++
	id := strconv.Itoa(s.nextID)
	resp.ID = id
	s.activations[id] = resp
	// 202: accepted, not yet done. The controller polls
	// map/activations/{id} or watches map/active.
	return stdhttp.StatusAccepted, resp, nil
}

// validateActionLocked checks one action against the IO view.
func (s *IS08ChannelMappingServer) validateActionLocked(action is08.MapEntries) error {
	for outID, chans := range action {
		out, found := s.io.Outputs[outID]
		if !found {
			return fmt.Errorf("unknown output %q", outID)
		}
		for idx, entry := range chans {
			n, err := strconv.Atoi(idx)
			if err != nil || n < 0 || n >= len(out.Channels) {
				return fmt.Errorf("output %q has no channel %q", outID, idx)
			}
			// Both null is legal: it means "unroute this channel".
			if entry.Input == nil && entry.ChannelIndex == nil {
				continue
			}
			if entry.Input == nil || entry.ChannelIndex == nil {
				return fmt.Errorf("output %q channel %s: input and channel_index must both be set or both null", outID, idx)
			}
			in, found := s.io.Inputs[*entry.Input]
			if !found {
				return fmt.Errorf("unknown input %q", *entry.Input)
			}
			if *entry.ChannelIndex < 0 || *entry.ChannelIndex >= len(in.Channels) {
				return fmt.Errorf("input %q has no channel %d", *entry.Input, *entry.ChannelIndex)
			}
		}
	}
	return nil
}

// applyLocked merges an action into the active map.
//
// A merge, not a replace: an action names only the channels it
// changes, and the rest keep their current route. Replacing wholesale
// would silently unroute every channel the controller did not mention.
func (s *IS08ChannelMappingServer) applyLocked(action is08.MapEntries) {
	for outID, chans := range action {
		cur, found := s.active.Map[outID]
		if !found {
			cur = map[string]is08.MapEntry{}
			s.active.Map[outID] = cur
		}
		for idx, entry := range chans {
			cur[idx] = entry
		}
	}
}

func (s *IS08ChannelMappingServer) handleActivationGet(base string, r *stdhttp.Request) (int, any, error) {
	id := trimPathID(r.URL.Path, base+"/map/activations/")
	if id == "" {
		return stdhttp.StatusNotFound, is08.ErrorBody{Code: 404, Error: "Unknown activation"}, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, found := s.activations[id]
	if !found {
		return stdhttp.StatusNotFound, is08.ErrorBody{Code: 404, Error: "Unknown activation", Debug: id}, nil
	}
	return stdhttp.StatusOK, a, nil
}

func (s *IS08ChannelMappingServer) handleActivationDelete(base string, r *stdhttp.Request) (int, any, error) {
	id := trimPathID(r.URL.Path, base+"/map/activations/")
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, found := s.activations[id]; !found {
		return stdhttp.StatusNotFound, is08.ErrorBody{Code: 404, Error: "Unknown activation", Debug: id}, nil
	}
	delete(s.activations, id)
	// 204: the activation is gone and there is nothing to say about
	// it. A body here would imply it still exists.
	return stdhttp.StatusNoContent, nil, nil
}

// trimPathID pulls the single path segment following prefix.
func trimPathID(path, prefix string) string {
	if len(path) <= len(prefix) || path[:len(prefix)] != prefix {
		return ""
	}
	id := path[len(prefix):]
	for len(id) > 0 && id[len(id)-1] == '/' {
		id = id[:len(id)-1]
	}
	if id == "" {
		return ""
	}
	// Only a single segment is an id; anything deeper is a route we do
	// not serve.
	for i := 0; i < len(id); i++ {
		if id[i] == '/' {
			return ""
		}
	}
	return id
}

// runActivations promotes scheduled channel-map activations whose time
// has come. Returns how many fired.
func (s *IS08ChannelMappingServer) runActivations() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	fired := 0
	for id, a := range s.activations {
		if a.Activation.ActivationTime != nil {
			continue // already done
		}
		if a.Activation.RequestedTime == nil {
			continue
		}
		due, err := parseTAI(*a.Activation.RequestedTime)
		if err != nil || now.Before(due) {
			continue
		}
		s.applyLocked(a.Action)
		stamp := is05.FormatTAINow(now)
		a.Activation.ActivationTime = &stamp
		s.activations[id] = a
		s.active.Activation = a.Activation
		fired++
	}
	return fired
}

// parseTAI reads a "<seconds>:<nanoseconds>" timestamp.
func parseTAI(v string) (time.Time, error) {
	var secs, nanos int64
	if _, err := fmt.Sscanf(v, "%d:%d", &secs, &nanos); err != nil {
		return time.Time{}, err
	}
	return time.Unix(secs, nanos), nil
}

// jsonRaw is retained for symmetry with the IS-05 decoder's strict
// handling; IS-08 bodies are decoded through the codec instead.
var _ = json.Unmarshal
