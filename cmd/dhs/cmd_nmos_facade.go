package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"time"

	"dhs/internal/amwa/codec/spec"
	"dhs/internal/amwa/consumer"
	"dhs/internal/amwa/facade"
)

// runNMOSFacade implements `dhs consumer nmos facade` — the AMWA
// Testing Facade, which is how the AMWA tool scores a CONTROLLER.
//
// The tool cannot drive a controller the way it drives a Node: the
// controller is the side that does the calling. So the tool POSTs
// questions ("select the Senders you can see", "activate this
// connection") and waits for answers on its callback. A human answers
// them in a UI; this verb answers them by actually driving
// `dhs consumer nmos` walk/connect against whatever registry the tool
// stood up — which is what makes the run reproducible.
func runNMOSFacade(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("facade", flag.ContinueOnError)
	bind := fs.String("bind", ":5001", "listen address the tool POSTs questions to")
	registry := fs.String("registry", "", "Registry origin (http://host:port); empty = discover per question")
	mdns := fs.Bool("mdns", false, "discover the Registry via mDNS")
	resolver := fs.String("resolver", "", "unicast DNS resolver host[:port] — IS-04-04 test_01 requires the discovery to go through the tool's mock DNS")
	domain := fs.String("domain", "testsuite.nmos.tv", "unicast DNS-SD discovery domain (the tool's DNS zone)")
	apiVer := fs.String("api-ver", "", "force a specific IS-04 wire minor; empty = highest mutual")
	timeout := fs.Duration("timeout", 5*time.Second, "DNS-SD discovery timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *registry == "" && *resolver == "" && !*mdns {
		return fmt.Errorf("nmos facade: pick one of --registry / --resolver / --mdns")
	}

	mode := ""
	if *registry == "" {
		if *resolver != "" {
			mode = "unicast"
		} else {
			mode = "mdns"
		}
	}

	srv, err := facade.New(facade.Options{
		Logger: slog.Default(),
		Bind:   *bind,
		// A fresh Controller per question, not one held across the run:
		// the tool re-registers resources between tests, and IS-04-04
		// test_01 checks its mock DNS server actually got queried — a
		// cached discovery would answer from memory and fail it.
		Controller: func(qctx context.Context) (*consumer.Controller, error) {
			return consumer.NewController(qctx, consumer.ControllerOptions{
				Logger:           slog.Default(),
				Reporter:         spec.NopReporter{},
				RegistryURL:      *registry,
				DiscoveryMode:    mode,
				DiscoveryTimeout: *timeout,
				UnicastResolver:  *resolver,
				UnicastDomain:    *domain,
				APIVer:           *apiVer,
			})
		},
	})
	if err != nil {
		return fmt.Errorf("nmos facade: %w", err)
	}
	fmt.Printf("Testing Facade listening on %s\n", *bind)
	fmt.Printf("  registry: %s\n", pickNonEmpty(*registry, mode+" discovery"))
	fmt.Printf("Point the AMWA tool's testing-facade endpoint slot here, then run IS-04-04 / IS-05-03.\n")
	return srv.ListenAndServe(ctx)
}

func pickNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
