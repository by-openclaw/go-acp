package dnssd

import (
	"context"
	"log/slog"
	"testing"

	codecdnssd "dhs/internal/amwa/codec/dnssd"
)

// fakeDaemonBrowser / fakeDaemonResponder stand in for a real system-daemon
// backend so NewBrowser / NewResponder take their daemon-success return.
// That return is dead on every build whose daemon probe is the no-daemon
// stub (everything but Linux-with-avahi), so it is driven through the
// tryDaemon*Fn seam rather than left uncovered.
type fakeDaemonBrowser struct{}

func (fakeDaemonBrowser) Browse(context.Context, string) (<-chan codecdnssd.Instance, error) {
	return nil, nil
}
func (fakeDaemonBrowser) Close() error { return nil }

type fakeDaemonResponder struct{}

func (fakeDaemonResponder) Announce(context.Context, codecdnssd.Instance) error { return nil }
func (fakeDaemonResponder) Update(context.Context, codecdnssd.Instance) error   { return nil }
func (fakeDaemonResponder) Close() error                                        { return nil }

func TestNewBrowserPrefersTheDaemonBackend(t *testing.T) {
	orig := tryDaemonBrowserFn
	tryDaemonBrowserFn = func(*slog.Logger) (Browser, bool) { return fakeDaemonBrowser{}, true }
	defer func() { tryDaemonBrowserFn = orig }()

	br, err := NewBrowser(nil) // nil logger also exercises the default-logger arm
	if err != nil {
		t.Fatalf("NewBrowser: %v", err)
	}
	if _, ok := br.(fakeDaemonBrowser); !ok {
		t.Errorf("NewBrowser returned %T, want the daemon browser when the probe succeeds", br)
	}
}

func TestNewResponderPrefersTheDaemonBackend(t *testing.T) {
	orig := tryDaemonResponderFn
	tryDaemonResponderFn = func(*slog.Logger) (Responder, bool) { return fakeDaemonResponder{}, true }
	defer func() { tryDaemonResponderFn = orig }()

	rs, err := NewResponder(nil)
	if err != nil {
		t.Fatalf("NewResponder: %v", err)
	}
	if _, ok := rs.(fakeDaemonResponder); !ok {
		t.Errorf("NewResponder returned %T, want the daemon responder when the probe succeeds", rs)
	}
}

// noDaemon makes the constructors take the stdlib path for one test,
// whatever this host happens to be running. Without it a test about the
// stdlib backend passes or fails according to whether avahi-daemon is
// installed on the machine running it, which is not a property of the
// code under test.
func noDaemon(t *testing.T) {
	t.Helper()
	prevB, prevR := tryDaemonBrowserFn, tryDaemonResponderFn
	tryDaemonBrowserFn = func(*slog.Logger) (Browser, bool) { return nil, false }
	tryDaemonResponderFn = func(*slog.Logger) (Responder, bool) { return nil, false }
	t.Cleanup(func() { tryDaemonBrowserFn, tryDaemonResponderFn = prevB, prevR })
}
