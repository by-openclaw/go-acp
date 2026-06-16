package codec

import (
	"bytes"
	"errors"
	"testing"
)

// TestDecodersRejectWrongID exercises the ErrWrongCommand guard on
// every per-command decoder that the coverage report flagged as
// missing its wrong-ID arm. Each decoder is handed a frame whose ID is
// some OTHER command byte (the request/response sibling) so the very
// first guard fires. The payload is sized correctly so only the ID
// mismatch — not a length check — is the rejection cause.
func TestDecodersRejectWrongID(t *testing.T) {
	// wrongID is any command byte different from the one the decoder
	// expects; pad is a payload long enough to clear the length guard
	// (so the ID guard is unambiguously the one firing).
	cases := []struct {
		name    string
		wrongID CommandID
		pad     int
		decode  func(Frame) error
	}{
		{"DecodeGo", TxGoDoneAck, 1, func(f Frame) error { _, e := DecodeGo(f); return e }},
		{"DecodeGoGroupSalvo", RxGo, 2, func(f Frame) error { _, e := DecodeGoGroupSalvo(f); return e }},
		{"DecodeConnectOnGoGroupSalvo", RxConnectOnGo, 4, func(f Frame) error { _, e := DecodeConnectOnGoGroupSalvo(f); return e }},
		{"DecodeExtendedConnectOnGoGroupSalvo", RxConnectOnGoGroupSalvo, 5, func(f Frame) error { _, e := DecodeExtendedConnectOnGoGroupSalvo(f); return e }},
		{"DecodeConnected", TxTally, 3, func(f Frame) error { _, e := DecodeConnected(f); return e }},
		{"DecodeGoDoneAck", RxGo, 1, func(f Frame) error { _, e := DecodeGoDoneAck(f); return e }},
		{"DecodeConnectOnGoGroupSalvoAck", RxConnectOnGoGroupSalvo, 4, func(f Frame) error { _, e := DecodeConnectOnGoGroupSalvoAck(f); return e }},
		{"DecodeGoDoneGroupSalvoAck", RxGoGroupSalvo, 2, func(f Frame) error { _, e := DecodeGoDoneGroupSalvoAck(f); return e }},
		{"DecodeExtendedConnectOnGoGroupSalvoAck", RxExtendedConnectOnGoGroupSalvo, 5, func(f Frame) error { _, e := DecodeExtendedConnectOnGoGroupSalvoAck(f); return e }},
		{"DecodeExtendedConnected", RxExtendedConnect, 5, func(f Frame) error { _, e := DecodeExtendedConnected(f); return e }},
		{"DecodeExtendedProtectConnected", TxExtendedProtectDisconnected, 5, func(f Frame) error { _, e := DecodeExtendedProtectConnected(f); return e }},
		{"DecodeExtendedProtectDisconnected", TxExtendedProtectConnected, 5, func(f Frame) error { _, e := DecodeExtendedProtectDisconnected(f); return e }},
		{"DecodeSourceLockStatusResponse", TxRouterConfigResponse1, 4, func(f Frame) error { _, e := DecodeSourceLockStatusResponse(f); return e }},
		{"DecodeRouterConfigResponse1", TxSourceLockStatusResponse, 4, func(f Frame) error { _, e := DecodeRouterConfigResponse1(f); return e }},
		{"DecodeRouterConfigResponse2", TxRouterConfigResponse1, 4, func(f Frame) error { _, e := DecodeRouterConfigResponse2(f); return e }},
		{"DecodeExtendedProtectTallyDump", TxRouterConfigResponse2, 1, func(f Frame) error { _, e := DecodeExtendedProtectTallyDump(f); return e }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := Frame{ID: tc.wrongID, Payload: make([]byte, tc.pad)}
			if err := tc.decode(f); !errors.Is(err, ErrWrongCommand) {
				t.Errorf("%s wrong-ID: got %v; want ErrWrongCommand", tc.name, err)
			}
		})
	}
}

// TestDecodersRejectShortPayload exercises the ErrShortPayload guard on
// every fixed-length decoder flagged by the coverage report. Each
// decoder is handed a correctly-typed frame whose payload is one byte
// short of the spec'd minimum, so the length guard — not the ID guard —
// is the rejection cause.
func TestDecodersRejectShortPayload(t *testing.T) {
	cases := []struct {
		name    string
		id      CommandID
		shortTo int // payload length one below the required minimum
		decode  func(Frame) error
	}{
		{"DecodeGo", RxGo, PayloadLenGo - 1, func(f Frame) error { _, e := DecodeGo(f); return e }},
		{"DecodeGoGroupSalvo", RxGoGroupSalvo, PayloadLenGoGroupSalvo - 1, func(f Frame) error { _, e := DecodeGoGroupSalvo(f); return e }},
		{"DecodeConnectOnGoGroupSalvo", RxConnectOnGoGroupSalvo, PayloadLenConnectOnGoGroupSalvo - 1, func(f Frame) error { _, e := DecodeConnectOnGoGroupSalvo(f); return e }},
		{"DecodeExtendedConnectOnGoGroupSalvo", RxExtendedConnectOnGoGroupSalvo, PayloadLenExtendedConnectOnGoGroupSalvo - 1, func(f Frame) error { _, e := DecodeExtendedConnectOnGoGroupSalvo(f); return e }},
		{"DecodeConnected", TxCrosspointConnected, PayloadLenConnected - 1, func(f Frame) error { _, e := DecodeConnected(f); return e }},
		{"DecodeConnectOnGoAck", TxConnectOnGoAck, PayloadLenConnectOnGoAck - 1, func(f Frame) error { _, e := DecodeConnectOnGoAck(f); return e }},
		{"DecodeGoDoneAck", TxGoDoneAck, PayloadLenGoDoneAck - 1, func(f Frame) error { _, e := DecodeGoDoneAck(f); return e }},
		{"DecodeConnectOnGoGroupSalvoAck", TxConnectOnGoGroupSalvoAck, PayloadLenConnectOnGoGroupSalvoAck - 1, func(f Frame) error { _, e := DecodeConnectOnGoGroupSalvoAck(f); return e }},
		{"DecodeGoDoneGroupSalvoAck", TxGoDoneGroupSalvoAck, PayloadLenGoDoneGroupSalvoAck - 1, func(f Frame) error { _, e := DecodeGoDoneGroupSalvoAck(f); return e }},
		{"DecodeExtendedConnectOnGoGroupSalvoAck", TxExtendedConnectOnGoGroupSalvoAck, PayloadLenExtendedConnectOnGoGroupSalvoAck - 1, func(f Frame) error { _, e := DecodeExtendedConnectOnGoGroupSalvoAck(f); return e }},
		{"DecodeExtendedConnected", TxExtendedConnected, PayloadLenExtendedConnected - 1, func(f Frame) error { _, e := DecodeExtendedConnected(f); return e }},
		{"DecodeExtendedProtectConnected", TxExtendedProtectConnected, PayloadLenExtendedProtectConnected - 1, func(f Frame) error { _, e := DecodeExtendedProtectConnected(f); return e }},
		{"DecodeExtendedProtectDisconnected", TxExtendedProtectDisconnected, PayloadLenExtendedProtectDisconnected - 1, func(f Frame) error { _, e := DecodeExtendedProtectDisconnected(f); return e }},
		// Variable-length: short header (< 2 / < 4 bytes) trips the
		// minimum-length guard before any size math.
		{"DecodeSourceLockStatusResponse", TxSourceLockStatusResponse, 1, func(f Frame) error { _, e := DecodeSourceLockStatusResponse(f); return e }},
		{"DecodeRouterConfigResponse1", TxRouterConfigResponse1, RouterConfigLevelMapBytes - 1, func(f Frame) error { _, e := DecodeRouterConfigResponse1(f); return e }},
		{"DecodeRouterConfigResponse2", TxRouterConfigResponse2, RouterConfigLevelMapBytes - 1, func(f Frame) error { _, e := DecodeRouterConfigResponse2(f); return e }},
		{"DecodeExtendedProtectTallyDump", TxExtendedProtectTallyDump, 0, func(f Frame) error { _, e := DecodeExtendedProtectTallyDump(f); return e }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := Frame{ID: tc.id, Payload: make([]byte, tc.shortTo)}
			if err := tc.decode(f); !errors.Is(err, ErrShortPayload) {
				t.Errorf("%s short: got %v; want ErrShortPayload", tc.name, err)
			}
		})
	}
}

// TestEncodersBadSourceFlag pins the Multiplier bit-3 "bad source" arm
// of the narrow §3.2.3-layout encoders (tx 03 TALLY, tx 04 CONNECTED,
// rx 35 CONNECT ON GO GROUP SALVO). §3.2.3: bit 3 of the Multiplier is
// the bad-source flag; setting BadSource must set 0x08 and survive a
// round-trip.
func TestEncodersBadSourceFlag(t *testing.T) {
	t.Run("tally", func(t *testing.T) {
		f := EncodeTally(TallyParams{Destination: 1, Source: 2, BadSource: true})
		// Multiplier = 0x08, Dst=0x01, Src=0x02.
		want := []byte{0x08, 0x01, 0x02}
		if !bytes.Equal(f.Payload, want) {
			t.Fatalf("payload = %X; want %X", f.Payload, want)
		}
		got, err := DecodeTally(f)
		if err != nil {
			t.Fatalf("DecodeTally: %v", err)
		}
		if !got.BadSource || got.Destination != 1 || got.Source != 2 {
			t.Errorf("decoded = %+v; want dst=1 src=2 badSource=true", got)
		}
	})

	t.Run("connected", func(t *testing.T) {
		f := EncodeConnected(ConnectedParams{Destination: 1, Source: 2, BadSource: true})
		want := []byte{0x08, 0x01, 0x02}
		if !bytes.Equal(f.Payload, want) {
			t.Fatalf("payload = %X; want %X", f.Payload, want)
		}
		got, err := DecodeConnected(f)
		if err != nil {
			t.Fatalf("DecodeConnected: %v", err)
		}
		if !got.BadSource {
			t.Errorf("decoded BadSource=false; want true (%+v)", got)
		}
	})

	t.Run("connect_on_go_group_salvo", func(t *testing.T) {
		f := EncodeConnectOnGoGroupSalvo(ConnectOnGoGroupSalvoParams{
			Destination: 1, Source: 2, SalvoID: 5, BadSource: true,
		})
		// Multiplier=0x08 Dst=0x01 Src=0x02 Salvo=0x05.
		want := []byte{0x08, 0x01, 0x02, 0x05}
		if !bytes.Equal(f.Payload, want) {
			t.Fatalf("payload = %X; want %X", f.Payload, want)
		}
		got, err := DecodeConnectOnGoGroupSalvo(f)
		if err != nil {
			t.Fatalf("DecodeConnectOnGoGroupSalvo: %v", err)
		}
		if !got.BadSource || got.SalvoID != 5 {
			t.Errorf("decoded = %+v; want salvo=5 badSource=true", got)
		}
	})
}

// TestEncodeExtendedConnectedStatusBits pins the tx 68 Status byte arms
// (§3.2.50): bit 0 = crosspoint-update-disabled, bit 1 = bad-source.
// EncodeExtendedConnected sets these from UpdateOff / BadSource — the
// flagged-true paths were uncovered.
func TestEncodeExtendedConnectedStatusBits(t *testing.T) {
	f := EncodeExtendedConnected(ExtendedConnectedParams{
		Destination: 5000, Source: 10000, UpdateOff: true, BadSource: true,
	})
	// dst=5000 → DIV128=39 (0x27) MOD128=8; src=10000 → DIV128=78 (0x4E) MOD128=16.
	// Status = bit0|bit1 = 0x03.
	want := []byte{0x27, 0x08, 0x4E, 0x10, 0x03}
	if !bytes.Equal(f.Payload, want) {
		t.Fatalf("payload = %X; want %X", f.Payload, want)
	}
	got, err := DecodeExtendedConnected(f)
	if err != nil {
		t.Fatalf("DecodeExtendedConnected: %v", err)
	}
	if !got.UpdateOff || !got.BadSource || got.Destination != 5000 || got.Source != 10000 {
		t.Errorf("decoded = %+v; want dst=5000 src=10000 updateOff+badSource", got)
	}
}
