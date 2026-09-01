package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"dhs/internal/export/canonical"
	"dhs/internal/manifest"
)

// runProducerTree loads the canonical tree a producer would serve (from --tree
// or --manifest) and prints it — no device or running server needed. Canonical
// producer verb (ADR-0002): lets an operator inspect / validate exactly what
// `serve` will expose. `--output json` prints the canonical tree; text prints
// it with a source header.
func runProducerTree(_ context.Context, protoName string, args []string) error {
	fs := flag.NewFlagSet("producer "+protoName+" tree", flag.ContinueOnError)
	treePath := fs.String("tree", "", "path to canonical tree.json (one of --tree | --manifest required)")
	manifestPath := fs.String("manifest", "", "path to manifest JSON (assembles the tree from DMs under --cache-dir)")
	cacheDir := fs.String("cache-dir", ".cache", "cache root for --manifest DM lookup")
	output := fs.String("output", "text", "output format: text | json")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if *treePath == "" && *manifestPath == "" {
		return fmt.Errorf("producer %s tree: one of --tree | --manifest is required", protoName)
	}
	if *treePath != "" && *manifestPath != "" {
		return fmt.Errorf("producer %s tree: --tree and --manifest are mutually exclusive", protoName)
	}

	var (
		tree *canonical.Export
		err  error
		src  = *treePath
	)
	if *manifestPath != "" {
		src = *manifestPath
		mf, merr := manifest.Load(*manifestPath)
		if merr != nil {
			return fmt.Errorf("load manifest: %w", merr)
		}
		if mf.Device.Protocol != protoName {
			return fmt.Errorf("manifest protocol %q != requested %q", mf.Device.Protocol, protoName)
		}
		tree, err = manifest.BuildExport(mf, *cacheDir)
		if err != nil {
			return fmt.Errorf("build tree from manifest: %w", err)
		}
	} else {
		tree, err = loadTree(*treePath)
		if err != nil {
			return fmt.Errorf("load tree: %w", err)
		}
	}

	b, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return fmt.Errorf("render tree: %w", err)
	}
	if *output == "json" {
		fmt.Println(string(b))
		return nil
	}
	fmt.Printf("producer %s tree (from %s):\n%s\n", protoName, src, string(b))
	return nil
}
