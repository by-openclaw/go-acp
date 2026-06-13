package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Write serialises a Manifest to .cache/manifest/<device-slug>.json
// atomically (tmp file + rename). Protocol-agnostic — same writer
// serves emberplus, acp1, acp2, probel, future TSL / OSC.
//
// Merge behaviour: if a manifest already exists for this device slug,
// endpoints from the existing file are unioned with m.Device.Endpoints
// (dedup by ip+port+transport). This is the redundant-controller path
// — running `dhs watch` against the device's primary IP, then again
// against the backup IP, accumulates both endpoints in one manifest.
// Frames and slots are replaced wholesale by the new write (the
// schema authority is the most recent walk).
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
	slug := slugifyDeviceName(m.Device.Name)
	dir := filepath.Join(cacheDir, "manifest")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("manifest: mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, slug+".json")

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
	return path, nil
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
