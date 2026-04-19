package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// runExtract drives `acp extract` (issue #47) — walks a device and
// writes the product-fixture triple (meta + wire + tree) into the
// output directory using the schema locked in #43.
func runExtract(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("extract", flag.ExitOnError)
	cf := addCommonFlags(fs)

	manufacturer := fs.String("manufacturer", "",
		"vendor display name preserving casing (e.g. Axon, Lawo). Required.")
	product := fs.String("product", "",
		"product identifier as the vendor writes it (e.g. DDB08, CDV08v06). Required.")
	ver := fs.String("version", "",
		"product / firmware version as reported by the device (e.g. 2.3). Required.")
	versionKind := fs.String("version-kind", "firmware",
		"firmware | software | release")
	description := fs.String("description", "",
		"free-text description from the device identity block (optional)")
	notes := fs.String("notes", "",
		"free-text notes for the engineer who captured (optional)")
	outDir := fs.String("out", "",
		"output directory (e.g. tests/fixtures/products/axon/DDB08/acp2/v2.3/). Required.")
	slot := fs.Int("slot", -1, "slot to walk (Ember+ defaults to 0 if omitted)")

	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp extract <host> --protocol P --manufacturer M --product P --version V --out DIR [--slot N]")
	}
	_ = fs.Parse(rest)

	if *manufacturer == "" || *product == "" || *ver == "" || *outDir == "" {
		return fmt.Errorf("--manufacturer, --product, --version, and --out are all required")
	}

	if cf.protocol == "emberplus" && *slot < 0 {
		*slot = 0
	}
	if *slot < 0 {
		return fmt.Errorf("--slot N is required (except Ember+)")
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return fmt.Errorf("create --out dir: %w", err)
	}

	// Point capture at the out-dir so the walk's frame recorder and
	// canonical-export writer both land there. The isDirectoryCapture
	// heuristic treats a path without .jsonl extension as dir mode.
	cf.capture = *outDir

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	// Walk raw ctx (no per-op timeout) — large devices take minutes.
	objs, err := plug.Walk(ctx, *slot)
	if err != nil {
		return fmt.Errorf("walk slot %d: %w", *slot, err)
	}
	fmt.Printf("walked %d objects on slot %d\n", len(objs), *slot)

	// writeCanonicalCapture (from cmd_walk.go) emits tree.json for
	// every plugin type. Ember+ additionally writes glow.json; that's
	// fine — tree.json is the fingerprint input.
	if err := writeCanonicalCapture(ctx, *outDir, plug, cf); err != nil {
		return fmt.Errorf("canonical capture: %w", err)
	}

	// Rename raw.<transport>.jsonl → wire.jsonl so the fixture layout
	// matches docs/fixtures-products.md exactly (the protocol is still
	// identifiable via meta.json). Best-effort: a missing raw file
	// isn't fatal (recorder may have been inactive for brief walks).
	rawPath := filepath.Join(*outDir, rawFrameFilename(cf.protocol))
	wirePath := filepath.Join(*outDir, "wire.jsonl")
	if err := os.Rename(rawPath, wirePath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: rename %s → wire.jsonl: %v\n",
			filepath.Base(rawPath), err)
	}

	treePath := filepath.Join(*outDir, "tree.json")
	fingerprint, err := sha256File(treePath)
	if err != nil {
		return fmt.Errorf("fingerprint tree.json: %w", err)
	}

	meta := metaJSON{
		Protocol:      cf.protocol,
		Manufacturer:  *manufacturer,
		Product:       *product,
		Version:       *ver,
		VersionKind:   *versionKind,
		DiscoveredAt:  time.Now().UTC().Format(time.RFC3339),
		Description:   *description,
		DMFingerprint: fingerprint,
		ObjectCount:   len(objs),
		CaptureTool:   buildCaptureToolInfo(),
		Notes:         *notes,
	}
	if err := writeMetaJSON(filepath.Join(*outDir, "meta.json"), &meta); err != nil {
		return fmt.Errorf("write meta.json: %w", err)
	}

	fmt.Printf("extract complete: %s/{meta.json, wire.jsonl, tree.json}\n", *outDir)
	fmt.Printf("  fingerprint: %s\n", fingerprint)
	return nil
}

// metaJSON mirrors the locked schema in docs/fixtures-products.md.
// Unexported on purpose — external consumers read the JSON file, not
// the Go struct.
type metaJSON struct {
	Protocol      string          `json:"protocol"`
	Manufacturer  string          `json:"manufacturer"`
	Product       string          `json:"product"`
	Version       string          `json:"version"`
	VersionKind   string          `json:"version_kind"`
	DiscoveredAt  string          `json:"discovered_at"`
	Description   string          `json:"description,omitempty"`
	DMFingerprint string          `json:"dm_fingerprint"`
	ObjectCount   int             `json:"object_count"`
	CaptureTool   captureToolInfo `json:"capture_tool"`
	Notes         string          `json:"notes,omitempty"`
}

// sha256File streams the named file through a SHA-256 digest and
// returns the `sha256:<hex>` form used by meta.json.dm_fingerprint.
// Any byte-identical tree.json produces the identical fingerprint —
// the property replay tests rely on.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// writeMetaJSON emits indented JSON so the file is diff-friendly in
// git. Keys follow the struct field order.
func writeMetaJSON(path string, m *metaJSON) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(m)
}
