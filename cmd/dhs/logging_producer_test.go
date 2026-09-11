package main

import (
	"context"
	"net"
	"testing"
)

// producerLogger reads the shared log flags from the context: no flags =
// the uniform default; --syslog-addr adds the UDP forwarder (cleanup closes
// it); an unusable --syslog-addr degrades to local logging instead of
// failing the verb.
func TestProducerLoggerFromContextFlags(t *testing.T) {
	logger, cleanup := producerLogger(context.Background())
	if logger == nil || cleanup == nil {
		t.Fatal("default producer logger must always exist")
	}
	cleanup()

	// A real UDP socket as the collector: dialing port 0 is refused on some
	// platforms (macOS), which would exercise the fallback branch instead.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pc.Close() }()
	ctx := withLogFlags(context.Background(), &logFlags{format: "json", level: "debug", syslogAddr: pc.LocalAddr().String()})
	logger, cleanup = producerLogger(ctx)
	if logger == nil {
		t.Fatal("logger with a syslog forwarder is nil")
	}
	if _, tee := logger.Handler().(teeHandler); !tee {
		t.Errorf("--syslog-addr must tee the local handler with the forwarder, got %T", logger.Handler())
	}
	logger.Info("probe")
	cleanup()

	ctx = withLogFlags(context.Background(), &logFlags{format: "text", level: "info", syslogAddr: "not a host:port"})
	logger, cleanup = producerLogger(ctx)
	if logger == nil {
		t.Fatal("unusable --syslog-addr must still return a local logger")
	}
	if _, tee := logger.Handler().(teeHandler); tee {
		t.Error("unusable --syslog-addr must not install a forwarder")
	}
	cleanup()
}
