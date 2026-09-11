package provider

// What the Node does around the Connection API rather than inside it:
// the control hrefs it advertises, the version bumps that tell a
// controller something changed, the IS-11 constraints that reshape a
// Flow, and the scheduler that makes a 202 mean something.

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
	"dhs/internal/amwa/codec/is11"
	httpsession "dhs/internal/amwa/session/http"
)

// nodeFor builds a Node server without serving it, for the helpers
// that do not need a listener.
func nodeFor(t *testing.T, bundle *NodeConfig, tweak func(*IS04NodeConfig)) *IS04NodeServer {
	t.Helper()
	cfg := IS04NodeConfig{Bind: "127.0.0.1:0", DiscoveryMode: "static"}
	if tweak != nil {
		tweak(&cfg)
	}
	s, err := NewIS04NodeServer(newLogTap().logger(), bundle, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// A control href has to name an address a controller can reach. The
// operator's --advertise-host wins when it is an IP literal, because
// it cannot be poisoned by stale endpoints riding in from a bundle
// file — poisoning that is silent and expensive: every control href
// points at a dead address and the controller shows empty IS-05
// panels with no error anywhere.
func TestControlHostPrefersTheOperatorsAddress(t *testing.T) {
	b := fullBundle(t)

	withIP := nodeFor(t, b, func(c *IS04NodeConfig) {
		c.AdvertiseHost = "192.0.2.7"
		c.Bind = "0.0.0.0:8080"
	})
	if got := withIP.controlHost(); got != "192.0.2.7:8080" {
		t.Errorf("an IP advertise-host = %q, want it with the bound port", got)
	}

	withName := nodeFor(t, b, func(c *IS04NodeConfig) {
		c.AdvertiseHost = "node.local:8080"
	})
	if got := withName.controlHost(); got == "" {
		t.Error("a named advertise-host must still produce a host")
	}

	// With nothing advertised and no address in the bundle, the
	// fallback is a name that at least resolves on the box itself.
	bare := fullBundle(t)
	bare.Node.Interfaces = nil
	bare.Node.API.Endpoints = nil
	plain := nodeFor(t, bare, nil)
	if got := plain.controlHost(); got == "" {
		t.Error("controlHost must always name something")
	}
}

// The IS-05 minors a Node serves are what a controller reads to pick
// one; with the Connection API disabled there are none, and saying
// "none" is different from saying "v1.0".
func TestConnectionVersions(t *testing.T) {
	with := nodeFor(t, fullBundle(t), func(c *IS04NodeConfig) { c.ConnectionAPIVer = "v1.2" })
	if got := with.ConnectionVersions(); len(got) != 1 || got[0] != "v1.2" {
		t.Errorf("= %v, want the pinned minor", got)
	}

	without := nodeFor(t, fullBundle(t), func(c *IS04NodeConfig) { c.NoConnectionAPI = true })
	if got := without.ConnectionVersions(); got != nil {
		t.Errorf("= %v, want nothing", got)
	}
}

// An Active Constraints change stamps a fresh version on the Sender
// and queues it for re-registration: IS-04 §5 makes `version` how a
// controller learns anything changed, and a Sender whose version
// stands still through a constraints change tells every cached
// controller that nothing happened.
func TestActiveConstraintsBumpTheSenderVersion(t *testing.T) {
	b := fullBundle(t)
	s := nodeFor(t, b, nil)
	sid := s.bundle.Senders[0].ID
	before := s.bundle.Senders[0].Version

	s.bumpSenderVersion(sid)

	if s.bundle.Senders[0].Version == before {
		t.Error("the version must move")
	}

	// A sender that is not there changes nothing, and neither does a
	// Node with no bundle at all.
	s.bumpSenderVersion("not-a-sender")
	bare := nodeFor(t, fullBundle(t), nil)
	bare.bundle = nil
	bare.bumpSenderVersion(sid)
}

// IS-11 Active Constraints reshape the Flow a controller reads back:
// the suite sets a constraint and expects the flow's grain_rate to
// come back constrained, then clears it and expects the bundle's
// original.
func TestActiveConstraintsReshapeTheFlow(t *testing.T) {
	b := fullBundle(t)
	s := nodeFor(t, b, nil)
	sid := s.bundle.Senders[0].ID

	original := s.bundle.Flows[0].GrainRate
	s.adaptFlowToConstraints(sid, is11.ActiveConstraints{
		ConstraintSets: []is11.ConstraintSet{{
			"urn:x-nmos:cap:format:grain_rate": map[string]any{
				"enum": []any{map[string]any{"numerator": 50.0, "denominator": 1.0}},
			},
		}},
	})
	got := s.bundle.Flows[0].GrainRate
	if got == nil || got.Numerator != 50 {
		t.Fatalf("grain_rate = %+v, want the constrained value", got)
	}

	// An empty shape restores what the bundle declared.
	s.adaptFlowToConstraints(sid, is11.ActiveConstraints{})
	if restored := s.bundle.Flows[0].GrainRate; restored != original {
		t.Errorf("grain_rate = %+v, want the bundle's original %+v", restored, original)
	}

	// A constraint set the controller disabled contributes nothing.
	s.adaptFlowToConstraints(sid, is11.ActiveConstraints{
		ConstraintSets: []is11.ConstraintSet{{
			"urn:x-nmos:cap:meta:enabled": false,
			"urn:x-nmos:cap:format:grain_rate": map[string]any{
				"enum": []any{map[string]any{"numerator": 25.0}},
			},
		}},
	})
	if got := s.bundle.Flows[0].GrainRate; got != nil && got.Numerator == 25 {
		t.Error("a disabled constraint set was applied")
	}

	// A sender with no flow, and a Node with no bundle, change nothing.
	s.adaptFlowToConstraints("not-a-sender", is11.ActiveConstraints{})
	bare := nodeFor(t, fullBundle(t), nil)
	bare.bundle = nil
	bare.adaptFlowToConstraints(sid, is11.ActiveConstraints{})
}

// The constraint's own shape is mirrored back: a denominator the
// controller left out stays out, because the AMWA suite compares the
// flow's rational to the constraint value as a whole JSON object and
// an added "denominator": 1 reads as a mismatch.
func TestRationalEnumFirst(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   any
		want *is04.GrainRate
	}{
		{"not a constraint", "nonsense", nil},
		{"no enum", map[string]any{"minimum": 1.0}, nil},
		{"an empty enum", map[string]any{"enum": []any{}}, nil},
		{"an enum of the wrong shape", map[string]any{"enum": []any{"50/1"}}, nil},
		{"no numerator", map[string]any{"enum": []any{map[string]any{"denominator": 1.0}}}, nil},
		{"a numerator alone", map[string]any{"enum": []any{map[string]any{"numerator": 50.0}}},
			&is04.GrainRate{Numerator: 50}},
		{"both", map[string]any{"enum": []any{map[string]any{"numerator": 30000.0, "denominator": 1001.0}}},
			&is04.GrainRate{Numerator: 30000, Denominator: 1001}},
	} {
		got := rationalEnumFirst(tc.in)
		switch {
		case tc.want == nil && got != nil:
			t.Errorf("%s = %+v, want nothing", tc.name, got)
		case tc.want != nil && got == nil:
			t.Errorf("%s = nothing, want %+v", tc.name, tc.want)
		case tc.want != nil && *got != *tc.want:
			t.Errorf("%s = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// Without a scheduler an endpoint accepts a scheduled PATCH, answers
// 202, and then never acts — the worst of the three possible
// behaviours, because it looks correct to the controller right up
// until the switch does not happen. With neither API mounted there is
// nothing to pump and the scheduler returns rather than spinning.
func TestActivationSchedulerStopsWithNothingToPump(t *testing.T) {
	s := nodeFor(t, fullBundle(t), func(c *IS04NodeConfig) {
		c.NoConnectionAPI = true
		c.NoChannelMappingAPI = true
	})

	done := make(chan struct{})
	go func() { defer close(done); s.runActivationScheduler(context.Background(), 0) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the scheduler must return when there is nothing to pump")
	}
}

// With no port anywhere — no bind port, none advertised — the host
// stands alone: half an address is more use to an operator reading a
// control href than one with a made-up port on it.
func TestControlHostWithoutAPort(t *testing.T) {
	b := fullBundle(t)
	withIP := nodeFor(t, b, func(c *IS04NodeConfig) {
		c.AdvertiseHost = "192.0.2.7"
		c.Bind = "0.0.0.0" // no port
	})
	if got := withIP.controlHost(); got != "192.0.2.7" {
		t.Errorf("= %q, want the bare address", got)
	}

	// The same with the address coming from the bundle rather than the
	// command line.
	fromBundle := nodeFor(t, b, func(c *IS04NodeConfig) { c.Bind = "0.0.0.0" })
	if got := fromBundle.controlHost(); got == "" {
		t.Error("the bundle's own address must be usable")
	}
}

// A scheduled activation fires from the scheduler's own tick, and the
// operator is told it happened: a switch nobody logged is a switch
// nobody can account for afterwards.
func TestSchedulerFiresAScheduledActivation(t *testing.T) {
	tap := newLogTap()
	b := fullBundle(t)
	s, err := NewIS04NodeServer(tap.logger(), b, IS04NodeConfig{
		Bind: "127.0.0.1:0", DiscoveryMode: "static", ConnectionAPIVer: "v1.2",
	})
	if err != nil {
		t.Fatal(err)
	}

	when := "0:0" // due immediately
	if _, _, err := s.connection.Store().applyPatch("senders", b.Senders[0].ID,
		is05.StagedSender{
			MasterEnableField: is05.MasterEnableField{MasterEnable: true},
			Activation: is05.Activation{
				Mode: is05.ActivationModeScheduledAbsolute, RequestedTime: &when,
			},
		}, patchFields{MasterEnable: true}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.runActivationScheduler(ctx, time.Millisecond)

	tap.until(t, "scheduled activation fired")
}

// IS-08 queues scheduled re-maps the same way and rides the same
// ticker: two would drift against each other, and a controller that
// schedules a route and a channel map for the same instant expects
// them to land together.
func TestSchedulerFiresAScheduledChannelMap(t *testing.T) {
	tap := newLogTap()
	s, err := NewIS04NodeServer(tap.logger(), audioBundle(), IS04NodeConfig{
		Bind: "127.0.0.1:0", DiscoveryMode: "static",
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.channelMapping == nil {
		t.Fatal("the audio bundle mounts IS-08")
	}
	// One scheduled re-map, due immediately.
	io := deriveIO(audioBundle())
	var inID, outID string
	for id := range io.Inputs {
		inID = id
		break
	}
	for id := range io.Outputs {
		outID = id
		break
	}
	body := cmScheduled("activate_scheduled_absolute", "0:0", outID, cmRoute(0, inID, 0))
	req := httptest.NewRequest(stdhttp.MethodPost, cmBase+"/map/activations/", strings.NewReader(body))
	if code, out, err := s.channelMapping.handleActivationPost(req); err != nil || code != stdhttp.StatusAccepted {
		t.Fatalf("scheduling a re-map = %d (%+v) %v", code, out, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.runActivationScheduler(ctx, time.Millisecond)

	tap.until(t, "scheduled channel map fired")
}

// An API the operator opted out of is not attached, and attaching it
// anyway would advertise a control href to a face that answers
// nothing.
func TestDisabledAPIsAreNotAttached(t *testing.T) {
	s := nodeFor(t, audioBundle(), func(c *IS04NodeConfig) {
		c.NoChannelMappingAPI = true
		c.NoStreamCompatAPI = true
	})
	srv := httpsession.NewServer(newLogTap().logger())

	s.attachChannelMappingAPI(srv)
	s.attachStreamCompatAPI(srv)

	if s.channelMapping != nil {
		t.Error("--no-channelmapping must not build the server")
	}
	if s.streamCompat != nil {
		t.Error("--no-streamcompat must not build the server")
	}
}
