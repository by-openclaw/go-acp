// Package storage provides file-backed persistence for walked object
// trees. The cache file uses the EXACT same format as `acp export
// --format json` (hierarchical tree) with values stripped.
//
// File layout (relative to the binary):
//
//	devices/{ip}/slot_{n}.json
//
// On load, the store validates against the live device Card Name.
// If the card was swapped, the cache is discarded.
package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"acp/internal/export"
	"acp/internal/protocol"
)

// TreeStore manages cached tree files on disk.
type TreeStore struct {
	baseDir string
}

// NewTreeStore creates a store rooted at the given directory.
func NewTreeStore(baseDir string) *TreeStore {
	return &TreeStore{baseDir: baseDir}
}

// NewTreeStoreNextToBinary creates a store rooted at the directory
// containing the running binary.
func NewTreeStoreNextToBinary() (*TreeStore, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("storage: locate binary: %w", err)
	}
	return NewTreeStore(filepath.Dir(exe)), nil
}

// slotPath returns the file path for a cached slot.
// Keyed by {ip}_{protocol} to avoid collisions when multiple protocols
// serve on the same host (e.g. ACP1 on :2071, Ember+ on :9092).
func (s *TreeStore) slotPath(ip string, slot int) string {
	return filepath.Join(s.baseDir, "devices", ip, fmt.Sprintf("slot_%d.json", slot))
}


// Save writes a walked tree to disk using the same hierarchical JSON
// format as `acp export --format json`. Values are stripped before
// writing — per CLAUDE.md, property values are NEVER written to disk.
func (s *TreeStore) Save(ip, proto string, slot int, objs []protocol.Object) error {
	// Strip values from objects.
	stripped := make([]protocol.Object, len(objs))
	for i, o := range objs {
		stripped[i] = o
		stripped[i].Value = protocol.Value{} // no values on disk
	}

	// Build a Snapshot — same as export.
	snap := &export.Snapshot{
		Device: export.DeviceInfo{
			IP:       ip,
			Protocol: proto,
		},
		Generator: "acp cache",
		CreatedAt: time.Now().UTC(),
		Slots: []export.SlotDump{{
			Slot:     slot,
			WalkedAt: time.Now().UTC(),
			Objects:  stripped,
		}},
	}

	// Write atomically: tmp file then rename.
	path := s.slotPath(ip, slot)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("storage: mkdir %s: %w", dir, err)
	}

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("storage: create %s: %w", tmp, err)
	}

	if err := export.WriteJSON(f, snap); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("storage: write: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("storage: close: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("storage: rename: %w", err)
	}
	return nil
}

// Load reads a cached slot from disk using the standard JSON reader.
// Returns nil, nil if the file does not exist (cache miss).
func (s *TreeStore) Load(ip string, slot int) (*export.Snapshot, error) {
	path := s.slotPath(ip, slot)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("storage: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	snap, err := export.ReadJSON(f)
	if err != nil {
		return nil, fmt.Errorf("storage: decode %s: %w", path, err)
	}
	return snap, nil
}

// FindCardName extracts the Card Name from a list of objects.
// Used for identity validation.
func FindCardName(objs []protocol.Object) string {
	for _, o := range objs {
		if o.Label == "Card Name" && o.Value.Kind == protocol.KindString {
			return o.Value.Str
		}
	}
	return ""
}

// Validate checks whether a cached snapshot matches the live device
// by comparing Card Name. Returns true if the cache is valid.
func Validate(snap *export.Snapshot, liveCardName string) bool {
	if snap == nil || len(snap.Slots) == 0 {
		return false
	}
	cachedName := FindCardName(snap.Slots[0].Objects)
	if cachedName == "" || liveCardName == "" {
		return false
	}
	return cachedName == liveCardName
}

// Delete removes the cached file for a slot.
func (s *TreeStore) Delete(ip string, slot int) error {
	path := s.slotPath(ip, slot)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("storage: remove %s: %w", path, err)
	}
	return nil
}
