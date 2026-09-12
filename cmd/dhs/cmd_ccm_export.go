package main

// `dhs consumer ccm export <host> --out <dir>` — the store-for-diff
// verb (owner ask, 2026-09-03). Three artifacts per device, all keyed
// by productName@productVersion (ADR-0022 Model@SwRev), so a firmware
// upgrade drops a second set beside the first and a plain diff shows
// exactly what CCM added/changed:
//
//   <out>/<Product@Ver>/api.yml     the served OpenAPI schema (DM contract)
//   <out>/<Product@Ver>/tree.json   the browsed device (streams keyed by UUID)
//   <out>/<Product@Ver>/tree.yaml   the same, extracted human-readable
//   <out>/<Product@Ver>/self.json   device identity
//
// api.yml is the SCHEMA (versioned by the OpenAPI + product version);
// tree.json is the walked DM instance. Both under one identity key.

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	ccmcodec "dhs/internal/ccm/codec"
	ccmc "dhs/internal/ccm/consumer"
)

func runCCMExport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("consumer ccm export", flag.ContinueOnError)
	out := fs.String("out", "ccm-export", "output directory root")
	verifyTLS := fs.Bool("verify-tls", false, "verify the device certificate (default: skip)")

	host := ""
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		host, args = args[0], args[1:]
	}
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if host == "" {
		return fmt.Errorf("consumer ccm export: a host is required")
	}

	c := ccmc.New(ccmc.Options{Host: host, VerifyTLS: *verifyTLS})

	dev, deviations, err := c.Walk(ctx)
	if err != nil {
		return fmt.Errorf("consumer ccm export: walk: %w", err)
	}
	// The full recursive DM — every node/resource, not just io/ip streams —
	// is the artifact that makes the versioned firmware diff complete.
	fullTree, treeDevs, treeErr := c.WalkTree(ctx)
	if treeErr != nil {
		return fmt.Errorf("consumer ccm export: walk-tree: %w", treeErr)
	}
	deviations = append(deviations, treeDevs...)
	spec, specErr := c.FetchSpec(ctx)
	if specErr != nil {
		// The schema is the point of the diff, but a firmware that does
		// not serve it must still yield the walked tree — record and go.
		deviations = append(deviations, "api.yml: "+specErr.Error())
	}

	// Identity key: productName@productVersion (ADR-0022 Model@SwRev).
	key := sanitizeKey(dev.ProductName + "@" + dev.ProductVersion)
	dir := filepath.Join(*out, key)
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return fmt.Errorf("consumer ccm export: %w", mkErr)
	}

	if spec != nil {
		if werr := os.WriteFile(filepath.Join(dir, "api.yml"), spec, 0o644); werr != nil {
			return fmt.Errorf("consumer ccm export: write api.yml: %w", werr)
		}
	}
	if werr := writeJSONFile(filepath.Join(dir, "tree.json"), dev); werr != nil {
		return werr
	}
	if werr := os.WriteFile(filepath.Join(dir, "tree.yaml"), []byte(ccmTreeYAML(dev)), 0o644); werr != nil {
		return fmt.Errorf("consumer ccm export: write tree.yaml: %w", werr)
	}
	if werr := writeDMTreeStable(filepath.Join(dir, "dm-tree.json"), fullTree); werr != nil {
		return fmt.Errorf("consumer ccm export: write dm-tree.json: %w", werr)
	}
	self := map[string]any{
		"productName": dev.ProductName, "productVersion": dev.ProductVersion,
		"modelVersion": dev.ModelVersion, "streams": len(dev.Streams),
	}
	if werr := writeJSONFile(filepath.Join(dir, "self.json"), self); werr != nil {
		return werr
	}

	fmt.Printf("ccm export %s (%s) -> %s\n", key, host, dir)
	specNote := "api.yml stored"
	if spec == nil {
		specNote = "api.yml NOT served by this firmware"
	}
	fmt.Printf("  %d stream(s), %d DM resource(s) across %d node(s), %s\n",
		len(dev.Streams), fullTree.Len(), len(fullTree.Branches), specNote)
	for _, d := range deviations {
		fmt.Fprintf(os.Stderr, "  deviation: %s\n", d)
	}
	fmt.Printf("  diff a later firmware with: diff -u %s/api.yml <newer>/api.yml\n", dir)
	return nil
}

// writeJSONFile is shared with cmd_walk.go.

// writeDMTreeStable writes the full recursive DM as one indented JSON
// object mapping resource path -> resource value, with EVERY object key
// sorted recursively (each resource is unmarshaled then re-marshaled) so
// two firmware captures diff line-by-line without spurious key-order
// churn. This is the complete device model artifact (ADR-0022).
func writeDMTreeStable(path string, tree *ccmcodec.DMTree) error {
	out := make(map[string]any, tree.Len())
	for _, p := range tree.SortedPaths() {
		var v any
		if err := json.Unmarshal(tree.Resources[p], &v); err != nil {
			// A resource that isn't valid JSON is impossible here (the walk
			// only stores classified bodies), but never lose it if so.
			v = string(tree.Resources[p])
		}
		out[p] = v
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// sanitizeKey makes an identity safe for a directory name.
func sanitizeKey(s string) string {
	if s == "" || s == "@" {
		return "unknown"
	}
	repl := func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '@', r == '.', r == '-', r == '_':
			return r
		}
		return '_'
	}
	return strings.Map(repl, s)
}

// ccmTreeYAML renders the walked device as a compact, diff-friendly
// YAML — one stream per block, UUID-sorted (stable across runs so two
// firmware exports diff cleanly).
func ccmTreeYAML(dev *ccmcodec.Device) string {
	var b strings.Builder
	fmt.Fprintf(&b, "product: %s\nversion: %s\nmodelVersion: %d\nstreams:\n",
		dev.ProductName, dev.ProductVersion, dev.ModelVersion)
	uuids := make([]string, 0, len(dev.Streams))
	for u := range dev.Streams {
		uuids = append(uuids, u)
	}
	sort.Strings(uuids)
	for _, u := range uuids {
		s := dev.Streams[u]
		fmt.Fprintf(&b, "  - uuid: %s\n    kind: %s\n    essence: %s\n    enable: %t\n    name: %q\n",
			s.UUID, s.Kind, s.Essence, s.Enable, s.Name)
		if s.MediaType != "" {
			fmt.Fprintf(&b, "    mediaType: %s\n", s.MediaType)
		}
		for _, leg := range s.Legs {
			fmt.Fprintf(&b, "    - leg: {ip: %s, port: %d, streamId: %s}\n", leg.IP, leg.Port, leg.StreamID)
		}
	}
	return b.String()
}
