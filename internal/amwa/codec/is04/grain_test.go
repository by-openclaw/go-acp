package is04_test

import (
	"encoding/json"
	"errors"
	"testing"

	"dhs/internal/amwa/codec/is04"
)

// IS-04 §5.2 encodes what happened by WHICH SIDE IS PRESENT, not by a type
// field. Getting this wrong means a controller reports a removal as a change.
func TestGrainDataRowKind(t *testing.T) {
	tests := []struct {
		name string
		row  is04.GrainDataRow
		want is04.ChangeKind
	}{
		{"added: post only", is04.GrainDataRow{Path: "a", Post: json.RawMessage(`{"id":"a"}`)}, is04.ChangeAdded},
		{"modified: both", is04.GrainDataRow{Path: "a", Pre: json.RawMessage(`{"id":"a"}`), Post: json.RawMessage(`{"id":"a"}`)}, is04.ChangeModified},
		{"removed: pre only", is04.GrainDataRow{Path: "a", Pre: json.RawMessage(`{"id":"a"}`)}, is04.ChangeRemoved},
		{"neither side", is04.GrainDataRow{Path: "a"}, is04.ChangeUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.row.Kind(); got != tc.want {
				t.Errorf("Kind() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGrainDataRowLabel(t *testing.T) {
	tests := []struct {
		name string
		row  is04.GrainDataRow
		want string
	}{
		{
			"prefers post",
			is04.GrainDataRow{Pre: json.RawMessage(`{"label":"old"}`), Post: json.RawMessage(`{"label":"new"}`)},
			"new",
		},
		{
			"falls back to pre on removal",
			is04.GrainDataRow{Pre: json.RawMessage(`{"label":"gone"}`)},
			"gone",
		},
		{"absent label", is04.GrainDataRow{Post: json.RawMessage(`{"id":"x"}`)}, ""},
		{"no payload", is04.GrainDataRow{}, ""},
		{"unparseable payload", is04.GrainDataRow{Post: json.RawMessage(`not json`)}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.row.Label(); got != tc.want {
				t.Errorf("Label() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDecodeGrain(t *testing.T) {
	// Shape taken from IS-04 §5.2: the envelope wraps a change set whose topic
	// is the subscribed resource path.
	raw := []byte(`{
	  "grain_type": "event",
	  "source_id": "6c1b2b3c-0000-4000-8000-000000000001",
	  "flow_id": "6c1b2b3c-0000-4000-8000-000000000002",
	  "origin_timestamp": "1787500000:0",
	  "sync_timestamp": "1787500000:0",
	  "creation_timestamp": "1787500000:0",
	  "rate": {"numerator": 0, "denominator": 1},
	  "duration": {"numerator": 0, "denominator": 1},
	  "grain": {
	    "type": "urn:x-nmos:format:data.event",
	    "topic": "/senders/",
	    "data": [
	      {"path": "id-1", "post": {"id": "id-1", "label": "VTX-01"}},
	      {"path": "id-2", "pre": {"id": "id-2", "label": "VTX-02"}}
	    ]
	  }
	}`)

	g, err := is04.DecodeGrain(raw)
	if err != nil {
		t.Fatalf("DecodeGrain: %v", err)
	}
	if g.GrainType != "event" {
		t.Errorf("grain_type = %q, want event", g.GrainType)
	}
	if g.Grain.Topic != "/senders/" {
		t.Errorf("topic = %q, want /senders/", g.Grain.Topic)
	}
	if g.Rate.Denominator != 1 {
		t.Errorf("rate denominator = %d, want 1", g.Rate.Denominator)
	}
	if len(g.Grain.Data) != 2 {
		t.Fatalf("data rows = %d, want 2", len(g.Grain.Data))
	}
	if k := g.Grain.Data[0].Kind(); k != is04.ChangeAdded {
		t.Errorf("row 0 kind = %q, want added", k)
	}
	if k := g.Grain.Data[1].Kind(); k != is04.ChangeRemoved {
		t.Errorf("row 1 kind = %q, want removed", k)
	}
	if l := g.Grain.Data[0].Label(); l != "VTX-01" {
		t.Errorf("row 0 label = %q, want VTX-01", l)
	}
}

func TestDecodeGrainRejects(t *testing.T) {
	if _, err := is04.DecodeGrain([]byte(`{`)); err == nil {
		t.Error("malformed JSON should be an error")
	}
	// Well-formed JSON that is not a grain: a peer sending something else on
	// the subscription socket must be distinguishable from a decode failure,
	// so the caller can skip it and keep watching.
	_, err := is04.DecodeGrain([]byte(`{"hello":"world"}`))
	if !errors.Is(err, is04.ErrNotGrain) {
		t.Errorf("err = %v, want ErrNotGrain", err)
	}
}
