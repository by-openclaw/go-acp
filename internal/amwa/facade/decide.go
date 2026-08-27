package facade

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"dhs/internal/amwa/consumer"
)

// decide turns one question into an answer by driving the Controller.
//
// The tool words its questions for a human, but every question also
// carries machine-usable truth: the offered answers name resources by
// UUID, and action questions carry metadata naming the sender and
// receiver to act on. Strategy selection keys on the question type
// first and falls back to phrasing only where the tool encodes intent
// nowhere else (offline-selection, the two "press Next when you see
// it" monitors, connect-vs-disconnect).
func (s *Server) decide(ctx context.Context, q Question) (any, error) {
	switch q.TestType {
	case "action":
		return nil, s.performAction(ctx, q)
	case "multi_choice":
		return s.chooseMulti(ctx, q)
	case "single_choice":
		return s.chooseSingle(ctx, q)
	}
	return nil, fmt.Errorf("unknown test_type %q", q.TestType)
}

// ---- selection questions ----------------------------------------------

// chooseMulti answers "select all that apply" questions.
func (s *Server) chooseMulti(ctx context.Context, q Question) ([]string, error) {
	if len(q.Answers) == 0 {
		return []string{}, nil
	}
	snap, err := s.walk(ctx)
	if err != nil {
		return nil, err
	}
	var keep map[string]bool
	switch {
	case mentionsOffline(q.Question):
		// "select the ones which have been put offline" — the right
		// answers are the offered resources the registry NO LONGER has.
		keep = complementOf(snapshotIDs(snap), q.Answers)
	case mentionsConnectionAPI(q.Question):
		// IS-05-03 test_01: only receivers whose Device advertises an
		// IS-05 control are correct — the registry lists more, and a
		// controller that offers to route what it cannot route is
		// worse than one that hides it.
		keep = connectableReceivers(snap)
	default:
		keep = snapshotIDs(snap)
	}
	var out []string
	for _, a := range q.Answers {
		if a.Resource.ID != "" && keep[a.Resource.ID] {
			out = append(out, a.AnswerID)
		}
	}
	sort.Strings(out)
	if out == nil {
		out = []string{}
	}
	return out, nil
}

// chooseSingle answers "pick exactly one" questions.
func (s *Server) chooseSingle(ctx context.Context, q Question) (string, error) {
	snap, err := s.walk(ctx)
	if err != nil {
		return "", err
	}
	question := strings.ToLower(q.Question)

	// Metadata first. IS-05-03 test_04's follow-up names the receiver
	// whose connection the question is about — the tool provides the
	// id precisely so an automated facade does not have to parse
	// prose. The answer is THAT receiver's live subscription; scanning
	// every active receiver instead picks up connections left over
	// from earlier tests, which is exactly the wrong answer the first
	// run gave.
	if q.Metadata != nil && q.Metadata.Receiver != nil {
		for _, r := range snap.Receivers {
			if r.ID != q.Metadata.Receiver.ID {
				continue
			}
			if r.Subscription.SenderID == nil {
				return "", fmt.Errorf("receiver %s carries no subscription.sender_id in the registry", r.ID)
			}
			want := *r.Subscription.SenderID
			for _, a := range q.Answers {
				if a.Resource.ID == want {
					return a.AnswerID, nil
				}
			}
			return "", fmt.Errorf("receiver %s is subscribed to %s, which is not among the offered answers", r.ID, want)
		}
		return "", fmt.Errorf("receiver %s named in metadata is not in the registry", q.Metadata.Receiver.ID)
	}

	var keep map[string]bool
	switch {
	case mentionsOffline(q.Question):
		// "select the sender which has been put 'offline'".
		keep = complementOf(snapshotIDs(snap), q.Answers)
	case strings.Contains(question, "receiver") && strings.Contains(question, "connect"):
		// IS-05-03 test_04: "identify the receiver that has just been
		// connected". Several receivers can hold live subscriptions —
		// earlier tests in the same run leave connections behind — so
		// "just been" matters: the IS-04 version timestamp bumps on
		// every update, and the most recently touched active receiver
		// is the one the tool connected.
		keep = latestActiveReceiver(snap)
	case strings.Contains(question, "sender") && strings.Contains(question, "connect"):
		// Fallback for a sender question with no metadata: senders
		// referenced by any live subscription.
		keep = map[string]bool{}
		for _, r := range snap.Receivers {
			if r.Subscription.Active && r.Subscription.SenderID != nil {
				keep[*r.Subscription.SenderID] = true
			}
		}
	default:
		keep = snapshotIDs(snap)
	}

	for _, a := range q.Answers {
		if a.Resource.ID != "" && keep[a.Resource.ID] {
			return a.AnswerID, nil
		}
	}
	return "", fmt.Errorf("no offered answer matched the registry state")
}

// latestActiveReceiver returns the single receiver with a live
// subscription whose IS-04 version timestamp is newest — "the one that
// just changed". Falls back to every active receiver when versions are
// unparseable, which degrades to the old behaviour instead of failing.
func latestActiveReceiver(snap *consumer.CatalogueSnapshot) map[string]bool {
	bestID := ""
	var bestSec, bestNano int64 = -1, -1
	all := map[string]bool{}
	for _, r := range snap.Receivers {
		if !r.Subscription.Active || r.Subscription.SenderID == nil {
			continue
		}
		all[r.ID] = true
		sec, nano, ok := parseTAIVersion(r.Version)
		if !ok {
			continue
		}
		if sec > bestSec || (sec == bestSec && nano > bestNano) {
			bestSec, bestNano, bestID = sec, nano, r.ID
		}
	}
	if bestID != "" {
		return map[string]bool{bestID: true}
	}
	return all
}

// parseTAIVersion splits the IS-04 "<seconds>:<nanoseconds>" version.
func parseTAIVersion(v string) (int64, int64, bool) {
	sec, nano, ok := strings.Cut(v, ":")
	if !ok {
		return 0, 0, false
	}
	s, err1 := strconv.ParseInt(sec, 10, 64)
	n, err2 := strconv.ParseInt(nano, 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return s, n, true
}

// ---- action questions --------------------------------------------------

// performAction carries out an "action" question.
func (s *Server) performAction(ctx context.Context, q Question) error {
	question := strings.ToLower(q.Question)

	// Monitors FIRST, before the metadata branch. This ordering is a
	// scar: IS-05-03 test_04's monitor ("the connection ... will be
	// disconnected ... as soon as the NCuT detects it, press Next")
	// carries metadata naming the receiver, and treating metadata as
	// "act on this" made the facade disconnect the receiver ITSELF and
	// answer within the same second — scored as "Connection still
	// active", because the tool's own background deactivation had not
	// happened yet. A monitor's metadata says what to WATCH, not what
	// to do.
	if isMonitor(question) {
		switch {
		case strings.Contains(question, "online"):
			return s.awaitNewSender(ctx)
		default:
			var watch string
			if q.Metadata != nil && q.Metadata.Receiver != nil {
				watch = q.Metadata.Receiver.ID
			}
			return s.awaitDeactivation(ctx, watch)
		}
	}

	// Route/unroute actions carry metadata naming the resources.
	if q.Metadata != nil && q.Metadata.Receiver != nil {
		c, err := s.opts.Controller(ctx)
		if err != nil {
			return fmt.Errorf("build controller: %w", err)
		}
		req := consumer.ConnectRequest{ReceiverID: q.Metadata.Receiver.ID}
		// The SAME metadata shape means opposite things on IS-05-03
		// test_02 ("perform an 'immediate' activation") and test_03
		// ("remove the connection"). The verb lives only in the prose.
		if q.Metadata.Sender != nil && !wantsDisconnect(question) {
			req.SenderID = q.Metadata.Sender.ID
		}
		res, err := c.Connect(ctx, req)
		if err != nil {
			return fmt.Errorf("connect: %w", err)
		}
		s.logger.Info("nmos/facade: action done",
			"receiver", res.ReceiverID, "master_enable", res.MasterEnable,
			"endpoint", res.Endpoint)
		return nil
	}

	// "Browse and click Next" prompts — the browse IS the walk.
	_, err := s.walk(ctx)
	return err
}

// isMonitor spots the "press Next as soon as the NCuT detects ..."
// questions. Both monitor phrasings in the tool open with "as soon
// as"; the do-something prompts say "once the connection is active" /
// "once the connection has been removed" instead. Answering a monitor
// early fails the test explicitly — the tool checks the answer arrived
// AFTER its background state change.
func isMonitor(lowerQuestion string) bool {
	return strings.Contains(lowerQuestion, "as soon as")
}

// awaitNewSender blocks until a sender id appears that was absent when
// the question arrived. IS-04-04 test_05 re-registers the offline
// sender after a random delay of up to 60s and requires the answer
// within 30s of that.
func (s *Server) awaitNewSender(ctx context.Context) error {
	before, err := s.walk(ctx)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, x := range before.Senders {
		known[x.ID] = true
	}
	return s.pollUntil(ctx, 100*time.Second, func(snap *consumer.CatalogueSnapshot) bool {
		for _, x := range snap.Senders {
			if !known[x.ID] {
				s.logger.Info("nmos/facade: sender back online", "id", x.ID)
				return true
			}
		}
		return false
	})
}

// awaitDeactivation blocks until a receiver with a live subscription
// loses it. IS-05-03 test_04 deactivates one in the background within
// 60s and requires the answer within 30s of that.
//
// watch, when non-empty, is the receiver id the question's metadata
// named — the connection under test. Watching only that one matters:
// other receivers can hold subscriptions left over from earlier tests,
// and any of them going away would otherwise count as the event.
func (s *Server) awaitDeactivation(ctx context.Context, watch string) error {
	before, err := s.walk(ctx)
	if err != nil {
		return err
	}
	active := map[string]bool{}
	for _, r := range before.Receivers {
		if r.Subscription.Active && r.Subscription.SenderID != nil {
			if watch == "" || r.ID == watch {
				active[r.ID] = true
			}
		}
	}
	if len(active) == 0 {
		return nil // nothing to watch; acknowledge rather than hang
	}
	return s.pollUntil(ctx, 100*time.Second, func(snap *consumer.CatalogueSnapshot) bool {
		for _, r := range snap.Receivers {
			if active[r.ID] && (!r.Subscription.Active || r.Subscription.SenderID == nil) {
				s.logger.Info("nmos/facade: receiver deactivated", "id", r.ID)
				return true
			}
		}
		return false
	})
}

// pollUntil re-walks the registry every two seconds until cond holds.
// Two seconds keeps the detection latency well inside the 30-second
// answer windows without hammering the registry.
func (s *Server) pollUntil(ctx context.Context, max time.Duration, cond func(*consumer.CatalogueSnapshot) bool) error {
	deadline := time.Now().Add(max)
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("condition not observed within %s", max)
			}
			snap, err := s.walk(ctx)
			if err != nil {
				continue // transient registry hiccup: keep watching
			}
			if cond(snap) {
				return nil
			}
		}
	}
}

// ---- shared helpers ------------------------------------------------------

// walk builds a fresh Controller and reads the whole catalogue. Fresh
// per call on purpose: the tool re-registers resources between (and
// inside) tests, and a cached catalogue answers about a registry state
// that no longer exists.
func (s *Server) walk(ctx context.Context) (*consumer.CatalogueSnapshot, error) {
	c, err := s.opts.Controller(ctx)
	if err != nil {
		return nil, fmt.Errorf("build controller: %w", err)
	}
	snap, _ := c.Walk(ctx)

	// At v1.0 the REST collections are not enough: pagination arrived
	// in v1.1, so a v1.0 Query API with more resources than its page
	// size has no REST way to serve the rest. The WebSocket
	// subscription's SYNC snapshot is the enumeration mechanism v1.0
	// actually specifies — use it for the two collections the
	// Controller suites score.
	if c.Codec().APIVer() == "v1.0" {
		if senders, err := c.SendersViaSubscription(ctx); err == nil && len(senders) >= len(snap.Senders) {
			snap.Senders = senders
		}
		if receivers, err := c.ReceiversViaSubscription(ctx); err == nil && len(receivers) >= len(snap.Receivers) {
			snap.Receivers = receivers
		}
	}
	return snap, nil
}

// snapshotIDs collects every resource UUID visible in the catalogue.
// Matching is by UUID only: display strings are prose built from
// label+description and would mismatch on a rename; the id is the only
// stable identifier NMOS has.
func snapshotIDs(snap *consumer.CatalogueSnapshot) map[string]bool {
	seen := map[string]bool{}
	for _, x := range snap.Nodes {
		seen[x.ID] = true
	}
	for _, x := range snap.Devices {
		seen[x.ID] = true
	}
	for _, x := range snap.Sources {
		seen[x.ID] = true
	}
	for _, x := range snap.Flows {
		seen[x.ID] = true
	}
	for _, x := range snap.Senders {
		seen[x.ID] = true
	}
	for _, x := range snap.Receivers {
		seen[x.ID] = true
	}
	return seen
}

// complementOf returns the offered resource ids NOT present in the
// registry — the answer set for every "which one disappeared" question.
func complementOf(present map[string]bool, offered []Answer) map[string]bool {
	out := map[string]bool{}
	for _, a := range offered {
		if a.Resource.ID != "" && !present[a.Resource.ID] {
			out[a.Resource.ID] = true
		}
	}
	return out
}

// connectableReceivers returns the receiver ids whose owning Device
// advertises an IS-05 control endpoint.
func connectableReceivers(snap *consumer.CatalogueSnapshot) map[string]bool {
	withIS05 := map[string]bool{}
	for _, d := range snap.Devices {
		for _, ctl := range d.Controls {
			if strings.HasPrefix(ctl.Type, "urn:x-nmos:control:sr-ctrl/") {
				withIS05[d.ID] = true
				break
			}
		}
	}
	out := map[string]bool{}
	for _, r := range snap.Receivers {
		if withIS05[r.DeviceID] {
			out[r.ID] = true
		}
	}
	return out
}

func mentionsOffline(q string) bool {
	return strings.Contains(strings.ToLower(q), "offline")
}

func mentionsConnectionAPI(q string) bool {
	return strings.Contains(strings.ToLower(q), "connection api")
}

// wantsDisconnect spots the removal phrasings the IS-05-03 suite uses.
func wantsDisconnect(lowerQuestion string) bool {
	return strings.Contains(lowerQuestion, "remov") ||
		strings.Contains(lowerQuestion, "disconnect") ||
		strings.Contains(lowerQuestion, "deactivat")
}
