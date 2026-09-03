package codec

// The committed Neuron OpenAPI spec (neuron-api-1.0.0.yml, fetched from
// the device's /api/v1/docs/api.yml) is the authoritative DM. These
// checks pin our hand-written model to the vendor's own schema so a
// firmware that reshapes the API is caught here, not in the field.
//
// Read as text (the repo is stdlib-only, no YAML parser) — the schema
// field names are unambiguous enough for a containment check.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func specText(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "neuron-api-1.0.0.yml"))
	if err != nil {
		t.Fatalf("read api spec: %v", err)
	}
	// Normalise CRLF → LF: a Windows checkout may store the committed
	// spec with CRLF, and the segment-boundary heuristic below keys on
	// "\n    ". Without this the schema-window math slices differently
	// on Windows and the uuid check false-negatives (CI, 2026-09-03).
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func TestSpecIsOpenAPINeuron(t *testing.T) {
	s := specText(t)
	if !strings.Contains(s, "openapi:") || !strings.Contains(s, "title: 'Neuron'") {
		t.Fatal("committed spec is not the Neuron OpenAPI document")
	}
}

// TestSpecStreamSchemasCarryUUID validates the design decision — the
// vendor's own sender/receiver schemas are UUID-keyed, so addressing by
// UUID (not oid) is correct, not our invention.
func TestSpecStreamSchemasCarryUUID(t *testing.T) {
	comp := specText(t)
	if i := strings.Index(comp, "components:"); i >= 0 {
		comp = comp[i:]
	}
	for _, schema := range []string{"IpSenderVideo", "IpReceiverVideo", "SelfGet"} {
		i := strings.Index(comp, schema+":")
		if i < 0 {
			t.Errorf("schema %s not found in the spec", schema)
			continue
		}
		seg := comp[i:]
		if end := strings.Index(seg[len(schema)+1:], "\n    "); end > 0 && end < 2000 {
			seg = seg[:end+len(schema)+1]
		} else if len(seg) > 2000 {
			seg = seg[:2000]
		}
		if !strings.Contains(seg, "uuid") {
			t.Errorf("schema %s does not carry a uuid field — the UUID-addressing design is not backed by the vendor spec", schema)
		}
	}
}

// TestSpecHasControlSurface records that the API is read+write: PUT
// operations exist for routing and matrix. Future connector units
// (get/set/route by UUID) build on these.
func TestSpecHasControlSurface(t *testing.T) {
	s := specText(t)
	for _, op := range []string{"PutIpReceiverVideo", "PutMatrixAudioMainState"} {
		if !strings.Contains(s, op) {
			t.Errorf("expected control operation %s in the spec", op)
		}
	}
}
