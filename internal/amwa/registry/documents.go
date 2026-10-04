package registry

// The document a resource was registered with.
//
// The store reasons with typed values — filters, ancestry, referential
// integrity — and used to hand those back re-encoded: a key the Node
// had not sent appeared with its zero value (`"authorization": false`
// on a service), and a key the types do not model was gone. A registry
// hands back what was registered. So the body a Node sends is kept
// beside the typed value, and it is what the Query API serves:
//
//   - at the minor the resource is registered at, and at a later minor
//     under query.downgrade: the document itself;
//   - at an earlier minor: the document with the keys that minor does
//     not know removed (translate.go — IS-04 "Upgrade Path").
//
// A resource that did not come through the Registration API (a typed
// Put) has no document, and is encoded from its typed value as before.

import (
	"bytes"
	"encoding/json"
	"reflect"
	"time"

	"dhs/internal/amwa/codec/is04"
)

// documentArrives stages the body of a registration for the typed Put
// that follows: fanOut files it when the Put announces the change.
func (s *Store) documentArrives(t is04.ResourceType, id string, body json.RawMessage) {
	var compact bytes.Buffer
	if id == "" || json.Compact(&compact, body) != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.arriving[t] == nil {
		s.arriving[t] = make(map[string]json.RawMessage)
	}
	s.arriving[t][id] = compact.Bytes()
}

// documentWithdrawn drops a staged body whose registration was refused.
func (s *Store) documentWithdrawn(t is04.ResourceType, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.arriving[t], id)
}

// documentSettled is the end of an accepted registration. A staged body
// still there means the typed Put found nothing changed and announced
// nothing — the types compare equal. The document can still differ, in
// a key the types do not model: then it replaces the one on file and
// the change is announced, as the update it is.
func (s *Store) documentSettled(t is04.ResourceType, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, staged := s.arriving[t][id]
	if !staged {
		return
	}
	old := s.documents[t][id]
	if sameDocument(old, body) {
		delete(s.arriving[t], id)
		return
	}
	s.markUpdated(t, id)
	post, _ := json.Marshal(s.typedLocked(t, id))
	c := Change{Kind: ChangeUpdated, ResourceType: t, ID: id, APIVer: s.apiVerOfLocked(t, id), Pre: post, Post: post, Timestamp: time.Now()}
	if old == nil {
		// The first document of a resource that had none: nothing
		// changed for a subscriber, the document is only put on file.
		s.fileDocument(&c)
		return
	}
	s.fanOut(c)
}

// fileDocument gives a change the registered documents it is about, and
// keeps the file in step: a created or updated resource takes the body
// that arrived with it (or loses its document, when a typed Put brought
// none), a deleted one leaves the file.
func (s *Store) fileDocument(c *Change) {
	t, id := c.ResourceType, c.ID
	old := s.documents[t][id]
	switch c.Kind {
	case ChangeCreated, ChangeUpdated:
		if c.Kind == ChangeUpdated {
			c.RawPre = old
		}
		body, staged := s.arriving[t][id]
		if !staged {
			delete(s.documents[t], id)
			return
		}
		delete(s.arriving[t], id)
		if s.documents[t] == nil {
			s.documents[t] = make(map[string]json.RawMessage)
		}
		s.documents[t][id] = body
		c.RawPost = body
	case ChangeDeleted:
		c.RawPre = old
		delete(s.documents[t], id)
	}
}

// typedLocked is the typed value of one resource. Caller holds mu.
func (s *Store) typedLocked(t is04.ResourceType, id string) any {
	switch t {
	case is04.ResourceNode:
		return s.nodes[id]
	case is04.ResourceDevice:
		return s.devices[id]
	case is04.ResourceSource:
		return s.sources[id]
	case is04.ResourceFlow:
		return s.flows[id]
	case is04.ResourceSender:
		return s.senders[id]
	}
	return s.receivers[id]
}

// Document is what the Query API serves of one resource at wireVer: its
// registered document, as that minor shows it. Nil when the resource
// has no document on file.
func (s *Store) Document(t is04.ResourceType, id, wireVer string) json.RawMessage {
	s.mu.RLock()
	doc, registered := s.documents[t][id], s.apiVerOfLocked(t, id)
	s.mu.RUnlock()
	return documentAt(t, doc, registered, wireVer)
}

// documentAt is doc, registered at one minor, as wireVer shows it: the
// document itself, or its translation to an earlier minor. Nil in, nil
// out.
func documentAt(t is04.ResourceType, doc json.RawMessage, registered, wireVer string) json.RawMessage {
	if doc == nil {
		return nil
	}
	if registered != "" && wireVer != "" && minorLess(wireVer, registered) {
		return translateDown(t, doc, registered, wireVer)
	}
	return doc
}

// sameDocument reports two bodies that say the same thing: the same
// bytes, or the same JSON in another order of keys.
func sameDocument(a, b json.RawMessage) bool {
	if bytes.Equal(a, b) {
		return true
	}
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

// documentsOf turns a page of typed resources into what the Query API
// serves at wireVer: each one's registered document, or — for one with
// no document on file — the typed value itself, encoded as it always
// was.
func (s *Store) documentsOf(t is04.ResourceType, items any, wireVer string) any {
	rv := reflect.ValueOf(items)
	if rv.Kind() != reflect.Slice {
		return items
	}
	out := make([]any, rv.Len())
	// The lock is held to read the file, not to translate: a page of a
	// thousand documents conformed to an earlier minor is work the
	// registration writers must not wait behind.
	docs := make([]json.RawMessage, rv.Len())
	registered := make([]string, rv.Len())
	s.mu.RLock()
	for i := range out {
		id := readJSONField(rv.Index(i), "id")
		docs[i], registered[i] = s.documents[t][id], s.apiVerOfLocked(t, id)
	}
	s.mu.RUnlock()
	filed := false
	for i := range out {
		if doc := documentAt(t, docs[i], registered[i], wireVer); doc != nil {
			out[i], filed = doc, true
			continue
		}
		out[i] = rv.Index(i).Interface()
	}
	if !filed {
		return items // nothing on file: the typed slice, as before
	}
	return out
}
