package emberplus

import (
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/emberplus/codec/glow"
)

// TestResolveTemplate_NilPath covers the empty-path early return.
func TestResolveTemplate_NilPath(t *testing.T) {
	p := freshPlugin()
	if got := p.ResolveTemplate(nil); got != nil {
		t.Errorf("ResolveTemplate(nil) = %v, want nil", got)
	}
}

// TestDecodeStreamBytes_UnknownFormat covers the default nil return.
func TestDecodeStreamBytes_UnknownFormat(t *testing.T) {
	if got := decodeStreamBytes(9999, []byte{1, 2, 3, 4, 5, 6, 7, 8}); got != nil {
		t.Errorf("unknown format = %v, want nil", got)
	}
}

// TestRegisterNumericPath_Empty covers the empty-path early return.
func TestRegisterNumericPath_Empty(t *testing.T) {
	p := freshPlugin()
	p.registerNumericPath(nil, "x") // no-op, no panic
	if len(p.numPath) != 0 {
		t.Error("empty path must not register")
	}
}

// TestNotifySubscribers_NilGlowParam covers the nil-glowParam guard.
func TestNotifySubscribers_NilGlowParam(t *testing.T) {
	p := freshPlugin()
	p.notifySubscribers(&treeEntry{}) // glowParam nil → early return, no panic
}

// TestResolveNumPath_DeriveWithSession covers resolveNumPath's derive
// branch with a live session attached (SetUseLegacyGlow is invoked).
func TestResolveNumPath_DeriveWithSession(t *testing.T) {
	p := freshPlugin()
	p.session = NewSession(discardLogger())
	// Non-qualified element (no explicit path) → derive from parent +
	// number, and flip the session to legacy glow.
	out := p.resolveNumPath(nil, []int32{1}, 2)
	if len(out) != 2 || out[0] != 1 || out[1] != 2 {
		t.Errorf("derived path = %v, want [1 2]", out)
	}
	if !p.session.isLegacyGlow() {
		t.Error("session should be flipped to legacy glow on the derive path")
	}
}

// TestProcessNode_QualifiedTemplateRef feeds a Node carrying a
// templateReference so processNode + nodeMeta's templateReference
// branches run.
func TestProcessNode_QualifiedTemplateRef(t *testing.T) {
	p := freshPlugin()
	p.handleElements([]glow.Element{{Node: &glow.Node{
		Path: []int32{1}, Number: 1, Identifier: "root",
		TemplateReference: []int32{9, 1}, SchemaIdentifiers: "sch", IsOnline: true,
	}}})
	e := p.numIndex["1"]
	if e == nil || e.obj.Meta["templateReference"] != "9.1" {
		t.Errorf("node templateReference not surfaced: %+v", e)
	}
}

// TestWildcardMatches_Filters covers wildcardMatches' --path,
// --no-streams, and --streams-only filter branches.
func TestWildcardMatches_Filters(t *testing.T) {
	p := freshPlugin()
	streamEntry := &treeEntry{
		glowParam:   &glow.Parameter{StreamIdentifier: 1, HasStreamIdentifier: true},
		numericPath: []int32{1, 1},
		obj:         consumer.Object{Path: []string{"root", "meter"}},
	}
	plainEntry := &treeEntry{
		glowParam:   &glow.Parameter{},
		numericPath: []int32{1, 2},
		obj:         consumer.Object{Path: []string{"root", "gain"}},
	}

	// nil entry.
	if p.wildcardMatches(nil) {
		t.Error("nil entry must not match")
	}

	// --no-streams drops stream entries.
	p.SetWildcardSubscribeFilter(nil, true, false)
	if p.wildcardMatches(streamEntry) {
		t.Error("--no-streams should drop a stream entry")
	}
	if !p.wildcardMatches(plainEntry) {
		t.Error("--no-streams should keep a plain entry")
	}

	// --streams-only drops plain entries.
	p.SetWildcardSubscribeFilter(nil, false, true)
	if p.wildcardMatches(plainEntry) {
		t.Error("--streams-only should drop a plain entry")
	}

	// --path filter (non-matching prefix).
	p.SetWildcardSubscribeFilter([]string{"other"}, false, false)
	if p.wildcardMatches(plainEntry) {
		t.Error("--path filter should drop a non-matching entry")
	}
	// matching prefix (label path).
	p.SetWildcardSubscribeFilter([]string{"root.gain"}, false, false)
	if !p.wildcardMatches(plainEntry) {
		t.Error("--path filter should keep a matching label-path entry")
	}
	// matching numeric prefix.
	p.SetWildcardSubscribeFilter([]string{"1.2"}, false, false)
	if !p.wildcardMatches(plainEntry) {
		t.Error("--path filter should keep a matching numeric-path entry")
	}
}
