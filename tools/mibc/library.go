package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dhs/internal/snmp/smi"
)

// tableSet is one table to write: a device's modules, under one name.
type tableSet struct {
	name string
	dir  string
	// maker is the manufacturer folder an import may be fetched from. An
	// import never crosses to another maker, so no table holds a stranger.
	maker string
	files []string
}

// standardDir is the library folder of the IETF modules every vendor MIB
// imports and no vendor ships. Every table carries them.
const standardDir = "standard"

// tableSuffix ends every table file.
const tableSuffix = ".tsv.gz"

// runLibrary compiles a MIB library laid out as
// <manufacturer>/<device-type>/<model>/ into one table per model, under the
// directory outDir. sets adds tables from outside the library, as
// NAME=DIR,... Each table holds the set's own modules, what they import from
// the same manufacturer, and the standard modules.
func runLibrary(library, outDir, sets string, pins map[string]string, exclude []string,
	verbose bool, stdout, stderr io.Writer) int {
	findings, count, err := compileLibrary(library, outDir, sets, pins, exclude, stdout)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "mibc:", err)
		return 2
	}
	report(stderr, findings, verbose)
	_, _ = fmt.Fprintf(stdout, "%d tables, %d findings\n", count, len(findings))
	return 0
}

func compileLibrary(library, outDir, sets string, pins map[string]string, exclude []string,
	stdout io.Writer) ([]smi.Finding, int, error) {
	tables, err := librarySets(library)
	if err != nil {
		return nil, 0, err
	}
	extra, err := parseSets(sets)
	if err != nil {
		return nil, 0, err
	}
	tables = append(tables, extra...)
	if err := refuseStale(outDir, tables); err != nil {
		return nil, 0, err
	}

	standard, err := collect([]string{filepath.Join(library, standardDir)}, exclude)
	if err != nil {
		return nil, 0, err
	}
	all := append([]string(nil), standard...)
	for i := range tables {
		if tables[i].files, err = collect([]string{tables[i].dir}, exclude); err != nil {
			return nil, 0, err
		}
		all = append(all, tables[i].files...)
	}
	index, err := indexModules(all)
	if err != nil {
		return nil, 0, err
	}
	fixes, err := smi.ParsePatches(patches)
	if err != nil {
		return nil, 0, err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, 0, err
	}

	var findings []smi.Finding
	for _, t := range tables {
		files := closure(append(append([]string(nil), t.files...), standard...), t.maker, index)
		var mods []*smi.Module
		for _, f := range files {
			ms, fnd, err := parseFile(f)
			if err != nil {
				return nil, 0, err
			}
			mods = append(mods, ms...)
			findings = append(findings, fnd...)
		}
		kept, dups := smi.Select(mods, pins)
		findings = append(findings, dups...)
		findings = append(findings, smi.Apply(kept, fixes)...)
		compiled := smi.Compile(kept)
		findings = append(findings, compiled.Findings...)
		path := filepath.Join(outDir, t.name+tableSuffix)
		if err := writeTable(path, compiled.Objects); err != nil {
			return nil, 0, err
		}
		_, _ = fmt.Fprintf(stdout, "wrote %s: %d modules, %d objects\n",
			path, len(kept), len(compiled.Objects))
	}
	return findings, len(tables), nil
}

// refuseStale stops a run that would leave a table in outDir uncompiled: a
// library that lost a model must not quietly ship last year's file in an old
// layout. Deleting the file is how a table is dropped on purpose.
func refuseStale(outDir string, tables []tableSet) error {
	planned := map[string]bool{}
	for _, t := range tables {
		planned[t.name+tableSuffix] = true
	}
	found, err := filepath.Glob(filepath.Join(outDir, "*"+tableSuffix))
	if err != nil {
		return err
	}
	var left []string
	for _, f := range found {
		if name := filepath.Base(f); !planned[name] {
			left = append(left, strings.TrimSuffix(name, tableSuffix))
		}
	}
	if len(left) > 0 {
		return fmt.Errorf("%s: %s not compiled by this run: put its source in the library, "+
			"or delete the file to drop the table on purpose", outDir, strings.Join(left, ", "))
	}
	return nil
}

// librarySets lists one set per model folder. A table is named
// <manufacturer>-<model>, lower case, so the name says whose it is.
func librarySets(library string) ([]tableSet, error) {
	dirs, err := filepath.Glob(filepath.Join(library, "*", "*", "*"))
	if err != nil {
		return nil, err
	}
	var out []tableSet
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		parts := strings.Split(filepath.ToSlash(dir), "/")
		maker, model := parts[len(parts)-3], parts[len(parts)-1]
		if maker == standardDir || maker == "assets" || strings.HasPrefix(maker, ".") {
			continue
		}
		out = append(out, tableSet{
			name:  maker + "-" + strings.ToLower(model),
			dir:   dir,
			maker: maker,
		})
	}
	return out, nil
}

// parseSets reads -set: NAME=DIR,NAME=DIR.
func parseSets(s string) ([]tableSet, error) {
	var out []tableSet
	for _, part := range splitList(s) {
		name, dir, ok := strings.Cut(part, "=")
		if !ok || name == "" || dir == "" {
			return nil, fmt.Errorf("set %q is not NAME=DIR", part)
		}
		out = append(out, tableSet{name: name, dir: dir})
	}
	return out, nil
}

// moduleIndex is what a first pass over the source learns: where each
// module lives, and what each file imports.
type moduleIndex struct {
	files   map[string][]string // module name -> files defining it
	imports map[string][]string // file -> modules it imports
	defines map[string][]string // file -> modules it defines
}

func indexModules(files []string) (*moduleIndex, error) {
	idx := &moduleIndex{
		files:   map[string][]string{},
		imports: map[string][]string{},
		defines: map[string][]string{},
	}
	for _, f := range files {
		mods, _, err := parseFile(f)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, m := range mods {
			idx.files[m.Name] = append(idx.files[m.Name], f)
			idx.defines[f] = append(idx.defines[f], m.Name)
			for _, from := range m.Imports {
				if !seen[from] {
					seen[from] = true
					idx.imports[f] = append(idx.imports[f], from)
				}
			}
		}
		sort.Strings(idx.imports[f])
	}
	return idx, nil
}

// closure adds what the files import, from the same manufacturer only. A
// module nobody in reach defines stays unresolved, and mibc reports it.
func closure(files []string, maker string, idx *moduleIndex) []string {
	defined := map[string]bool{}
	out := append([]string(nil), files...)
	for _, f := range files {
		for _, m := range idx.defines[f] {
			defined[m] = true
		}
	}
	for i := 0; i < len(out); i++ {
		for _, mod := range idx.imports[out[i]] {
			if defined[mod] {
				continue
			}
			for _, c := range idx.files[mod] {
				if maker != "" && strings.Contains(c, "/"+maker+"/") {
					out = append(out, c)
					for _, m := range idx.defines[c] {
						defined[m] = true
					}
					break
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// parseFile reads and parses one file. Each table parses its own source:
// compiling marks the modules, so two tables cannot share them.
func parseFile(f string) ([]*smi.Module, []smi.Finding, error) {
	src, err := readFile(f)
	if err != nil {
		return nil, nil, err
	}
	return smi.Parse(f, src)
}
