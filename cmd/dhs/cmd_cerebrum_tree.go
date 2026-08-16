package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"dhs/internal/cerebrum-nb/codec"
	cerebrum "dhs/internal/cerebrum-nb/consumer"
	"dhs/internal/consumer"
)

// cerebrumTree renders the NB-readable catalogue with the canonical tree
// renderer (same flags and output shapes as `dhs consumer <proto> tree` —
// ASCII or PlantUML mindmap). Two domains, both strictly what the 0v16 API
// exposes — nothing extrapolated:
//
//	Salvos      §5.3  GROUP_LIST → INSTANCE_LIST → INSTANCE_DETAILS
//	            (groups → instances → metadata; salvo ITEM rows are NOT
//	            exposed over NB — live-verified 2026-08-16)
//	Categories  §5.2  CATEGORY_LIST → CATEGORY_DETAILS per category
//	            (categories → item rows: SOURCE / DEST names, nested
//	            CATEGORY references, TEXT/FILE/CUSTOM — exactly the §3.3
//	            ITEM_TYPE rows the wire returns)
func cerebrumTree(_ context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb tree", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	format := fs.String("format", "ascii", `output format: "ascii" (default) or "plantuml"`)
	depth := fs.Int("depth", 0, "max render depth from the focus node (0 = unlimited)")
	out := fs.String("out", "", "write to this file instead of stdout")
	filter := fs.String("filter", "", "case-insensitive substring filter (drops non-matching lines)")
	focus := fs.String("path", "", `focus subtree at this dotted path (e.g. "Salvos.Salvo Group 1" or "Categories.SRC-INTERPHONIE"; in --device mode: the start group, e.g. "PROCESSING AUDIO")`)
	domain := fs.String("domain", "all", "which catalogue(s) to walk: sources | dests | categories | salvos | devices | all (all = every domain — the whole-Cerebrum tree; sources/dests are the 2 wildcard MNE reads, devices is the LIST read)")
	device := fs.String("device", "", "DEVICE OBJECT TREE mode: walk one device's §5.4.3 object tree recursively (group obtains return children — live 2026-08-16). NAME with --by-name (exact string incl. whitespace!) or IP")
	byName := fs.Bool("by-name", false, "--device is a DEVICE_NAME (exact, incl. trailing whitespace) instead of an IP")
	subDev := fs.String("sub-device", "", "sub-device index for --device mode (from device-details, e.g. 1)")
	maxReq := fs.Int("max-requests", 2000, "--device mode: cap on group obtains for the walk (safety against unexpected fan-out)")
	alt := fs.Int("alt", 0, "label set for SOURCE/DEST item annotation: 0 = primary mnemonic, N = alternate set N (ALT_MNE, e.g. 1 = Panels on the NOC)")
	noMne := fs.Bool("no-mne", false, "skip the mnemonic join — show raw item IDs only (also skips the two SRCE/DEST_MNE wildcard reads)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	switch *domain {
	case "sources", "dests", "categories", "salvos", "devices", "all":
	default:
		return fmt.Errorf("cerebrum-nb tree: --domain must be sources | dests | categories | salvos | devices | all, got %q", *domain)
	}
	if *format != "ascii" && *format != "plantuml" {
		return fmt.Errorf("cerebrum-nb tree: --format must be \"ascii\" or \"plantuml\", got %q", *format)
	}

	// Open --out before any wire traffic so a bad path fails fast (missing
	// parent directories are created, same contract as --log / --out CSV).
	var writer io.Writer = os.Stdout
	if *out != "" {
		if dir := filepath.Dir(*out); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
		}
		f, ferr := os.Create(*out)
		if ferr != nil {
			return fmt.Errorf("open %s: %w", *out, ferr)
		}
		defer func() { _ = f.Close() }()
		writer = f
	}

	p, sess, _, err := dialCerebrum(cf, fs.Args(), "tree")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()

	var objs []consumer.Object
	var renderFocus string
	if *device != "" {
		if *subDev == "" {
			return fmt.Errorf("cerebrum-nb tree: --device mode needs --sub-device (index from device-details)")
		}
		// OBJECT="" is refused by the server (live: NACK 10) — the device
		// root cannot be listed, so the walk is seeded from start groups:
		// --path takes one group or a ';'-separated list (e.g. the top
		// folders from the Object Browser).
		var starts []string
		for _, s := range strings.Split(*focus, ";") {
			if s = strings.TrimSpace(s); s != "" && s != "." {
				starts = append(starts, s)
			}
		}
		if len(starts) == 0 {
			return fmt.Errorf("cerebrum-nb tree: --device mode needs --path with one or more start groups (';'-separated) — the server refuses an empty OBJECT, so the root must be seeded (top folders from the device's Object Browser)")
		}
		devObjs, derr := cerebrumDeviceTreeObjects(sess, cf.timeout, *device, *byName, *subDev, starts, *maxReq)
		if derr != nil {
			return derr
		}
		objs = devObjs
		renderFocus = "" // --path already scoped the walk itself
	} else {
		if *domain == "sources" || *domain == "dests" || *domain == "all" {
			mneObjs, merr := cerebrumMneTreeObjects(sess, cf.timeout, *domain, *alt)
			if merr != nil {
				return merr
			}
			objs = append(objs, mneObjs...)
		}
		if *domain == "categories" || *domain == "all" {
			catObjs, cerr := cerebrumCategoryTreeObjects(sess, cf.timeout, *alt, *noMne)
			if cerr != nil {
				return cerr
			}
			objs = append(objs, catObjs...)
		}
		if *domain == "salvos" || *domain == "all" {
			salvoObjs, serr := cerebrumSalvoTreeObjects(sess, cf.timeout)
			if serr != nil {
				return serr
			}
			objs = append(objs, salvoObjs...)
		}
		if *domain == "devices" || *domain == "all" {
			devObjs, derr := cerebrumDeviceListTreeObjects(sess, cf.timeout)
			if derr != nil {
				return derr
			}
			objs = append(objs, devObjs...)
		}
		renderFocus = cerebrumExpandFocus(objs, *focus)
	}

	opts := treeRenderOpts{
		FromPath: renderFocus,
		Depth:    *depth,
		ASCII:    *format == "ascii",
		Filter:   *filter,
	}
	if *format == "plantuml" {
		return renderTreePlantUML(writer, objs, opts)
	}
	return renderTree(writer, objs, opts)
}

// cerebrumExpandFocus resolves a --path that names a node ANYWHERE in the
// tree, not only from the root: sub-category nesting means a category's
// canonical path runs through its parents (Categories.DESTINATIONS.
// DST-FUSION), but operators reference categories directly — with or
// without the intermediate levels. The user's dotted segments are matched
// IN ORDER against any object path, gaps allowed (case-insensitive), so
// "Categories.DST-FUSION", "DST-FUSION" and the full canonical path all
// resolve. The prefix ending at the last matched segment becomes the
// renderer focus; the shallowest match wins. No match returns the input
// unchanged so the renderer's own error still fires.
func cerebrumExpandFocus(objs []consumer.Object, focus string) string {
	if focus == "" {
		return focus
	}
	segs := strings.Split(focus, ".")
	best := []string(nil)
	for i := range objs {
		p := objs[i].Path
		j := 0
		end := -1
		for k := 0; k < len(p) && j < len(segs); k++ {
			if strings.EqualFold(p[k], segs[j]) {
				j++
				end = k
			}
		}
		if j == len(segs) {
			full := p[:end+1]
			if best == nil || len(full) < len(best) {
				best = append([]string{}, full...)
			}
		}
	}
	if best == nil {
		return focus
	}
	return strings.Join(best, ".")
}

// cerebrumDeviceTreeObjects walks one device's object tree over §5.4.3
// VALUE obtains — the NB analogue of an acp2 walk (live 2026-08-16): a
// GROUP path returns its CHILDREN as OBJECT_VALUE rows (sub-groups arrive
// as available=0 empty rows, leaves carry the value), while a LEAF path
// echoes itself as a single row. Recursion descends group candidates and
// re-classifies "single self-echo" answers as unavailable leaves. start
// "" (or the "." sentinel) walks from the device root; maxReq caps the
// obtain count.
func cerebrumDeviceTreeObjects(sess *cerebrum.Session, timeout time.Duration, device string, byName bool, subDev string, starts []string, maxReq int) ([]consumer.Object, error) {
	rootLabel := strings.TrimSpace(device)
	requests := 0
	obtain := func(object string) ([]codec.DeviceObjectValue, error) {
		requests++
		dc := &codec.DeviceChange{Type: "VALUE", SubDevice: subDev, Object: object}
		if byName {
			dc.DeviceName = device
		} else {
			dc.IPAddress = device
		}
		got, err := obtainSingleDeviceChange(sess, timeout, dc, "VALUE")
		if err != nil {
			return nil, err
		}
		if got == nil || got.Device == nil {
			return nil, nil
		}
		return got.Device.ObjectValues, nil
	}

	var objs []consumer.Object
	truncated := false
	emitLeaf := func(ov codec.DeviceObjectValue) {
		meta := fmt.Sprintf("available=%s", boolFlag(ov.Available))
		if ov.Value != "" {
			meta += fmt.Sprintf(" value=%q", ov.Value)
		}
		if ov.DataType != "" {
			meta += " type=" + ov.DataType
		}
		// A degenerate MIN==MAX range carries no information (ENUMs report
		// 0..0 — their real constraint is the enum list; Identifier-style
		// INTEGERs likewise) — print range only when it constrains.
		if (ov.Min != "" || ov.Max != "") && ov.Min != ov.Max {
			meta += fmt.Sprintf(" range=%s..%s", ov.Min, ov.Max)
		}
		if len(ov.EnumList) > 0 {
			meta += fmt.Sprintf(" enum=%s", strings.Join(ov.EnumList, "|"))
		}
		// Access bits from the wire descriptor (read=1, write=2 — the
		// canonical access byte), so RW objects render as RW-, not R--.
		var access uint8
		if ov.Readable {
			access |= 0x01
		}
		if ov.Writable {
			access |= 0x02
		}
		objs = append(objs, consumer.Object{
			Path:  append([]string{rootLabel}, strings.Split(ov.Object, ".")...),
			Label: ov.Object,
			Kind:  consumer.KindString, Access: access,
			Value: consumer.Value{Kind: consumer.KindString, Str: meta},
		})
	}

	var walk func(group string) error
	walk = func(group string) error {
		if requests >= maxReq {
			truncated = true
			return nil
		}
		rows, err := obtain(group)
		if err != nil {
			return err
		}
		// A single self-echo row = this path is a real LEAF (possibly
		// unavailable), not a group.
		if group != "" && len(rows) == 1 && rows[0].Object == group {
			emitLeaf(rows[0])
			return nil
		}
		for _, ov := range rows {
			if ov.Object == group {
				continue
			}
			// Group candidate: no value, not available, no descriptor —
			// descend; the self-echo check above re-classifies real leaves.
			if !ov.Available && ov.Value == "" && ov.DataType == "" {
				if err := walk(ov.Object); err != nil {
					return err
				}
				continue
			}
			emitLeaf(ov)
		}
		return nil
	}
	for _, start := range starts {
		if err := walk(start); err != nil {
			return nil, err
		}
	}
	if truncated {
		fmt.Fprintf(os.Stderr, "cerebrum-nb tree: WARNING — walk truncated at --max-requests=%d obtains (raise it for the full tree)\n", maxReq)
	}
	fmt.Fprintf(os.Stderr, "cerebrum-nb tree: device walk used %d obtain(s), %d object(s)\n", requests, len(objs))
	return objs, nil
}

// cerebrumMneTreeObjects renders the resource inventory PER ROUTER (owner
// rule: "RM first and then all the routers if exists"): Sources / Dests →
// router node ("RM 0.0.0.0" then each ROUTER-class device from the §5.4
// LIST) → one leaf per resource — ID zero-padded so the sorted tree keeps
// numeric order, capability levels, label from the selected set (--alt)
// and the alt list. A router that refuses the MNE reads is skipped with a
// warning (lenient — per-router grants proven live 2026-08-16 on .27).
func cerebrumMneTreeObjects(sess *cerebrum.Session, timeout time.Duration, domain string, alt int) ([]consumer.Object, error) {
	want := cerebrumStateWant{Verb: "tree"}
	if domain == "sources" || domain == "all" {
		want.SrcMne = true
	}
	if domain == "dests" || domain == "all" {
		want.DstMne = true
	}

	// RM sentinel first, then every ROUTER-class device the plant lists.
	routers := []string{"0.0.0.0"}
	if got, err := obtainSingleDeviceChange(sess, timeout, &codec.DeviceChange{Type: "LIST"}, "LIST"); err == nil && got != nil && got.Device != nil {
		seen := map[string]bool{"0.0.0.0": true}
		for _, d := range got.Device.Devices {
			isRouter := d.DeviceType == codec.DeviceType("ROUTER")
			for _, t := range d.DeviceTypes {
				if t == codec.DeviceType("ROUTER") {
					isRouter = true
				}
			}
			if isRouter && d.IPAddress != "" && !seen[d.IPAddress] {
				seen[d.IPAddress] = true
				routers = append(routers, d.IPAddress)
			}
		}
	}

	pad := func(id string) string {
		if n, aerr := strconv.Atoi(strings.TrimSpace(id)); aerr == nil {
			return fmt.Sprintf("%05d", n)
		}
		return id
	}
	var objs []consumer.Object
	emit := func(root, routerNode string, rows []cerebrumMneRow) {
		for _, r := range rows {
			label := r.Mnemonic
			if alt > 0 {
				label = r.Alts[alt]
			}
			meta := fmt.Sprintf("label=%q", label)
			if len(r.Levels) > 0 {
				meta += " levels=" + strings.Join(r.Levels, ";")
			}
			if len(r.Alts) > 0 && alt <= 0 {
				meta += " alts=" + formatAlts(r.Alts)
			}
			objs = append(objs, consumer.Object{
				Path:  []string{root, routerNode, pad(r.ID)},
				Label: r.ID,
				Kind:  consumer.KindString, Access: 1,
				Value: consumer.Value{Kind: consumer.KindString, Str: meta},
			})
		}
	}
	for _, router := range routers {
		node := router
		if router == "0.0.0.0" {
			node = "RM 0.0.0.0"
		}
		st, err := cerebrumObtainState(context.Background(), sess, router, "ROUTER", 15*time.Second, want)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cerebrum-nb tree: router %s skipped (%v)\n", router, err)
			continue
		}
		emit("Sources", node, st.Src)
		emit("Dests", node, st.Dst)
	}
	return objs, nil
}

// cerebrumDeviceListTreeObjects renders the §5.4 LIST as a Devices tree:
// Devices → class → IP (one leaf per class instance, mirroring the wire's
// one-row-per-class shape).
func cerebrumDeviceListTreeObjects(sess *cerebrum.Session, timeout time.Duration) ([]consumer.Object, error) {
	got, err := obtainSingleDeviceChange(sess, timeout, &codec.DeviceChange{Type: "LIST"}, "LIST")
	if err != nil {
		return nil, err
	}
	if got == nil || got.Device == nil {
		return nil, fmt.Errorf("cerebrum-nb tree: no DEVICE_CHANGE TYPE=LIST reply within timeout")
	}
	var objs []consumer.Object
	for _, d := range got.Device.Devices {
		class := string(d.DeviceType)
		if class == "" {
			class = "UNKNOWN"
		}
		meta := "class=" + class
		if d.DeviceName != "" {
			meta += fmt.Sprintf(" name=%q", d.DeviceName)
		}
		objs = append(objs, consumer.Object{
			Path:  []string{"Devices", class, d.IPAddress},
			Label: d.IPAddress,
			Kind:  consumer.KindString, Access: 1,
			Value: consumer.Value{Kind: consumer.KindString, Str: meta},
		})
	}
	return objs, nil
}

// cerebrumSalvoTreeObjects walks §5.3 (GROUP_LIST → INSTANCE_LIST →
// INSTANCE_DETAILS) into canonical objects under the "Salvos" root.
func cerebrumSalvoTreeObjects(sess *cerebrum.Session, timeout time.Duration) ([]consumer.Object, error) {
	groups, err := obtainSingleSalvoChange(sess, timeout,
		&codec.SalvoChange{Type: "GROUP_LIST"}, "GROUP_LIST")
	if err != nil {
		return nil, err
	}
	if groups == nil || groups.Salvo == nil {
		return nil, fmt.Errorf("cerebrum-nb tree: no GROUP_LIST reply within timeout")
	}

	var objs []consumer.Object
	for _, g := range groups.Salvo.Groups {
		il, ierr := obtainSingleSalvoChange(sess, timeout,
			&codec.SalvoChange{Type: "INSTANCE_LIST", Group: g}, "INSTANCE_LIST")
		if ierr != nil {
			return nil, ierr
		}
		var instances []string
		if il != nil && il.Salvo != nil {
			instances = il.Salvo.Instances
		}
		if len(instances) == 0 {
			objs = append(objs, consumer.Object{
				Path: []string{"Salvos", g}, Label: g,
				Kind: consumer.KindString, Access: 1,
				Value: consumer.Value{Kind: consumer.KindString, Str: "(no instances)"},
			})
			continue
		}
		for _, inst := range instances {
			dt, derr := obtainSingleSalvoChange(sess, timeout,
				&codec.SalvoChange{Type: "INSTANCE_DETAILS", Group: g, Instance: inst}, "INSTANCE_DETAILS")
			if derr != nil {
				return nil, derr
			}
			meta := "(no details)"
			if dt != nil && dt.Salvo != nil && dt.Salvo.InstanceDetails != nil {
				d := dt.Salvo.InstanceDetails
				meta = fmt.Sprintf("available=%s active=%s", boolFlag(d.Available), boolFlag(d.Active))
				if d.Description != "" {
					meta += fmt.Sprintf(" description=%q", d.Description)
				}
				if d.Date != "" || d.Time != "" {
					meta += fmt.Sprintf(" saved=%s %s", d.Date, d.Time)
				}
			}
			objs = append(objs, consumer.Object{
				Path: []string{"Salvos", g, inst}, Label: inst,
				Kind: consumer.KindString, Access: 1,
				Value: consumer.Value{Kind: consumer.KindString, Str: meta},
			})
		}
	}
	return objs, nil
}

// cerebrumCategoryTreeObjects walks §5.2 (CATEGORY_LIST → CATEGORY_DETAILS
// per category) into canonical objects under the "Categories" root. Item
// rows render exactly as the wire returns them (§3.3 ITEM_TYPE + VALUE):
// SOURCE / DEST rows carry the routable ID on this class of plant
// (§1.8 name-or-ID), so the leaf is annotated with the primary mnemonic
// joined from the SRCE_MNE / DEST_MNE catalogue (two wildcard OBTAINs —
// the same reads list-sources / list-dests use). Nested CATEGORY, TEXT,
// FILE, CUSTOM rows render verbatim. The index prefix preserves the
// category's slot order in the sorted tree.
func cerebrumCategoryTreeObjects(sess *cerebrum.Session, timeout time.Duration, alt int, noMne bool) ([]consumer.Object, error) {
	// Mnemonic join: ID -> label for SOURCE and DEST item rows. --alt picks
	// the set (0 = primary, N = ALT_MNE slot N); --no-mne skips the join.
	srcName := map[string]string{}
	dstName := map[string]string{}
	if !noMne {
		st, serr := cerebrumObtainState(context.Background(), sess, "0.0.0.0", "ROUTER", 15*time.Second, cerebrumStateWant{
			SrcMne: true, DstMne: true, Verb: "tree",
		})
		if serr != nil {
			return nil, serr
		}
		pick := func(r cerebrumMneRow) string {
			if alt <= 0 {
				return r.Mnemonic
			}
			return r.Alts[alt]
		}
		for _, r := range st.Src {
			srcName[r.ID] = pick(r)
		}
		for _, r := range st.Dst {
			dstName[r.ID] = pick(r)
		}
	}

	// Fetch every category's details once (shared §5.2 walk), then compose
	// the FOREST: categories referenced as CATEGORY items nest under their
	// parent (recursively — real sub-category subtrees, matching the UI
	// Inherits/membership view) instead of duplicating at top level. Roots
	// are the categories no other category references.
	names, details, err := fetchCerebrumCategories(sess, timeout)
	if err != nil {
		return nil, err
	}
	referenced := map[string]bool{}
	for _, d := range details {
		if d == nil {
			continue
		}
		for _, it := range d.Items {
			if it.Type == "CATEGORY" {
				referenced[it.Value] = true
			}
		}
	}

	var objs []consumer.Object
	var emit func(cat string, path []string, visited map[string]bool)
	emit = func(cat string, path []string, visited map[string]bool) {
		d := details[cat]
		if d == nil || len(d.Items) == 0 {
			meta := "(no items)"
			if d != nil && d.Label != "" {
				meta = fmt.Sprintf("label=%q (no items)", d.Label)
			}
			objs = append(objs, consumer.Object{
				Path: path, Label: cat,
				Kind: consumer.KindString, Access: 1,
				Value: consumer.Value{Kind: consumer.KindString, Str: meta},
			})
			return
		}
		for _, it := range d.Items {
			if it.Type == "CATEGORY" {
				if _, known := details[it.Value]; known && !visited[it.Value] {
					visited[it.Value] = true
					emit(it.Value, append(append([]string{}, path...), it.Value), visited)
					continue
				}
				// Unknown target or cycle — render the reference verbatim.
			}
			val := it.Value
			switch it.Type {
			case "SOURCE", "SRCE":
				if mne, ok := srcName[it.Value]; ok && mne != "" {
					val = fmt.Sprintf("%s %q", it.Value, mne)
				}
			case "DEST", "DESTINATION":
				if mne, ok := dstName[it.Value]; ok && mne != "" {
					val = fmt.Sprintf("%s %q", it.Value, mne)
				}
			}
			objs = append(objs, consumer.Object{
				Path:  append(append([]string{}, path...), fmt.Sprintf("%04d %s", it.Index, it.Type)),
				Label: it.Value, ID: it.Index,
				Kind: consumer.KindString, Access: 1,
				Value: consumer.Value{Kind: consumer.KindString, Str: val},
			})
		}
	}
	for _, cat := range names {
		if referenced[cat] {
			continue // rendered under its parent(s)
		}
		emit(cat, []string{"Categories", cat}, map[string]bool{cat: true})
	}
	return objs, nil
}
