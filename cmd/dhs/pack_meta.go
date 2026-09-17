package main

// Router-pack meta.json (#738 unit 3). Every export that writes a
// pack folder drops a meta.json beside the facet files so the pack is
// self-describing: which protocol, which target, when, which tool
// build, and which pack format revision.
//
// Evolvability rules (#738): pack_version bumps on any format change;
// new concepts land as new FILES, new attributes as appended COLUMNS
// keyed by header; readers ignore unknown extras and hard-fail on
// missing required files. That keeps every pack on disk diffable
// against packs extracted years apart.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// packVersion is the current router-pack format revision.
const packVersion = "1.0.0"

type packMeta struct {
	PackVersion string `json:"pack_version"`
	Protocol    string `json:"protocol"`
	Target      string `json:"target"`
	GeneratedAt string `json:"generated_at"`
	ToolVersion string `json:"tool_version"`
	ToolCommit  string `json:"tool_commit,omitempty"`
}

// writePackMeta writes dir/meta.json for one export invocation.
func writePackMeta(dir, proto, target string) error {
	m := packMeta{
		PackVersion: packVersion,
		Protocol:    proto,
		Target:      target,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		ToolVersion: version,
		ToolCommit:  commit,
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "meta.json"), append(b, '\n'), 0o644)
}
