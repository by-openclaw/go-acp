package consumer

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"

	dhsc "dhs/internal/consumer"
	"dhs/internal/plugin"
	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/mib"
	"dhs/internal/snmp/provider"
	"dhs/internal/wiretrace"
)

// A capture of a real exchange — taken with the session's own tap, the
// way --capture takes it — validates offline: every datagram decodes, the
// directions are counted, and what the agent refused is named.
func TestValidateReadsBackACapturedSession(t *testing.T) {
	addr := agentUnder(t, provider.Communities{Read: "public", Write: "public"})
	var mu sync.Mutex
	var trames []wiretrace.Trame
	s := dial(t, addr, Options{
		Version: codec.Version2c, Community: "public",
		Tap: func(dir string, datagram []byte) {
			mu.Lock()
			defer mu.Unlock()
			trames = append(trames, wiretrace.Trame{Direction: wiretrace.Direction(dir), Hex: hex.EncodeToString(datagram)})
		},
	})
	if _, err := s.Get(context.Background(), mib.SysDescr); err != nil {
		t.Fatal(err)
	}
	// A write the agent refuses: the refusal must show in the report.
	_, _ = s.Set(context.Background(), codec.VarBind{Name: mib.SysDescr, Value: codec.Value{Type: codec.TypeOctetString, Bytes: []byte("x")}})

	p := (&Factory{}).New(plugin.Deps{Logger: quiet()}).(*Plugin)
	mu.Lock()
	captured := append([]wiretrace.Trame(nil), trames...)
	mu.Unlock()
	rep, err := p.Validate(context.Background(), captured, dhsc.ValidateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.TramesProcessed != len(captured) || len(rep.Errors) != 0 {
		t.Fatalf("%d of %d datagrams decoded, errors %v", rep.TramesProcessed, len(captured), rep.Errors)
	}
	if rep.PerDirection["tx"] == 0 || rep.PerDirection["tx"] != rep.PerDirection["rx"] {
		t.Errorf("directions %v, want as many answers as requests", rep.PerDirection)
	}
	refused := false
	for _, inv := range rep.Invariants {
		if strings.Contains(inv, "error-status") {
			refused = true
		}
		if strings.Contains(inv, "no request in the capture") {
			t.Errorf("a matched response was called unsolicited: %s", inv)
		}
	}
	if !refused {
		t.Errorf("the refused write is not in the invariants: %v", rep.Invariants)
	}

	// The answers alone: each is a response nothing in the capture asked for.
	var answers []wiretrace.Trame
	for _, tr := range captured {
		if tr.Direction == "rx" {
			answers = append(answers, tr)
		}
	}
	rep, err = p.Validate(context.Background(), answers, dhsc.ValidateOpts{})
	if err != nil || len(rep.Invariants) < len(answers) {
		t.Errorf("answers without their requests: err %v, invariants %v", err, rep.Invariants)
	}
}

// What cannot be decoded is reported by index, not skipped; a marker stops
// the run; a cancelled context ends it; and the two outputs a capture
// cannot give are refused by name.
func TestValidateReportsWhatItCannotRead(t *testing.T) {
	p := (&Factory{}).New(plugin.Deps{Logger: quiet()}).(*Plugin)
	get, err := codec.Encode(codec.Message{Version: codec.Version2c, Community: "public",
		PDU: &codec.PDU{Type: codec.PDUTypeGet, RequestID: 7, VarBinds: []codec.VarBind{{Name: mib.SysDescr}}}})
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("ff", 40)
	trames := []wiretrace.Trame{
		{Direction: "tx", Hex: "zz"},
		{Direction: "rx", Hex: long},
		{Direction: "tx", Hex: hex.EncodeToString(get), Note: "stop-here"},
		{Direction: "tx", Hex: hex.EncodeToString(get)},
	}
	rep, err := p.Validate(context.Background(), trames, dhsc.ValidateOpts{StopAt: "stop-here"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Errors) != 2 || rep.Errors[0].TrameIndex != 0 || rep.Errors[1].TrameIndex != 1 {
		t.Fatalf("errors %+v, want trames 0 and 1", rep.Errors)
	}
	if len(rep.Errors[1].HexPrefix) != 32 {
		t.Errorf("hex prefix %q, want the first sixteen bytes", rep.Errors[1].HexPrefix)
	}
	if rep.TramesProcessed != 1 || rep.StoppedAt != "stop-here" {
		t.Errorf("processed %d, stopped at %q; want 1 and the marker", rep.TramesProcessed, rep.StoppedAt)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Validate(ctx, trames, dhsc.ValidateOpts{}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
	for _, o := range []dhsc.ValidateOpts{{OutTree: "t.json"}, {OutParams: "p.csv"}} {
		if _, err := p.Validate(context.Background(), trames, o); !errors.Is(err, ErrValidateOutput) {
			t.Errorf("%+v: %v, want ErrValidateOutput", o, err)
		}
	}
}

// A datagram that decodes to a message with no readable PDU — v3 under
// privacy — is reported as that, not counted as read.
func TestASealedPDUIsReportedNotCounted(t *testing.T) {
	if err := unreadable(codec.Message{Version: codec.Version3}, nil); err == nil || !strings.Contains(err.Error(), "sealed") {
		t.Errorf("a message with no PDU: %v", err)
	}
	boom := errors.New("malformed")
	if err := unreadable(codec.Message{}, boom); !errors.Is(err, boom) {
		t.Errorf("a decode error is passed on: %v", err)
	}
	if err := unreadable(codec.Message{PDU: &codec.PDU{}}, nil); err != nil {
		t.Errorf("a readable message: %v", err)
	}
}
