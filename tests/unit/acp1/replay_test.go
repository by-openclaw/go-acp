// Package acp1_test — replay tests decode real device captures from
// testdata/acp1/ through the ACP1 message decoder, verifying that
// every message decodes without error and that reply counts match
// the known emulator layout.
package acp1_test

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"acp/internal/protocol/acp1"
)

type captureRecord struct {
	Timestamp string `json:"ts"`
	Proto     string `json:"proto"`
	Direction string `json:"dir"`
	Hex       string `json:"hex"`
	Len       int    `json:"len"`
}

func loadCapture(t *testing.T, name string) []captureRecord {
	t.Helper()
	path := filepath.Join("..", "..", "fixtures", "acp1", name)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	var recs []captureRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		// Skip LFS pointer files (CI without git-lfs installed).
		if len(line) > 0 && line[0] != '{' {
			t.Skip("testdata file is a Git LFS pointer, not actual content — skipping (install git-lfs or run `git lfs pull`)")
		}
		var r captureRecord
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		recs = append(recs, r)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return recs
}

// TestReplay_ACP1MessageDecode verifies every captured ACP1 message
// decodes through the message decoder without error.
func TestReplay_ACP1MessageDecode(t *testing.T) {
	recs := loadCapture(t, "slot0_walk.json")
	if len(recs) == 0 {
		t.Fatal("no records in capture")
	}

	var decoded, failed int
	var requests, replies int

	for i, r := range recs {
		raw, err := hex.DecodeString(r.Hex)
		if err != nil {
			t.Fatalf("record %d: hex decode: %v", i, err)
		}

		msg, err := acp1.Decode(raw)
		if err != nil {
			t.Errorf("record %d (%s): decode: %v", i, r.Direction, err)
			failed++
			continue
		}
		decoded++

		// PVER must always be 1.
		if msg.PVER != 1 {
			t.Errorf("record %d: PVER %d, want 1", i, msg.PVER)
		}

		switch msg.MType {
		case acp1.MTypeRequest:
			requests++
		case acp1.MTypeReply:
			replies++
		}
	}

	t.Logf("ACP1 messages: %d decoded, %d failed (%d req, %d reply)",
		decoded, failed, requests, replies)

	if failed > 0 {
		t.Errorf("%d messages failed to decode", failed)
	}
}

// TestReplay_ACP1PropertyDecode verifies that getObject replies can
// be decoded into typed objects with valid properties.
func TestReplay_ACP1PropertyDecode(t *testing.T) {
	recs := loadCapture(t, "slot0_walk.json")

	var objects int
	typeCounts := map[acp1.ObjectType]int{}

	for i, r := range recs {
		raw, err := hex.DecodeString(r.Hex)
		if err != nil {
			continue
		}
		msg, err := acp1.Decode(raw)
		if err != nil {
			continue
		}

		// Only interested in getObject replies (MCODE=5, MType=reply).
		if msg.MType != acp1.MTypeReply || msg.MCode != byte(acp1.MethodGetObject) {
			continue
		}

		obj, err := acp1.DecodeObject(msg.Value)
		if err != nil {
			t.Errorf("record %d: DecodeObject(group=%d, id=%d): %v",
				i, msg.ObjGroup, msg.ObjID, err)
			continue
		}
		objects++
		typeCounts[obj.Type]++
	}

	t.Logf("objects decoded: %d", objects)
	for typ, count := range typeCounts {
		t.Logf("  type %d: %d", typ, count)
	}

	// Emulator slot 0 should have at least 50 objects across all groups.
	if objects < 50 {
		t.Errorf("expected ≥50 objects for slot 0 walk, got %d", objects)
	}
}

// TestReplay_ACP1MTIDSequence verifies MTID rules: requests have
// non-zero MTID, replies match their request MTID.
func TestReplay_ACP1MTIDSequence(t *testing.T) {
	recs := loadCapture(t, "slot0_walk.json")

	var lastReqMTID uint32
	for i, r := range recs {
		raw, err := hex.DecodeString(r.Hex)
		if err != nil {
			continue
		}
		msg, err := acp1.Decode(raw)
		if err != nil {
			continue
		}

		if msg.MType == acp1.MTypeRequest {
			if msg.MTID == 0 {
				t.Errorf("record %d: request with MTID=0 (spec: must be non-zero)", i)
			}
			lastReqMTID = msg.MTID
		}
		if msg.MType == acp1.MTypeReply {
			if msg.MTID != lastReqMTID {
				t.Errorf("record %d: reply MTID=%d doesn't match last request MTID=%d",
					i, msg.MTID, lastReqMTID)
			}
		}
	}
}
