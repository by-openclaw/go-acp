//go:build integration

// Live integration tests against a real EVS Cerebrum NB server. Skip
// unless CEREBRUM_TEST_HOST + CEREBRUM_TEST_USER + CEREBRUM_TEST_PASS
// are set in the environment.
//
// Run via:
//
//	CEREBRUM_TEST_HOST=10.6.239.50 \
//	CEREBRUM_TEST_USER=admin       \
//	CEREBRUM_TEST_PASS=s3cr3t      \
//	    go test -tags=integration ./internal/cerebrum-nb/consumer
//
// CI runs unit tests only; this file is gated behind the build tag.
package cerebrumnb

import (
	"context"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"acp/internal/cerebrum-nb/codec"
)

func liveTarget(t *testing.T) (host string, port int, user, pass string) {
	t.Helper()
	host = os.Getenv("CEREBRUM_TEST_HOST")
	if host == "" {
		t.Skip("CEREBRUM_TEST_HOST not set; skipping live integration")
	}
	user = os.Getenv("CEREBRUM_TEST_USER")
	pass = os.Getenv("CEREBRUM_TEST_PASS")
	if user == "" || pass == "" {
		t.Skip("CEREBRUM_TEST_USER and/or CEREBRUM_TEST_PASS not set; skipping")
	}
	port = DefaultPort
	if p := os.Getenv("CEREBRUM_TEST_PORT"); p != "" {
		port, _ = strconv.Atoi(p)
	}
	return host, port, user, pass
}

func TestLive_LoginAndPoll(t *testing.T) {
	host, port, user, pass := liveTarget(t)

	p := NewPlugin(nil)
	p.Username = user
	p.Password = pass

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.Connect(ctx, host, port); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	if !p.Session().LoggedIn() {
		t.Fatal("session not logged in after Connect")
	}

	pr, err := p.Session().Poll(ctx)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if pr == nil {
		t.Fatal("nil poll reply")
	}
	t.Logf("poll: connected_active=%v primary=%v secondary=%v",
		pr.ConnectedServerActive, pr.PrimaryServerState, pr.SecondaryServerState)
}

func TestLive_ListDevices(t *testing.T) {
	host, port, user, pass := liveTarget(t)

	p := NewPlugin(nil)
	p.Username = user
	p.Password = pass

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.Connect(ctx, host, port); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	var seen atomic.Int32
	p.Session().OnEvent(codec.KindDeviceChange, func(f *codec.Frame) {
		if f.Device != nil && f.Device.Type == "LIST" {
			seen.Add(1)
			t.Logf("device: %s/%s @ %s", f.Device.DeviceType, f.Device.DeviceName, f.Device.IPAddress)
		}
	})

	obCtx, obCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer obCancel()
	if err := p.Session().Obtain(obCtx, []codec.SubItem{
		&codec.DeviceChange{Type: "LIST"},
	}); err != nil {
		t.Fatalf("obtain device LIST: %v", err)
	}

	// Devices stream in over time after the obtain ack; wait briefly.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && seen.Load() == 0 {
		time.Sleep(100 * time.Millisecond)
	}
	if seen.Load() == 0 {
		t.Logf("no devices reported within 5s — empty Cerebrum or filtered config")
	}
}
