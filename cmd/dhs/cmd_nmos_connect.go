package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"dhs/internal/amwa/codec/is05"
	"dhs/internal/amwa/codec/spec"
	"dhs/internal/amwa/consumer"
)

// runNMOSConnect implements `dhs consumer nmos connect` — the IS-05
// half of the Controller role.
//
// Resources are named by IS-04 UUID because that is the only stable
// identifier NMOS has; labels are mutable and non-unique, so routing by
// label would silently move the wrong signal the first time two
// receivers shared a name. `walk -l` prints the ids to paste here.
func runNMOSConnect(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	node := fs.String("node", "", "drive ONE Node directly (http://host:port) — no Registry in the path")
	registry := fs.String("registry", "", "Registry origin (http://host:port); when empty, --mdns discovers one")
	mdns := fs.Bool("mdns", true, "discover the Registry via mDNS; ignored if --registry or --node is set")
	resolver := fs.String("resolver", "", "unicast DNS resolver IP (implies unicast discovery)")
	domain := fs.String("domain", "by-systems.arpa", "unicast DNS-SD discovery domain")
	apiVer := fs.String("api-ver", "", "force a specific IS-04 wire minor; empty = highest mutual")
	timeout := fs.Duration("timeout", 5*time.Second, "DNS-SD discovery timeout")

	receiver := fs.String("receiver", "", "IS-04 Receiver UUID to drive (required)")
	sender := fs.String("sender", "", "IS-04 Sender UUID to route to it; omit to DISCONNECT the receiver")
	disconnect := fs.Bool("disconnect", false, "explicitly disconnect --receiver (same as omitting --sender)")
	mode := fs.String("mode", "activate_immediate",
		"activate_immediate | activate_scheduled_relative | activate_scheduled_absolute")
	when := fs.String("when", "", "TAI time <secs>:<nanos> for the scheduled modes")
	dryRun := fs.Bool("dry-run", false,
		"resolve and print the endpoint, the exact PATCH body and the receiver's current route — send nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *receiver == "" {
		return fmt.Errorf("nmos connect: --receiver <uuid> is required " +
			"(run `dhs consumer nmos walk -l` to list them)")
	}
	if *disconnect {
		*sender = ""
	}

	discovery := ""
	if *node == "" && *registry == "" {
		if *resolver != "" {
			discovery = "unicast"
		} else if *mdns {
			discovery = "mdns"
		}
	}

	rep := &spec.SliceReporter{}
	c, err := consumer.NewController(ctx, consumer.ControllerOptions{
		Logger:           slog.Default(),
		Reporter:         rep,
		NodeURL:          *node,
		RegistryURL:      *registry,
		DiscoveryMode:    discovery,
		DiscoveryTimeout: *timeout,
		UnicastResolver:  *resolver,
		UnicastDomain:    *domain,
		APIVer:           *apiVer,
	})
	if err != nil {
		return fmt.Errorf("nmos connect: %w", err)
	}

	res, err := c.Connect(ctx, consumer.ConnectRequest{
		SenderID:   *sender,
		ReceiverID: *receiver,
		Mode:       is05.ActivationMode(*mode),
		When:       *when,
		DryRun:     *dryRun,
	})
	if err != nil {
		printComplianceSummary(rep.Snapshot())
		return err
	}

	verb := "CONNECT"
	if *sender == "" {
		verb = "DISCONNECT"
	}

	if res.DryRun {
		fmt.Printf("DRY RUN — nothing was sent\n\n")
		fmt.Printf("would %s via %s\n", verb, res.Endpoint)
		fmt.Printf("  receiver      %s\n", res.ReceiverID)
		cur := "(none)"
		if res.CurrentSenderID != nil {
			cur = *res.CurrentSenderID
		}
		fmt.Printf("  currently     sender=%s master_enable=%t\n", cur, res.CurrentMasterEnable)
		body, _ := json.MarshalIndent(res.Patch, "  ", "  ")
		fmt.Printf("  PATCH %s/single/receivers/%s/staged\n  %s\n",
			res.Endpoint, res.ReceiverID, body)
		printComplianceSummary(rep.Snapshot())
		return nil
	}

	fmt.Printf("%s via %s\n", verb, res.Endpoint)
	fmt.Printf("  receiver      %s\n", res.ReceiverID)
	if res.SenderID != nil {
		fmt.Printf("  sender        %s\n", *res.SenderID)
	} else {
		fmt.Printf("  sender        (none)\n")
	}
	fmt.Printf("  master_enable %t\n", res.MasterEnable)
	fmt.Printf("  activation    %s\n", res.Mode)
	if res.ActivationAt != "" {
		fmt.Printf("  requested_at  %s TAI\n", res.ActivationAt)
	}
	if res.SDPBytes > 0 {
		fmt.Printf("  transport     %d bytes of SDP staged\n", res.SDPBytes)
	}

	// The device's own answer is the truth, so say plainly when it
	// disagrees with what was asked for.
	if *sender != "" && !res.MasterEnable {
		fmt.Fprintf(os.Stderr, "\nWARNING: the receiver reports master_enable=false — "+
			"the route was staged but no signal will flow.\n")
	}
	printComplianceSummary(rep.Snapshot())
	return nil
}
