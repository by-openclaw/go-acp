package main

// `dhs producer ccm serve` — flag validation is an operator-sentence error
// caught before any file is read or socket bound; a valid dm-tree serves and
// stops cleanly on context cancel.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const ccmTestTree = `{"self":{"productName":"BRIDGE","productVersion":"7.0.2","modelVersion":3},"io/ip/senders/tx-1":{"uuid":"tx-1","name":"CAM 1"}}`

func writeCCMTree(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "dm-tree.json")
	if err := os.WriteFile(p, []byte(ccmTestTree), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// Help in place of a verb prints the catalogue and is not an error.
func TestCCMProducerHelpAndUnknownVerb(t *testing.T) {
	if err := runCCMProducer(context.Background(), nil); err != nil {
		t.Errorf("no verb must print help, got %v", err)
	}
	if err := runCCMProducer(context.Background(), []string{"-h"}); err != nil {
		t.Errorf("-h must print help, got %v", err)
	}
	if err := runCCMProducer(context.Background(), []string{"walk"}); err == nil || !strings.Contains(err.Error(), "unknown verb") {
		t.Errorf("unknown verb = %v, want the unknown-verb error", err)
	}
}

// --dm-tree is the one required flag: without a model there is nothing to
// replay, and the error says where the file comes from.
func TestCCMServeRequiresDMTree(t *testing.T) {
	err := runCCMServe(context.Background(), []string{"--bind", "127.0.0.1:0"})
	if err == nil || !strings.Contains(err.Error(), "--dm-tree") {
		t.Errorf("err = %v, want the --dm-tree required error", err)
	}
}

// A certificate without its key (or the reverse) cannot serve HTTPS; caught at
// flag time, before reading the tree.
func TestCCMServeTLSPairMismatch(t *testing.T) {
	for _, args := range [][]string{
		{"--dm-tree", "x.json", "--tls-cert", "a.crt"},
		{"--dm-tree", "x.json", "--tls-key", "a.key"},
	} {
		err := runCCMServe(context.Background(), args)
		if err == nil || !strings.Contains(err.Error(), "together") {
			t.Errorf("%v = %v, want the cert/key pairing error", args, err)
		}
	}
}

// A missing or malformed dm-tree is reported with the flag that named it.
func TestCCMServeBadTreeFile(t *testing.T) {
	if err := runCCMServe(context.Background(), []string{"--dm-tree", filepath.Join(t.TempDir(), "nope.json")}); err == nil || !strings.Contains(err.Error(), "--dm-tree") {
		t.Errorf("missing file = %v, want a read --dm-tree error", err)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(bad, []byte(`{`), 0o600)
	if err := runCCMServe(context.Background(), []string{"--dm-tree", bad}); err == nil || !strings.Contains(err.Error(), "dm-tree") {
		t.Errorf("malformed tree = %v, want a parse error", err)
	}
}

// A missing --api-spec file is an error rather than silently serving no spec.
func TestCCMServeBadSpecFile(t *testing.T) {
	err := runCCMServe(context.Background(), []string{
		"--dm-tree", writeCCMTree(t),
		"--api-spec", filepath.Join(t.TempDir(), "nope.yml"),
	})
	if err == nil || !strings.Contains(err.Error(), "--api-spec") {
		t.Errorf("err = %v, want a read --api-spec error", err)
	}
}

// A bad TLS certificate path fails when the server config is built, with the
// transport's own message, not at the first connection.
func TestCCMServeBadTLSFiles(t *testing.T) {
	err := runCCMServe(context.Background(), []string{
		"--dm-tree", writeCCMTree(t),
		"--tls-cert", filepath.Join(t.TempDir(), "no.crt"),
		"--tls-key", filepath.Join(t.TempDir(), "no.key"),
	})
	if err == nil {
		t.Fatal("unreadable TLS files must fail the serve")
	}
}

// The happy path: a valid tree (with a spec and a metrics endpoint) serves and
// returns nil once the context is cancelled.
func TestCCMServeRunsAndStopsOnCancel(t *testing.T) {
	spec := filepath.Join(t.TempDir(), "api.yml")
	_ = os.WriteFile(spec, []byte("openapi: '3.1.2'\n"), 0o600)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runCCMServe(ctx, []string{
			"--dm-tree", writeCCMTree(t),
			"--api-spec", spec,
			"--bind", "127.0.0.1:0",
			"--metrics-addr", "127.0.0.1:0",
		})
	}()
	time.Sleep(150 * time.Millisecond) // let it bind
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serve returned %v after cancel, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not return after cancel")
	}
}

// A missing --readme file is an error naming the flag, not a silent fallback
// to the embedded README the operator did not ask for.
func TestCCMServeBadReadmeFile(t *testing.T) {
	err := runCCMServe(context.Background(), []string{
		"--dm-tree", writeCCMTree(t),
		"--readme", filepath.Join(t.TempDir(), "nope.md"),
	})
	if err == nil || !strings.Contains(err.Error(), "--readme") {
		t.Errorf("err = %v, want a read --readme error", err)
	}
}
