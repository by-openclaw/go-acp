package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Load reads a manifest JSON file at path and validates its top-level
// shape. Per-protocol `addr` blocks are NOT validated here — that
// belongs to the plugin that owns the consumer.
//
// Validation:
//   - device.name and device.protocol non-empty
//   - at least one endpoint
//   - each endpoint has ip + port>0 + transport ∈ {tcp,udp}
//   - at least one frame; each frame has at least one slot; each slot
//     has a non-empty DM reference of the form Model@SwRev
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("manifest read %s: %w", path, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("manifest parse %s: %w", path, err)
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Manifest) validate() error {
	if m.Device.Name == "" {
		return fmt.Errorf("manifest: device.name empty")
	}
	if m.Device.Protocol == "" {
		return fmt.Errorf("manifest: device.protocol empty")
	}
	if len(m.Device.Endpoints) == 0 {
		return fmt.Errorf("manifest: device.endpoints empty")
	}
	for i, ep := range m.Device.Endpoints {
		if ep.IP == "" {
			return fmt.Errorf("manifest: device.endpoints[%d].ip empty", i)
		}
		if ep.Port <= 0 || ep.Port > 65535 {
			return fmt.Errorf("manifest: device.endpoints[%d].port out of range: %d", i, ep.Port)
		}
		if ep.Transport != "tcp" && ep.Transport != "udp" {
			return fmt.Errorf("manifest: device.endpoints[%d].transport %q (want tcp|udp)", i, ep.Transport)
		}
	}
	if len(m.Frames) == 0 {
		return fmt.Errorf("manifest: frames empty")
	}
	for fi, fr := range m.Frames {
		if len(fr.Slots) == 0 {
			return fmt.Errorf("manifest: frames[%d].slots empty", fi)
		}
		for si, sl := range fr.Slots {
			if sl.DM == "" {
				return fmt.Errorf("manifest: frames[%d].slots[%d].dm empty", fi, si)
			}
			if !strings.Contains(sl.DM, "@") {
				return fmt.Errorf("manifest: frames[%d].slots[%d].dm %q must be Model@SwRev", fi, si, sl.DM)
			}
		}
	}
	return nil
}

// DMPath returns the canonical on-disk path for a manifest DM reference
// rooted at cacheDir (typically `.cache/`).
//
//	cacheDir=".cache" + proto="acp2" + dm="SHPRM1@5.3.5"
//	→ .cache/dm/acp2/SHPRM1@5.3.5.json
func DMPath(cacheDir, proto, dm string) string {
	// dm may already carry the .json suffix (auto-written manifests
	// since chunk 72ed361). Avoid doubling the extension.
	if !strings.HasSuffix(dm, ".json") {
		dm = dm + ".json"
	}
	return filepath.Join(cacheDir, "dm", proto, dm)
}
