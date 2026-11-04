package manifest

import (
	"os"
	"testing"
)

func writeM(t *testing.T, dir string, m *Manifest) {
	t.Helper()
	m.Frames = []Frame{{Name: "frame", Slots: []Slot{{Addr: map[string]any{"slot": 0}, DM: "X@1.json"}}}}
	if _, err := Write(dir, m); err != nil {
		t.Fatal(err)
	}
}

func TestPrimaryOfAndByUnit(t *testing.T) {
	dir := t.TempDir()
	ep := func(ips ...string) []Endpoint {
		var out []Endpoint
		for _, ip := range ips {
			out = append(out, Endpoint{IP: ip, Port: 2072, Transport: "tcp"})
		}
		return out
	}
	// a frame with three controllers
	writeM(t, dir, &Manifest{Device: Device{Name: "frame", Protocol: "acp2", IP: "10.0.0.1", Endpoints: ep("10.0.0.1", "10.0.0.2", "10.0.0.3"), Unit: map[string]string{"serial": "S1"}}})
	// a device behind a control plane: its endpoint is the server, not itself
	writeM(t, dir, &Manifest{Device: Device{Name: "dev", Protocol: "acp2", IP: "10.0.0.9", Endpoints: ep("10.0.0.2")}})

	for ip, want := range map[string]string{"10.0.0.1": "10.0.0.1", "10.0.0.2": "10.0.0.1", "10.0.0.3": "10.0.0.1"} {
		if got, found, _ := PrimaryOf(dir, "acp2", ip); !found || got != want {
			t.Errorf("PrimaryOf(%s) = %q %v", ip, got, found)
		}
	}
	if _, found, _ := PrimaryOf(dir, "acp2", "10.0.0.7"); found {
		t.Error("an unknown IP found")
	}
	if _, found, _ := PrimaryOf(dir, "acp1", "10.0.0.2"); found {
		t.Error("another protocol's manifest used")
	}

	// a second device claiming controller 10.0.0.3: a conflict, never a pick
	writeM(t, dir, &Manifest{Device: Device{Name: "other", Protocol: "acp2", IP: "10.0.0.5", Endpoints: ep("10.0.0.5", "10.0.0.3")}})
	if _, found, conflict := PrimaryOf(dir, "acp2", "10.0.0.3"); found || len(conflict) != 2 {
		t.Errorf("conflict %v found %v", conflict, found)
	}

	if got := ByUnit(dir, "acp2", "serial", "S1", "10.0.0.4"); got != "10.0.0.1" {
		t.Errorf("ByUnit = %q", got)
	}
	if ByUnit(dir, "acp2", "serial", "S1", "10.0.0.1") != "" || ByUnit(dir, "acp2", "serial", "", "x") != "" || ByUnit(dir, "acp2", "serial", "S9", "x") != "" {
		t.Error("self, an empty serial or an unknown one matched")
	}
	writeM(t, dir, &Manifest{Device: Device{Name: "twin", Protocol: "acp2", IP: "10.0.0.6", Endpoints: ep("10.0.0.6"), Unit: map[string]string{"serial": "S1"}}})
	if ByUnit(dir, "acp2", "serial", "S1", "10.0.0.4") != "" {
		t.Error("a serial two devices claim matched one of them")
	}

	Remove(dir, "acp2", "10.0.0.6")
	if _, err := os.Stat(PathFor(dir, &Manifest{Device: Device{Protocol: "acp2", IP: "10.0.0.6"}})); err == nil {
		t.Error("Remove left the file")
	}
	if err := os.WriteFile(PathFor(dir, &Manifest{Device: Device{Protocol: "acp2", IP: "bad"}}), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, found, _ := PrimaryOf(dir, "acp2", "10.0.0.1"); !found || got != "10.0.0.1" {
		t.Error("an unreadable manifest broke the scan")
	}
}
