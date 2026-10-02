package codec

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// CCM 0v1 §13 — events over a WebSocket.
//
// A client subscribes to the same paths it would GET (§13.3: every
// GETtable resource is subscribable, one to one) and the device answers
// with the resource's whole state once, then with RFC 6902 patches as
// it changes. This file is the wire shape of that exchange and the
// patch arithmetic; who is connected and what is being watched belongs
// to the consumer.

// WebSocketPath is where the event channel hangs, relative to the API
// base (§13.2).
const WebSocketPath = "/ws"

// Message types (§13.3.1).
const (
	MsgCreateSubscription         = "CreateSubscription"
	MsgCreateSubscriptionResponse = "CreateSubscriptionResponse"
	MsgDeleteSubscription         = "DeleteSubscription"
	MsgDeleteSubscriptionResponse = "DeleteSubscriptionResponse"
	// MsgEvents is the type §13.3.6 and §13.4 give a notification.
	MsgEvents = "Events"
	// MsgEvent is what SHUFFLE 6.0.0 sends instead. Same payload.
	MsgEvent = "Event"
)

// StatusOK is the status of an accepted request (§13.3.2).
const StatusOK = 200

// JSON Patch operations a device uses (§13.4.1 names these three).
const (
	OpAdd     = "add"
	OpRemove  = "remove"
	OpReplace = "replace"
)

type request struct {
	Type    string `json:"type"`
	ID      int64  `json:"id"`
	Payload any    `json:"payload"`
}

// CreateSubscription is the request that starts a subscription to
// relativeURL (§13.3.1). A `*` in place of one path parameter
// subscribes to every resource matching (§13.3.5).
func CreateSubscription(id int64, relativeURL string) []byte {
	return encode(request{MsgCreateSubscription, id, map[string]string{"relativeUrl": relativeURL}})
}

// DeleteSubscription is the request that ends one (§13.3.3).
func DeleteSubscription(id int64, subscriptionID string) []byte {
	return encode(request{MsgDeleteSubscription, id, map[string]string{"subscriptionId": subscriptionID}})
}

func encode(r request) []byte {
	// A string, an integer and a map of strings: Marshal cannot fail.
	b, _ := json.Marshal(r)
	return b
}

// Message is one decoded frame from the device.
type Message struct {
	Type string
	ID   int64

	// Status, Text and SubscriptionID are a response's payload
	// (§13.3.2, §13.3.4).
	Status         int
	Text           string
	SubscriptionID string

	// Events is a notification's payload: one entry per resource that
	// changed (§13.4).
	Events []DocumentPatch
}

// DocumentPatch is the change to one resource.
type DocumentPatch struct {
	// DocumentRoot is the resource's path, API-relative.
	DocumentRoot string `json:"documentRoot"`
	// Patch is an RFC 6902 document relative to that resource.
	Patch []PatchOp `json:"patch"`
}

// PatchOp is one RFC 6902 operation.
type PatchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}

type envelope struct {
	Type    string          `json:"type"`
	ID      int64           `json:"id"`
	Payload json.RawMessage `json:"payload"`
}

type reply struct {
	Status         int    `json:"status"`
	Message        string `json:"message"`
	SubscriptionID string `json:"subscriptionId"`
}

// ParseMessage decodes one frame. A type this file does not know comes
// back with only Type and ID set, for the caller to report.
func ParseMessage(data []byte) (Message, error) {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return Message{}, fmt.Errorf("ccm: event frame: %w", err)
	}
	m := Message{Type: env.Type, ID: env.ID}
	switch env.Type {
	case MsgCreateSubscriptionResponse, MsgDeleteSubscriptionResponse:
		var r reply
		if err := json.Unmarshal(env.Payload, &r); err != nil {
			return Message{}, fmt.Errorf("ccm: %s payload: %w", env.Type, err)
		}
		m.Status, m.Text, m.SubscriptionID = r.Status, r.Message, r.SubscriptionID
	case MsgEvents, MsgEvent:
		if err := json.Unmarshal(env.Payload, &m.Events); err != nil {
			return Message{}, fmt.Errorf("ccm: %s payload: %w", env.Type, err)
		}
	}
	return m, nil
}

// Pointer splits an RFC 6901 JSON Pointer into its reference tokens.
// The empty pointer is the whole document.
func Pointer(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("ccm: JSON pointer %q does not start with /", path)
	}
	segs := strings.Split(path[1:], "/")
	for i, s := range segs {
		// RFC 6901 §4: ~1 first, then ~0, so "~01" stays "~1".
		segs[i] = strings.ReplaceAll(strings.ReplaceAll(s, "~1", "/"), "~0", "~")
	}
	return segs, nil
}

// ApplyPatch applies one operation to doc and returns the document that
// results. doc is what encoding/json decodes into an `any`.
//
// A patch that does not fit the document it is applied to is an error
// and never a guess: §13.4.1 has the client discard a model that may
// have drifted and repopulate it.
func ApplyPatch(doc any, op PatchOp) (any, error) {
	switch op.Op {
	case OpAdd, OpRemove, OpReplace:
	default:
		return doc, fmt.Errorf("ccm: JSON patch operation %q is not one of add, remove, replace", op.Op)
	}
	segs, err := Pointer(op.Path)
	if err != nil {
		return doc, err
	}
	out, err := patchAt(doc, segs, op)
	if err != nil {
		return doc, fmt.Errorf("ccm: JSON patch %s %q: %w", op.Op, op.Path, err)
	}
	return out, nil
}

func patchAt(node any, segs []string, op PatchOp) (any, error) {
	if len(segs) == 0 {
		if op.Op == OpRemove {
			return nil, nil
		}
		return op.Value, nil
	}
	seg, last := segs[0], len(segs) == 1
	switch t := node.(type) {
	case map[string]any:
		child, exists := t[seg]
		if !exists && (!last || op.Op != OpAdd) {
			return node, fmt.Errorf("no member %q", seg)
		}
		if last && op.Op == OpRemove {
			delete(t, seg)
			return t, nil
		}
		v, err := patchAt(child, segs[1:], op)
		if err != nil {
			return node, err
		}
		t[seg] = v
		return t, nil
	case []any:
		if last && op.Op == OpAdd {
			i := len(t)
			if seg != "-" {
				var err error
				if i, err = index(seg, len(t)+1); err != nil {
					return node, err
				}
			}
			t = append(t, nil)
			copy(t[i+1:], t[i:])
			t[i] = op.Value
			return t, nil
		}
		i, err := index(seg, len(t))
		if err != nil {
			return node, err
		}
		if last && op.Op == OpRemove {
			return append(t[:i], t[i+1:]...), nil
		}
		v, err := patchAt(t[i], segs[1:], op)
		if err != nil {
			return node, err
		}
		t[i] = v
		return t, nil
	}
	return node, fmt.Errorf("%q is inside a value, not an object or an array", seg)
}

// index reads an array reference token and bounds it to [0, n).
func index(seg string, n int) (int, error) {
	i, err := strconv.Atoi(seg)
	if err != nil || i < 0 || i >= n {
		return 0, fmt.Errorf("no element %q in an array of %d", seg, n)
	}
	return i, nil
}
