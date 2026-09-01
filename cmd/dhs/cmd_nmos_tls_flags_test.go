package main

// Repeatable TLS flag parsing (issue #948, dual-certificate serving):
// --tls-cert/--tls-key (and the mirror's --serve-tls-cert/-key) may be
// given once per pair; a cert/key count mismatch is an operator-
// sentence error caught right after flag parsing — before any config
// is loaded or socket bound.

import (
	"context"
	"strings"
	"testing"
)

func TestNodeServeTLSFlagCountMismatch(t *testing.T) {
	err := runNMOSNodeServe(context.Background(), []string{
		"--tls-cert", "rsa.crt",
		"--tls-cert", "ecdsa.crt",
		"--tls-key", "rsa.key",
	})
	if err == nil || !strings.Contains(err.Error(), "--tls-key") {
		t.Errorf("node serve mismatch = %v, want the cert/key count error", err)
	}
}

func TestRegistryServeTLSFlagCountMismatch(t *testing.T) {
	err := runNMOSRegistryServe(context.Background(), []string{
		"--tls-cert", "rsa.crt",
		"--tls-key", "rsa.key",
		"--tls-key", "ecdsa.key",
	})
	if err == nil || !strings.Contains(err.Error(), "--tls-key") {
		t.Errorf("registry serve mismatch = %v, want the cert/key count error", err)
	}
}

func TestMirrorServeTLSFlagCountMismatch(t *testing.T) {
	err := runNMOSRegistryMirror(context.Background(), []string{
		"--source", "http://s:1", "--target", "http://t:2", "--serve", ":0",
		"--serve-tls-cert", "rsa.crt",
		"--serve-tls-cert", "ecdsa.crt",
		"--serve-tls-key", "rsa.key",
	})
	if err == nil || !strings.Contains(err.Error(), "--serve-tls-key") {
		t.Errorf("mirror mismatch = %v, want the cert/key count error", err)
	}
}
