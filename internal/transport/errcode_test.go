package transport

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"dhs/internal/errcode"
)

// TestClassifyDialError pins the cross-OS classification logic that maps
// raw net.OpError errors to the right transport sentinel (refused / timeout
// / generic dial-failed).
func TestClassifyDialError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want *errcode.Code
	}{
		{name: "nil → nil", err: nil, want: nil},
		{name: "ECONNREFUSED → ErrRefused", err: syscall.ECONNREFUSED, want: ErrRefused},
		{name: "wrapped ECONNREFUSED → ErrRefused", err: &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, want: ErrRefused},
		{name: "os.ErrDeadlineExceeded → ErrTimeout", err: os.ErrDeadlineExceeded, want: ErrTimeout},
		{name: "context.DeadlineExceeded → ErrTimeout", err: context.DeadlineExceeded, want: ErrTimeout},
		{name: "generic error → ErrDialFailed", err: errors.New("unknown network failure"), want: ErrDialFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyDialError(tc.err)
			if got != tc.want {
				t.Errorf("classifyDialError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestDialTCP_InputValidation pins that bad input fails fast with the
// validation:* codes before any network operation.
func TestDialTCP_InputValidation(t *testing.T) {
	cases := []struct {
		name string
		host string
		port int
		want *errcode.Code
	}{
		{name: "empty host", host: "", port: 9100, want: ErrInvalidHost},
		{name: "port zero", host: "127.0.0.1", port: 0, want: ErrInvalidPort},
		{name: "port negative", host: "127.0.0.1", port: -1, want: ErrInvalidPort},
		{name: "port too large", host: "127.0.0.1", port: 70000, want: ErrInvalidPort},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DialTCP(context.Background(), tc.host, tc.port)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("DialTCP(%q, %d) = %v, want errors.Is == %v", tc.host, tc.port, err, tc.want)
			}
			if got := errcode.Exit(err); got != 2 {
				t.Errorf("exit class for %v = %d, want 2 (usage)", tc.want, got)
			}
			if !strings.HasPrefix(err.Error(), "validation:") {
				t.Errorf("err string = %q, want validation:* prefix", err.Error())
			}
		})
	}
}

// TestDialUDP_InputValidation mirrors TestDialTCP_InputValidation for UDP.
func TestDialUDP_InputValidation(t *testing.T) {
	cases := []struct {
		name string
		host string
		port int
		want *errcode.Code
	}{
		{name: "empty host", host: "", port: 2071, want: ErrInvalidHost},
		{name: "port zero", host: "127.0.0.1", port: 0, want: ErrInvalidPort},
		{name: "port too large", host: "127.0.0.1", port: 70000, want: ErrInvalidPort},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DialUDP(context.Background(), tc.host, tc.port)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("DialUDP(%q, %d) = %v, want errors.Is == %v", tc.host, tc.port, err, tc.want)
			}
			if got := errcode.Exit(err); got != 2 {
				t.Errorf("exit class for %v = %d, want 2 (usage)", tc.want, got)
			}
		})
	}
}

// TestListenUDP_PortValidation pins the port range check.
func TestListenUDP_PortValidation(t *testing.T) {
	_, err := ListenUDP(context.Background(), 70000)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidPort) {
		t.Errorf("ListenUDP(70000) err = %v, want errors.Is == ErrInvalidPort", err)
	}
	if got := errcode.Exit(err); got != 2 {
		t.Errorf("exit class = %d, want 2", got)
	}
}

// TestDialTCP_LiveRefused performs a live dial to a port nothing is
// listening on and asserts the error chains to ErrRefused. Skipped on
// hosts where the loopback port range may be unpredictable.
func TestDialTCP_LiveRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live dial test in -short mode")
	}
	// Bind a socket then close it to get a definitely-free port.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("setup listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	// Tiny window where the port may still be in TIME_WAIT — best-effort.

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = DialTCP(ctx, "127.0.0.1", port)
	if err == nil {
		t.Fatal("expected dial to fail, got nil")
	}
	// On most platforms the kernel returns ECONNREFUSED here. Accept
	// either refused or generic dial-failed since timing can race.
	if !errors.Is(err, ErrRefused) && !errors.Is(err, ErrDialFailed) {
		t.Errorf("DialTCP to free port = %v, want errors.Is ErrRefused or ErrDialFailed", err)
	}
	if got := errcode.Exit(err); got != 1 {
		t.Errorf("exit class = %d, want 1 (runtime)", got)
	}
	if !strings.HasPrefix(err.Error(), "transport:") {
		t.Errorf("err string = %q, want transport:* prefix", err.Error())
	}
}

// TestTCPConn_NilMethods pins that calling methods on a nil receiver
// returns the typed ErrNilConn rather than panicking or returning a
// raw string.
func TestTCPConn_NilMethods(t *testing.T) {
	var tc *TCPConn

	if err := tc.Send(context.Background(), []byte{0x01}); !errors.Is(err, ErrNilConn) {
		t.Errorf("nil.Send err = %v, want errors.Is ErrNilConn", err)
	}
	if _, err := tc.Receive(context.Background(), 256); !errors.Is(err, ErrNilConn) {
		t.Errorf("nil.Receive err = %v, want errors.Is ErrNilConn", err)
	}
	// Close on nil is intentionally a no-op (returns nil).
	if err := tc.Close(); err != nil {
		t.Errorf("nil.Close err = %v, want nil", err)
	}
}

// TestUDPConn_NilMethods is the UDP analogue.
func TestUDPConn_NilMethods(t *testing.T) {
	var uc *UDPConn

	if err := uc.Send(context.Background(), []byte{0x01}); !errors.Is(err, ErrNilConn) {
		t.Errorf("nil.Send err = %v, want errors.Is ErrNilConn", err)
	}
	if _, err := uc.Receive(context.Background(), 256); !errors.Is(err, ErrNilConn) {
		t.Errorf("nil.Receive err = %v, want errors.Is ErrNilConn", err)
	}
	if err := uc.Close(); err != nil {
		t.Errorf("nil.Close err = %v, want nil", err)
	}
}

// TestSentinels_AllRegisterAsTransport pins that every transport:* code
// declared in errcode.go has the right Layer + Class.
func TestSentinels_AllRegisterAsTransport(t *testing.T) {
	transportCodes := []*errcode.Code{
		ErrDialFailed, ErrRefused, ErrTimeout, ErrListenFailed,
		ErrNilConn, ErrPayloadTooLarge, ErrSetDeadlineFailed,
		ErrWriteFailed, ErrShortWrite, ErrReadFailed,
		ErrOversizedDatagram, ErrMLENOutOfRange, ErrWrongConnType,
		ErrCloseFailed, ErrCaptureCreateFailed,
	}
	for _, c := range transportCodes {
		if c.Layer != errcode.LayerTransport {
			t.Errorf("%v: Layer = %q, want %q", c, c.Layer, errcode.LayerTransport)
		}
		if c.Class != errcode.ClassRuntime {
			t.Errorf("%v: Class = %d, want ClassRuntime (1)", c, c.Class)
		}
	}

	validationCodes := []*errcode.Code{
		ErrInvalidHost, ErrInvalidPort, ErrEmptyPayload, ErrInvalidMaxSize,
	}
	for _, c := range validationCodes {
		if c.Layer != errcode.LayerValidation {
			t.Errorf("%v: Layer = %q, want %q", c, c.Layer, errcode.LayerValidation)
		}
		if c.Class != errcode.ClassUsage {
			t.Errorf("%v: Class = %d, want ClassUsage (2)", c, c.Class)
		}
	}
}

// TestSentinels_StringShape pins the wire-shape contract: every code
// stringifies as "<layer>:<code>".
func TestSentinels_StringShape(t *testing.T) {
	cases := map[*errcode.Code]string{
		ErrDialFailed:          "transport:dial-failed",
		ErrRefused:             "transport:refused",
		ErrTimeout:             "transport:timeout",
		ErrListenFailed:        "transport:listen-failed",
		ErrNilConn:             "transport:nil-conn",
		ErrPayloadTooLarge:     "transport:payload-too-large",
		ErrSetDeadlineFailed:   "transport:set-deadline-failed",
		ErrWriteFailed:         "transport:write-failed",
		ErrShortWrite:          "transport:short-write",
		ErrReadFailed:          "transport:read-failed",
		ErrOversizedDatagram:   "transport:oversized-datagram",
		ErrMLENOutOfRange:      "transport:mlen-out-of-range",
		ErrWrongConnType:       "transport:wrong-conn-type",
		ErrCloseFailed:         "transport:close-failed",
		ErrCaptureCreateFailed: "transport:capture-create-failed",
		ErrInvalidHost:         "validation:invalid-host",
		ErrInvalidPort:         "validation:invalid-port",
		ErrEmptyPayload:        "validation:empty-payload",
		ErrInvalidMaxSize:      "validation:invalid-max-size",
	}
	for code, want := range cases {
		if got := code.Error(); got != want {
			t.Errorf("%v.Error() = %q, want %q", code, got, want)
		}
	}
}
