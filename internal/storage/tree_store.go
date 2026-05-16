// Package storage provides file-backed persistence for walked object
// trees. The cache file uses the EXACT same format as `dhs export
// --format json` (hierarchical tree) with values stripped.
//
// File layout (relative to the project cache root, per ADR-0020 Bucket 4):
//
//	.cache/devices/{ip}/slot_{n}.json
//
// On load, the store validates against the live device Card Name.
// If the card was swapped, the cache is discarded.
package storage

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dhs/internal/export"
	"dhs/internal/export/canonical"
	"dhs/internal/consumer"
)

// TreeStore manages cached tree files on disk.
type TreeStore struct {
	baseDir string
}

// NewTreeStore creates a store rooted at the given directory.
func NewTreeStore(baseDir string) *TreeStore {
	return &TreeStore{baseDir: baseDir}
}

// BaseDir returns the on-disk root the store writes under (typically
// ".cache" next to the binary). Callers that need to compose sibling
// paths — e.g. ".cache/audit/<proto>/..." next to ".cache/dm/<proto>/..."
// — use this accessor rather than reaching for the unexported field.
func (s *TreeStore) BaseDir() string {
	if s == nil {
		return ""
	}
	return s.baseDir
}

// NewTreeStoreInProjectCache creates a store rooted at .cache/ next to
// the project (or install) root. Per ADR-0020 Bucket 4: cache is
// gitignored and regeneratable, separate from manual captures.
//
// Path resolution:
//
//   - if the binary lives at <X>/bin/dhs[.exe] (dev / convention layout),
//     cache root is <X>/.cache/  — keeping cache OUT of bin/.
//   - otherwise (production, dropped binary), cache root is
//     <binary-dir>/.cache/.
func NewTreeStoreInProjectCache() (*TreeStore, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("storage: locate binary: %w", err)
	}
	parent := filepath.Dir(exe)
	base := parent
	if filepath.Base(parent) == "bin" {
		base = filepath.Dir(parent)
	}
	return NewTreeStore(filepath.Join(base, ".cache")), nil
}

// slotPath returns the file path for a cached slot.
// Keyed by {ip}_{protocol} to avoid collisions when multiple protocols
// serve on the same host (e.g. ACP1 on :2071, Ember+ on :9092).
func (s *TreeStore) slotPath(ip string, slot int) string {
	return filepath.Join(s.baseDir, "devices", ip, fmt.Sprintf("slot_%d.json", slot))
}


// Save writes a walked tree to disk using the same hierarchical JSON
// format as `dhs export --format json`. Values are stripped before
// writing — per CLAUDE.md, property values are NEVER written to disk.
func (s *TreeStore) Save(ip, proto string, slot int, objs []consumer.Object) error {
	// Strip values from objects.
	stripped := make([]consumer.Object, len(objs))
	for i, o := range objs {
		stripped[i] = o
		stripped[i].Value = consumer.Value{} // no values on disk
	}

	// Build a Snapshot — same as export.
	snap := &export.Snapshot{
		Device: export.DeviceInfo{
			IP:       ip,
			Protocol: proto,
		},
		Generator: "dhs cache",
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

// identityPath returns the file path for an identity-keyed cache.
// Identity strings come straight from the consumer (e.g.
// "SHPRM1@0.7"); we sanitise to keep them filesystem-safe across
// Windows + POSIX. Per #424, files now live under a per-protocol
// subfolder so ACP1 / ACP2 / Ember+ caches don't collide on
// identical Model strings.
func (s *TreeStore) identityPath(proto, identity string) string {
	return filepath.Join(s.baseDir, "dm", sanitizeSeg(proto), sanitizeSeg(identity)+".json")
}

// legacyIdentityPath returns the pre-#424 file path (no <proto>/
// subfolder). Kept so LoadByIdentity can fall back to caches written
// by older versions of dhs.
func (s *TreeStore) legacyIdentityPath(identity string) string {
	return filepath.Join(s.baseDir, "dm", sanitizeSeg(identity)+".json")
}

// sanitizeSeg strips characters that are illegal in filenames on
// Windows + POSIX so identity strings + proto names go to disk safely.
func sanitizeSeg(s string) string {
	for _, ch := range []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"} {
		s = strings.ReplaceAll(s, ch, "_")
	}
	return s
}

// DM is the on-disk format for an identity-keyed cache file.
//
// Per DHS 2016 MasterView contract (refs #430): the DM is a per-card
// schema — slot-agnostic, host-agnostic, instance-agnostic. The file
// describes ONE card type. Two slots holding the same card load the
// same DM file; two cards = two DM files. IP, port, slot number, host
// info NEVER live in a DM.
//
// Identity = "<Model>@<SwRev>" (split on the last '@'). For ACP1 the
// Identity Object Group's CardName + Software rev provide both;
// ACP2 maps Card Name + Product Version equivalently.
type DM struct {
	Model    string `json:"model"`
	SwRev    string `json:"sw_rev"`
	Protocol string `json:"protocol"`
	// Root is the canonical-tree root element — the same JSON layout
	// `dhs export --format json` outputs and `dhs producer --tree
	// X.json` reads. Preferred for any protocol whose plugin satisfies
	// ExportCanonical (Ember+ today; acp1/acp2 follow-up). When
	// non-nil, readers consume Root and ignore Objects.
	Root canonical.Element `json:"root,omitempty"`
	// Templates carries the canonical-tree TemplateEntry list when
	// the source provider exposes Glow templates (Ember+ §p.54-58).
	Templates []*canonical.TemplateEntry `json:"templates,omitempty"`
	// Objects is the legacy flat consumer.Object slice — ACP1/ACP2 use
	// this exclusively today. Ember+ no longer writes it: the
	// canonical Root supersedes (refs #438). Kept on the struct so
	// legacy ACP1/ACP2 callers keep working until they migrate.
	Objects []consumer.Object `json:"objects,omitempty"`
}

// UnmarshalJSON dispatches DM.Root through canonical.UnmarshalElement
// (Element is an interface — concrete type can't be inferred from the
// JSON alone, needs the same structural peek the canonical package
// already uses for nested children).
func (d *DM) UnmarshalJSON(data []byte) error {
	type alias struct {
		Model     string                     `json:"model"`
		SwRev     string                     `json:"sw_rev"`
		Protocol  string                     `json:"protocol"`
		Root      json.RawMessage            `json:"root"`
		Templates []*canonical.TemplateEntry `json:"templates"`
		Objects   []consumer.Object          `json:"objects"`
	}
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	d.Model = raw.Model
	d.SwRev = raw.SwRev
	d.Protocol = raw.Protocol
	d.Templates = raw.Templates
	d.Objects = raw.Objects
	if len(raw.Root) > 0 && string(raw.Root) != "null" {
		el, err := canonical.UnmarshalElement(raw.Root)
		if err != nil {
			return fmt.Errorf("dm root: %w", err)
		}
		d.Root = el
	}
	return nil
}

// splitIdentity breaks "<Model>@<SwRev>" on the LAST '@' so a model
// name containing '@' survives. Returns (model, swRev) with swRev = ""
// when there's no '@' in the input.
func splitIdentity(identity string) (string, string) {
	i := strings.LastIndex(identity, "@")
	if i < 0 {
		return identity, ""
	}
	return identity[:i], identity[i+1:]
}

// SaveByIdentity writes the per-card DM to .cache/dm/<proto>/<identity>.json.
//
// On-disk shape is the slot-agnostic DM struct above: {model, sw_rev,
// protocol, objects}. No `device` envelope, no `slots` wrapper, no
// IP/port/num_slots — those are runtime state, not card schema. Per
// DHS 2016, the DM is the schema of ONE card type and survives IP /
// slot / re-cabling changes.
//
// Each Object's Slot field is zeroed before serialization: the DM
// doesn't claim "this object lives in slot N" — slot is a frame
// composition concern, defined by the frame manifest at producer
// startup.
//
// WriteDM persists a fully-assembled DM struct.
//
// Caller contract:
//   - For Ember+ (or any protocol with ExportCanonical): pass Root +
//     Templates from canonical.Export. Objects MUST be nil. The DM
//     file then carries ONLY the canonical hierarchical tree —
//     identity fields at top level, no flat Objects redundancy.
//   - For ACP1 / ACP2 (legacy path): pass Objects. Root nil. The DM
//     file carries the flat consumer.Object slice.
//
// Atomic write same as SaveByIdentity.
func (s *TreeStore) WriteDM(proto, identity string, dm DM) error {
	if proto == "" {
		return fmt.Errorf("storage: WriteDM: empty proto")
	}
	if identity == "" {
		return fmt.Errorf("storage: WriteDM: empty identity")
	}
	if dm.Protocol == "" {
		dm.Protocol = proto
	}
	if dm.Model == "" || dm.SwRev == "" {
		m, r := splitIdentity(identity)
		if dm.Model == "" {
			dm.Model = m
		}
		if dm.SwRev == "" {
			dm.SwRev = r
		}
	}
	// Keep BOTH Root and Objects populated when both are passed:
	// Root is the canonical hierarchical shape (for federation /
	// provider-side serve), Objects is the flat list (for the
	// consumer's existing SeedTreeFromCachedObjects hot-load).
	// Disk redundancy ~few MB; trade-off is worth it because the
	// consumer's per-verb hot-load needs the flat list to seed
	// numIndex without re-walking the canonical tree on every call.
	if dm.Objects != nil {
		clean := make([]consumer.Object, len(dm.Objects))
		for i, o := range dm.Objects {
			clean[i] = o
			clean[i].Slot = 0
		}
		dm.Objects = clean
	}
	return s.writeDMToPath(proto, identity, dm)
}

// writeDMToPath is the shared atomic write used by SaveByIdentity and
// WriteDM. Kept private to centralise the tmp-rename semantics.
func (s *TreeStore) writeDMToPath(proto, identity string, dm DM) error {
	path := s.identityPath(proto, identity)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("storage: mkdir %s: %w", dir, err)
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("storage: create %s: %w", tmp, err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(dm); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("storage: encode: %w", err)
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

// Atomic write: tmp file + rename.
func (s *TreeStore) SaveByIdentity(proto, identity string, objs []consumer.Object) error {
	if proto == "" {
		return fmt.Errorf("storage: SaveByIdentity: empty proto")
	}
	if identity == "" {
		return fmt.Errorf("storage: SaveByIdentity: empty identity")
	}
	model, swRev := splitIdentity(identity)

	// Zero Slot on each object — the DM is slot-agnostic.
	clean := make([]consumer.Object, len(objs))
	for i, o := range objs {
		clean[i] = o
		clean[i].Slot = 0
	}
	dm := DM{
		Model:    model,
		SwRev:    swRev,
		Protocol: proto,
		Objects:  clean,
	}

	path := s.identityPath(proto, identity)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("storage: mkdir %s: %w", dir, err)
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("storage: create %s: %w", tmp, err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(dm); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("storage: encode: %w", err)
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

// LoadByIdentity reads the identity-keyed cache. Returns (nil, nil)
// on cache miss; err non-nil only on read / decode failures.
//
// Auto-detects on-disk shape:
//   - New DM shape (#430): top-level keys {model, sw_rev, protocol,
//     objects}. Slot-agnostic. Synthesised into a Snapshot at load
//     time so existing callers keep working.
//   - Legacy Snapshot shape (pre-#430): top-level keys {device, slots,
//     ...}. Accepted for one release cycle, then dropped.
//
// Path resolution order:
//   1. .cache/dm/<proto>/<identity>.json (new per-proto path, #425)
//   2. .cache/dm/<identity>.json         (legacy flat path)
//
// Both paths can carry either DM or legacy Snapshot content — the
// shape detection is content-based, not path-based.
func (s *TreeStore) LoadByIdentity(proto, identity string) (*export.Snapshot, error) {
	if proto == "" {
		return nil, fmt.Errorf("storage: LoadByIdentity: empty proto")
	}
	if identity == "" {
		return nil, fmt.Errorf("storage: LoadByIdentity: empty identity")
	}
	path := s.identityPath(proto, identity)
	f, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("storage: open %s: %w", path, err)
		}
		legacy := s.legacyIdentityPath(identity)
		f, err = os.Open(legacy)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("storage: open %s: %w", legacy, err)
		}
		path = legacy
	}
	defer func() { _ = f.Close() }()

	// Read once; decide format from a peek key.
	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("storage: read %s: %w", path, err)
	}
	// Cheap shape detection: probe one key from the top-level object.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("storage: decode %s: %w", path, err)
	}
	if _, hasModel := probe["model"]; hasModel {
		// New DM shape — synthesise a Snapshot wrapping the Objects.
		var dm DM
		if err := json.Unmarshal(raw, &dm); err != nil {
			return nil, fmt.Errorf("storage: decode dm %s: %w", path, err)
		}
		snap := &export.Snapshot{
			Device: export.DeviceInfo{Protocol: dm.Protocol},
			Slots: []export.SlotDump{{
				Slot:     0,
				WalkedAt: time.Now().UTC(),
				Objects:  dm.Objects,
			}},
		}
		return snap, nil
	}
	// Legacy Snapshot shape — decode directly.
	var snap export.Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, fmt.Errorf("storage: decode legacy %s: %w", path, err)
	}
	return &snap, nil
}

// FindCardName extracts the Card Name from a list of objects.
// Used for identity validation.
func FindCardName(objs []consumer.Object) string {
	for _, o := range objs {
		if o.Label == "Card Name" && o.Value.Kind == consumer.KindString {
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
