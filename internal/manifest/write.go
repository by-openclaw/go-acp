package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Write serialises a Manifest to .cache/manifest/<proto>/<key>.json
// atomically (tmp file + rename), where key is the device's identity
// IP (ADR-0028), falling back to the slugified device name when no IP
// is known (the IP-less edge case — callers warn loudly). Protocol-
// agnostic — same writer serves emberplus, acp1, acp2, probel,
// cerebrum-nb, future TSL / OSC.
//
// Merge behaviour: if a manifest already exists for this key,
// endpoints from the existing file are unioned with m.Device.Endpoints
// (dedup by ip+port+transport). This is the redundant-controller path
// — running `dhs watch` against the device's primary IP, then again
// against the backup IP, accumulates both endpoints in one manifest.
// Frames and slots are replaced wholesale by the new write (the
// schema authority is the most recent walk).
//
// Legacy migration (pre-ADR-0028 layout, manifest/<name-slug>.json
// with no proto subfolder): read once, endpoints merged in, legacy
// file removed after the new write succeeds.
//
// Test seams for the two I/O arms that cannot be provoked through the
// filesystem: a Manifest is always JSON-encodable so enc.Encode never
// fails naturally, and a freshly-created file rarely fails Close. Tests
// override these to exercise the error paths (cf. the bcastDialErrHook
// pattern in the acp1 provider). Production behaviour is the default.
var (
	encodeManifest = func(enc *json.Encoder, m *Manifest) error { return enc.Encode(m) }
	closeFile      = func(f *os.File) error { return f.Close() }
)

// Returns the full path written (for logging).
func Write(cacheDir string, m *Manifest) (string, error) {
	if m == nil || m.Device.Name == "" {
		return "", fmt.Errorf("manifest: Write: device name required")
	}
	if m.Device.Protocol == "" {
		return "", fmt.Errorf("manifest: Write: device protocol required (the ADR-0028 key is manifest/<proto>/<ip>.json)")
	}
	key := m.Device.IP
	if key == "" {
		key = slugifyDeviceName(m.Device.Name)
	}
	dir := filepath.Join(cacheDir, "manifest", sanitizeKeySeg(m.Device.Protocol))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("manifest: mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, sanitizeKeySeg(key)+".json")

	// Legacy migration: the pre-ADR-0028 name-keyed file (no proto
	// subfolder). Its endpoints join the union; the file is removed
	// after the new write lands.
	legacy := filepath.Join(cacheDir, "manifest", slugifyDeviceName(m.Device.Name)+".json")
	if prior, perr := Load(legacy); perr == nil && prior != nil {
		m.Device.Endpoints = mergeEndpoints(prior.Device.Endpoints, m.Device.Endpoints)
	}

	// Endpoint merge: load existing if present, union with new.
	if prior, perr := Load(path); perr == nil && prior != nil {
		m.Device.Endpoints = mergeEndpoints(prior.Device.Endpoints, m.Device.Endpoints)
	}

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", fmt.Errorf("manifest: create %s: %w", tmp, err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := encodeManifest(enc, m); err != nil {
		_ = closeFile(f)
		_ = os.Remove(tmp)
		return "", fmt.Errorf("manifest: encode: %w", err)
	}
	if err := closeFile(f); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("manifest: close: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("manifest: rename: %w", err)
	}
	// New write landed — retire the legacy name-keyed file (read-once-
	// rewritten per ADR-0028). Best-effort: a leftover legacy file is
	// re-merged on the next write, never lost.
	if legacy != path {
		_ = os.Remove(legacy)
	}
	return path, nil
}

// sanitizeKeySeg strips characters that are illegal in filenames on
// Windows + POSIX so IPs (IPv6 colons), protocol names and name-slug
// fallbacks go to disk safely.
func sanitizeKeySeg(s string) string {
	for _, ch := range []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"} {
		s = strings.ReplaceAll(s, ch, "_")
	}
	return s
}

// mergeEndpoints unions prior + incoming, dedup by (ip, port,
// transport). Order: existing entries first (preserves the order
// the operator originally registered), new entries appended after.
// Used to accumulate redundant-controller endpoints across multiple
// walks of the same device.
func mergeEndpoints(prior, incoming []Endpoint) []Endpoint {
	seen := make(map[string]bool, len(prior)+len(incoming))
	key := func(e Endpoint) string {
		return e.IP + "|" + fmt.Sprintf("%d", e.Port) + "|" + e.Transport
	}
	out := make([]Endpoint, 0, len(prior)+len(incoming))
	for _, e := range prior {
		k := key(e)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, e)
	}
	for _, e := range incoming {
		k := key(e)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, e)
	}
	return out
}

// slugifyDeviceName converts "Tiny Ember+ Router" → "tiny-ember-router".
// Lower-cased; ASCII letters/digits + dot kept; everything else (incl.
// '+', whitespace) → '-'; collapsed; trimmed of leading/trailing '-'.
func slugifyDeviceName(name string) string {
	out := make([]byte, 0, len(name))
	prevDash := false
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'A' && c <= 'Z':
			out = append(out, c+32) // lower-case
			prevDash = false
		case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.':
			out = append(out, c)
			prevDash = false
		default:
			if !prevDash {
				out = append(out, '-')
				prevDash = true
			}
		}
	}
	// Trim leading/trailing '-'.
	start, end := 0, len(out)
	for start < end && out[start] == '-' {
		start++
	}
	for end > start && out[end-1] == '-' {
		end--
	}
	return string(out[start:end])
}
