package main

// DHS-MIB is committed so an operator can load it without building dhs,
// and generated so it cannot describe an agent that no longer exists. This
// test is what holds the two together: the committed file must be exactly
// what `dhs producer snmp mib` writes today.
//
// Regenerate deliberately (and review the diff) with:
//
//	go test ./cmd/dhs -run TestDHSMIBIsCurrent -update-dhs-mib
//
// A diff that adds or changes a definition also needs a new REVISION in
// internal/snmp/provider/dhsmib.go — managers compare LAST-UPDATED.

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateDHSMIB = flag.Bool("update-dhs-mib", false,
	"regenerate internal/snmp/mib/DHS-MIB.mib from `dhs producer snmp mib`")

var committedDHSMIB = filepath.Join("..", "..", "internal", "snmp", "mib", "DHS-MIB.mib")

func TestDHSMIBIsCurrent(t *testing.T) {
	out := filepath.Join(t.TempDir(), "DHS-MIB.mib")
	if err := runSNMPMIB(context.Background(), []string{"--out", out}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if *updateDHSMIB {
		if err := os.WriteFile(committedDHSMIB, got, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(committedDHSMIB)
	if err != nil {
		t.Fatalf("%v\ngenerate it with: go test ./cmd/dhs -run TestDHSMIBIsCurrent -update-dhs-mib", err)
	}
	// A Windows checkout may have turned the committed file's line endings.
	if strings.ReplaceAll(string(want), "\r\n", "\n") != string(got) {
		t.Errorf("%s is not what `dhs producer snmp mib` writes; regenerate with:\n"+
			"  go test ./cmd/dhs -run TestDHSMIBIsCurrent -update-dhs-mib", committedDHSMIB)
	}
}

func TestSNMPMIBRefusesAnUnknownFlag(t *testing.T) {
	if err := runSNMPMIB(context.Background(), []string{"--nonsense"}); err == nil {
		t.Error("an unknown flag must be refused")
	}
}
