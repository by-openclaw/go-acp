// Command genpen compiles IANA's private enterprise number registry into the
// file internal/snmp/mib embeds.
//
//	curl -sSLO https://www.iana.org/assignments/enterprise-numbers.txt
//	go run ./tools/genpen -in enterprise-numbers.txt -out internal/snmp/mib/enterprises.tsv.gz
//
// A sysObjectID sits under 1.3.6.1.4.1.<number>, so this list names the
// constructor of any device with no MIB for it at all. It is an offline step
// like tools/mibc, and the result is committed: the binary opens no socket
// to find out who made a box.
//
// Number and organisation only. The registry also lists a contact and an
// address for each entry, and those are people; nothing here needs them.
//
// # Contract
//
// The file goes to -out, and a one-line summary to stdout. The same input
// gives the same bytes.
//
//	0  the file was written
//	2  the command could not run: usage, an unreadable -in, an -in that is
//	   not the registry, or an -out that cannot be written
package main

import (
	"bufio"
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/snmp/mib"
)

// osExit is os.Exit behind a package variable, so main itself can be
// exercised rather than only the function under it.
var osExit = os.Exit

func main() { osExit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(argv []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("genpen", flag.ContinueOnError)
	fs.SetOutput(stderr)
	in := fs.String("in", "", "IANA's enterprise-numbers.txt")
	out := fs.String("out", "", "file to write (gzip-compressed TSV)")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if *in == "" || *out == "" {
		_, _ = fmt.Fprintln(stderr, "usage: genpen -in enterprise-numbers.txt -out FILE")
		return 2
	}
	n, date, err := compile(*in, *out)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "genpen:", err)
		return 2
	}
	_, _ = fmt.Fprintf(stdout, "wrote %s: %d enterprises, registry of %s\n", *out, n, date)
	return 0
}

func compile(in, out string) (int, string, error) {
	src, err := os.Open(in)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = src.Close() }()
	rows, date, err := parse(src)
	if err != nil {
		return 0, "", err
	}
	dst, err := os.Create(out)
	if err != nil {
		return 0, "", err
	}
	werr := encode(dst, rows, date)
	if cerr := dst.Close(); werr == nil {
		werr = cerr
	}
	return len(rows), date, werr
}

// row is one registered number.
type row struct {
	number uint64
	name   string
}

var (
	updated = regexp.MustCompile(`^\(last updated (\d{4}-\d{2}-\d{2})\)$`)
	number  = regexp.MustCompile(`^\d+$`)
)

// parse reads the registry's layout: the number at the left margin, the
// organisation on the next line, the contact and the address further in.
// A number nobody holds is listed by the registry as ---none--- or
// Unassigned, and is not a name.
func parse(r io.Reader) ([]row, string, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	var rows []row
	date := ""
	for sc.Scan() {
		line := sc.Text()
		if m := updated.FindStringSubmatch(line); m != nil {
			date = m[1]
		}
		if !number.MatchString(line) {
			continue
		}
		n, err := strconv.ParseUint(line, 10, 32)
		if err != nil {
			return nil, "", fmt.Errorf("%q is not an enterprise number", line)
		}
		if !sc.Scan() {
			break
		}
		name := strings.Join(strings.Fields(sc.Text()), " ")
		if name == "" || name == "---none---" || strings.EqualFold(name, "unassigned") ||
			strings.EqualFold(name, "reserved") {
			continue
		}
		rows = append(rows, row{n, name})
	}
	if err := sc.Err(); err != nil {
		return nil, "", err
	}
	if date == "" || len(rows) == 0 {
		return nil, "", errors.New("this is not IANA's enterprise-numbers.txt")
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].number < rows[j].number })
	return rows, date, nil
}

// encode writes the layout internal/snmp/mib reads. No timestamp and no
// gzip header name, so the bytes depend only on the input.
func encode(w io.Writer, rows []row, date string) error {
	zw := gzip.NewWriter(w)
	if _, err := fmt.Fprintln(zw, mib.EnterpriseHeader+date); err != nil {
		return err
	}
	for _, r := range rows {
		if _, err := fmt.Fprintf(zw, "%d\t%s\n", r.number, r.name); err != nil {
			return err
		}
	}
	return zw.Close()
}
