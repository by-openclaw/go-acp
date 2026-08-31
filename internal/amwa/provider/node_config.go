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
	"dhs/internal/amwa/codec/is05"
	"dhs/internal/amwa/codec/is08"
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

	// EventTypes carries IS-07 `type` documents, keyed by Source id.
	//
	// Not derivable from IS-04, and that is the point: IS-04 says a
	// Source emits `boolean` events, and IS-07's type document says
	// whether those two boolean values are called "on"/"off" or
	// "PGM"/"PVW" and what a controller should render. An enum source
	// is exactly a source whose type document carries `values`, so
	// without a place to declare them the Node can only ever publish
	// unlabelled primitives.
	//
	// Optional. A Source with no entry here gets the plain type
	// document its event_type implies.
	EventTypes map[string]json.RawMessage `json:"event_types,omitempty"`

	// StreamCompatibility seeds the IS-11 surface: Inputs and Outputs
	// (physical interfaces, not derivable from IS-04), their
	// association with Senders/Receivers, the constraint URNs each
	// Sender supports, and optional EDID blobs. Optional — a Node
	// without it serves an empty (but valid) IS-11 tree.
	StreamCompatibility *StreamCompatSeed `json:"stream_compatibility,omitempty"`

	// ChannelMapping seeds the IS-08 caps per input/output id. Not
	// derivable from IS-04: whether a device can re-order channels or
	// routes them in hardware blocks is channel-mapping vocabulary
	// only. Optional — inputs default to {reordering:true, block_size:1}
	// and outputs to the unrestricted routable list.
	ChannelMapping *ChannelMappingSeed `json:"channel_mapping,omitempty"`

	// Connection seeds the IS-05 boot state per endpoint id.
	//
	// Without it every endpoint boots master_enable=false with one leg
	// of unset parameters — correct for a factory-fresh device, and
	// useless for a device that is supposed to LOOK configured: a
	// controller (or Cerebrum) reading ACTIVE sees no multicast group,
	// no ports, nothing. IS-04 has no field for any of this — transport
	// configuration is IS-05's — which is why it is a sibling block
	// keyed by resource id rather than extra keys smuggled into the
	// IS-04 resources (those are schema-checked on the wire).
	Connection *ConnectionSeed `json:"connection,omitempty"`
}

// ChannelMappingSeed maps IS-08 io ids (the IS-04 resource ids the io
// view derives from) to declared caps.
type ChannelMappingSeed struct {
	Inputs  map[string]*ChannelMappingInputSeed  `json:"inputs,omitempty"`
	Outputs map[string]*ChannelMappingOutputSeed `json:"outputs,omitempty"`

	// BootMap is a channel map applied at startup through the normal
	// immediate-activation path, so the device boots with its channels
	// routed AND an activation record a controller can read — the
	// map/active twin of the IS-05 connection seed. Shape is exactly
	// an IS-08 action: output id → channel index → {input,
	// channel_index}. It is validated against the declared caps at
	// load; a boot map the device itself would 400 is a config error.
	BootMap map[string]map[string]is08.MapEntry `json:"boot_map,omitempty"`
}

// ChannelMappingInputSeed declares one input's routing constraints.
type ChannelMappingInputSeed struct {
	Reordering *bool `json:"reordering,omitempty"`
	BlockSize  *int  `json:"block_size,omitempty"`
}

// ChannelMappingOutputSeed restricts which inputs an output accepts.
// A null entry in the list keeps unrouting legal (IS-08 spells
// "may be left unrouted" exactly that way).
type ChannelMappingOutputSeed struct {
	RoutableInputs []*string `json:"routable_inputs,omitempty"`
}

// validateChannelMappingSeed rejects seeds naming unknown io ids or
// declaring impossible caps. The io ids are checked against the SAME
// derivation deriveIO uses, so a typo fails at load rather than
// surfacing as an AMWA CouldNotTest four suites later.
func validateChannelMappingSeed(cfg *NodeConfig) error {
	if cfg.ChannelMapping == nil {
		return nil
	}
	io := deriveIO(cfg)
	for id, seed := range cfg.ChannelMapping.Inputs {
		if _, ok := io.Inputs[id]; !ok {
			return fmt.Errorf("channel_mapping.inputs[%q]: no such channel-mapping input derives from the bundle", id)
		}
		if seed != nil && seed.BlockSize != nil && *seed.BlockSize < 1 {
			return fmt.Errorf("channel_mapping.inputs[%q].block_size %d: must be >= 1", id, *seed.BlockSize)
		}
	}
	for id, seed := range cfg.ChannelMapping.Outputs {
		if _, ok := io.Outputs[id]; !ok {
			return fmt.Errorf("channel_mapping.outputs[%q]: no such channel-mapping output derives from the bundle", id)
		}
		if seed == nil {
			continue
		}
		for i, in := range seed.RoutableInputs {
			if in == nil {
				continue
			}
			if _, ok := io.Inputs[*in]; !ok {
				return fmt.Errorf("channel_mapping.outputs[%q].routable_inputs[%d] %q: not an input of this bundle", id, i, *in)
			}
		}
	}
	if len(cfg.ChannelMapping.BootMap) > 0 {
		if err := validateAction(io, is08.MapEntries(cfg.ChannelMapping.BootMap)); err != nil {
			return fmt.Errorf("channel_mapping.boot_map: %w", err)
		}
	}
	return nil
}

// ConnectionSeed maps IS-04 resource ids to their boot connection
// state.
type ConnectionSeed struct {
	Senders   map[string]*EndpointSeed `json:"senders,omitempty"`
	Receivers map[string]*EndpointSeed `json:"receivers,omitempty"`
}

// EndpointSeed is one endpoint's boot state. TransportParams follows
// IS-05 exactly: one map per leg, two legs meaning ST 2022-7. Values
// override the transport's defaults; "auto" survives and resolves the
// way a controller-written "auto" would. master_enable=true makes the
// Node promote the endpoint at boot — active params go concrete and a
// Sender gets its SDP — as if a controller had activated it.
type EndpointSeed struct {
	MasterEnable    bool                   `json:"master_enable"`
	TransportParams []is05.TransportParams `json:"transport_params,omitempty"`
}

// validateConnectionSeed rejects seeds that name unknown endpoints or
// carry parameters the endpoint's transport does not define.
//
// The key check matters more than it looks: an unknown key would not
// fail loudly anywhere downstream — it would ride into staged, the
// constraints envelope would not carry it, and the AMWA tool would
// report the mismatch as "Invalid combination of parameters on
// constraints endpoint" four suites away from the typo.
func validateConnectionSeed(cfg *NodeConfig) error {
	if cfg.Connection == nil {
		return nil
	}
	check := func(kind string, seeds map[string]*EndpointSeed, transportOf map[string]string, isSender bool) error {
		for id, seed := range seeds {
			transport, ok := transportOf[id]
			if !ok {
				return fmt.Errorf("connection.%s[%q]: no such %s in the bundle", kind, id, kind[:len(kind)-1])
			}
			if seed == nil {
				continue
			}
			legal := defaultLegParams(transport, isSender)
			if len(seed.TransportParams) > 2 {
				return fmt.Errorf("connection.%s[%q]: %d legs — IS-05 RTP carries at most 2 (ST 2022-7)", kind, id, len(seed.TransportParams))
			}
			if len(seed.TransportParams) == 2 && !isRTP(transport) {
				return fmt.Errorf("connection.%s[%q]: two legs on %s — only RTP transports carry a 2022-7 pair", kind, id, transport)
			}
			for li, leg := range seed.TransportParams {
				for k := range leg {
					if _, ok := legal[k]; !ok {
						return fmt.Errorf("connection.%s[%q].transport_params[%d]: %q is not a parameter of %s", kind, id, li, k, transport)
					}
				}
			}
		}
		return nil
	}
	sndTransport := map[string]string{}
	for i := range cfg.Senders {
		sndTransport[cfg.Senders[i].ID] = cfg.Senders[i].Transport
	}
	rcvTransport := map[string]string{}
	for i := range cfg.Receivers {
		rcvTransport[cfg.Receivers[i].ID] = cfg.Receivers[i].Transport
	}
	if err := check("senders", cfg.Connection.Senders, sndTransport, true); err != nil {
		return err
	}
	return check("receivers", cfg.Connection.Receivers, rcvTransport, false)
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
	if err := validateConnectionSeed(cfg); err != nil {
		return err
	}
	return validateChannelMappingSeed(cfg)
}
