package main

// AMWA NMOS IS-07 Event & Tally CLI verbs:
//
//	dhs consumer nmos events watch  ws://<node>:<port><path> --sources <uuid,uuid>
//	dhs producer nmos events serve  --bind :8090 --path /x-nmos/events/v1.0/ws

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	stdhttp "net/http"
	"os"
	"strings"
	"time"

	"acp/internal/amwa/codec/is07"
	"acp/internal/amwa/session/events"
)

// runNMOSEventsConsumer dispatches `dhs consumer nmos events <verb>`.
func runNMOSEventsConsumer(ctx context.Context, args []string) error {
	if len(args) == 0 || hasHelpFlag(args) {
		printNMOSEventsConsumerHelp()
		return nil
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "watch":
		return runNMOSEventsWatch(ctx, rest)
	}
	return fmt.Errorf("consumer nmos events: unknown verb %q (expected: watch)", verb)
}

// runNMOSEventsProducer dispatches `dhs producer nmos events <verb>`.
func runNMOSEventsProducer(ctx context.Context, args []string) error {
	if len(args) == 0 || hasHelpFlag(args) {
		printNMOSEventsProducerHelp()
		return nil
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "serve":
		return runNMOSEventsServe(ctx, rest)
	}
	return fmt.Errorf("producer nmos events: unknown verb %q (expected: serve)", verb)
}

// runNMOSEventsWatch connects to a Node's IS-07 WS endpoint, sends a
// command_subscription with the requested source IDs, and prints
// every incoming Message frame as JSON for the operator.
func runNMOSEventsWatch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	wsURL := fs.String("url", "", "ws://<node>:<port>/x-nmos/events/v1.0/ws (required)")
	sources := fs.String("sources", "", "comma-separated source UUIDs to subscribe to")
	heartbeat := fs.Duration("heartbeat", 5*time.Second, "client-side command_health interval (0 disables)")
	timeout := fs.Duration("timeout", 0, "stop after this duration; 0 = run until ^C")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *wsURL == "" {
		return errors.New("--url is required (e.g. ws://node:8090/x-nmos/events/v1.0/ws)")
	}
	srcList := splitNonEmpty(*sources)

	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}

	sub := events.NewSubscriber(events.SubscriberOptions{
		HeartbeatInterval: *heartbeat,
	})
	if err := sub.Connect(ctx, *wsURL); err != nil {
		return err
	}
	defer func() { _ = sub.Close() }()
	if err := sub.Subscribe(srcList); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "subscribed to %s (%d sources)\n", *wsURL, len(srcList))

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return sub.Run(ctx, func(m is07.Message) {
		_ = enc.Encode(map[string]any{
			"kind":    string(m.Kind()),
			"message": m,
		})
	})
}

// runNMOSEventsServe stands up a Publisher behind an HTTP server at
// the given path. Useful for interop testing — `dhs consumer nmos
// events watch` against this exercises the full WS round-trip
// without a real Node.
func runNMOSEventsServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("events serve", flag.ContinueOnError)
	bind := fs.String("bind", ":8090", "HTTP listen address")
	path := fs.String("path", "/x-nmos/events/v1.0/ws", "WS endpoint path")
	heartbeat := fs.Duration("heartbeat", 5*time.Second, "publisher heartbeat interval (0 disables)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	pub := events.NewPublisher(events.PublisherOptions{
		HeartbeatInterval: *heartbeat,
	})
	mux := stdhttp.NewServeMux()
	mux.Handle(*path, pub.Handler())
	srv := &stdhttp.Server{Addr: *bind, Handler: mux}
	fmt.Fprintf(os.Stderr, "nmos/is07: serving %s%s\n", *bind, *path)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case err := <-errCh:
		_ = pub.Close()
		if errors.Is(err, stdhttp.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		_ = pub.Close()
		return nil
	}
}

func splitNonEmpty(csv string) []string {
	if csv == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func printNMOSEventsConsumerHelp() {
	fmt.Println(`dhs consumer nmos events — IS-07 Event & Tally subscriber

USAGE
  dhs consumer nmos events watch --url ws://<node>:<port>/x-nmos/events/v1.0/ws \
       --sources uuid1,uuid2 [--heartbeat 5s] [--timeout 0]

EXAMPLES
  dhs consumer nmos events watch --url ws://10.0.0.10:8090/x-nmos/events/v1.0/ws \
       --sources 1f1e1d1c-1b1a-4019-8817-161514131211`)
}

func printNMOSEventsProducerHelp() {
	fmt.Println(`dhs producer nmos events — IS-07 Event & Tally publisher

USAGE
  dhs producer nmos events serve --bind :PORT [--path /x-nmos/events/v1.0/ws] [--heartbeat 5s]

EXAMPLES
  dhs producer nmos events serve --bind :8090
  dhs producer nmos events serve --bind 0.0.0.0:8090 --heartbeat 0`)
}
