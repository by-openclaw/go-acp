package main

import (
	"runtime/debug"
	"strings"
)

// captureToolInfo carries the four fields the meta.json capture_tool
// object requires (issue #47, fixture schema from #43).
type captureToolInfo struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	GitTag    string `json:"git_tag"`
	GitCommit string `json:"git_commit"`
}

// buildCaptureToolInfo assembles the struct at capture time. Reads
// runtime/debug.BuildInfo.Settings for `vcs.revision` + `vcs.modified`
// (populated by `go build -buildvcs=true`, default since Go 1.18).
// A dirty worktree flags the git_tag with a "-dirty" suffix so such
// captures are easy to refuse committing.
func buildCaptureToolInfo() captureToolInfo {
	info := captureToolInfo{
		Name:    "acp",
		Version: version,
		GitTag:  gitTag,
	}
	if info.Version == "" || info.Version == "dev" {
		info.Version = "devel"
	}

	// Prefer ldflags-injected `commit` (package-level var in main.go);
	// fall back to runtime/debug.BuildInfo (populated by
	// `go build -buildvcs=true`, default since Go 1.18).
	vcsCommit := commit
	var modified bool
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if vcsCommit == "" {
					vcsCommit = s.Value
				}
			case "vcs.modified":
				modified = s.Value == "true"
			}
		}
	}

	switch {
	case len(vcsCommit) >= 7:
		info.GitCommit = vcsCommit[:7]
	case vcsCommit != "":
		info.GitCommit = vcsCommit
	default:
		info.GitCommit = "unknown"
	}

	if info.GitTag == "" {
		if vcsCommit != "" {
			info.GitTag = "devel-" + info.GitCommit
		} else {
			info.GitTag = "devel"
		}
	}

	if modified && !strings.HasSuffix(info.GitTag, "-dirty") {
		info.GitTag += "-dirty"
	}

	return info
}
