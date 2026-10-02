package consumer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dhs/internal/ccm/codec"
	dhsc "dhs/internal/consumer"
	"dhs/internal/transport/ws"
)

// The event channel — CCM 0v1 §13.
//
// A device that serves it pushes state: a subscription answers with the
// resource as it stands and then with what changes, so `watch` reads
// nothing twice and learns of a crosspoint, a gain or a delay the
// moment the device does. A device that does not serve it is polled,
// exactly as before (values.go). Which of the two a device is, is asked
// of the device at Connect and never assumed from its name or firmware.
//
// SHUFFLE 6.0.0 serves it; CONVERT/BRIDGE 7.0.3 and NeuronView 1.13.2
// answer the upgrade with 404.

// Deviations from §13 this connector absorbs and counts.
const (
	// NoWebSocket: the device refused the upgrade. §13.1 has every
	// non-trivial device serve the channel.
	NoWebSocket = "ccm_no_websocket"
	// EventTypeSingular: a notification typed "Event" where §13.3.6 and
	// §13.4 say "Events".
	EventTypeSingular = "ccm_ws_event_type"
	// FrameUnreadable: a frame that is not a §13 message.
	FrameUnreadable = "ccm_ws_frame_unreadable"
	// SubscriptionRefused: the device refused a path its own api.yml
	// declares as a GET (§13.3: every GETtable resource is subscribable).
	SubscriptionRefused = "ccm_ws_subscription_refused"
	// PatchUnapplied: a patch that does not fit the state the device
	// sent before it. The session ends and is rebuilt (§13.4.1).
	PatchUnapplied = "ccm_ws_patch_unapplied"
)

// subscribeTimeout bounds the wait for the device to answer a
// subscription. pingEvery is how often the client pings; a channel
// silent for three of them is treated as lost. Variables so a test can
// shorten them; production never reassigns them.
var (
	subscribeTimeout = 15 * time.Second
	pingEvery        = 10 * time.Second
)

// dialEvents opens the WebSocket. A variable so a test can refuse it.
var dialEvents = ws.Dial

// pusher is one session's event channel.
type pusher struct {
	p    *Plugin
	conn *ws.Conn
	spec *codec.Spec
	log  *slog.Logger

	mu      sync.Mutex
	subs    map[string]*pushSub
	pending map[int64]chan codec.Message
	nextID  int64
	// queue is the member subscriptions still to be made; wake tells
	// the goroutine that makes them there is work (see members).
	queue []fanJob
	wake  chan struct{}
	// answerWithin bounds the wait for one answer.
	answerWithin time.Duration

	// docs is the state the device sent, by resource — what a patch is
	// applied to (§13.4.1). Only the reader touches it.
	docs map[string]any

	// closing is set when this side ends the channel, so the reader
	// does not report its own teardown as a loss.
	closing atomic.Bool
	// done closes when the channel ends, for any reason.
	done chan struct{}
}

// pushSub is one Subscribe: where its events go, which paths it asked
// for, and what the device granted it.
type pushSub struct {
	fn    dhsc.EventFunc
	scope string
	// nested is the templates to subscribe to parent by parent, and
	// fanned the member subscriptions already queued for them.
	nested [][]string
	fanned map[string]bool
	ids    []string
}

// openEvents dials the device's event channel. A device that does not
// serve one is not an error: nil comes back and the session polls.
func (p *Plugin) openEvents(ctx context.Context, client *Client, spec *codec.Spec) *pusher {
	url := strings.Replace(client.Base(), "http", "ws", 1) + codec.WebSocketPath
	conn, err := dialEvents(ctx, url, &ws.DialOptions{TLSConfig: client.tls})
	if err != nil {
		p.ComplianceProfile().Note(NoWebSocket)
		p.deps.Logger.Info("ccm: no event channel on this device, watch polls", "url", url, "err", err)
		return nil
	}
	// Read once, here: the keepalive goroutine is handed the value.
	every := pingEvery
	conn.SetIdleTimeout(3 * every)
	s := &pusher{
		p: p, conn: conn, spec: spec, log: p.deps.Logger,
		subs:    map[string]*pushSub{},
		pending: map[int64]chan codec.Message{},
		docs:    map[string]any{},
		done:    make(chan struct{}),
		wake:    make(chan struct{}, 1),
		// Read once, here, for the same reason as the ping interval.
		answerWithin: subscribeTimeout,
	}
	go s.read()
	go s.members()
	go s.keepalive(every)
	p.deps.Logger.Debug("ccm: event channel open", "url", url)
	return s
}

// events returns the session's event channel, nil when it polls.
func (p *Plugin) events() *pusher {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.push
}

// SessionDone closes when the event channel ends, which is what lets a
// watch reconnect and subscribe again instead of going quiet. A polled
// session has no channel to lose and answers nil.
func (p *Plugin) SessionDone() <-chan struct{} {
	if s := p.events(); s != nil {
		return s.done
	}
	return nil
}

// close ends the channel and waits for the reader to finish.
func (s *pusher) close() {
	s.closing.Store(true)
	_ = s.conn.Close(1000, "")
	<-s.done
}

// read is the one reader. It ends when the socket does.
func (s *pusher) read() {
	defer close(s.done)
	defer func() { _ = s.conn.Close(1000, "") }()
	for {
		_, data, err := s.conn.ReadMessage(context.Background())
		if err != nil {
			if !s.closing.Load() {
				s.log.Warn("ccm: event channel lost", "err", err)
			}
			return
		}
		s.p.RecordRx()
		if err := s.handle(data); err != nil {
			s.p.ComplianceProfile().Note(PatchUnapplied)
			s.log.Warn("ccm: event channel out of step with the device, ending the session", "err", err)
			return
		}
	}
}

// keepalive pings so that a dead link is noticed while nothing changes
// on the device.
func (s *pusher) keepalive(every time.Duration) {
	t := s.p.Clock().NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C():
			// A failed ping needs no handling here: the reader meets
			// the same broken socket and ends the session.
			_ = s.conn.Ping(context.Background(), nil)
		}
	}
}

// handle takes one frame. An error means the cached state can no longer
// be trusted.
func (s *pusher) handle(data []byte) error {
	m, err := codec.ParseMessage(data)
	if err != nil {
		s.p.ComplianceProfile().Note(FrameUnreadable)
		s.log.Warn("ccm: unreadable event frame", "err", err)
		return nil
	}
	switch m.Type {
	case codec.MsgCreateSubscriptionResponse:
		s.mu.Lock()
		ch := s.pending[m.ID]
		delete(s.pending, m.ID)
		s.mu.Unlock()
		if ch != nil {
			ch <- m
		}
	case codec.MsgDeleteSubscriptionResponse:
	case codec.MsgEvent:
		s.p.ComplianceProfile().Note(EventTypeSingular)
		return s.dispatch(m.Events)
	case codec.MsgEvents:
		return s.dispatch(m.Events)
	default:
		s.p.ComplianceProfile().Note(FrameUnreadable)
		s.log.Warn("ccm: event frame of an unknown type", "type", m.Type)
	}
	return nil
}

// dispatch applies each patch to the state it belongs to and publishes
// the leaves it touched.
func (s *pusher) dispatch(events []codec.DocumentPatch) error {
	now := s.p.Clock().Now()
	for _, d := range events {
		tpl, known := s.spec.TemplateFor(d.DocumentRoot)
		writable := known && s.spec.Writable(tpl)
		for _, op := range d.Patch {
			doc, err := codec.ApplyPatch(s.docs[d.DocumentRoot], op)
			if err != nil {
				return fmt.Errorf("%s: %w", d.DocumentRoot, err)
			}
			s.docs[d.DocumentRoot] = doc
			if op.Op == codec.OpRemove {
				s.log.Info("ccm: removed on the device", "resource", d.DocumentRoot, "field", op.Path)
				continue
			}
			if op.Path == "" {
				// The resource as a whole: it may have members to watch.
				s.fan(d.DocumentRoot)
			}
			// ApplyPatch accepted the pointer, so it parses.
			segs, _ := codec.Pointer(op.Path)
			field, v := locate(doc, segs)
			var objs []dhsc.Object
			walkJSON(append(segments(d.DocumentRoot), field...), v, writable, &objs)
			typeObjects(s.spec, d.DocumentRoot, objs)
			s.publish(objs, now)
		}
	}
	return nil
}

// locate follows a pointer through a document the patch was just
// applied to, and returns the value there with the path an operator
// addresses it by — an array element by its uuid, as a walk names it.
func locate(doc any, segs []string) ([]string, any) {
	path := make([]string, 0, len(segs))
	for _, seg := range segs {
		switch t := doc.(type) {
		case map[string]any:
			doc = t[seg]
			path = append(path, seg)
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil {
				// "-": the element an add appended.
				i = len(t) - 1
			}
			doc = t[i]
			path = append(path, elementName(i, doc))
		}
	}
	return path, doc
}

// publish hands each leaf to every subscription whose scope covers it.
func (s *pusher) publish(objs []dhsc.Object, now time.Time) {
	s.mu.Lock()
	subs := make([]*pushSub, 0, len(s.subs))
	for _, sub := range s.subs {
		subs = append(subs, sub)
	}
	s.mu.Unlock()

	for _, o := range objs {
		path := strings.Join(o.Path, "/")
		ev := dhsc.Event{
			Path:      strings.Join(o.Path, "."),
			Label:     o.Label,
			Unit:      o.Unit,
			Access:    o.Access,
			Value:     o.Value,
			Timestamp: now,
			Freshness: "live",
		}
		for _, sub := range subs {
			if sub.scope == "" || path == sub.scope || strings.HasPrefix(path, sub.scope+"/") {
				sub.fn(ev)
			}
		}
	}
}

// subscribe asks the device for every resource under req's path and
// returns once it has answered for each.
func (s *pusher) subscribe(req dhsc.ValueRequest, fn dhsc.EventFunc) error {
	if fn == nil {
		return errors.New("ccm: nil event func")
	}
	scope := strings.Trim(strings.ReplaceAll(strings.TrimSpace(req.Path), ".", "/"), "/")
	urls, nested, skipped := subscriptionURLs(s.spec, scope)
	if len(skipped) > 0 {
		s.log.Warn("ccm: not watched — the parent of these resources cannot be subscribed to",
			"resources", strings.Join(skipped, " "))
	}
	if len(urls) == 0 {
		return fmt.Errorf("ccm: nothing to watch under %q", req.Path)
	}

	key := subKey(req)
	sub := &pushSub{fn: fn, scope: scope, nested: nested, fanned: map[string]bool{}}
	s.mu.Lock()
	if _, dup := s.subs[key]; dup {
		s.mu.Unlock()
		return fmt.Errorf("ccm: already watching %q", req.Path)
	}
	// Registered before anything is sent: the device may push the
	// initial state ahead of its answer.
	s.subs[key] = sub
	s.mu.Unlock()

	// One request at a time: that is the pace SHUFFLE 6.0.0 was
	// measured to take without its REST API slowing down.
	for _, u := range urls {
		m, err := s.ask(u)
		if err != nil {
			s.unsubscribe(req)
			return err
		}
		s.granted(key, sub, u, m)
	}
	s.mu.Lock()
	n := len(sub.ids)
	s.mu.Unlock()
	if n == 0 {
		s.unsubscribe(req)
		return fmt.Errorf("ccm: the device refused every subscription under %q", req.Path)
	}
	s.log.Info("ccm: watching over the event channel", "scope", scope, "subscriptions", n)
	return nil
}

// ask sends one CreateSubscription and waits for the device's answer.
func (s *pusher) ask(url string) (codec.Message, error) {
	ch := make(chan codec.Message, 1)
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	s.pending[id] = ch
	s.mu.Unlock()
	forget := func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}

	if err := s.conn.WriteText(context.Background(), codec.CreateSubscription(id, url)); err != nil {
		forget()
		return codec.Message{}, fmt.Errorf("ccm: subscribe %s: %w", url, err)
	}
	s.p.RecordTx()
	select {
	case m := <-ch:
		return m, nil
	case <-s.done:
		forget()
		return codec.Message{}, fmt.Errorf("ccm: event channel closed while subscribing to %s", url)
	case <-s.p.Clock().After(s.answerWithin):
		forget()
		return codec.Message{}, fmt.Errorf("ccm: the device did not answer the subscription to %s within %s", url, s.answerWithin)
	}
}

// granted records the device's answer to one subscription. A refusal is
// counted and the watch stands on what was accepted. A subscription
// granted after its Subscribe was withdrawn is handed back.
func (s *pusher) granted(key string, sub *pushSub, url string, m codec.Message) {
	if m.Status != codec.StatusOK {
		s.p.ComplianceProfile().Note(SubscriptionRefused)
		s.log.Warn("ccm: subscription refused", "resource", url, "status", m.Status, "message", m.Text)
		return
	}
	s.mu.Lock()
	current := s.subs[key] == sub
	if current {
		sub.ids = append(sub.ids, m.SubscriptionID)
	}
	s.mu.Unlock()
	if !current {
		s.release(m.SubscriptionID)
	}
}

// release hands one subscription back. Best effort: a channel that is
// already gone took its subscriptions with it (§13.5).
func (s *pusher) release(subscriptionID string) {
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	s.mu.Unlock()
	_ = s.conn.WriteText(context.Background(), codec.DeleteSubscription(id, subscriptionID))
}

// unsubscribe ends one Subscribe. Unknown requests are not an error:
// watch tears down defensively.
func (s *pusher) unsubscribe(req dhsc.ValueRequest) {
	key := subKey(req)
	s.mu.Lock()
	sub := s.subs[key]
	delete(s.subs, key)
	var ids []string
	if sub != nil {
		ids = sub.ids
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.release(id)
	}
}

// Members.
//
// A resource below a member of a collection — a sender's channels —
// needs two path parameters, and one subscription may carry one `*`
// (see subscriptionURLs). So the members are subscribed to one parent
// at a time: each parent is learnt from the state the device pushes
// for it, and its members are asked for then. A parent that appears
// later is picked up the same way.
//
// Measured on SHUFFLE 6.0.0 (2026-10-02): 1544 senders, 16 896 channel
// documents, 44 s at 41 ms per answer, the REST API answering in 50 ms
// (151 ms at worst) throughout.

// fanJob is one member subscription still to be made.
type fanJob struct {
	key string
	url string
}

// fan queues the member subscriptions a newly seen resource calls for.
func (s *pusher) fan(root string) {
	s.mu.Lock()
	for key, sub := range s.subs {
		for _, u := range memberURLs(root, sub.nested) {
			if !sub.fanned[u] {
				sub.fanned[u] = true
				s.queue = append(s.queue, fanJob{key, u})
			}
		}
	}
	waiting := len(s.queue) > 0
	s.mu.Unlock()
	if waiting {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

// memberURLs is what to subscribe to below root: for each nested
// template root is a member of, the level below it with its one
// parameter as `*`.
func memberURLs(root string, nested [][]string) []string {
	at := segments(root)
	var out []string
	for _, tpl := range nested {
		if len(tpl) <= len(at) || !strings.HasPrefix(tpl[len(at)-1], "{") {
			continue
		}
		match := true
		for i, seg := range at {
			match = match && (strings.HasPrefix(tpl[i], "{") || tpl[i] == seg)
		}
		rest, params := append([]string(nil), tpl[len(at):]...), 0
		for i, seg := range rest {
			if strings.HasPrefix(seg, "{") {
				rest[i] = "*"
				params++
			}
		}
		if match && params == 1 {
			out = append(out, root+"/"+strings.Join(rest, "/"))
		}
	}
	return out
}

// members makes the queued subscriptions, one at a time.
func (s *pusher) members() {
	for {
		select {
		case <-s.done:
			return
		case <-s.wake:
		}
		made := 0
		for {
			s.mu.Lock()
			if len(s.queue) == 0 {
				s.mu.Unlock()
				break
			}
			job := s.queue[0]
			s.queue = s.queue[1:]
			sub := s.subs[job.key]
			s.mu.Unlock()
			if sub == nil {
				// Its Subscribe was withdrawn while this waited.
				continue
			}
			m, err := s.ask(job.url)
			if err != nil {
				// A channel that stops answering is a lost one: end
				// it, and the watch starts again from a new session.
				s.log.Warn("ccm: member subscription failed, ending the session", "err", err)
				_ = s.conn.Close(1011, "")
				return
			}
			s.granted(job.key, sub, job.url, m)
			made++
		}
		if made > 0 {
			s.log.Info("ccm: members watched", "subscriptions", made)
		}
	}
}

// subKey is the identity of a request: two Subscribes for the same
// scope are the same subscription.
func subKey(r dhsc.ValueRequest) string {
	return fmt.Sprintf("s=%d|p=%s|l=%s|g=%s|id=%d", r.Slot, r.Path, r.Label, r.Group, r.ID)
}

// subscriptionURLs turns a scope into what to subscribe to: every GET
// the device's api.yml declares at or under it, a path parameter the
// scope does not name becoming `*` (§13.3.5).
//
// At most one `*` per path. §13.3.5 allows several, and SHUFFLE 6.0.0
// accepts `/io/ip/senders/audio/*/channels/*` — and then stops
// answering its REST API for about a minute while it assembles every
// channel of 1544 senders. A path that would need a second `*` comes
// back in nested, as its template with the scope applied, to be
// subscribed to parent by parent (see members). When its parent is not
// itself a GET there is nothing to learn the parents from, and it comes
// back in skipped.
//
// A collection — a path whose only job is to list the ids of the
// members below it — is not subscribed to: its members are.
func subscriptionURLs(spec *codec.Spec, scope string) (urls []string, nested [][]string, skipped []string) {
	var want []string
	if scope != "" {
		want = strings.Split(scope, "/")
	}
	gets := spec.With(codec.GET)
	collection, readable := map[string]bool{}, map[string]bool{}
	for _, t := range gets {
		readable[t] = true
		if i := strings.LastIndex(t, "/"); i > 0 && strings.HasPrefix(t[i+1:], "{") {
			collection[t[:i]] = true
		}
	}

	seen := map[string]bool{}
	add := func(u string) {
		if !seen[u] {
			seen[u] = true
			urls = append(urls, u)
		}
	}
	for _, t := range gets {
		if collection[t] {
			continue
		}
		tpl := segments(t)
		if len(tpl) < len(want) {
			continue
		}
		segs := append([]string(nil), tpl...)
		wild, first, ok := 0, 0, true
		for i, seg := range tpl {
			param := strings.HasPrefix(seg, "{")
			switch {
			case i < len(want) && param:
				segs[i] = want[i]
				tpl[i] = want[i]
			case i < len(want):
				ok = ok && seg == want[i]
			case param:
				segs[i] = "*"
				if wild++; wild == 1 {
					first = i
				}
			}
		}
		switch {
		case !ok:
		case wild <= 1:
			add("/" + strings.Join(segs, "/"))
		case readable["/"+strings.Join(segments(t)[:first+1], "/")]:
			nested = append(nested, tpl)
		default:
			skipped = append(skipped, t)
		}
	}
	// A scope inside one resource: that resource carries the field.
	if resource, field, err := split(spec, scope); err == nil && len(field) > 0 {
		add(resource)
	}
	sort.Strings(urls)
	return urls, nested, skipped
}
