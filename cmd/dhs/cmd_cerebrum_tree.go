package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	focus := fs.String("path", "", `focus subtree at this dotted path (e.g. "Salvos.Salvo Group 1" or "Categories.SRC-INTERPHONIE")`)
	domain := fs.String("domain", "all", "which catalogue(s) to walk: salvos | categories | all")
	alt := fs.Int("alt", 0, "label set for SOURCE/DEST item annotation: 0 = primary mnemonic, N = alternate set N (ALT_MNE, e.g. 1 = Panels on the NOC)")
	noMne := fs.Bool("no-mne", false, "skip the mnemonic join — show raw item IDs only (also skips the two SRCE/DEST_MNE wildcard reads)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	switch *domain {
	case "salvos", "categories", "all":
	default:
		return fmt.Errorf("cerebrum-nb tree: --domain must be salvos | categories | all, got %q", *domain)
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

	opts := treeRenderOpts{
		FromPath: cerebrumExpandFocus(objs, *focus),
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
	list, err := obtainSingleCategoryChange(sess, timeout,
		&codec.CategoryChange{Type: "CATEGORY_LIST"}, "CATEGORY_LIST")
	if err != nil {
		return nil, err
	}
	if list == nil || list.Category == nil {
		return nil, fmt.Errorf("cerebrum-nb tree: no CATEGORY_LIST reply within timeout")
	}

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

	// Fetch every category's details once, then compose the FOREST:
	// categories referenced as CATEGORY items nest under their parent
	// (recursively — real sub-category subtrees, matching the UI Inherits/
	// membership view) instead of duplicating at top level. Roots are the
	// categories no other category references.
	details := map[string]*codec.CategoryDetailsInfo{}
	referenced := map[string]bool{}
	for _, cat := range list.Category.Categories {
		det, derr := obtainSingleCategoryChange(sess, timeout,
			&codec.CategoryChange{Type: "CATEGORY_DETAILS", Category: cat}, "CATEGORY_DETAILS")
		if derr != nil {
			return nil, derr
		}
		if det != nil && det.Category != nil && det.Category.Details != nil {
			details[cat] = det.Category.Details
			for _, it := range det.Category.Details.Items {
				if it.Type == "CATEGORY" {
					referenced[it.Value] = true
				}
			}
		} else {
			details[cat] = nil
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
	for _, cat := range list.Category.Categories {
		if referenced[cat] {
			continue // rendered under its parent(s)
		}
		emit(cat, []string{"Categories", cat}, map[string]bool{cat: true})
	}
	return objs, nil
}
