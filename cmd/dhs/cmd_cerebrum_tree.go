package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
		catObjs, cerr := cerebrumCategoryTreeObjects(sess, cf.timeout)
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
		FromPath: *focus,
		Depth:    *depth,
		ASCII:    *format == "ascii",
		Filter:   *filter,
	}
	if *format == "plantuml" {
		return renderTreePlantUML(writer, objs, opts)
	}
	return renderTree(writer, objs, opts)
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
// SOURCE / DEST names, nested CATEGORY references, TEXT / FILE / CUSTOM.
// The index prefix preserves the category's slot order in the sorted tree.
func cerebrumCategoryTreeObjects(sess *cerebrum.Session, timeout time.Duration) ([]consumer.Object, error) {
	list, err := obtainSingleCategoryChange(sess, timeout,
		&codec.CategoryChange{Type: "CATEGORY_LIST"}, "CATEGORY_LIST")
	if err != nil {
		return nil, err
	}
	if list == nil || list.Category == nil {
		return nil, fmt.Errorf("cerebrum-nb tree: no CATEGORY_LIST reply within timeout")
	}

	var objs []consumer.Object
	for _, cat := range list.Category.Categories {
		det, derr := obtainSingleCategoryChange(sess, timeout,
			&codec.CategoryChange{Type: "CATEGORY_DETAILS", Category: cat}, "CATEGORY_DETAILS")
		if derr != nil {
			return nil, derr
		}
		if det == nil || det.Category == nil || det.Category.Details == nil || len(det.Category.Details.Items) == 0 {
			meta := "(no items)"
			if det != nil && det.Category != nil && det.Category.Details != nil && det.Category.Details.Label != "" {
				meta = fmt.Sprintf("label=%q (no items)", det.Category.Details.Label)
			}
			objs = append(objs, consumer.Object{
				Path: []string{"Categories", cat}, Label: cat,
				Kind: consumer.KindString, Access: 1,
				Value: consumer.Value{Kind: consumer.KindString, Str: meta},
			})
			continue
		}
		for _, it := range det.Category.Details.Items {
			objs = append(objs, consumer.Object{
				Path:  []string{"Categories", cat, fmt.Sprintf("%04d %s", it.Index, it.Type)},
				Label: it.Value, ID: it.Index,
				Kind: consumer.KindString, Access: 1,
				Value: consumer.Value{Kind: consumer.KindString, Str: it.Value},
			})
		}
	}
	return objs, nil
}
