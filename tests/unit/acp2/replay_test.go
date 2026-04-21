// Package acp2_test — replay tests decode real device captures from
// testdata/acp2/ through the AN2 framer and ACP2 codec, verifying that
// every frame and message decodes without error and that reply counts
// match the known device layout.
package acp2_test

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"acp/internal/acp2/consumer"
)

// captureRecord mirrors transport.CaptureRecord — duplicated here to
// avoid importing internal/transport from a test package.
type captureRecord struct {
	Timestamp string `json:"ts"`
	Proto     string `json:"proto"`
	Direction string `json:"dir"`
	Hex       string `json:"hex"`
	Len       int    `json:"len"`
}

// loadCapture reads a JSONL capture file and returns all records.
func loadCapture(t *testing.T, name string) []captureRecord {
	t.Helper()
	path := filepath.Join("..", "..", "fixtures", "acp2", name)
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

// TestReplay_AN2FrameDecode verifies every captured frame decodes
// through the AN2 framer without error.
func TestReplay_AN2FrameDecode(t *testing.T) {
	recs := loadCapture(t, "slot0_walk.json")
	if len(recs) == 0 {
		t.Fatal("no records in capture")
	}

	var decoded, failed int
	for i, r := range recs {
		raw, err := hex.DecodeString(r.Hex)
		if err != nil {
			t.Fatalf("record %d: hex decode: %v", i, err)
		}

		frame, err := acp2.ReadAN2Frame(bytes.NewReader(raw))
		if err != nil {
			t.Errorf("record %d (%s): AN2 decode: %v", i, r.Direction, err)
			failed++
			continue
		}
		decoded++

		// Verify basic AN2 invariants.
		if frame.Proto != acp2.AN2ProtoACP2 && frame.Proto != acp2.AN2ProtoInternal {
			t.Errorf("record %d: unexpected proto %d", i, frame.Proto)
		}
	}

	t.Logf("AN2 frames: %d decoded, %d failed out of %d total", decoded, failed, len(recs))
	if failed > 0 {
		t.Errorf("%d frames failed to decode", failed)
	}
}

// TestReplay_ACP2MessageDecode decodes the ACP2 payload from every
// AN2 data frame with proto=2 and verifies message structure.
func TestReplay_ACP2MessageDecode(t *testing.T) {
	recs := loadCapture(t, "slot0_walk.json")

	var messages, requests, replies, errors int
	for i, r := range recs {
		raw, err := hex.DecodeString(r.Hex)
		if err != nil {
			t.Fatalf("record %d: hex decode: %v", i, err)
		}

		frame, err := acp2.ReadAN2Frame(bytes.NewReader(raw))
		if err != nil {
			continue // already tested in AN2 test
		}

		// Only decode ACP2 data frames.
		if frame.Proto != acp2.AN2ProtoACP2 {
			continue
		}
		if frame.Type != acp2.AN2TypeData {
			continue
		}

		msg, err := acp2.DecodeACP2Message(frame.Payload)
		if err != nil {
			t.Errorf("record %d (%s): ACP2 decode: %v (hex=%s)",
				i, r.Direction, err, r.Hex)
			errors++
			continue
		}
		messages++

		switch msg.Type {
		case acp2.ACP2TypeRequest:
			requests++
		case acp2.ACP2TypeReply:
			replies++
			// Replies to get_object should have properties.
			if msg.Func == acp2.ACP2FuncGetObject && len(msg.Properties) == 0 {
				t.Errorf("record %d: get_object reply with 0 properties, obj_id=%d",
					i, msg.ObjID)
			}
		}
	}

	t.Logf("ACP2 messages: %d total (%d req, %d reply, %d errors)",
		messages, requests, replies, errors)

	// Slot 0 walk: root + all children. Expect at least 200 request/reply
	// pairs (214 objects × request + reply = ~428 messages).
	if requests < 200 {
		t.Errorf("expected ≥200 requests for slot 0 walk, got %d", requests)
	}
	if replies < 200 {
		t.Errorf("expected ≥200 replies for slot 0 walk, got %d", replies)
	}
}

// TestReplay_PropertyDecode verifies that properties in get_object
// replies decode with correct alignment and known PIDs.
func TestReplay_PropertyDecode(t *testing.T) {
	recs := loadCapture(t, "slot0_walk.json")

	var totalProps int
	pidCounts := map[uint8]int{}

	for i, r := range recs {
		raw, err := hex.DecodeString(r.Hex)
		if err != nil {
			continue
		}
		frame, err := acp2.ReadAN2Frame(bytes.NewReader(raw))
		if err != nil || frame.Proto != acp2.AN2ProtoACP2 || frame.Type != acp2.AN2TypeData {
			continue
		}

		msg, err := acp2.DecodeACP2Message(frame.Payload)
		if err != nil {
			continue
		}

		// Only interested in get_object replies.
		if msg.Type != acp2.ACP2TypeReply || msg.Func != acp2.ACP2FuncGetObject {
			continue
		}

		for _, p := range msg.Properties {
			totalProps++
			pidCounts[p.PID]++

			// PID must be 1-20 per spec.
			if p.PID < 1 || p.PID > 20 {
				t.Errorf("record %d obj_id=%d: property PID %d out of range [1,20]",
					i, msg.ObjID, p.PID)
			}
		}
	}

	t.Logf("properties decoded: %d total", totalProps)
	for pid, count := range pidCounts {
		t.Logf("  PID %2d: %d occurrences", pid, count)
	}

	// Every get_object reply should have at least pid=1 (object_type)
	// and pid=2 (label). Expect these to be the most common.
	if pidCounts[1] < 200 {
		t.Errorf("expected ≥200 object_type (pid=1) properties, got %d", pidCounts[1])
	}
	if pidCounts[2] < 200 {
		t.Errorf("expected ≥200 label (pid=2) properties, got %d", pidCounts[2])
	}
}
