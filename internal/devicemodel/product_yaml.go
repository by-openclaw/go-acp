package devicemodel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"dhs/internal/identity"
)

// ProductMeta is the cross-protocol metadata for one product entry. It
// lives in product.yaml at the root of every model-rev directory:
// <root>/<vendor>/<product>/<model>-<sw_rev>/product.yaml.
type ProductMeta struct {
	Vendor      string `json:"vendor"`
	Product     string `json:"product"`
	Model       string `json:"model"`
	SwRev       string `json:"sw_rev"`
	HwRev       string `json:"hw_rev,omitempty"`
	Description string `json:"description,omitempty"`
}

// IdentityProbe holds the per-protocol identity-probe configuration.
// ACP1 is documentation-only (deterministic fixed-pid lookup at group=1).
// ACP2 caches the alias-scan result so future encounters skip the scan.
// Future protocols slot in alongside.
type IdentityProbe struct {
	ACP1 *IdentityProbeACP1 `json:"acp1,omitempty"`
	ACP2 *IdentityProbeACP2 `json:"acp2,omitempty"`
}

// IdentityProbeACP1 documents the deterministic ACP1 identity probe.
// The fields are fixed by spec p.20; this section is informational.
type IdentityProbeACP1 struct {
	Group int          `json:"group"`
	Pids  IdentityPids `json:"pids"`
}

// IdentityProbeACP2 caches the runtime-resolved ACP2 identity PIDs and
// the labels the alias-scan saw at first encounter. Empty on first
// encounter; populated after the alias-scan completes.
type IdentityProbeACP2 struct {
	Pids        IdentityPids    `json:"pids"`
	AliasesSeen IdentityAliases `json:"aliases_seen"`
}

// IdentityPids is the (Model, SwRev, HwRev) PID triple a plugin uses to
// fetch identity. ACP1 PIDs are bytes (0..255); ACP2 PIDs are u32.
type IdentityPids struct {
	Model int `json:"model"`
	SwRev int `json:"sw_rev"`
	HwRev int `json:"hw_rev,omitempty"`
}

// IdentityAliases is the labels the ACP2 alias-scan matched on first
// encounter. Persisted for diagnostics.
type IdentityAliases struct {
	Model string `json:"model"`
	SwRev string `json:"sw_rev"`
	HwRev string `json:"hw_rev,omitempty"`
}

// WalkMetadata records when and by what tool the schema was captured.
// All fields optional.
type WalkMetadata struct {
	WalkedAt   time.Time `json:"walked_at,omitempty"`
	WalkedBy   string    `json:"walked_by,omitempty"`
	Controller string    `json:"controller,omitempty"`
	Notes      string    `json:"notes,omitempty"`
}

// productYAML is the on-disk envelope for product.yaml. The file is
// encoded as JSON (which is valid YAML 1.2) so we round-trip cleanly
// without depending on a third-party YAML library.
type productYAML struct {
	Product            ProductMeta   `json:"product"`
	SupportedProtocols []string      `json:"supported_protocols,omitempty"`
	IdentityProbe      IdentityProbe `json:"identity_probe,omitempty"`
	WalkMetadata       WalkMetadata  `json:"walk_metadata,omitempty"`
}

// LoadProductYAML reads product.yaml from disk. Returns os.ErrNotExist
// (wrapped) if the file is absent so callers can treat that as "no
// metadata yet" rather than a hard failure.
func LoadProductYAML(path string) (*ProductMeta, *IdentityProbe, *WalkMetadata, []string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("dmlib: read %s: %w", path, err)
	}
	var py productYAML
	if err := json.Unmarshal(raw, &py); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("dmlib: decode %s: %w", path, err)
	}
	if py.Product.Model == "" || py.Product.SwRev == "" {
		return nil, nil, nil, nil, fmt.Errorf("dmlib: %s: missing model or sw_rev", path)
	}
	return &py.Product, &py.IdentityProbe, &py.WalkMetadata, py.SupportedProtocols, nil
}

// SaveProductYAML writes product.yaml atomically. Vendor strings flow
// through identity.YAMLValue so untrusted wire content never reaches
// disk verbatim.
func SaveProductYAML(path string, m *ProductMeta, ip *IdentityProbe, wm *WalkMetadata, supported []string) error {
	if m == nil || m.Model == "" || m.SwRev == "" {
		return fmt.Errorf("dmlib: SaveProductYAML: missing model or sw_rev")
	}
	clean := *m
	clean.Vendor = identity.YAMLValue(m.Vendor)
	clean.Product = identity.YAMLValue(m.Product)
	clean.Model = identity.YAMLValue(m.Model)
	clean.SwRev = identity.YAMLValue(m.SwRev)
	clean.HwRev = identity.YAMLValue(m.HwRev)
	clean.Description = identity.YAMLValue(m.Description)

	py := productYAML{
		Product:            clean,
		SupportedProtocols: supported,
	}
	if ip != nil {
		py.IdentityProbe = *ip
	}
	if wm != nil {
		py.WalkMetadata = *wm
	}

	raw, err := json.MarshalIndent(py, "", "  ")
	if err != nil {
		return fmt.Errorf("dmlib: encode %s: %w", path, err)
	}
	raw = append(raw, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("dmlib: mkdir %s: %w", filepath.Dir(path), err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("dmlib: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("dmlib: rename %s: %w", path, err)
	}
	return nil
}
