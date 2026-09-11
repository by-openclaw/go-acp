// Command mibc compiles SNMP MIB source into the table internal/snmp/mib
// embeds.
//
//	go run ./tools/mibc -out internal/snmp/mib/tables.tsv.gz <root>...
//
// It is the OFFLINE half of the rule in internal/snmp/CLAUDE.md: MIB
// source is parsed here, by a developer, and the result is committed; the
// shipped binary reads numbers and names and never a MIB. The roots are
// every directory the modules come from — today a checkout of
// github.com/by-protocol/mib (the IRDs and the IETF standard modules) and
// internal/snell-rollcall/assets/Protocol/SNMP/SNMP_MIBs (the Snell set).
// Every IMPORT must resolve across the roots together; run tools/mibcheck
// -fetch first if it does not.
//
// # Contract
//
// The table goes to -out, and a one-line summary to stdout. Findings —
// duplicate modules and which copy won, names that did not resolve, OIDs
// two modules both name — go to stderr, all of them with -v and counted
// by kind without it. The table is deterministic: the same roots produce
// the same bytes, so a regenerated table that differs is a real change.
//
//	0  the table was written
//	2  the command could not run: usage, an unreadable root, a file that
//	   cannot be tokenised, or an -out that cannot be written
//
// Findings are not a failure. Vendor MIBs carry defects this compiler
// works around by design, and refusing them would refuse the devices.
package main

import (
	"compress/gzip"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/snmp/mib"
	"dhs/internal/snmp/smi"
)

// osExit is os.Exit behind a package variable, so main itself can be
// exercised rather than only the function under it.
// Production never reassigns it.
var osExit = os.Exit

func main() { osExit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// patches corrects vendor defects that only the vendor's own files can
// prove. Embedded, so every run applies the same reviewed list; see the
// file for the rule on what may go in it.
//
//go:embed patches.txt
var patches string

func run(argv []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mibc", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", "", "table to write (gzip-compressed TSV)")
	pins := fs.String("pin", "IP-MIB=RX1290",
		"comma-separated MODULE=PATH-SUBSTRING: which file wins when a module ships in copies no date can decide between")
	exclude := fs.String("exclude", "Copy of ",
		"comma-separated filename prefixes to ignore — stray copies left beside the originals")
	verbose := fs.Bool("v", false, "list every finding, not only their counts")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if *out == "" || fs.NArg() == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: mibc -out FILE [-pin MOD=SUBSTR] [-exclude PREFIX] [-v] ROOT...")
		return 2
	}
	pinMap, err := parsePins(*pins)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "mibc:", err)
		return 2
	}

	files, err := collect(fs.Args(), splitList(*exclude))
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "mibc:", err)
		return 2
	}

	var mods []*smi.Module
	var findings []smi.Finding
	for _, f := range files {
		src, err := readFile(f)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "mibc:", err)
			return 2
		}
		ms, fnd, err := smi.Parse(f, src)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "mibc:", err)
			return 2
		}
		mods = append(mods, ms...)
		findings = append(findings, fnd...)
	}

	kept, dups := smi.Select(mods, pinMap)
	findings = append(findings, dups...)

	fixes, err := smi.ParsePatches(patches)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "mibc:", err)
		return 2
	}
	findings = append(findings, smi.Apply(kept, fixes)...)
	compiled := smi.Compile(kept)
	findings = append(findings, compiled.Findings...)

	if err := writeTable(*out, compiled.Objects); err != nil {
		_, _ = fmt.Fprintln(stderr, "mibc:", err)
		return 2
	}

	report(stderr, findings, *verbose)
	_, _ = fmt.Fprintf(stdout, "wrote %s: %d files, %d modules, %d objects, %d findings\n",
		*out, len(files), len(kept), len(compiled.Objects), len(findings))
	return 0
}

// report lists findings, or counts them by kind.
func report(w io.Writer, findings []smi.Finding, verbose bool) {
	if verbose {
		for _, f := range findings {
			_, _ = fmt.Fprintln(w, f)
		}
		return
	}
	counts := map[string]int{}
	for _, f := range findings {
		counts[f.Kind]++
	}
	kinds := make([]string, 0, len(counts))
	for k := range counts {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		_, _ = fmt.Fprintf(w, "mibc: %d %s (run with -v to list them)\n", counts[k], k)
	}
}

func parsePins(s string) (map[string]string, error) {
	out := map[string]string{}
	for _, p := range splitList(s) {
		mod, sub, ok := strings.Cut(p, "=")
		if !ok || mod == "" || sub == "" {
			return nil, fmt.Errorf("pin %q is not MODULE=PATH-SUBSTRING", p)
		}
		out[mod] = sub
	}
	return out, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// collect lists the MIB source under the roots. The rule is tools/
// mibcheck's, so the two tools read the same files: .mib, .my, .txt and
// extensionless (the IETF modules ship without one), never a README.
func collect(roots, exclude []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, root := range roots {
		err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !isMIB(info.Name(), exclude) {
				return nil
			}
			if slash := filepath.ToSlash(p); !seen[slash] {
				seen[slash] = true
				out = append(out, slash)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}

func isMIB(name string, exclude []string) bool {
	for _, x := range exclude {
		if strings.HasPrefix(name, x) {
			return false
		}
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".mib", ".my", ".txt", "":
	default:
		return false
	}
	return !strings.EqualFold(strings.TrimSuffix(name, filepath.Ext(name)), "README")
}

// readFile is os.ReadFile behind a package variable, so the one failure a
// real tree cannot produce on demand — a file that is listed and then
// cannot be read — can be shown to stop the run. Production never
// reassigns it.
var readFile = os.ReadFile

// writeTable writes the table to path, closing the file whatever happens
// and reporting the close failure when nothing failed first.
func writeTable(path string, objs []smi.Object) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	return encodeTable(f, objs)
}

// encodeTable writes the objects as gzip-compressed TSV, one per line:
//
//	oid  name  module  kind  base  tc  access  enums  index
//
// enums are name=value joined by ';', index names by ','. No timestamp and
// no gzip header name, so the bytes depend only on the input.
func encodeTable(w io.Writer, objs []smi.Object) error {
	zw := gzip.NewWriter(w)
	// The header is the reader's constant, so the layout the reader
	// accepts and the layout written here cannot drift apart.
	if _, err := fmt.Fprintln(zw, mib.TableHeader); err != nil {
		return err
	}
	for _, o := range objs {
		enums := make([]string, len(o.Enums))
		for i, e := range o.Enums {
			enums[i] = e.Name + "=" + strconv.FormatInt(e.Value, 10)
		}
		row := strings.Join([]string{
			o.Dotted(), o.Name, o.Module, o.Kind.String(), o.Base, o.TC, o.Access,
			strings.Join(enums, ";"), strings.Join(o.Index, ","),
		}, "\t")
		if _, err := fmt.Fprintln(zw, row); err != nil {
			return err
		}
	}
	return zw.Close()
}
