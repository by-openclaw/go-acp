package registry

import (
	"context"
	"errors"
	"testing"
	"time"

	registryslot "acp/internal/registry"
)

func TestFactoryRegisteredViaInit(t *testing.T) {
	f, ok := registryslot.Lookup(PluginName)
	if !ok {
		t.Fatalf("expected %q to be registered via init()", PluginName)
	}
	meta := f.Meta()
	if meta.Name != PluginName {
		t.Errorf("Name: %q", meta.Name)
	}
	if meta.DefaultPort == 0 {
		t.Errorf("DefaultPort should be non-zero")
	}
}

func TestPickAdvertiseHostPort(t *testing.T) {
	cases := []struct {
		name      string
		opts      registryslot.ServeOptions
		hostFrag  string // substring check (avoids OS hostname leakage)
		port      uint16
		wantErr   bool
	}{
		{
			"AdvertiseHost wins",
			registryslot.ServeOptions{AdvertiseHost: "fakehost.local:8235"},
			"fakehost.local",
			8235,
			false,
		},
		{
			"BindAddrs[0] used as fallback",
			registryslot.ServeOptions{BindAddrs: []string{"10.0.0.5:9000"}},
			"10.0.0.5",
			9000,
			false,
		},
		{
			"0.0.0.0 expands to hostname",
			registryslot.ServeOptions{AdvertiseHost: "0.0.0.0:1234"},
			".local",
			1234,
			false,
		},
		{
			"empty input fails",
			registryslot.ServeOptions{},
			"",
			0,
			true,
		},
		{
			"bad port fails",
			registryslot.ServeOptions{AdvertiseHost: "host:bad"},
			"",
			0,
			true,
		},
	}
	// Pin the OS hostname so the .local-suffix test is deterministic.
	prev := osHostnameFn
	osHostnameFn = func() (string, error) { return "test-host", nil }
	defer func() { osHostnameFn = prev }()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host, port, err := pickAdvertiseHostPort(tc.opts)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if port != tc.port {
				t.Errorf("port: %d, want %d", port, tc.port)
			}
			if tc.hostFrag != "" && !contains(host, tc.hostFrag) {
				t.Errorf("host %q does not contain %q", host, tc.hostFrag)
			}
		})
	}
}

func TestServeStaticModeSkipsMDNS(t *testing.T) {
	// Mode "static" should not open a multicast socket — verifies the
	// scaffold respects --discovery=static even when no IS-04 HTTP
	// surface is wired up yet.
	r := &Registry{}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := r.Serve(ctx, registryslot.ServeOptions{
		AdvertiseHost: "127.0.0.1:8235",
		DiscoveryMode: "static",
	})
	// Serve returns when ctx cancels; we expect no error path triggered.
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("unexpected error: %v", err)
	}
	stats := r.Stats()
	if stats.Registrations != 0 {
		t.Errorf("expected zero announcements in static mode, got %d", stats.Registrations)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
