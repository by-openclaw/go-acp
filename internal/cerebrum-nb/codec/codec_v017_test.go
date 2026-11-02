package codec

import (
	"strings"
	"testing"
)

// Tests in this file pin the EVS Cerebrum Northbound API v0.17 additions
// over the 0v16 baseline. Every expected attribute sequence is taken FROM
// THE v0.17 PDF worked examples (section cites inline), not from the
// codec's own output.
//
// Spec: internal/cerebrum-nb/assets/Cerebrum Northbound API v0.17.pdf

// §4.2.3 p16: <ROUTING TYPE="RM_SRCE_ADD" COUNT="4" VIRTUAL="0" FEDERATION="0"/>
func TestEncodeRMAdd_v017(t *testing.T) {
	out := string(EncodeAction(7, &RoutingAction{Type: "RM_SRCE_ADD", Count: "4", Virtual: "0", Federation: "0"}))
	want := `TYPE="RM_SRCE_ADD" COUNT="4" VIRTUAL="0" FEDERATION="0"`
	if !strings.Contains(out, want) {
		t.Fatalf("RM_SRCE_ADD: got %s want it to carry %s", out, want)
	}
	out = string(EncodeAction(8, &RoutingAction{Type: "RM_DEST_ADD", Count: "1", Virtual: "1", Federation: "1"}))
	if !strings.Contains(out, `TYPE="RM_DEST_ADD" COUNT="1" VIRTUAL="1" FEDERATION="1"`) {
		t.Fatalf("RM_DEST_ADD: got %s", out)
	}
}

// §4.2.4 p17: <ROUTING TYPE="RM_SRCE_DELETE" SRCE_ID="21,22,23" FEDERATION="0"/>
func TestEncodeRMDelete_v017(t *testing.T) {
	out := string(EncodeAction(9, &RoutingAction{Type: "RM_SRCE_DELETE", SrceID: "21,22,23", Federation: "0"}))
	if !strings.Contains(out, `TYPE="RM_SRCE_DELETE" SRCE_ID="21,22,23" FEDERATION="0"`) {
		t.Fatalf("RM_SRCE_DELETE: got %s", out)
	}
	out = string(EncodeAction(10, &RoutingAction{Type: "RM_DEST_DELETE", DestID: "5", Federation: "1"}))
	if !strings.Contains(out, `TYPE="RM_DEST_DELETE" DEST_ID="5" FEDERATION="1"`) {
		t.Fatalf("RM_DEST_DELETE: got %s", out)
	}
}

// §4.1.7 p14, federation example: <ROUTING TYPE="SRCE_ASSOC" FEDERATION="1"
// LOGICAL_SRCE_ID="199" LOGICAL_LEVEL_ID="2" TARGET_DEVICE_NAME="Hybrid Router"
// TARGET_DEVICE_TYPE="ROUTER" TARGET_LEVEL_ID="1" TARGET_SRCE_ID="100"/>.
// Attribute order is not significant in XML; the test checks every
// attribute of the example is present, and that a local association
// (pre-v0.17 form) carries no FEDERATION at all.
func TestEncodeAssocFederation_v017(t *testing.T) {
	a := RoutingAction{Type: "SRCE_ASSOC", LogicalSrceID: "199", LogicalLevelID: "2", TargetDeviceName: "Hybrid Router",
		TargetDeviceType: "ROUTER", TargetLevelID: "1", TargetSrceID: "100", Federation: "1"}
	out := string(EncodeAction(11, &a))
	for _, attr := range []string{`TYPE="SRCE_ASSOC"`, `FEDERATION="1"`, `LOGICAL_SRCE_ID="199"`, `LOGICAL_LEVEL_ID="2"`,
		`TARGET_DEVICE_NAME="Hybrid Router"`, `TARGET_DEVICE_TYPE="ROUTER"`, `TARGET_LEVEL_ID="1"`, `TARGET_SRCE_ID="100"`} {
		if !strings.Contains(out, attr) {
			t.Errorf("federation SRCE_ASSOC: missing %s in %s", attr, out)
		}
	}
	a.Federation = ""
	if local := string(EncodeAction(12, &a)); strings.Contains(local, "FEDERATION") {
		t.Fatalf("local SRCE_ASSOC must not carry FEDERATION: %s", local)
	}
	// §4.2.1 p15: RM_SRCE_TAGS with FEDERATION
	tags := string(EncodeAction(13, &RoutingAction{Type: "RM_SRCE_TAGS", SrceID: "1", LevelID: "1", Tags: "Tag1", Federation: "1"}))
	if !strings.Contains(tags, `TYPE="RM_SRCE_TAGS" SRCE_ID="1"`) || !strings.Contains(tags, `TAGS="Tag1" FEDERATION="1"`) {
		t.Fatalf("RM_SRCE_TAGS federation: %s", tags)
	}
}

// §4.2.5 p17: <ROUTING TYPE="RM_SRCE_ADD" FEDERATION="0"><RESULT VALUE="ACCEPTED"/></ROUTING>
func TestDecodeRoutingResult_v017(t *testing.T) {
	f, err := Decode([]byte(`<ROUTING MTID="7" TYPE="RM_SRCE_ADD" FEDERATION="0"><RESULT VALUE="ACCEPTED"/></ROUTING>`))
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind != KindRoutingResult || f.Kind.String() != "routing_result" {
		t.Fatalf("kind: got %s want routing_result", f.Kind)
	}
	if f.MTID != "7" || f.RoutingResult == nil || f.RoutingResult.Type != "RM_SRCE_ADD" || f.RoutingResult.Federation != "0" ||
		f.RoutingResult.Value != "ACCEPTED" || !f.RoutingResult.Accepted {
		t.Fatalf("result: %+v (mtid %q)", f.RoutingResult, f.MTID)
	}
	// FAILED, and the spec's example without an MTID
	f, err = Decode([]byte(`<ROUTING TYPE="RM_DEST_DELETE" FEDERATION="1"><RESULT VALUE="FAILED"/></ROUTING>`))
	if err != nil {
		t.Fatal(err)
	}
	if f.MTID != "" || f.RoutingResult.Value != "FAILED" || f.RoutingResult.Accepted || f.RoutingResult.Federation != "1" {
		t.Fatalf("failed result: %+v", f.RoutingResult)
	}
	// No <RESULT> child at all: not accepted, value empty.
	f, err = Decode([]byte(`<ROUTING TYPE="RM_SRCE_ADD"/>`))
	if err != nil {
		t.Fatal(err)
	}
	if f.RoutingResult.Value != "" || f.RoutingResult.Accepted {
		t.Fatalf("no result child: %+v", f.RoutingResult)
	}
}
