package emberplus

import (
	"sync"
	"testing"

	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/consumer"
)

func newFilterTestPlugin() *Plugin {
	return &Plugin{
		numIndex:   make(map[string]*treeEntry),
		pathIndex:  make(map[string]*treeEntry),
		labelIndex: make(map[string][]*treeEntry),
		treeMu:     sync.RWMutex{},
	}
}

// addFilterParam registers a Parameter at numericPath / labelPath with
// the given streamIdentifier (0 = plain, !=0 = stream).
func addFilterParam(p *Plugin, oid []int32, path []string, streamID int64) *treeEntry {
	e := &treeEntry{
		obj: consumer.Object{
			Path:  append([]string(nil), path...),
			Label: path[len(path)-1],
		},
		glowParam: &glow.Parameter{
			Identifier:       path[len(path)-1],
			StreamIdentifier: streamID,
		},
		numericPath: append([]int32(nil), oid...),
	}
	p.numIndex[numericKey(oid)] = e
	return e
}

func TestWildcardMatches_NoFilter(t *testing.T) {
	p := newFilterTestPlugin()
	plain := addFilterParam(p, []int32{1, 2, 3}, []string{"mixers", "primary", "x"}, 0)
	stream := addFilterParam(p, []int32{1, 4, 5}, []string{"mixers", "primary", "meter"}, 7)

	if !p.wildcardMatches(plain) {
		t.Error("no filter — plain Parameter should match")
	}
	if !p.wildcardMatches(stream) {
		t.Error("no filter — stream Parameter should match")
	}
}

func TestWildcardMatches_NoStreams(t *testing.T) {
	p := newFilterTestPlugin()
	p.SetWildcardSubscribeFilter(nil, true, false)
	plain := addFilterParam(p, []int32{1, 2, 3}, []string{"x"}, 0)
	stream := addFilterParam(p, []int32{1, 4, 5}, []string{"y"}, 7)

	if !p.wildcardMatches(plain) {
		t.Error("--no-streams — plain Parameter should match")
	}
	if p.wildcardMatches(stream) {
		t.Error("--no-streams — stream Parameter should NOT match")
	}
}

func TestWildcardMatches_StreamsOnly(t *testing.T) {
	p := newFilterTestPlugin()
	p.SetWildcardSubscribeFilter(nil, false, true)
	plain := addFilterParam(p, []int32{1, 2, 3}, []string{"x"}, 0)
	stream := addFilterParam(p, []int32{1, 4, 5}, []string{"y"}, 7)

	if p.wildcardMatches(plain) {
		t.Error("--streams-only — plain Parameter should NOT match")
	}
	if !p.wildcardMatches(stream) {
		t.Error("--streams-only — stream Parameter should match")
	}
}

func TestWildcardMatches_PathByOID(t *testing.T) {
	p := newFilterTestPlugin()
	p.SetWildcardSubscribeFilter([]string{"1.2.3"}, false, false)
	inSubtree := addFilterParam(p, []int32{1, 2, 3, 5}, []string{"a", "b", "c", "d"}, 0)
	atPrefix := addFilterParam(p, []int32{1, 2, 3}, []string{"a", "b", "c"}, 0)
	outside := addFilterParam(p, []int32{1, 4, 5}, []string{"e", "f", "g"}, 0)

	if !p.wildcardMatches(inSubtree) {
		t.Error("--path 1.2.3 — descendant 1.2.3.5 should match")
	}
	if !p.wildcardMatches(atPrefix) {
		t.Error("--path 1.2.3 — exact OID match should hit")
	}
	if p.wildcardMatches(outside) {
		t.Error("--path 1.2.3 — outside OID 1.4.5 should NOT match")
	}
}

func TestWildcardMatches_PathByLabel(t *testing.T) {
	p := newFilterTestPlugin()
	p.SetWildcardSubscribeFilter([]string{"mixers.primary.channels"}, false, false)
	in := addFilterParam(p, []int32{2, 2, 2, 1, 6, 3, 1},
		[]string{"mixers", "primary", "channels", "inputs", "input-6", "metering", "main"}, 0)
	out := addFilterParam(p, []int32{2, 1, 1},
		[]string{"mixers", "global", "identity"}, 0)

	if !p.wildcardMatches(in) {
		t.Error("--path mixers.primary.channels — descendant should match")
	}
	if p.wildcardMatches(out) {
		t.Error("--path mixers.primary.channels — sibling subtree should NOT match")
	}
}

func TestWildcardMatches_PathList(t *testing.T) {
	p := newFilterTestPlugin()
	p.SetWildcardSubscribeFilter([]string{"1.2", "mixers.global"}, false, false)
	a := addFilterParam(p, []int32{1, 2, 7}, []string{"x", "y", "z"}, 0)
	b := addFilterParam(p, []int32{9, 9}, []string{"mixers", "global", "id"}, 0)
	c := addFilterParam(p, []int32{3, 4, 5}, []string{"foo", "bar", "baz"}, 0)

	if !p.wildcardMatches(a) {
		t.Error("path list — OID-match should hit")
	}
	if !p.wildcardMatches(b) {
		t.Error("path list — label-match should hit")
	}
	if p.wildcardMatches(c) {
		t.Error("path list — unmatched should NOT hit")
	}
}

func TestWildcardMatches_PathWithNoStreams(t *testing.T) {
	p := newFilterTestPlugin()
	p.SetWildcardSubscribeFilter([]string{"1.2"}, true, false)
	plainIn := addFilterParam(p, []int32{1, 2, 3}, []string{"x"}, 0)
	streamIn := addFilterParam(p, []int32{1, 2, 4}, []string{"y"}, 7)
	plainOut := addFilterParam(p, []int32{9, 9}, []string{"z"}, 0)

	if !p.wildcardMatches(plainIn) {
		t.Error("--path 1.2 + --no-streams — plain Param in subtree should match")
	}
	if p.wildcardMatches(streamIn) {
		t.Error("--path 1.2 + --no-streams — stream Param in subtree should NOT match")
	}
	if p.wildcardMatches(plainOut) {
		t.Error("--path 1.2 + --no-streams — plain Param outside subtree should NOT match")
	}
}

func TestWildcardMatches_PathWithStreamsOnly(t *testing.T) {
	p := newFilterTestPlugin()
	p.SetWildcardSubscribeFilter([]string{"mixers.primary.channels"}, false, true)
	streamIn := addFilterParam(p, []int32{2, 2, 2, 1, 6, 3, 1},
		[]string{"mixers", "primary", "channels", "inputs", "input-6", "metering", "main"}, 4242)
	plainIn := addFilterParam(p, []int32{2, 2, 2, 1, 7, 2, 1},
		[]string{"mixers", "primary", "channels", "inputs", "input-7", "labels", "name"}, 0)
	streamOut := addFilterParam(p, []int32{9, 9}, []string{"y"}, 99)

	if !p.wildcardMatches(streamIn) {
		t.Error("--path + --streams-only — stream Param in subtree should match")
	}
	if p.wildcardMatches(plainIn) {
		t.Error("--path + --streams-only — plain Param in subtree should NOT match")
	}
	if p.wildcardMatches(streamOut) {
		t.Error("--path + --streams-only — stream Param outside subtree should NOT match")
	}
}
