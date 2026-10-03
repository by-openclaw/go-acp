package registry

import (
	"bytes"
	"fmt"
	"testing"

	"dhs/internal/amwa/codec/is04"
)

// The wire form of an unchanged resource is served from the cache; a
// changed one is encoded again; a deleted one is forgotten; a minor no
// codec serves falls back to the canonical form.
func TestEncodedFormsServeUnchangedResourcesFromTheCache(t *testing.T) {
	s := NewStore()
	const id = "11111111-1111-4111-8111-111111111111"
	n := validNode(id)
	if err := s.PutNode(n); err != nil {
		t.Fatal(err)
	}

	first := s.SnapshotChangesFor("v1.3", is04.ResourceNode)
	second := s.SnapshotChangesFor("v1.3", is04.ResourceNode)
	if len(first) != 1 || len(second) != 1 || !bytes.Equal(first[0].Post, second[0].Post) {
		t.Fatalf("the same resource twice = %d/%d grains, bytes equal %v", len(first), len(second), bytes.Equal(first[0].Post, second[0].Post))
	}
	hits, misses, size := s.EncodedFormStats()
	if hits != 1 || misses != 1 || size != 1 {
		t.Fatalf("after two snapshots: hits=%d misses=%d size=%d, want 1/1/1", hits, misses, size)
	}

	// Another minor is its own entry.
	s.SnapshotChangesFor("v1.2", is04.ResourceNode)
	if _, _, size := s.EncodedFormStats(); size != 2 {
		t.Fatalf("a second minor adds an entry, size=%d", size)
	}

	// A change misses and replaces the entry; the bytes differ.
	n.Label = "renamed"
	if err := s.PutNode(n); err != nil {
		t.Fatal(err)
	}
	third := s.SnapshotChangesFor("v1.3", is04.ResourceNode)
	if bytes.Equal(first[0].Post, third[0].Post) || !bytes.Contains(third[0].Post, []byte("renamed")) {
		t.Fatalf("a changed resource must be re-encoded: %s", third[0].Post)
	}
	if hits, misses, size := s.EncodedFormStats(); hits != 1 || misses != 3 || size != 2 {
		t.Fatalf("after the change: hits=%d misses=%d size=%d, want 1/3/2", hits, misses, size)
	}

	// Deleting the resource forgets every minor's form.
	s.DeleteNode(id)
	if _, _, size := s.EncodedFormStats(); size != 0 {
		t.Fatalf("a deleted resource leaves the cache, size=%d", size)
	}

	// A minor without a codec: the canonical form, still cached.
	if err := s.PutNode(validNode(id)); err != nil {
		t.Fatal(err)
	}
	got := s.SnapshotChangesFor("v9.9", is04.ResourceNode)
	if len(got) != 1 || !bytes.Contains(got[0].Post, []byte(id)) {
		t.Fatalf("no codec for v9.9: canonical form expected, got %s", got[0].Post)
	}
	// A value that cannot be marshalled is encoded straight through.
	if b := s.encoded.get(is04.ResourceNode, "x", make(chan int), "v1.3"); b != nil {
		t.Fatalf("an unmarshallable value yields nothing, got %q", b)
	}
}

// What a SYNC grain of a plant-sized topic costs with and without the
// cache: the first snapshot encodes every sender through the codec,
// the second serves the same bytes.
func BenchmarkSnapshotSendersCached(b *testing.B) {
	s := NewStore()
	const node = "11111111-1111-4111-8111-111111111111"
	const dev = "22222222-2222-4222-8222-222222222222"
	if err := s.PutNode(validNode(node)); err != nil {
		b.Fatal(err)
	}
	if err := s.PutDevice(validDevice(dev, node)); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 1700; i++ {
		src := validSource(fmt.Sprintf("33333333-3333-4333-8333-%012d", i), dev)
		if err := s.PutSource(src); err != nil {
			b.Fatal(err)
		}
		fl := validFlow(fmt.Sprintf("44444444-4444-4444-8444-%012d", i), src.ID, dev)
		if err := s.PutFlow(fl); err != nil {
			b.Fatal(err)
		}
		snd := validSender(fmt.Sprintf("55555555-5555-4555-8555-%012d", i), dev)
		snd.FlowID = &fl.ID
		if err := s.PutSender(snd); err != nil {
			b.Fatal(err)
		}
	}
	b.Run("first", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			s.encoded = newEncodedForms()
			s.SnapshotChangesFor("v1.3", is04.ResourceSender)
		}
	})
	b.Run("cached", func(b *testing.B) {
		s.SnapshotChangesFor("v1.3", is04.ResourceSender)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			s.SnapshotChangesFor("v1.3", is04.ResourceSender)
		}
	})
}
