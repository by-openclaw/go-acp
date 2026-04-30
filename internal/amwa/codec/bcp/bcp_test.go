package bcp_test

import (
	"testing"

	"acp/internal/amwa/codec/bcp"
	_ "acp/internal/amwa/codec/bcp/bcp00201"
	_ "acp/internal/amwa/codec/bcp/bcp00202"
	_ "acp/internal/amwa/codec/bcp/bcp00401"
	_ "acp/internal/amwa/codec/bcp/bcp00402"
	_ "acp/internal/amwa/codec/bcp/bcp00601"
	_ "acp/internal/amwa/codec/bcp/bcp00604"
	_ "acp/internal/amwa/codec/bcp/bcp00801"
	_ "acp/internal/amwa/codec/bcp/bcp00802"
)

func TestAllValidatorsRegistered(t *testing.T) {
	want := []string{
		"bcp-002-01", "bcp-002-02",
		"bcp-004-01", "bcp-004-02",
		"bcp-006-01", "bcp-006-04",
		"bcp-008-01", "bcp-008-02",
	}
	for _, id := range want {
		if _, ok := bcp.Get(id, "v1.0"); !ok {
			t.Errorf("validator %s/v1.0 not registered", id)
		}
	}
	if got := len(bcp.All()); got != len(want) {
		t.Errorf("expected %d validators, got %d", len(want), got)
	}
}

func TestForKind(t *testing.T) {
	cases := map[bcp.Kind]int{
		bcp.KindSender:    3, // bcp-002-01, bcp-002-02, bcp-004-02
		bcp.KindReceiver:  1, // bcp-004-01
		bcp.KindFlow:      2, // bcp-006-01, bcp-006-04
		bcp.KindMS05Class: 2, // bcp-008-01, bcp-008-02
	}
	for k, want := range cases {
		got := len(bcp.ForKind(k))
		if got != want {
			t.Errorf("ForKind(%q) = %d, want %d", k, got, want)
		}
	}
}

func TestBCP00201ValidGrouphint(t *testing.T) {
	body := []byte(`{"tags":{"urn:x-nmos:tag:grouping/v1.0":["camera1:video","node:camera1:audio:left"]}}`)
	events := bcp.Run(bcp.KindSender, body)
	for _, e := range events {
		if e.SpecID == "bcp-002-01" {
			t.Errorf("unexpected event: %+v", e)
		}
	}
}

func TestBCP00201InvalidGrouphint(t *testing.T) {
	body := []byte(`{"tags":{"urn:x-nmos:tag:grouping/v1.0":["only-one-component"]}}`)
	events := bcp.Run(bcp.KindSender, body)
	found := false
	for _, e := range events {
		if e.SpecID == "bcp-002-01" && e.Code == "bcp_002_01_grouphint_malformed" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected grouphint malformed event")
	}
}

func TestBCP00202ValidAsset(t *testing.T) {
	body := []byte(`{"tags":{"urn:x-nmos:tag:asset/v1.0":["manufacturer=BY-SYSTEMS","product=dhs"]}}`)
	events := bcp.Run(bcp.KindSender, body)
	for _, e := range events {
		if e.SpecID == "bcp-002-02" {
			t.Errorf("unexpected event: %+v", e)
		}
	}
}

func TestBCP00202InvalidAsset(t *testing.T) {
	body := []byte(`{"tags":{"urn:x-nmos:tag:asset/v1.0":["bare-string"]}}`)
	events := bcp.Run(bcp.KindSender, body)
	found := false
	for _, e := range events {
		if e.SpecID == "bcp-002-02" && e.Code == "bcp_002_02_asset_malformed" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected asset malformed event")
	}
}

func TestBCP00401EmptyConstraintSet(t *testing.T) {
	body := []byte(`{"caps":{"constraint_sets":[{}]}}`)
	events := bcp.Run(bcp.KindReceiver, body)
	found := false
	for _, e := range events {
		if e.SpecID == "bcp-004-01" && e.Code == "bcp_004_01_empty_constraint_set" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected empty constraint set event")
	}
}

func TestBCP00601FormatMismatch(t *testing.T) {
	body := []byte(`{"format":"urn:x-nmos:format:audio","media_type":"video/jxsv"}`)
	events := bcp.Run(bcp.KindFlow, body)
	found := false
	for _, e := range events {
		if e.SpecID == "bcp-006-01" && e.Code == "bcp_006_01_format_mismatch" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected JPEG XS format mismatch event")
	}
}

func TestBCP00604FormatMismatch(t *testing.T) {
	body := []byte(`{"format":"urn:x-nmos:format:audio","media_type":"video/MP2T"}`)
	events := bcp.Run(bcp.KindFlow, body)
	found := false
	for _, e := range events {
		if e.SpecID == "bcp-006-04" && e.Code == "bcp_006_04_format_mismatch" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected MPEG TS format mismatch event")
	}
}

func TestBCP00801ClassIDMismatch(t *testing.T) {
	// NcReceiverMonitor name + wrong classId
	body := []byte(`{"classId":[1,2,2,5],"name":"NcReceiverMonitor"}`)
	events := bcp.Run(bcp.KindMS05Class, body)
	found := false
	for _, e := range events {
		if e.SpecID == "bcp-008-01" && e.Code == "bcp_008_01_class_id_mismatch" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected NcReceiverMonitor classId mismatch event")
	}
}

func TestBCP00801ValidNcReceiverMonitor(t *testing.T) {
	body := []byte(`{"classId":[1,2,2,1],"name":"NcReceiverMonitor"}`)
	events := bcp.Run(bcp.KindMS05Class, body)
	for _, e := range events {
		if e.SpecID == "bcp-008-01" && e.Severity == 2 { // SeverityError
			t.Errorf("unexpected error: %+v", e)
		}
	}
}

func TestBCP00802ValidNcSenderMonitor(t *testing.T) {
	body := []byte(`{"classId":[1,2,2,2],"name":"NcSenderMonitor"}`)
	events := bcp.Run(bcp.KindMS05Class, body)
	for _, e := range events {
		if e.SpecID == "bcp-008-02" && e.Severity == 2 {
			t.Errorf("unexpected error: %+v", e)
		}
	}
}
