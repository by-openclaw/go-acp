// Package acp2_test — offline compliance-event tests.
//
// These tests exercise the error-to-event mapping without a live
// session: they load the real wire captures the CLI produced on the
// VM (tests/fixtures/acp2/err_no_access.jsonl, err_invalid_obj.jsonl),
// find the ACP2 error reply frame in each, and assert that the pure
// helper EventForErrStatus maps the status code to the expected
// compliance event label.
//
// Replay inputs are the unmodified CLI --capture output from
//   acp set 10.41.40.195 --protocol acp2 --slot 1 --id 2 --value "X"
//   acp get 10.41.40.195 --protocol acp2 --slot 1 --id 999999
// on the production VM. Re-capturing with newer firmware and
// dropping the file in place re-validates the mapping; no test code
// changes required.
package acp2_test

import (
	"bytes"
	"encoding/hex"
	"testing"

	"acp/internal/protocol/acp2"
)

// findACP2Error walks every frame in the capture and returns the
// first decoded ACP2Message whose Type is Error, plus the error
// status extracted from Func (spec p.5). t.Fatal if none found.
func findACP2Error(t *testing.T, captureName string) (*acp2.ACP2Message, acp2.ACP2ErrStatus) {
	t.Helper()
	recs := loadCapture(t, captureName)
	for _, r := range recs {
		if r.Direction != "rx" {
			continue
		}
		raw, err := hex.DecodeString(r.Hex)
		if err != nil {
			t.Fatalf("hex decode: %v", err)
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
			return msg, acp2.ACP2ErrStatus(msg.Func)
		}
	}
	t.Fatalf("no ACP2 error reply in capture %s", captureName)
	return nil, 0
}

// TestCompliance_ErrNoAccess replays the "set on read-only" capture
// and verifies the reply maps to AccessDeniedReceived. Captured on
// 10.41.40.195 slot 1 id 2 (IDENTITY.Card Name, R--).
func TestCompliance_ErrNoAccess(t *testing.T) {
	msg, stat := findACP2Error(t, "err_no_access.jsonl")
	if stat != acp2.ErrNoAccess {
		t.Fatalf("expected ErrNoAccess (%d), got %d", acp2.ErrNoAccess, stat)
	}
	if got := acp2.EventForErrStatus(stat); got != acp2.AccessDeniedReceived {
		t.Errorf("EventForErrStatus(%d) = %q, want %q",
			stat, got, acp2.AccessDeniedReceived)
	}
	err := msg.ToACP2Error()
	ace, ok := err.(*acp2.ACP2Error)
	if !ok {
		t.Fatalf("ToACP2Error type = %T, want *acp2.ACP2Error", err)
	}
	if ace.Status != acp2.ErrNoAccess {
		t.Errorf("ACP2Error.Status = %d, want %d", ace.Status, acp2.ErrNoAccess)
	}
}

// TestCompliance_ErrInvalidObj replays the "get on unknown id"
// capture and verifies the reply maps to InvalidObjectReceived.
// Captured on 10.41.40.195 slot 1 id 999999.
func TestCompliance_ErrInvalidObj(t *testing.T) {
	msg, stat := findACP2Error(t, "err_invalid_obj.jsonl")
	if stat != acp2.ErrInvalidObjID {
		t.Fatalf("expected ErrInvalidObjID (%d), got %d", acp2.ErrInvalidObjID, stat)
	}
	if got := acp2.EventForErrStatus(stat); got != acp2.InvalidObjectReceived {
		t.Errorf("EventForErrStatus(%d) = %q, want %q",
			stat, got, acp2.InvalidObjectReceived)
	}
	err := msg.ToACP2Error()
	ace, ok := err.(*acp2.ACP2Error)
	if !ok {
		t.Fatalf("ToACP2Error type = %T, want *acp2.ACP2Error", err)
	}
	if ace.Status != acp2.ErrInvalidObjID {
		t.Errorf("ACP2Error.Status = %d, want %d", ace.Status, acp2.ErrInvalidObjID)
	}
}

// TestEventForErrStatus_All verifies the full mapping table. Catches
// any future spec-code addition that forgets to update the switch.
func TestEventForErrStatus_All(t *testing.T) {
	cases := []struct {
		stat acp2.ACP2ErrStatus
		want string
	}{
		{acp2.ErrProtocol, acp2.ProtocolErrorReceived},
		{acp2.ErrInvalidObjID, acp2.InvalidObjectReceived},
		{acp2.ErrInvalidIdx, acp2.InvalidIndexReceived},
		{acp2.ErrInvalidPID, acp2.InvalidPidReceived},
		{acp2.ErrNoAccess, acp2.AccessDeniedReceived},
		{acp2.ErrInvalidValue, acp2.InvalidValueReceived},
	}
	for _, c := range cases {
		if got := acp2.EventForErrStatus(c.stat); got != c.want {
			t.Errorf("EventForErrStatus(%d) = %q, want %q", c.stat, got, c.want)
		}
	}
	// Out-of-range code → no event (session drops through).
	if got := acp2.EventForErrStatus(acp2.ACP2ErrStatus(99)); got != "" {
		t.Errorf("EventForErrStatus(99) = %q, want empty", got)
	}
}
