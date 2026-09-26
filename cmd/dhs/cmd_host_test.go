package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"dhs/internal/lldp"
)

func hostIfaces() []net.Interface {
	return []net.Interface{
		{Name: "eth0", Flags: net.FlagUp, HardwareAddr: net.HardwareAddr{2, 0, 0, 0, 0, 1}},
		{Name: "eth1", Flags: net.FlagUp, HardwareAddr: net.HardwareAddr{2, 0, 0, 0, 0, 2}},
	}
}

func listed(ifs []net.Interface, err error) func(string) ([]net.Interface, error) {
	return func(string) ([]net.Interface, error) { return ifs, err }
}

var sw = lldp.Neighbor{
	SysName: "core-sw1", SysDesc: "switch", ChassisID: "00-11-22-33-44-55",
	PortID: "Ethernet12", PortDesc: "rack 4", MgmtAddr: net.IPv4(10, 0, 0, 1), TTL: 120 * time.Second,
}

func TestHostInfoTable(t *testing.T) {
	var gotUntil func(map[string]lldp.Neighbor) bool
	capture := func(_ context.Context, iface string, window time.Duration, until func(map[string]lldp.Neighbor) bool) (map[string]lldp.Neighbor, error) {
		if iface != "" || window != 35*time.Second {
			t.Errorf("capture(%q, %s), want every interface for 35s", iface, window)
		}
		gotUntil = until
		return map[string]lldp.Neighbor{"eth0": sw}, nil
	}
	var out, errOut bytes.Buffer
	if err := hostInfo(context.Background(), nil, &out, &errOut, listed(hostIfaces(), nil), capture); err != nil {
		t.Fatalf("hostInfo: %v", err)
	}
	s := out.String()
	for _, want := range []string{"HOST", "eth0", "core-sw1", "Ethernet12", "rack 4", "10.0.0.1", "eth1", "(nothing heard in 35s)"} {
		if !strings.Contains(s, want) {
			t.Errorf("table lacks %q:\n%s", want, s)
		}
	}
	if !strings.Contains(errOut.String(), "listening for LLDP on eth0, eth1") {
		t.Errorf("stderr = %q", errOut.String())
	}
	// Until is satisfied only once every listed interface is heard.
	if gotUntil(map[string]lldp.Neighbor{"eth0": sw}) || !gotUntil(map[string]lldp.Neighbor{"eth0": sw, "eth1": sw}) {
		t.Error("until must wait for every interface")
	}
}

func TestHostInfoJSON(t *testing.T) {
	capture := func(context.Context, string, time.Duration, func(map[string]lldp.Neighbor) bool) (map[string]lldp.Neighbor, error) {
		nb := sw
		nb.SysName, nb.MgmtAddr = "", nil
		return map[string]lldp.Neighbor{"eth0": nb}, nil
	}
	var out bytes.Buffer
	if err := hostInfo(context.Background(), []string{"--json", "--iface", "eth0", "--window", "1s"}, &out, &bytes.Buffer{},
		listed(hostIfaces()[:1], nil), capture); err != nil {
		t.Fatalf("hostInfo: %v", err)
	}
	var r hostReport
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if len(r.Interfaces) != 1 || r.Interfaces[0].Switch == nil || r.Interfaces[0].Switch.PortID != "Ethernet12" ||
		r.Interfaces[0].Switch.TTLSeconds != 120 || r.Interfaces[0].Switch.MgmtAddr != "" {
		t.Errorf("report = %+v", r)
	}
}

// A switch that sends no system name is shown by its chassis ID.
func TestHostInfoFallsBackToTheChassisID(t *testing.T) {
	capture := func(context.Context, string, time.Duration, func(map[string]lldp.Neighbor) bool) (map[string]lldp.Neighbor, error) {
		nb := sw
		nb.SysName, nb.PortDesc, nb.MgmtAddr = "", "", nil
		return map[string]lldp.Neighbor{"eth0": nb}, nil
	}
	var out bytes.Buffer
	if err := hostInfo(context.Background(), nil, &out, &bytes.Buffer{}, listed(hostIfaces()[:1], nil), capture); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "00-11-22-33-44-55") {
		t.Errorf("table:\n%s", out.String())
	}
}

// When capture is not possible, the interfaces are still listed, the reason
// is shown, and the command fails.
func TestHostInfoReportsAnLLDPFailure(t *testing.T) {
	capture := func(context.Context, string, time.Duration, func(map[string]lldp.Neighbor) bool) (map[string]lldp.Neighbor, error) {
		return nil, lldp.ErrCaptureUnsupported
	}
	var out bytes.Buffer
	err := hostInfo(context.Background(), nil, &out, &bytes.Buffer{}, listed(hostIfaces(), nil), capture)
	if !errors.Is(err, lldp.ErrCaptureUnsupported) {
		t.Errorf("err = %v", err)
	}
	if !strings.Contains(out.String(), "eth1") || !strings.Contains(out.String(), "(LLDP unavailable)") ||
		!strings.Contains(out.String(), "not supported") {
		t.Errorf("table:\n%s", out.String())
	}
}

// Ctrl-C is not a failure: what was heard is printed and the exit is clean.
func TestHostInfoCancelledIsNotAFailure(t *testing.T) {
	capture := func(context.Context, string, time.Duration, func(map[string]lldp.Neighbor) bool) (map[string]lldp.Neighbor, error) {
		return map[string]lldp.Neighbor{"eth0": sw}, context.Canceled
	}
	if err := hostInfo(context.Background(), nil, &bytes.Buffer{}, &bytes.Buffer{}, listed(hostIfaces(), nil), capture); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestHostInfoInterfaceErrors(t *testing.T) {
	boom := errors.New("no such interface")
	never := func(context.Context, string, time.Duration, func(map[string]lldp.Neighbor) bool) (map[string]lldp.Neighbor, error) {
		t.Error("capture must not run")
		return nil, nil
	}
	if err := hostInfo(context.Background(), nil, &bytes.Buffer{}, &bytes.Buffer{}, listed(nil, boom), never); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
	// No interface at all: nothing to listen on, said plainly.
	var out bytes.Buffer
	if err := hostInfo(context.Background(), nil, &out, &bytes.Buffer{}, listed(nil, nil), never); err != nil {
		t.Errorf("err = %v", err)
	}
	if !strings.Contains(out.String(), "no interface is up") {
		t.Errorf("out = %q", out.String())
	}
}

func TestHostInfoBadFlag(t *testing.T) {
	if err := hostInfo(context.Background(), []string{"--nope"}, &bytes.Buffer{}, &bytes.Buffer{}, listed(nil, nil), nil); err == nil {
		t.Error("an unknown flag must be refused")
	}
}

func TestRunHostDispatch(t *testing.T) {
	if err := runHost(context.Background(), nil); err != nil {
		t.Errorf("no verb: %v", err)
	}
	if err := runHost(context.Background(), []string{"bogus"}); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("unknown verb: %v", err)
	}
}
