// acp convert — offline format translation between snapshot formats.
// No device connection required; reads an existing json/yaml/csv file
// and rewrites it in another format. Round-trip is lossless for
// json↔yaml; csv is deliberately narrower (see internal/export/csv.go
// header comment) so json→csv→json may drop nested fields.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"acp/internal/export"
)

func runConvert(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("convert", flag.ExitOnError)
	in := fs.String("in", "", "input snapshot file (.json, .yaml, .csv)")
	out := fs.String("out", "", "output file path (format derived from extension)")
	format := fs.String("format", "", "output format override: json | yaml | csv")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" {
		return fmt.Errorf("--in is required")
	}
	if *out == "" && *format == "" {
		return fmt.Errorf("either --out or --format is required")
	}

	snap, err := export.LoadSnapshot(*in)
	if err != nil {
		return err
	}

	// Resolve the target format: --format wins, else extension of --out.
	fmtStr := *format
	if fmtStr == "" {
		switch strings.ToLower(filepath.Ext(*out)) {
		case ".yaml", ".yml":
			fmtStr = "yaml"
		case ".csv":
			fmtStr = "csv"
		default:
			fmtStr = "json"
		}
	}
	fmtEnum, err := export.ParseFormat(fmtStr)
	if err != nil {
		return err
	}

	// Output: stdout when --out omitted, file otherwise.
	var w io.Writer = os.Stdout
	if *out != "" {
		f, ferr := os.Create(*out)
		if ferr != nil {
			return fmt.Errorf("create %s: %w", *out, ferr)
		}
		defer func() { _ = f.Close() }()
		w = f
	}

	switch fmtEnum {
	case export.FormatJSON:
		if err := export.WriteJSON(w, snap); err != nil {
			return err
		}
	case export.FormatYAML:
		if err := export.WriteYAML(w, snap); err != nil {
			return err
		}
	case export.FormatCSV:
		if err := export.WriteCSV(w, snap); err != nil {
			return err
		}
	}

	if *out != "" {
		fmt.Fprintf(os.Stderr, "converted %s → %s (%s)\n", *in, *out, fmtEnum)
	}
	return nil
}

func helpConvert() {
	fmt.Println(`acp convert — translate a snapshot file between json / yaml / csv
(offline — no device connection needed)

IN   acp convert --in device.json --out device.csv
OUT  converted device.json → device.csv (csv)

USAGE
  acp convert --in FILE --out FILE [--format json|yaml|csv]
  acp convert --in FILE --format csv              (writes to stdout)

FLAGS
  --in FILE       input snapshot (.json, .yaml, .csv). Format detected
                  from extension.
  --out FILE      output file. Format detected from extension unless
                  --format is given. Omit to stream to stdout.
  --format F      override output format: json, yaml, csv.

NOTES
  • json ↔ yaml is lossless (same shape).
  • csv is flat (one row per object). json → csv → json may drop
    nested fields like slot_status arrays or preset-depth detail.
  • Use this when you want to edit an existing snapshot as CSV in
    Excel, then convert back to json before "acp import --file …".

EXAMPLES
  acp convert --in device.json --out device.csv
  acp convert --in device.csv  --out device.json
  acp convert --in device.json --format yaml > device.yaml`)
}
