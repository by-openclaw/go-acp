package codec

import (
	"encoding/binary"
	"fmt"
)

// Wire sizes of the session structures.
const (
	ConnectSize   = 44 // spec 11.2.2 CONNECT_STR
	TermSessSize  = 22 // spec 11.2.3 TERMSESS_STR
	ClearSessSize = 4  // spec 11.2.4 CLEARSESS_STR
	WaitSize      = 4  // spec 11.6.2 WAIT_STR
)

// Back-channel readiness values (rc3comm.h).
const (
	BackChannelDisable uint8 = 0
	BackChannelEnable  uint8 = 1
	// BackChannelFutureOnly asks the server to report future changes without
	// the catch-up burst it would otherwise send on enable.
	BackChannelFutureOnly uint8 = 2
)

// ReportAllCommands is the command number that enables or disables reporting
// for every command at once (spec 9.33). Not every server honours per-command
// selection, but every server honours this value.
const ReportAllCommands uint16 = 0xFFFF

// Connect is the payload of Call: the services a client wants, the level it
// wants them at, and who is asking (spec 11.2.2).
//
// Services are all-or-nothing: a server that cannot supply every requested bit
// refuses the whole call rather than granting a subset. A client that wants to
// fall back from the 32-bit generation must therefore retry the call without
// SvcLongStr rather than expect a partial grant.
type Connect struct {
	Services  Service
	UserLevel UserLevel
	Caller    DeviceInfo
}

func (c Connect) String() string {
	return fmt.Sprintf("svc=%s level=%s caller=%s", c.Services, c.UserLevel, c.Caller.Address)
}

// AppendTo appends the 44-byte wire form.
func (c Connect) AppendTo(dst []byte) ([]byte, error) {
	var head [4]byte
	binary.BigEndian.PutUint16(head[0:2], uint16(c.Services))
	binary.BigEndian.PutUint16(head[2:4], uint16(c.UserLevel))
	return c.Caller.AppendTo(append(dst, head[:]...))
}

// DecodeConnect reads a CONNECT_STR from the front of b.
func DecodeConnect(b []byte) (Connect, error) {
	if err := need(b, ConnectSize, "Connect", ""); err != nil {
		return Connect{}, err
	}
	return Connect{
		Services:  Service(binary.BigEndian.Uint16(b[0:2])),
		UserLevel: UserLevel(binary.BigEndian.Uint16(b[2:4])),
		Caller:    deviceInfoAt(b[4:44]),
	}, nil
}

// TermSess is the payload of Term: why the session ended (spec 11.2.3).
type TermSess struct {
	Code   TermCode
	Reason string // 19 usable bytes
}

func (t TermSess) String() string {
	if t.Reason == "" {
		return t.Code.String()
	}
	return fmt.Sprintf("%s %q", t.Code, t.Reason)
}

// AppendTo appends the 22-byte wire form.
func (t TermSess) AppendTo(dst []byte) ([]byte, error) {
	var code [2]byte
	binary.BigEndian.PutUint16(code[:], uint16(t.Code))
	return appendFixedString(append(dst, code[:]...), t.Reason, MaxTextSize)
}

// DecodeTermSess reads a TERMSESS_STR from the front of b.
//
// The reason string is optional on the wire: a device may send the code alone.
func DecodeTermSess(b []byte) (TermSess, error) {
	if err := need(b, 2, "TermSess", "Code"); err != nil {
		return TermSess{}, err
	}
	t := TermSess{Code: TermCode(binary.BigEndian.Uint16(b[0:2]))}
	if len(b) >= TermSessSize {
		t.Reason = fixedString(b[2:22])
	}
	return t, nil
}

// ClearSess asks a server to free sessions on the named services
// (spec 11.2.4). A Sessions value of 255 means all of them.
type ClearSess struct {
	Services Service
	Sessions uint16
}

func (c ClearSess) String() string {
	return fmt.Sprintf("svc=%s free=%d", c.Services, c.Sessions)
}

// AppendTo appends the 4-byte wire form.
func (c ClearSess) AppendTo(dst []byte) []byte {
	var b [ClearSessSize]byte
	binary.BigEndian.PutUint16(b[0:2], uint16(c.Services))
	binary.BigEndian.PutUint16(b[2:4], c.Sessions)
	return append(dst, b[:]...)
}

// DecodeClearSess reads a CLEARSESS_STR from the front of b.
func DecodeClearSess(b []byte) (ClearSess, error) {
	if err := need(b, ClearSessSize, "ClearSess", ""); err != nil {
		return ClearSess{}, err
	}
	return ClearSess{
		Services: Service(binary.BigEndian.Uint16(b[0:2])),
		Sessions: binary.BigEndian.Uint16(b[2:4]),
	}, nil
}

// Wait tells a waiting client to extend its timeout (spec 11.6.2).
//
// The vendor library never implements this: its handler answers Nack, and
// because Wait is a valid blind reply the frame is delivered to the caller as
// though it were the answer. We decode it properly so the session layer can do
// what the specification asks and reset the timer.
type Wait struct {
	Seconds uint16
	Mode    Mode   // 0, or ModeString when a reason follows
	Reason  string // optional, NUL-terminated
}

func (w Wait) String() string {
	if w.Reason == "" {
		return fmt.Sprintf("%ds", w.Seconds)
	}
	return fmt.Sprintf("%ds %q", w.Seconds, w.Reason)
}

// AppendTo appends the wire form, including the optional reason.
func (w Wait) AppendTo(dst []byte) ([]byte, error) {
	var b [WaitSize]byte
	binary.BigEndian.PutUint16(b[0:2], w.Seconds)
	mode := w.Mode
	if w.Reason != "" {
		mode |= ModeString
	}
	binary.BigEndian.PutUint16(b[2:4], uint16(mode))
	dst = append(dst, b[:]...)
	if w.Reason != "" {
		return appendCString(dst, w.Reason)
	}
	return dst, nil
}

// DecodeWait reads a WAIT_STR and its optional trailing reason.
func DecodeWait(b []byte) (Wait, error) {
	if err := need(b, WaitSize, "Wait", ""); err != nil {
		return Wait{}, err
	}
	w := Wait{
		Seconds: binary.BigEndian.Uint16(b[0:2]),
		Mode:    Mode(binary.BigEndian.Uint16(b[2:4])),
	}
	if w.Mode.Has(ModeString) && len(b) > WaitSize {
		w.Reason, _ = cString(b[WaitSize:])
	}
	return w, nil
}
