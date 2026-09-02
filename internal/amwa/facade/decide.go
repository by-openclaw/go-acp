package facade

import (
	"context"
	"fmt"
	"io"
	stdhttp "net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"dhs/internal/amwa/codec/is04"
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
	question := strings.ToLower(q.Question)
	var keep map[string]bool
	switch {
	case mentionsOffline(q.Question):
		// "select the ones which have been put offline" — the right
		// answers are the offered resources the registry NO LONGER has.
		keep = complementOf(snapshotIDs(snap), q.Answers)
	case q.Metadata != nil && q.Metadata.Sender != nil:
		// BCP-007-03-02 test_04: "select the Receivers that are
		// compatible with the following MXL Sender" — the sender rides
		// in metadata, and compatibility is the suite's published rule
		// (both ends MXL, flow media_type in caps.media_types, any
		// BCP-004-01 constraint set satisfied by the flow's fields).
		keep = compatibleReceiversForSender(snap, q.Metadata.Sender.ID)
	case strings.Contains(question, "compatible"):
		// BCP-006-01-02 test_03/test_04: TR-08 compatibility with a
		// counterpart carried in prose, not metadata. The prose IS
		// machine-usable by UUID: the tool's display_answer format is
		// "label (description, <uuid>)" (ControllerTest.py
		// _format_device_metadata), so the counterpart's id — the one
		// stable identifier NMOS has — is extracted with a UUID
		// pattern, never by label matching. Compatibility is judged
		// the way a real BCP-006-01 controller judges it: fetch the
		// sender's transport file and match the receiver's BCP-004-01
		// constraint sets against the SDP's parameters (see
		// tr08CompatibleFromProse for why the registry alone cannot
		// express the rule).
		keep = tr08CompatibleFromProse(ctx, snap, q.Question)
		if keep == nil {
			s.logger.Warn("nmos/facade: compatibility question names no resolvable counterpart (or its SDP is unreadable) — answering 'unable to identify'",
				"question_id", q.QuestionID)
			keep = map[string]bool{}
		}
	case mentionsConnectionAPI(q.Question):
		// IS-05-03 test_01: only receivers whose Device advertises an
		// IS-05 control are correct — the registry lists more, and a
		// controller that offers to route what it cannot route is
		// worse than one that hides it.
		keep = connectableReceivers(snap)
	case strings.Contains(question, "jpeg xs") && strings.Contains(question, "senders"):
		// BCP-006-01-02 test_01: a Sender is JPEG XS capable when the
		// Flow it transmits carries media_type video/jxsv (BCP-006-01
		// puts the coding on the Flow, not the Sender).
		keep = jxsvCapableSenders(snap)
	case strings.Contains(question, "jpeg xs") && strings.Contains(question, "receivers"):
		// BCP-006-01-02 test_02: a Receiver is JPEG XS capable when
		// video/jxsv is a member of caps.media_types.
		keep = jxsvCapableReceivers(snap)
	case strings.Contains(question, "mxl senders"):
		// BCP-007-03-02 test_01: transport urn:x-nmos:transport:mxl.
		keep = mxlSenders(snap)
	case strings.Contains(question, "mxl receivers"):
		// BCP-007-03-02 test_02.
		keep = mxlReceivers(snap)
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

// ---- BCP-006-01-02 / BCP-007-03-02 capability selection -----------------
//
// The four helpers below mirror the published rules the two Controller
// suites score, driven from the registry snapshot alone.

// jxsvCapableSenders — a Sender is JPEG XS capable when the Flow it
// transmits carries media_type video/jxsv.
func jxsvCapableSenders(snap *consumer.CatalogueSnapshot) map[string]bool {
	flowMediaType := map[string]string{}
	for _, f := range snap.Flows {
		flowMediaType[f.ID] = f.MediaType
	}
	out := map[string]bool{}
	for _, s := range snap.Senders {
		if s.FlowID != nil && flowMediaType[*s.FlowID] == "video/jxsv" {
			out[s.ID] = true
		}
	}
	return out
}

// jxsvCapableReceivers — video/jxsv is a member of caps.media_types.
func jxsvCapableReceivers(snap *consumer.CatalogueSnapshot) map[string]bool {
	out := map[string]bool{}
	for _, r := range snap.Receivers {
		for _, mt := range r.Caps.MediaTypes {
			if mt == "video/jxsv" {
				out[r.ID] = true
				break
			}
		}
	}
	return out
}

// isMXLTransport matches urn:x-nmos:transport:mxl (and any dotted
// subclassification, the way RTP variants subclass their base URN).
func isMXLTransport(t string) bool {
	return t == is04.TransportMXL || strings.HasPrefix(t, is04.TransportMXL+".")
}

// mxlSenders / mxlReceivers — BCP-007-03 discovery: transport is the
// MXL URN.
func mxlSenders(snap *consumer.CatalogueSnapshot) map[string]bool {
	out := map[string]bool{}
	for _, s := range snap.Senders {
		if isMXLTransport(s.Transport) {
			out[s.ID] = true
		}
	}
	return out
}

func mxlReceivers(snap *consumer.CatalogueSnapshot) map[string]bool {
	out := map[string]bool{}
	for _, r := range snap.Receivers {
		if isMXLTransport(r.Transport) {
			out[r.ID] = true
		}
	}
	return out
}

// uuidRE matches an RFC 4122 UUID — the id embedded in the tool's
// display_answer prose ("label (description, <uuid>)").
var uuidRE = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// tr08CompatibleFromProse answers the BCP-006-01-02 TR-08
// compatibility questions. The question embeds the counterpart's
// UUID; whichever side it resolves to in the registry decides the
// direction — a sender selects compatible receivers (test_03), a
// receiver selects compatible senders (test_04). Returns nil when no
// embedded UUID resolves (or the named sender's SDP is unreadable),
// so the caller can log and take the question's own "unable to
// identify" branch.
//
// Why the SENDER'S SDP, not its registered Flow: TR-08 capability
// sets differ by color sampling and depth, and IS-04 registers
// neither — the tool's own fixture book has Set A/B and Set C interop
// points that are byte-identical in every registered flow field
// (same geometry, framerate, profile High444.12, level, colorimetry,
// TCS, even the same derived bit_rate) and differ ONLY in the SDP's
// sampling=YCbCr-4:2:2 vs 4:4:4. The first #954 receipts are that
// ambiguity scored ("Receivers incorrectly identified as compatible").
// So the facade does what BCP-006-01 says a controller does: fetch
// the transport file from the sender's manifest_href and match the
// receiver's BCP-004-01 constraint sets against SDP-derived
// parameters. That reproduces the tool's capability_set /
// conformance_level table exactly, because each mock receiver's
// constraint sets enumerate precisely its compatible interop points —
// sampling and depth included.
func tr08CompatibleFromProse(ctx context.Context, snap *consumer.CatalogueSnapshot, questionText string) map[string]bool {
	for _, id := range uuidRE.FindAllString(questionText, -1) {
		for i := range snap.Senders {
			if snap.Senders[i].ID != id {
				continue
			}
			params := senderSDPParams(ctx, &snap.Senders[i])
			if params == nil {
				return nil
			}
			out := map[string]bool{}
			for _, r := range snap.Receivers {
				if tr08SDPMatch(params, r.Caps) {
					out[r.ID] = true
				}
			}
			return out
		}
		for i := range snap.Receivers {
			if snap.Receivers[i].ID != id {
				continue
			}
			caps := snap.Receivers[i].Caps
			out := map[string]bool{}
			for j := range snap.Senders {
				if params := senderSDPParams(ctx, &snap.Senders[j]); params != nil && tr08SDPMatch(params, caps) {
					out[snap.Senders[j].ID] = true
				}
			}
			return out
		}
	}
	return nil
}

// sdpClient fetches sender transport files (manifest_href) — the
// BCP-006-01 controller's evidence source for capability matching.
var sdpClient = &stdhttp.Client{Timeout: 5 * time.Second}

// senderSDPParams fetches and parses one sender's transport file into
// BCP-004-01 parameter values. nil when the sender advertises no
// manifest, the fetch fails, or the SDP yields nothing usable — a
// sender whose capabilities cannot be read is never claimed
// compatible.
func senderSDPParams(ctx context.Context, snd *is04.Sender) map[string]any {
	if snd.ManifestHref == nil || *snd.ManifestHref == "" {
		return nil
	}
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, *snd.ManifestHref, nil)
	if err != nil {
		return nil
	}
	resp, err := sdpClient.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != stdhttp.StatusOK {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil
	}
	params := sdpCapParams(string(body))
	if len(params) == 0 {
		return nil
	}
	return params
}

// sdpCapParams maps an SDP's media description onto BCP-004-01
// parameter-constraint values, keyed by cap URN. Field names follow
// the ST 2110 / RFC 9134 fmtp grammar the AMWA tool's own SDP
// templates emit (sampling, depth, width, height, exactframerate,
// interlace, colorimetry, TCS, RANGE, profile, level, sublevel,
// packetmode, TP) plus b=AS for bit_rate. Numbers land as float64 so
// they compare cleanly with JSON-decoded constraint values.
func sdpCapParams(sdp string) map[string]any {
	out := map[string]any{}
	kind := ""
	for _, line := range strings.Split(sdp, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "m="):
			if f := strings.Fields(strings.TrimPrefix(line, "m=")); len(f) > 0 {
				kind = f[0]
			}
		case strings.HasPrefix(line, "b=AS:"):
			if n, err := strconv.ParseFloat(strings.TrimPrefix(line, "b=AS:"), 64); err == nil {
				out["urn:x-nmos:cap:format:bit_rate"] = n
			}
		case strings.HasPrefix(line, "a=rtpmap:"):
			// a=rtpmap:<pt> <subtype>/<clock> — with the m= kind this
			// is the media type.
			if f := strings.Fields(line); len(f) >= 2 && kind != "" {
				if sub, _, ok := strings.Cut(f[1], "/"); ok {
					out["urn:x-nmos:cap:format:media_type"] = kind + "/" + sub
				}
			}
		case strings.HasPrefix(line, "a=fmtp:"):
			rest := strings.TrimPrefix(line, "a=fmtp:")
			if i := strings.IndexByte(rest, ' '); i >= 0 {
				rest = rest[i+1:]
			}
			interlaced := false
			for _, kv := range strings.Split(rest, ";") {
				kv = strings.TrimSpace(kv)
				if kv == "" {
					continue
				}
				k, v, has := strings.Cut(kv, "=")
				if !has {
					if strings.TrimSpace(k) == "interlace" {
						interlaced = true
					}
					continue
				}
				k, v = strings.TrimSpace(k), strings.TrimSpace(v)
				switch k {
				case "profile":
					out["urn:x-nmos:cap:format:profile"] = v
				case "level":
					out["urn:x-nmos:cap:format:level"] = v
				case "sublevel":
					out["urn:x-nmos:cap:format:sublevel"] = v
				case "sampling":
					out["urn:x-nmos:cap:format:color_sampling"] = v
				case "colorimetry":
					out["urn:x-nmos:cap:format:colorspace"] = v
				case "TCS":
					out["urn:x-nmos:cap:format:transfer_characteristic"] = v
				case "TP":
					out["urn:x-nmos:cap:transport:st2110_21_sender_type"] = v
				case "packetmode":
					if v == "0" {
						out["urn:x-nmos:cap:transport:packet_transmission_mode"] = "codestream"
					}
				case "depth":
					if n, err := strconv.ParseFloat(v, 64); err == nil {
						out["urn:x-nmos:cap:format:component_depth"] = n
					}
				case "width":
					if n, err := strconv.ParseFloat(v, 64); err == nil {
						out["urn:x-nmos:cap:format:frame_width"] = n
					}
				case "height":
					if n, err := strconv.ParseFloat(v, 64); err == nil {
						out["urn:x-nmos:cap:format:frame_height"] = n
					}
				case "exactframerate":
					num, den, ok := strings.Cut(v, "/")
					d := 1
					if ok {
						if n, err := strconv.Atoi(den); err == nil {
							d = n
						}
					}
					if n, err := strconv.Atoi(num); err == nil {
						out["urn:x-nmos:cap:format:grain_rate"] = is04.GrainRate{Numerator: n, Denominator: d}
					}
				}
			}
			if interlaced {
				out["urn:x-nmos:cap:format:interlace_mode"] = "interlaced_tff"
			} else {
				out["urn:x-nmos:cap:format:interlace_mode"] = "progressive"
			}
		}
	}
	return out
}

// tr08SDPMatch — the sender's SDP parameters against one receiver's
// declared capabilities: media type membership, then at least one
// BCP-004-01 constraint set every one of whose parameter constraints
// is satisfied by the SDP value (constraints for parameters the SDP
// does not carry are skipped, the tool's own leniency; a receiver
// with no constraint sets declares no coded-format capability and
// matches nothing).
func tr08SDPMatch(params map[string]any, caps is04.ReceiverCaps) bool {
	mt, _ := params["urn:x-nmos:cap:format:media_type"].(string)
	if mt == "" {
		return false
	}
	member := false
	for _, m := range caps.MediaTypes {
		if m == mt {
			member = true
			break
		}
	}
	if !member || len(caps.ConstraintSets) == 0 {
		return false
	}
	for _, cs := range caps.ConstraintSets {
		if sdpMatchesConstraintSet(params, cs) {
			return true
		}
	}
	return false
}

// sdpMatchesConstraintSet applies one constraint set to SDP-derived
// values — meta keys and parameters without an SDP-derived value are
// skipped.
func sdpMatchesConstraintSet(params map[string]any, cs map[string]any) bool {
	for capURI, raw := range cs {
		constraint, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		v, known := params[capURI]
		if !known {
			continue
		}
		if !constraintSatisfied(v, constraint) {
			return false
		}
	}
	return true
}

// compatibleReceiversForSender — BCP-007-03-02's compatibility rule,
// exactly as the suite scores it: both ends on the MXL transport, the
// sender's Flow media_type a member of the receiver's caps.media_types,
// and at least one BCP-004-01 constraint set satisfied by the Flow's
// fields (a receiver with NO constraint_sets is not compatible — the
// suite requires the capability declaration, not just the media type).
// A sender that is missing or not MXL selects nothing: the only
// metadata-carrying multi_choice the tool ships today is the MXL one,
// and an empty selection is the question's own "unable to identify".
func compatibleReceiversForSender(snap *consumer.CatalogueSnapshot, senderID string) map[string]bool {
	out := map[string]bool{}
	var flow *is04.Flow
	for _, s := range snap.Senders {
		if s.ID != senderID || !isMXLTransport(s.Transport) || s.FlowID == nil {
			continue
		}
		for i := range snap.Flows {
			if snap.Flows[i].ID == *s.FlowID {
				flow = &snap.Flows[i]
				break
			}
		}
		break
	}
	if flow == nil {
		return out
	}
	for _, r := range snap.Receivers {
		if !isMXLTransport(r.Transport) {
			continue
		}
		mtOK := false
		for _, mt := range r.Caps.MediaTypes {
			if mt == flow.MediaType {
				mtOK = true
				break
			}
		}
		if !mtOK || len(r.Caps.ConstraintSets) == 0 {
			continue
		}
		for _, cs := range r.Caps.ConstraintSets {
			if flowMatchesConstraintSet(flow, cs) {
				out[r.ID] = true
				break
			}
		}
	}
	return out
}

// flowMatchesConstraintSet applies one BCP-004-01 constraint set to a
// Flow. Only the parameter constraints the MXL suite scores are
// evaluated (the CAP_URI table below); meta keys and unknown cap URNs
// are skipped, matching the suite's own leniency.
func flowMatchesConstraintSet(f *is04.Flow, cs map[string]any) bool {
	for capURI, raw := range cs {
		constraint, ok := raw.(map[string]any)
		if !ok {
			continue // meta:label / meta:enabled and friends
		}
		v, known := flowCapValue(f, capURI)
		if !known {
			continue
		}
		if !constraintSatisfied(v, constraint) {
			return false
		}
	}
	return true
}

// flowCapValue resolves one urn:x-nmos:cap:format:* parameter to the
// Flow's value. Numbers come back as float64 so they compare cleanly
// with JSON-decoded constraint values.
func flowCapValue(f *is04.Flow, capURI string) (any, bool) {
	switch capURI {
	case "urn:x-nmos:cap:format:media_type":
		return f.MediaType, true
	case "urn:x-nmos:cap:format:frame_width":
		return float64(f.FrameWidth), true
	case "urn:x-nmos:cap:format:frame_height":
		return float64(f.FrameHeight), true
	case "urn:x-nmos:cap:format:grain_rate":
		if f.GrainRate == nil {
			return nil, false
		}
		return *f.GrainRate, true
	case "urn:x-nmos:cap:format:interlace_mode":
		return f.Interlace, true
	case "urn:x-nmos:cap:format:colorspace":
		return f.ColorSpace, true
	case "urn:x-nmos:cap:format:transfer_characteristic":
		return f.TransferChar, true
	case "urn:x-nmos:cap:format:component_depth":
		depth := 0
		for _, c := range f.Components {
			if c.BitDepth > depth {
				depth = c.BitDepth
			}
		}
		if depth == 0 {
			return nil, false
		}
		return float64(depth), true
	}
	return nil, false
}

// constraintSatisfied applies the BCP-004-01 enum / minimum / maximum
// keywords to one value.
func constraintSatisfied(v any, constraint map[string]any) bool {
	if enum, ok := constraint["enum"].([]any); ok {
		found := false
		for _, e := range enum {
			if capValueEqual(v, e) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if minRaw, ok := constraint["minimum"].(float64); ok {
		n, isNum := v.(float64)
		if !isNum || n < minRaw {
			return false
		}
	}
	if maxRaw, ok := constraint["maximum"].(float64); ok {
		n, isNum := v.(float64)
		if !isNum || n > maxRaw {
			return false
		}
	}
	return true
}

// capValueEqual compares a Flow value with one JSON-decoded enum entry.
// Rational entries (grain_rate) compare numerator/denominator with the
// spec's implicit denominator of 1.
func capValueEqual(v any, entry any) bool {
	switch fv := v.(type) {
	case string:
		s, ok := entry.(string)
		return ok && s == fv
	case float64:
		n, ok := entry.(float64)
		return ok && n == fv
	case is04.GrainRate:
		m, ok := entry.(map[string]any)
		if !ok {
			return false
		}
		num, _ := m["numerator"].(float64)
		den := 1.0
		if d, ok := m["denominator"].(float64); ok {
			den = d
		}
		fDen := fv.Denominator
		if fDen == 0 {
			fDen = 1
		}
		return int(num) == fv.Numerator && int(den) == fDen
	}
	return false
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
