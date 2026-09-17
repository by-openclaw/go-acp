//go:build linux

package lldp

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"
)

func TestSupportedIsTrueOnLinux(t *testing.T) {
	if !Supported() {
		t.Error("linux captures via AF_PACKET")
	}
}

func TestHtonsIsBigEndianRegardlessOfHost(t *testing.T) {
	var b [2]byte
	binary.NativeEndian.PutUint16(b[:], htons(EtherType))
	if binary.BigEndian.Uint16(b[:]) != EtherType {
		t.Errorf("htons(0x%04X) did not land big-endian in memory", EtherType)
	}
}

// A name that does not exist must be an ERROR, not an empty result: a typo
// otherwise looks exactly like an unplugged cable and sends the operator to
// the wrong end of the link. This runs before any socket is opened, so it
// needs no privileges.
func TestUnknownInterfaceIsAnError(t *testing.T) {
	_, _, err := interfaceIndex("definitely-not-an-interface-0")
	if err == nil {
		t.Fatal("an unknown interface name must be rejected")
	}
	if !strings.Contains(err.Error(), "definitely-not-an-interface-0") {
		t.Errorf("error must name the interface: %v", err)
	}

	// Neighbors resolves the interface first, so it fails the same way
	// without needing CAP_NET_RAW.
	if _, err := (Capture{Iface: "definitely-not-an-interface-0"}).Neighbors(context.Background()); err == nil {
		t.Error("Neighbors must reject an unknown interface")
	}
}

func TestInterfaceIndexResolvesARealInterface(t *testing.T) {
	ifs, err := net.Interfaces()
	if err != nil || len(ifs) == 0 {
		t.Skip("no interfaces to test against")
	}
	want := ifs[0]
	byIndex, idx, err := interfaceIndex(want.Name)
	if err != nil {
		t.Fatalf("interfaceIndex(%q): %v", want.Name, err)
	}
	if idx != want.Index {
		t.Errorf("index = %d, want %d", idx, want.Index)
	}
	if byIndex[want.Index] != want.Name {
		t.Errorf("byIndex[%d] = %q, want %q", want.Index, byIndex[want.Index], want.Name)
	}

	// Empty name means "every interface" and resolves to no specific index.
	if _, idx, err := interfaceIndex(""); err != nil || idx != -1 {
		t.Errorf("empty name: idx=%d err=%v, want -1 and no error", idx, err)
	}
}

// Unprivileged, AF_PACKET is refused. The error must say what to do about it
// rather than surfacing a bare EPERM.
func TestCaptureWithoutCapabilityExplainsItself(t *testing.T) {
	_, err := Capture{Window: time.Millisecond}.Neighbors(context.Background())
	if err == nil {
		t.Skip("this host can open AF_PACKET (running privileged); nothing to assert")
	}
	if !strings.Contains(err.Error(), "cap_net_raw") {
		t.Errorf("a permission failure must name the capability: %v", err)
	}
}
