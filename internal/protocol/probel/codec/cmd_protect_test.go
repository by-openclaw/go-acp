package codec

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func TestProtectInterrogate_General(t *testing.T) {
	p := ProtectInterrogateParams{MatrixID: 1, LevelID: 2, DestinationID: 500, DeviceID: 400}
	f := EncodeProtectInterrogate(p)
	if f.ID != RxProtectInterrogate {
		t.Errorf("ID got %#x", f.ID)
	}
	// matrix/level=0x12, dest=500 → dest/128=3 in bits4-6=0x30; dev=400/128=3 in bits0-2=0x03
	// dest%128=116=0x74
	want := []byte{0x12, 0x33, 0x74}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("Payload got %X want %X", f.Payload, want)
	}
	got, err := DecodeProtectInterrogate(f)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	// In general form DeviceID is encoded only as /128 → decoded value is 3*128=384
	if got.MatrixID != p.MatrixID || got.LevelID != p.LevelID || got.DestinationID != p.DestinationID {
		t.Errorf("round-trip: got %+v want %+v", got, p)
	}
}

func TestProtectInterrogate_Extended(t *testing.T) {
	p := ProtectInterrogateParams{MatrixID: 30, LevelID: 2, DestinationID: 1000}
	f := EncodeProtectInterrogate(p)
	if f.ID != RxProtectInterrogateExt {
		t.Errorf("ID got %#x", f.ID)
	}
	want := []byte{30, 2, 0x03, 0xE8}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("Payload got %X", f.Payload)
	}
	got, _ := DecodeProtectInterrogate(f)
	if got.MatrixID != p.MatrixID || got.DestinationID != p.DestinationID {
		t.Errorf("round-trip: got %+v want %+v", got, p)
	}
}

func TestProtectTally_RoundTrip(t *testing.T) {
	p := ProtectTallyParams{
		MatrixID:      1,
		LevelID:       2,
		DestinationID: 500,
		DeviceID:      600,
		State:         ProtectProbel,
	}
	f := EncodeProtectTally(p)
	if f.ID != TxProtectTally {
		t.Errorf("ID got %#x", f.ID)
	}
	// matrix/level=0x12, state=1, multiplier=0x34, dest%128=0x74, dev%128=0x58
	want := []byte{0x12, 0x01, 0x34, 0x74, 0x58}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("Payload got %X want %X", f.Payload, want)
	}
	got, err := DecodeProtectTally(f)
	if err != nil || got != p {
		t.Errorf("round-trip: got %+v err %v want %+v", got, err, p)
	}
}

func TestProtectTally_Extended(t *testing.T) {
	p := ProtectTallyParams{
		MatrixID:      30,
		LevelID:       4,
		DestinationID: 1000,
		DeviceID:      2000,
		State:         ProtectOEM,
	}
	f := EncodeProtectTally(p)
	if f.ID != TxProtectTallyExt {
		t.Errorf("ID got %#x want ext 0x8B", f.ID)
	}
	// matrix=30, level=4, state=3, dest=03 E8, dev=07 D0
	want := []byte{30, 4, 3, 0x03, 0xE8, 0x07, 0xD0}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("Payload got %X", f.Payload)
	}
	got, _ := DecodeProtectTally(f)
	if got != p {
		t.Errorf("round-trip ext: got %+v want %+v", got, p)
	}
}

func TestProtectConnect_RoundTrip(t *testing.T) {
	p := ProtectConnectParams{MatrixID: 1, LevelID: 2, DestinationID: 500, DeviceID: 600}
	f := EncodeProtectConnect(p)
	if f.ID != RxProtectConnect {
		t.Errorf("ID got %#x", f.ID)
	}
	want := []byte{0x12, 0x34, 0x74, 0x58}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("Payload got %X", f.Payload)
	}
	got, _ := DecodeProtectConnect(f)
	if got != p {
		t.Errorf("got %+v want %+v", got, p)
	}
}

func TestProtectConnected_RoundTrip(t *testing.T) {
	p := ProtectConnectedParams{
		MatrixID: 1, LevelID: 2, DestinationID: 500, DeviceID: 600, State: ProtectProbelOver,
	}
	f := EncodeProtectConnected(p)
	if f.ID != TxProtectConnected {
		t.Errorf("ID got %#x want 0x0D", f.ID)
	}
	// Same body as Tally with CommandID 0x0D
	got, err := DecodeProtectConnected(f)
	if err != nil || got != p {
		t.Errorf("round-trip: got %+v err %v want %+v", got, err, p)
	}
}

func TestProtectDisconnect_RoundTrip(t *testing.T) {
	p := ProtectDisconnectParams{MatrixID: 2, LevelID: 1, DestinationID: 100, DeviceID: 200}
	f := EncodeProtectDisconnect(p)
	if f.ID != RxProtectDisconnect {
		t.Errorf("ID got %#x want 0x0E", f.ID)
	}
	got, _ := DecodeProtectDisconnect(f)
	if got != p {
		t.Errorf("got %+v want %+v", got, p)
	}
}

func TestProtectDisconnected_Ext(t *testing.T) {
	p := ProtectDisconnectedParams{
		MatrixID: 20, LevelID: 4, DestinationID: 1000, DeviceID: 500, State: ProtectNone,
	}
	f := EncodeProtectDisconnected(p)
	if f.ID != TxProtectDisconnectedExt {
		t.Errorf("ID got %#x want 0x8F", f.ID)
	}
	got, err := DecodeProtectDisconnected(f)
	if err != nil || got != p {
		t.Errorf("round-trip: got %+v err %v want %+v", got, err, p)
	}
}

func TestProtectDeviceNameRequest(t *testing.T) {
	f := EncodeProtectDeviceNameRequest(ProtectDeviceNameRequestParams{DeviceID: 300})
	if f.ID != RxProtectDeviceNameRequest {
		t.Errorf("ID got %#x", f.ID)
	}
	// 300/128=2, 300%128=44=0x2C
	want := []byte{0x02, 0x2C}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("Payload got %X want %X", f.Payload, want)
	}
	got, err := DecodeProtectDeviceNameRequest(f)
	if err != nil || got.DeviceID != 300 {
		t.Errorf("round-trip: got %+v err %v", got, err)
	}
}

func TestProtectDeviceNameResponse(t *testing.T) {
	p := ProtectDeviceNameResponseParams{DeviceID: 300, DeviceName: "PANEL01"}
	f := EncodeProtectDeviceNameResponse(p)
	if f.ID != TxProtectDeviceNameResponse {
		t.Errorf("ID got %#x", f.ID)
	}
	// 10 bytes: 02 2C + " PANEL01" (space-padded to 8)
	want := []byte{0x02, 0x2C, ' ', 'P', 'A', 'N', 'E', 'L', '0', '1'}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("Payload got %X want %X", f.Payload, want)
	}
	got, err := DecodeProtectDeviceNameResponse(f)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.DeviceID != p.DeviceID || got.DeviceName != p.DeviceName {
		t.Errorf("round-trip: got %+v want %+v", got, p)
	}
}

func TestProtectTallyDumpRequest(t *testing.T) {
	p := ProtectTallyDumpRequestParams{MatrixID: 1, LevelID: 2, DestinationID: 500}
	f := EncodeProtectTallyDumpRequest(p)
	if f.ID != RxProtectTallyDumpRequest {
		t.Errorf("ID got %#x", f.ID)
	}
	// matrix/level=0x12, dest=01 F4
	want := []byte{0x12, 0x01, 0xF4}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("Payload got %X", f.Payload)
	}
	got, _ := DecodeProtectTallyDumpRequest(f)
	if got != p {
		t.Errorf("got %+v want %+v", got, p)
	}
}

func TestProtectTallyDump_RoundTrip(t *testing.T) {
	p := ProtectTallyDumpParams{
		MatrixID:           1,
		LevelID:            2,
		FirstDestinationID: 100,
		Items: []ProtectTallyItem{
			{State: ProtectProbel, DeviceID: 50},
			{State: ProtectOEM, DeviceID: 500},
			{State: ProtectNone, DeviceID: 0},
		},
	}
	f := EncodeProtectTallyDump(p)
	if f.ID != TxProtectTallyDump {
		t.Errorf("ID got %#x", f.ID)
	}
	// matrix/level=0x12, count=3, dest=00 64
	// item0: hi = (1<<4)|(50/256=0) = 0x10, lo = 50
	// item1: hi = (3<<4)|(500/256=1) = 0x31, lo = 500%256 = 244 = 0xF4
	// item2: hi = 0, lo = 0
	want := []byte{0x12, 0x03, 0x00, 0x64, 0x10, 50, 0x31, 0xF4, 0x00, 0x00}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("Payload got %X want %X", f.Payload, want)
	}
	got, err := DecodeProtectTallyDump(f)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.MatrixID != p.MatrixID || !reflect.DeepEqual(got.Items, p.Items) {
		t.Errorf("round-trip: got %+v want %+v", got, p)
	}
}

func TestMasterProtectConnect(t *testing.T) {
	p := MasterProtectConnectParams{MatrixID: 2, LevelID: 3, DestinationID: 400, DeviceID: 500}
	f := EncodeMasterProtectConnect(p)
	if f.ID != RxMasterProtectConnect {
		t.Errorf("ID got %#x want 0x1D", f.ID)
	}
	want := []byte{2, 3, 0x01, 0x90, 0x01, 0xF4}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("Payload got %X", f.Payload)
	}
	got, err := DecodeMasterProtectConnect(f)
	if err != nil || got != p {
		t.Errorf("round-trip: got %+v err %v want %+v", got, err, p)
	}
}

func TestProtect_DecodeErrors(t *testing.T) {
	if _, err := DecodeProtectInterrogate(Frame{ID: TxCrosspointTally}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("interrogate: %v", err)
	}
	if _, err := DecodeProtectTally(Frame{ID: TxProtectTally, Payload: []byte{1, 2}}); !errors.Is(err, ErrShortPayload) {
		t.Errorf("tally short: %v", err)
	}
	if _, err := DecodeProtectConnect(Frame{ID: RxMaintenance}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("connect wrong: %v", err)
	}
	if _, err := DecodeProtectDisconnect(Frame{ID: RxProtectConnect}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("disconnect must reject RxProtectConnect: %v", err)
	}
}
