package mnset

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ADR-0025 deliverable 6: the module's wire, replayable from the repo.
// A REST connector's "frame" is one exchange — a request and the
// document that came back — so testdata/protocol_types/<kind>/ holds
// one of each kind the module serves, captured from the FusioN6 at
// 10.6.40.53 on 2026-09-24.
//
// Re-capture with:
//
//	dhs consumer mnset walk 10.6.40.53 --slot 0 --capture cap/
//
// What these tests are for: the decoder reads a body the device really
// sent, not one a developer typed from memory. A module that changes
// shape in a firmware update breaks them here, offline, instead of at
// 03:00 on a live walk.

const replayRoot = "../testdata"

// captureRecord is one ADR-0028 capture line.
type captureRecord struct {
	Proto string `json:"proto"`
	Dir   string `json:"dir"`
	Hex   string `json:"hex"`
}

// exchange reads one protocol_types folder: the request line, and the
// body the module answered with.
func exchange(t *testing.T, kind string) (request string, body []byte) {
	t.Helper()
	f, err := os.Open(filepath.Join(replayRoot, "protocol_types", kind, "wire.jsonl"))
	if err != nil {
		t.Fatalf("%s: %v", kind, err)
	}
	defer func() { _ = f.Close() }()

	dec := json.NewDecoder(f)
	for {
		var rec captureRecord
		if err := dec.Decode(&rec); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		raw, err := hex.DecodeString(rec.Hex)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		switch rec.Dir {
		case "tx":
			request, _, _ = strings.Cut(string(raw), "\r\n")
		case "rx":
			_, payload, _ := strings.Cut(string(raw), "\r\n\r\n")
			body = []byte(payload)
		}
	}
	if request == "" || len(body) == 0 {
		t.Fatalf("%s: the capture has no exchange in it", kind)
	}
	return request, body
}

// everyKind is every resource shape the module serves. Adding one here
// without adding the folder fails, which is the point: a new resource
// kind is a new thing the decoder has to survive.
var everyKind = []string{
	"root-listing", "self-information", "self-ipconfig", "port", "flow",
	"receivers", "senders", "refclk", "telemetry-node", "sdi-output",
	"sdi-audio", "clean-switch", "route-bulk", "sdp-text", "devices", "lldp",
}

func TestEveryCapturedResourceDecodes(t *testing.T) {
	for _, kind := range everyKind {
		t.Run(kind, func(t *testing.T) {
			req, body := exchange(t, kind)
			if !strings.HasPrefix(req, "GET /emsfp/node/v1") {
				t.Errorf("request = %q", req)
			}
			doc, err := decodeDoc(body)
			if kind == "sdp-text" {
				// The one resource that is not JSON: the connector
				// keeps it as the text the module sent, and a decode
				// that "succeeded" here would mean we had mangled it.
				if err == nil {
					t.Errorf("an SDP is text, not a document: %v", doc)
				}
				if !strings.HasPrefix(string(body), "v=0") {
					t.Errorf("SDP body = %.40q", body)
				}
				return
			}
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if doc == nil {
				t.Fatal("decoded to nothing")
			}
		})
	}
}

func TestTheCapturedBodiesAreTheOnesTheDecoderReads(t *testing.T) {
	// body.txt is what a reader of the fixture looks at; it must be
	// the same bytes the capture carries, or the folder lies.
	for _, kind := range everyKind {
		_, body := exchange(t, kind)
		side, err := os.ReadFile(filepath.Join(replayRoot, "protocol_types", kind, "body.txt"))
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if string(side) != string(body) {
			t.Errorf("%s: body.txt and wire.jsonl disagree", kind)
		}
	}
}

func TestTheModuleStillLooksLikeItselfInTheCapture(t *testing.T) {
	// A handful of facts the connector relies on, read from the wire
	// rather than asserted about the device: the identity it reports,
	// the root listing it walks from, and the shapes that decide how a
	// document is read.
	_, info := exchange(t, "self-information")
	doc, err := decodeDoc(info)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := doc.(map[string]any)
	if !ok {
		t.Fatalf("self/information is a document, got %T", doc)
	}
	// The two fields the DM key is built from (ADR-0022), plus the
	// serial an operator reads off the label. A firmware that renames
	// any of them files every export under a different identity, so
	// this is the assertion that earns its place.
	for _, k := range []string{"base_type", "current_version", "serial_number"} {
		if _, ok := m[k]; !ok {
			t.Errorf("self/information has no %q: %v", k, keysOf(m))
		}
	}
	if got := identityOf(m); got != "FusioN6@0x68cd783f" {
		t.Errorf("identity from the wire = %q", got)
	}

	_, root := exchange(t, "root-listing")
	list, err := decodeDoc(root)
	if err != nil {
		t.Fatal(err)
	}
	items, ok := list.([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("the node root is a listing, got %T", list)
	}
	for _, want := range []string{"self/", "flows/", "port/"} {
		found := false
		for _, it := range items {
			if s, _ := it.(string); s == want {
				found = true
			}
		}
		if !found {
			t.Errorf("the root listing no longer offers %q: %v", want, items)
		}
	}

	// An array document (receivers) and a plain-array one (devices)
	// are read differently; both shapes must still be what they were.
	_, recv := exchange(t, "receivers")
	if d, err := decodeDoc(recv); err != nil {
		t.Fatal(err)
	} else if _, ok := d.([]any); !ok {
		t.Errorf("receivers = %T, want an array", d)
	}
	_, devs := exchange(t, "devices")
	if d, err := decodeDoc(devs); err != nil {
		t.Fatal(err)
	} else if _, ok := d.([]any); !ok {
		t.Errorf("devices = %T, want an array", d)
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
