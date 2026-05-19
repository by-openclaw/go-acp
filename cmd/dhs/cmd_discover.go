package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"dhs/internal/acp1/consumer"
	"dhs/internal/errcode"
	"dhs/internal/session/dnssd"
)

// R18 #477 validation code: native mDNS tool absent on PATH.
var errMdnsToolNotFound = errcode.New(errcode.LayerValidation, "mdns-tool-not-found", errcode.ClassUsage)

// runDiscover dispatches the discover verb. ACP1 uses UDP subnet
// broadcast (existing behaviour); every other protocol uses mDNS /
// DNS-SD via the dnssd package (R18 #477). The protocol comes from
// the cf.protocol flag set by addCommonFlags.
func runDiscover(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("discover", flag.ExitOnError)
	cf := addCommonFlags(fs)
	durationStr := fs.String("duration", "5s", "how long to listen (e.g. 5s, 30s)")
	active := fs.Bool("active", true, "acp1 only: also send a broadcast probe (recommended)")
	port := fs.Int("scan-port", 2071, "acp1 only: UDP port to scan")
	_ = fs.Parse(args)

	d, err := time.ParseDuration(*durationStr)
	if err != nil {
		return fmt.Errorf("--duration: %w", err)
	}

	if cf.protocol != "acp1" {
		return runDiscoverMDNS(ctx, cf.protocol, d)
	}

	fmt.Printf("scanning for ACP1 devices on :%d for %s (active=%v)...\n",
		*port, d, *active)

	results, err := acp1.Discover(ctx, acp1.DiscoverConfig{
		Duration: d,
		Active:   *active,
		Port:     *port,
	})
	if err != nil {
		return err
	}

	if len(results) == 0 {
		fmt.Println("no devices found — check you are on the same subnet")
		return nil
	}

	fmt.Printf("\n%-16s %-5s %-6s %-16s %-20s %-20s %s\n",
		"IP", "PORT", "SLOTS", "SOURCE", "FIRST SEEN", "LAST SEEN", "")
	fmt.Println(strings.Repeat("-", 90))
	for _, r := range results {
		fmt.Printf("%-16s %-5d %-6d %-16s %-20s %-20s\n",
			r.IP, r.Port, r.NumSlots, r.Source,
			r.FirstSeen.Format("15:04:05.000"),
			r.LastSeen.Format("15:04:05.000"))
	}
	fmt.Printf("\n%d device(s) found\n", len(results))
	return nil
}

// runDiscoverMDNS browses for the protocol-specific DNS-SD service
// type via the dnssd tooled backend (R18 #477 v1). Service type per
// protocol:
//
//	emberplus  -> _ember._tcp
//	nmos       -> _nmos-node._tcp
//	others     -> validation:mdns-tool-not-found (no convention yet)
func runDiscoverMDNS(ctx context.Context, proto string, duration time.Duration) error {
	svcType := protocolMDNSService(proto)
	if svcType == "" {
		return fmt.Errorf("%w: protocol %q has no documented DNS-SD service type", errMdnsToolNotFound, proto)
	}
	// R18 #477 strict-spec: pure-Go browser is the primary path so
	// dhs works on hosts without avahi-browse / dns-sd installed.
	// NewToolBrowser remains exported as a compat fallback.
	browser := dnssd.NewPureBrowser()
	fmt.Printf("browsing %s.local for %s ...\n", svcType, duration)
	services, err := browser.Browse(ctx, dnssd.BrowseOptions{
		ServiceType: svcType,
		Duration:    duration,
	})
	if err != nil {
		if errors.Is(err, dnssd.ErrUnsupported) {
			return fmt.Errorf("%w: install Wireshark? no — install avahi-utils (Linux) or Bonjour SDK (macOS/Windows)", errMdnsToolNotFound)
		}
		return err
	}
	if len(services) == 0 {
		fmt.Println("no responders found on this subnet")
		return nil
	}
	fmt.Printf("\n%-30s %-22s %-5s %-30s %s\n", "NAME", "HOST", "PORT", "HOSTNAME", "TXT")
	fmt.Println(strings.Repeat("-", 110))
	for _, s := range services {
		txt := formatTXT(s.TXT)
		fmt.Printf("%-30s %-22s %-5d %-30s %s\n", s.Name, s.Host, s.Port, s.Hostname, txt)
	}
	fmt.Printf("\n%d responder(s) found\n", len(services))
	return nil
}

// protocolMDNSService maps a dhs protocol name onto the conventional
// DNS-SD service type advertised in the wild.
func protocolMDNSService(proto string) string {
	switch proto {
	case "emberplus":
		return "_ember._tcp"
	case "nmos":
		return "_nmos-node._tcp"
	}
	return ""
}

// formatTXT renders a TXT map in deterministic key order.
func formatTXT(txt map[string]string) string {
	if len(txt) == 0 {
		return ""
	}
	keys := make([]string, 0, len(txt))
	for k := range txt {
		keys = append(keys, k)
	}
	// Simple sort (avoid sort import bloat — verb already imports strings).
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%s=%s", k, txt[k])
	}
	return b.String()
}
