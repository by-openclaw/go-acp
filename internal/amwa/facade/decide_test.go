package facade

// The question semantics: what answer a given question + plant state
// produces. Every case is phrased the way the AMWA tool phrases it
// (ControllerTest.py / IS-05-03 / IS-04-04 / BCP-006-01-02 /
// BCP-007-03-02) and asserted against the registry state stdPlant
// serves, never against the code's own choice.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
	v10 "dhs/internal/amwa/codec/is04/v10"
	"dhs/internal/amwa/consumer"
)

func ids(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDecideDispatchesOnTestType(t *testing.T) {
	p := stdPlant(t)
	s, _ := facadeFor(t, p.controller("v1.3"))
	ctx := context.Background()

	got, err := s.decide(ctx, Question{TestType: "single_choice", Question: "select the node", Answers: answers(nodeID)})
	if err != nil || got != "ans-a" {
		t.Errorf("single_choice = %v, %v; want ans-a", got, err)
	}
	got, err = s.decide(ctx, Question{TestType: "multi_choice", Question: "select the nodes", Answers: answers(nodeID, ghostID)})
	if list, ok := got.([]string); err != nil || !ok || !equalStrings(list, []string{"ans-a"}) {
		t.Errorf("multi_choice = %v, %v; want [ans-a]", got, err)
	}
	// A browse-and-click-Next action: the browse is the walk.
	got, err = s.decide(ctx, Question{TestType: "action", Question: "browse the senders, then click Next"})
	if err != nil || got != nil {
		t.Errorf("action = %v, %v; want nil, nil", got, err)
	}
	if _, err = s.decide(ctx, Question{TestType: "free_text"}); err == nil {
		t.Error("an unknown test_type must be an error, not a guess")
	}
}

func TestChooseSingle(t *testing.T) {
	meta := func(r string) *Metadata { return &Metadata{Receiver: &Resource{ID: r}} }
	cases := []struct {
		name    string
		q       Question
		want    string
		wantErr string
	}{
		{"metadata receiver answers with its live subscription's sender",
			Question{Metadata: meta(rcvAID), Question: "identify the sender", Answers: answers(sndBID, sndAID)}, "ans-b", ""},
		{"metadata receiver with no subscription cannot be answered",
			Question{Metadata: meta(rcvBID), Answers: answers(sndAID)}, "", "carries no subscription"},
		{"metadata receiver subscribed to an unoffered sender cannot be answered",
			Question{Metadata: meta(rcvAID), Answers: answers(sndBID)}, "", "not among the offered answers"},
		{"metadata receiver absent from the registry cannot be answered",
			Question{Metadata: meta(ghostID), Answers: answers(sndAID)}, "", "not in the registry"},
		{"the offline sender is the offered one the registry no longer lists",
			Question{Question: "select the sender which has been put 'offline'", Answers: answers(sndAID, ghostID)}, "ans-b", ""},
		{"the just-connected receiver is the newest active one, not the newest",
			Question{Question: "identify the receiver that has just been connected", Answers: answers(rcvBID, rcvCID, rcvAID)}, "ans-c", ""},
		{"a connected sender is one a live subscription references",
			Question{Question: "select the sender that has just been connected", Answers: answers(ghostID, sndBID)}, "ans-b", ""},
		{"otherwise any registered resource is a valid pick",
			Question{Question: "select a resource you can see", Answers: answers(ghostID, srcID)}, "ans-b", ""},
		{"nothing offered matching the registry is an error, not a guess",
			Question{Question: "select a resource you can see", Answers: answers(ghostID)}, "", "no offered answer matched"},
	}
	p := stdPlant(t)
	s, _ := facadeFor(t, p.controller("v1.3"))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.q.TestType = "single_choice"
			got, err := s.chooseSingle(context.Background(), tc.q)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want one mentioning %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("answer = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	t.Run("an unreachable registry is an error", func(t *testing.T) {
		s, _ := facadeFor(t, failingFactory)
		if _, err := s.chooseSingle(context.Background(), Question{Answers: answers(nodeID)}); err == nil {
			t.Fatal("chooseSingle without a registry must fail")
		}
	})
}

func TestChooseMulti(t *testing.T) {
	compatible := "select the Receivers that are compatible with the following Sender:\n\n"
	cases := []struct {
		name string
		q    Question
		want []string
	}{
		{"no answers offered selects nothing", Question{Question: "select"}, []string{}},
		{"offline resources are the offered ones the registry no longer lists",
			Question{Question: "select the Senders which have been put offline", Answers: answers(sndAID, ghostID, sndBID)}, []string{"ans-b"}},
		{"an MXL sender in metadata selects the MXL receivers whose caps its flow satisfies",
			Question{Metadata: &Metadata{Sender: &Resource{ID: sndBID}}, Question: "select the Receivers compatible with the MXL Sender",
				Answers: answers(rcvAID, rcvBID, rcvCID)}, []string{"ans-c"}},
		{"a TR-08 sender named by UUID selects the receivers whose constraint sets match its SDP",
			Question{Question: compatible + "snd (Mock Sender, " + sndAID + ")", Answers: answers(rcvAID, rcvBID, rcvCID)}, []string{"ans-a"}},
		{"a compatibility question naming no resolvable counterpart answers unable to identify",
			Question{Question: compatible + "snd (Mock Sender, " + ghostID + ")", Answers: answers(rcvAID)}, []string{}},
		{"Connection API receivers are those whose Device advertises IS-05",
			Question{Question: "select the Receivers that can be controlled via the Connection API", Answers: answers(rcvAID, rcvBID, rcvCID)}, []string{"ans-a", "ans-b"}},
		{"JPEG XS senders transmit a video/jxsv flow",
			Question{Question: "select the JPEG XS capable Senders", Answers: answers(sndAID, sndBID)}, []string{"ans-a"}},
		{"JPEG XS receivers list video/jxsv in caps.media_types",
			Question{Question: "select the JPEG XS capable Receivers", Answers: answers(rcvAID, rcvBID, rcvCID)}, []string{"ans-a"}},
		{"MXL senders ride the MXL transport",
			Question{Question: "select the MXL Senders", Answers: answers(sndAID, sndBID)}, []string{"ans-b"}},
		{"MXL receivers ride the MXL transport",
			Question{Question: "select the MXL Receivers", Answers: answers(rcvAID, rcvBID, rcvCID)}, []string{"ans-c"}},
		{"otherwise every registered resource offered is selected, in answer order",
			Question{Question: "select all the resources you can see", Answers: answers(rcvCID, ghostID, nodeID, dev1ID, srcID, flowJID, sndAID)},
			[]string{"ans-a", "ans-c", "ans-d", "ans-e", "ans-f", "ans-g"}},
	}
	p := stdPlant(t)
	s, _ := facadeFor(t, p.controller("v1.3"))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.q.TestType = "multi_choice"
			got, err := s.chooseMulti(context.Background(), tc.q)
			if err != nil {
				t.Fatalf("chooseMulti: %v", err)
			}
			if got == nil || !equalStrings(got, tc.want) {
				t.Errorf("answer = %v, want %v", got, tc.want)
			}
		})
	}
	t.Run("an unreachable registry is an error", func(t *testing.T) {
		s, _ := facadeFor(t, failingFactory)
		if _, err := s.chooseMulti(context.Background(), Question{Answers: answers(nodeID)}); err == nil {
			t.Fatal("chooseMulti without a registry must fail")
		}
	})
}

// TestPerformActionRoutes: the same metadata shape is a connect on
// IS-05-03 test_02 and a disconnect on test_03; only the prose says
// which, and the PATCH the Device receives is the proof.
func TestPerformActionRoutes(t *testing.T) {
	meta := &Metadata{Sender: &Resource{ID: sndAID}, Receiver: &Resource{ID: rcvBID}}
	cases := []struct {
		name       string
		question   string
		wantSender any
		wantMaster bool
		wantSDP    bool
	}{
		{"an immediate activation stages the sender with master_enable and its transport file",
			"Perform an 'immediate' activation between sender and receiver, then click Next once the connection is active",
			sndAID, true, true},
		{"a removal stages sender_id null with master_enable false",
			"Remove the connection between sender and receiver, then click Next once the connection has been removed",
			nil, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := stdPlant(t)
			s, logs := facadeFor(t, p.controller("v1.3"))
			err := s.performAction(context.Background(), Question{TestType: "action", Question: tc.question, Metadata: meta})
			if err != nil {
				t.Fatalf("performAction: %v", err)
			}
			select {
			case call := <-p.patches:
				if call.receiverID != rcvBID {
					t.Errorf("PATCHed receiver %s, want %s", call.receiverID, rcvBID)
				}
				if call.body["sender_id"] != tc.wantSender || call.body["master_enable"] != tc.wantMaster {
					t.Errorf("staged %v, want sender_id=%v master_enable=%v", call.body, tc.wantSender, tc.wantMaster)
				}
				if _, has := call.body["transport_file"]; has != tc.wantSDP {
					t.Errorf("transport_file staged = %v, want %v", has, tc.wantSDP)
				}
			default:
				t.Fatal("the Device received no PATCH")
			}
			if !strings.Contains(logs.String(), "action done") {
				t.Errorf("log lacks the action receipt:\n%s", logs.String())
			}
		})
	}
	t.Run("a Device that refuses the stage fails the action", func(t *testing.T) {
		p := stdPlant(t)
		p.patchStatus = http.StatusInternalServerError
		s, _ := facadeFor(t, p.controller("v1.3"))
		err := s.performAction(context.Background(), Question{TestType: "action", Question: "perform an immediate activation", Metadata: meta})
		if err == nil || !strings.Contains(err.Error(), "connect:") {
			t.Errorf("err = %v, want the connect failure", err)
		}
	})
	t.Run("an unreachable registry fails the action", func(t *testing.T) {
		s, _ := facadeFor(t, failingFactory)
		err := s.performAction(context.Background(), Question{TestType: "action", Question: "perform an immediate activation", Metadata: meta})
		if err == nil || !strings.Contains(err.Error(), "build controller") {
			t.Errorf("err = %v, want the controller failure", err)
		}
	})
	t.Run("a browse prompt with no metadata needs a reachable registry", func(t *testing.T) {
		s, _ := facadeFor(t, failingFactory)
		if err := s.performAction(context.Background(), Question{TestType: "action", Question: "browse and click Next"}); err == nil {
			t.Error("the browse walk must surface a registry failure")
		}
	})
}

// TestMonitorQuestions: "as soon as the NCuT detects ..., press Next"
// questions answer only AFTER the tool's background change is visible
// in the registry — never by acting themselves.
func TestMonitorQuestions(t *testing.T) {
	online := "The sender will be put back online in a moment. As soon as the NCuT detects it, press Next"
	removed := "The connection will be disconnected in the background. As soon as the NCuT detects it, press Next"

	t.Run("a sender coming back online is a sender id absent at the question", func(t *testing.T) {
		shortMonitors(t, 5, 5000)
		p := stdPlant(t)
		p.onList = func(plural string, n int) {
			if plural == "senders" && n == 2 {
				p.senders = append(p.senders, senderOn(sndCID, dev1ID, is04.TransportRTPMcast, nil, nil))
			}
		}
		s, logs := facadeFor(t, p.controller("v1.3"))
		if err := s.performAction(context.Background(), Question{TestType: "action", Question: online}); err != nil {
			t.Fatalf("performAction: %v", err)
		}
		if !strings.Contains(logs.String(), sndCID) {
			t.Errorf("log does not name the returned sender:\n%s", logs.String())
		}
		if p.listCount("senders") < 2 {
			t.Error("the monitor answered without re-walking the registry")
		}
		// The Device was never driven: a monitor watches, it does not act.
		select {
		case call := <-p.patches:
			t.Errorf("monitor PATCHed the Device: %v", call)
		default:
		}
	})
	t.Run("a disconnection is the watched receiver losing its subscription", func(t *testing.T) {
		shortMonitors(t, 5, 5000)
		p := stdPlant(t)
		// Still connected on the first poll, gone on the second: the
		// monitor must keep watching, not answer on "no change".
		p.onList = func(plural string, n int) {
			if plural == "receivers" && n == 3 {
				p.receivers[0].Subscription = is04.ReceiverSubscription{}
			}
		}
		s, logs := facadeFor(t, p.controller("v1.3"))
		q := Question{TestType: "action", Question: removed, Metadata: &Metadata{Receiver: &Resource{ID: rcvAID}}}
		if err := s.performAction(context.Background(), q); err != nil {
			t.Fatalf("performAction: %v", err)
		}
		if !strings.Contains(logs.String(), "receiver deactivated") {
			t.Errorf("log lacks the deactivation receipt:\n%s", logs.String())
		}
		select {
		case call := <-p.patches:
			t.Errorf("monitor PATCHed the Device itself: %v", call)
		default:
		}
	})
	t.Run("without metadata any active receiver going idle counts", func(t *testing.T) {
		shortMonitors(t, 5, 5000)
		p := stdPlant(t)
		p.onList = func(plural string, n int) {
			if plural == "receivers" && n == 2 {
				p.receivers[2].Subscription = is04.ReceiverSubscription{}
			}
		}
		s, _ := facadeFor(t, p.controller("v1.3"))
		if err := s.performAction(context.Background(), Question{TestType: "action", Question: removed}); err != nil {
			t.Fatalf("performAction: %v", err)
		}
	})
	t.Run("a watched receiver with nothing to lose is acknowledged at once", func(t *testing.T) {
		shortMonitors(t, 5, 5000)
		p := stdPlant(t)
		s, _ := facadeFor(t, p.controller("v1.3"))
		q := Question{TestType: "action", Question: removed, Metadata: &Metadata{Receiver: &Resource{ID: rcvBID}}}
		if err := s.performAction(context.Background(), q); err != nil {
			t.Fatalf("performAction: %v", err)
		}
		if n := p.listCount("receivers"); n != 1 {
			t.Errorf("registry walked %d times, want exactly the one that found nothing to watch", n)
		}
	})
	t.Run("a change that never comes is reported after the window", func(t *testing.T) {
		shortMonitors(t, 5, 40)
		p := stdPlant(t)
		s, _ := facadeFor(t, p.controller("v1.3"))
		err := s.performAction(context.Background(), Question{TestType: "action", Question: online})
		if err == nil || !strings.Contains(err.Error(), "not observed within") {
			t.Errorf("err = %v, want the window timeout", err)
		}
	})
	t.Run("an unreachable registry fails both monitors", func(t *testing.T) {
		s, _ := facadeFor(t, failingFactory)
		for _, q := range []string{online, removed} {
			if err := s.performAction(context.Background(), Question{TestType: "action", Question: q}); err == nil {
				t.Errorf("%q without a registry must fail", q)
			}
		}
	})
}

// TestPollUntil pins the watch loop on its own: the caller's context
// ends it, and a registry hiccup between polls is tolerated rather than
// reported as the event not happening.
func TestPollUntil(t *testing.T) {
	never := func(*consumer.CatalogueSnapshot) bool { return false }

	t.Run("the caller's context ends the watch", func(t *testing.T) {
		shortMonitors(t, 1000, 5000)
		p := stdPlant(t)
		s, _ := facadeFor(t, p.controller("v1.3"))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := s.pollUntil(ctx, monitorWindow, never); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	})
	t.Run("a transient registry failure keeps the watch alive", func(t *testing.T) {
		shortMonitors(t, 5, 5000)
		p := stdPlant(t)
		calls := 0
		flaky := func(ctx context.Context) (*consumer.Controller, error) {
			calls++
			if calls == 1 {
				return nil, io.ErrUnexpectedEOF
			}
			return p.controller("v1.3")(ctx)
		}
		s, _ := facadeFor(t, flaky)
		seen := func(snap *consumer.CatalogueSnapshot) bool { return len(snap.Senders) == 2 }
		if err := s.pollUntil(context.Background(), monitorWindow, seen); err != nil {
			t.Fatalf("pollUntil: %v", err)
		}
		if calls < 2 {
			t.Errorf("registry built %d times, want the failed one plus a retry", calls)
		}
	})
	t.Run("a registry that stays down runs the window out", func(t *testing.T) {
		shortMonitors(t, 5, 40)
		s, _ := facadeFor(t, failingFactory)
		err := s.pollUntil(context.Background(), monitorWindow, never)
		if err == nil || !strings.Contains(err.Error(), "not observed within") {
			t.Errorf("err = %v, want the window timeout", err)
		}
	})
}

// TestWalkAtV10 pins the v1.0 enumeration rule: the REST collections
// cannot page, so the subscription's SYNC snapshot is the listing when
// it is at least as complete.
func TestWalkAtV10(t *testing.T) {
	p := newPlant(t, v10.New())
	p.devices = []is04.Device{deviceWith(dev1ID, "")}
	// v1.0.3 types flow_id and manifest_href as non-nullable strings.
	v10Sender := func(id string) is04.Sender {
		return senderOn(id, dev1ID, is04.TransportRTPMcast, strPtr(flowJID), strPtr("http://10.0.0.1/"+id+".sdp"))
	}
	p.senders = []is04.Sender{v10Sender(sndAID)}
	p.receivers = []is04.Receiver{receiverOn(rcvAID, "1:0", dev1ID, is04.TransportRTPMcast, is04.ReceiverCaps{}, nil)}
	// The subscription sees the whole plant; REST saw one page of it.
	p.subscriptionSenders = append(p.senders, v10Sender(sndBID))
	p.subscriptionReceivers = append(p.receivers, receiverOn(rcvBID, "1:0", dev1ID, is04.TransportRTPMcast, is04.ReceiverCaps{}, nil))

	s, _ := facadeFor(t, p.controller("v1.0"))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	snap, err := s.walk(ctx)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(snap.Senders) != 2 || len(snap.Receivers) != 2 {
		t.Errorf("walk saw %d senders / %d receivers, want the subscription's 2 / 2", len(snap.Senders), len(snap.Receivers))
	}
	if got := ids(snapshotIDs(snap)); len(got) != 5 {
		t.Errorf("snapshotIDs = %v, want the device and both pairs", got)
	}
}

// --- pure helpers, from synthetic snapshots ----------------------------

func TestLatestActiveReceiver(t *testing.T) {
	active := func(id, version string) is04.Receiver {
		return is04.Receiver{ResourceCore: is04.ResourceCore{ID: id, Version: version},
			Subscription: is04.ReceiverSubscription{SenderID: strPtr(sndAID), Active: true}}
	}
	idle := is04.Receiver{ResourceCore: is04.ResourceCore{ID: "idle", Version: "999:0"}}
	cases := []struct {
		name string
		rcvs []is04.Receiver
		want []string
	}{
		{"the newest active version wins, nanoseconds breaking ties",
			[]is04.Receiver{idle, active("old", "10:5"), active("new", "10:9"), active("older", "9:999")}, []string{"new"}},
		{"unparseable versions fall back to every active receiver",
			[]is04.Receiver{active("x", "not-a-version"), active("y", ""), idle}, []string{"x", "y"}},
		{"no active receiver selects nothing", []is04.Receiver{idle}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := latestActiveReceiver(&consumer.CatalogueSnapshot{Receivers: tc.rcvs})
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for _, id := range tc.want {
				if !got[id] {
					t.Errorf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestParseTAIVersion(t *testing.T) {
	cases := []struct {
		name, in  string
		sec, nano int64
		ok        bool
	}{
		{"seconds and nanoseconds", "1700000000:123", 1700000000, 123, true},
		{"no colon", "1700000000", 0, 0, false},
		{"non-numeric seconds", "x:1", 0, 0, false},
		{"non-numeric nanoseconds", "1:y", 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sec, nano, ok := parseTAIVersion(tc.in)
			if sec != tc.sec || nano != tc.nano || ok != tc.ok {
				t.Errorf("parseTAIVersion(%q) = %d, %d, %v; want %d, %d, %v", tc.in, sec, nano, ok, tc.sec, tc.nano, tc.ok)
			}
		})
	}
}

func TestSnapshotHelpers(t *testing.T) {
	snap := &consumer.CatalogueSnapshot{
		Nodes: []is04.Node{{ResourceCore: is04.ResourceCore{ID: "n"}}},
		Devices: []is04.Device{{ResourceCore: is04.ResourceCore{ID: "d-is05"}, Controls: []is04.DeviceControl{{Type: "urn:x-nmos:control:sr-ctrl/v1.1", Href: "http://d/x-nmos/connection/v1.1"}}},
			{ResourceCore: is04.ResourceCore{ID: "d-plain"}, Controls: []is04.DeviceControl{{Type: "urn:x-nmos:control:manifest-base/v1.0", Href: "http://d/"}}}},
		Sources:   []is04.Source{{ResourceCore: is04.ResourceCore{ID: "s"}}},
		Flows:     []is04.Flow{{ResourceCore: is04.ResourceCore{ID: "f"}}},
		Senders:   []is04.Sender{{ResourceCore: is04.ResourceCore{ID: "snd"}}},
		Receivers: []is04.Receiver{{ResourceCore: is04.ResourceCore{ID: "r1"}, DeviceID: "d-is05"}, {ResourceCore: is04.ResourceCore{ID: "r2"}, DeviceID: "d-plain"}},
	}
	t.Run("snapshotIDs sees every kind", func(t *testing.T) {
		got := snapshotIDs(snap)
		for _, id := range []string{"n", "d-is05", "d-plain", "s", "f", "snd", "r1", "r2"} {
			if !got[id] {
				t.Errorf("snapshotIDs lacks %s: %v", id, got)
			}
		}
		if len(got) != 8 {
			t.Errorf("snapshotIDs = %v, want exactly 8", got)
		}
	})
	t.Run("complementOf is the offered ids the registry lacks", func(t *testing.T) {
		got := complementOf(snapshotIDs(snap), append(answers("snd", "gone", "also-gone"), Answer{AnswerID: "blank"}))
		if len(got) != 2 || !got["gone"] || !got["also-gone"] {
			t.Errorf("complementOf = %v, want gone + also-gone (a blank resource is never an answer)", got)
		}
	})
	t.Run("connectableReceivers follow the owning Device's sr-ctrl control", func(t *testing.T) {
		got := connectableReceivers(snap)
		if len(got) != 1 || !got["r1"] {
			t.Errorf("connectableReceivers = %v, want exactly r1", got)
		}
	})
}

func TestPhrasingHelpers(t *testing.T) {
	cases := []struct {
		name string
		fn   func(string) bool
		in   string
		want bool
	}{
		{"offline anywhere in the prose", mentionsOffline, "select the Sender which has been put 'OFFLINE'", true},
		{"no offline", mentionsOffline, "select the sender", false},
		{"Connection API by name", mentionsConnectionAPI, "receivers reachable via the Connection API", true},
		{"no Connection API", mentionsConnectionAPI, "receivers you can see", false},
		{"monitors open with as soon as", isMonitor, "as soon as the ncut detects it, press next", true},
		{"a do-something prompt is not a monitor", isMonitor, "once the connection is active, press next", false},
		{"remove is a disconnect", wantsDisconnect, "remove the connection", true},
		{"disconnect is a disconnect", wantsDisconnect, "disconnect the receiver", true},
		{"deactivate is a disconnect", wantsDisconnect, "deactivate the receiver", true},
		{"activation is not a disconnect", wantsDisconnect, "perform an immediate activation", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.fn(tc.in); got != tc.want {
				t.Errorf("%q -> %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
