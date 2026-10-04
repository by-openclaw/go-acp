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
// minor. found is false when that minor does not list it; err is a
// source that did not answer, which says nothing either way.
func (m *Mirror) sourceDoc(ctx context.Context, topic, ver, id string, extra map[string]string) (doc json.RawMessage, found bool, err error) {
	qc, ok := m.sourceClients[ver]
	if !ok {
		return nil, false, nil
	}
	filter := map[string]string{"id": id}
	for k, v := range extra {
		filter[k] = v
	}
	docs, err := qc.ListRaw(ctx, topic, filter)
	if err != nil {
		return nil, false, err
	}
	if len(docs) != 1 {
		return nil, false, nil
	}
	return docs[0], true, nil
}

// registeredAt returns the minor a resource first seen on the ver
// subscription is registered at, and its document there: the highest
// minor above ver that lists it, or ver itself when none does. known is
// false when the source did not answer: the row is then not enough to
// place the resource — placed at ver it could go to the target as a
// lower minor's view of itself.
func (m *Mirror) registeredAt(ctx context.Context, topic, ver, id string, doc json.RawMessage) (reg string, at json.RawMessage, known bool) {
	for _, higher := range m.sourceMinors() {
		if !minorLess(ver, higher) {
			break
		}
		found, ok, err := m.sourceDoc(ctx, topic, higher, id, nil)
		if err != nil {
			return "", nil, false
		}
		if ok {
			return higher, found, true
		}
	}
	return ver, doc, true
}

// registeredNow says where a resource is registered at this moment: the
// highest minor that lists it. It is asked when a resource leaves the
// minor it was tracked at while a lower minor showed it too — it may be
// gone, registered again at the same minor (a Node that restarted), or
// registered at another one. A lower minor that still shows it proves
// nothing by itself: a source that translates shows there a resource
// registered above.
//
// One question settles the ordinary case, a resource that is simply
// gone: does the highest minor list it when asked to include every
// earlier one? Only a yes is followed down, minor by minor.
func (m *Mirror) registeredNow(ctx context.Context, topic, id string) (string, json.RawMessage, bool) {
	minors := m.sourceMinors()
	anywhere := map[string]string{"query.downgrade": minors[len(minors)-1]}
	if _, there, _ := m.sourceDoc(ctx, topic, minors[0], id, anywhere); !there {
		return "", nil, false
	}
	for _, ver := range minors {
		if doc, ok, _ := m.sourceDoc(ctx, topic, ver, id, nil); ok {
			return ver, doc, true
		}
	}
	return "", nil, false
}
