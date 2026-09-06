package cerebrumnb

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/wiretrace"
)

func hexDoc(doc string) string { return hex.EncodeToString([]byte(doc)) }

func trame(dir wiretrace.Direction, doc string) wiretrace.Trame {
	return wiretrace.Trame{Direction: dir, Hex: hexDoc(doc)}
}

func TestCerebrumValidate_CountsInvariantsAndTree(t *testing.T) {
	dir := t.TempDir()
	tree := filepath.Join(dir, "tree.json")
	params := filepath.Join(dir, "params.csv")

	valueDoc := `<DEVICE_CHANGE TYPE="VALUE" DEVICE_NAME="cvt 1" SUB_DEVICE="1" OBJECT="A.Delay">` +
		`<OBJECT_VALUE OBJECT="A.Delay" VALUE="5.5" AVAILABLE="1" DATA_TYPE="FLOAT" READABLE="1" WRITABLE="1" UNITS="ms" ENUM_LIST=""/>` +
		`</DEVICE_CHANGE>`
	valueDoc2 := `<DEVICE_CHANGE TYPE="VALUE" DEVICE_NAME="cvt 1" SUB_DEVICE="1" OBJECT="A.Delay">` +
		`<OBJECT_VALUE OBJECT="A.Delay" VALUE="7.5" AVAILABLE="1" DATA_TYPE="FLOAT" READABLE="1" WRITABLE="1"/>` +
		`</DEVICE_CHANGE>`
	enumDoc := `<DEVICE_CHANGE TYPE="VALUE" DEVICE_NAME="cvt 1" SUB_DEVICE="1" OBJECT="A.Mode">` +
		`<OBJECT_VALUE OBJECT="A.Mode" VALUE="1" AVAILABLE="1" DATA_TYPE="ENUM" READABLE="1" WRITABLE="0" ENUM_LIST="On,Off"/>` +
		`</DEVICE_CHANGE>`
	emptyObjDoc := `<DEVICE_CHANGE TYPE="VALUE" DEVICE_NAME="cvt 1" SUB_DEVICE="1">` +
		`<OBJECT_VALUE OBJECT="" VALUE="x" AVAILABLE="1" DATA_TYPE="STRING"/>` +
		`</DEVICE_CHANGE>`

	trames := []wiretrace.Trame{
		trame(wiretrace.DirectionTx, `<POLL MTID="1"/>`),
		trame(wiretrace.DirectionRx, `<poll_reply mtid="1"/>`), // lowercase → case-normalized invariant
		trame(wiretrace.DirectionRx, `<NACK MTID="2" ERROR="NOT_LOGGED_IN" ERROR_CODE="6"/>`),
		trame(wiretrace.DirectionRx, valueDoc),
		trame(wiretrace.DirectionRx, valueDoc2), // same path — last value wins, ID stable
		trame(wiretrace.DirectionRx, enumDoc),
		trame(wiretrace.DirectionRx, emptyObjDoc), // empty OBJECT — skipped from the tree
		trame(wiretrace.DirectionRx, `<DEVICE_CHANGE TYPE="DETAILS" DEVICE_NAME="cvt 1"/>`),
	}

	p := NewPlugin(nil)
	rep, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{
		OutTree: tree, OutParams: params,
	})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if rep.TramesProcessed != len(trames) {
		t.Fatalf("processed = %d want %d", rep.TramesProcessed, len(trames))
	}
	if rep.PerDirection[wiretrace.DirectionTx] != 1 || rep.PerDirection[wiretrace.DirectionRx] != len(trames)-1 {
		t.Fatalf("per-direction = %+v", rep.PerDirection)
	}
	joined := strings.Join(rep.Invariants, "\n")
	if !strings.Contains(joined, "cerebrum_case_normalized") || !strings.Contains(joined, "nack") {
		t.Fatalf("invariants = %v", rep.Invariants)
	}

	data, err := os.ReadFile(tree)
	if err != nil {
		t.Fatalf("out-tree: %v", err)
	}
	s := string(data)
	// The canonical writer nests by path segments: cvt 1 → 1 → A → Delay.
	if !strings.Contains(s, `"cerebrum-nb"`) || !strings.Contains(s, `"cvt 1"`) ||
		!strings.Contains(s, `"Delay"`) || !strings.Contains(s, `"Mode"`) {
		t.Fatalf("tree.json content: %s", s[:minInt(400, len(s))])
	}
	// Last value wins: 7.5, not 5.5.
	if strings.Contains(s, "5.5") || !strings.Contains(s, "7.5") {
		t.Fatalf("last-value-wins violated: %s", s[:minInt(400, len(s))])
	}
	if _, err := os.ReadFile(params); err != nil {
		t.Fatalf("out-params: %v", err)
	}
}

func TestCerebrumValidate_ErrorArmsStopAtCancel(t *testing.T) {
	trames := []wiretrace.Trame{
		{Direction: wiretrace.DirectionRx, Hex: "zz"},                                        // hex error
		trame(wiretrace.DirectionRx, "not xml at all - long enough to clamp the hex prefix"), // decode error, >16 bytes
		{Direction: wiretrace.DirectionRx, Hex: hexDoc(`<ACK MTID="1"/>`), Note: "here"},
		trame(wiretrace.DirectionRx, `<ACK MTID="2"/>`), // never reached with stop-at
	}
	p := NewPlugin(nil)
	rep, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{StopAt: "here"})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(rep.Errors) != 2 || rep.TramesProcessed != 1 || rep.StoppedAt != "here" {
		t.Fatalf("report = %+v", rep)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Validate(ctx, trames, consumer.ValidateOpts{}); err == nil {
		t.Fatal("cancelled ctx must surface")
	}
}

func TestCerebrumValidate_OutputWriteErrors(t *testing.T) {
	trames := []wiretrace.Trame{trame(wiretrace.DirectionRx, `<ACK MTID="1"/>`)}
	bad := filepath.Join(t.TempDir(), "no", "such", "dir", "x.json")
	p := NewPlugin(nil)
	if _, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{OutTree: bad}); err == nil || !strings.Contains(err.Error(), "out-tree") {
		t.Fatalf("want out-tree write error, got %v", err)
	}
	if _, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{OutParams: bad}); err == nil || !strings.Contains(err.Error(), "out-params") {
		t.Fatalf("want out-params write error, got %v", err)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
