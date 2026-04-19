package diff

import (
	"fmt"
	"io"
)

// orderedCategories is the display order used by both text + changelog
// formatters. Breaking first so the reader sees risk before additions.
var orderedCategories = []string{
	CategoryBreaking, CategoryChanged, CategoryAdded, CategoryRemoved,
}

// WriteText emits a human-readable report for terminal consumption.
// Each category is its own section; empty categories are skipped.
// A zero-entry report prints "no changes" so scripting can grep for it.
func (r *Report) WriteText(w io.Writer) error {
	counts := r.Counts()
	total := len(r.Entries)
	if total == 0 {
		_, err := fmt.Fprintln(w, "no changes")
		return err
	}

	if _, err := fmt.Fprintf(w, "%d changes: Breaking=%d Changed=%d Added=%d Removed=%d\n\n",
		total,
		counts[CategoryBreaking],
		counts[CategoryChanged],
		counts[CategoryAdded],
		counts[CategoryRemoved]); err != nil {
		return err
	}

	for _, cat := range orderedCategories {
		group := r.byCategory(cat)
		if len(group) == 0 {
			continue
		}
		if _, err := fmt.Fprintf(w, "## %s (%d)\n", cat, len(group)); err != nil {
			return err
		}
		for _, e := range group {
			if _, err := fmt.Fprintf(w, "  - %s\n", e.Message); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	return nil
}

// WriteChangelog emits a Keep-a-Changelog markdown block for one
// version bump. Section headings match the fixture library's
// CHANGELOG convention (docs/fixtures-products.md §CHANGELOG).
func (r *Report) WriteChangelog(w io.Writer, version, date string) error {
	if _, err := fmt.Fprintf(w, "## [%s] — %s\n\n", version, date); err != nil {
		return err
	}
	nonEmpty := 0
	for _, cat := range orderedCategories {
		group := r.byCategory(cat)
		if len(group) == 0 {
			continue
		}
		nonEmpty++
		if _, err := fmt.Fprintf(w, "### %s\n", cat); err != nil {
			return err
		}
		for _, e := range group {
			if _, err := fmt.Fprintf(w, "- %s\n", e.Message); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	if nonEmpty == 0 {
		if _, err := fmt.Fprintln(w, "No schema changes."); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	return nil
}

// byCategory returns the subset of entries belonging to the named
// category, preserving the slice's sorted order.
func (r *Report) byCategory(cat string) []Entry {
	var out []Entry
	for _, e := range r.Entries {
		if e.Category == cat {
			out = append(out, e)
		}
	}
	return out
}
