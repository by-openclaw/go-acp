package mib

// One device, one MIB. A connection talks to one device, so it loads that
// device's table and nothing else: an object another model defines at the
// same OID cannot name this one's, which a single merged table could only
// prevent by rule. tools/mibc writes the files under tables/, one for each
// model in the MIB library, each with the standard modules beside it.

import (
	"embed"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"

	"dhs/internal/snmp/codec"
)

// BaseTable holds the standard modules and our own DHS-MIB, and nothing
// from a vendor. It is what a device with no table of its own gets.
const BaseTable = "standard"

//go:embed tables/*.tsv.gz
var tableFiles embed.FS

// loaders opens each table on first use, because a `get sysDescr.0` has
// no business decompressing names it will never print. The embed pattern
// guarantees the files; one that did not parse would panic in mustLoad.
var loaders = func() map[string]func() *Table {
	entries, _ := tableFiles.ReadDir("tables")
	m := make(map[string]func() *Table, len(entries))
	for _, e := range entries {
		file := e.Name()
		m[strings.TrimSuffix(file, ".tsv.gz")] = sync.OnceValue(func() *Table {
			b, _ := tableFiles.ReadFile(path.Join("tables", file))
			return mustLoad(b)
		})
	}
	return m
}()

// TableNames lists the tables this binary carries.
func TableNames() []string {
	names := make([]string, 0, len(loaders))
	for n := range loaders {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Load returns one device's table by name, ateme-dr5000 for example.
func Load(name string) (*Table, error) {
	load, ok := loaders[name]
	if !ok {
		return nil, fmt.Errorf("mib: no table %q; this binary has %s",
			name, strings.Join(TableNames(), ", "))
	}
	return load(), nil
}

// Base is [BaseTable].
func Base() *Table { return loaders[BaseTable]() }

// byObjectID says which table a device's sysObjectID selects. Only a
// branch one model owns belongs here: the TT1260 and RX1290 report the same
// sysObjectID and the DEV switches share one tree, so those take --mib.
var byObjectID = []struct {
	root  codec.OID
	table string
}{
	{oid("1.3.6.1.4.1.27338.5"), "ateme-dr5000"},
	{SnellWilcox, "snell"},
	{DHSAgent, BaseTable},
}

// ForObjectID names the table a sysObjectID selects, if one does.
func ForObjectID(o codec.OID) (string, bool) {
	for _, e := range byObjectID {
		if o.HasPrefix(e.root) {
			return e.table, true
		}
	}
	return "", false
}
