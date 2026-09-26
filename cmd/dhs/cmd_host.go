// runHost implements `dhs host <verb>` — facts about the machine dhs runs on,
// not about a device it talks to.
//
//	info   the host, its network interfaces, and the switch + port each
//	       interface is plugged into, heard over LLDP (#1147)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"dhs/internal/lldp"
)

func runHost(ctx context.Context, args []string) error {
	if len(args) == 0 || isHelpToken(args[0]) {
		fmt.Println(hostHelp)
		return nil
	}
	switch args[0] {
	case "info":
		return runHostInfo(ctx, args[1:], os.Stdout, os.Stderr)
	default:
		return fmt.Errorf("unknown host verb %q (verbs: info)", args[0])
	}
}

const hostHelp = `dhs host — the machine dhs runs on

USAGE
  dhs host info [--iface NAME] [--window 35s] [--json]

  Lists this machine's network interfaces and, for each, the switch and
  port it is plugged into, as the switch announces it over LLDP.`

// lldpTxInterval is IEEE 802.1AB's default msgTxInterval: a switch announces
// itself on each port every 30 s, so a shorter wait can miss it on a link
// that is working perfectly.
const lldpTxInterval = 30 * time.Second

// hostCapture is the LLDP source `host info` listens with. Tests replace it.
type hostCapture func(ctx context.Context, iface string, window time.Duration,
	until func(map[string]lldp.Neighbor) bool) (map[string]lldp.Neighbor, error)

func realHostCapture(ctx context.Context, iface string, window time.Duration,
	until func(map[string]lldp.Neighbor) bool) (map[string]lldp.Neighbor, error) {
	return lldp.Capture{Iface: iface, Window: window, Until: until}.Neighbors(ctx)
}

func runHostInfo(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return hostInfo(ctx, args, stdout, stderr, lldp.CaptureInterfaces, realHostCapture)
}

func hostInfo(ctx context.Context, args []string, stdout, stderr io.Writer,
	interfaces func(string) ([]net.Interface, error), capture hostCapture) error {
	fs := flag.NewFlagSet("host info", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		iface  = fs.String("iface", "", "one interface only (default: every interface that is up, not loopback, with an Ethernet address)")
		window = fs.Duration("window", lldpTxInterval+5*time.Second, "how long to listen for LLDP; switches announce every 30s by default, so less can miss one. Stops early once every interface has been heard")
		asJSON = fs.Bool("json", false, "print JSON instead of a table")
	)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, hostHelp+`

  Capture needs, per OS:
    Linux    CAP_NET_RAW on the binary (setcap cap_net_raw+ep), or root
    macOS    root, or read access to /dev/bpf* (the access_bpf group)
    Windows  Npcap installed (https://npcap.com/) — not shipped with dhs

FLAGS`)
		fs.PrintDefaults()
	}
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}

	ifs, err := interfaces(*iface)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(ifs))
	for _, i := range ifs {
		names = append(names, i.Name)
	}
	var (
		heard   map[string]lldp.Neighbor
		lldpErr error
	)
	if len(ifs) > 0 {
		_, _ = fmt.Fprintf(stderr, "listening for LLDP on %s for up to %s …\n", strings.Join(names, ", "), *window)
		heard, lldpErr = capture(ctx, *iface, *window, func(m map[string]lldp.Neighbor) bool {
			for _, n := range names {
				if _, ok := m[n]; !ok {
					return false
				}
			}
			return true
		})
	}

	report := hostReport{Host: hostname(), OS: runtime.GOOS, Arch: runtime.GOARCH}
	for _, i := range ifs {
		r := interfaceReport{Name: i.Name, MAC: i.HardwareAddr.String(), Addresses: addresses(i)}
		if nb, ok := heard[i.Name]; ok {
			r.Switch = neighborReport(nb)
		}
		report.Interfaces = append(report.Interfaces, r)
	}
	if lldpErr != nil {
		report.LLDPError = lldpErr.Error()
	}

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	} else {
		printHostReport(stdout, report, *window)
	}
	if lldpErr != nil && !errors.Is(lldpErr, context.Canceled) {
		return fmt.Errorf("host info: LLDP: %w", lldpErr)
	}
	return nil
}

type hostReport struct {
	Host       string            `json:"host"`
	OS         string            `json:"os"`
	Arch       string            `json:"arch"`
	Interfaces []interfaceReport `json:"interfaces"`
	LLDPError  string            `json:"lldp_error,omitempty"`
}

type interfaceReport struct {
	Name      string        `json:"name"`
	MAC       string        `json:"mac"`
	Addresses []string      `json:"addresses"`
	Switch    *switchReport `json:"switch"`
}

// switchReport is what the switch at the other end of the cable said.
type switchReport struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	ChassisID   string `json:"chassis_id"`
	PortID      string `json:"port_id"`
	PortDesc    string `json:"port_description,omitempty"`
	MgmtAddr    string `json:"management_address,omitempty"`
	TTLSeconds  int    `json:"ttl_s"`
}

func neighborReport(nb lldp.Neighbor) *switchReport {
	r := &switchReport{
		Name:        nb.SysName,
		Description: nb.SysDesc,
		ChassisID:   nb.ChassisID,
		PortID:      nb.PortID,
		PortDesc:    nb.PortDesc,
		TTLSeconds:  int(nb.TTL / time.Second),
	}
	if nb.MgmtAddr != nil {
		r.MgmtAddr = nb.MgmtAddr.String()
	}
	return r
}

func printHostReport(w io.Writer, r hostReport, window time.Duration) {
	_, _ = fmt.Fprintf(w, "HOST  %s  (%s/%s)\n\n", r.Host, r.OS, r.Arch)
	if len(r.Interfaces) == 0 {
		_, _ = fmt.Fprintln(w, "no interface is up with an Ethernet address")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "INTERFACE\tMAC\tADDRESSES\tSWITCH\tPORT\tPORT DESCRIPTION\tSWITCH MGMT")
	for _, i := range r.Interfaces {
		addrs := strings.Join(i.Addresses, " ")
		if addrs == "" {
			addrs = "-"
		}
		if i.Switch == nil {
			what := fmt.Sprintf("(nothing heard in %s)", window)
			if r.LLDPError != "" {
				what = "(LLDP unavailable)"
			}
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t\t\t\n", i.Name, i.MAC, addrs, what)
			continue
		}
		s := i.Switch
		name := s.Name
		if name == "" {
			name = s.ChassisID
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			i.Name, i.MAC, addrs, name, s.PortID, dash(s.PortDesc), dash(s.MgmtAddr))
	}
	_ = tw.Flush()
	if r.LLDPError != "" {
		_, _ = fmt.Fprintf(w, "\nLLDP: %s\n", r.LLDPError)
	}
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func addresses(i net.Interface) []string {
	out := []string{}
	addrs, err := i.Addrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		out = append(out, a.String())
	}
	return out
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "?"
	}
	return h
}
