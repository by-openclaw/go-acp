// Command gen-probel-tree emits a canonical JSON tree suitable for the
// Probel SW-P-08 producer (--tree flag). Intended for scale benchmarking
// at 2 matrices × 65535 × 1 level (worst-case tally-dump path).
//
// Usage:
//
//	go run ./tools/gen-probel-tree \
//	    -matrices 2 -size 65535 -levels 1 \
//	    -out internal/probel-sw08p/assets/scale_2mtx_65535_1lvl.json
//
// The output matches the schema consumed by internal/probel-sw08p/provider/tree.go
// (canonical.Export → canonical.Matrix[]). All source + destination names
// get labelled positionally ("SRC_NNNNN" / "TGT_NNNNN"), so the tree
// doubles as an exerciser of the name/label RX command paths.
//
// # Contract
//
// The tree goes to stdout when no -out is given, so it can be piped;
// with -out it is written to the file and a one-line summary goes to
// stderr instead. Nothing but the tree ever reaches stdout.
//
//	0  the tree was written
//	1  the bounds were wrong, or the file could not be written
//
// The bounds are the protocol's, not this tool's: SW-P-08 addresses a
// matrix and a level in one byte each and a source or destination in
// two, so a tree outside 1..255 / 1..65535 describes a router the
// protocol cannot express — and the producer would serve it happily
// right up to the first command that could not name a crosspoint.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"dhs/internal/export/canonical"
)

// osExit is os.Exit behind a package variable, so main itself can be
// exercised rather than only the function under it.
// Production never reassigns it.
var osExit = os.Exit

func main() { osExit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(argv []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gen-probel-tree", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		matrices = fs.Int("matrices", 2, "number of matrices")
		size     = fs.Int("size", 65535, "target/source count per (matrix, level)")
		levels   = fs.Int("levels", 1, "levels per matrix")
		out      = fs.String("out", "", "output path (default stdout)")
	)
	if err := fs.Parse(argv); err != nil {
		return 1
	}

	// The bounds are SW-P-08's own address widths; see the package doc.
	for _, b := range []struct {
		name  string
		value int
		max   int
	}{
		{"matrices", *matrices, 255},
		{"size", *size, 65535},
		{"levels", *levels, 255},
	} {
		if b.value <= 0 || b.value > b.max {
			_, _ = fmt.Fprintf(stderr, "gen-probel-tree: %s must be 1..%d\n", b.name, b.max)
			return 1
		}
	}

	start := time.Now()
	exp := buildExport(*matrices, *size, *levels)

	w := stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "gen-probel-tree:", err)
			return 1
		}
		defer func() { _ = f.Close() }()
		w = f
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(exp); err != nil {
		_, _ = fmt.Fprintln(stderr, "gen-probel-tree:", err)
		return 1
	}

	if *out != "" {
		// Size from the file rather than the encoder: what matters to
		// somebody about to load this into a producer is what landed
		// on disk.
		var mb float64
		if st, err := os.Stat(*out); err == nil {
			mb = float64(st.Size()) / (1024 * 1024)
		}
		_, _ = fmt.Fprintf(stderr,
			"wrote %s: %d matrices × %d levels × %d×%d, %.1f MB, elapsed %s\n",
			*out, *matrices, *levels, *size, *size, mb,
			time.Since(start).Round(time.Millisecond))
	}
	return 0
}

func buildExport(nMatrices, size, nLevels int) *canonical.Export {
	rootDesc := fmt.Sprintf("Probel scale bench: %d matrices × %d levels × %d×%d",
		nMatrices, nLevels, size, size)

	root := &canonical.Node{
		Header: canonical.Header{
			Number:      1,
			Identifier:  "router",
			Path:        "router",
			OID:         "1",
			Description: &rootDesc,
			IsOnline:    true,
			Access:      canonical.AccessRead,
			Children:    make([]canonical.Element, 0, nMatrices),
		},
	}

	for m := 0; m < nMatrices; m++ {
		root.Children = append(root.Children, buildMatrix(m, size, nLevels))
	}

	return &canonical.Export{Root: root}
}

func buildMatrix(matrixIdx, size, nLevels int) *canonical.Matrix {
	ident := fmt.Sprintf("matrix-%d", matrixIdx)
	path := "router." + ident
	desc := fmt.Sprintf("scale bench matrix %d (%dx%d, %d levels)",
		matrixIdx, size, size, nLevels)

	labels := make([]canonical.MatrixLabel, nLevels)
	targetLabels := make(map[string]map[string]string, nLevels)
	sourceLabels := make(map[string]map[string]string, nLevels)

	for l := 0; l < nLevels; l++ {
		lvlKey := fmt.Sprintf("L%d", l)
		lvlPath := fmt.Sprintf("%s.level-%d", path, l)
		levelDesc := lvlKey
		labels[l] = canonical.MatrixLabel{
			BasePath:    lvlPath,
			Description: &levelDesc,
		}

		tgt := make(map[string]string, size)
		src := make(map[string]string, size)
		for i := 0; i < size; i++ {
			k := strconv.Itoa(i)
			tgt[k] = fmt.Sprintf("TGT_M%d_L%d_%05d", matrixIdx, l, i)
			src[k] = fmt.Sprintf("SRC_M%d_L%d_%05d", matrixIdx, l, i)
		}
		targetLabels[lvlKey] = tgt
		sourceLabels[lvlKey] = src
	}

	return &canonical.Matrix{
		Header: canonical.Header{
			Number:      matrixIdx + 1,
			Identifier:  ident,
			Path:        path,
			OID:         fmt.Sprintf("1.%d", matrixIdx+1),
			Description: &desc,
			IsOnline:    true,
			Access:      canonical.AccessReadWrite,
			Children:    canonical.EmptyChildren(),
		},
		Type:         canonical.MatrixOneToN,
		Mode:         canonical.ModeLinear,
		TargetCount:  int64(size),
		SourceCount:  int64(size),
		Labels:       labels,
		Targets:      []canonical.MatrixTarget{},
		Sources:      []canonical.MatrixSource{},
		Connections:  []canonical.MatrixConnection{},
		TargetLabels: targetLabels,
		SourceLabels: sourceLabels,
	}
}
