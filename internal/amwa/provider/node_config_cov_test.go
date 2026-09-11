package provider

// The bundle is the operator's description of the plant, and a
// description that does not hang together must fail at load rather
// than four AMWA suites later, where the symptom names nothing that
// appears in the file.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is05"
)

// loadBundle writes a bundle to a file and loads it back, the way the
// CLI does.
func loadBundle(t *testing.T, cfg *NodeConfig) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "node.json")
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = LoadNodeConfigFromFile(path)
	return err
}

// A file that is not there, and one that is not JSON, are told apart:
// an operator who mistyped a path should not be reading a parser
// error.
func TestLoadRefusesWhatItCannotRead(t *testing.T) {
	if _, err := LoadNodeConfigFromFile(filepath.Join(t.TempDir(), "absent.json")); err == nil ||
		!strings.Contains(err.Error(), "read ") {
		t.Errorf("a file that is not there = %v", err)
	}

	path := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNodeConfigFromFile(path); err == nil ||
		strings.Contains(err.Error(), "read ") {
		t.Errorf("a file that is not JSON = %v", err)
	}
}

// Every resource is validated on its own terms and then against the
// devices declared alongside it. A dangling device_id describes a
// plant that cannot exist, and serving it publishes that fiction.
func TestBundleReferentialIntegrity(t *testing.T) {
	absent := "aaaaaaaa-9999-4999-8999-999999999999"

	for _, tc := range []struct {
		name   string
		break_ func(*NodeConfig)
		want   string
	}{
		{"a source on no device", func(c *NodeConfig) { c.Sources[0].DeviceID = absent }, "sources[0].device_id"},
		{"a flow on no device", func(c *NodeConfig) { c.Flows[0].DeviceID = absent }, "flows[0].device_id"},
		{"a sender on no device", func(c *NodeConfig) { c.Senders[0].DeviceID = absent }, "senders[0].device_id"},
		{"a receiver on no device", func(c *NodeConfig) { c.Receivers[0].DeviceID = absent }, "receivers[0].device_id"},

		{"a source that is not one", func(c *NodeConfig) { c.Sources[0].ID = "not-a-uuid" }, "sources[0]"},
		{"a flow that is not one", func(c *NodeConfig) { c.Flows[0].ID = "not-a-uuid" }, "flows[0]"},
		{"a sender that is not one", func(c *NodeConfig) { c.Senders[0].Transport = "" }, "senders[0]"},
		{"a receiver that is not one", func(c *NodeConfig) { c.Receivers[0].Transport = "" }, "receivers[0]"},
		{"a device that is not one", func(c *NodeConfig) { c.Devices[0].Type = "" }, "devices[0]"},
		{"a node that is not one", func(c *NodeConfig) { c.Node.Href = "" }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := fullBundle(t)
			tc.break_(b)
			err := validateBundle(b)
			if err == nil {
				t.Fatalf("%s must be refused", tc.name)
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Errorf("= %v, want %q named", err, tc.want)
			}
		})
	}
}

// The connection seed names endpoints and their parameters, and both
// are checked against the bundle: an unknown key would ride into
// staged, be absent from the constraints envelope, and surface as
// "Invalid combination of parameters on constraints endpoint" four
// suites away from the typo.
func TestConnectionSeedRefusals(t *testing.T) {
	absent := "aaaaaaaa-9999-4999-8999-999999999999"

	for _, tc := range []struct {
		name string
		seed func(*NodeConfig)
		want string
	}{
		{"a sender the bundle does not have", func(c *NodeConfig) {
			c.Connection = &ConnectionSeed{Senders: map[string]*EndpointSeed{absent: {}}}
		}, "no such sender"},

		{"a receiver the bundle does not have", func(c *NodeConfig) {
			c.Connection = &ConnectionSeed{Receivers: map[string]*EndpointSeed{absent: {}}}
		}, "no such receiver"},

		{"more legs than IS-05 carries", func(c *NodeConfig) {
			c.Connection = &ConnectionSeed{Senders: map[string]*EndpointSeed{
				c.Senders[0].ID: {TransportParams: []is05.TransportParams{{}, {}, {}}},
			}}
		}, "at most 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := fullBundle(t)
			tc.seed(b)
			err := validateBundle(b)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %q", err, tc.want)
			}
		})
	}
}

// A seed naming no endpoint at all is fine — an operator may declare
// the section and fill it in later — and one that names real ones
// loads.
func TestConnectionSeedThatIsFine(t *testing.T) {
	b := fullBundle(t)
	b.Connection = &ConnectionSeed{
		Senders:   map[string]*EndpointSeed{b.Senders[0].ID: nil},
		Receivers: map[string]*EndpointSeed{b.Receivers[0].ID: {MasterEnable: true}},
	}
	if err := loadBundle(t, b); err != nil {
		t.Fatalf("a seed naming real endpoints must load: %v", err)
	}
}

// The channel-mapping seed is checked against the same derivation the
// server uses, so a typo fails at load rather than surfacing as an
// AMWA CouldNotTest four suites later.
func TestChannelMappingSeedRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		seed func(*NodeConfig)
		want string
	}{
		{"an input the bundle does not derive", func(c *NodeConfig) {
			c.ChannelMapping = &ChannelMappingSeed{
				Inputs: map[string]*ChannelMappingInputSeed{"not-an-input": {}},
			}
		}, "no such channel-mapping input"},

		{"an output the bundle does not derive", func(c *NodeConfig) {
			c.ChannelMapping = &ChannelMappingSeed{
				Outputs: map[string]*ChannelMappingOutputSeed{"not-an-output": {}},
			}
		}, "no such channel-mapping output"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := audioBundle()
			tc.seed(b)
			err := validateBundle(b)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %q", err, tc.want)
			}
		})
	}
}

// The seed's own values are checked too, not just the ids it names: a
// block size below one describes a channel grouping that cannot exist,
// and a routable input that is not an input of this bundle names a
// route the server could never make.
func TestChannelMappingSeedValues(t *testing.T) {
	b := audioBundle()
	io := deriveIO(b)

	var input, output string
	for id := range io.Inputs {
		input = id
		break
	}
	for id := range io.Outputs {
		output = id
		break
	}
	if input == "" || output == "" {
		t.Fatal("the audio bundle derives at least one input and one output")
	}

	zero := 0
	b.ChannelMapping = &ChannelMappingSeed{
		Inputs: map[string]*ChannelMappingInputSeed{input: {BlockSize: &zero}},
	}
	if err := validateBundle(b); err == nil || !strings.Contains(err.Error(), "block_size") {
		t.Errorf("a block size below one = %v", err)
	}

	notAnInput := "not-an-input"
	b.ChannelMapping = &ChannelMappingSeed{
		Outputs: map[string]*ChannelMappingOutputSeed{
			output: {RoutableInputs: []*string{nil, &notAnInput}},
		},
	}
	if err := validateBundle(b); err == nil || !strings.Contains(err.Error(), "not an input") {
		t.Errorf("a routable input this bundle does not have = %v", err)
	}

	// An output named with no seed body at all is the operator saying
	// "this one exists, defaults are fine".
	b.ChannelMapping = &ChannelMappingSeed{
		Outputs: map[string]*ChannelMappingOutputSeed{output: nil},
	}
	if err := validateBundle(b); err != nil {
		t.Errorf("an output with no seed body = %v, want it accepted", err)
	}
}

// A transport parameter the endpoint's transport does not define would
// ride into staged, be absent from the constraints envelope, and
// surface four suites away as a constraints mismatch. It is refused at
// load, where the file that carries it is still in front of the
// operator.
func TestConnectionSeedRefusesAnUnknownParameter(t *testing.T) {
	b := fullBundle(t)
	b.Connection = &ConnectionSeed{Senders: map[string]*EndpointSeed{
		b.Senders[0].ID: {TransportParams: []is05.TransportParams{{"not_a_parameter": 1}}},
	}}

	err := validateBundle(b)
	if err == nil || !strings.Contains(err.Error(), "is not a parameter of") {
		t.Fatalf("= %v, want the unknown parameter named", err)
	}
}

// Two legs are an ST 2022-7 pair, which only RTP carries. A pair
// declared on an MQTT sender describes redundancy that transport has
// no notion of.
func TestConnectionSeedRefusesTwoLegsOffRTP(t *testing.T) {
	b := fullBundle(t)
	b.Senders[0].Transport = "urn:x-nmos:transport:mqtt"
	b.Connection = &ConnectionSeed{Senders: map[string]*EndpointSeed{
		b.Senders[0].ID: {TransportParams: []is05.TransportParams{{}, {}}},
	}}

	err := validateBundle(b)
	if err == nil || !strings.Contains(err.Error(), "2022-7 pair") {
		t.Fatalf("= %v, want the pair refused off RTP", err)
	}
}
