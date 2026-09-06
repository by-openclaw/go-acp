// Package transport_test enforces the rule that closes the transport
// refactor: NO TRANSPORT CODE IN ANY PROTOCOL PACKAGE.
//
// Opening sockets, configuring TLS and setting socket options are
// transport-layer jobs. When a protocol package does them itself, three
// things follow, and all three have already happened in this repo:
//
//   - the behaviour diverges. Six of eight accept paths had no SO_KEEPALIVE;
//     four connectors built a *tls.Config each and three of them forgot
//     MinVersion.
//   - a bug has to be fixed N times, or gets fixed once and stays broken
//     everywhere else. The WSAEMSGSIZE split and the ignored ctx cancel were
//     both single fixes only because the code was in one place by then.
//   - the transport cannot be tested on its own, so it is exercised rather
//     than specified — which is how tcp.go sat at 23% and udp.go at 11%.
//
// A rule with no gate decays, so this is the gate. It complements the
// depguard rules in .golangci.yml the same way internal/amwa/dependencies_test.go
// does: depguard fails fast in the lint stage, this fails the test stage and
// runs even when golangci-lint is not installed.
//
// THE ALLOWLIST BELOW IS THE CLOSING LIST. Every entry is a known violation
// with the reason it is still there and what removes it. Shrinking it to
// empty finishes the refactor; adding to it requires saying why in the same
// breath.
package transport_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// socketOpeners are the calls that mean "this package opens its own pipe".
// net.Conn, net.Addr and friends are types, not transport ownership, so the
// check is on CALLS rather than on importing net at all — denying the net
// import outright would be unimplementable.
var socketOpeners = map[string]bool{
	"net.Dial":         true,
	"net.DialTimeout":  true,
	"net.DialIP":       true,
	"net.DialTCP":      true,
	"net.DialUDP":      true,
	"net.Listen":       true,
	"net.ListenPacket": true,
	"net.ListenTCP":    true,
	"net.ListenUDP":    true,
	"net.ListenIP":     true,
}

// socketTypes are constructed rather than called.
var socketTypes = map[string]bool{
	"net.Dialer":       true,
	"net.ListenConfig": true,
}

// allowed maps a repo-relative file to why it may still do transport work.
// Grouped by what removes it, so the list reads as a plan rather than as an
// apology. Two groups are permanent by design; the rest are debt.
var allowed = map[string]string{
	// PERMANENT — not a session pipe.
	//
	// A one-shot SYN reachability probe that connects and immediately
	// closes. Routing it through the shared dialer would apply keepalive to
	// a socket discarded microseconds later, and give the seam no user.
	"internal/acp1/consumer/session_health.go": "probeReachable: one-shot SYN test, not a session",
	"internal/acp2/consumer/session_health.go": "probeReachable: one-shot SYN test, not a session",
	// A broadcast socket built with a Control hook so SO_BROADCAST is set
	// before bind — a different kind of socket, using transport's own
	// SetSocketBroadcast helper.
	"internal/acp1/consumer/discover.go": "SO_BROADCAST discovery socket; uses transport.SetSocketBroadcast",

	// DEBT — provider accept paths still own their listener.
	//
	// They already apply the shared socket policy via ApplySocketOptions;
	// what remains is the bind itself moving to transport.ListenTCP, as the
	// osc and tsl consumers have already done.
	"internal/acp1/provider/server.go": "broadcast dial pins a source address; transport has no model for that",

	// DEBT — the HTTP server has not been extracted from amwa yet.
	//
	// Its Serve/TLS/CORS/preflight/mux half is generic; what is NMOS is the
	// IS-04 §4.4 error body and the BCP-003-02 gate. Extracting it also
	// closes the conformance gap where http has a client and no server.
	"internal/amwa/registry/mirror_serve.go": "TLS listener → transport once the server moves",

	// DEBT — certificate handling.
	//
	// certmgr ISSUES and renews certificates (BCP-003-03 EST), which is a
	// different job from choosing a TLS posture. It stays amwa-side, but the
	// tls.Config it hands out should come from transport.TLSOptions.
	"internal/amwa/session/certmgr/certmgr.go": "cert lifecycle is amwa's; the *tls.Config it emits should come from transport",

	// DEBT — DNS-SD runs its own multicast and unicast sockets.
	//
	// Discovery is a transport by the rule, but mDNS has requirements
	// (multicast group membership, per-interface binding) that transport
	// does not model yet.
	"internal/amwa/session/dnssd/avahi_linux.go": "mDNS socket; transport has no multicast model yet",
	"internal/amwa/session/dnssd/unicast.go":     "unicast DNS query socket; same",
}

func TestNoTransportCodeInProtocolPackages(t *testing.T) {
	root := repoRoot(t)

	var violations []string
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// internal/transport IS the transport.
			//
			// cmd/ is out of scope on purpose: the rule is about protocol
			// packages, and the CLI layer legitimately opens things that are
			// not protocol sessions — the syslog sink being the current
			// example.
			if strings.HasSuffix(filepath.ToSlash(path), "internal/transport") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel := filepath.ToSlash(mustRel(t, root, path))
		if _, ok := allowed[rel]; ok {
			return nil
		}
		for _, what := range transportUsesIn(t, path) {
			violations = append(violations, rel+": "+what)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	for _, v := range violations {
		t.Errorf("transport code in a protocol package — %s\n"+
			"    open sockets and configure TLS through internal/transport.\n"+
			"    If this one genuinely cannot, add it to `allowed` with the reason.", v)
	}
}

// An allowlist that is never pruned stops being a plan and becomes a place
// things go to be forgotten. This fails when an entry no longer violates
// anything, so finishing a migration forces the entry out with it — and the
// list can only ever shrink toward empty.
func TestAllowlistHasNoStaleEntries(t *testing.T) {
	root := repoRoot(t)
	for rel, why := range allowed {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(path); err != nil {
			t.Errorf("allowlist names %s, which no longer exists — remove the entry (%s)", rel, why)
			continue
		}
		if uses := transportUsesIn(t, path); len(uses) == 0 {
			t.Errorf("allowlist entry %s is STALE — it no longer does transport work.\n"+
				"    Delete the entry; the refactor got one file closer to done. (%s)", rel, why)
		}
	}
}

// transportUsesIn reports the transport-level things a file does.
func transportUsesIn(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly|parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var out []string
	for _, imp := range f.Imports {
		if imp.Path != nil && imp.Path.Value == `"crypto/tls"` {
			out = append(out, "imports crypto/tls — TLS posture belongs to transport.TLSOptions")
		}
	}

	full, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	seen := map[string]bool{}
	ast.Inspect(full, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		name := ident.Name + "." + sel.Sel.Name
		switch {
		case socketOpeners[name] && !seen[name]:
			seen[name] = true
			out = append(out, "calls "+name)
		case socketTypes[name] && !seen[name]:
			seen[name] = true
			out = append(out, "builds a "+name)
		case sel.Sel.Name == "SetKeepAlive" && !seen[name]:
			seen[name] = true
			out = append(out, "sets SO_KEEPALIVE directly — use transport.ApplySocketOptions")
		}
		return true
	})
	return out
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// This file lives at internal/transport/, so the module root is two up.
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func mustRel(t *testing.T, base, path string) string {
	t.Helper()
	rel, err := filepath.Rel(base, path)
	if err != nil {
		t.Fatalf("rel: %v", err)
	}
	return rel
}
