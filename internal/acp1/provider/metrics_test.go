package acp1

import (
	"io"
	"log/slog"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/plugin"
)

func metricsServer(t *testing.T) *server {
	t.Helper()
	return newServer(plugin.Deps{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, nil)
}

// The provider exposed no metrics at all, so `producer acp1 serve
// --metrics-addr` mounted an endpoint with no acp1 series in it — the CLI
// warns "provider does not expose Metrics() — skipping" and serves nothing.
func TestServerExposesMetrics(t *testing.T) {
	if metricsServer(t).Metrics() == nil {
		t.Fatal("Metrics must be non-nil — WithDefaults always fills it")
	}
}

// Frames are attributed by ACP1 method, the protocol's own command axis and
// the one a decoded message hands us for free.
func TestNewServerRegistersEveryMethod(t *testing.T) {
	names := metricsServer(t).Metrics().Snapshot().CmdNames
	for _, m := range []codec.Method{
		codec.MethodGetValue, codec.MethodSetValue, codec.MethodSetIncValue,
		codec.MethodSetDecValue, codec.MethodSetDefValue, codec.MethodGetObject,
	} {
		if got := names[uint8(m)]; got != methodName(m) {
			t.Errorf("method %d registered as %q, want %q", m, got, methodName(m))
		}
	}
}

// A request that decodes is counted against its own method, and the reply
// against the same one — a reply carries the request's MCode.
func TestHandleDatagramCountsBothDirections(t *testing.T) {
	s := metricsServer(t)

	req := &codec.Message{
		MTID: 7, PVER: 1, MType: codec.MTypeRequest,
		MCode: uint8(codec.MethodGetObject), ObjGroup: codec.GroupRoot,
	}
	raw, err := req.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var sent int
	s.handleDatagram2(raw, "10.0.0.9:2071", func(b []byte) error {
		sent = len(b)
		return nil
	})
	if sent == 0 {
		t.Fatal("the provider sent no reply, so there is nothing to count")
	}

	snap := s.Metrics().Snapshot()
	get := uint8(codec.MethodGetObject)
	if snap.RxHitsByCmd[get] != 1 || snap.RxBytesByCmd[get] != uint64(len(raw)) {
		t.Errorf("rx for getObject = %d hits / %d bytes, want 1 / %d",
			snap.RxHitsByCmd[get], snap.RxBytesByCmd[get], len(raw))
	}
	if snap.TxHitsByCmd[get] != 1 || snap.TxBytesByCmd[get] != uint64(sent) {
		t.Errorf("tx for getObject = %d hits / %d bytes, want 1 / %d",
			snap.TxHitsByCmd[get], snap.TxBytesByCmd[get], sent)
	}
}

// Bytes that do not decode still arrived. They are counted as an aggregate,
// since there is no method to attribute them to, and the decode error is
// counted beside them — silence here would make a peer talking nonsense look
// identical to a peer that has gone away.
func TestHandleDatagramCountsUndecodableBytes(t *testing.T) {
	s := metricsServer(t)

	s.handleDatagram2([]byte{0xFF}, "10.0.0.9:2071", func([]byte) error {
		t.Error("nothing should be sent in reply to garbage")
		return nil
	})

	snap := s.Metrics().Snapshot()
	if snap.RxFrames != 1 || snap.RxBytes != 1 {
		t.Errorf("rx = %d frames / %d bytes, want 1 / 1", snap.RxFrames, snap.RxBytes)
	}
	if snap.DecodeErrors != 1 {
		t.Errorf("DecodeErrors = %d, want 1", snap.DecodeErrors)
	}
}
