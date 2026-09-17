package main

// ADR-0028 path composer — the ONE place default artifact homes are
// built. Every verb that lets a flag default composes here; unit-5 CI
// tests pin each composed shape to the ADR table, so layout drift
// fails the build instead of scattering files.
//
//	snapshots/<proto>/<key>/<facet>            operator state (export/import)
//	captures/<proto>/<key>/<verb>[-<scope>]-<utcstamp>.jsonl   wire evidence
//	.cache/logs/<proto>/<key>/<verb>.log       disposable diagnostics
//
// key = the device identity IP (the RM sentinel 0.0.0.0 included);
// name-slug only where no IP exists (callers warn loudly).

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"dhs/internal/datastore"
)

// hostOnly strips an optional :port from an addr so the host alone
// keys the artifact folders (ports are reachability, not identity).
func hostOnly(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

// artifactRoot resolves the ADR-0028 bucket root (the directory
// .cache/, snapshots/ and captures/ are siblings under). Falls back
// to the current directory when the binary path cannot be resolved —
// deterministic either way, never silent.
func artifactRoot() string {
	root, err := datastore.ProjectRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: artifact root: %v — using current directory\n", err)
		return "."
	}
	return root
}

// snapshotDir returns snapshots/<proto>/<key>/ — the one folder per
// device that holds every state facet (params/xpoint/src/dst/…).
func snapshotDir(proto, key string) string {
	return filepath.Join(artifactRoot(), "snapshots",
		datastore.SanitizePathSeg(proto), datastore.SanitizePathSeg(key))
}

// defaultCapturePath returns captures/<proto>/<key>/<verb>[-<scope>]-
// <utcstamp>.jsonl — the self-describing wire-evidence location.
func defaultCapturePath(proto, key, verb, scope string, now time.Time) string {
	name := datastore.SanitizePathSeg(verb)
	if scope != "" {
		name += "-" + datastore.SanitizePathSeg(scope)
	}
	name += "-" + now.UTC().Format("20060102T1504Z") + ".jsonl"
	return filepath.Join(artifactRoot(), "captures",
		datastore.SanitizePathSeg(proto), datastore.SanitizePathSeg(key), name)
}

// defaultLogPath returns .cache/logs/<proto>/<key>/<verb>.log —
// disposable diagnostics in the machine bucket.
func defaultLogPath(proto, key, verb string) string {
	return filepath.Join(artifactRoot(), ".cache", "logs",
		datastore.SanitizePathSeg(proto), datastore.SanitizePathSeg(key),
		datastore.SanitizePathSeg(verb)+".log")
}

// cerebrumExpandAutoPaths resolves the "auto" sentinel on --capture /
// --log to the ADR-0028 defaults (verb-aware, keyed by the NB server
// host we dial), creating parent directories. Called by every
// cerebrum dial path before the logger / recorder open.
func cerebrumExpandAutoPaths(cf *cerebrumFlags, verb, host string) {
	if cf.capture == "auto" {
		cf.capture = defaultCapturePath("cerebrum-nb", host, verb, "", time.Now())
		_ = os.MkdirAll(filepath.Dir(cf.capture), 0o755)
		fmt.Fprintf(os.Stderr, "cerebrum-nb %s: --capture auto → %s (ADR-0028)\n", verb, cf.capture)
	}
	if cf.logPath == "auto" {
		cf.logPath = defaultLogPath("cerebrum-nb", host, verb)
		_ = os.MkdirAll(filepath.Dir(cf.logPath), 0o755)
		// Report the file that will actually be written. The local log
		// rotates daily, so the logical path ".../watch.log" never exists on
		// disk — printing it sends the operator looking for the wrong file.
		fmt.Fprintf(os.Stderr, "cerebrum-nb %s: --log auto → %s (ADR-0028, rolls daily)\n",
			verb, dailyPathFor(cf.logPath, time.Now()))
	}
}

// facetName renders a facet filename. With a prefix the legacy
// "<prefix>-<facet>.csv" shape is kept (explicit --out-dir/--in-dir
// compatibility); the default snapshot folder uses the plain ADR
// shape "<facet>.csv" — the folder already identifies the device.
func facetName(prefix, facet string) string {
	if prefix != "" {
		return prefix + "-" + facet + ".csv"
	}
	return facet + ".csv"
}

// facetFile joins facetName inside dir.
func facetFile(dir, prefix, facet string) string {
	return filepath.Join(dir, facetName(prefix, facet))
}
