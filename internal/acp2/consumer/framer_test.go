package acp2

import (
	"bytes"
	"testing"
)

func TestEncodeDecodeAN2Frame(t *testing.T) {
	tests := []struct {
		name  string
		frame AN2Frame
	}{
		{
			name: "ACP2 data frame",
			frame: AN2Frame{
				Proto:   AN2ProtoACP2,
				Slot:    1,
				MTID:    0, // data frames always have mtid=0
				Type:    AN2TypeData,
				Payload: []byte{0x00, 0x01, 0x01, 0x00},
			},
		},
		{
			name: "AN2 internal request",
			frame: AN2Frame{
				Proto:   AN2ProtoInternal,
				Slot:    0,
				MTID:    1,
				Type:    AN2TypeRequest,
				Payload: []byte{AN2FuncGetVersion},
			},
		},
		{
			name: "empty payload",
			frame: AN2Frame{
				Proto:   AN2ProtoACP2,
				Slot:    255, // broadcast
				MTID:    0,
				Type:    AN2TypeEvent,
				Payload: nil,
			},
		},
		{
			name: "large payload",
			frame: AN2Frame{
				Proto:   AN2ProtoACP2,
				Slot:    10,
				MTID:    0,
				Type:    AN2TypeData,
				Payload: bytes.Repeat([]byte{0xAB}, 1000),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := EncodeAN2Frame(&tt.frame)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}

			// Verify magic bytes.
			if data[0] != 0xC6 || data[1] != 0x35 {
				t.Fatalf("magic: got 0x%02X%02X, want 0xC635", data[0], data[1])
			}

			// Verify header fields.
			if data[2] != byte(tt.frame.Proto) {
				t.Errorf("proto: got %d, want %d", data[2], tt.frame.Proto)
			}
			if data[3] != tt.frame.Slot {
				t.Errorf("slot: got %d, want %d", data[3], tt.frame.Slot)
			}
			if data[4] != tt.frame.MTID {
				t.Errorf("mtid: got %d, want %d", data[4], tt.frame.MTID)
			}
			if data[5] != byte(tt.frame.Type) {
				t.Errorf("type: got %d, want %d", data[5], tt.frame.Type)
			}

			// Decode and compare.
			decoded, consumed, err := DecodeAN2Frame(data)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if consumed != len(data) {
				t.Errorf("consumed: got %d, want %d", consumed, len(data))
			}
			if decoded.Proto != tt.frame.Proto {
				t.Errorf("decoded proto: got %d, want %d", decoded.Proto, tt.frame.Proto)
			}
			if decoded.Slot != tt.frame.Slot {
				t.Errorf("decoded slot: got %d, want %d", decoded.Slot, tt.frame.Slot)
			}
			if decoded.MTID != tt.frame.MTID {
				t.Errorf("decoded mtid: got %d, want %d", decoded.MTID, tt.frame.MTID)
			}
			if decoded.Type != tt.frame.Type {
				t.Errorf("decoded type: got %d, want %d", decoded.Type, tt.frame.Type)
			}
			wantPayload := tt.frame.Payload
			if wantPayload == nil {
				wantPayload = []byte{}
			}
			if !bytes.Equal(decoded.Payload, wantPayload) {
				t.Errorf("decoded payload mismatch: got %d bytes, want %d bytes",
					len(decoded.Payload), len(wantPayload))
			}
		})
	}
}

func TestDecodeAN2Frame_BadMagic(t *testing.T) {
	data := []byte{0x00, 0x00, 0x02, 0x01, 0x00, 0x04, 0x00, 0x04, 0x00, 0x01, 0x01, 0x00}
	_, _, err := DecodeAN2Frame(data)
	if err == nil {
		t.Fatal("expected error for bad magic")
	}
}

func TestDecodeAN2Frame_TooShort(t *testing.T) {
	data := []byte{0xC6, 0x35, 0x02}
	_, _, err := DecodeAN2Frame(data)
	if err == nil {
		t.Fatal("expected error for short buffer")
	}
}

func TestReadAN2Frame(t *testing.T) {
	frame := &AN2Frame{
		Proto:   AN2ProtoACP2,
		Slot:    5,
		MTID:    0,
		Type:    AN2TypeData,
		Payload: []byte{0x01, 0x03, 0x01, 0x00, 0x00, 0x00, 0x01, 0x00},
	}
	data, err := EncodeAN2Frame(frame)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	reader := bytes.NewReader(data)
	decoded, err := ReadAN2Frame(reader)
	if err != nil {
		t.Fatalf("ReadAN2Frame: %v", err)
	}

	if decoded.Proto != frame.Proto || decoded.Slot != frame.Slot {
		t.Errorf("mismatch: proto=%d/%d slot=%d/%d",
			decoded.Proto, frame.Proto, decoded.Slot, frame.Slot)
	}
	if !bytes.Equal(decoded.Payload, frame.Payload) {
		t.Errorf("payload mismatch")
	}
}

func TestReadAN2Frame_BadMagic(t *testing.T) {
	data := []byte{0x00, 0x00, 0x02, 0x01, 0x00, 0x04, 0x00, 0x00}
	reader := bytes.NewReader(data)
	_, err := ReadAN2Frame(reader)
	if err == nil {
		t.Fatal("expected error for bad magic")
	}
}
