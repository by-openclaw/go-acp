package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// validManifest returns a minimal manifest that passes validate, so each
// test can mutate exactly one field to exercise a single failure arm.
func validManifest() *Manifest {
	return &Manifest{
		Device: Device{
			Name:     "neuron-test",
			Protocol: "acp2",
			Endpoints: []Endpoint{
				{IP: "10.100.0.103", Port: 2072, Transport: "tcp"},
			},
		},
		Frames: []Frame{
			{Name: "chassis", Slots: []Slot{
				{Addr: map[string]any{"slot": 0}, DM: "SHPRM1@5.3.5"},
			}},
		},
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(m *Manifest)
		wantErr bool
	}{
		{"ok single tcp", func(*Manifest) {}, false},
		{"ok single udp", func(m *Manifest) {
			m.Device.Endpoints[0].Transport = "udp"
		}, false},
		{"ok redundant tcp", func(m *Manifest) {
			m.Device.Endpoints = append(m.Device.Endpoints,
				Endpoint{IP: "10.100.0.109", Port: 2072, Transport: "tcp"})
		}, false},
		{"empty name", func(m *Manifest) { m.Device.Name = "" }, true},
		{"empty protocol", func(m *Manifest) { m.Device.Protocol = "" }, true},
		{"no endpoints", func(m *Manifest) { m.Device.Endpoints = nil }, true},
		{"empty ip", func(m *Manifest) { m.Device.Endpoints[0].IP = "" }, true},
		{"port zero", func(m *Manifest) { m.Device.Endpoints[0].Port = 0 }, true},
		{"port too high", func(m *Manifest) { m.Device.Endpoints[0].Port = 70000 }, true},
		{"bad transport", func(m *Manifest) { m.Device.Endpoints[0].Transport = "sctp" }, true},
		// The redundant-controller transport rule: udp is only valid as
		// the sole endpoint; a second endpoint forces all-tcp.
		{"udp first of two", func(m *Manifest) {
			m.Device.Endpoints[0].Transport = "udp"
			m.Device.Endpoints = append(m.Device.Endpoints,
				Endpoint{IP: "10.100.0.109", Port: 2072, Transport: "tcp"})
		}, true},
		{"udp second of two", func(m *Manifest) {
			m.Device.Endpoints = append(m.Device.Endpoints,
				Endpoint{IP: "10.100.0.109", Port: 2072, Transport: "udp"})
		}, true},
		{"no frames", func(m *Manifest) { m.Frames = nil }, true},
		{"empty slots", func(m *Manifest) { m.Frames[0].Slots = nil }, true},
		{"empty dm", func(m *Manifest) { m.Frames[0].Slots[0].DM = "" }, true},
		{"dm missing at", func(m *Manifest) { m.Frames[0].Slots[0].DM = "SHPRM1" }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := validManifest()
			tc.mutate(m)
			err := m.validate()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	t.Run("read error", func(t *testing.T) {
		if _, err := Load(filepath.Join(t.TempDir(), "nope.json")); err == nil {
			t.Fatal("expected read error")
		}
	})
	t.Run("parse error", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "bad.json")
		if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Fatal("expected parse error")
		}
	})
	t.Run("validate error", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "invalid.json")
		if err := os.WriteFile(p, []byte(`{"device":{"name":""}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Fatal("expected validate error")
		}
	})
	t.Run("success", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "ok.json")
		b, _ := json.Marshal(validManifest())
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		m, err := Load(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.Device.Name != "neuron-test" || len(m.Frames) != 1 {
			t.Fatalf("round-trip mismatch: %+v", m.Device)
		}
	})
}

// TestSlotProtos pins the per-slot GetSlotInfo advertisement map:
// declared lists keyed by numeric slot, undeclared slots absent, and
// entries with a non-numeric or out-of-range slot addr skipped.
func TestSlotProtos(t *testing.T) {
	m := &Manifest{Frames: []Frame{{
		Name: "chassis",
		Slots: []Slot{
			{Addr: map[string]any{"slot": 0}, DM: "A@1", Protos: []uint8{2, 3, 4}},
			{Addr: map[string]any{"slot": 1}, DM: "B@1"},                             // no override
			{Addr: map[string]any{"oid": "1.4"}, DM: "C@1", Protos: []uint8{2}},      // non-numeric addr
			{Addr: map[string]any{"slot": 999}, DM: "D@1", Protos: []uint8{2}},       // out of range
			{Addr: map[string]any{"slot": float64(2)}, DM: "E@1", Protos: []uint8{2, 3}},
		},
	}}}
	got := m.SlotProtos()
	if len(got) != 2 {
		t.Fatalf("SlotProtos = %v, want 2 entries", got)
	}
	if fmt.Sprintf("%v", got[0]) != "[2 3 4]" || fmt.Sprintf("%v", got[2]) != "[2 3]" {
		t.Fatalf("SlotProtos = %v", got)
	}
}

func TestDMPath(t *testing.T) {
	if got, want := DMPath(".cache", "acp2", "SHPRM1@5.3.5"),
		filepath.Join(".cache", "dm", "acp2", "SHPRM1@5.3.5.json"); got != want {
		t.Fatalf("DMPath = %q, want %q", got, want)
	}
	// Already-suffixed reference must not double the extension.
	if got, want := DMPath(".cache", "acp1", "CARD@1.0.json"),
		filepath.Join(".cache", "dm", "acp1", "CARD@1.0.json"); got != want {
		t.Fatalf("DMPath suffixed = %q, want %q", got, want)
	}
}

func TestWrite(t *testing.T) {
	t.Run("nil manifest", func(t *testing.T) {
		if _, err := Write(t.TempDir(), nil); err == nil {
			t.Fatal("expected error for nil manifest")
		}
	})
	t.Run("empty name", func(t *testing.T) {
		if _, err := Write(t.TempDir(), &Manifest{}); err == nil {
			t.Fatal("expected error for empty device name")
		}
	})
	t.Run("empty protocol", func(t *testing.T) {
		m := validManifest()
		m.Device.Protocol = ""
		if _, err := Write(t.TempDir(), m); err == nil {
			t.Fatal("expected error for empty protocol (ADR-0028 key)")
		}
	})
	t.Run("ip key (ADR-0028)", func(t *testing.T) {
		cache := t.TempDir()
		m := validManifest()
		m.Device.IP = "10.100.0.103"
		m.Device.FQDN = "neuron-test.plant.example"
		path, err := Write(cache, m)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(cache, "manifest", "acp2", "10.100.0.103.json")
		if path != want {
			t.Fatalf("path = %q, want %q", path, want)
		}
		got, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if got.Device.IP != "10.100.0.103" || got.Device.FQDN != "neuron-test.plant.example" {
			t.Fatalf("ip/fqdn round-trip: %+v", got.Device)
		}
	})
	t.Run("name-slug fallback when no IP", func(t *testing.T) {
		cache := t.TempDir()
		m := validManifest()
		m.Device.Name = "Tiny Ember+ Router"
		path, err := Write(cache, m)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(cache, "manifest", "acp2", "tiny-ember-router.json")
		if path != want {
			t.Fatalf("path = %q, want %q", path, want)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("file not written: %v", err)
		}
	})
	t.Run("legacy name-keyed manifest migrated", func(t *testing.T) {
		cache := t.TempDir()
		// Old layout: manifest/<name-slug>.json with a different endpoint.
		legacyDir := filepath.Join(cache, "manifest")
		if err := os.MkdirAll(legacyDir, 0o755); err != nil {
			t.Fatal(err)
		}
		legacy := validManifest()
		legacy.Device.Endpoints = []Endpoint{{IP: "10.100.0.109", Port: 2072, Transport: "tcp"}}
		b, _ := json.Marshal(legacy)
		legacyPath := filepath.Join(legacyDir, "neuron-test.json")
		if err := os.WriteFile(legacyPath, b, 0o644); err != nil {
			t.Fatal(err)
		}
		m := validManifest()
		m.Device.IP = "10.100.0.103"
		path, err := Write(cache, m)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Device.Endpoints) != 2 {
			t.Fatalf("legacy endpoints not merged: %+v", got.Device.Endpoints)
		}
		if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
			t.Fatalf("legacy file not retired: %v", err)
		}
	})
	t.Run("merge unions endpoints", func(t *testing.T) {
		cache := t.TempDir()
		m1 := validManifest()
		if _, err := Write(cache, m1); err != nil {
			t.Fatal(err)
		}
		m2 := validManifest()
		m2.Device.Endpoints = []Endpoint{
			{IP: "10.100.0.109", Port: 2072, Transport: "tcp"},
		}
		path, err := Write(cache, m2)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Device.Endpoints) != 2 {
			t.Fatalf("endpoints = %d, want 2 (merged)", len(got.Device.Endpoints))
		}
	})
	t.Run("mkdir error", func(t *testing.T) {
		// cacheDir is a regular file → joining "manifest" under it
		// can't be created.
		f := filepath.Join(t.TempDir(), "afile")
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Write(f, validManifest()); err == nil {
			t.Fatal("expected mkdir error")
		}
	})
	t.Run("create error", func(t *testing.T) {
		cache := t.TempDir()
		dir := filepath.Join(cache, "manifest", "acp2")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		// Obstruct the tmp path with a directory so os.Create fails.
		if err := os.Mkdir(filepath.Join(dir, "neuron-test.json.tmp"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := Write(cache, validManifest()); err == nil {
			t.Fatal("expected create error")
		}
	})
	t.Run("rename error", func(t *testing.T) {
		cache := t.TempDir()
		dir := filepath.Join(cache, "manifest", "acp2")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		// Obstruct the destination with a directory so os.Rename fails.
		if err := os.Mkdir(filepath.Join(dir, "neuron-test.json"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := Write(cache, validManifest()); err == nil {
			t.Fatal("expected rename error")
		}
	})
	t.Run("encode error", func(t *testing.T) {
		orig := encodeManifest
		encodeManifest = func(*json.Encoder, *Manifest) error { return errors.New("boom") }
		defer func() { encodeManifest = orig }()
		if _, err := Write(t.TempDir(), validManifest()); err == nil {
			t.Fatal("expected encode error")
		}
	})
	t.Run("close error", func(t *testing.T) {
		orig := closeFile
		closeFile = func(f *os.File) error { _ = f.Close(); return errors.New("boom") }
		defer func() { closeFile = orig }()
		if _, err := Write(t.TempDir(), validManifest()); err == nil {
			t.Fatal("expected close error")
		}
	})
}

func TestMergeEndpoints(t *testing.T) {
	prior := []Endpoint{
		{IP: "10.100.0.103", Port: 2072, Transport: "tcp"},
		{IP: "10.100.0.103", Port: 2072, Transport: "tcp"}, // dup within prior
	}
	incoming := []Endpoint{
		{IP: "10.100.0.103", Port: 2072, Transport: "tcp"}, // dup of prior
		{IP: "10.100.0.109", Port: 2072, Transport: "tcp"}, // new
	}
	got := mergeEndpoints(prior, incoming)
	if len(got) != 2 {
		t.Fatalf("merged = %d, want 2: %+v", len(got), got)
	}
	if got[0].IP != "10.100.0.103" || got[1].IP != "10.100.0.109" {
		t.Fatalf("order/union wrong: %+v", got)
	}
}

func TestSlugifyDeviceName(t *testing.T) {
	tests := map[string]string{
		"Tiny Ember+ Router": "tiny-ember-router",
		"  +Lead Trail+  ":   "lead-trail",
		"ALLCAPS":            "allcaps",
		"dots.kept.1":        "dots.kept.1",
	}
	for in, want := range tests {
		if got := slugifyDeviceName(in); got != want {
			t.Fatalf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
