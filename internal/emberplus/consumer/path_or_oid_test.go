package emberplus

import (
	"testing"

	"dhs/internal/consumer"
)

// TestFindEntry_OIDLookup pins R21 #486 acceptance criterion 1: a seeded
// tree must resolve a request equally well by numeric OID (`1.6.1`) or
// by dotted label (`identity.vInteger`). The cached cache-load + walked
// tree paths share the same `findEntry` resolution so this single
// fixture covers every --path-using verb.
func TestFindEntry_OIDLookup(t *testing.T) {
	p := newSeedTestPlugin()
	p.SeedTreeFromCachedObjects(0, []consumer.Object{{
		OID:   "1.6.1",
		Label: "vInteger",
		Path:  []string{"identity", "types", "vInteger"},
		Meta: map[string]any{
			"element": "parameter",
			"type":    "integer",
		},
		Value: consumer.Value{Kind: consumer.KindInt, Int: 42},
	}})

	// OID form.
	keyOID, entryOID := p.findEntry(consumer.ValueRequest{Path: "1.6.1", ID: -1})
	if entryOID == nil {
		t.Fatal("OID lookup returned nil entry — numIndex never wired for path resolution")
	}
	if keyOID != "1.6.1" {
		t.Errorf("OID lookup key: got %q, want %q", keyOID, "1.6.1")
	}

	// Dotted-label form.
	keyLbl, entryLbl := p.findEntry(consumer.ValueRequest{Path: "identity.types.vInteger", ID: -1})
	if entryLbl == nil {
		t.Fatal("dotted-label lookup returned nil entry")
	}
	if keyLbl != "1.6.1" {
		t.Errorf("dotted-label lookup key: got %q, want %q", keyLbl, "1.6.1")
	}

	// Both lookups must resolve to the SAME tree entry — anything else
	// would mean a duplicate is sitting in the indices, which would
	// rot under cascading announces.
	if entryOID != entryLbl {
		t.Errorf("OID and label resolved to different *treeEntry — duplicate index entries?")
	}
}

// TestFindEntry_OIDNotFound confirms a syntactically-valid but unknown
// OID misses gracefully (callers map nil entry to plugin:object-not-found).
// No panic, no partial resolve.
func TestFindEntry_OIDNotFound(t *testing.T) {
	p := newSeedTestPlugin()
	p.SeedTreeFromCachedObjects(0, []consumer.Object{{
		OID:   "1.6.1",
		Label: "vInteger",
		Path:  []string{"identity", "types", "vInteger"},
		Meta:  map[string]any{"element": "parameter", "type": "integer"},
	}})

	_, entry := p.findEntry(consumer.ValueRequest{Path: "1.99.99", ID: -1})
	if entry != nil {
		t.Errorf("unknown OID 1.99.99 unexpectedly resolved")
	}
}
