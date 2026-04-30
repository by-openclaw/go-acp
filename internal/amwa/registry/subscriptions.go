package registry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
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
type SubscriptionResource struct {
	ID            string `json:"id"`
	WSHref        string `json:"ws_href"`
	MaxUpdateRate int    `json:"max_update_rate_ms,omitempty"`
	Persist       bool   `json:"persist"`
	Secure        bool   `json:"secure"`
	ResourcePath  string `json:"resource_path"`
	Params        any    `json:"params,omitempty"`
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
// resource id, `pre` / `post` are the before/after JSON.
type GrainDataRow struct {
	Path string          `json:"path"`
	Pre  json.RawMessage `json:"pre,omitempty"`
	Post json.RawMessage `json:"post,omitempty"`
}

// subscription is one in-flight WS session.
type subscription struct {
	ID           string
	ResourcePath string
	WSHref       string
	Persist      bool
	Secure       bool
	MaxUpdateRate int

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
		m.mu.Lock()
		m.subs[id] = &subscription{
			ID:            id,
			ResourcePath:  req.ResourcePath,
			WSHref:        ws,
			Persist:       req.Persist,
			Secure:        req.Secure,
			MaxUpdateRate: req.MaxUpdateRate,
			source:        id,
			closeCh:       make(chan struct{}),
		}
		m.mu.Unlock()
		return stdhttp.StatusCreated, res, nil
	}
}

// HandleList returns the list of active subscriptions.
func (m *SubscriptionManager) HandleList() httpsession.HandlerFunc {
	return func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		m.mu.Lock()
		defer m.mu.Unlock()
		out := make([]SubscriptionResource, 0, len(m.subs))
		for _, s := range m.subs {
			out = append(out, SubscriptionResource{
				ID: s.ID, WSHref: s.WSHref, MaxUpdateRate: s.MaxUpdateRate,
				Persist: s.Persist, Secure: s.Secure, ResourcePath: s.ResourcePath,
			})
		}
		return 0, out, nil
	}
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
		// resource_path.
		now := time.Now()
		for _, c := range m.store.SnapshotChanges() {
			if !subscriptionMatches(sub.ResourcePath, c) {
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
// subscription.
func (m *SubscriptionManager) onChange(c Change) {
	m.mu.Lock()
	subs := make([]*subscription, 0, len(m.subs))
	for _, s := range m.subs {
		if s.ws != nil && subscriptionMatches(s.ResourcePath, c) {
			subs = append(subs, s)
		}
	}
	m.mu.Unlock()
	now := time.Now()
	for _, s := range subs {
		frame, err := buildGrain(s.source, c, now)
		if err != nil {
			continue
		}
		if err := s.ws.SendText(frame); err != nil {
			m.logger.Warn("registry/subs: send grain failed", "id", s.ID, "err", err)
		}
	}
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
func buildGrain(source string, c Change, now time.Time) ([]byte, error) {
	ts := fmt.Sprintf("%d:%d", now.Unix(), now.Nanosecond())
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
			Data: []GrainDataRow{{
				Path: c.ID,
				Pre:  c.Pre,
				Post: c.Post,
			}},
		},
	}
	_ = c.Kind // change kind isn't part of the grain envelope itself; the
	// post-only / pre-only / both-set tuple already encodes
	// created (pre=null, post=set), updated (both set), deleted (post=null), sync (post=set).
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
