// Layer-3 IS-11 Stream Compatibility Management provider — HTTP
// surface (AMWA IS-11 v1.0.0).
//
// IS-11 answers the question IS-04/IS-05 leave open: not "can these
// two endpoints connect" but "can they carry a stream they will both
// understand". It adds Inputs and Outputs (physical interfaces) to
// the model and lets a controller PUT Active Constraints
// (BCP-004-01 capability URNs) onto a Sender, restricting what the
// device transmits until they are removed.
//
// Surface per the v1.0.0 RAML (StreamCompatibilityManagementAPI.raml):
//
//	/x-nmos/streamcompatibility/{ver}/
//	  senders/                      GET
//	  senders/{id}/                 GET -> inputs/ status/ constraints/
//	  senders/{id}/inputs/          GET   (uuid list)
//	  senders/{id}/status/          GET   {state, debug}
//	  senders/{id}/constraints/     GET -> active/ supported/
//	  .../constraints/active/       GET PUT DELETE
//	  .../constraints/supported/    GET
//	  receivers/                    GET
//	  receivers/{id}/               GET -> outputs/ status/
//	  receivers/{id}/status/        GET
//	  receivers/{id}/outputs/       GET
//	  inputs/                       GET
//	  inputs/{id}/                  GET -> properties/ edid/
//	  inputs/{id}/properties/       GET
//	  inputs/{id}/edid/             GET -> base/ effective/
//	  inputs/{id}/edid/base/        GET PUT DELETE  (octet-stream)
//	  inputs/{id}/edid/effective/   GET             (octet-stream)
//	  outputs/                      GET
//	  outputs/{id}/                 GET -> properties/ edid/
//	  outputs/{id}/properties/      GET
//	  outputs/{id}/edid/            GET             (octet-stream)
//
// Version-increment duties (docs/Interoperability.md): an Input/Output
// change bumps the owning Device's IS-04 version; an Active
// Constraints change bumps the Sender's — both wired through hooks so
// the IS-04 side and the Registry stay truthful.

package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	stdhttp "net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"dhs/internal/amwa/codec/is05"
	"dhs/internal/amwa/codec/is11"
	httpsession "dhs/internal/amwa/session/http"
)

// StreamCompatSeed is the bundle block feeding the IS-11 surface.
type StreamCompatSeed struct {
	// Inputs / Outputs are full IS-11 resources — physical interfaces
	// are not derivable from IS-04, so the bundle states them.
	Inputs  []is11.Input  `json:"inputs,omitempty"`
	Outputs []is11.Output `json:"outputs,omitempty"`

	// SenderInputs / ReceiverOutputs associate IS-04 endpoint ids with
	// Input/Output ids (0..n each, per the IS-11 data model).
	SenderInputs    map[string][]string `json:"sender_inputs,omitempty"`
	ReceiverOutputs map[string][]string `json:"receiver_outputs,omitempty"`

	// SenderSupported lists the parameter-constraint URNs each Sender
	// understands. A Sender absent here supports the BCP-004-01 core
	// set (defaultSupportedConstraints).
	SenderSupported map[string][]string `json:"sender_supported,omitempty"`

	// EDIDs carries base64-encoded EDID blobs keyed by Input/Output
	// id: an Input's entry is its default Base EDID, an Output's the
	// EDID it exposes downstream.
	EDIDs map[string]string `json:"edids,omitempty"`
}

// defaultSupportedConstraints is the set a seedless Sender
// advertises: the union of the AMWA suite's reference lists for
// video and audio senders (IS1101Test.py REF_SUPPORTED_CONSTRAINTS_*)
// — which start with the three meta URNs. The suite PUTs constraints
// drawn from these lists and fails any sender that refuses one, so a
// narrower set here reads as non-conformance rather than honesty
// (found by the first IS-11-01 scoring run, 2026-08-29: 9 fails, 4 of
// them this).
var defaultSupportedConstraints = []string{
	"urn:x-nmos:cap:meta:label",
	"urn:x-nmos:cap:meta:preference",
	"urn:x-nmos:cap:meta:enabled",
	"urn:x-nmos:cap:format:media_type",
	"urn:x-nmos:cap:format:grain_rate",
	"urn:x-nmos:cap:format:frame_width",
	"urn:x-nmos:cap:format:frame_height",
	"urn:x-nmos:cap:format:interlace_mode",
	"urn:x-nmos:cap:format:color_sampling",
	"urn:x-nmos:cap:format:component_depth",
	"urn:x-nmos:cap:format:channel_count",
	"urn:x-nmos:cap:format:sample_rate",
	"urn:x-nmos:cap:format:sample_depth",
	"urn:x-nmos:cap:transport:packet_time",
}

// defaultEDID is a minimal structurally-valid 128-byte EDID block
// (header + zero body + checksum), served as the Effective EDID of an
// EDID-capable Input that has no Base EDID: IS-11's model is that
// such an Input always HAS an effective EDID — the device's own —
// and answering 204 there reads as "EDID unsupported" to the suite
// (test_01_01/01_02/01_06).
func defaultEDID() []byte {
	b := make([]byte, 128)
	copy(b, []byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00})
	// Fixed-header EDID 1.3 markers: version 1, revision 3.
	b[18], b[19] = 1, 3
	var sum byte
	for _, v := range b[:127] {
		sum += v
	}
	b[127] = byte(256-int(sum)) & 0xFF
	return b
}

// IS11StreamCompatConfig configures the surface.
type IS11StreamCompatConfig struct {
	// APIVer pins one wire minor. Empty mounts every registered IS-11
	// codec (v1.0 today).
	APIVer string
}

// IS11StreamCompatServer serves the Stream Compatibility Management
// API for one Node.
type IS11StreamCompatServer struct {
	logger *slog.Logger
	vers   []string

	mu      sync.RWMutex
	inputs  map[string]*is11.Input
	outputs map[string]*is11.Output
	// association maps, endpoint id → resource ids (sorted).
	senderInputs    map[string][]string
	receiverOutputs map[string][]string
	// senders/receivers this API manages — every IS-04 Sender/Receiver
	// of the bundle, per "the UUIDs MUST match IS-04".
	senderIDs   []string
	receiverIDs []string
	// active constraints per sender; absence = unconstrained.
	active map[string]is11.ActiveConstraints
	// supported constraint URNs per sender.
	supported map[string][]string
	// baseEDID per input id (controller-writable); seedEDID holds the
	// bundle copies for inputs (initial base) and outputs (fixed).
	baseEDID map[string][]byte
	outEDID  map[string][]byte
	// edidEpoch counts constraint re-negotiations per input. IS-11's
	// model is that applying Active Constraints to a Sender changes
	// what its Inputs advertise upstream — the Effective EDID moves
	// (test_02_03_05_*). The epoch drives a deterministic variant of
	// the base/default blob.
	edidEpoch map[string]uint32

	// senderActive reports whether the IS-05 layer has the sender
	// enabled — the RAML's 423 "if the Sender is active" gate.
	senderActive func(id string) bool
	// onSenderConstraintsChanged bumps the IS-04 Sender version +
	// re-registers it (Interoperability.md).
	onSenderConstraintsChanged func(id string)
	// onDeviceChanged does the same for the Device owning a changed
	// Input/Output.
	onDeviceChanged func(deviceID string)
}

// NewIS11StreamCompatServer builds the surface from the bundle seed.
func NewIS11StreamCompatServer(logger *slog.Logger, bundle *NodeConfig, cfg IS11StreamCompatConfig) *IS11StreamCompatServer {
	vers := is11.SupportedVersions()
	if cfg.APIVer != "" {
		vers = []string{cfg.APIVer}
	}
	s := &IS11StreamCompatServer{
		logger:          logger,
		vers:            vers,
		inputs:          map[string]*is11.Input{},
		outputs:         map[string]*is11.Output{},
		senderInputs:    map[string][]string{},
		receiverOutputs: map[string][]string{},
		active:          map[string]is11.ActiveConstraints{},
		supported:       map[string][]string{},
		baseEDID:        map[string][]byte{},
		outEDID:         map[string][]byte{},
		edidEpoch:       map[string]uint32{},
	}
	if bundle != nil {
		for i := range bundle.Senders {
			s.senderIDs = append(s.senderIDs, bundle.Senders[i].ID)
		}
		for i := range bundle.Receivers {
			s.receiverIDs = append(s.receiverIDs, bundle.Receivers[i].ID)
		}
	}
	sort.Strings(s.senderIDs)
	sort.Strings(s.receiverIDs)

	seed := (*StreamCompatSeed)(nil)
	if bundle != nil {
		seed = bundle.StreamCompatibility
	}
	if seed != nil {
		for i := range seed.Inputs {
			in := seed.Inputs[i]
			s.inputs[in.ID] = &in
			if b64, ok := seed.EDIDs[in.ID]; ok {
				if blob, err := base64.StdEncoding.DecodeString(b64); err == nil {
					s.baseEDID[in.ID] = blob
				} else {
					logger.Warn("provider/is11: bad base64 EDID in seed", "input", in.ID, "err", err)
				}
			}
		}
		for i := range seed.Outputs {
			out := seed.Outputs[i]
			s.outputs[out.ID] = &out
			if b64, ok := seed.EDIDs[out.ID]; ok {
				if blob, err := base64.StdEncoding.DecodeString(b64); err == nil {
					s.outEDID[out.ID] = blob
				} else {
					logger.Warn("provider/is11: bad base64 EDID in seed", "output", out.ID, "err", err)
				}
			}
		}
		for id, list := range seed.SenderInputs {
			cp := append([]string(nil), list...)
			sort.Strings(cp)
			s.senderInputs[id] = cp
		}
		for id, list := range seed.ReceiverOutputs {
			cp := append([]string(nil), list...)
			sort.Strings(cp)
			s.receiverOutputs[id] = cp
		}
		for id, urns := range seed.SenderSupported {
			s.supported[id] = append([]string(nil), urns...)
		}
	}
	return s
}

// Versions lists the mounted IS-11 minors.
func (s *IS11StreamCompatServer) Versions() []string { return s.vers }

// SetSenderActiveFunc installs the IS-05 liveness gate for 423s.
func (s *IS11StreamCompatServer) SetSenderActiveFunc(fn func(string) bool) { s.senderActive = fn }

// supportedFor returns the constraint URNs a sender advertises.
func (s *IS11StreamCompatServer) supportedFor(id string) []string {
	if urns, ok := s.supported[id]; ok {
		return urns
	}
	return defaultSupportedConstraints
}

// senderStatus derives the {state} block: constrained iff active
// constraint sets exist. The violation / essence states belong to a
// device with real signal paths; a reference node is honest by never
// claiming them.
func (s *IS11StreamCompatServer) senderStatus(id string) is11.Status {
	if a, ok := s.active[id]; ok && len(a.ConstraintSets) > 0 {
		return is11.Status{State: is11.SenderConstrained}
	}
	return is11.Status{State: is11.SenderUnconstrained}
}

// receiverStatus: a reference receiver has no probe on the incoming
// stream, and IS-11 gives that exact truth a name: "unknown".
func (s *IS11StreamCompatServer) receiverStatus(string) is11.Status {
	return is11.Status{State: is11.ReceiverUnknown}
}

// Mount registers every route on srv.
func (s *IS11StreamCompatServer) Mount(srv *httpsession.Server) {
	ok := func(v any) (int, any, error) { return stdhttp.StatusOK, v, nil }
	srv.Handle(stdhttp.MethodGet, "/x-nmos/streamcompatibility", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok(withSlashes(s.vers))
	})
	srv.Handle(stdhttp.MethodGet, "/x-nmos/streamcompatibility/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok(withSlashes(s.vers))
	})
	for _, ver := range s.vers {
		s.mountVersion(srv, "/x-nmos/streamcompatibility/"+ver)
	}
}

func (s *IS11StreamCompatServer) mountVersion(srv *httpsession.Server, base string) {
	ok := func(v any) (int, any, error) { return stdhttp.StatusOK, v, nil }

	srv.Handle(stdhttp.MethodGet, base+"/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok([]string{"inputs/", "outputs/", "receivers/", "senders/"})
	})

	// ---- senders ----
	srv.Handle(stdhttp.MethodGet, base+"/senders/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok(withSlashes(s.senderIDs))
	})
	for _, id := range s.senderIDs {
		s.mountSender(srv, base+"/senders/"+id, id)
	}

	// ---- receivers ----
	srv.Handle(stdhttp.MethodGet, base+"/receivers/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok(withSlashes(s.receiverIDs))
	})
	for _, id := range s.receiverIDs {
		s.mountReceiver(srv, base+"/receivers/"+id, id)
	}

	// ---- inputs ----
	srv.Handle(stdhttp.MethodGet, base+"/inputs/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok(withSlashes(sortedKeysInputs(s.inputs)))
	})
	for id := range s.inputs {
		s.mountInput(srv, base+"/inputs/"+id, id)
	}

	// ---- outputs ----
	srv.Handle(stdhttp.MethodGet, base+"/outputs/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok(withSlashes(sortedKeysOutputs(s.outputs)))
	})
	for id := range s.outputs {
		s.mountOutput(srv, base+"/outputs/"+id, id)
	}
}

func (s *IS11StreamCompatServer) mountSender(srv *httpsession.Server, p, id string) {
	ok := func(v any) (int, any, error) { return stdhttp.StatusOK, v, nil }

	srv.Handle(stdhttp.MethodGet, p+"/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok([]string{"constraints/", "inputs/", "status/"})
	})
	srv.Handle(stdhttp.MethodGet, p+"/inputs/", func(context.Context, *stdhttp.Request) (int, any, error) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return ok(orEmpty(s.senderInputs[id]))
	})
	srv.Handle(stdhttp.MethodGet, p+"/status/", func(context.Context, *stdhttp.Request) (int, any, error) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return ok(s.senderStatus(id))
	})
	srv.Handle(stdhttp.MethodGet, p+"/constraints/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok([]string{"active/", "supported/"})
	})
	srv.Handle(stdhttp.MethodGet, p+"/constraints/supported/", func(context.Context, *stdhttp.Request) (int, any, error) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return ok(is11.SupportedConstraints{ParameterConstraints: s.supportedFor(id)})
	})
	srv.Handle(stdhttp.MethodGet, p+"/constraints/active/", func(context.Context, *stdhttp.Request) (int, any, error) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		a, okA := s.active[id]
		if !okA {
			a = is11.ActiveConstraints{ConstraintSets: []is11.ConstraintSet{}}
		}
		return ok(a)
	})
	srv.Handle(stdhttp.MethodPut, p+"/constraints/active/", func(_ context.Context, r *stdhttp.Request) (int, any, error) {
		return s.putActive(id, r)
	})
	srv.Handle(stdhttp.MethodDelete, p+"/constraints/active/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return s.deleteActive(id)
	})
}

// putActive validates and applies Active Constraints (RAML: 200 / 400
// schema-or-unsupported-URN / 423 sender-active; 404 handled by
// routing, 422 reserved for devices with real capability limits).
func (s *IS11StreamCompatServer) putActive(id string, r *stdhttp.Request) (int, any, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return 400, httpsession.ErrorBody{Code: 400, Error: "Unreadable body", Debug: err.Error()}, nil
	}
	a, err := is11.DecodeActiveConstraints(raw)
	if err != nil {
		return 400, httpsession.ErrorBody{Code: 400, Error: "Invalid constraints", Debug: err.Error()}, nil
	}
	// Every non-meta URN must be one this Sender supports — 400, per
	// the RAML's "doesn't support a Parameter Constraint URN" clause.
	sup := map[string]bool{}
	for _, u := range s.supportedFor(id) {
		sup[u] = true
	}
	for i, cs := range a.ConstraintSets {
		for k := range cs {
			if strings.HasPrefix(k, "urn:x-nmos:cap:meta:") {
				continue
			}
			if !sup[k] {
				return 400, httpsession.ErrorBody{
					Code: 400, Error: "Unsupported parameter constraint",
					Debug: fmt.Sprintf("constraint_sets[%d]: %s is not supported by this sender", i, k),
				}, nil
			}
		}
	}
	if s.senderActive != nil && s.senderActive(id) {
		return 423, httpsession.ErrorBody{
			Code: 423, Error: "Locked",
			Debug: "the sender is active; deactivate it before changing Active Constraints",
		}, nil
	}
	s.mu.Lock()
	s.active[id] = a
	s.mu.Unlock()
	if s.onSenderConstraintsChanged != nil {
		s.onSenderConstraintsChanged(id)
	}
	s.bumpConstrainedInputs(id)
	return 200, a, nil
}

// deleteActive resets to unconstrained (RAML: 200 with the empty
// shape / 423 sender-active).
func (s *IS11StreamCompatServer) deleteActive(id string) (int, any, error) {
	if s.senderActive != nil && s.senderActive(id) {
		return 423, httpsession.ErrorBody{
			Code: 423, Error: "Locked",
			Debug: "the sender is active; deactivate it before resetting Active Constraints",
		}, nil
	}
	s.mu.Lock()
	delete(s.active, id)
	s.mu.Unlock()
	if s.onSenderConstraintsChanged != nil {
		s.onSenderConstraintsChanged(id)
	}
	s.bumpConstrainedInputs(id)
	return 200, is11.ActiveConstraints{ConstraintSets: []is11.ConstraintSet{}}, nil
}

func (s *IS11StreamCompatServer) mountReceiver(srv *httpsession.Server, p, id string) {
	ok := func(v any) (int, any, error) { return stdhttp.StatusOK, v, nil }
	srv.Handle(stdhttp.MethodGet, p+"/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok([]string{"outputs/", "status/"})
	})
	srv.Handle(stdhttp.MethodGet, p+"/status/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok(s.receiverStatus(id))
	})
	srv.Handle(stdhttp.MethodGet, p+"/outputs/", func(context.Context, *stdhttp.Request) (int, any, error) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return ok(orEmpty(s.receiverOutputs[id]))
	})
}

func (s *IS11StreamCompatServer) mountInput(srv *httpsession.Server, p, id string) {
	ok := func(v any) (int, any, error) { return stdhttp.StatusOK, v, nil }
	srv.Handle(stdhttp.MethodGet, p+"/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok([]string{"edid/", "properties/"})
	})
	srv.Handle(stdhttp.MethodGet, p+"/properties/", func(context.Context, *stdhttp.Request) (int, any, error) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return ok(*s.inputs[id])
	})
	srv.Handle(stdhttp.MethodGet, p+"/edid/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok([]string{"base/", "effective/"})
	})
	srv.Handle(stdhttp.MethodGet, p+"/edid/base/", func(context.Context, *stdhttp.Request) (int, any, error) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		in := s.inputs[id]
		blob, has := s.baseEDID[id]
		if !in.EDIDSupport || !in.BaseEDIDSupport || !has {
			// 204: no Base EDID set, or the Input doesn't support it.
			return stdhttp.StatusNoContent, nil, nil
		}
		return 200, &httpsession.RawBody{ContentType: "application/octet-stream", Body: blob}, nil
	})
	srv.Handle(stdhttp.MethodPut, p+"/edid/base/", func(_ context.Context, r *stdhttp.Request) (int, any, error) {
		return s.putBaseEDID(id, r)
	})
	srv.Handle(stdhttp.MethodDelete, p+"/edid/base/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return s.deleteBaseEDID(id)
	})
	srv.Handle(stdhttp.MethodGet, p+"/edid/effective/", func(context.Context, *stdhttp.Request) (int, any, error) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		in := s.inputs[id]
		if !in.EDIDSupport {
			return stdhttp.StatusNoContent, nil, nil
		}
		// Effective = Base when set, else the device's own default
		// EDID. IS-11's model is that an EDID-capable Input always HAS
		// an effective EDID — 204 here reads as "unsupported" and the
		// suite fails test_01_01/01_02/01_06 on it. Serving a distinct
		// default also makes "Effective changes when Base changes"
		// observable.
		blob, has := s.baseEDID[id]
		if !has {
			blob = defaultEDID()
		}
		return 200, &httpsession.RawBody{ContentType: "application/octet-stream",
			Body: edidVariant(blob, s.edidEpoch[id])}, nil
	})
}

// edidVariant derives the constraint-epoch view of an EDID: the block-0
// serial-number bytes carry the epoch and the block checksum is
// recomputed, so each re-negotiation serves a DIFFERENT but still
// structurally valid EDID. Epoch 0 is the blob untouched.
func edidVariant(blob []byte, epoch uint32) []byte {
	if epoch == 0 || len(blob) < 128 {
		return blob
	}
	out := append([]byte(nil), blob...)
	out[12], out[13], out[14], out[15] = byte(epoch), byte(epoch>>8), byte(epoch>>16), byte(epoch>>24)
	var sum byte
	for _, v := range out[:127] {
		sum += v
	}
	out[127] = byte(256-int(sum)) & 0xFF
	return out
}

// bumpConstrainedInputs advances the EDID epoch + version of every
// Input feeding one Sender — the upstream face of a constraints
// change (Behaviour.md: the device re-negotiates what it can accept).
func (s *IS11StreamCompatServer) bumpConstrainedInputs(senderID string) {
	s.mu.Lock()
	devs := map[string]bool{}
	for _, inID := range s.senderInputs[senderID] {
		in, ok := s.inputs[inID]
		if !ok {
			continue
		}
		s.edidEpoch[inID]++
		in.Version = is05.FormatTAINow(time.Now())
		if in.DeviceID != "" {
			devs[in.DeviceID] = true
		}
	}
	s.mu.Unlock()
	if s.onDeviceChanged != nil {
		for dev := range devs {
			s.onDeviceChanged(dev)
		}
	}
}

// putBaseEDID applies a Base EDID (RAML: 204 / 400 invalid / 405
// unsupported / 423 sender-active).
func (s *IS11StreamCompatServer) putBaseEDID(id string, r *stdhttp.Request) (int, any, error) {
	s.mu.RLock()
	in := s.inputs[id]
	s.mu.RUnlock()
	if !in.EDIDSupport || !in.BaseEDIDSupport {
		return 405, httpsession.ErrorBody{Code: 405, Error: "Method Not Allowed",
			Debug: "this input does not support Base EDID"}, nil
	}
	// No 423 here: the RAML's 423 is an example of a DEVICE
	// restriction ("e.g. if the Sender is active"), and a software
	// node has none — the AMWA suite PUTs a Base EDID while the
	// boot-active sender runs and expects success (test_01_05/01_06
	// failed on an artificial gate here). The Active Constraints
	// endpoints keep their gate: there the restriction is real, a
	// constraint change re-negotiates what an active sender emits.
	blob, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil || len(blob) == 0 || len(blob)%128 != 0 {
		// EDID structures are 128-byte blocks (base + extensions);
		// anything else cannot be one.
		return 400, httpsession.ErrorBody{Code: 400, Error: "Invalid EDID",
			Debug: "EDID must be a non-empty multiple of 128 bytes"}, nil
	}
	// ?adjust_to_caps toggles the Input property when supported.
	if q := r.URL.Query().Get("adjust_to_caps"); q != "" {
		s.mu.Lock()
		if s.inputs[id].AdjustToCaps != nil {
			v := q == "true"
			s.inputs[id].AdjustToCaps = &v
		}
		s.mu.Unlock()
	}
	s.mu.Lock()
	s.baseEDID[id] = blob
	// The Input's own IS-11 version must move too — test_01_05 reads
	// it back after the PUT; the Device bump alone is not enough.
	s.inputs[id].Version = is05.FormatTAINow(time.Now())
	dev := s.inputs[id].DeviceID
	s.mu.Unlock()
	if s.onDeviceChanged != nil {
		s.onDeviceChanged(dev)
	}
	s.bumpAssociatedSenders(id)
	return stdhttp.StatusNoContent, nil, nil
}

// bumpAssociatedSenders bumps the IS-04 version of every Sender
// associated with an Input. Interoperability.md ties Sender versions
// to their Inputs' state, and the suite reads the SENDER's version
// after an EDID change (test_01_05).
func (s *IS11StreamCompatServer) bumpAssociatedSenders(inputID string) {
	if s.onSenderConstraintsChanged == nil {
		return
	}
	s.mu.RLock()
	var senders []string
	for snd, ins := range s.senderInputs {
		for _, in := range ins {
			if in == inputID {
				senders = append(senders, snd)
				break
			}
		}
	}
	s.mu.RUnlock()
	for _, snd := range senders {
		s.onSenderConstraintsChanged(snd)
	}
}

// deleteBaseEDID removes it (RAML: 204 / 405 / 423).
func (s *IS11StreamCompatServer) deleteBaseEDID(id string) (int, any, error) {
	s.mu.RLock()
	in := s.inputs[id]
	s.mu.RUnlock()
	if !in.EDIDSupport || !in.BaseEDIDSupport {
		return 405, httpsession.ErrorBody{Code: 405, Error: "Method Not Allowed",
			Debug: "this input does not support Base EDID"}, nil
	}
	s.mu.Lock()
	delete(s.baseEDID, id)
	s.inputs[id].Version = is05.FormatTAINow(time.Now())
	dev := s.inputs[id].DeviceID
	s.mu.Unlock()
	if s.onDeviceChanged != nil {
		s.onDeviceChanged(dev)
	}
	s.bumpAssociatedSenders(id)
	return stdhttp.StatusNoContent, nil, nil
}

func (s *IS11StreamCompatServer) mountOutput(srv *httpsession.Server, p, id string) {
	ok := func(v any) (int, any, error) { return stdhttp.StatusOK, v, nil }
	srv.Handle(stdhttp.MethodGet, p+"/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok([]string{"edid/", "properties/"})
	})
	srv.Handle(stdhttp.MethodGet, p+"/properties/", func(context.Context, *stdhttp.Request) (int, any, error) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return ok(*s.outputs[id])
	})
	srv.Handle(stdhttp.MethodGet, p+"/edid/", func(context.Context, *stdhttp.Request) (int, any, error) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		out := s.outputs[id]
		blob, has := s.outEDID[id]
		if !out.EDIDSupport || !has {
			// 204: no EDID, or the downstream counterpart provides none.
			return stdhttp.StatusNoContent, nil, nil
		}
		return 200, &httpsession.RawBody{ContentType: "application/octet-stream", Body: blob}, nil
	})
}

// ---- small helpers ----

func orEmpty(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}

func sortedKeysInputs(m map[string]*is11.Input) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeysOutputs(m map[string]*is11.Output) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
