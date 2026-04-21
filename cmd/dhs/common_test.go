package main

import "testing"

// TestRawFrameFilename — the mapping from protocol name to --capture
// <dir> raw-frame filename (issue #41). Must match the transport
// framing each plugin speaks so anyone looking at a capture folder
// can read the filename and know the wire format without opening it.
func TestRawFrameFilename(t *testing.T) {
	cases := []struct {
		proto string
		want  string
	}{
		{"acp1", "raw.acp1.jsonl"},
		{"acp2", "raw.an2.jsonl"},
		{"emberplus", "raw.s101.jsonl"},
		{"", "raw.jsonl"},
		{"unknown", "raw.jsonl"},
	}
	for _, c := range cases {
		t.Run(c.proto, func(t *testing.T) {
			got := rawFrameFilename(c.proto)
			if got != c.want {
				t.Errorf("rawFrameFilename(%q) = %q, want %q", c.proto, got, c.want)
			}
		})
	}
}
