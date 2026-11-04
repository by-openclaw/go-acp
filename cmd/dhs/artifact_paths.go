package main

// ADR-0028 path composer — the ONE place default artifact homes are
// built. Every verb that lets a flag default composes here; unit-5 CI
// tests pin each composed shape to the ADR table, so layout drift
// fails the build instead of scattering files.
//
//	snapshots/<proto>/<key>/<facet>            operator state (export/import)
//	captures/<proto>/<key>/<verb>[-<scope>]-<utcstamp>.jsonl   wire evidence
//	.cache/logs/<proto>/<key>/<verb>.log       disposable diagnostics
//	.cache/manifest/<proto>/<key>.json         inventory anchor (manifest.Write)
//
// key = the device identity IP (the RM sentinel 0.0.0.0 included), a
// hostname resolved to it (deviceKey); name-slug only where no IP exists
// (callers warn loudly).

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"dhs/internal/datastore"
	"dhs/internal/manifest"
)

// hostOnly strips an optional :port from an addr so the host alone
// keys the artifact folders (ports are reachability, not identity).
func hostOnly(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

// deviceKey is the ADR-0028 folder and manifest key of a target: its IP
// alone, no port. A hostname is resolved here, so one device lands in one
// folder whether it was addressed by name or by IP; the name itself goes
// into the manifest as FQDN (fqdnOf), never into a path. A name that does
// not resolve keys by itself, with a warning.
func deviceKey(addr string) string {
	return deviceKeyWith(net.DefaultResolver.LookupHost, addr)
}

func deviceKeyWith(lookup func(context.Context, string) ([]string, error), addr string) string {
	h := hostOnly(addr)
	if h == "" || net.ParseIP(h) != nil {
		return h
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ips, err := lookup(ctx, h)
	if err != nil || len(ips) == 0 {
		fmt.Fprintf(os.Stderr, "warning: %s does not resolve (%v) — keyed by the name, not an IP (ADR-0028)\n", h, err)
		return h
	}
	for _, ip := range ips {
		if p := net.ParseIP(ip); p != nil && p.To4() != nil {
			return ip
		}
	}
	return ips[0]
}

// deviceKeyFor is deviceKey for a device that may have several
// controller IPs (ADR-0028 §2): any of them keys to the device's ONE
// folder and manifest, the primary IP's. The primary comes from the
// global --primary flag, else from the manifest of proto that lists this
// IP as a controller. Two devices listing the same IP is a conflict:
// reported, and the IP keys by itself.
func deviceKeyFor(proto, addr string) string {
	key := deviceKey(addr)
	if p := os.Getenv(primaryEnv); p != "" {
		return deviceKey(p)
	}
	primary, found, conflict := manifest.PrimaryOf(manifestCacheDir(), proto, key)
	switch {
	case len(conflict) > 0:
		fmt.Fprintf(os.Stderr, "warning: %s is a controller of %d %s devices (%v) — keyed by itself; fix the manifests\n", key, len(conflict), proto, conflict)
	case found && primary != key:
		fmt.Fprintf(os.Stderr, "%s: %s is a controller of %s — its folder and manifest (ADR-0028)\n", proto, key, primary)
		return primary
	}
	return key
}

// keyManifest keys a manifest a connector built for the controller it
// reached at addr: the device's primary IP (deviceKeyFor), or, when this
// controller is not known yet, the device already on file with the same
// serial number — the same box seen at another IP joins it, and the
// controller's own one-IP manifest goes. The endpoint stays the
// controller reached.
func keyManifest(mf *manifest.Manifest, proto, addr string) *manifest.Manifest {
	own := deviceKey(addr)
	key := deviceKeyFor(proto, addr)
	bySerial := false
	if key == own && os.Getenv(primaryEnv) == "" {
		if other := manifest.ByUnit(manifestCacheDir(), proto, "serial", mf.Device.Unit["serial"], own); other != "" {
			key, bySerial = other, true
		}
	}
	// UDP serves one controller per device (ADR-0023: announces are
	// subnet broadcast); a second one is never joined.
	if key != own {
		for _, e := range mf.Device.Endpoints {
			if e.Transport == "udp" {
				fmt.Fprintf(os.Stderr, "warning: %s %s is UDP — one controller per device (ADR-0023); not joined to %s\n", proto, own, key)
				return keyAs(mf, own, addr)
			}
		}
	}
	if bySerial {
		fmt.Fprintf(os.Stderr, "%s: %s has the serial of %s — joins it as a controller (ADR-0028)\n", proto, own, key)
		manifest.Remove(manifestCacheDir(), proto, own)
	}
	return keyAs(mf, key, addr)
}

func keyAs(mf *manifest.Manifest, key, addr string) *manifest.Manifest {
	mf.Device.IP = key
	mf.Device.FQDN = fqdnOf(addr)
	return mf
}

// fqdnOf is the hostname a target was addressed by, for the manifest's
// FQDN field; empty when it was addressed by IP.
func fqdnOf(addr string) string {
	h := hostOnly(addr)
	if net.ParseIP(h) != nil {
		return ""
	}
	return h
}

// manifestCacheDir is the .cache the manifests live in: the tree store's
// base dir, the sibling of dm/ — never the current directory.
func manifestCacheDir() string {
	if treeStore != nil {
		return treeStore.BaseDir()
	}
	return filepath.Join(artifactRoot(), ".cache")
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
