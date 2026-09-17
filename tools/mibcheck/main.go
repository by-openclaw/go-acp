// Command mibcheck validates a directory of SNMP MIB source before anything
// is generated from it.
//
//	go run ./tools/mibcheck <dir>...
//
// A MIB is a contract we decode devices against, and a defect in one is
// silent: the compiler accepts it, the generated tables look plausible, and
// a value decodes to the wrong label — or to two labels — for as long as
// nobody looks. Vendor MIBs do contain such defects. The Ericsson Television
// set ships with an enumeration where one wire value carries two names and
// a second name is duplicated:
//
//	inS2StatusModulationType ::= INTEGER {
//	    modBpsk(1), modQpsk(2), mod8psk(3), mod16qam(4),
//	    modAuto(5), mod16sqam(5), modAuto(7) }
//
// so decoding a 5 is ambiguous and 6 is unreachable, against a DESCRIPTION
// that lists seven distinct modulations.
//
// The checks are deliberately the ones that need no grammar — this is a
// linter, not a compiler, and it must be able to run over a set that does
// not yet compile:
//
//	duplicate-value  two members of one enumeration share a wire value
//	duplicate-name   one name appears twice in one enumeration
//	missing-import   a module is IMPORTed that nothing in the set DEFINES
//
// Exits non-zero when anything is found, so it can gate a commit.
//
// With -fetch it also RESOLVES the missing imports, downloading each absent
// module into -out and re-checking the enlarged set:
//
//	go run ./tools/mibcheck -fetch -out standard ird
//
// Every download is validated before it is written: it must declare the
// module it was asked for and contain no markup. That is not paranoia.
// mibs.observium.org serves a rendered HTML page at the URL that looks like
// a raw download, so an unchecked fetch produces a 47 KB web page named
// SNMPv2-SMI instead of the 9 KB module, and the defect surfaces much later
// as a parse error in something apparently unrelated.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// defaultMIBSource is where -fetch looks. mibbrowser.online publishes raw
// module source at a predictable path; the other well-known browsers render
// HTML instead, which is why this is the default and why every download is
// validated regardless of where it came from.
const defaultMIBSource = "https://mibbrowser.online/mibs/"

// fetchTimeout bounds one download. A linter must not hang on a slow mirror.
const fetchTimeout = 30 * time.Second

func main() {
	quiet := flag.Bool("q", false, "print only findings, no summary")
	fetch := flag.Bool("fetch", false, "download modules that are imported but missing")
	out := flag.String("out", "", "directory to write fetched modules into (required with -fetch)")
	source := flag.String("source", defaultMIBSource, "base URL to fetch missing modules from")
	flag.Parse()

	dirs := flag.Args()
	if len(dirs) == 0 {
		fmt.Fprintln(os.Stderr, "usage: mibcheck [-q] [-fetch -out DIR] <dir>...")
		os.Exit(2)
	}
	if *fetch && *out == "" {
		fmt.Fprintln(os.Stderr, "mibcheck: -fetch needs -out <dir>")
		os.Exit(2)
	}

	set, err := scan(dirs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mibcheck:", err)
		os.Exit(2)
	}

	if *fetch {
		if n := fetchMissing(*source, *out, set.missing()); n > 0 {
			// Re-scan from disk rather than trusting what was just written:
			// what matters is what the next compile will see.
			fmt.Fprintf(os.Stderr, "mibcheck: fetched %d module(s) into %s; re-checking\n", n, *out)
			if set, err = scan(append(dirs, *out)); err != nil {
				fmt.Fprintln(os.Stderr, "mibcheck:", err)
				os.Exit(2)
			}
		}
	}

	findings := set.findings()
	for _, f := range findings {
		fmt.Println(f)
	}
	if !*quiet {
		fmt.Fprintf(os.Stderr, "\nmibcheck: %d file(s), %d module(s) defined, %d finding(s)\n",
			set.files, len(set.defined), len(findings))
	}
	if len(findings) > 0 {
		os.Exit(1)
	}
}

// Finding is one defect, formatted like a compiler diagnostic so an editor
// can jump to it.
type Finding struct {
	File string
	Line int
	Rule string
	Msg  string
}

func (f Finding) String() string {
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d: %s: %s", f.File, f.Line, f.Rule, f.Msg)
	}
	return fmt.Sprintf("%s: %s: %s", f.File, f.Rule, f.Msg)
}

// Set is one scan of a MIB collection: what it defines, what it imports, and
// the per-file defects found along the way.
type Set struct {
	files    int
	defined  map[string]string   // module -> file that defines it
	imported map[string][]string // module -> files that import it
	enums    []Finding
}

// scan reads every MIB under dirs once.
func scan(dirs []string) (*Set, error) {
	paths, err := collect(dirs)
	if err != nil {
		return nil, err
	}
	s := &Set{
		defined:  map[string]string{},
		imported: map[string][]string{},
	}
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		s.files++
		text := stripComments(string(src))
		s.enums = append(s.enums, checkEnums(p, text)...)
		for _, m := range definedModules(text) {
			s.defined[m] = p
		}
		for _, m := range importedModules(text) {
			s.imported[m] = append(s.imported[m], p)
		}
	}
	return s, nil
}

// missing lists modules the set imports but never defines. This is what
// makes a set incomplete rather than wrong: the compile fails on a machine
// without them and succeeds on one that happens to have them installed,
// which is the worst of both.
func (s *Set) missing() []string {
	var out []string
	for m := range s.imported {
		if _, ok := s.defined[m]; !ok {
			out = append(out, m)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Set) findings() []Finding {
	out := append([]Finding(nil), s.enums...)
	for _, m := range s.missing() {
		files := append([]string(nil), s.imported[m]...)
		sort.Strings(files)
		where := ""
		if len(files) > 0 {
			where = files[0]
		}
		out = append(out, Finding{where, 0, "missing-import",
			fmt.Sprintf("module %q is imported by %d file(s) but defined nowhere in the set",
				m, len(files))})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// mibExt reports whether a filename looks like MIB source. Extensionless
// files are included: the IETF modules ship with no extension at all.
func mibExt(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mib", ".my", ".txt", "":
		return true
	}
	return false
}

func collect(dirs []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, d := range dirs {
		err := filepath.Walk(d, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !mibExt(info.Name()) {
				return nil
			}
			// A README sitting beside the modules is not one of them.
			if strings.EqualFold(info.Name(), "README.md") {
				return nil
			}
			slash := filepath.ToSlash(p)
			// -fetch appends the output dir to the scan list, which may
			// already be inside one of them; count each file once.
			if !seen[slash] {
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

// stripComments removes ASN.1 comments (-- to end of line) while preserving
// line numbering, so a reported line still matches the source. Without this
// a commented-out example enumeration would be linted as if it were real.
func stripComments(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if j := strings.Index(ln, "--"); j >= 0 {
			lines[i] = ln[:j]
		}
	}
	return strings.Join(lines, "\n")
}

var (
	enumRe   = regexp.MustCompile(`(?s)\b(?:INTEGER|Integer32|BITS)\s*\{(.*?)\}`)
	pairRe   = regexp.MustCompile(`([A-Za-z][\w-]*)\s*\(\s*(-?\d+)\s*\)`)
	defineRe = regexp.MustCompile(`(?m)^\s*([A-Za-z][\w-]*)\s+DEFINITIONS\s*::=`)
	fromRe   = regexp.MustCompile(`\bFROM\s+([A-Za-z][\w-]*)`)
	importRe = regexp.MustCompile(`(?s)\bIMPORTS\b(.*?);`)
)

// checkEnums reports duplicate values and duplicate names inside a single
// enumeration. Both are defects regardless of intent: a repeated value makes
// decoding ambiguous, and a repeated name makes encoding ambiguous.
func checkEnums(file, text string) []Finding {
	var out []Finding
	for _, loc := range enumRe.FindAllStringSubmatchIndex(text, -1) {
		body := text[loc[2]:loc[3]]
		pairs := pairRe.FindAllStringSubmatch(body, -1)
		if len(pairs) < 2 {
			continue
		}
		line := 1 + strings.Count(text[:loc[0]], "\n")

		byValue := map[int][]string{}
		byName := map[string][]int{}
		var valueOrder []int
		var nameOrder []string
		for _, p := range pairs {
			name := p[1]
			v, err := strconv.Atoi(p[2])
			if err != nil {
				continue
			}
			if _, seen := byValue[v]; !seen {
				valueOrder = append(valueOrder, v)
			}
			if _, seen := byName[name]; !seen {
				nameOrder = append(nameOrder, name)
			}
			byValue[v] = append(byValue[v], name)
			byName[name] = append(byName[name], v)
		}

		for _, v := range valueOrder {
			if names := byValue[v]; len(names) > 1 {
				out = append(out, Finding{file, line, "duplicate-value",
					fmt.Sprintf("value %d is used by %s — decoding it is ambiguous",
						v, strings.Join(names, " and "))})
			}
		}
		for _, n := range nameOrder {
			if vs := byName[n]; len(vs) > 1 {
				strs := make([]string, len(vs))
				for i, v := range vs {
					strs[i] = strconv.Itoa(v)
				}
				out = append(out, Finding{file, line, "duplicate-name",
					fmt.Sprintf("name %q is used for values %s — encoding it is ambiguous",
						n, strings.Join(strs, " and "))})
			}
		}
	}
	return out
}

func definedModules(text string) []string {
	var out []string
	for _, m := range defineRe.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	return out
}

// importedModules reads the names after FROM inside IMPORTS blocks only, so
// the word "FROM" in a DESCRIPTION cannot invent a dependency.
func importedModules(text string) []string {
	var out []string
	for _, blk := range importRe.FindAllStringSubmatch(text, -1) {
		for _, m := range fromRe.FindAllStringSubmatch(blk[1], -1) {
			out = append(out, m[1])
		}
	}
	return out
}

// fetchMissing downloads each missing module into dir and returns how many
// were written. A module that cannot be fetched, or that fails validation,
// is reported and skipped rather than written — a bad file on disk is worse
// than an absent one, because the next run stops reporting it as missing.
func fetchMissing(source, dir string, missing []string) int {
	if len(missing) == 0 {
		return 0
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mibcheck: %v\n", err)
		return 0
	}
	n := 0
	for _, m := range missing {
		body, err := download(source, m)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mibcheck: fetch %s: %v\n", m, err)
			continue
		}
		if err := validate(m, body); err != nil {
			fmt.Fprintf(os.Stderr, "mibcheck: fetch %s: %v (not written)\n", m, err)
			continue
		}
		path := filepath.Join(dir, m)
		if err := os.WriteFile(path, body, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "mibcheck: write %s: %v\n", path, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "mibcheck: fetched %s (%d bytes) -> %s\n", m, len(body), path)
		n++
	}
	return n
}

func download(source, module string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	url := strings.TrimSuffix(source, "/") + "/" + module + ".mib"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	// Cap the read: a MIB is tens of kilobytes, and an unbounded read from
	// a mirror that decided to serve something else is not worth the risk.
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// validate rejects anything that is not the module it was asked for. Both
// checks matter: the markup test catches a rendered browser page, and the
// DEFINITIONS test catches a mirror that served a different module.
func validate(module string, body []byte) error {
	text := string(body)
	lower := strings.ToLower(text)
	if strings.Contains(lower, "<html") || strings.Contains(lower, "<!doctype") {
		return fmt.Errorf("looks like a web page, not MIB source")
	}
	got := definedModules(stripComments(text))
	for _, g := range got {
		if g == module {
			return nil
		}
	}
	if len(got) == 0 {
		return fmt.Errorf("declares no module at all")
	}
	return fmt.Errorf("declares %s, not %s", strings.Join(got, ", "), module)
}
