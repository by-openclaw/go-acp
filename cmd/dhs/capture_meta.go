package main

// ADR-0028 capture self-description (#703 unit 3): line one of every
// --capture JSONL is a meta record carrying the exact CLI invocation
// (credentials REDACTED), the binary identity, and the wire context —
// an evidence file must explain itself forever.

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"dhs/internal/wiretrace"
)

// captureMeta builds the meta record for one capture session.
func captureMeta(proto, target, verb string) wiretrace.MetaRecord {
	return wiretrace.MetaRecord{
		CLI:           redactCLI(os.Args),
		BinaryVersion: version,
		BinarySHA256:  binarySHA256(),
		Proto:         proto,
		Target:        target,
		Verb:          verb,
		StartedUTC:    time.Now().UTC().Format(time.RFC3339),
	}
}

// redactCLI reproduces the invocation with secret-bearing flag values
// replaced by *** — both the "--pass value" and "--pass=value" forms
// (single-dash variants included). The capture already holds the
// LOGIN frame in cleartext (documented secret); the meta line must
// not add a second copy.
func redactCLI(args []string) string {
	secret := func(flag string) bool {
		f := strings.TrimLeft(flag, "-")
		return f == "pass" || f == "password" || f == "token"
	}
	out := make([]string, 0, len(args))
	skipNext := false
	for _, a := range args {
		if skipNext {
			out = append(out, "***")
			skipNext = false
			continue
		}
		if strings.HasPrefix(a, "-") {
			if eq := strings.IndexByte(a, '='); eq >= 0 {
				if secret(a[:eq]) {
					out = append(out, a[:eq]+"=***")
					continue
				}
			} else if secret(a) {
				out = append(out, a)
				skipNext = true
				continue
			}
		}
		out = append(out, a)
	}
	return strings.Join(out, " ")
}

// binarySHA256 hashes the running executable once per process — the
// same fingerprint printed on binary handovers, so a capture binds to
// the exact build that produced it.
var binarySHA256 = sync.OnceValue(func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	f, err := os.Open(exe)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
})
