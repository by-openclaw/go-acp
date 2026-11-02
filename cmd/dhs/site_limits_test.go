package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/consumer"
)

// ADR-0037 P7: the site limits refuse before anything is sent, on the
// generic set and on the rrcs-own import alike, naming both ranges.
func TestSiteLimitsOnSetAndImport(t *testing.T) {
	f, changes := rrcsSetFake(t, true)
	dir := t.TempDir()
	limits := filepath.Join(dir, "site-limits.csv")
	_ = os.WriteFile(limits, []byte("path,min,max,allowed\n.PortAes67Output.Channels,1,8,\n.PortAes67Output.Protocol,,,2|5\n"), 0o644)
	ctx := context.Background()
	var val *consumer.ValidationError

	// The generic set: 12 channels where the site allows 8 and the
	// factory 16.
	_, err := rrcsStdout(t, func() error {
		return runSet(ctx, []string{f.addr(), "--protocol", "rrcs", "--path", "net.1.node.61.port.7.out.PortAes67Output.Channels",
			"--value", "12", "--write-to", f.addr(), "--site-limits", limits})
	})
	if !errors.As(err, &val) || !strings.Contains(err.Error(), "refused by the site limit (site 1..8, factory 1..16)") {
		t.Fatalf("set above the site limit: %v", err)
	}
	// A protocol the site does not allow (RTSP = 3).
	_, err = rrcsStdout(t, func() error {
		return runSet(ctx, []string{f.addr(), "--protocol", "rrcs", "--path", "net.1.node.61.port.7.out.PortAes67Output.Protocol",
			"--value", "3", "--write-to", f.addr(), "--site-limits", limits})
	})
	if !errors.As(err, &val) || !strings.Contains(err.Error(), "allowed 2|5") {
		t.Fatalf("set outside the allowed values: %v", err)
	}
	// Within the site limit the dry run passes.
	out, err := rrcsStdout(t, func() error {
		return runSet(ctx, []string{f.addr(), "--protocol", "rrcs", "--path", "net.1.node.61.port.7.out.PortAes67Output.Channels",
			"--value", "8", "--dry-run", "--site-limits", limits})
	})
	if err != nil || !strings.Contains(out, "would set") {
		t.Fatalf("set within the site limit: %v\n%s", err, out)
	}
	// --site-limits off: the factory alone judges.
	out, err = rrcsStdout(t, func() error {
		return runSet(ctx, []string{f.addr(), "--protocol", "rrcs", "--path", "net.1.node.61.port.7.out.PortAes67Output.Channels",
			"--value", "12", "--dry-run", "--site-limits", "off"})
	})
	if err != nil || !strings.Contains(out, "would set") {
		t.Fatalf("set with the limits off: %v\n%s", err, out)
	}
	if len(*changes) != 0 {
		t.Fatalf("%d changes sent", len(*changes))
	}

	// The rrcs-own import: the refused row fails, the others go on.
	file := filepath.Join(dir, "rows.csv")
	_ = os.WriteFile(file, []byte("path,value\n"+
		"net.1.node.61.port.7.out.PortAes67Output.Channels,12\n"+
		"net.1.node.61.port.7.out.PortAes67Output.Multicast,239.9.9.9\n"), 0o644)
	out, err = rrcsStdout(t, func() error {
		return runRRCS(ctx, []string{"import", f.addr(), "--file", file, "--dry-run", "--site-limits", limits})
	})
	if err == nil || !strings.Contains(out, "refused by the site limit (site 1..8, factory 1..16)") || !strings.Contains(out, "would apply 1, unchanged 0, skipped 0, failed 1") {
		t.Fatalf("import with a refused row: %v\n%s", err, out)
	}
	// A bad limits file stops the run before anything is read or sent.
	bad := filepath.Join(dir, "bad.csv")
	_ = os.WriteFile(bad, []byte("path,min,max,allowed\n.X,9,1,\n"), 0o644)
	if err := runRRCS(ctx, []string{"import", f.addr(), "--file", file, "--dry-run", "--site-limits", bad}); !errors.As(err, &val) {
		t.Fatalf("bad limits file: %v", err)
	}
}
