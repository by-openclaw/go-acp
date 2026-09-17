package codec

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// specDeviceInfo is the 40-byte DEVICEINFO_STR of spec 11.3.4, laid out field
// by field so a change to any offset fails here rather than three units later.
const specDeviceInfo = "0003" + // rProtocolVersion 3
	"0000 ff 00 00ff" + // rAddress 0000-FF-00:FF
	"8003" + // rID.rServices Menus|Control|LongStr
	"01e3" + // rID.rTypeID 483
	"01 00 20 01" + // rID.rVersion 1.0, alpha ' ', command set 1
	"5465737443 6c69656e74 0000000000 0000000000" + // rID.rName "TestClient"
	"0000" + // rStatus.rServiceStatus none busy
	"0008" // rStatus.rStatus Present

func wantDeviceInfo() DeviceInfo {
	return DeviceInfo{
		ProtocolVersion: 3,
		Address:         Address{Unit: 0xFF, Index: IndexUnknown},
		ID: ID{
			Services: SvcMenus | SvcControl | SvcLongStr,
			TypeID:   483,
			Version:  Version{Major: 1, Minor: 0, Alpha: ' ', CmdSet: 1},
			Name:     "TestClient",
		},
		Status: UnitStatus{Status: StatusPresent},
	}
}

func TestDeviceInfo_Decode(t *testing.T) {
	got, err := DecodeDeviceInfo(mustHex(t, specDeviceInfo))
	if err != nil {
		t.Fatalf("DecodeDeviceInfo: %v", err)
	}
	if want := wantDeviceInfo(); got != want {
		t.Errorf("decoded\n got %+v\nwant %+v", got, want)
	}
}

func TestDeviceInfo_Encode(t *testing.T) {
	want := mustHex(t, specDeviceInfo)
	got, err := wantDeviceInfo().AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("encoded\n got %x\nwant %x", got, want)
	}
	if len(got) != DeviceInfoSize {
		t.Errorf("encoded %d bytes, want %d", len(got), DeviceInfoSize)
	}
}

func TestDeviceInfo_Errors(t *testing.T) {
	good := mustHex(t, specDeviceInfo)

	// Every truncation must be reported, never silently zero-filled.
	for _, n := range []int{0, 1, 7, 39} {
		if _, err := DecodeDeviceInfo(good[:n]); !errors.Is(err, ErrShortBuffer) {
			t.Errorf("DecodeDeviceInfo(%d bytes) err = %v, want ErrShortBuffer", n, err)
		}
	}

	d := wantDeviceInfo()
	d.ID.Name = strings.Repeat("x", MaxTextSize)
	if _, err := d.AppendTo(nil); !errors.Is(err, ErrStringTooLong) {
		t.Errorf("err = %v, want ErrStringTooLong", err)
	}
}

// TestDeviceInfo_AddressNetIsNotARoute pins the note in spec 11.3.4: the
// address inside a DeviceInfo is not rewritten as the message crosses a
// bridge, so its Net is meaningless. Routing from it reaches the wrong unit on
// any bridged network.
func TestDeviceInfo_AddressNetIsNotARoute(t *testing.T) {
	got, err := DecodeDeviceInfo(mustHex(t, specDeviceInfo))
	if err != nil {
		t.Fatalf("DecodeDeviceInfo: %v", err)
	}
	if got.Address.Net != 0 {
		t.Errorf("Net = %04X; a device reports its own address with no route", got.Address.Net)
	}
}

func TestID_RoundTrip(t *testing.T) {
	tests := []ID{
		{},
		{Services: SvcMenus, TypeID: 1, Version: Version{Major: 2, Minor: 3, Alpha: 'a', CmdSet: 4}, Name: "Vega"},
		// The name field holds 19 bytes plus the terminator.
		{Name: strings.Repeat("n", MaxTextSize-1)},
		{Services: 0xFFFF, TypeID: 0xFFFF, Version: Version{0xFF, 0xFF, 0xFF, 0xFF}},
	}
	for _, want := range tests {
		b, err := want.AppendTo(nil)
		if err != nil {
			t.Fatalf("AppendTo %+v: %v", want, err)
		}
		if len(b) != IDSize {
			t.Errorf("encoded %d bytes, want %d", len(b), IDSize)
		}
		got, err := DecodeID(b)
		if err != nil {
			t.Fatalf("DecodeID: %v", err)
		}
		if got != want {
			t.Errorf("round trip\n got %+v\nwant %+v", got, want)
		}
	}
}

func TestID_Short(t *testing.T) {
	if _, err := DecodeID(make([]byte, IDSize-1)); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
}

func TestID_String(t *testing.T) {
	d := wantDeviceInfo().ID
	got := d.String()
	for _, want := range []string{"483", "1.0 .cs1", `"TestClient"`, "LongStr"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, want it to contain %q", got, want)
		}
	}
}

// TestVersion_String covers the alpha field, which is a character and is a
// space for a purely numeric version. Devices also leave it zero, and printing
// a NUL into a log line is worse than printing a space.
func TestVersion_String(t *testing.T) {
	tests := []struct {
		v    Version
		want string
	}{
		{Version{1, 0, ' ', 1}, "1.0 .cs1"},
		{Version{2, 14, 'b', 3}, "2.14b.cs3"},
		{Version{1, 0, 0, 0}, "1.0 .cs0"},    // NUL prints as a space
		{Version{1, 0, 0xFF, 0}, "1.0 .cs0"}, // so does anything unprintable
		{Version{255, 255, '~', 255}, "255.255~.cs255"},
	}
	for _, tc := range tests {
		if got := tc.v.String(); got != tc.want {
			t.Errorf("Version%+v = %q, want %q", tc.v, got, tc.want)
		}
	}
}

func TestUnitStatus_RoundTrip(t *testing.T) {
	tests := []UnitStatus{
		{},
		{Status: StatusPresent},
		{ServiceStatus: SvcMenus | SvcControl, Status: StatusOnline | StatusPresent | StatusLocal},
		{ServiceStatus: 0xFFFF, Status: 0xFFFF},
	}
	for _, want := range tests {
		b := want.AppendTo(nil)
		if len(b) != StatusSize {
			t.Errorf("encoded %d bytes, want %d", len(b), StatusSize)
		}
		got, err := DecodeUnitStatus(b)
		if err != nil {
			t.Fatalf("DecodeUnitStatus: %v", err)
		}
		if got != want {
			t.Errorf("round trip %+v -> %+v", want, got)
		}
	}

	if _, err := DecodeUnitStatus([]byte{0, 0, 0}); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}

	s := UnitStatus{ServiceStatus: SvcMenus, Status: StatusPresent}
	if got := s.String(); got != `busy=Menus status=Present` {
		t.Errorf("String() = %q", got)
	}
}

func TestDeviceInfo_String(t *testing.T) {
	got := wantDeviceInfo().String()
	for _, want := range []string{"0000-FF-00:0FF", "TestClient", "Present"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, want it to contain %q", got, want)
		}
	}
}

// TestProtocolVersionIsInformational records that RollCall has no version
// negotiation: the field is written and never read, and capability is carried
// by the service mask instead. A future change that starts branching on this
// value should fail here first.
func TestProtocolVersionIsInformational(t *testing.T) {
	if ProtocolVersion != 3 {
		t.Errorf("ProtocolVersion = %d, want 3", ProtocolVersion)
	}
	d := wantDeviceInfo()
	d.ProtocolVersion = 99
	b, err := d.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	got, err := DecodeDeviceInfo(b)
	if err != nil {
		t.Fatalf("an unexpected protocol version must still decode: %v", err)
	}
	if got.ProtocolVersion != 99 {
		t.Errorf("ProtocolVersion = %d, want it carried through unchanged", got.ProtocolVersion)
	}
}
