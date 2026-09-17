package main

// The two pieces the supervised watch is built on. The dial/setup/backoff
// cycle itself is tested in internal/consumer; these cover the CLI's half:
// deciding whether a protocol CAN recover, and re-establishing a session in
// place when it must.

import (
	"context"
	"errors"
	"testing"
	"time"

	"dhs/internal/consumer"
)

// recoverablePlugin adds the death signal to the shared fakePlugin.
type recoverablePlugin struct {
	fakePlugin
	done chan struct{}

	connectCalls    int
	disconnectCalls int
	lastHost        string
	lastPort        int
	connectErr      error
	deadline        time.Time
	hadDeadline     bool
}

func (p *recoverablePlugin) SessionDone() <-chan struct{} { return p.done }

func (p *recoverablePlugin) Connect(ctx context.Context, ip string, port int) error {
	p.connectCalls++
	p.lastHost, p.lastPort = ip, port
	p.deadline, p.hadDeadline = ctx.Deadline()
	return p.connectErr
}

func (p *recoverablePlugin) Disconnect() error {
	p.disconnectCalls++
	return nil
}

// A protocol that cannot tell us its session died cannot be supervised —
// and neither can one whose channel is nil, which means "no session to lose"
// (ACP1 over UDP) rather than "not implemented".
func TestWatchCanRecover(t *testing.T) {
	tests := []struct {
		name string
		plug consumer.Protocol
		want bool
	}{
		{"no capability", &fakePlugin{}, false},
		{"capability, nil channel", &recoverablePlugin{}, false},
		{"capability, live channel", &recoverablePlugin{done: make(chan struct{})}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := watchCanRecover(tc.plug); got != tc.want {
				t.Errorf("watchCanRecover = %v, want %v", got, tc.want)
			}
		})
	}
}

// Reconnect releases the dead session before opening a new one — a
// half-open session that is never disconnected is a leaked socket and a
// leaked goroutine, which is the bug this whole branch exists to close.
func TestReconnectPluginDisconnectsThenConnects(t *testing.T) {
	p := &recoverablePlugin{done: make(chan struct{})}
	cf := &commonFlags{protocol: "acp1", port: 2071, timeout: time.Second}

	if err := reconnectPlugin(context.Background(), p, "10.6.239.113", cf); err != nil {
		t.Fatalf("reconnectPlugin: %v", err)
	}
	if p.disconnectCalls != 1 {
		t.Errorf("Disconnect called %d times, want 1", p.disconnectCalls)
	}
	if p.connectCalls != 1 {
		t.Errorf("Connect called %d times, want 1", p.connectCalls)
	}
	if p.lastHost != "10.6.239.113" || p.lastPort != 2071 {
		t.Errorf("reconnected to %s:%d, want 10.6.239.113:2071", p.lastHost, p.lastPort)
	}
}

// A tight --timeout must not kill a reconnect that legitimately needs
// several round trips: the same 5 s floor connect() applies.
func TestReconnectPluginAppliesTheDialTimeoutFloor(t *testing.T) {
	p := &recoverablePlugin{}
	cf := &commonFlags{protocol: "acp1", port: 2071, timeout: 10 * time.Millisecond}

	start := time.Now()
	if err := reconnectPlugin(context.Background(), p, "10.0.0.1", cf); err != nil {
		t.Fatalf("reconnectPlugin: %v", err)
	}
	if !p.hadDeadline {
		t.Fatal("Connect got no deadline")
	}
	if d := p.deadline.Sub(start); d < 4*time.Second {
		t.Errorf("dial deadline %s, want the 5s floor rather than --timeout", d)
	}
}

// With no --port the connector's registered default is used, so a reconnect
// does not silently land on port 0.
func TestReconnectPluginResolvesTheDefaultPort(t *testing.T) {
	p := &recoverablePlugin{}
	cf := &commonFlags{protocol: "acp1", timeout: time.Second} // port left 0

	if err := reconnectPlugin(context.Background(), p, "10.0.0.1", cf); err != nil {
		t.Fatalf("reconnectPlugin: %v", err)
	}
	if p.lastPort == 0 {
		t.Fatal("reconnect used port 0 instead of the connector default")
	}
}

func TestReconnectPluginUnknownProtocol(t *testing.T) {
	p := &recoverablePlugin{}
	cf := &commonFlags{protocol: "not-a-protocol", timeout: time.Second}

	if err := reconnectPlugin(context.Background(), p, "10.0.0.1", cf); err == nil {
		t.Fatal("reconnectPlugin succeeded for an unknown protocol")
	}
}

func TestReconnectPluginSurfacesConnectFailure(t *testing.T) {
	want := errors.New("still down")
	p := &recoverablePlugin{connectErr: want}
	cf := &commonFlags{protocol: "acp1", port: 2071, timeout: time.Second}

	if err := reconnectPlugin(context.Background(), p, "10.0.0.1", cf); !errors.Is(err, want) {
		t.Fatalf("reconnectPlugin = %v, want %v", err, want)
	}
}
