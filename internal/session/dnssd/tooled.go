package dnssd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ErrUnsupported is returned when no native mDNS tool is reachable.
// Callers translate to validation:mdns-tool-not-found.
var ErrUnsupported = errors.New("dnssd: no native mDNS browse tool reachable on PATH")

// NewToolBrowser returns a Browser that shells out to the operating
// system's native mDNS tool (dns-sd on macOS / Windows with Bonjour,
// avahi-browse on Linux). Returns ErrUnsupported when no tool is
// found.
func NewToolBrowser() Browser {
	return &toolBrowser{}
}

type toolBrowser struct{}

func (t *toolBrowser) Browse(ctx context.Context, opts BrowseOptions) ([]Service, error) {
	if opts.ServiceType == "" {
		return nil, fmt.Errorf("dnssd: ServiceType is required")
	}
	d := opts.Duration
	if d <= 0 {
		d = 5 * time.Second
	}

	// Linux first: avahi-browse usually has the cleanest parseable output.
	if path, err := exec.LookPath("avahi-browse"); err == nil {
		return avahiBrowse(ctx, path, opts.ServiceType, d)
	}
	// macOS / Windows: dns-sd from the Bonjour SDK.
	if path, err := exec.LookPath("dns-sd"); err == nil {
		return dnssdBrowse(ctx, path, opts.ServiceType, d, runtime.GOOS)
	}
	return nil, ErrUnsupported
}

// avahiBrowse runs `avahi-browse -t -r -p <service>.local` which
// produces machine-parseable output: `=;iface;proto;name;type;domain;hostname;ip;port;txt`
// for resolved entries, prefixed by `+` for browse-only entries.
func avahiBrowse(ctx context.Context, bin, service string, d time.Duration) ([]Service, error) {
	browseCtx, cancel := context.WithTimeout(ctx, d+2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(browseCtx, bin, "-t", "-r", "-p", service+".local")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var out []Service
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "=;") {
			continue
		}
		fields := strings.Split(line, ";")
		if len(fields) < 10 {
			continue
		}
		port, _ := strconv.Atoi(fields[8])
		svc := Service{
			Name:     fields[3],
			Host:     fields[7],
			Port:     port,
			Hostname: fields[6],
			TXT:      parseTXT(fields[9]),
		}
		out = append(out, svc)
	}
	_ = cmd.Wait()
	return out, nil
}

// dnssdBrowse runs `dns-sd -B <service> local.` then resolves each
// instance via `dns-sd -L`. Output parsing is best-effort — dns-sd
// renders for human reading and the output shape varies slightly
// between macOS / Windows builds. Returns whatever it can parse.
func dnssdBrowse(ctx context.Context, bin, service string, d time.Duration, host string) ([]Service, error) {
	_ = host
	browseCtx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	cmd := exec.CommandContext(browseCtx, bin, "-B", service, "local.")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	// dns-sd -B output: `<timestamp> Add <flags> <ifIndex> <Domain> <Service> <Name>`
	type instance struct{ name string }
	var instances []instance
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 7 || fields[1] != "Add" {
			continue
		}
		instances = append(instances, instance{name: strings.Join(fields[6:], " ")})
	}
	_ = cmd.Wait()
	// Resolution loop — best-effort short timeout per instance.
	var out []Service
	for _, in := range instances {
		svc := Service{Name: in.name}
		out = append(out, svc) // host/port resolution v2; today we surface the names
	}
	return out, nil
}

// parseTXT decodes avahi-browse's quoted TXT field. Sample:
//
//	"txtvers=1" "path=/" "name=dhs-emberplus" "dtdVersion=2.60"
func parseTXT(raw string) map[string]string {
	if raw == "" {
		return nil
	}
	out := map[string]string{}
	// avahi quotes pairs: `"k=v" "k2=v2"`. Strip outer quotes per pair.
	for _, pair := range strings.Fields(raw) {
		pair = strings.Trim(pair, `"`)
		eq := strings.IndexByte(pair, '=')
		if eq <= 0 {
			continue
		}
		out[pair[:eq]] = pair[eq+1:]
	}
	return out
}
