//go:build integration

package probelsw02p_integration

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"dhs/internal/export"
)

// matrixTreeFixture is the committed canonical JSON export of the matrix
// tree the loopback provider serves. It is produced from servedExport()
// via the repo's own canonical-JSON writer so the on-disk fixture can
// never drift from what the emulator actually serves.
const matrixTreeFixture = "../testdata/exports/matrix_tree.json"

// TestMatrixTreeExportFixture asserts the committed canonical JSON export
// is byte-identical to what export.WriteCanonicalJSON emits for the
// served matrix tree. Run with DHS_REGEN_FIXTURES=1 to (re)write the
// fixture after an intentional tree change:
//
//	DHS_REGEN_FIXTURES=1 go test -tags integration \
//	  ./internal/probel-sw02p/integration/ -run TestMatrixTreeExportFixture
func TestMatrixTreeExportFixture(t *testing.T) {
	var buf bytes.Buffer
	if err := export.WriteCanonicalJSON(context.Background(), &buf, servedExport()); err != nil {
		t.Fatalf("WriteCanonicalJSON: %v", err)
	}
	want := buf.Bytes()

	if os.Getenv("DHS_REGEN_FIXTURES") == "1" {
		if err := os.MkdirAll(filepath.Dir(matrixTreeFixture), 0o755); err != nil {
			t.Fatalf("mkdir exports: %v", err)
		}
		if err := os.WriteFile(matrixTreeFixture, want, 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		t.Logf("regenerated %s (%d bytes)", matrixTreeFixture, len(want))
		return
	}

	got, err := os.ReadFile(matrixTreeFixture)
	if err != nil {
		t.Fatalf("read committed fixture %s: %v "+
			"(regenerate with DHS_REGEN_FIXTURES=1)", matrixTreeFixture, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("committed %s is stale — does not match the served matrix "+
			"tree. Regenerate with DHS_REGEN_FIXTURES=1.", matrixTreeFixture)
	}
}
