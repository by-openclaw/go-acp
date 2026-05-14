package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Write serialises a Manifest to .cache/manifest/<device-slug>.json
// atomically (tmp file + rename). Device slug is the lower-cased
// device.Name with whitespace and unsafe chars stripped — predictable
// path operators can find without guessing.
//
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
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", fmt.Errorf("manifest: create %s: %w", tmp, err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", fmt.Errorf("manifest: encode: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("manifest: close: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("manifest: rename: %w", err)
	}
	return path, nil
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
