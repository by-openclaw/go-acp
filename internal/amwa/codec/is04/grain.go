package is04

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Grain is the IS-04 §5.2 WebSocket envelope a Query API subscription ships on
// every frame. It is the only push surface IS-04 defines: a Node publishes no
// resource changes of its own, so watching a plant means watching a Registry's
// Query API subscription.
type Grain struct {
	GrainType         string `json:"grain_type"`
	SourceID          string `json:"source_id"`
	FlowID            string `json:"flow_id"`
	OriginTimestamp   string `json:"origin_timestamp"`
	SyncTimestamp     string `json:"sync_timestamp"`
	CreationTimestamp string `json:"creation_timestamp"`
	// Reuses the Source/Flow GrainRate — the same numerator/denominator pair.
	Rate     GrainRate `json:"rate"`
	Duration GrainRate `json:"duration"`
	Grain    GrainBody `json:"grain"`
}

// GrainBody carries the change set. Topic is the resource path the
// subscription was opened on, e.g. "/senders/".
type GrainBody struct {
	Type  string         `json:"type"`
	Topic string         `json:"topic"`
	Data  []GrainDataRow `json:"data"`
}

// GrainDataRow is one change. Path is the resource id; Pre and Post are the
// before and after states.
//
// The three cases are distinguished by which side is present, NOT by a type
// field — that is how IS-04 encodes them:
//
//	Pre == nil, Post != nil  -> ADDED
//	Pre != nil, Post != nil  -> MODIFIED (or SYNC on the initial burst)
//	Pre != nil, Post == nil  -> REMOVED
type GrainDataRow struct {
	Path string          `json:"path"`
	Pre  json.RawMessage `json:"pre,omitempty"`
	Post json.RawMessage `json:"post,omitempty"`
}

// ChangeKind classifies a GrainDataRow.
type ChangeKind string

// Change kinds carried by a Query API subscription grain.
const (
	ChangeAdded    ChangeKind = "added"
	ChangeModified ChangeKind = "modified"
	ChangeRemoved  ChangeKind = "removed"
	ChangeUnknown  ChangeKind = "unknown"
)

// Kind reports what happened to the resource. A row with neither side is not
// something IS-04 defines; it is reported as unknown rather than guessed at.
func (r GrainDataRow) Kind() ChangeKind {
	hasPre, hasPost := len(r.Pre) > 0, len(r.Post) > 0
	switch {
	case !hasPre && hasPost:
		return ChangeAdded
	case hasPre && hasPost:
		return ChangeModified
	case hasPre && !hasPost:
		return ChangeRemoved
	default:
		return ChangeUnknown
	}
}

// Label pulls the human-readable label out of whichever side is present, for
// log lines. Empty when the payload carries none.
func (r GrainDataRow) Label() string {
	for _, raw := range []json.RawMessage{r.Post, r.Pre} {
		if len(raw) == 0 {
			continue
		}
		var v struct {
			Label string `json:"label"`
		}
		if err := json.Unmarshal(raw, &v); err == nil && v.Label != "" {
			return v.Label
		}
	}
	return ""
}

// ErrNotGrain is returned when a frame is well-formed JSON but not a grain.
var ErrNotGrain = errors.New("is04: frame is not a grain envelope")

// DecodeGrain decodes one WebSocket frame.
func DecodeGrain(raw []byte) (*Grain, error) {
	var g Grain
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, fmt.Errorf("is04: decode grain: %w", err)
	}
	if g.GrainType == "" && g.Grain.Topic == "" && len(g.Grain.Data) == 0 {
		return nil, ErrNotGrain
	}
	return &g, nil
}
