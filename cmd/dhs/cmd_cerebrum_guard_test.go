package main

import (
	"context"
	"strings"
	"testing"
)

// cerebrumWriteVerbs is one valid call of every Northbound verb that
// writes, on a host nothing listens on: the guard must answer before the
// dial, so none of these ever reaches the network.
var cerebrumWriteVerbs = [][]string{
	{"route", "127.0.0.1:1", "--dest", "1", "--srce", "1", "--level", "1"},
	{"replace", "127.0.0.1:1", "--srce", "1", "--with", "2"},
	{"import", "127.0.0.1:1", "--xpoint", "x.csv"},
	{"lock", "127.0.0.1:1", "--dest", "1"},
	{"unlock", "127.0.0.1:1", "--dest", "1"},
	{"set-mnemonic", "127.0.0.1:1", "--kind", "DEST_MNE", "--dest", "1", "--level", "1", "--mnemonic", "X"},
	{"set-tags", "127.0.0.1:1", "--kind", "RM_DEST_TAGS", "--dest", "1", "--level", "1", "--tags", "a"},
	{"salvo", "127.0.0.1:1", "--op", "run", "--group", "G"},
	{"set-value", "127.0.0.1:1", "--device", "D", "--sub-device", "0", "--object", "O", "--value", "1"},
	{"device-config", "remove", "127.0.0.1:1", "--device-type", "generic", "--ip", "1.2.3.4"},
	{"assoc", "127.0.0.1:1", "--kind", "SRCE_ASSOC", "--srce", "1", "--level", "1", "--target-device", "R", "--target-level", "1", "--target-io", "2"},
	{"rm-add", "127.0.0.1:1", "--kind", "source"},
	{"rm-delete", "127.0.0.1:1", "--kind", "source", "--id", "21"},
	{"category", "127.0.0.1:1", "--op", "delete", "--category", "C"},
}

func TestCerebrumWriteVerbsNeedWriteTo(t *testing.T) {
	for _, args := range cerebrumWriteVerbs {
		t.Run(args[0], func(t *testing.T) {
			err := runCerebrum(context.Background(), args)
			if err == nil || !strings.Contains(err.Error(), "add --write-to 127.0.0.1:1") {
				t.Fatalf("without --write-to: want the guard refusal, got %v", err)
			}
			err = runCerebrum(context.Background(), append(args, "--write-to", "10.0.0.9"))
			if err == nil || !strings.Contains(err.Error(), "--write-to 10.0.0.9 does not match the host 127.0.0.1:1") {
				t.Fatalf("with another host: want the mismatch refusal, got %v", err)
			}
			// A dry run naming another host is refused too: the operator
			// confirmed another target.
			err = runCerebrum(context.Background(), append(args, "--dry-run", "--write-to", "10.0.0.9"))
			if err == nil || !strings.Contains(err.Error(), "does not match the host") {
				t.Fatalf("dry run with another host: want the mismatch refusal, got %v", err)
			}
		})
	}
}

// The verbs that read nothing before they write answer a dry run without
// dialing; the others dial to read, which this host refuses, so the guard
// let them through.
func TestCerebrumWriteVerbsDryRunPassesGuard(t *testing.T) {
	noRead := map[string]bool{"assoc": true, "rm-add": true, "rm-delete": true, "category": true}
	for _, args := range cerebrumWriteVerbs {
		t.Run(args[0], func(t *testing.T) {
			err := runCerebrum(context.Background(), append(args, "--dry-run", "--timeout", "200ms"))
			if noRead[args[0]] {
				if err != nil {
					t.Fatalf("dry run: want nothing sent and no error, got %v", err)
				}
				return
			}
			if err == nil || strings.Contains(err.Error(), "write-to") {
				t.Fatalf("dry run: want the read to fail on the dead host, not the guard; got %v", err)
			}
		})
	}
}
