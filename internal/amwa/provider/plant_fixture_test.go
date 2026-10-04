package provider

// The Ansible plant fixture is a real NodeConfig consumed by the lab
// deployment — a bundle edit that fails validation would otherwise
// surface as a dead plant node after the next converge, three hops
// from the typo.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlantFixtureValidates(t *testing.T) {
	path := filepath.Join("..", "..", "..", "ansible", "roles", "dhs_amwa_plant", "files", "amwa-test-node.json")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("plant fixture not present: %v", err)
	}
	cfg, err := LoadNodeConfigFromFile(path)
	if err != nil {
		t.Fatalf("plant fixture does not validate: %v", err)
	}

	// The IS-08 constraint surface the AMWA suite exercises must stay
	// declared: a block-routing input and a restricted output.
	if cfg.ChannelMapping == nil {
		t.Fatal("plant fixture lost its channel_mapping seed")
	}
	io := deriveIO(cfg)
	in, ok := io.Inputs["2c47bf5e-1b2c-4abc-9def-deadbeef0031"]
	if !ok || in.Caps == nil || in.Caps.Reordering || in.Caps.BlockSize != 2 || len(in.Channels) < 4 {
		t.Errorf("block input caps = %+v (channels %d), want reordering=false block_size=2 with >=4 channels", in.Caps, len(in.Channels))
	}
	out, ok := io.Outputs["2c47bf5e-1b2c-4abc-9def-deadbeef0032"]
	if !ok || out.Caps == nil || len(out.Caps.RoutableInputs) != 2 || len(out.Channels) < 4 {
		t.Errorf("wide output = caps %+v (channels %d), want restricted routable_inputs and >=4 channels", out.Caps, len(out.Channels))
	}
	if len(cfg.ChannelMapping.BootMap) == 0 {
		t.Error("plant fixture lost its boot_map (the boot activation record the AMWA auto rows read)")
	}
}

// ADR-0025 deliverable 4 for the Node: the committed fixture the node is
// built from, with nothing but the repository. An NMOS Node has no
// DM + manifest — its fixture is its bundle, the resource graph it
// serves (internal/amwa/testdata/integration-test/README.md). Never
// skipped: a missing or invalid fixture is a failure.
func TestCommittedNodeFixtureBuildsTheNode(t *testing.T) {
	path := filepath.Join("..", "..", "..", "tests", "integration", "nmos", "amwa", "amwa-test-node.json")
	cfg, err := LoadNodeConfigFromFile(path)
	if err != nil {
		t.Fatalf("the committed node fixture does not build a node: %v", err)
	}
	if cfg.Node.ID == "" || len(cfg.Devices) == 0 || len(cfg.Senders) == 0 || len(cfg.Receivers) == 0 {
		t.Fatalf("the fixture is not a whole node: node %q, %d devices, %d senders, %d receivers",
			cfg.Node.ID, len(cfg.Devices), len(cfg.Senders), len(cfg.Receivers))
	}
}
