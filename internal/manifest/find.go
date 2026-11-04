package manifest

import (
	"os"
	"path/filepath"
	"strings"
)

// One device, many controllers (ADR-0028 §2): the device has ONE
// manifest keyed by its primary IP and every controller IP listed in it
// as an endpoint. The lookups below find that manifest from any of them.

// direct reports a manifest whose device is reached at its own address
// (its key IP is one of its endpoints). A device behind a control plane
// (cerebrum-nb: every endpoint is the Northbound server) is not direct,
// and its endpoints say nothing about the device's own IPs.
func (m *Manifest) direct() bool {
	for _, e := range m.Device.Endpoints {
		if e.IP == m.Device.IP {
			return true
		}
	}
	return false
}

// each loads every manifest of proto under cacheDir.
func each(cacheDir, proto string) []*Manifest {
	paths, _ := filepath.Glob(filepath.Join(cacheDir, "manifest", sanitizeKeySeg(proto), "*.json"))
	var out []*Manifest
	for _, p := range paths {
		if m, err := Load(p); err == nil {
			out = append(out, m)
		}
	}
	return out
}

// PrimaryOf returns the key IP of the one direct device of proto that
// lists ip as a controller. found is false when no manifest does; a
// second device listing the same IP is a conflict, reported in
// conflict and never resolved by picking one.
func PrimaryOf(cacheDir, proto, ip string) (primary string, found bool, conflict []string) {
	for _, m := range each(cacheDir, proto) {
		if !m.direct() {
			continue
		}
		for _, e := range m.Device.Endpoints {
			if e.IP == ip {
				conflict = append(conflict, m.Device.IP)
				break
			}
		}
	}
	if len(conflict) == 1 {
		return conflict[0], true, nil
	}
	if len(conflict) == 0 {
		return "", false, nil
	}
	return "", false, conflict
}

// ByUnit returns the key IP of the direct device of proto whose unit
// facts carry field = value (a serial number), other than self: the
// same box seen at another controller IP. Empty when none, or when
// more than one claims it.
func ByUnit(cacheDir, proto, field, value, self string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	match := ""
	for _, m := range each(cacheDir, proto) {
		if !m.direct() || m.Device.IP == self || m.Device.Unit[field] != value {
			continue
		}
		if match != "" {
			return ""
		}
		match = m.Device.IP
	}
	return match
}

// Remove deletes the manifest a key IP owns, once its controller has
// joined another device's manifest.
func Remove(cacheDir, proto, key string) {
	_ = os.Remove(filepath.Join(cacheDir, "manifest", sanitizeKeySeg(proto), sanitizeKeySeg(key)+".json"))
}
