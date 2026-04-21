package scenario

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"acp/internal/acp2/consumer"
)

// Run dispatches the scenario to the right per-protocol runner and
// asserts the scenario's expectations. Call as a sub-test:
//
//	t.Run(scenarioName, func(t *testing.T) { scenario.Run(t, s) })
//
// Any mismatch is reported with t.Errorf or t.Fatalf as appropriate.
func Run(t *testing.T, s *Scenario) {
	t.Helper()
	switch s.Protocol {
	case "acp2":
		runACP2(t, s)
	case "acp1":
		t.Skipf("acp1 scenario runner not implemented yet (scenario %q)", s.Name)
	case "emberplus":
		t.Skipf("emberplus scenario runner not implemented yet (scenario %q)", s.Name)
	default:
		t.Fatalf("unknown protocol %q in scenario %q", s.Protocol, s.Name)
	}
}

// captureRecord mirrors the on-wire jsonl record shape the CLI's
// --capture recorder emits. One line per frame; `hex` holds the raw
// transport bytes.
type captureRecord struct {
	Timestamp string `json:"ts"`
	Protocol  string `json:"proto,omitempty"`
	Direction string `json:"dir"`
	Hex       string `json:"hex"`
	Length    int    `json:"len,omitempty"`
}

// readCapture opens the resolved wire.jsonl path (or legacy raw.*
// filenames) and returns every line as a parsed captureRecord. Skips
// a fixture file that's still a Git LFS pointer rather than failing
// hard — CI without git-lfs installed runs successfully.
func readCapture(t *testing.T, wirePath string) []captureRecord {
	t.Helper()
	f, err := os.Open(wirePath)
	if err != nil {
		t.Fatalf("open wire file %s: %v", wirePath, err)
	}
	defer func() { _ = f.Close() }()

	var recs []captureRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) > 0 && line[0] != '{' {
			t.Skipf("wire file %s is a Git LFS pointer, not real content — skipping", wirePath)
		}
		var r captureRecord
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatalf("unmarshal line in %s: %v", wirePath, err)
		}
		recs = append(recs, r)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", wirePath, err)
	}
	return recs
}

// runACP2 is the protocol-specific runner. Walks inbound frames,
// decodes the first ACP2 error reply, asserts the status code + the
// compliance-event label derived via acp2.EventForErrStatus.
func runACP2(t *testing.T, s *Scenario) {
	t.Helper()
	wirePath, err := s.ResolveWirePath()
	if err != nil {
		t.Fatalf("scenario %q: %v", s.Name, err)
	}
	recs := readCapture(t, wirePath)

	var errMsg *acp2.ACP2Message
	var errStat acp2.ACP2ErrStatus

	for _, r := range recs {
		if r.Direction != "rx" {
			continue
		}
		raw, err := hex.DecodeString(r.Hex)
		if err != nil {
			t.Fatalf("scenario %q: hex decode: %v", s.Name, err)
		}
		frame, err := acp2.ReadAN2Frame(bytes.NewReader(raw))
		if err != nil {
			continue
		}
		if frame.Proto != acp2.AN2ProtoACP2 || frame.Type != acp2.AN2TypeData {
			continue
		}
		msg, err := acp2.DecodeACP2Message(frame.Payload)
		if err != nil {
			continue
		}
		if msg.Type == acp2.ACP2TypeError {
			errMsg = msg
			errStat = acp2.ACP2ErrStatus(msg.Func)
			break
		}
	}

	if len(s.ExpectEvents) > 0 || s.ExpectErrorClass != "" || s.ExpectErrorStatus != nil {
		if errMsg == nil {
			t.Fatalf("scenario %q: expected an ACP2 error reply, none found in %s",
				s.Name, wirePath)
		}
	}

	// Assert expected compliance events.
	for _, wantEvent := range s.ExpectEvents {
		got := acp2.EventForErrStatus(errStat)
		if got != wantEvent {
			t.Errorf("scenario %q: EventForErrStatus(%d) = %q, want %q",
				s.Name, errStat, got, wantEvent)
		}
	}

	// Assert expected error class + status (optional).
	if s.ExpectErrorClass != "" {
		err := errMsg.ToACP2Error()
		typeName := fmt.Sprintf("%T", err)
		// Accept either the fully-qualified type "*acp2.ACP2Error" or
		// the short form "ACP2Error" for ergonomic scenario files.
		short := typeName[strings.LastIndex(typeName, ".")+1:]
		short = strings.TrimPrefix(short, "*")
		if s.ExpectErrorClass != short && s.ExpectErrorClass != typeName {
			t.Errorf("scenario %q: error class got %q, want %q",
				s.Name, short, s.ExpectErrorClass)
		}
	}
	if s.ExpectErrorStatus != nil {
		want := acp2.ACP2ErrStatus(*s.ExpectErrorStatus)
		if errStat != want {
			t.Errorf("scenario %q: error status got %d, want %d",
				s.Name, errStat, want)
		}
	}
}
