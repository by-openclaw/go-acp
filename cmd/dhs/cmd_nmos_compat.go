package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"dhs/internal/amwa/codec/is11"
	"dhs/internal/amwa/codec/spec"
	"dhs/internal/amwa/consumer"
)

// runNMOSCompat implements `dhs consumer nmos compat` — the IS-11 half
// of the Controller role: why a Sender and a Receiver do not agree, in
// the Device's own words, and holding a Sender to what the far end
// takes.
func runNMOSCompat(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("compat", flag.ContinueOnError)
	node := fs.String("node", "", "drive ONE Node directly (http://host:port) — no Registry in the path")
	registry := fs.String("registry", "", "Registry origin (http://host:port); when empty, --mdns discovers one")
	mdns := fs.Bool("mdns", true, "discover the Registry via mDNS; ignored if --registry or --node is set")
	resolver := fs.String("resolver", "", "unicast DNS resolver IP (implies unicast discovery)")
	domain := fs.String("domain", "by-systems.arpa", "unicast DNS-SD discovery domain")
	apiVer := fs.String("api-ver", "", "force a specific IS-04 wire minor; empty = highest mutual")
	timeout := fs.Duration("timeout", 5*time.Second, "DNS-SD discovery timeout")

	sender := fs.String("sender", "", "IS-04 Sender UUID to read or constrain")
	receiver := fs.String("receiver", "", "IS-04 Receiver UUID to read")
	constraints := fs.String("constraints", "",
		"`file` holding the IS-11 active constraints to PUT on --sender: {\"constraint_sets\":[{\"urn:x-nmos:cap:…\":{…}}]}")
	release := fs.Bool("release", false, "remove --sender's active constraints")
	dryRun := fs.Bool("dry-run", false, "check the constraints against what the Sender supports and read its state — change nothing")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	logger, _, logClean, _ := consumerLogger(ctx, "nmos", "session", "compat")
	defer logClean()

	var ac *is11.ActiveConstraints
	if *constraints != "" {
		raw, err := os.ReadFile(*constraints)
		if err != nil {
			return fmt.Errorf("nmos compat: --constraints: %w", err)
		}
		parsed, err := is11.DecodeActiveConstraints(raw)
		if err != nil {
			return fmt.Errorf("nmos compat: --constraints %s: %w", *constraints, err)
		}
		ac = &parsed
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
		Logger:           logger,
		Deps:             pluginDeps(logger),
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
		return fmt.Errorf("nmos compat: %w", err)
	}

	res, err := c.Compat(ctx, consumer.CompatRequest{
		SenderID:    *sender,
		ReceiverID:  *receiver,
		Constraints: ac,
		Release:     *release,
		DryRun:      *dryRun,
	})
	if err != nil {
		printComplianceSummary(rep.Snapshot())
		return err
	}
	fmt.Print(compatReport(res))
	printComplianceSummary(rep.Snapshot())
	return nil
}

// compatReport words the result for the terminal: the state first, with
// the Device's reason, because that is the line an operator came for.
func compatReport(res *consumer.CompatResult) string {
	var b strings.Builder
	if res.DryRun {
		b.WriteString("DRY RUN — nothing was changed\n\n")
	}
	if res.ReceiverID != "" {
		fmt.Fprintf(&b, "receiver %s via %s\n", res.ReceiverID, res.Endpoint)
		writeCompatStatus(&b, res.Status)
		for _, out := range res.Outputs {
			fmt.Fprintf(&b, "  output %s  %s  connected=%t  %s\n", out.ID, out.Label, out.Connected, out.Status.State)
		}
		return b.String()
	}
	fmt.Fprintf(&b, "sender %s via %s\n", res.SenderID, res.Endpoint)
	if res.Changed {
		b.WriteString("  constraints changed; the Device now reports:\n")
	}
	writeCompatStatus(&b, res.Status)
	if len(res.Active.ConstraintSets) == 0 {
		b.WriteString("  active constraints: none\n")
	} else {
		body, _ := json.MarshalIndent(res.Active.ConstraintSets, "  ", "  ")
		fmt.Fprintf(&b, "  active constraints:\n  %s\n", body)
	}
	if len(res.Supported) > 0 {
		fmt.Fprintf(&b, "  can be constrained on: %s\n", strings.Join(res.Supported, ", "))
	}
	for _, in := range res.Inputs {
		fmt.Fprintf(&b, "  input %s  %s  connected=%t  %s\n", in.ID, in.Label, in.Connected, in.Status.State)
	}
	return b.String()
}

func writeCompatStatus(b *strings.Builder, st is11.Status) {
	fmt.Fprintf(b, "  state: %s\n", st.State)
	if st.Debug != "" {
		fmt.Fprintf(b, "  the Device says: %s\n", st.Debug)
	}
}
