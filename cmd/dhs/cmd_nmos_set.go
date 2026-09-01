package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"dhs/internal/amwa/codec/is05"
	"dhs/internal/amwa/codec/spec"
	"dhs/internal/amwa/consumer"
)

// runNMOSSet implements `dhs consumer nmos set` — IS-05 transport
// configuration, the half that makes a device actually emit.
//
// `connect` points a Receiver at a Sender. This points a Sender at a
// network. A device can be fully connected and still move nothing: a
// real EVS Neuron ships every Sender master_enable=true with
// destination_ip 0.0.0.0, which is enabled and addressed nowhere.
func runNMOSSet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("set", flag.ContinueOnError)
	node := fs.String("node", "", "drive ONE Node directly (http://host:port) — no Registry in the path")
	registry := fs.String("registry", "", "Registry origin (http://host:port); when empty, --mdns discovers one")
	mdns := fs.Bool("mdns", true, "discover the Registry via mDNS; ignored if --registry or --node is set")
	resolver := fs.String("resolver", "", "unicast DNS resolver IP (implies unicast discovery)")
	domain := fs.String("domain", "by-systems.arpa", "unicast DNS-SD discovery domain")
	apiVer := fs.String("api-ver", "", "force a specific IS-04 wire minor; empty = highest mutual")
	timeout := fs.Duration("timeout", 5*time.Second, "DNS-SD discovery timeout")

	sender := fs.String("sender", "", "IS-04 Sender UUID to configure (required)")
	dest := fs.String("destination", "",
		"destination IP per transport leg, comma-separated in device order "+
			"(ST 2022-7 senders have two legs and they must not share a group)")
	port := fs.String("port", "", "destination port per leg, comma-separated; empty leaves the device's own")
	enable := fs.Bool("enable", false, "also set master_enable=true")
	disable := fs.Bool("disable", false, "also set master_enable=false")
	mode := fs.String("mode", "activate_immediate",
		"activate_immediate | activate_scheduled_relative | activate_scheduled_absolute")
	when := fs.String("when", "", "TAI time <secs>:<nanos> for the scheduled modes")
	dryRun := fs.Bool("dry-run", false,
		"print the endpoint, the exact PATCH body and the sender's current legs — send nothing")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if *sender == "" {
		return fmt.Errorf("nmos set: --sender <uuid> is required " +
			"(run `dhs consumer nmos walk -l` to list them)")
	}
	if *enable && *disable {
		return fmt.Errorf("nmos set: --enable and --disable are mutually exclusive")
	}

	ips, err := splitList(*dest)
	if err != nil {
		return fmt.Errorf("nmos set --destination: %w", err)
	}
	var ports []int
	if *port != "" {
		raw, err := splitList(*port)
		if err != nil {
			return fmt.Errorf("nmos set --port: %w", err)
		}
		for _, p := range raw {
			n, err := strconv.Atoi(p)
			if err != nil {
				return fmt.Errorf("nmos set --port: %q is not a port number", p)
			}
			ports = append(ports, n)
		}
	}

	var master *bool
	switch {
	case *enable:
		v := true
		master = &v
	case *disable:
		v := false
		master = &v
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
		return fmt.Errorf("nmos set: %w", err)
	}

	res, err := c.SetSender(ctx, consumer.SetSenderRequest{
		SenderID:         *sender,
		DestinationIPs:   ips,
		DestinationPorts: ports,
		MasterEnable:     master,
		Mode:             is05.ActivationMode(*mode),
		When:             *when,
		DryRun:           *dryRun,
	})
	if err != nil {
		printComplianceSummary(rep.Snapshot())
		return err
	}

	if res.DryRun {
		fmt.Printf("DRY RUN — nothing was sent\n\n")
		fmt.Printf("sender %s via %s\n", res.SenderID, res.Endpoint)
		fmt.Printf("  currently  master_enable=%t\n", res.MasterEnable)
		printLegs("             ", res.Current)
		body, _ := json.MarshalIndent(res.Patch, "  ", "  ")
		fmt.Printf("  PATCH %s/single/senders/%s/staged\n  %s\n",
			res.Endpoint, res.SenderID, body)
		printComplianceSummary(rep.Snapshot())
		return nil
	}

	fmt.Printf("SET sender %s via %s\n", res.SenderID, res.Endpoint)
	fmt.Printf("  master_enable %t\n", res.MasterEnable)
	printLegs("  ", res.Legs)

	// The device's own answer is the truth. A leg still on 0.0.0.0 emits
	// nothing no matter how the request looked.
	for i, leg := range res.Legs {
		if leg.DestinationIP == "" || leg.DestinationIP == "0.0.0.0" {
			fmt.Fprintf(os.Stderr,
				"\nWARNING: leg %d has no destination (%q) — this leg emits nothing.\n",
				i, leg.DestinationIP)
		}
	}
	printComplianceSummary(rep.Snapshot())
	return nil
}

func printLegs(indent string, legs []consumer.LegState) {
	for i, l := range legs {
		dst := l.DestinationIP
		if dst == "" {
			dst = "(none)"
		}
		fmt.Printf("%sleg %d  src=%-15s dst=%-15s port=%-6d rtp=%t\n",
			indent, i, l.SourceIP, dst, l.DestinationPort, l.RTPEnabled)
	}
}

// splitList parses a comma-separated flag into trimmed, non-empty
// values. An empty flag yields nil, meaning "leave it alone".
func splitList(s string) ([]string, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, fmt.Errorf("empty value in %q — one entry per transport leg", s)
		}
		out = append(out, p)
	}
	return out, nil
}
