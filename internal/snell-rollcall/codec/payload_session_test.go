package codec

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// specConnect is the 44-byte CONNECT_STR of spec 11.2.2: the requested service
// mask, the requested user level, and the caller's own DeviceInfo.
const specConnect = "8003" + // rServices Menus|Control|LongStr
	"0002" + // rUserLevel supervisor
	specDeviceInfo

func TestConnect_Decode(t *testing.T) {
	got, err := DecodeConnect(mustHex(t, specConnect))
	if err != nil {
		t.Fatalf("DecodeConnect: %v", err)
	}
	want := Connect{
		Services:  SvcMenus | SvcControl | SvcLongStr,
		UserLevel: LevelSupervisor,
		Caller:    wantDeviceInfo(),
	}
	if got != want {
		t.Errorf("decoded\n got %+v\nwant %+v", got, want)
	}
}

func TestConnect_Encode(t *testing.T) {
	c := Connect{
		Services:  SvcMenus | SvcControl | SvcLongStr,
		UserLevel: LevelSupervisor,
		Caller:    wantDeviceInfo(),
	}
	got, err := c.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	if want := mustHex(t, specConnect); !bytes.Equal(got, want) {
		t.Errorf("encoded\n got %x\nwant %x", got, want)
	}
	if len(got) != ConnectSize {
		t.Errorf("encoded %d bytes, want %d", len(got), ConnectSize)
	}
}

// TestConnect_GenerationSelection pins how a client chooses a wire generation.
// There is no version field: the presence of SvcLongStr in the Call is the
// whole of the negotiation, and a server grants services all-or-nothing. A
// client that wants to fall back must issue a second Call without the bit,
// never expect a partial grant.
func TestConnect_GenerationSelection(t *testing.T) {
	base := Connect{Services: SvcMenus | SvcControl, Caller: wantDeviceInfo()}

	gen16 := base
	gen32 := base
	gen32.Services |= SvcLongStr

	if gen16.Services.LongStrings() {
		t.Error("a 16-bit call must not carry the long-string bit")
	}
	if !gen32.Services.LongStrings() {
		t.Error("a 32-bit call must carry the long-string bit")
	}

	// The two calls differ in exactly one bit of one field, so a device that
	// answers one and refuses the other is telling us its generation.
	b16, err := gen16.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	b32, err := gen32.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	if len(b16) != len(b32) {
		t.Fatalf("the two generations must call with the same %d-byte structure", ConnectSize)
	}
	diff := 0
	for i := range b16 {
		if b16[i] != b32[i] {
			diff++
		}
	}
	if diff != 1 {
		t.Errorf("the calls differ in %d bytes, want 1 (the service mask high byte)", diff)
	}
}

func TestConnect_Short(t *testing.T) {
	good := mustHex(t, specConnect)
	for _, n := range []int{0, 3, 43} {
		if _, err := DecodeConnect(good[:n]); !errors.Is(err, ErrShortBuffer) {
			t.Errorf("DecodeConnect(%d bytes) err = %v, want ErrShortBuffer", n, err)
		}
	}

	c := Connect{Caller: wantDeviceInfo()}
	c.Caller.ID.Name = strings.Repeat("x", MaxTextSize)
	if _, err := c.AppendTo(nil); !errors.Is(err, ErrStringTooLong) {
		t.Errorf("err = %v, want ErrStringTooLong", err)
	}
}

func TestConnect_String(t *testing.T) {
	c := Connect{Services: SvcMenus, UserLevel: LevelFactory, Caller: wantDeviceInfo()}
	got := c.String()
	for _, want := range []string{"Menus", "factory", "0000-FF-00"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, want it to contain %q", got, want)
		}
	}
}

func TestTermSess_RoundTrip(t *testing.T) {
	tests := []TermSess{
		{Code: TermUser},
		{Code: TermTimeout, Reason: "idle"},
		{Code: TermNetError, Reason: strings.Repeat("r", MaxTextSize-1)},
		{Code: TermRemote, Reason: "peer closed"},
	}
	for _, want := range tests {
		b, err := want.AppendTo(nil)
		if err != nil {
			t.Fatalf("AppendTo %+v: %v", want, err)
		}
		if len(b) != TermSessSize {
			t.Errorf("encoded %d bytes, want %d", len(b), TermSessSize)
		}
		got, err := DecodeTermSess(b)
		if err != nil {
			t.Fatalf("DecodeTermSess: %v", err)
		}
		if got != want {
			t.Errorf("round trip %+v -> %+v", want, got)
		}
	}
}

// TestTermSess_CodeOnly covers the short form real devices send: the two-byte
// reason code with no text. Requiring the full structure would drop the
// termination and leave the session layer waiting for a reply that never comes.
func TestTermSess_CodeOnly(t *testing.T) {
	got, err := DecodeTermSess([]byte{0x00, 0x01})
	if err != nil {
		t.Fatalf("DecodeTermSess: %v", err)
	}
	if got.Code != TermTimeout || got.Reason != "" {
		t.Errorf("= %+v, want the timeout code with no reason", got)
	}

	if _, err := DecodeTermSess([]byte{0x00}); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
}

func TestTermSess_String(t *testing.T) {
	if got := (TermSess{Code: TermTimeout}).String(); got != "timeout" {
		t.Errorf("String() = %q, want %q", got, "timeout")
	}
	if got := (TermSess{Code: TermUser, Reason: "bye"}).String(); got != `user "bye"` {
		t.Errorf("String() = %q", got)
	}
}

func TestClearSess_RoundTrip(t *testing.T) {
	tests := []ClearSess{
		{},
		{Services: SvcMenus | SvcControl, Sessions: 1},
		{Services: SvcFile, Sessions: 255}, // 255 means every session
	}
	for _, want := range tests {
		b := want.AppendTo(nil)
		if len(b) != ClearSessSize {
			t.Errorf("encoded %d bytes, want %d", len(b), ClearSessSize)
		}
		got, err := DecodeClearSess(b)
		if err != nil {
			t.Fatalf("DecodeClearSess: %v", err)
		}
		if got != want {
			t.Errorf("round trip %+v -> %+v", want, got)
		}
	}

	if _, err := DecodeClearSess([]byte{0, 0, 0}); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
	if got := (ClearSess{Services: SvcMenus, Sessions: 255}).String(); got != "svc=Menus free=255" {
		t.Errorf("String() = %q", got)
	}
}

// TestWait_RoundTrip covers the timeout-extension message. The vendor library
// never implements it and answers Nack, but Wait is a valid blind reply, so a
// device that does send it would otherwise be misread as answering the request.
func TestWait_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   Wait
		want Wait
		size int
	}{
		{"bare", Wait{Seconds: 30}, Wait{Seconds: 30}, WaitSize},
		{
			"with a reason",
			Wait{Seconds: 5, Reason: "loading"},
			Wait{Seconds: 5, Mode: ModeString, Reason: "loading"},
			WaitSize + len("loading") + 1,
		},
		{
			"mode set explicitly",
			Wait{Seconds: 1, Mode: ModeString, Reason: "x"},
			Wait{Seconds: 1, Mode: ModeString, Reason: "x"},
			WaitSize + 2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := tc.in.AppendTo(nil)
			if err != nil {
				t.Fatalf("AppendTo: %v", err)
			}
			if len(b) != tc.size {
				t.Errorf("encoded %d bytes, want %d", len(b), tc.size)
			}
			got, err := DecodeWait(b)
			if err != nil {
				t.Fatalf("DecodeWait: %v", err)
			}
			if got != tc.want {
				t.Errorf("= %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestWait_StringModeWithoutText covers a peer that sets the string flag but
// sends nothing after the fixed part. The message still extends the timeout,
// so dropping it would time the session out for no reason.
func TestWait_StringModeWithoutText(t *testing.T) {
	got, err := DecodeWait([]byte{0x00, 0x0A, 0x00, 0x02})
	if err != nil {
		t.Fatalf("DecodeWait: %v", err)
	}
	if got.Seconds != 10 || got.Reason != "" {
		t.Errorf("= %+v, want 10 seconds and no reason", got)
	}
}

func TestWait_Short(t *testing.T) {
	if _, err := DecodeWait([]byte{0, 0, 0}); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
}

func TestWait_ReasonTooLong(t *testing.T) {
	w := Wait{Seconds: 1, Reason: strings.Repeat("x", MaxLongString)}
	if _, err := w.AppendTo(nil); !errors.Is(err, ErrStringTooLong) {
		t.Errorf("err = %v, want ErrStringTooLong", err)
	}
}

func TestWait_String(t *testing.T) {
	if got := (Wait{Seconds: 30}).String(); got != "30s" {
		t.Errorf("String() = %q", got)
	}
	if got := (Wait{Seconds: 5, Reason: "busy"}).String(); got != `5s "busy"` {
		t.Errorf("String() = %q", got)
	}
}

// TestBackChannelConstants pins the three readiness values. FutureOnly is the
// one worth naming: it suppresses the catch-up burst a server otherwise sends
// on enable, which is the difference between a quiet subscribe and several
// hundred frames arriving at once on a large unit.
func TestBackChannelConstants(t *testing.T) {
	if BackChannelDisable != 0 || BackChannelEnable != 1 || BackChannelFutureOnly != 2 {
		t.Errorf("back-channel values are %d/%d/%d, want 0/1/2",
			BackChannelDisable, BackChannelEnable, BackChannelFutureOnly)
	}
	if ReportAllCommands != 0xFFFF {
		t.Errorf("ReportAllCommands = %04X, want FFFF", ReportAllCommands)
	}
}
