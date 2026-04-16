// Package export serializes and deserializes device snapshots.
//
// A "device snapshot" is everything a walker knows about one device at a
// moment in time: the device IP/port/protocol, plus one or more walked
// slots, each containing the flat list of decoded protocol.Object values.
// The package supports three formats:
//
//	JSON  — lossless, stdlib encoding/json, single file
//	YAML  — lossless, hand-rolled minimal emitter (no external deps)
//	CSV   — lossy, stdlib encoding/csv, one row per object
//
// The same Snapshot type is the input to all three encoders and the
// output of the JSON/YAML decoders. CSV import is not supported — CSV
// cannot round-trip enum item lists, slot status arrays, or alarm
// message pairs without ambiguity.
package export

import (
	"fmt"
	"time"

	"acp/internal/protocol"
)

// Snapshot is the top-level object every export file contains. It is
// protocol-agnostic by design — both the ACP1 plugin today and the
// future ACP2 plugin produce identical Snapshot shapes. Downstream
// consumers (CLI, REST API, UI) treat them the same.
type Snapshot struct {
	Device    DeviceInfo   `json:"device"`
	Slots     []SlotDump   `json:"slots"`
	Generator string       `json:"generator,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
}

// DeviceInfo is the per-snapshot device header. Mirrors the subset of
// protocol.DeviceInfo we care about for persistence.
type DeviceInfo struct {
	IP              string `json:"ip"`
	Port            int    `json:"port"`
	Protocol        string `json:"protocol"`
	ProtocolVersion int    `json:"protocol_version,omitempty"`
	NumSlots        int    `json:"num_slots"`
}

// SlotDump is one walked slot with its object tree. Objects is a copy
// of the walker's output; callers should not mutate it post-snapshot.
type SlotDump struct {
	Slot    int               `json:"slot"`
	Status  string            `json:"status,omitempty"`
	WalkedAt time.Time        `json:"walked_at"`
	Objects []protocol.Object `json:"objects"`
}

// Format is the enum of supported serialization formats.
type Format int

const (
	FormatJSON Format = iota
	FormatYAML
	FormatCSV
)

// ParseFormat translates the CLI/API string form into the enum. Case
// insensitive. Empty string defaults to JSON.
func ParseFormat(s string) (Format, error) {
	switch s {
	case "", "json", "JSON", "Json":
		return FormatJSON, nil
	case "yaml", "YAML", "Yaml", "yml":
		return FormatYAML, nil
	case "csv", "CSV", "Csv":
		return FormatCSV, nil
	}
	return FormatJSON, fmt.Errorf("unknown format %q (use json, yaml, or csv)", s)
}

// String returns the canonical file extension for a Format.
func (f Format) String() string {
	switch f {
	case FormatYAML:
		return "yaml"
	case FormatCSV:
		return "csv"
	default:
		return "json"
	}
}
