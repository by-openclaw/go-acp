package devicemodel

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/export"
)

func TestProductYAML_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "product.yaml")

	pm := &ProductMeta{
		Vendor:      "Axon",
		Product:     "Synapse",
		Model:       "RRS18",
		SwRev:       "1601",
		HwRev:       "0100",
		Description: "Reference reception card",
	}
	ip := &IdentityProbe{
		ACP1: &IdentityProbeACP1{
			Group: 1,
			Pids:  IdentityPids{Model: 0, SwRev: 3, HwRev: 4},
		},
		ACP2: &IdentityProbeACP2{
			Pids:        IdentityPids{Model: 1234, SwRev: 1235, HwRev: 1236},
			AliasesSeen: IdentityAliases{Model: "Card Type", SwRev: "Software Version", HwRev: "Hardware Revision"},
		},
	}
	wm := &WalkMetadata{
		WalkedAt:   time.Date(2026, 4, 22, 10, 30, 15, 0, time.UTC),
		WalkedBy:   "dhs/0.7.0",
		Controller: "Synapse Setup 4.2",
		Notes:      "RRS18 reception card",
	}
	supported := []string{"acp1", "acp2"}

	if err := SaveProductYAML(path, pm, ip, wm, supported); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, gotIP, gotWM, gotSP, err := LoadProductYAML(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if *got != *pm {
		t.Fatalf("ProductMeta roundtrip diverged:\n got %+v\nwant %+v", *got, *pm)
	}
	if gotIP.ACP1.Group != 1 || gotIP.ACP1.Pids.Model != 0 {
		t.Fatalf("ACP1 probe diverged: %+v", gotIP.ACP1)
	}
	if gotIP.ACP2.Pids.Model != 1234 || gotIP.ACP2.AliasesSeen.Model != "Card Type" {
		t.Fatalf("ACP2 probe diverged: %+v", gotIP.ACP2)
	}
	if !gotWM.WalkedAt.Equal(wm.WalkedAt) {
		t.Fatalf("WalkedAt diverged: got %v, want %v", gotWM.WalkedAt, wm.WalkedAt)
	}
	if len(gotSP) != 2 || gotSP[0] != "acp1" {
		t.Fatalf("supported_protocols diverged: %v", gotSP)
	}
}

func TestProductYAML_PartialOptional(t *testing.T) {
	// Only mandatory fields populated; everything else zero-valued.
	dir := t.TempDir()
	path := filepath.Join(dir, "product.yaml")

	pm := &ProductMeta{Model: "RRS18", SwRev: "1601"}
	if err := SaveProductYAML(path, pm, nil, nil, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, gotIP, gotWM, gotSP, err := LoadProductYAML(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Model != "RRS18" || got.SwRev != "1601" {
		t.Fatalf("ProductMeta lost: %+v", *got)
	}
	if gotIP.ACP1 != nil || gotIP.ACP2 != nil {
		t.Fatal("identity_probe should be empty")
	}
	if !gotWM.WalkedAt.IsZero() || gotWM.WalkedBy != "" {
		t.Fatalf("walk_metadata should be empty: %+v", *gotWM)
	}
	if len(gotSP) != 0 {
		t.Fatalf("supported_protocols should be empty: %v", gotSP)
	}
}

func TestProductYAML_RejectsMissingModel(t *testing.T) {
	if err := SaveProductYAML(filepath.Join(t.TempDir(), "p.yaml"), &ProductMeta{SwRev: "1601"}, nil, nil, nil); err == nil {
		t.Fatal("Save accepted missing Model")
	}
	if err := SaveProductYAML(filepath.Join(t.TempDir(), "p.yaml"), &ProductMeta{Model: "RRS18"}, nil, nil, nil); err == nil {
		t.Fatal("Save accepted missing SwRev")
	}
}

func TestProductYAML_VendorSanitised(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "product.yaml")

	pm := &ProductMeta{
		Vendor:  "Axon\r\nFW=evil",
		Product: "Synapse",
		Model:   "RRS18",
		SwRev:   "1601",
	}
	if err := SaveProductYAML(path, pm, nil, nil, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, _, _, _, err := LoadProductYAML(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// CRLF collapsed to space by identity.YAMLValue.
	if got.Vendor != "Axon FW=evil" {
		t.Fatalf("vendor not sanitised: %q", got.Vendor)
	}
}

func TestProductYAML_AbsentFile(t *testing.T) {
	_, _, _, _, err := LoadProductYAML(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil {
		t.Fatal("Load returned nil error for missing file")
	}
	if !os.IsNotExist(unwrapPathErr(err)) {
		t.Fatalf("err = %v, want os.ErrNotExist underneath", err)
	}
}

func TestResolve_LoadsProductMetadata(t *testing.T) {
	root := t.TempDir()
	r := New(root)

	// Build a schema with full metadata.
	s := &Schema{
		Fingerprint: Fingerprint{
			Vendor: "axon", Product: "synapse",
			Model: "RRS18", SwRev: "1601", Proto: "acp1",
		},
		Slots: map[int]*export.Snapshot{
			1: makeSnapshot("RRS18", []consumer.Object{
				{Slot: 1, Group: "control", ID: 0, Label: "Card Name", Kind: consumer.KindString},
			}),
		},
		Product: ProductMeta{
			Vendor: "Axon", Product: "Synapse",
			Model: "RRS18", SwRev: "1601", HwRev: "0100",
			Description: "Reception card",
		},
		Identity: IdentityProbe{
			ACP1: &IdentityProbeACP1{Group: 1, Pids: IdentityPids{Model: 0, SwRev: 3, HwRev: 4}},
		},
		Walk: WalkMetadata{
			WalkedBy:   "dhs/0.7.0",
			Controller: "Synapse Setup",
		},
		SupportedProtocols: []string{"acp1"},
	}
	if err := r.Persist(s); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	got, err := r.Resolve(s.Fingerprint)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Product.Vendor != "Axon" {
		t.Fatalf("Product.Vendor = %q, want Axon", got.Product.Vendor)
	}
	if got.Identity.ACP1 == nil || got.Identity.ACP1.Group != 1 {
		t.Fatalf("Identity.ACP1 = %+v, want group=1", got.Identity.ACP1)
	}
	if got.Walk.WalkedBy != "dhs/0.7.0" {
		t.Fatalf("Walk.WalkedBy = %q", got.Walk.WalkedBy)
	}
	if len(got.SupportedProtocols) != 1 || got.SupportedProtocols[0] != "acp1" {
		t.Fatalf("SupportedProtocols = %v", got.SupportedProtocols)
	}
}

func TestResolve_MissingProductYAML_StillResolves(t *testing.T) {
	// Schema without product.yaml should still resolve cleanly.
	r := New(fixture(t))
	got, err := r.Resolve(Fingerprint{
		Vendor: "axon", Product: "synapse",
		Model: "RRS18", SwRev: "1601", Proto: "acp1",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Product.Model != "" {
		t.Fatalf("Product should be zero-valued when product.yaml absent: %+v", got.Product)
	}
	if len(got.Slots) != 1 {
		t.Fatalf("slots = %d, want 1", len(got.Slots))
	}
}

func TestUnwrapPathErr_Identity(t *testing.T) {
	if unwrapPathErr(nil) != nil {
		t.Fatal("nil should unwrap to nil")
	}
	base := errors.New("plain")
	if unwrapPathErr(base) != base {
		t.Fatal("non-wrapped should return as-is")
	}
}
