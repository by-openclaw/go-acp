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
//	.cache/manifest/cerebrum-nb/<ip>.json      device → sub-device → DM ref (ADR-0028 IP key)
//
// The walk contract is `tree --device`'s, verbatim (seeded start
// groups, recursion into group candidates, self-echo leaf
// re-classification — live-proven on the NOC 2026-08-16); the row →
// canonical mapping is `validate --out-tree`'s, verbatim
// (cerebrum.CanonicalDeviceObject) — so a live extract and a
// capture-derived tree of the same device stay byte-comparable.
//
// Identity: the SAME discovery as acp2's IdentityProbe, over NB —
// the device object tree exposes the identical identity objects
// (owner-confirmed 2026-08-17), addressed by the acp2 tree labels
// root-stripped: "IDENTITY.Card Name" + "IDENTITY.Product Version"
// (fallback "BOARD.Hardware Version"). So a cerebrum extract of a
// CONVERT lands under the same <Model@SwRev>.json name as the acp2
// extract of that card — the S9 dual-oracle diff needs zero renames.
// --product / --version are overrides for devices whose tree carries
// no identity objects.
func cerebrumExtract(ctx context.Context, args []string) error {
	_ = ctx
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb extract", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	device := fs.String("device", "", "device to extract: NAME with --by-name (exact string incl. whitespace!) or IP")
	byName := fs.Bool("by-name", false, "--device is a DEVICE_NAME (exact, incl. trailing whitespace) instead of an IP")
	subDev := fs.String("sub-device", "", "sub-device index (from device-details, e.g. 1)")
	focus := fs.String("path", "", "optional start group(s) for the walk, ';'-separated. Omitted (or \".\"): the root is DISCOVERED automatically — the probe ladder tries the §5.4.3 root spellings and seeds the walk from whatever the server enumerates")
	maxReq := fs.Int("max-requests", 2000, "cap on group obtains for the walk (safety against unexpected fan-out)")
	product := fs.String("product", "", `override the DM Model (default: "IDENTITY.Card Name" obtained from the device tree — same identity object acp2's probe reads)`)
	version := fs.String("version", "", `override the DM SwRev (default: "IDENTITY.Product Version" from the device tree, falling back to "BOARD.Hardware Version")`)
	refresh := fs.Bool("refresh", false, "re-walk even when the DM for this Model@SwRev already exists (default: cache hit = zero walk — schema is captured once per card+firmware, ADR-0028)")
	if err := parseVerbFlags(fs, args); err != nil {
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

	// Identity probe — the same discovery acp2's IdentityProbe does,
	// over NB. Object labels are DRIVER-exact (case included): the
	// Neuron family exposes "IDENTITY.Card Name" / "IDENTITY.Product
	// Version", the Synapse family "Identity.Card name" / "Identity.
	// Software rev" (both live-verified 2026-08-18). The ladder tries
	// each family in turn; --product/--version skip their obtains.
	deviceName := *device
	model := *product
	swRev := *version
	probe := func(candidates ...string) string {
		for _, obj := range candidates {
			if v := cerebrumObtainIdentityLeaf(sess, cf.timeout, *device, *byName, *subDev, obj); v != "" {
				return v
			}
		}
		return ""
	}
	if model == "" {
		model = probe("IDENTITY.Card Name", "Identity.Card name")
	}
	if swRev == "" {
		swRev = probe("IDENTITY.Product Version", "Identity.Software rev", "BOARD.Hardware Version", "Identity.Hardware rev")
	}
	// Contract (ADR-0022): the DM is keyed by Model@SwRev — but that is a
	// key, not an operator input. A device with no identity object (the
	// Cerebrum SERVER itself, and any node that simply does not publish one)
	// must STILL walk and store: fall back to the device's own address/name
	// as Model and "unknown" as SwRev, loudly. --product / --version
	// override; they are never required.
	identityFromTree := model != "" && swRev != ""
	if model == "" {
		model = sanitizeKey(strings.TrimSpace(deviceName))
		if model == "" {
			model = "device"
		}
		fmt.Fprintf(os.Stderr, "cerebrum-nb extract: NOTE — no identity object in the tree; Model defaulted to %q (override with --product)\n", model)
	}
	if swRev == "" {
		swRev = "unknown"
		fmt.Fprintf(os.Stderr, "cerebrum-nb extract: NOTE — no version object in the tree; SwRev defaulted to %q (override with --version)\n", swRev)
	}
	if identityFromTree {
		fmt.Printf("identity: %s@%s (from the device tree — same objects as the acp2 probe)\n", model, swRev)
	} else {
		fmt.Printf("identity: %s@%s (defaulted — device published no identity object)\n", model, swRev)
	}

	// ADR-0028 manifest key = the device's OWN IP (never the NB server
	// endpoint — every device behind one Cerebrum would collide on it).
	// Known only when --device was addressed by IP; the by-name form
	// falls back to the name slug, loudly.
	deviceIP := ""
	if !*byName {
		deviceIP = *device
	} else {
		fmt.Fprintf(os.Stderr, "cerebrum-nb extract: NOTE — manifest keyed by name slug (device IP unknown when addressing --by-name); address --device by IP for the IP-keyed manifest (ADR-0028)\n")
	}
	identity := model + "@" + swRev

	// DM-cache skip (ADR-0028 §6: schema once, state on demand): the
	// identity probe cost 2–3 obtains; if this Model@SwRev is already
	// cached, a 38k-object walk buys nothing. The manifest binding for
	// THIS unit is still written (a second unit of a known card is new
	// inventory, not a new schema).
	if !*refresh {
		if dmPath, hit := cerebrumDMCacheHit(identity); hit {
			fmt.Printf("DM cache hit:     %s — zero walk (--refresh forces a re-walk)\n", dmPath)
			mfPath, merr := writeCerebrumManifest(identity, deviceName, deviceIP, host, port, *subDev)
			if merr != nil {
				return merr
			}
			fmt.Printf("manifest written: %s\n", mfPath)
			return nil
		}
	}

	// No seeds given → discover the root (acp2/ember parity: nobody
	// should need to know the DM structure up front).
	if len(starts) == 0 {
		discovered, spelling, derr := cerebrumDiscoverRootGroups(sess, cf.timeout, *device, *byName, *subDev)
		if derr != nil {
			return fmt.Errorf("cerebrum-nb extract: root discovery failed (%w) — seed the walk manually with --path \"GROUP[;GROUP…]\" (top folders from the device's Object Browser) and report which root spelling your server wants", derr)
		}
		starts = discovered
		fmt.Printf("root discovered via %s: %d top group(s): %s\n",
			spelling, len(starts), strings.Join(starts, "; "))
	}

	rows, requests, err := cerebrumWalkSeeds(sess, cf.timeout, *device, *byName, *subDev, starts, *maxReq, "extract")
	if err != nil {
		return err
	}
	fmt.Printf("extracted %d object(s) from %q sub-device %s in %d obtain(s)\n",
		len(rows), deviceName, *subDev, requests)

	// DM paths are device-agnostic (ADR-0022: the schema of one card
	// type, acp2 parity — slot/host never persisted): no device-name
	// or sub-device prefix. The manifest carries the binding.
	objs := make([]consumer.Object, 0, len(rows))
	for i := range rows {
		objs = append(objs, cerebrum.CanonicalDeviceObject("", "", &rows[i], i))
	}

	dmPath, mfPath, err := writeCerebrumExtract(identity, deviceName, deviceIP, host, port, *subDev, objs)
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

// cerebrumWalkSeeds is the per-seed lenient walk shared by extract and
// export --device: a card variant may lack one of the seeded top
// folders, and one missing group must not abort the whole read — it is
// skipped LOUDLY and listed in a SKIPPED summary (never silent).
// All-seeds-failed and truncation still error; a truncated read is
// refused outright (a partial model is not a device model).
func cerebrumWalkSeeds(sess *cerebrum.Session, timeout time.Duration, device string, byName bool, subDev string, starts []string, maxReq int, verb string) ([]codec.DeviceObjectValue, int, error) {
	var rows []codec.DeviceObjectValue
	requests := 0
	var failedSeeds []string
	for _, start := range starts {
		r, req, truncated, werr := cerebrumDeviceWalkValues(sess, timeout, device, byName, subDev, []string{start}, maxReq-requests)
		requests += req
		if werr != nil {
			fmt.Fprintf(os.Stderr, "cerebrum-nb %s: WARNING — start group %q failed (%v); continuing with the remaining groups\n", verb, start, werr)
			failedSeeds = append(failedSeeds, start)
			continue
		}
		if truncated {
			return nil, requests, fmt.Errorf("cerebrum-nb %s: walk truncated at --max-requests=%d obtains — refusing a partial read; raise --max-requests and re-run", verb, maxReq)
		}
		rows = append(rows, r...)
	}
	if len(failedSeeds) == len(starts) {
		return nil, requests, fmt.Errorf("cerebrum-nb %s: every start group failed — nothing read (check --path against the device's Object Browser)", verb)
	}
	if len(failedSeeds) > 0 {
		fmt.Fprintf(os.Stderr, "cerebrum-nb %s: %d start group(s) SKIPPED: %s — the result covers the remaining groups only\n", verb, len(failedSeeds), strings.Join(failedSeeds, "; "))
	}
	if len(rows) == 0 {
		return nil, requests, fmt.Errorf("cerebrum-nb %s: walk returned 0 objects (check --path start groups against the device's Object Browser)", verb)
	}
	return rows, requests, nil
}

// cerebrumDiscoverRootGroups probes the §5.4.3 root-handle spellings
// until one enumerates the device's top groups — the NB analogue of
// acp2 walking children from object 1 and ember GetDirectory on root.
// Only "no OBJECT attribute" was ever tried live (NACK 10,
// 2026-08-16); the ladder covers the spellings never sent:
//
//	OBJECT=""        literal empty attribute (Add() drops empties, so
//	                 this frame differs from the no-attr form)
//	OBJECT="ROOT-NODE-V2"  the acp2 root's own name — NB paths are
//	                 that tree root-stripped, so the root itself is
//	                 the natural handle
//	OBJECT="*"       the wildcard form §5.4.3 uses for table paths
//	no OBJECT attr   the known-NACK form, retried last for the record
//
// Returns the child group names + the winning spelling. Every rung's
// failure is collected so the error names what was refused.
func cerebrumDiscoverRootGroups(sess *cerebrum.Session, timeout time.Duration, device string, byName bool, subDev string) ([]string, string, error) {
	type rung struct {
		object   string
		explicit bool
		label    string
	}
	ladder := []rung{
		{"", true, `OBJECT=""`},
		{"ROOT-NODE-V2", false, `OBJECT="ROOT-NODE-V2"`},
		{"*", false, `OBJECT="*"`},
		{"", false, "no OBJECT attribute"},
	}
	var trail []string
	for _, r := range ladder {
		dc := &codec.DeviceChange{Type: "VALUE", SubDevice: subDev, Object: r.object, ExplicitEmptyObject: r.explicit}
		if byName {
			dc.DeviceName = device
		} else {
			dc.IPAddress = device
		}
		got, err := obtainSingleDeviceChange(sess, timeout, dc, "VALUE")
		if err != nil {
			trail = append(trail, fmt.Sprintf("%s → %v", r.label, err))
			continue
		}
		if got == nil || got.Device == nil {
			trail = append(trail, r.label+" → empty reply")
			continue
		}
		var groups []string
		seen := map[string]bool{}
		for _, ov := range got.Device.ObjectValues {
			// Children only: skip a self-echo of the probe object and
			// blank rows. The walk re-classifies group vs leaf itself.
			if ov.Object == "" || ov.Object == r.object || seen[ov.Object] {
				continue
			}
			seen[ov.Object] = true
			groups = append(groups, ov.Object)
		}
		if len(groups) > 0 {
			return groups, r.label, nil
		}
		trail = append(trail, r.label+" → no children")
	}
	return nil, "", fmt.Errorf("every root spelling refused: %s", strings.Join(trail, " | "))
}

// cerebrumObtainIdentityLeaf reads one identity object via a single
// §5.4.3 VALUE obtain (the leaf self-echo shape). Empty string on any
// refusal / unavailable / missing row — the caller decides whether an
// override or an error takes over. Addressing mirrors the walk
// (DEVICE_NAME verbatim or IP).
func cerebrumObtainIdentityLeaf(sess *cerebrum.Session, timeout time.Duration, device string, byName bool, subDev, object string) string {
	dc := &codec.DeviceChange{Type: "VALUE", SubDevice: subDev, Object: object}
	if byName {
		dc.DeviceName = device
	} else {
		dc.IPAddress = device
	}
	got, err := obtainSingleDeviceChange(sess, timeout, dc, "VALUE")
	if err != nil || got == nil || got.Device == nil {
		return ""
	}
	for _, ov := range got.Device.ObjectValues {
		if ov.Object == object && ov.Available {
			return strings.TrimSpace(ov.Value)
		}
	}
	return ""
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
func writeCerebrumExtract(identity, deviceName, deviceIP, host string, port int, subDev string, objs []consumer.Object) (dmPath, mfPath string, err error) {
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

	mfPath, err = writeCerebrumManifest(identity, deviceName, deviceIP, host, port, subDev)
	if err != nil {
		return "", "", err
	}
	return dmPath, mfPath, nil
}

// writeCerebrumManifest writes ONLY the manifest binding — used by the
// DM-cache-hit path too, where a second unit of a known card is new
// inventory (its own manifest) but not a new schema (no DM write).
func writeCerebrumManifest(identity, deviceName, deviceIP, host string, port int, subDev string) (string, error) {
	if treeStore == nil {
		return "", fmt.Errorf("cerebrum-nb extract: tree store not initialised (.cache unavailable)")
	}
	mf := &manifest.Manifest{
		Device: manifest.Device{
			// Name is the exact wire DEVICE_NAME (incl. the live
			// trailing-whitespace quirk) trimmed for display; Addr
			// keeps it verbatim for addressing. IP is the ADR-0028
			// file key — the device's own IP, not the NB endpoint.
			Name:     strings.TrimSpace(deviceName),
			Protocol: "cerebrum-nb",
			IP:       deviceIP,
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
	mfPath, err := manifest.Write(treeStore.BaseDir(), mf)
	if err != nil {
		return "", fmt.Errorf("cerebrum-nb extract: write manifest: %w", err)
	}
	return mfPath, nil
}

// cerebrumDMCacheHit reports whether the DM for identity already
// exists in the store (ADR-0028 §6: schema captured once per
// card+firmware — an existing file means zero walk).
func cerebrumDMCacheHit(identity string) (string, bool) {
	if treeStore == nil {
		return "", false
	}
	p := treeStore.IdentityPath("cerebrum-nb", identity)
	if p == "" {
		return "", false
	}
	if _, err := os.Stat(p); err != nil {
		return "", false
	}
	return p, true
}
