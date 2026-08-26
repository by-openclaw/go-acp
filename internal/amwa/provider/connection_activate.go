// Layer-3 IS-05 Connection API provider — staging and activation.
//
// IS-05 §5.2: a PATCH writes STAGED. Whether it also promotes staged to
// ACTIVE depends entirely on `activation.mode`:
//
//	(absent)                      stage only — nothing moves
//	activate_immediate            promote now
//	activate_scheduled_relative   promote after activation_time
//	activate_scheduled_absolute   promote at activation_time (TAI)
//
// The separation is the point. A device that promotes on every PATCH
// passes casual testing and then breaks the first time a controller
// stages a route it means to take later — which is exactly how
// multi-device switches are built.

package provider

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"dhs/internal/amwa/codec/is05"
)

// applyPatch merges a PATCH body into staged and, if the activation
// mode says so, promotes it.
//
// Returns the resulting staged state and the HTTP status the spec
// requires: 200 for a stage, and 200 for an immediate activation —
// but 202 for a SCHEDULED one, because the response describes
// something that has not happened yet (§5.2).
// present carries which top-level fields the PATCH body actually
// contained. It is required because MasterEnable is a plain bool on
// the canonical struct: a typed decode cannot tell "master_enable
// absent" from "master_enable false", and a merge that guesses would
// silently disable an endpoint the controller never mentioned.
type patchFields struct {
	MasterEnable    bool
	TransportParams bool
	TransportFile   bool
	SenderID        bool
	ReceiverID      bool
}

func (s *connectionStore) applyPatch(kind, id string, patch is05.StagedSender, present patchFields) (is05.StagedSender, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	m := s.senders
	if kind == "receivers" {
		m = s.receivers
	}
	e, ok := m[id]
	if !ok {
		return is05.StagedSender{}, 404, fmt.Errorf("no %s with id %q", kind, id)
	}

	if err := is05.ValidateActivation(patch.Activation); err != nil {
		return is05.StagedSender{}, 400, err
	}

	// A PATCH is a MERGE, not a replace: fields the controller did not
	// send keep their staged value, and a leg it did not mention keeps
	// its parameters. Replacing wholesale would make every PATCH a
	// full re-specification of the endpoint, which is not what any
	// controller sends.
	merged := cloneStaged(e.staged)
	if present.MasterEnable {
		merged.MasterEnable = patch.MasterEnable
	}
	if present.TransportFile && patch.TransportFile != nil {
		tf := *patch.TransportFile
		merged.TransportFile = &tf
	}
	// A sender PATCH carries receiver_id and a receiver PATCH carries
	// sender_id. Both land in the shared struct's one id slot — the
	// VIEW renames it per collection — so either being present must
	// write it, or a receiver's sender_id is silently dropped
	// (test_21).
	if present.ReceiverID || present.SenderID {
		merged.ReceiverID = patch.ReceiverID
	}
	if present.TransportParams && len(patch.TransportParams) > 0 {
		if len(patch.TransportParams) != len(merged.TransportParams) {
			// The leg COUNT is fixed by the endpoint: a 2022-7 sender
			// has two legs and a single-leg PATCH against it is a
			// controller error, not a reconfiguration.
			return is05.StagedSender{}, 400, fmt.Errorf(
				"transport_params has %d leg(s), endpoint has %d — the leg count is fixed by the endpoint",
				len(patch.TransportParams), len(merged.TransportParams))
		}
		for i, leg := range patch.TransportParams {
			for k, v := range leg {
				// The parameter must be one this endpoint publishes.
				//
				// IS-05 constraints carry additionalProperties:false,
				// so the published set is the WHOLE set -- a name
				// outside it is not an extension, it is a typo or a
				// controller aimed at different hardware. Merging it
				// anyway leaves the endpoint reporting a parameter no
				// schema describes, and the endpoint then fails its
				// own constraints on the next read. 400 at the door
				// (test_19/test_20).
				if i < len(e.constraints) {
					if _, known := e.constraints[i][k]; !known {
						return is05.StagedSender{}, 400, fmt.Errorf(
							"transport_params leg %d: %q is not a parameter of this endpoint", i, k)
					}
				}
				merged.TransportParams[i][k] = v
			}
		}
	}
	merged.Activation = patch.Activation

	mode := patch.Activation.Mode
	switch mode {
	case "":
		// Stage only.
		e.staged = merged
		return cloneStaged(e.staged), 200, nil

	case is05.ActivationModeImmediate:
		e.staged = merged
		s.promoteLocked(e)
		// The response reports the activation that was PERFORMED.
		//
		// Internally the staged activation block is cleared, which is
		// right — a controller reading staged later must see a clean
		// slate. But answering the PATCH with that cleared block tells
		// the caller "mode: ''" for an activation it just performed,
		// and the caller has no other way to learn the mode or the
		// time. test_25 through test_30 all fail on exactly that.
		out := cloneStaged(e.staged)
		out.Activation = e.active.Activation
		return out, 200, nil

	case is05.ActivationModeScheduledAbsolute, is05.ActivationModeScheduledRelative:
		when, err := s.scheduledTimeLocked(patch.Activation)
		if err != nil {
			return is05.StagedSender{}, 400, err
		}
		e.staged = merged
		e.scheduled = &when
		return cloneStaged(e.staged), 202, nil
	}
	return is05.StagedSender{}, 400, fmt.Errorf("unknown activation mode %q", mode)
}

// scheduledTimeLocked resolves the CLIENT's requested_time to a wall
// clock.
//
// The direction matters and is easy to invert: a controller sends
// `requested_time` — when it WANTS the switch — and the server answers
// with `activation_time`, when the switch actually happened or is
// scheduled for. Reading activation_time from the request would make
// every scheduled activation fail validation, since the field is the
// server's to fill.
func (s *connectionStore) scheduledTimeLocked(a is05.Activation) (time.Time, error) {
	if a.RequestedTime == nil || *a.RequestedTime == "" {
		return time.Time{}, fmt.Errorf("%s requires requested_time", a.Mode)
	}
	sec, nsec, ok := splitTAITime(*a.RequestedTime)
	if !ok {
		return time.Time{}, fmt.Errorf("requested_time %q is not <sec>:<nsec>", *a.RequestedTime)
	}
	if a.Mode == is05.ActivationModeScheduledRelative {
		return s.now().Add(time.Duration(sec)*time.Second + time.Duration(nsec)), nil
	}
	return time.Unix(sec, nsec), nil
}

// promoteLocked moves staged to active. Caller holds the lock.
func (s *connectionStore) promoteLocked(e *connectionEndpoint) {
	e.active = cloneStaged(e.staged)
	// ACTIVE describes what the device is DOING. "the device will
	// decide" is not something it can still be doing once activated,
	// so every "auto" is replaced with the value actually chosen.
	for i := range e.active.TransportParams {
		resolveAuto(e.active.TransportParams[i], s.nodeIP, e.isSender, i)
	}
	now := is05.FormatTAINow(s.now())
	// The ACTIVE block records the activation that produced it; the
	// STAGED block is reset. A controller reading staged after an
	// activation must see a clean slate, not the request it just sent.
	e.active.Activation = is05.Activation{
		Mode: e.staged.Activation.Mode,
		// What the controller asked for, echoed back, and when it
		// actually took effect. Both are needed: a scheduled switch is
		// audited on the gap between them.
		RequestedTime:  e.staged.Activation.RequestedTime,
		ActivationTime: &now,
	}
	e.staged.Activation = is05.Activation{}
	e.scheduled = nil
	if s.onPromote != nil {
		e.transportFile = s.onPromote(kindOf(e.isSender), e.id, e.active)
	}
}

// kindOf names the collection an endpoint belongs to, in the spelling
// the URL uses.
func kindOf(isSender bool) string {
	if isSender {
		return "senders"
	}
	return "receivers"
}

// runScheduled promotes any endpoint whose scheduled time has passed.
//
// Called on a tick by the server. Scheduled activation is what makes a
// coordinated multi-device switch possible: every device is staged,
// then all of them flip at one absolute TAI instant.
func (s *connectionStore) runScheduled() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	n := 0
	for _, m := range []map[string]*connectionEndpoint{s.senders, s.receivers} {
		for _, e := range m {
			if e.scheduled != nil && !e.scheduled.After(now) {
				s.promoteLocked(e)
				n++
			}
		}
	}
	return n
}

// splitTAITime parses "<sec>:<nsec>".
func splitTAITime(v string) (sec int64, nsec int64, ok bool) {
	i := strings.IndexByte(v, ':')
	if i < 0 {
		return 0, 0, false
	}
	sec, err := strconv.ParseInt(v[:i], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	nsec, err = strconv.ParseInt(v[i+1:], 10, 64)
	if err != nil || nsec < 0 || nsec > 999999999 {
		return 0, 0, false
	}
	return sec, nsec, true
}
