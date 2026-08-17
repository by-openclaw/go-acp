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

	// Identity probe — acp2's IdentityProbe over NB: the device tree
	// exposes the same identity objects, addressed by the acp2 labels
	// root-stripped. Card Name is the Model; Product Version is the
	// SwRev (Hardware Version fallback, same preference order as
	// acp2). Overrides skip their obtain.
	deviceName := *device
	model := *product
	swRev := *version
	if model == "" {
		model = cerebrumObtainIdentityLeaf(sess, cf.timeout, *device, *byName, *subDev, "IDENTITY.Card Name")
	}
	if swRev == "" {
		swRev = cerebrumObtainIdentityLeaf(sess, cf.timeout, *device, *byName, *subDev, "IDENTITY.Product Version")
	}
	if swRev == "" {
		swRev = cerebrumObtainIdentityLeaf(sess, cf.timeout, *device, *byName, *subDev, "BOARD.Hardware Version")
	}
	if model == "" {
		return fmt.Errorf("cerebrum-nb extract: no Model — the device tree answered neither \"IDENTITY.Card Name\" nor an override; pass --product explicitly")
	}
	if swRev == "" {
		return fmt.Errorf("cerebrum-nb extract: no SwRev — the device tree answered neither \"IDENTITY.Product Version\" nor \"BOARD.Hardware Version\"; pass --version explicitly")
	}
	fmt.Printf("identity: %s@%s (from the device tree — same objects as the acp2 probe)\n", model, swRev)

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

	// Walk seed-by-seed: a card variant may lack one of the seeded top
	// folders (CONVERT IP vs Hybrid), and one missing group must not
	// abort the whole model — it is skipped LOUDLY and listed in the
	// summary. All-seeds-failed still errors.
	var rows []codec.DeviceObjectValue
	requests := 0
	var failedSeeds []string
	for _, start := range starts {
		r, req, truncated, werr := cerebrumDeviceWalkValues(sess, cf.timeout, *device, *byName, *subDev, []string{start}, *maxReq-requests)
		requests += req
		if werr != nil {
			fmt.Fprintf(os.Stderr, "cerebrum-nb extract: WARNING — start group %q failed (%v); continuing with the remaining groups\n", start, werr)
			failedSeeds = append(failedSeeds, start)
			continue
		}
		if truncated {
			return fmt.Errorf("cerebrum-nb extract: walk truncated at --max-requests=%d obtains — a truncated DM is not a device model; raise --max-requests and re-run", *maxReq)
		}
		rows = append(rows, r...)
	}
	if len(failedSeeds) == len(starts) {
		return fmt.Errorf("cerebrum-nb extract: every start group failed — nothing to persist (check --path against the device's Object Browser)")
	}
	if len(failedSeeds) > 0 {
		fmt.Fprintf(os.Stderr, "cerebrum-nb extract: %d start group(s) SKIPPED: %s — the DM covers the remaining groups only\n", len(failedSeeds), strings.Join(failedSeeds, "; "))
	}
	if len(rows) == 0 {
		return fmt.Errorf("cerebrum-nb extract: walk returned 0 objects — nothing to persist (check --path start groups against the device's Object Browser)")
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

	identity := model + "@" + swRev
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
