package provider

// Shared fixtures for the coverage pass: a log tap that turns the
// Node's own log lines into a synchronisation channel (every branch
// that matters says something, so a test can WAIT for the line rather
// than sleep), a scripted DNS-SD browser and responder (the real ones
// join 224.0.0.251 — never in a unit test), and the seam swappers that
// restore the production constructors on cleanup.

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	dnssdcodec "dhs/internal/amwa/codec/dnssd"
	dnssdsession "dhs/internal/amwa/session/dnssd"
)

// logTap is a slog.Handler that records every message and offers each
// one on a channel, so a test can block until the code under test has
// reached the line that logs it.
type logTap struct {
	ch  chan string
	mu  sync.Mutex
	all []string
}

func newLogTap() *logTap { return &logTap{ch: make(chan string, 4096)} }

func (l *logTap) logger() *slog.Logger { return slog.New(l) }

func (l *logTap) Enabled(context.Context, slog.Level) bool { return true }

func (l *logTap) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	l.all = append(l.all, r.Message)
	l.mu.Unlock()
	select {
	case l.ch <- r.Message:
	default:
	}
	return nil
}

func (l *logTap) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *logTap) WithGroup(string) slog.Handler      { return l }

// wait blocks until a message containing substr has been logged (or
// fails the test after a generous bound — this is a liveness check,
// never a timing assertion).
func (l *logTap) wait(t *testing.T, substr string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case m := <-l.ch:
			if strings.Contains(m, substr) {
				return
			}
		case <-deadline:
			t.Fatalf("log line containing %q never appeared; seen:\n  %s", substr, strings.Join(l.snapshot(), "\n  "))
		}
	}
}

func (l *logTap) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.all...)
}

func (l *logTap) has(substr string) bool {
	for _, m := range l.snapshot() {
		if strings.Contains(m, substr) {
			return true
		}
	}
	return false
}

// fakeBrowser is a scripted dnssdsession.Browser: each Browse hands
// back a channel the test feeds; a service listed in browseErr fails
// to open.
type fakeBrowser struct {
	mu        sync.Mutex
	chans     map[string]chan dnssdcodec.Instance
	browseErr map[string]error
	closed    int
	closeErr  error
}

func newFakeBrowser() *fakeBrowser {
	return &fakeBrowser{chans: map[string]chan dnssdcodec.Instance{}, browseErr: map[string]error{}}
}

func (b *fakeBrowser) Browse(_ context.Context, service string) (<-chan dnssdcodec.Instance, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.browseErr[service]; err != nil {
		return nil, err
	}
	ch := make(chan dnssdcodec.Instance, 64)
	b.chans[service] = ch
	return ch, nil
}

func (b *fakeBrowser) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed++
	return b.closeErr
}

// feed returns the channel a Browse for service was handed.
func (b *fakeBrowser) feed(t *testing.T, service string) chan dnssdcodec.Instance {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	ch, ok := b.chans[service]
	if !ok {
		t.Fatalf("no Browse was opened for %s", service)
	}
	return ch
}

var _ dnssdsession.Browser = (*fakeBrowser)(nil)

// useFakeBrowser routes newDNSSDBrowser at the given fake for the
// test's lifetime. A nil fake makes the constructor fail, which is how
// "no multicast socket could be opened" is produced on demand.
func useFakeBrowser(t *testing.T, fb *fakeBrowser) {
	t.Helper()
	prev := newDNSSDBrowser
	newDNSSDBrowser = func(*slog.Logger) (dnssdsession.Browser, error) {
		if fb == nil {
			return nil, errors.New("mdns: multicast group unavailable (scripted)")
		}
		return fb, nil
	}
	t.Cleanup(func() { newDNSSDBrowser = prev })
}

// scriptedResponder is a Responder whose Announce / Update outcomes a
// test scripts; it records what was announced.
type scriptedResponder struct {
	mu          sync.Mutex
	announceErr error
	updateErr   error
	announced   []dnssdcodec.Instance
	updated     []dnssdcodec.Instance
	closed      int
}

func (r *scriptedResponder) Announce(_ context.Context, ins dnssdcodec.Instance) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.announceErr != nil {
		return r.announceErr
	}
	r.announced = append(r.announced, ins)
	return nil
}

func (r *scriptedResponder) Update(_ context.Context, ins dnssdcodec.Instance) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateErr != nil {
		return r.updateErr
	}
	r.updated = append(r.updated, ins)
	return nil
}

func (r *scriptedResponder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed++
	return nil
}

func (r *scriptedResponder) lastAnnounce(t *testing.T) dnssdcodec.Instance {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.announced) == 0 {
		t.Fatal("nothing was announced")
	}
	return r.announced[len(r.announced)-1]
}

var _ dnssdsession.Responder = (*scriptedResponder)(nil)

// useResponder routes newDNSSDResponder at the given responder. A nil
// responder makes the constructor fail.
func useResponder(t *testing.T, r *scriptedResponder) {
	t.Helper()
	prev := newDNSSDResponder
	newDNSSDResponder = func(*slog.Logger) (dnssdsession.Responder, error) {
		if r == nil {
			return nil, errors.New("mdns: responder socket unavailable (scripted)")
		}
		return r, nil
	}
	t.Cleanup(func() { newDNSSDResponder = prev })
}

// useHostname scripts osHostname for the test's lifetime.
func useHostname(t *testing.T, name string, err error) {
	t.Helper()
	prev := osHostname
	osHostname = func() (string, error) { return name, err }
	t.Cleanup(func() { osHostname = prev })
}

// registryInstance is one `_nmos-register._tcp` advertisement as the
// browser would deliver it.
func registryInstance(name, service, host string, port uint16, txt map[string]string) dnssdcodec.Instance {
	return dnssdcodec.Instance{
		Name: name, Service: service, Domain: "local",
		Host: host, Port: port, TXT: txt,
	}
}
