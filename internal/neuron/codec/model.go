// Package codec is the stdlib-only decoder for the EVS Neuron REST API
// (https://<neuron>/api/v1/), the control surface distinct from ACP2:
// HTTPS/JSON, essence-typed io/processing/matrix trees, and — unlike
// ACP2's numeric object ids — every stream resource carries a stable
// **UUID**. This lib addresses resources by that UUID, so a Neuron
// sender seen here lines up with the same sender in the plant's NMOS
// registry and (via the leg stream ids) the ACP2 view of the box.
//
// ADR-0006: stdlib only, lift-to-own-repo ready. Never imports dhs/*.
package codec

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Essence is the media class an io/ip stream carries.
type Essence string

const (
	EssenceVideo Essence = "video"
	EssenceAudio Essence = "audio"
	EssenceData  Essence = "data"
)

// Kind distinguishes the two stream directions.
type Kind string

const (
	KindSender   Kind = "sender"
	KindReceiver Kind = "receiver"
)

// Leg is one ST 2022-7 path of a sender: the multicast address, port,
// and the stream id the Neuron reports as `mac` (a UUID, not a MAC).
type Leg struct {
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	StreamID string `json:"mac"`
}

// Stream is one io/ip sender or receiver, keyed by its UUID.
type Stream struct {
	UUID      string  `json:"uuid"`
	Kind      Kind    `json:"-"`
	Essence   Essence `json:"-"`
	Name      string  `json:"name"`
	Enable    bool    `json:"enable"`
	MediaType string  `json:"mediaType,omitempty"`
	Legs      []Leg   `json:"legs,omitempty"`
	SDP       string  `json:"sdp,omitempty"`
	NMOS      struct {
		GroupHint string `json:"groupHint"`
		Label     string `json:"label"`
	} `json:"nmos"`
}

// Device is the decoded Neuron: its identity plus every stream, keyed
// by UUID.
type Device struct {
	ProductName    string `json:"productName"`
	ProductVersion string `json:"productVersion"`
	ModelVersion   int    `json:"modelVersion"`
	Streams        map[string]Stream
}

// Stream returns one stream by UUID.
func (d *Device) Stream(uuid string) (Stream, bool) {
	s, ok := d.Streams[uuid]
	return s, ok
}

// StreamsByKind returns the streams of one direction, UUID-sorted.
func (d *Device) StreamsByKind(k Kind) []Stream {
	var out []Stream
	for _, s := range d.Streams {
		if s.Kind == k {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UUID < out[j].UUID })
	return out
}

// selfBody is the /api/v1/self shape.
type selfBody struct {
	App struct {
		ProductName    string `json:"productName"`
		ProductVersion string `json:"productVersion"`
		ModelVersion   int    `json:"modelVersion"`
	} `json:"app"`
}

// DecodeSelf parses an /api/v1/self body into device identity.
func DecodeSelf(body []byte) (Device, error) {
	var s selfBody
	if err := json.Unmarshal(body, &s); err != nil {
		return Device{}, fmt.Errorf("neuron: decode self: %w", err)
	}
	return Device{
		ProductName:    s.App.ProductName,
		ProductVersion: s.App.ProductVersion,
		ModelVersion:   s.App.ModelVersion,
		Streams:        map[string]Stream{},
	}, nil
}

// DecodeStreams parses an io/ip senders|receivers/<essence> array into
// streams, stamping each with its kind + essence. Duplicate UUIDs (a
// device bug) keep the last, and a stream with no UUID is skipped with
// a reported reason rather than silently dropped.
func DecodeStreams(body []byte, kind Kind, essence Essence) ([]Stream, []string, error) {
	var raw []Stream
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil, fmt.Errorf("neuron: decode %s/%s: %w", kind, essence, err)
	}
	var skipped []string
	out := raw[:0]
	for i := range raw {
		if raw[i].UUID == "" {
			skipped = append(skipped, fmt.Sprintf("%s/%s[%d] %q has no uuid", kind, essence, i, raw[i].Name))
			continue
		}
		raw[i].Kind = kind
		raw[i].Essence = essence
		out = append(out, raw[i])
	}
	return out, skipped, nil
}
