//go:build integration

package blackmagic_hyperdeck_integration

import (
	"context"
	"log/slog"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	hyperdeck "acp/internal/blackmagic-hyperdeck/consumer"
)

func TestRealDeviceDeviceInfo(t *testing.T) {
	host := os.Getenv("BLACKMAGIC_HYPERDECK_TEST_HOST")
	if host == "" {
		t.Skip("BLACKMAGIC_HYPERDECK_TEST_HOST not set")
	}
	port := hyperdeck.DefaultPort
	if p := os.Getenv("BLACKMAGIC_HYPERDECK_TEST_PORT"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			t.Fatalf("BLACKMAGIC_HYPERDECK_TEST_PORT: %v", err)
		}
		port = n
	}
	if h, p, err := net.SplitHostPort(host); err == nil {
		host = h
		n, err := strconv.Atoi(p)
		if err != nil {
			t.Fatalf("host port: %v", err)
		}
		port = n
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	p := hyperdeck.NewPlugin(slog.Default())
	if err := p.Connect(ctx, host, port); err != nil {
		t.Fatal(err)
	}
	defer p.Disconnect()

	info, err := p.GetDeviceInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Port == 0 || info.ProtocolVersion == 0 {
		t.Fatalf("unexpected device info: %+v", info)
	}
}
