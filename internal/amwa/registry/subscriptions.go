package registry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	stdhttp "net/http"

	"acp/internal/amwa/codec/is04"
	httpsession "acp/internal/amwa/session/http"
)

// SubscriptionRequest is the body POSTed to /subscriptions.
type SubscriptionRequest struct {
	MaxUpdateRate int    `json:"max_update_rate_ms,omitempty"`
	Persist       bool   `json:"persist"`
	Secure        bool   `json:"secure"`
	ResourcePath  string `json:"resource_path"` // e.g. "/nodes", "/devices"
	Params        any    `json:"params,omitempty"`
}

// SubscriptionResource is the body returned by POST /subscriptions
// and listed by GET /subscriptions, per IS-04 §4.2.
//
// Schema notes — every field in this struct is required by some
// minor we serve, so omitempty is dropped:
//   - v1.0 (queryapi-subscription-response.json): `id`, `ws_href`,
//     `max_update_rate_ms`, `persist`, `resource_path`, `params`
//     (all required, `secure` not in schema).
//   - v1.1+: same six + `secure` (required).
//
// Per-version stripping happens in `subscriptionForVersion` (drops
// `secure` for v1.0). `params` is forced to `{}` when nil so the
// `required` + `type=object` constraint always passes.
type SubscriptionResource struct {
	ID            string `json:"id"`
	WSHref        string `json:"ws_href"`
	MaxUpdateRate int    `json:"max_update_rate_ms"`
	Persist       bool   `json:"persist"`
	Secure        bool   `json:"secure"`
	ResourcePath  string `json:"resource_path"`
	Params        any    `json:"params"`
}

// subscriptionForVersion returns the per-API-version JSON shape of a
// SubscriptionResource. Drops `secure` for v1.0 (the field landed in
// v1.1) and forces `params` to an empty object when nil. Returns the
// canonical struct unchanged for v1.1+, where every field maps
// directly to the v1.X schema.
func subscriptionForVersion(r SubscriptionResource, apiVer string) any {
	if r.Params == nil {
		r.Params = map[string]any{}
	}
	if apiVer == "v1.0" {
		return map[string]any{
			"id":                 r.ID,
			"ws_href":            r.WSHref,
			"max_update_rate_ms": r.MaxUpdateRate,
			"persist":            r.Persist,
			"resource_path":      r.ResourcePath,
			"params":             r.Params,
		}
	}
	return r
}

// Grain is the IS-04 §5.2 envelope shipped on every WebSocket frame.
type Grain struct {
	GrainType         string  `json:"grain_type"`
	SourceID          string  `json:"source_id"`
	FlowID            string  `json:"flow_id"`
	OriginTimestamp   string  `json:"origin_timestamp"`
	SyncTimestamp     string  `json:"sync_timestamp"`
	CreationTimestamp string  `json:"creation_timestamp"`
	Rate              GrainRT `json:"rate"`
	Duration          GrainRT `json:"duration"`
	Grain             GrainBody `json:"grain"`
}

type GrainRT struct {
	Numerator   int `json:"numerator"`
	Denominator int `json:"denominator"`
}

// GrainBody carries the actual change set per IS-04 §5.2.
type GrainBody struct {
	Type  string         `json:"type"`
	Topic string         `json:"topic"`
	Data  []GrainDataRow `json:"data"`
}

// GrainDataRow is one entry in GrainBody.Data — `path` is the
// resource id, `pre` / `post` are the before/after JSON. Both are
// pointers so a nil pointer drops the key entirely on encode (the
// AMWA Testing tool's IS-04-02 test_23_1 specifically asserts that
// CREATED grains have no `pre` key, not that `pre` == null).
type GrainDataRow struct {
	Path string           `json:"path"`
	Pre  *json.RawMessage `json:"pre,omitempty"`
	Post *json.RawMessage `json:"post,omitempty"`
}

// subscription is one in-flight WS session.
type subscription struct {
	ID           string
	ResourcePath string
	WSHref       string
	Persist      bool
	Secure       bool
	MaxUpdateRate int

	// params is the IS-04 §6.1.5 basic-query / RQL filter the
	// subscriber requested at POST time. Mirrors the Query API GET
	// query string: top-level field=value plus optional
	// `query.rql=eq(field,value)`.
	params map[string][]string

	// downgrade is `query.downgrade=v1.X`, lifted out of params at
	// subscription-creation time so it isn't applied as a regular
	// equality filter (a resource never carries `query.downgrade` as
	// a JSON field). When empty, the subscription is strictly bound
	// to its own api_ver.
	downgrade string

	ws       *httpsession.WebSocket
	source   string // sub UUID echoed in grain.source_id
	closeCh  chan struct{}
}

// SubscriptionManager owns all in-flight subscriptions, the WS
// upgrade handler, and the change fan-out.
type SubscriptionManager struct {
	logger *slog.Logger
	store  *Store

	// advertiseHost provides the ws:// URL we hand out.
	advertiseHost string
	apiVer        string

	mu   sync.Mutex
	subs map[string]*subscription
}

// NewSubscriptionManager builds the manager + wires it to the store.
// advertiseHost is the host:port we use to construct ws_href.
func NewSubscriptionManager(logger *slog.Logger, store *Store, advertiseHost, apiVer string) *SubscriptionManager {
	if logger == nil {
		logger = slog.Default()
	}
	if apiVer == "" {
		apiVer = is04.APIVersion
	}
	m := &SubscriptionManager{
		logger:        logger,
		store:         store,
		advertiseHost: advertiseHost,
		apiVer:        apiVer,
		subs:          make(map[string]*subscription),
	}
	store.AddListener(m.onChange)
	return m
}

// HandlePost is the POST /subscriptions handler — creates a new
// subscription and returns the SubscriptionResource.
func (m *SubscriptionManager) HandlePost(base string) httpsession.HandlerFunc {
	return func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		var req SubscriptionRequest
		d := json.NewDecoder(r.Body)
		if err := d.Decode(&req); err != nil {
			return stdhttp.StatusBadRequest, httpsession.ErrorBody{Code: 400, Error: "Bad Request", Debug: err.Error()}, nil
		}
		if req.ResourcePath == "" {
			return stdhttp.StatusBadRequest, httpsession.ErrorBody{Code: 400, Error: "Bad Request", Debug: "resource_path required"}, nil
		}
		id, err := newUUIDLike()
		if err != nil {
			return stdhttp.StatusInternalServerError, httpsession.ErrorBody{Code: 500, Error: "Internal Server Error", Debug: err.Error()}, nil
		}
		ws := "ws://" + m.advertiseHost + base + "/subscriptions/" + id + "/ws"
		res := SubscriptionResource{
			ID: id, WSHref: ws,
			MaxUpdateRate: req.MaxUpdateRate,
			Persist:       req.Persist, Secure: req.Secure,
			ResourcePath: req.ResourcePath, Params: req.Params,
		}
		params := paramsAsQuery(req.Params)
		downgrade := ""
		if vs, ok := params["query.downgrade"]; ok && len(vs) > 0 {
			downgrade = vs[0]
			delete(params, "query.downgrade")
		}
		// Strip pagination + non-rql control params — they're not
		// equality filters. Keep `query.rql` (the RQL predicate) so
		// jsonMatchesFilter can honor it. Same handling as the Query
		// API GET path.
		for k := range params {
			if strings.HasPrefix(k, "paging.") {
				delete(params, k)
				continue
			}
			if strings.HasPrefix(k, "query.") && k != "query.rql" {
				delete(params, k)
			}
		}
		if len(params) == 0 {
			params = nil
		}
		m.mu.Lock()
		m.subs[id] = &subscription{
			ID:            id,
			ResourcePath:  req.ResourcePath,
			WSHref:        ws,
			Persist:       req.Persist,
			Secure:        req.Secure,
			MaxUpdateRate: req.MaxUpdateRate,
			params:        params,
			downgrade:     downgrade,
			source:        id,
			closeCh:       make(chan struct{}),
		}
		m.mu.Unlock()
		// IS-04 §6.1.6 (Query API) — Subscription POST returns 201 +
		// `Location` pointing at /subscriptions/{id}. AMWA test_29 /
		// test_31 explicitly check this header.
		loc := base + "/subscriptions/" + id
		return stdhttp.StatusCreated, &httpsession.WithHeaders{
			Body:    subscriptionForVersion(res, m.apiVer),
			Headers: map[string]string{"Location": loc},
		}, nil
	}
}

// HandleList returns the list of active subscriptions.
func (m *SubscriptionManager) HandleList() httpsession.HandlerFunc {
	return func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		m.mu.Lock()
		defer m.mu.Unlock()
		out := make([]any, 0, len(m.subs))
		for _, s := range m.subs {
			out = append(out, subscriptionForVersion(SubscriptionResource{
				ID: s.ID, WSHref: s.WSHref, MaxUpdateRate: s.MaxUpdateRate,
				Persist: s.Persist, Secure: s.Secure, ResourcePath: s.ResourcePath,
				Params: queryAsParams(s.params),
			}, m.apiVer))
		}
		return 0, out, nil
	}
}

// HandleGetByID returns the SubscriptionResource for a single
// subscription id. AMWA test_29 specifically POSTs a sub then GETs
// /subscriptions/{id} and expects 200 + the same shape.
func (m *SubscriptionManager) HandleGetByID(prefix string) httpsession.HandlerFunc {
	return func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		id := strings.TrimPrefix(r.URL.Path, prefix)
		if id == "" || strings.Contains(id, "/") {
			return stdhttp.StatusNotFound, httpsession.ErrorBody{Code: 404, Error: "Not Found", Debug: r.URL.Path}, nil
		}
		m.mu.Lock()
		s, ok := m.subs[id]
		m.mu.Unlock()
		if !ok {
			return stdhttp.StatusNotFound, httpsession.ErrorBody{Code: 404, Error: "Not Found", Debug: id}, nil
		}
		return 0, subscriptionForVersion(SubscriptionResource{
			ID: s.ID, WSHref: s.WSHref, MaxUpdateRate: s.MaxUpdateRate,
			Persist: s.Persist, Secure: s.Secure, ResourcePath: s.ResourcePath,
			Params: queryAsParams(s.params),
		}, m.apiVer), nil
	}
}

// paramsAsQuery flattens an IS-04 subscription `params` JSON object
// into the map shape `splitFilterParams` + `jsonMatchesFilter`
// consume. Accepts the canonical shape
// `{"description":"foo","label":"bar"}`. RQL is delivered via the
// special `query.rql` key.
func paramsAsQuery(p any) map[string][]string {
	if p == nil {
		return nil
	}
	out := make(map[string][]string)
	if m, ok := p.(map[string]any); ok {
		for k, v := range m {
			switch tv := v.(type) {
			case string:
				out[k] = []string{tv}
			case []any:
				for _, e := range tv {
					if s, ok := e.(string); ok {
						out[k] = append(out[k], s)
					}
				}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// queryAsParams is the inverse used when echoing the params back on
// GET /subscriptions and GET /subscriptions/{id}.
func queryAsParams(q map[string][]string) any {
	if len(q) == 0 {
		return nil
	}
	out := make(map[string]any, len(q))
	for k, vs := range q {
		if len(vs) == 1 {
			out[k] = vs[0]
		} else if len(vs) > 1 {
			ss := make([]any, len(vs))
			for i, v := range vs {
				ss[i] = v
			}
			out[k] = ss
		}
	}
	return out
}

// ServeHTTP is the WS upgrade handler. Routed at
// /x-nmos/query/<v>/subscriptions/{id}/ws.
func (m *SubscriptionManager) UpgradeHandler(base string) func(stdhttp.ResponseWriter, *stdhttp.Request) {
	prefix := base + "/subscriptions/"
	return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		// Path tail must be `<id>/ws`.
		rest := r.URL.Path[len(prefix):]
		if len(rest) <= 3 || rest[len(rest)-3:] != "/ws" {
			stdhttp.NotFound(w, r)
			return
		}
		id := rest[:len(rest)-3]
		m.mu.Lock()
		sub, ok := m.subs[id]
		m.mu.Unlock()
		if !ok {
			stdhttp.NotFound(w, r)
			return
		}
		ws, err := httpsession.AcceptWebSocket(w, r)
		if err != nil {
			m.logger.Warn("registry/subs: upgrade", "err", err)
			return
		}
		m.mu.Lock()
		sub.ws = ws
		m.mu.Unlock()

		// Send sync grains for every existing resource matching the
		// resource_path AND the subscription params filter. SYNC has
		// pre == post (current snapshot), so a single jsonMatchesFilter
		// against Post is sufficient. We also apply the same no-
		// downgrade-by-default version gate the Query API uses, so a
		// /query/v1.3 subscription doesn't bootstrap with v1.0-only
		// resources (AMWA test_22_2). When the subscriber requested
		// `query.downgrade=v1.X` at POST time, lower-version
		// resources become visible per AMWA's downgrade semantics.
		now := time.Now()
		for _, c := range m.store.SnapshotChanges(m.apiVer) {
			if !subscriptionMatches(sub.ResourcePath, c) {
				continue
			}
			if !versionAllowed(c.APIVer, m.apiVer, sub.downgrade) {
				continue
			}
			if !jsonMatchesFilter(c.Post, sub.params) && len(sub.params) > 0 {
				continue
			}
			frame, err := buildGrain(sub.source, c, now)
			if err != nil {
				m.logger.Warn("registry/subs: build sync grain", "err", err)
				continue
			}
			if err := ws.SendText(frame); err != nil {
				m.logger.Warn("registry/subs: send sync", "err", err)
				_ = ws.Close()
				return
			}
		}

		// Hold the connection open until the peer closes or we evict.
		// We delegate to ReadText which auto-replies to ping frames.
		go func() {
			defer m.removeSub(id)
			for {
				if _, err := ws.ReadText(); err != nil {
					return
				}
			}
		}()
	}
}

// onChange is the store listener — fan-out to every matching
// subscription. For each subscriber we project the Change against the
// subscriber's filter and synthesize a grain whose pre/post pair
// reflects the resource's relationship to the filter set:
//
//   - resource entered the filter (pre absent or unmatched, post matched)
//     → "created from filter"  : pre dropped, post kept
//   - resource left the filter (pre matched, post absent or unmatched)
//     → "deleted from filter"  : pre kept, post dropped
//   - resource updated within the filter (both matched)
//     → "modified": both kept
//   - resource was outside the filter on both sides → no emit
//
// Without a filter we ship the raw Change (existing semantics).
func (m *SubscriptionManager) onChange(c Change) {
	// We can no longer short-circuit on m.apiVer here: per-subscription
	// `query.downgrade` may relax the version gate, so fan out has to
	// run per-subscriber. listeners are invoked while the Store holds
	// its write lock, so we read api_ver from the Change envelope
	// (already populated by the Put* method) instead of re-locking.
	m.mu.Lock()
	subs := make([]*subscription, 0, len(m.subs))
	for _, s := range m.subs {
		if s.ws == nil {
			continue
		}
		if !subscriptionMatches(s.ResourcePath, c) {
			continue
		}
		if !versionAllowed(c.APIVer, m.apiVer, s.downgrade) {
			continue
		}
		subs = append(subs, s)
	}
	m.mu.Unlock()
	now := time.Now()
	for _, s := range subs {
		// Filter against the CANONICAL body (with all v1.3-shape
		// fields present) so a `description=...` filter still
		// matches a v1.0 Node whose description got stripped on
		// the wire. After projection, re-encode the surviving
		// pre/post bodies via the wire codec so the subscriber
		// only sees fields that exist in their wire minor.
		projected, ok := projectChange(c, s.params)
		if !ok {
			continue
		}
		projected = reencodeChange(projected, m.apiVer)
		frame, err := buildGrain(s.source, projected, now)
		if err != nil {
			continue
		}
		if err := s.ws.SendText(frame); err != nil {
			m.logger.Warn("registry/subs: send grain failed", "id", s.ID, "err", err)
		}
	}
}

// reencodeChange decodes c.Pre/c.Post into the typed canonical struct
// for c.ResourceType and re-marshals via the codec for wireVer. When
// no codec is registered for wireVer (or decode fails), the original
// Change is returned unchanged.
func reencodeChange(c Change, wireVer string) Change {
	codec, ok := is04.Get(wireVer)
	if !ok {
		return c
	}
	out := c
	out.Pre = reencodeBody(c.Pre, c.ResourceType, codec)
	out.Post = reencodeBody(c.Post, c.ResourceType, codec)
	return out
}

func reencodeBody(raw json.RawMessage, t is04.ResourceType, codec is04.Codec) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	switch t {
	case is04.ResourceNode:
		var v is04.Node
		if err := json.Unmarshal(raw, &v); err != nil {
			return raw
		}
		if b, err := codec.EncodeNode(v); err == nil {
			return b
		}
	case is04.ResourceDevice:
		var v is04.Device
		if err := json.Unmarshal(raw, &v); err != nil {
			return raw
		}
		if b, err := codec.EncodeDevice(v); err == nil {
			return b
		}
	case is04.ResourceSource:
		var v is04.Source
		if err := json.Unmarshal(raw, &v); err != nil {
			return raw
		}
		if b, err := codec.EncodeSource(v); err == nil {
			return b
		}
	case is04.ResourceFlow:
		var v is04.Flow
		if err := json.Unmarshal(raw, &v); err != nil {
			return raw
		}
		if b, err := codec.EncodeFlow(v); err == nil {
			return b
		}
	case is04.ResourceSender:
		var v is04.Sender
		if err := json.Unmarshal(raw, &v); err != nil {
			return raw
		}
		if b, err := codec.EncodeSender(v); err == nil {
			return b
		}
	case is04.ResourceReceiver:
		var v is04.Receiver
		if err := json.Unmarshal(raw, &v); err != nil {
			return raw
		}
		if b, err := codec.EncodeReceiver(v); err == nil {
			return b
		}
	}
	return raw
}

// projectChange clips a raw Change against an optional filter so the
// resulting grain reports the resource's filter-set transitions per
// IS-04 §5.2.
func projectChange(c Change, params map[string][]string) (Change, bool) {
	if len(params) == 0 {
		return c, true
	}
	preMatch := jsonMatchesFilter(c.Pre, params)
	postMatch := jsonMatchesFilter(c.Post, params)
	if !preMatch && !postMatch {
		return c, false
	}
	out := c
	if !preMatch {
		out.Pre = nil
	}
	if !postMatch {
		out.Post = nil
	}
	// Re-derive Kind from the projected pair so buildGrain renders
	// the correct shape: pre-only ⇒ deleted, post-only ⇒ created,
	// both ⇒ updated/sync (preserve sync if it was sync originally).
	switch {
	case len(out.Pre) > 0 && len(out.Post) > 0:
		if c.Kind == ChangeSync {
			out.Kind = ChangeSync
		} else {
			out.Kind = ChangeUpdated
		}
	case len(out.Pre) > 0:
		out.Kind = ChangeDeleted
	case len(out.Post) > 0:
		out.Kind = ChangeCreated
	}
	return out, true
}

// jsonMatchesFilter unmarshals data into a generic map and compares
// the requested top-level fields. Supports the same shape as the
// Query API filter:
//   - "field=value" → top-level equality
//   - "query.rql=eq(field,value)" → single-predicate RQL
func jsonMatchesFilter(data json.RawMessage, q map[string][]string) bool {
	if len(data) == 0 {
		return false
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return false
	}
	for k, vs := range q {
		switch {
		case strings.HasPrefix(k, "paging.") || strings.HasPrefix(k, "query."):
			if k != "query.rql" {
				continue
			}
			for _, v := range vs {
				p := parseRQLEq(v)
				if p == nil {
					continue
				}
				val, ok := m[p.Field].(string)
				if !ok || val != p.Value {
					return false
				}
			}
		default:
			val, ok := m[k].(string)
			if !ok {
				return false
			}
			ok2 := false
			for _, want := range vs {
				if val == want {
					ok2 = true
					break
				}
			}
			if !ok2 {
				return false
			}
		}
	}
	return true
}

func (m *SubscriptionManager) removeSub(id string) {
	m.mu.Lock()
	if s, ok := m.subs[id]; ok {
		if s.ws != nil {
			_ = s.ws.Close()
		}
		delete(m.subs, id)
	}
	m.mu.Unlock()
}

// subscriptionMatches reports whether c targets the given
// resource_path. resource_path is a single collection like `/nodes`
// or `/devices`. We don't yet support filter expressions — those
// land alongside the RQL filter in the Query API.
func subscriptionMatches(resourcePath string, c Change) bool {
	want := "/" + c.ResourceType.Plural()
	return resourcePath == "" || resourcePath == "/" || resourcePath == want
}

// buildGrain turns a Change into the IS-04 §5.2 grain wire envelope.
//
// Per IS-04 v1.3.3 §5.2, the (pre, post) pair encodes the change
// kind:
//   - created  → pre absent, post present
//   - updated  → pre present, post present (different bodies)
//   - deleted  → pre present, post absent
//   - sync     → pre present, post present (SAME body)
func buildGrain(source string, c Change, now time.Time) ([]byte, error) {
	ts := fmt.Sprintf("%d:%d", now.Unix(), now.Nanosecond())
	row := GrainDataRow{Path: c.ID}
	switch c.Kind {
	case ChangeCreated:
		if len(c.Post) > 0 {
			p := c.Post
			row.Post = &p
		}
	case ChangeUpdated:
		if len(c.Pre) > 0 {
			p := c.Pre
			row.Pre = &p
		}
		if len(c.Post) > 0 {
			p := c.Post
			row.Post = &p
		}
	case ChangeDeleted:
		if len(c.Pre) > 0 {
			p := c.Pre
			row.Pre = &p
		}
	case ChangeSync:
		// SYNC echoes the current resource as both pre and post —
		// IS-04 §5.2 grain semantics for "no change since the
		// subscriber connected".
		if len(c.Post) > 0 {
			p := c.Post
			row.Pre = &p
			row.Post = &p
		}
	}
	g := Grain{
		GrainType:         "event",
		SourceID:          source,
		FlowID:            source,
		OriginTimestamp:   ts,
		SyncTimestamp:     ts,
		CreationTimestamp: ts,
		Rate:              GrainRT{Numerator: 0, Denominator: 1},
		Duration:          GrainRT{Numerator: 0, Denominator: 1},
		Grain: GrainBody{
			Type:  "urn:x-nmos:format:data.event",
			Topic: "/" + c.ResourceType.Plural() + "/",
			Data:  []GrainDataRow{row},
		},
	}
	return json.Marshal(g)
}

// newUUIDLike returns a v4-shaped UUID built from crypto/rand. We
// don't need RFC 4122 strictness here — Subscription IDs are opaque
// to clients — but the v4 shape keeps every IS-04 id pattern check
// happy.
func newUUIDLike() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16])), nil
}
