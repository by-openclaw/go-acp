package registry

// The wire form of a resource at a minor, kept from one use to the next.
//
// A SYNC grain and a Query listing both hand out every resource of a
// topic encoded at the URL's wire version, and encoding runs the codec's
// schema validation per resource. On the plant that was 20–30 s per
// topic for every new subscription (issue #1281) — for bytes that do not
// change between one subscriber and the next. Resources were validated
// when they were ingested; a subscriber needs the bytes, not a second
// verdict.
//
// The cache needs no invalidation plumbing: an entry remembers the
// canonical form it was encoded from, and a lookup re-marshals the
// struct (cheap) and compares. A changed resource misses and is encoded
// again; an unchanged one is served as it was. Keyed by type, id and
// wire version, bounded by resources × minors.

import (
	"crypto/sha256"
	"encoding/json"
	"sync"

	"dhs/internal/amwa/codec/is04"
)

type encodedKey struct {
	t   is04.ResourceType
	id  string
	ver string
}

type encodedEntry struct {
	sum   [32]byte // of the canonical JSON the bytes were encoded from
	bytes []byte
}

// encodedForms is the cache; its own lock, never the store's.
type encodedForms struct {
	mu      sync.Mutex
	entries map[encodedKey]encodedEntry
	hits    uint64
	misses  uint64
}

func newEncodedForms() *encodedForms {
	return &encodedForms{entries: make(map[encodedKey]encodedEntry)}
}

// encodeOne is what a miss costs: the codec at wireVer, or the canonical
// form when no codec is registered for it or it refuses the resource
// (one that does not fit that minor), as the Query API has always done.
func encodeOne(t is04.ResourceType, v any, wireVer string) []byte {
	if b, ok := encodeForVersion(t, v, wireVer); ok {
		return b
	}
	b, _ := json.Marshal(v)
	return b
}

// get is the wire form of v at wireVer: cached when v is what it was,
// encoded and remembered when it is not.
func (c *encodedForms) get(t is04.ResourceType, id string, v any, wireVer string) []byte {
	canon, err := json.Marshal(v)
	if err != nil {
		return encodeOne(t, v, wireVer)
	}
	sum := sha256.Sum256(canon)
	key := encodedKey{t: t, id: id, ver: wireVer}

	c.mu.Lock()
	e, ok := c.entries[key]
	if ok && e.sum == sum {
		c.hits++
		c.mu.Unlock()
		return e.bytes
	}
	c.misses++
	c.mu.Unlock()

	b := encodeOne(t, v, wireVer)
	c.mu.Lock()
	c.entries[key] = encodedEntry{sum: sum, bytes: b}
	c.mu.Unlock()
	return b
}

// forget drops every minor's form of one resource: what a delete owes
// the cache so it cannot outgrow the store.
func (c *encodedForms) forget(t is04.ResourceType, id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.entries {
		if k.t == t && k.id == id {
			delete(c.entries, k)
		}
	}
}

// stats reports hits and misses, for the metrics line and the tests.
func (c *encodedForms) stats() (hits, misses uint64, size int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.misses, len(c.entries)
}
