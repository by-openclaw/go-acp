package registry

// The minor a resource is registered at, read from a source that
// translates.
//
// IS-04 "Upgrade Path", Requirements for Registries: a Query API that
// serves several minors MUST show a resource registered at a higher
// minor on its lower endpoints too, with the keys the lower minor does
// not know removed. nmos-cpp does. A resource registered at v1.3 then
// arrives on the v1.0, v1.1, v1.2 and v1.3 subscriptions as four
// documents, and only the last is the resource: the others are views of
// it. A grain row carries no version stamp, so the minor a resource is
// registered at is read off the wire as the HIGHEST minor that shows it.
//
// A source that shows a resource on its own minor only gives the same
// answer: one minor shows it, and that one is the highest.

import (
	"context"
	"encoding/json"
	"sort"
)

// minorLess reports a < b for two "vMAJOR.MINOR" strings.
func minorLess(a, b string) bool { return a != b && apiVerLE(a, b) }

// sourceMinors lists the minors the mirror reads the source at, highest
// first.
func (m *Mirror) sourceMinors() []string {
	vers := append([]string(nil), mirrorSourceVersions(m.opts.APIVer)...)
	sort.Slice(vers, func(i, j int) bool { return minorLess(vers[j], vers[i]) })
	return vers
}

// sourceDoc reads one resource from the source's Query API at one
// minor. Anything but exactly that resource — a minor with no client,
// a source that does not answer, an empty list — is "not there".
func (m *Mirror) sourceDoc(ctx context.Context, topic, ver, id string, extra map[string]string) (json.RawMessage, bool) {
	qc, ok := m.sourceClients[ver]
	if !ok {
		return nil, false
	}
	filter := map[string]string{"id": id}
	for k, v := range extra {
		filter[k] = v
	}
	docs, err := qc.ListRaw(ctx, topic, filter)
	if err != nil || len(docs) != 1 {
		return nil, false
	}
	return docs[0], true
}

// registeredAt returns the minor a resource first seen on the ver
// subscription is registered at, and its document there: the highest
// minor above ver that lists it, or ver itself when none does.
func (m *Mirror) registeredAt(ctx context.Context, topic, ver, id string, doc json.RawMessage) (string, json.RawMessage) {
	for _, higher := range m.sourceMinors() {
		if !minorLess(ver, higher) {
			break
		}
		if found, ok := m.sourceDoc(ctx, topic, higher, id, nil); ok {
			return higher, found
		}
	}
	return ver, doc
}

// registeredBelow looks for a resource that left the minor it was
// registered at while a lower minor went on showing it. A Node that
// changes minor deletes its resources and registers them again, and the
// two subscriptions race: the lower one's row can come first and be
// taken for a view. One question settles the ordinary case, a resource
// that is simply gone — is it registered at any minor below? — and only
// a yes is followed down to the minor.
func (m *Mirror) registeredBelow(ctx context.Context, topic, ver, id string) (string, json.RawMessage, bool) {
	minors := m.sourceMinors()
	asked := false
	for _, lower := range minors {
		if !minorLess(lower, ver) {
			continue
		}
		if !asked {
			asked = true
			anyBelow := map[string]string{"query.downgrade": minors[len(minors)-1]}
			if _, there := m.sourceDoc(ctx, topic, lower, id, anyBelow); !there {
				return "", nil, false
			}
		}
		if doc, ok := m.sourceDoc(ctx, topic, lower, id, nil); ok {
			return lower, doc, true
		}
	}
	return "", nil, false
}
