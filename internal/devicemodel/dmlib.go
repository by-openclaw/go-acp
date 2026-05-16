// Package dmlib is the Device Model library: a runtime resolver for the
// per-product schema catalogue at
// tests/fixtures/products/<vendor>/<product>/<model>-<sw_rev>/<protocol>/.
//
// One product can carry several protocols (sibling proto dirs); each proto
// dir holds per-slot tree snapshots in the same JSON shape as
// `dhs export --format json`. The library is the source-of-truth schema
// every plugin seeds from on Connect.
//
// Lookup key is (Model, SwRev, Proto). HwRev rides along as metadata only.
// Schemas are NEVER deduplicated across sw_rev — each rev is its own dir.
//
// This package is cross-protocol foundation; it has no plugin imports and
// uses no third-party dependencies.
package devicemodel

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dhs/internal/export"
	"dhs/internal/identity"
	"dhs/internal/consumer"
)

// Sentinel errors returned by the resolver.
var (
	ErrNotFound = errors.New("dmlib: schema not found")
	ErrInvalid  = errors.New("dmlib: invalid fingerprint")
)

// Fingerprint identifies a product schema. (Model, SwRev, Proto) is the
// lookup key; Vendor and Product locate the on-disk directory but are not
// part of the equality check (callers may resolve "RRS18@1601 acp1"
// without knowing the vendor).
type Fingerprint struct {
	Vendor  string
	Product string
	Model   string
	SwRev   string
	HwRev   string // metadata only; never in lookup key
	Proto   string // "acp1" | "acp2" | "emberplus" | ...
}

// Schema is one resolved product entry: per-slot tree snapshots for one
// (Model, SwRev, Proto) tuple plus optional cross-protocol metadata.
type Schema struct {
	Fingerprint Fingerprint

	// Slots holds one tree per slot the product exposes. For multi-slot
	// frames (Synapse) every slot has its own snapshot. For single-slot
	// devices the map has one entry, conventionally slot 1.
	Slots map[int]*export.Snapshot

	// Product is the cross-protocol product metadata loaded from
	// product.yaml at the model-rev directory root. Optional — schemas
	// without product.yaml resolve cleanly with Product zero-valued.
	Product ProductMeta

	// Identity holds the per-protocol identity-probe configuration
	// loaded from product.yaml. Optional.
	Identity IdentityProbe

	// Walk records when/by-what-tool the schema was captured.
	// Optional.
	Walk WalkMetadata

	// SupportedProtocols lists every protocol with a sibling proto dir
	// at the model-rev root. Loaded from product.yaml.
	SupportedProtocols []string
}

// Diff is the per-slot delta between two Schemas of the SAME (Model,
// Proto) — i.e. two firmware revisions of one product. Diffing across
// models is conceptually meaningless and Diff returns an empty result
// (Mismatch=true) for that case.
type Diff struct {
	AddedSlots   []int
	RemovedSlots []int

	// PerSlot holds the object-level diff for slots present in both
	// snapshots. Keyed by slot number.
	PerSlot map[int]SlotDiff

	// Mismatch is set when the two schemas don't share (Model, Proto).
	// Callers must check this before consuming PerSlot — a Mismatch
	// diff is empty by design, not because the schemas are identical.
	Mismatch bool
}

// SlotDiff is the object-level delta for one slot.
type SlotDiff struct {
	Added   []string // labels of objects only in cur
	Removed []string // labels of objects only in prev
	Changed []string // labels of objects with differing metadata (kind/min/max/...)
}

// Resolver is the lookup + persistence interface every consumer of the DM
// library uses.
type Resolver interface {
	// Resolve returns the schema for an exact (Model, SwRev, Proto) match.
	// Vendor + Product on the fingerprint are used to anchor the search
	// path when set; when blank, every vendor dir is scanned.
	Resolve(fp Fingerprint) (*Schema, error)

	// LookupAlternate returns the fingerprints of every (Vendor, Product,
	// Model, Proto) sibling that has a different SwRev on disk. Useful
	// for the changelog verb.
	LookupAlternate(fp Fingerprint) ([]Fingerprint, error)

	// Persist writes the schema's per-slot snapshots to disk under the
	// canonical layout. Atomic per slot file.
	Persist(s *Schema) error

	// Diff returns the per-slot object-level delta between two schemas.
	// Both must share the same (Model, SwRev, Proto) fingerprint;
	// otherwise the diff is undefined and an empty Diff is returned.
	Diff(prev, cur *Schema) Diff
}

// New constructs a file-backed Resolver rooted at the given directory.
// The root must already exist.
func New(rootDir string) Resolver {
	return &fileResolver{root: rootDir}
}

type fileResolver struct {
	root string
}

// Resolve walks the on-disk layout and reads every slot snapshot for the
// requested (Model, SwRev, Proto). Returns ErrNotFound if no matching
// directory exists. ErrInvalid if the fingerprint is missing required
// fields (Model, SwRev, Proto).
func (r *fileResolver) Resolve(fp Fingerprint) (*Schema, error) {
	if fp.Model == "" || fp.SwRev == "" || fp.Proto == "" {
		return nil, ErrInvalid
	}
	dir, err := r.protoDir(fp)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dmlib: stat %s: %w", dir, err)
	}
	slots, err := readSlots(dir)
	if err != nil {
		return nil, err
	}
	if len(slots) == 0 {
		return nil, ErrNotFound
	}
	s := &Schema{Fingerprint: fp, Slots: slots}

	// Load product.yaml from the model-rev root if present. Absence is
	// fine — schemas may exist without cross-protocol metadata.
	pyPath, perr := r.productYAMLPath(fp)
	if perr == nil {
		if pm, ip, wm, sp, lerr := LoadProductYAML(pyPath); lerr == nil {
			s.Product = *pm
			s.Identity = *ip
			s.Walk = *wm
			s.SupportedProtocols = sp
		} else if !os.IsNotExist(unwrapPathErr(lerr)) {
			return nil, lerr
		}
	}
	return s, nil
}

// LookupAlternate scans the same vendor/product/model dir for sibling
// sw_rev directories. Returns the resolved fingerprints; the schema
// itself is not loaded — callers Resolve() the ones they want.
func (r *fileResolver) LookupAlternate(fp Fingerprint) ([]Fingerprint, error) {
	if fp.Model == "" || fp.Proto == "" {
		return nil, ErrInvalid
	}
	productDir, err := r.productDir(fp)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(productDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("dmlib: read %s: %w", productDir, err)
	}
	prefix := fp.Model + "-"
	var out []Fingerprint
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		swrev := strings.TrimPrefix(name, prefix)
		if swrev == fp.SwRev {
			continue
		}
		// Confirm the proto sub-dir exists for this sibling.
		protoSub := filepath.Join(productDir, name, fp.Proto)
		if _, err := os.Stat(protoSub); err != nil {
			continue
		}
		out = append(out, Fingerprint{
			Vendor:  fp.Vendor,
			Product: fp.Product,
			Model:   fp.Model,
			SwRev:   swrev,
			Proto:   fp.Proto,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SwRev < out[j].SwRev })
	return out, nil
}

// Persist writes every slot's snapshot atomically and, when the schema
// carries cross-protocol metadata, writes product.yaml at the model-rev
// root.
func (r *fileResolver) Persist(s *Schema) error {
	if s == nil {
		return ErrInvalid
	}
	if s.Fingerprint.Model == "" || s.Fingerprint.SwRev == "" || s.Fingerprint.Proto == "" {
		return ErrInvalid
	}
	dir, err := r.protoDir(s.Fingerprint)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("dmlib: mkdir %s: %w", dir, err)
	}
	for slot, snap := range s.Slots {
		if snap == nil {
			continue
		}
		if err := writeSlot(dir, slot, snap); err != nil {
			return err
		}
	}

	// Save product.yaml if the schema carries metadata. The Product
	// struct's Model + SwRev are the trigger; both must be non-empty.
	if s.Product.Model != "" && s.Product.SwRev != "" {
		pyPath, perr := r.productYAMLPath(s.Fingerprint)
		if perr != nil {
			return perr
		}
		if err := SaveProductYAML(pyPath, &s.Product, &s.Identity, &s.Walk, s.SupportedProtocols); err != nil {
			return err
		}
	}
	return nil
}

// Diff computes the per-slot object-level delta. Caller is responsible
// for ensuring both snapshots share a fingerprint; mismatched diffs
// return an empty Diff with all maps initialised.
func (r *fileResolver) Diff(prev, cur *Schema) Diff {
	d := Diff{PerSlot: map[int]SlotDiff{}}
	if prev == nil || cur == nil {
		return d
	}
	// Diff is semantically valid only between two firmware revisions of
	// the same product. (Model, Proto) must match; SwRev / HwRev may
	// differ — that's the whole point.
	if prev.Fingerprint.Model != cur.Fingerprint.Model ||
		prev.Fingerprint.Proto != cur.Fingerprint.Proto {
		d.Mismatch = true
		return d
	}
	for slot := range cur.Slots {
		if _, ok := prev.Slots[slot]; !ok {
			d.AddedSlots = append(d.AddedSlots, slot)
		}
	}
	for slot := range prev.Slots {
		if _, ok := cur.Slots[slot]; !ok {
			d.RemovedSlots = append(d.RemovedSlots, slot)
		}
	}
	for slot, prevSnap := range prev.Slots {
		curSnap, ok := cur.Slots[slot]
		if !ok {
			continue
		}
		d.PerSlot[slot] = diffSlot(prevSnap, curSnap)
	}
	sort.Ints(d.AddedSlots)
	sort.Ints(d.RemovedSlots)
	return d
}

// productDir returns <root>/<vendor>/<product>/. Vendor and Product on
// the fingerprint must be non-empty.
func (r *fileResolver) productDir(fp Fingerprint) (string, error) {
	if fp.Vendor == "" || fp.Product == "" {
		return "", ErrInvalid
	}
	v, err := identity.PathSegment(fp.Vendor)
	if err != nil {
		return "", fmt.Errorf("dmlib: vendor: %w", err)
	}
	p, err := identity.PathSegment(fp.Product)
	if err != nil {
		return "", fmt.Errorf("dmlib: product: %w", err)
	}
	return filepath.Join(r.root, v, p), nil
}

// productYAMLPath returns <root>/<vendor>/<product>/<model>-<sw_rev>/product.yaml.
func (r *fileResolver) productYAMLPath(fp Fingerprint) (string, error) {
	pd, err := r.productDir(fp)
	if err != nil {
		return "", err
	}
	m, err := identity.PathSegment(fp.Model)
	if err != nil {
		return "", fmt.Errorf("dmlib: model: %w", err)
	}
	sw, err := identity.PathSegment(fp.SwRev)
	if err != nil {
		return "", fmt.Errorf("dmlib: sw_rev: %w", err)
	}
	return filepath.Join(pd, m+"-"+sw, "product.yaml"), nil
}

// unwrapPathErr peels wrapping layers to find an os.PathError that
// carries the actual not-exist signal. Used in Resolve to distinguish
// "metadata absent" from real I/O failures.
func unwrapPathErr(err error) error {
	if err == nil {
		return nil
	}
	for {
		inner := errors.Unwrap(err)
		if inner == nil {
			return err
		}
		err = inner
	}
}

// protoDir returns <root>/<vendor>/<product>/<model>-<sw_rev>/<proto>/.
func (r *fileResolver) protoDir(fp Fingerprint) (string, error) {
	pd, err := r.productDir(fp)
	if err != nil {
		return "", err
	}
	m, err := identity.PathSegment(fp.Model)
	if err != nil {
		return "", fmt.Errorf("dmlib: model: %w", err)
	}
	sw, err := identity.PathSegment(fp.SwRev)
	if err != nil {
		return "", fmt.Errorf("dmlib: sw_rev: %w", err)
	}
	pr, err := identity.PathSegment(fp.Proto)
	if err != nil {
		return "", fmt.Errorf("dmlib: proto: %w", err)
	}
	return filepath.Join(pd, m+"-"+sw, pr), nil
}

// readSlots loads every slot_<n>.json file in dir into a map keyed by
// slot number. Files with non-conforming names are ignored.
func readSlots(dir string) (map[int]*export.Snapshot, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("dmlib: read %s: %w", dir, err)
	}
	out := map[int]*export.Snapshot{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "slot_") || !strings.HasSuffix(name, ".json") {
			continue
		}
		var slot int
		if _, err := fmt.Sscanf(name, "slot_%d.json", &slot); err != nil {
			continue
		}
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("dmlib: open %s: %w", name, err)
		}
		snap, err := export.ReadJSON(f)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("dmlib: decode %s: %w", name, err)
		}
		out[slot] = snap
	}
	return out, nil
}

// writeSlot writes one snapshot file atomically (.tmp + rename).
func writeSlot(dir string, slot int, snap *export.Snapshot) error {
	path := filepath.Join(dir, fmt.Sprintf("slot_%d.json", slot))
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("dmlib: create %s: %w", tmp, err)
	}
	if err := export.WriteJSON(f, snap); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("dmlib: write %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("dmlib: close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("dmlib: rename %s: %w", path, err)
	}
	return nil
}

// diffSlot compares two snapshots of the same slot at the object level.
// Comparison is by Label; Object metadata changes (kind, min, max, ...)
// surface in Changed. Values are ignored — schemas don't carry values.
func diffSlot(prev, cur *export.Snapshot) SlotDiff {
	prevByLabel := map[string]int{}
	for i := range prev.Slots {
		for j, o := range prev.Slots[i].Objects {
			prevByLabel[o.Label] = (i << 16) | j
		}
	}
	curByLabel := map[string]int{}
	for i := range cur.Slots {
		for j, o := range cur.Slots[i].Objects {
			curByLabel[o.Label] = (i << 16) | j
		}
	}
	var d SlotDiff
	for label := range curByLabel {
		if _, ok := prevByLabel[label]; !ok {
			d.Added = append(d.Added, label)
		}
	}
	for label := range prevByLabel {
		if _, ok := curByLabel[label]; !ok {
			d.Removed = append(d.Removed, label)
		}
	}
	for label, curIdx := range curByLabel {
		prevIdx, ok := prevByLabel[label]
		if !ok {
			continue
		}
		ci, cj := curIdx>>16, curIdx&0xFFFF
		pi, pj := prevIdx>>16, prevIdx&0xFFFF
		if !objectsEqual(prev.Slots[pi].Objects[pj], cur.Slots[ci].Objects[cj]) {
			d.Changed = append(d.Changed, label)
		}
	}
	sort.Strings(d.Added)
	sort.Strings(d.Removed)
	sort.Strings(d.Changed)
	return d
}

// objectsEqual compares schema-relevant Object fields. Value is ignored
// because schemas are value-less; only structure matters.
func objectsEqual(a, b consumer.Object) bool {
	return a.Slot == b.Slot &&
		a.Group == b.Group &&
		a.ID == b.ID &&
		a.Kind == b.Kind &&
		a.Access == b.Access &&
		a.MaxLen == b.MaxLen &&
		a.Unit == b.Unit
}
