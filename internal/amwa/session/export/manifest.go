package export

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ManifestEntry is one captured device, as the manifest lists it.
//
// This is the index an operator opens first: which device is in which
// folder, at which address, under which name. Without it, identifying a
// device means opening 44 device.json files — or encoding the identity
// into the folder name, which is what pushed paths past the Windows
// 260-character limit.
type ManifestEntry struct {
	// Folder is relative to the manifest, so the index survives the
	// capture being moved or renamed.
	Folder string `json:"folder"`
	Target string `json:"target"`
	Host   string `json:"host"`
	Port   string `json:"port"`
	Role   string `json:"role"`

	Hostname string `json:"hostname,omitempty"`
	Label    string `json:"label,omitempty"`
	ID       string `json:"id,omitempty"`

	APIs     map[string][]string `json:"apis,omitempty"`
	Requests int                 `json:"requests"`
	Failures int                 `json:"failures"`
	SDPFiles int                 `json:"sdp_files"`
}

// Manifest indexes a whole capture.
type Manifest struct {
	CapturedAt string          `json:"captured_at"`
	Target     string          `json:"target"`
	Harvester  string          `json:"harvester"`
	Devices    []ManifestEntry `json:"devices"`
	// NodesListed is what the registry advertised; len(Devices)-1 is
	// what answered. The two differing is the plant's stale
	// registrations, and it belongs on the front page.
	NodesListed int `json:"nodes_listed,omitempty"`
}

// writeManifest builds and writes manifest.json at the capture root.
//
// It is written last, from the Result tree, so it reflects the final
// folder names rather than the ones the walk started with.
func writeManifest(root string, res *Result, opts Options) error {
	m := Manifest{
		CapturedAt:  opts.Now().Format("2006-01-02T15:04:05Z07:00"),
		Target:      res.Target,
		Harvester:   Version,
		NodesListed: res.NodesSeen,
	}
	collectManifest(root, res, &m)
	sort.SliceStable(m.Devices, func(i, j int) bool {
		// Registry first, then by address — the order someone reads it.
		if (m.Devices[i].Role == "registry") != (m.Devices[j].Role == "registry") {
			return m.Devices[i].Role == "registry"
		}
		return m.Devices[i].Target < m.Devices[j].Target
	})

	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "manifest.json"), b, 0o644)
}

// collectManifest walks a Result tree into manifest entries.
func collectManifest(root string, res *Result, m *Manifest) {
	rel, err := filepath.Rel(root, res.Dir)
	if err != nil {
		rel = res.Dir
	}
	host, port := splitHostPort(res.Target)
	m.Devices = append(m.Devices, ManifestEntry{
		Folder:   filepath.ToSlash(rel),
		Target:   res.Target,
		Host:     host,
		Port:     port,
		Role:     res.Role,
		Hostname: res.Hostname,
		Label:    res.Label,
		ID:       res.ID,
		APIs:     res.APIs,
		Requests: res.Requests,
		Failures: res.Failures,
		SDPFiles: res.SDPFiles,
	})
	for i := range res.Followed {
		collectManifest(root, &res.Followed[i], m)
	}
}

// splitHostPort separates `host:port` without failing on a bare host.
func splitHostPort(target string) (host, port string) {
	i := strings.LastIndex(target, ":")
	if i < 0 {
		return target, ""
	}
	return target[:i], target[i+1:]
}
