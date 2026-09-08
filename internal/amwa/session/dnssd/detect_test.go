//go:build !linux

package dnssd

import "testing"

// TestTryDaemonStubs pins the non-Linux daemon-detection stubs: on every
// platform except Linux there is no Bonjour/Avahi bridge yet, so both
// probes MUST report "not available" so NewBrowser/NewResponder fall
// through to the stdlib implementation. (On Linux these symbols are
// provided by avahi_linux.go instead and are covered there.)
func TestTryDaemonStubs(t *testing.T) {
	if br, ok := tryDaemonBrowser(discardLogger()); ok || br != nil {
		t.Errorf("tryDaemonBrowser = (%v, %v), want (nil, false)", br, ok)
	}
	if rs, ok := tryDaemonResponder(discardLogger()); ok || rs != nil {
		t.Errorf("tryDaemonResponder = (%v, %v), want (nil, false)", rs, ok)
	}
}
