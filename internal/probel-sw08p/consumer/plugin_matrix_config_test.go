package probelsw08p

import (
	"io"
	"log/slog"
	"testing"
)

// TestSetMatrixConfigRoundTrip pins the SW-P-08 consumer's matrix-
// config getter/setter — same shape as sw02p so users carry one
// mental model across both Probel protocols.
func TestSetMatrixConfigRoundTrip(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	f := &Factory{}
	p := f.New(logger).(*Plugin)

	if got := p.MatrixConfig(); got != (MatrixConfig{}) {
		t.Errorf("default MatrixConfig = %+v; want zero value", got)
	}

	want := MatrixConfig{MatrixID: 3, Level: 7, Dsts: 1024, Srcs: 768}
	p.SetMatrixConfig(want)
	if got := p.MatrixConfig(); got != want {
		t.Errorf("MatrixConfig() = %+v; want %+v", got, want)
	}
}
