package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"dhs/internal/amwa/codec/is08"
	"dhs/internal/amwa/codec/spec"
	"dhs/internal/amwa/consumer"
)

// runNMOSMap implements `dhs consumer nmos map` — the IS-08 half of the
// Controller role: read a Device's audio channel map, or re-map it.
//
// With no --route it prints what the Device holds. Inputs and outputs
// are named by their IS-08 ids, which are per Device: the read is how
// an operator learns them before routing anything.
func runNMOSMap(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("map", flag.ContinueOnError)
	node := fs.String("node", "", "drive ONE Node directly (http://host:port) — no Registry in the path")
	registry := fs.String("registry", "", "Registry origin (http://host:port); when empty, --mdns discovers one")
	mdns := fs.Bool("mdns", true, "discover the Registry via mDNS; ignored if --registry or --node is set")
	resolver := fs.String("resolver", "", "unicast DNS resolver IP (implies unicast discovery)")
	domain := fs.String("domain", "by-systems.arpa", "unicast DNS-SD discovery domain")
	apiVer := fs.String("api-ver", "", "force a specific IS-04 wire minor; empty = highest mutual")
	timeout := fs.Duration("timeout", 5*time.Second, "DNS-SD discovery timeout")

	device := fs.String("device", "", "IS-04 Device UUID whose channel map is read or changed (required)")
	var routes multiFlag
	fs.Var(&routes, "route", "repeatable `<output>:<channel>=<input>:<channel>`; an empty right side (`<output>:<channel>=`) leaves that channel unrouted")
	mode := fs.String("mode", "activate_immediate",
		"activate_immediate | activate_scheduled_relative | activate_scheduled_absolute")
	when := fs.String("when", "", "TAI time <secs>:<nanos> for the scheduled modes")
	cancel := fs.String("cancel", "", "withdraw the scheduled activation with this `id` before its instant")
	dryRun := fs.Bool("dry-run", false,
		"check the routes against what the Device declares and print the exact POST body — send nothing")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	logger, _, logClean, _ := consumerLogger(ctx, "nmos", "session", "map")
	defer logClean()

	parsed := make([]consumer.ChannelRoute, 0, len(routes))
	for _, arg := range routes {
		r, err := parseChannelRoute(arg)
		if err != nil {
			return fmt.Errorf("nmos map: %w", err)
		}
		parsed = append(parsed, r)
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
		return fmt.Errorf("nmos map: %w", err)
	}

	res, err := c.ChannelMap(ctx, consumer.ChannelMapRequest{
		DeviceID: *device,
		Routes:   parsed,
		Mode:     is08.ActivationMode(*mode),
		When:     *when,
		Cancel:   *cancel,
		DryRun:   *dryRun,
	})
	if err != nil {
		printComplianceSummary(rep.Snapshot())
		return err
	}
	fmt.Print(channelMapReport(res, *cancel))
	printComplianceSummary(rep.Snapshot())
	return nil
}

// parseChannelRoute reads `<output>:<channel>=<input>:<channel>`; an
// empty right side leaves the output channel unrouted. The channel is
// split off at the LAST colon, so an id that itself contains one still
// reads.
func parseChannelRoute(arg string) (consumer.ChannelRoute, error) {
	const want = "want <output>:<channel>=<input>:<channel> (an empty right side leaves the channel unrouted)"
	left, right, ok := strings.Cut(arg, "=")
	if !ok {
		return consumer.ChannelRoute{}, fmt.Errorf("--route %q: %s", arg, want)
	}
	output, channel, err := splitChannel(strings.TrimSpace(left))
	if err != nil {
		return consumer.ChannelRoute{}, fmt.Errorf("--route %q: %s", arg, want)
	}
	r := consumer.ChannelRoute{Output: output, Channel: channel}
	if right = strings.TrimSpace(right); right == "" {
		return r, nil
	}
	if r.Input, r.InputChannel, err = splitChannel(right); err != nil {
		return consumer.ChannelRoute{}, fmt.Errorf("--route %q: %s", arg, want)
	}
	return r, nil
}

// splitChannel reads `<id>:<channel>`.
func splitChannel(s string) (string, int, error) {
	i := strings.LastIndexByte(s, ':')
	if i <= 0 {
		return "", 0, fmt.Errorf("no channel in %q", s)
	}
	n, err := strconv.Atoi(s[i+1:])
	if err != nil || n < 0 {
		return "", 0, fmt.Errorf("%q is not a channel index", s[i+1:])
	}
	return s[:i], n, nil
}

// channelMapReport words the result for the terminal.
func channelMapReport(res *consumer.ChannelMapResult, cancel string) string {
	var b strings.Builder
	switch {
	case res.DryRun:
		b.WriteString("DRY RUN — nothing was sent\n\n")
		if cancel != "" {
			fmt.Fprintf(&b, "would DELETE %s/map/activations/%s\n", res.Endpoint, cancel)
			return b.String()
		}
		body, _ := json.MarshalIndent(res.Request, "  ", "  ")
		fmt.Fprintf(&b, "would POST %s/map/activations\n  %s\n\ncurrent map:\n", res.Endpoint, body)
		writeChannelMap(&b, res)
	case res.Cancelled != "":
		fmt.Fprintf(&b, "CANCELLED activation %s via %s\n", res.Cancelled, res.Endpoint)
	case res.Scheduled:
		fmt.Fprintf(&b, "SCHEDULED via %s\n  activation %s — withdraw it with --cancel %s\n",
			res.Endpoint, res.ActivationID, res.ActivationID)
	case res.ActivationID != "":
		fmt.Fprintf(&b, "MAPPED via %s (activation %s)\n\nthe Device now reports:\n", res.Endpoint, res.ActivationID)
		writeChannelMap(&b, res)
	default:
		fmt.Fprintf(&b, "channel map of device %s via %s\n\n", res.DeviceID, res.Endpoint)
		writeChannelMap(&b, res)
		if len(res.Pending) > 0 {
			ids := make([]string, 0, len(res.Pending))
			for id := range res.Pending {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			fmt.Fprintf(&b, "\nscheduled, not yet applied: %s\n", strings.Join(ids, ", "))
		}
	}
	return b.String()
}

// writeChannelMap prints every output channel and what feeds it, in a
// stable order, with the labels the Device gives its channels.
func writeChannelMap(b *strings.Builder, res *consumer.ChannelMapResult) {
	outputs := make([]string, 0, len(res.IO.Outputs))
	for id := range res.IO.Outputs {
		outputs = append(outputs, id)
	}
	sort.Strings(outputs)
	for _, id := range outputs {
		out := res.IO.Outputs[id]
		name := ""
		if out.Properties != nil {
			name = out.Properties.Name
		}
		fmt.Fprintf(b, "  output %s  %s\n", id, name)
		for ch, label := range out.Channels {
			from := "(unrouted)"
			if e, ok := res.Active.Map[id][strconv.Itoa(ch)]; ok && e.Input != nil && e.ChannelIndex != nil {
				from = fmt.Sprintf("%s:%d", *e.Input, *e.ChannelIndex)
				if in, ok := res.IO.Inputs[*e.Input]; ok && *e.ChannelIndex < len(in.Channels) {
					from += "  " + in.Channels[*e.ChannelIndex].Label
				}
			}
			fmt.Fprintf(b, "    %d  %-12s <- %s\n", ch, label.Label, from)
		}
	}
}
