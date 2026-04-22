// Command gen-probel-tree emits a canonical JSON tree suitable for the
// Probel SW-P-08 producer (--tree flag). Intended for scale benchmarking
// (see memory/project_scale_bench_2mtx_65535.md).
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
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"acp/internal/export/canonical"
)

func main() {
	var (
		matrices = flag.Int("matrices", 2, "number of matrices")
		size     = flag.Int("size", 65535, "target/source count per (matrix, level)")
		levels   = flag.Int("levels", 1, "levels per matrix")
		out      = flag.String("out", "", "output path (default stdout)")
	)
	flag.Parse()

	if *matrices <= 0 || *matrices > 255 {
		fatal("matrices must be 1..255")
	}
	if *size <= 0 || *size > 65535 {
		fatal("size must be 1..65535")
	}
	if *levels <= 0 || *levels > 255 {
		fatal("levels must be 1..255")
	}

	start := time.Now()
	exp := buildExport(*matrices, *size, *levels)

	w := os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fatal(err.Error())
		}
		defer func() { _ = f.Close() }()
		w = f
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(exp); err != nil {
		fatal(err.Error())
	}

	if *out != "" {
		st, _ := os.Stat(*out)
		fmt.Fprintf(os.Stderr,
			"wrote %s: %d matrices × %d levels × %d×%d, %.1f MB, elapsed %s\n",
			*out, *matrices, *levels, *size, *size,
			float64(st.Size())/(1024*1024),
			time.Since(start).Round(time.Millisecond))
	}
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

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "gen-probel-tree:", msg)
	os.Exit(1)
}
