// Layer-3 IS-04 Node API provider — config loader.
//
// A Node serves its own resource graph: Node (singleton), Devices (1+),
// Sources (0+), Flows (0+), Senders (0+), Receivers (0+). The producer
// loads a single JSON file holding the union, validates each resource
// against its IS-04 v1.3 schema, then serves the Node API on top.

package provider

import (
	"encoding/json"
	"fmt"
	"os"

	"dhs/internal/amwa/codec/is04"
)

// NodeConfig is the on-disk Node bundle. Fields parallel the Node API
// collections; the file is JSON, one section per resource type.
type NodeConfig struct {
	Node      is04.Node       `json:"node"`
	Devices   []is04.Device   `json:"devices"`
	Sources   []is04.Source   `json:"sources"`
	Flows     []is04.Flow     `json:"flows"`
	Senders   []is04.Sender   `json:"senders"`
	Receivers []is04.Receiver `json:"receivers"`
}

// LoadNodeConfigFromFile reads + validates a Node config bundle.
func LoadNodeConfigFromFile(path string) (*NodeConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("provider/node: read %s: %w", path, err)
	}
	var cfg NodeConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("provider/node: %s: %w", path, err)
	}
	return &cfg, validateBundle(&cfg)
}

// validateBundle runs each resource's Validate + cross-resource
// referential-integrity checks.
func validateBundle(cfg *NodeConfig) error {
	if err := cfg.Node.Validate(); err != nil {
		return err
	}
	nodeID := cfg.Node.ID
	deviceIDs := map[string]struct{}{}
	for i := range cfg.Devices {
		d := &cfg.Devices[i]
		if err := d.Validate(); err != nil {
			return fmt.Errorf("devices[%d]: %w", i, err)
		}
		if d.NodeID != nodeID {
			return fmt.Errorf("devices[%d].node_id %q: must equal node.id %q", i, d.NodeID, nodeID)
		}
		deviceIDs[d.ID] = struct{}{}
	}
	for i := range cfg.Sources {
		s := &cfg.Sources[i]
		if err := s.Validate(); err != nil {
			return fmt.Errorf("sources[%d]: %w", i, err)
		}
		if _, ok := deviceIDs[s.DeviceID]; !ok {
			return fmt.Errorf("sources[%d].device_id %q: not declared under devices[]", i, s.DeviceID)
		}
	}
	for i := range cfg.Flows {
		f := &cfg.Flows[i]
		if err := f.Validate(); err != nil {
			return fmt.Errorf("flows[%d]: %w", i, err)
		}
		if _, ok := deviceIDs[f.DeviceID]; !ok {
			return fmt.Errorf("flows[%d].device_id %q: not declared under devices[]", i, f.DeviceID)
		}
	}
	for i := range cfg.Senders {
		s := &cfg.Senders[i]
		if err := s.Validate(); err != nil {
			return fmt.Errorf("senders[%d]: %w", i, err)
		}
		if _, ok := deviceIDs[s.DeviceID]; !ok {
			return fmt.Errorf("senders[%d].device_id %q: not declared under devices[]", i, s.DeviceID)
		}
	}
	for i := range cfg.Receivers {
		r := &cfg.Receivers[i]
		if err := r.Validate(); err != nil {
			return fmt.Errorf("receivers[%d]: %w", i, err)
		}
		if _, ok := deviceIDs[r.DeviceID]; !ok {
			return fmt.Errorf("receivers[%d].device_id %q: not declared under devices[]", i, r.DeviceID)
		}
	}
	return nil
}
