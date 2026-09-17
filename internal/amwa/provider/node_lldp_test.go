package provider

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/lldp"
)

// nodeWithIfaces builds the minimum server state applyLLDPLocked reads, so
// the test does not need a whole served Node.
func nodeWithIfaces(src lldp.Source, names ...string) *IS04NodeServer {
	ifs := make([]is04.NodeIface, 0, len(names))
	for _, n := range names {
		ifs = append(ifs, is04.NodeIface{Name: n, PortID: "00-00-00-00-00-01"})
	}
	return &IS04NodeServer{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		cfg:    IS04NodeConfig{LLDP: src},
		bundle: &NodeConfig{Node: is04.Node{Interfaces: ifs}},
	}
}

func TestAttachedNetworkDeviceFromLLDP(t *testing.T) {
	s := nodeWithIfaces(lldp.StaticSource{
		"eth0": {ChassisID: "00-11-22-33-44-55", PortID: "aa-bb-cc-dd-ee-ff", SysName: "core-sw-1"},
	}, "eth0", "eth1")
	s.applyLLDPLocked(context.Background())

	got := s.bundle.Node.Interfaces[0].AttachedNetworkDevice
	if got == nil {
		t.Fatal("eth0 had a neighbour and must carry attached_network_device")
	}
	if got.ChassisID != "00-11-22-33-44-55" || got.PortID != "aa-bb-cc-dd-ee-ff" {
		t.Errorf("got %+v", got)
	}
	// eth1 had no neighbour: absent is the correct value, not an empty object.
	if s.bundle.Node.Interfaces[1].AttachedNetworkDevice != nil {
		t.Error("an interface with no neighbour must leave the field unset")
	}
}

// Both members are required once the object exists, so a neighbour missing
// either must produce no object at all rather than one that fails validation
// for every reader.
func TestHalfFilledNeighbourIsSkipped(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    lldp.Neighbor
	}{
		{"no chassis", lldp.Neighbor{PortID: "aa-bb-cc-dd-ee-ff"}},
		{"no port", lldp.Neighbor{ChassisID: "00-11-22-33-44-55"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := nodeWithIfaces(lldp.StaticSource{"eth0": tc.n}, "eth0")
			s.applyLLDPLocked(context.Background())
			if s.bundle.Node.Interfaces[0].AttachedNetworkDevice != nil {
				t.Error("a half-filled neighbour must not become a half-filled object")
			}
		})
	}
}

// A plant with LLDP switched off, or a Windows host with no capture, must
// still produce a usable Node.
func TestLLDPFailureIsNeverFatal(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"capture unsupported", lldp.ErrCaptureUnsupported},
		{"source broken", errors.New("switch unreachable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := nodeWithIfaces(lldp.SourceFunc(func(context.Context) (map[string]lldp.Neighbor, error) {
				return nil, tc.err
			}), "eth0")
			s.applyLLDPLocked(context.Background())
			if s.bundle.Node.Interfaces[0].AttachedNetworkDevice != nil {
				t.Error("a failed lookup must leave the field unset, not invent one")
			}
		})
	}
}

// A Cache serves its stale view alongside the error. That partial result is
// still worth using — dropping it would blank a field that was correct a
// moment ago because one refresh failed.
func TestPartialResultWithErrorIsStillUsed(t *testing.T) {
	s := nodeWithIfaces(lldp.SourceFunc(func(context.Context) (map[string]lldp.Neighbor, error) {
		return map[string]lldp.Neighbor{
			"eth0": {ChassisID: "00-11-22-33-44-55", PortID: "aa-bb-cc-dd-ee-ff"},
		}, errors.New("refresh failed, serving stale")
	}), "eth0")
	s.applyLLDPLocked(context.Background())
	if s.bundle.Node.Interfaces[0].AttachedNetworkDevice == nil {
		t.Error("a stale-but-present neighbour must still fill the field")
	}
}

func TestNoSourceIsANoOp(t *testing.T) {
	s := nodeWithIfaces(nil, "eth0")
	s.applyLLDPLocked(context.Background())
	if s.bundle.Node.Interfaces[0].AttachedNetworkDevice != nil {
		t.Error("no source means the Node does not know; the field stays unset")
	}
}

func TestNoInterfacesIsANoOp(t *testing.T) {
	s := nodeWithIfaces(lldp.StaticSource{"eth0": {ChassisID: "a", PortID: "b"}})
	s.applyLLDPLocked(context.Background())
	if len(s.bundle.Node.Interfaces) != 0 {
		t.Error("interfaces must not be invented from LLDP")
	}
}

// Neighbours on names the Node does not declare is the classic
// bundle-vs-host mismatch, and must not pass silently as "no LLDP".
func TestNeighboursOnUndeclaredInterfaces(t *testing.T) {
	s := nodeWithIfaces(lldp.StaticSource{
		"ens192": {ChassisID: "00-11-22-33-44-55", PortID: "aa-bb-cc-dd-ee-ff"},
	}, "eth0")
	s.applyLLDPLocked(context.Background())
	if s.bundle.Node.Interfaces[0].AttachedNetworkDevice != nil {
		t.Error("a neighbour on another interface must not be applied to eth0")
	}
}
