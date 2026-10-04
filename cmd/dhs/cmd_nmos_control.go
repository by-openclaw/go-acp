package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"dhs/internal/amwa/codec/spec"
	"dhs/internal/amwa/consumer"
)

// runNMOSControl implements `dhs consumer nmos control` — the IS-12 half
// of the Controller role: read and set a Device's model (MS-05-02) over
// its control WebSocket, call its methods, and watch its properties
// change.
//
// With only --device it lists the objects of the model by role path;
// with --role-path it describes that object's class. Those two reads
// are how an operator learns the property and method ids the other
// flags take.
func runNMOSControl(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("control", flag.ContinueOnError)
	node := fs.String("node", "", "drive ONE Node directly (http://host:port) — no Registry in the path")
	registry := fs.String("registry", "", "Registry origin (http://host:port); when empty, --mdns discovers one")
	mdns := fs.Bool("mdns", true, "discover the Registry via mDNS; ignored if --registry or --node is set")
	resolver := fs.String("resolver", "", "unicast DNS resolver IP (implies unicast discovery)")
	domain := fs.String("domain", "by-systems.arpa", "unicast DNS-SD discovery domain")
	apiVer := fs.String("api-ver", "", "force a specific IS-04 wire minor; empty = highest mutual")
	timeout := fs.Duration("timeout", 5*time.Second, "DNS-SD discovery timeout")

	device := fs.String("device", "", "IS-04 Device UUID whose model is read or changed (required)")
	rolePath := fs.String("role-path", "", "the object to address (root, root.receivers.rx1, …); empty lists every object")
	get := fs.String("get", "", "read this property `id` (e.g. 1p6)")
	set := fs.String("set", "", "write a property: `id=json` (e.g. 1p6='\"Studio A\"')")
	invoke := fs.String("invoke", "", "call this method `id` (e.g. 3m1)")
	arguments := fs.String("args", "", "the method's arguments, as a JSON object")
	watch := fs.Bool("watch", false, "print the property changes of --role-path (of every object with none) as the Device announces them")
	duration := fs.Duration("duration", 0, "with --watch: stop after this long; 0 = until interrupted")
	dryRun := fs.Bool("dry-run", false, "read, change nothing")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	logger, _, logClean, _ := consumerLogger(ctx, "nmos", "session", "control")
	defer logClean()

	req := consumer.ControlRequest{
		DeviceID: *device, RolePath: *rolePath, Get: *get, Invoke: *invoke, Watch: *watch, DryRun: *dryRun,
	}
	if *arguments != "" {
		req.Arguments = json.RawMessage(*arguments)
	}
	if *set != "" {
		id, value, ok := strings.Cut(*set, "=")
		if !ok || id == "" {
			return fmt.Errorf("nmos control: --set %q: want <property-id>=<json>", *set)
		}
		req.Set, req.SetValue = id, json.RawMessage(value)
	}
	if *watch {
		req.OnChange = func(c consumer.ControlChange) { fmt.Print(controlChangeLine(c)) }
		if *duration > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, *duration)
			defer cancel()
		}
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
		return fmt.Errorf("nmos control: %w", err)
	}

	res, err := c.Control(ctx, req)
	if err != nil {
		printComplianceSummary(rep.Snapshot())
		return err
	}
	fmt.Print(controlReport(res, req))
	printComplianceSummary(rep.Snapshot())
	return nil
}

// controlChangeLine words one property change, as it arrives.
func controlChangeLine(c consumer.ControlChange) string {
	at := ""
	if c.SequenceItemIndex != nil {
		at = "[" + strconv.Itoa(*c.SequenceItemIndex) + "]"
	}
	name := c.RolePath
	if name == "" {
		name = "oid " + strconv.Itoa(c.OID)
	}
	return fmt.Sprintf("changed  %s.%s%s = %s\n", name, c.Property, at, c.Value)
}

// classIDText spells a class id the way MS-05-02 writes it: 1.2.2.1.
func classIDText(id []int32) string {
	parts := make([]string, len(id))
	for i, n := range id {
		parts[i] = strconv.Itoa(int(n))
	}
	return strings.Join(parts, ".")
}

// controlReport words the result for the terminal.
func controlReport(res *consumer.ControlResult, req consumer.ControlRequest) string {
	var b strings.Builder
	if res.DryRun {
		b.WriteString("DRY RUN — nothing was changed\n\n")
	}
	switch {
	case req.Watch:
		fmt.Fprintf(&b, "%d change(s) via %s\n", res.Changes, res.Endpoint)
	case res.Objects != nil:
		fmt.Fprintf(&b, "objects via %s\n", res.Endpoint)
		for _, o := range res.Objects {
			label := ""
			if o.UserLabel != "" {
				label = "  " + o.UserLabel
			}
			fmt.Fprintf(&b, "  oid %-6d class %-12s %s%s\n", o.OID, classIDText(o.ClassID), o.RolePath, label)
		}
	case req.Invoke != "":
		if res.DryRun {
			fmt.Fprintf(&b, "would invoke %s on %s via %s\n", req.Invoke, res.RolePath, res.Endpoint)
			break
		}
		fmt.Fprintf(&b, "INVOKED %s on %s via %s: status %d\n", req.Invoke, res.RolePath, res.Endpoint, res.Result.Status)
		if len(res.Value) > 0 {
			fmt.Fprintf(&b, "  value: %s\n", res.Value)
		}
	case req.Set != "":
		if res.DryRun {
			fmt.Fprintf(&b, "would set %s.%s = %s (the Device holds %s)\n", res.RolePath, req.Set, req.SetValue, res.Value)
			break
		}
		fmt.Fprintf(&b, "SET %s.%s via %s: the Device now holds %s\n", res.RolePath, req.Set, res.Endpoint, res.Value)
	case req.Get != "":
		fmt.Fprintf(&b, "%s.%s = %s\n", res.RolePath, req.Get, res.Value)
	default:
		fmt.Fprintf(&b, "%s (oid %d) via %s\n", res.RolePath, res.OID, res.Endpoint)
		class, _ := json.MarshalIndent(res.Class, "  ", "  ")
		fmt.Fprintf(&b, "  class:\n  %s\n", class)
	}
	return b.String()
}
