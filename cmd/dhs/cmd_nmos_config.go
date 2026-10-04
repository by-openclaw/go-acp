package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"dhs/internal/amwa/codec/is14"
	"dhs/internal/amwa/codec/spec"
	"dhs/internal/amwa/consumer"
)

// runNMOSConfig implements `dhs consumer nmos config` — the IS-14 half
// of the Controller role: read and set a Device's model, back it up and
// restore it.
//
// With only --device it lists the role paths; with --role-path it
// describes that object. Those two reads are how an operator learns the
// property and method ids the other flags take.
func runNMOSConfig(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	node := fs.String("node", "", "drive ONE Node directly (http://host:port) — no Registry in the path")
	registry := fs.String("registry", "", "Registry origin (http://host:port); when empty, --mdns discovers one")
	mdns := fs.Bool("mdns", true, "discover the Registry via mDNS; ignored if --registry or --node is set")
	resolver := fs.String("resolver", "", "unicast DNS resolver IP (implies unicast discovery)")
	domain := fs.String("domain", "by-systems.arpa", "unicast DNS-SD discovery domain")
	apiVer := fs.String("api-ver", "", "force a specific IS-04 wire minor; empty = highest mutual")
	timeout := fs.Duration("timeout", 5*time.Second, "DNS-SD discovery timeout")

	device := fs.String("device", "", "IS-04 Device UUID whose model is read or changed (required)")
	rolePath := fs.String("role-path", "", "the object to address (root, root.gain, …); empty lists every role path")
	get := fs.String("get", "", "read this property `id` (e.g. 3p1)")
	set := fs.String("set", "", "write a property: `id=json` (e.g. 3p1=-6.0, 1p6='\"label\"')")
	invoke := fs.String("invoke", "", "call this method `id` (e.g. 3m1)")
	arguments := fs.String("args", "", "the method's arguments, as a JSON object")
	backup := fs.String("backup", "", "write the bulk properties of --role-path to this `file` (- for stdout)")
	restore := fs.String("restore", "", "restore the bulk properties in this `file` onto --role-path; the Device validates first and nothing is applied unless every object validates")
	recurse := fs.Bool("recurse", true, "with --backup / --restore: include everything under --role-path")
	rebuild := fs.Bool("rebuild", false, "with --restore: Rebuild mode (structural changes allowed) instead of Modify")
	validateOnly := fs.Bool("validate-only", false, "with --restore: ask the Device what it would do, apply nothing")
	dryRun := fs.Bool("dry-run", false, "read and validate, change nothing")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	logger, _, logClean, _ := consumerLogger(ctx, "nmos", "session", "config")
	defer logClean()

	req := consumer.ConfigRequest{
		DeviceID: *device, RolePath: *rolePath, Get: *get, Invoke: *invoke,
		Backup: *backup != "", Recurse: *recurse, ValidateOnly: *validateOnly, DryRun: *dryRun,
	}
	if *arguments != "" {
		req.Arguments = json.RawMessage(*arguments)
	}
	if *set != "" {
		id, value, ok := strings.Cut(*set, "=")
		if !ok || id == "" {
			return fmt.Errorf("nmos config: --set %q: want <property-id>=<json>", *set)
		}
		req.Set, req.SetValue = id, json.RawMessage(value)
	}
	if *rebuild {
		req.RestoreMode = is14.RestoreModeRebuild
	}
	if *restore != "" {
		raw, err := os.ReadFile(*restore)
		if err != nil {
			return fmt.Errorf("nmos config: --restore: %w", err)
		}
		holder, err := is14.DecodeBulkPropertiesHolder(raw)
		if err != nil {
			return fmt.Errorf("nmos config: --restore %s: %w", *restore, err)
		}
		req.Restore = &holder
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
		return fmt.Errorf("nmos config: %w", err)
	}

	res, err := c.Configure(ctx, req)
	if err != nil {
		printComplianceSummary(rep.Snapshot())
		return err
	}
	if res.Holder != nil {
		body, err := is14.EncodeBulkPropertiesHolder(*res.Holder)
		if err != nil {
			return fmt.Errorf("nmos config: encode the backup: %w", err)
		}
		if *backup == "-" {
			fmt.Println(string(body))
		} else if err := writeFileAtomic(*backup, body); err != nil {
			return fmt.Errorf("nmos config: --backup: %w", err)
		}
	}
	fmt.Print(configReport(res, req, *backup))
	printComplianceSummary(rep.Snapshot())
	return nil
}

// writeFileAtomic writes to a sibling .tmp and renames it into place,
// so a backup is never left half-written under its final name.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// configReport words the result for the terminal.
func configReport(res *consumer.ConfigResult, req consumer.ConfigRequest, backupPath string) string {
	var b strings.Builder
	if res.DryRun {
		b.WriteString("DRY RUN — nothing was changed\n\n")
	}
	switch {
	case res.RolePaths != nil:
		fmt.Fprintf(&b, "role paths via %s\n", res.Endpoint)
		for _, p := range res.RolePaths {
			fmt.Fprintf(&b, "  %s\n", p)
		}
	case res.Holder != nil:
		objects := len(res.Holder.Values)
		if backupPath != "-" {
			fmt.Fprintf(&b, "BACKUP of %s via %s: %d object(s) written to %s\n", res.RolePath, res.Endpoint, objects, backupPath)
		}
	case req.Restore != nil:
		verb := "VALIDATED (nothing applied)"
		if res.Restored {
			verb = "RESTORED"
		}
		fmt.Fprintf(&b, "%s %s via %s\n", verb, res.RolePath, res.Endpoint)
		for _, v := range res.Validations {
			fmt.Fprintf(&b, "  %-4d %s\n", v.Status, strings.Join(v.Path, "."))
			for _, n := range v.Notices {
				fmt.Fprintf(&b, "         %s: %s\n", n.Name, n.NoticeMessage)
			}
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
		fmt.Fprintf(&b, "%s via %s\n", res.RolePath, res.Endpoint)
		fmt.Fprintf(&b, "  properties: %s\n", strings.Join(res.PropertyIDs, " "))
		fmt.Fprintf(&b, "  methods:    %s\n", strings.Join(res.MethodIDs, " "))
		desc, _ := json.MarshalIndent(res.Descriptor, "  ", "  ")
		fmt.Fprintf(&b, "  descriptor:\n  %s\n", desc)
	}
	return b.String()
}
