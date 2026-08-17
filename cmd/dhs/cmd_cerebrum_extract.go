package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"dhs/internal/cerebrum-nb/codec"
	cerebrum "dhs/internal/cerebrum-nb/consumer"
	"dhs/internal/consumer"
	"dhs/internal/datastore"
	"dhs/internal/manifest"
)

// cerebrumExtract drives `dhs consumer cerebrum-nb extract` — D2 unit
// (b), issue #700: walk one device's §5.4.3 object tree over VALUE
// obtains and persist the ADR-0022 card data model + manifest:
//
//	.cache/dm/cerebrum-nb/<Model@SwRev>.json   flat canonical Objects
//	.cache/manifest/<device-slug>.json         device → sub-device → DM ref
//
// The walk contract is `tree --device`'s, verbatim (seeded start
// groups, recursion into group candidates, self-echo leaf
// re-classification — live-proven on the NOC 2026-08-16); the row →
// canonical mapping is `validate --out-tree`'s, verbatim
// (cerebrum.CanonicalDeviceObject) — so a live extract and a
// capture-derived tree of the same device stay byte-comparable.
//
// Identity: Model is auto-probed from the device DETAILS reply
// (vendor type), overridable with --product. The NB wire exposes NO
// firmware / software version anywhere (DETAILS carries ip/name/
// vendor only), so --version is required from the operator.
func cerebrumExtract(ctx context.Context, args []string) error {
	_ = ctx
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb extract", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	device := fs.String("device", "", "device to extract: NAME with --by-name (exact string incl. whitespace!) or IP")
	byName := fs.Bool("by-name", false, "--device is a DEVICE_NAME (exact, incl. trailing whitespace) instead of an IP")
	subDev := fs.String("sub-device", "", "sub-device index (from device-details, e.g. 1)")
	focus := fs.String("path", "", "start group(s) for the walk, ';'-separated — the server refuses an empty OBJECT, so the root must be seeded (top folders from the device's Object Browser)")
	maxReq := fs.Int("max-requests", 2000, "cap on group obtains for the walk (safety against unexpected fan-out)")
	product := fs.String("product", "", "product / model identifier for the DM identity (default: probed from the device DETAILS vendor type)")
	version := fs.String("version", "", "firmware / software version for the DM identity (REQUIRED — the NB wire exposes no version)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *device == "" {
		return cerebrumValErr("extract", "--device is required (NAME with --by-name, or IP)")
	}
	if *subDev == "" {
		return cerebrumValErr("extract", "--sub-device is required (index from device-details)")
	}
	var starts []string
	for _, s := range strings.Split(*focus, ";") {
		if s = strings.TrimSpace(s); s != "" && s != "." {
			starts = append(starts, s)
		}
	}
	if len(starts) == 0 {
		return cerebrumValErr("extract", "--path is required with one or more start groups (';'-separated) — the server refuses an empty OBJECT, so the walk must be seeded")
	}
	if *version == "" {
		return cerebrumValErr("extract", "--version is required — the NB wire exposes no firmware/software version, the DM identity needs it from you (e.g. --version 6.7.4)")
	}

	rest := fs.Args()
	if len(rest) < 1 {
		return cerebrumValErr("extract", "missing host[:port] argument")
	}
	host, port, err := splitHostPort(rest[0], cf.port)
	if err != nil {
		return err
	}

	p, sess, _, err := dialCerebrum(cf, rest, "extract")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()

	// DETAILS probe: manifest device name (exact wire NAME) + Model
	// default. Lenient — a refused DETAILS only matters if --product
	// was not given. §5.4 DETAILS is IP-addressed on the wire: the
	// by-name form NACKs 10 on the live NOC (2026-08-17), so NAME
	// addressing resolves NAME → IP via LIST + per-IP DETAILS first.
	deviceName := *device
	model := *product
	var probeErr error
	if *byName {
		if ip, m, ok := cerebrumResolveDeviceByName(sess, cf.timeout, *device); ok {
			fmt.Fprintf(os.Stderr, "cerebrum-nb extract: resolved %q → %s (LIST + DETAILS join)\n", *device, ip)
			if m.Name != "" {
				deviceName = m.Name
			}
			if model == "" && m.VendorType != "" {
				model = m.VendorType
			}
		} else {
			probeErr = fmt.Errorf("device NAME %q not found in the LIST + DETAILS join", *device)
		}
	} else {
		dc := &codec.DeviceChange{Type: "DETAILS", IPAddress: *device}
		if got, derr := obtainSingleDeviceChange(sess, cf.timeout, dc, "DETAILS"); derr == nil && got != nil && got.Device != nil && got.Device.Details != nil {
			d := got.Device.Details
			if d.Name != "" {
				deviceName = d.Name
			}
			if model == "" && d.VendorType != "" {
				model = d.VendorType
			}
		} else if derr != nil {
			probeErr = derr
		}
	}
	if probeErr != nil {
		fmt.Fprintf(os.Stderr, "cerebrum-nb extract: WARNING — DETAILS probe failed (%v); using --device/--product as-is\n", probeErr)
	}
	if model == "" {
		return cerebrumValErr("extract", "no Model for the DM identity — the DETAILS probe carried no vendor type, pass --product explicitly (e.g. --product \"CONVERT Hybrid\")")
	}

	rows, requests, truncated, err := cerebrumDeviceWalkValues(sess, cf.timeout, *device, *byName, *subDev, starts, *maxReq)
	if err != nil {
		return err
	}
	if truncated {
		return fmt.Errorf("cerebrum-nb extract: walk truncated at --max-requests=%d obtains — a truncated DM is not a device model; raise --max-requests and re-run", *maxReq)
	}
	if len(rows) == 0 {
		return fmt.Errorf("cerebrum-nb extract: walk returned 0 objects — nothing to persist (check --path start groups against the device's Object Browser)")
	}
	fmt.Printf("extracted %d object(s) from %q sub-device %s in %d obtain(s)\n",
		len(rows), deviceName, *subDev, requests)

	objs := make([]consumer.Object, 0, len(rows))
	for i := range rows {
		objs = append(objs, cerebrum.CanonicalDeviceObject(deviceName, *subDev, &rows[i], i))
	}

	identity := model + "@" + *version
	dmPath, mfPath, err := writeCerebrumExtract(identity, deviceName, host, port, *subDev, objs)
	if err != nil {
		return err
	}

	fingerprint, ferr := sha256File(dmPath)
	if ferr != nil {
		return fmt.Errorf("fingerprint %s: %w", dmPath, ferr)
	}
	fmt.Printf("DM written:       %s\n", dmPath)
	fmt.Printf("  fingerprint:    %s\n", fingerprint)
	fmt.Printf("manifest written: %s\n", mfPath)
	return nil
}

// cerebrumResolveDeviceByName resolves a verbatim DEVICE_NAME to its
// IP + DETAILS meta via one §5.4.1 LIST obtain (single frame) and the
// per-IP DETAILS join — the same join list-devices --names uses.
// Exact-name match first (names are verbatim incl. trailing
// whitespace); a trimmed-space match second, so a copy-paste that
// lost the trailing blank still resolves.
func cerebrumResolveDeviceByName(sess *cerebrum.Session, timeout time.Duration, name string) (string, cerebrumDeviceMeta, bool) {
	got, err := obtainSingleDeviceChange(sess, timeout, &codec.DeviceChange{Type: "LIST"}, "LIST")
	if err != nil || got == nil || got.Device == nil {
		return "", cerebrumDeviceMeta{}, false
	}
	seen := map[string]bool{}
	var ips []string
	for _, e := range got.Device.Devices {
		if e.IPAddress != "" && !seen[e.IPAddress] {
			seen[e.IPAddress] = true
			ips = append(ips, e.IPAddress)
		}
	}
	meta := cerebrumDeviceNameJoin(sess, timeout, ips)
	for _, ip := range ips {
		if m, ok := meta[ip]; ok && m.Name == name {
			return ip, m, true
		}
	}
	for _, ip := range ips {
		if m, ok := meta[ip]; ok && strings.TrimSpace(m.Name) == strings.TrimSpace(name) {
			return ip, m, true
		}
	}
	return "", cerebrumDeviceMeta{}, false
}

// writeCerebrumExtract persists the ADR-0022 pair for one extracted
// device: the DM (flat canonical Objects — the acp1/acp2 caller
// contract of WriteDM; cerebrum device objects are flat dotted paths,
// same family) and the manifest binding device → sub-device → DM ref.
// Both land under the tree store's base dir (".cache" next to the
// binary) so dm/ and manifest/ stay siblings per ADR-0022.
//
// Pure persistence — no wire I/O — so tests drive it with fabricated
// rows against a temp store.
func writeCerebrumExtract(identity, deviceName, host string, port int, subDev string, objs []consumer.Object) (dmPath, mfPath string, err error) {
	if treeStore == nil {
		return "", "", fmt.Errorf("cerebrum-nb extract: tree store not initialised (.cache unavailable)")
	}
	if err := treeStore.WriteDM("cerebrum-nb", identity, datastore.DM{
		Protocol: "cerebrum-nb",
		Objects:  objs,
	}); err != nil {
		return "", "", fmt.Errorf("cerebrum-nb extract: write DM: %w", err)
	}
	dmPath = treeStore.IdentityPath("cerebrum-nb", identity)

	mf := &manifest.Manifest{
		Device: manifest.Device{
			// Name is the exact wire DEVICE_NAME (incl. the live
			// trailing-whitespace quirk); the slug sanitises it for
			// the filename, Addr keeps it verbatim for addressing.
			Name:     strings.TrimSpace(deviceName),
			Protocol: "cerebrum-nb",
			Endpoints: []manifest.Endpoint{
				// The endpoint is the Cerebrum NB server we consume
				// through — devices are reached via the control
				// plane, never directly.
				{IP: host, Port: port, Transport: "tcp"},
			},
		},
		Frames: []manifest.Frame{{
			Name: "device",
			Slots: []manifest.Slot{{
				Addr: map[string]any{"device": deviceName, "sub_device": subDev},
				DM:   identity + ".json",
			}},
		}},
	}
	mfPath, err = manifest.Write(treeStore.BaseDir(), mf)
	if err != nil {
		return "", "", fmt.Errorf("cerebrum-nb extract: write manifest: %w", err)
	}
	return dmPath, mfPath, nil
}
