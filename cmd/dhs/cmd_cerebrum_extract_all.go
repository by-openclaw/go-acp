package main

// `dhs consumer cerebrum-nb extract-all` — one device model per device, for
// everything Cerebrum knows.
//
// Northbound cannot list a device's root: every spelling is refused on 2.9.3.
// A walk therefore needs its start groups from somewhere else, and there are
// two places: the device's model under another protocol, since a Neuron's
// groups are the same over ACP2 and Northbound, and --roots, a file holding
// the folders of the Cerebrum Object Browser for what has no other model.

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/cerebrum-nb/codec"
)

// cerebrumSiblingRoots reads the top groups of a device's model under another
// protocol: the exact version first, then any version of that model.
func cerebrumSiblingRoots(model, swRev string) []string {
	if treeStore == nil {
		return nil
	}
	p := treeStore.IdentityPath("acp2", model+"@"+swRev)
	if p == "" {
		return nil
	}
	if roots := cerebrumRootsOfDM(p); len(roots) > 0 {
		return roots
	}
	others, _ := filepath.Glob(filepath.Join(filepath.Dir(p), model+"@*.json"))
	sort.Strings(others)
	for _, o := range others {
		if roots := cerebrumRootsOfDM(o); len(roots) > 0 {
			return roots
		}
	}
	return nil
}

// cerebrumRootsOfDM is the second path element of every object: the first is
// the ACP2 root, which Northbound paths leave out.
func cerebrumRootsOfDM(path string) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var dm struct {
		Objects []struct {
			Path []string `json:"path"`
		} `json:"objects"`
	}
	if json.Unmarshal(raw, &dm) != nil {
		return nil
	}
	seen := map[string]bool{}
	var roots []string
	for _, o := range dm.Objects {
		if len(o.Path) > 1 && !seen[o.Path[1]] {
			seen[o.Path[1]] = true
			roots = append(roots, o.Path[1])
		}
	}
	return roots
}

// cerebrumExtractTarget is one sub-device to model.
type cerebrumExtractTarget struct {
	IP, Name, Sub string
}

// cerebrumExtractTargets lists every sub-device of a device: a device with
// none is its own sub-device 0.
func cerebrumExtractTargets(ip, name string, subs []codec.DeviceEntry) []cerebrumExtractTarget {
	if len(subs) == 0 {
		return []cerebrumExtractTarget{{IP: ip, Name: name, Sub: "0"}}
	}
	out := make([]cerebrumExtractTarget, 0, len(subs))
	for _, s := range subs {
		out = append(out, cerebrumExtractTarget{IP: ip, Name: name, Sub: strconv.Itoa(s.Index)})
	}
	return out
}

func cerebrumExtractAll(ctx context.Context, args []string) error {
	roots, args, err := extractStringFlag(args, "--roots")
	if err != nil {
		return err
	}
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb extract-all", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	refresh := fs.Bool("refresh", false, "re-walk even when the DM already exists")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if len(fs.Args()) < 1 {
		return cerebrumValErr("extract-all", "missing host[:port] argument")
	}
	seeds := map[string][]string{}
	if roots != "" {
		raw, rerr := os.ReadFile(roots)
		if rerr != nil {
			return rerr
		}
		if rerr := json.Unmarshal(raw, &seeds); rerr != nil {
			return fmt.Errorf("cerebrum-nb extract-all: --roots %s: %w (want {\"<ip>/<sub-device>\": [\"FOLDER\", …]})", roots, rerr)
		}
	}

	targets, err := cerebrumListExtractTargets(cf, fs.Args())
	if err != nil {
		return err
	}
	// A session per device: one Northbound licence at a time.
	var failed []string
	for _, t := range targets {
		key := t.IP + "/" + t.Sub
		sub := append([]string{}, args...)
		// A CONVERT is 49 880 objects: well past extract's default cap.
		sub = append(sub, "--device", t.IP, "--sub-device", t.Sub, "--max-requests", "50000")
		if *refresh {
			sub = append(sub, "--refresh")
		}
		if s, ok := seeds[key]; ok {
			sub = append(sub, "--path", strings.Join(s, ";"))
		}
		fmt.Printf("=== %s %q sub-device %s\n", t.IP, t.Name, t.Sub)
		if xerr := cerebrumExtract(ctx, sub); xerr != nil {
			fmt.Printf("FAILED %s: %v\n", key, xerr)
			failed = append(failed, key)
		}
	}
	fmt.Printf("extract-all: %d of %d sub-devices modelled\n", len(targets)-len(failed), len(targets))
	if len(failed) > 0 {
		return fmt.Errorf("cerebrum-nb extract-all: no model for %s: add their Object Browser folders to --roots", strings.Join(failed, ", "))
	}
	return nil
}

// cerebrumListExtractTargets reads the LIST and each device's sub-devices.
// Cerebrum itself is 0.0.0.0 and is not in its own LIST.
func cerebrumListExtractTargets(cf *cerebrumFlags, positionals []string) ([]cerebrumExtractTarget, error) {
	p, sess, _, err := dialCerebrum(cf, positionals, "extract-all")
	if err != nil {
		return nil, err
	}
	defer func() { _ = p.Disconnect() }()
	got, err := obtainSingleDeviceChange(sess, cf.timeout, &codec.DeviceChange{Type: "LIST"}, "LIST")
	if err != nil {
		return nil, fmt.Errorf("cerebrum-nb extract-all: device LIST: %w", err)
	}
	if got == nil || got.Device == nil {
		return nil, fmt.Errorf("cerebrum-nb extract-all: no device LIST within %s", cf.timeout)
	}
	ips := []string{"0.0.0.0"}
	seen := map[string]bool{"0.0.0.0": true}
	for _, d := range got.Device.Devices {
		if !seen[d.IPAddress] {
			seen[d.IPAddress] = true
			ips = append(ips, d.IPAddress)
		}
	}
	var out []cerebrumExtractTarget
	for _, ip := range ips {
		dt, derr := obtainSingleDeviceChange(sess, cf.timeout, &codec.DeviceChange{Type: "DETAILS", IPAddress: ip, DeviceType: "DEVICE"}, "DETAILS")
		if derr != nil || dt == nil || dt.Device == nil {
			// A row with no DEVICE class, a pure router: nothing to walk.
			fmt.Fprintf(os.Stderr, "cerebrum-nb extract-all: %s: no DEVICE details, skipped (%v)\n", ip, derr)
			continue
		}
		name := ""
		if dt.Device.Details != nil {
			name = dt.Device.Details.Name
		}
		out = append(out, cerebrumExtractTargets(ip, name, dt.Device.SubDevices)...)
	}
	return out, nil
}
