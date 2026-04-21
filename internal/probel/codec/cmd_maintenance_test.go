package codec

import (
	"bytes"
	"errors"
	"testing"
)

func TestMaintenance_HardReset(t *testing.T) {
	f := EncodeMaintenance(MaintenanceParams{Function: MaintHardReset})
	if f.ID != RxMaintenance {
		t.Errorf("ID got %#x", f.ID)
	}
	if !bytes.Equal(f.Payload, []byte{0x00}) {
		t.Errorf("Payload got %X want 00", f.Payload)
	}
	got, err := DecodeMaintenance(f)
	if err != nil || got.Function != MaintHardReset {
		t.Errorf("round-trip: got %+v err %v", got, err)
	}
}

func TestMaintenance_SoftReset(t *testing.T) {
	f := EncodeMaintenance(MaintenanceParams{Function: MaintSoftReset})
	if !bytes.Equal(f.Payload, []byte{0x01}) {
		t.Errorf("Payload got %X want 01", f.Payload)
	}
	got, _ := DecodeMaintenance(f)
	if got.Function != MaintSoftReset {
		t.Errorf("got %+v", got)
	}
}

func TestMaintenance_ClearProtects(t *testing.T) {
	p := MaintenanceParams{Function: MaintClearProtects, MatrixID: 3, LevelID: 0xFF}
	f := EncodeMaintenance(p)
	want := []byte{0x02, 0x03, 0xFF}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("Payload got %X want %X", f.Payload, want)
	}
	got, err := DecodeMaintenance(f)
	if err != nil || got != p {
		t.Errorf("round-trip: got %+v err %v want %+v", got, err, p)
	}
}

func TestMaintenance_DecodeErrors(t *testing.T) {
	_, err := DecodeMaintenance(Frame{ID: RxCrosspointConnect, Payload: []byte{0x01}})
	if !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong: got %v", err)
	}
	_, err = DecodeMaintenance(Frame{ID: RxMaintenance, Payload: nil})
	if !errors.Is(err, ErrShortPayload) {
		t.Errorf("empty: got %v", err)
	}
	_, err = DecodeMaintenance(Frame{ID: RxMaintenance, Payload: []byte{0x02}}) // ClearProtects needs 3 bytes
	if !errors.Is(err, ErrShortPayload) {
		t.Errorf("short clear-protects: got %v", err)
	}
}

func TestDualControllerStatus_Request(t *testing.T) {
	f := EncodeDualControllerStatusRequest()
	if f.ID != RxDualControllerStatusRequest {
		t.Errorf("ID got %#x", f.ID)
	}
	if len(f.Payload) != 0 {
		t.Errorf("payload should be empty, got %X", f.Payload)
	}
	if err := DecodeDualControllerStatusRequest(f); err != nil {
		t.Errorf("decode: %v", err)
	}
	if err := DecodeDualControllerStatusRequest(Frame{ID: TxCrosspointTally}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong id: %v", err)
	}
}

func TestDualControllerStatus_Response(t *testing.T) {
	cases := []struct {
		name string
		in   DualControllerStatusParams
		want []byte
	}{
		{"master active ok", DualControllerStatusParams{SlaveActive: false, Active: true}, []byte{0x02, 0x00}},
		{"slave active ok", DualControllerStatusParams{SlaveActive: true, Active: true}, []byte{0x03, 0x00}},
		{"master inactive idle faulty", DualControllerStatusParams{IdleControllerFaulty: true}, []byte{0x00, 0x01}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := EncodeDualControllerStatusResponse(tc.in)
			if !bytes.Equal(f.Payload, tc.want) {
				t.Errorf("Payload got %X want %X", f.Payload, tc.want)
			}
			got, err := DecodeDualControllerStatusResponse(f)
			if err != nil || got != tc.in {
				t.Errorf("round-trip: got %+v err %v want %+v", got, err, tc.in)
			}
		})
	}
}

func TestMaintenance_FramerRoundTrip(t *testing.T) {
	p := MaintenanceParams{Function: MaintClearProtects, MatrixID: 0xFF, LevelID: 0xFF}
	wire := Pack(EncodeMaintenance(p))
	back, _, err := Unpack(wire)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	got, err := DecodeMaintenance(back)
	if err != nil || got != p {
		t.Errorf("got %+v err %v want %+v", got, err, p)
	}
}
