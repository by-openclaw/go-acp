package main

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"acp/internal/probel-sw08p/codec"
)

// parseNameLen maps the --size flag (4 | 8 | 12 | 16) into the
// SW-P-08 NameLength enum. 8 is the most common default on the wire.
func parseNameLen(s string) (codec.NameLength, error) {
	switch s {
	case "4":
		return codec.NameLen4, nil
	case "8":
		return codec.NameLen8, nil
	case "12":
		return codec.NameLen12, nil
	case "16":
		return codec.NameLen16, nil
	}
	return codec.NameLen8, fmt.Errorf("--size: expected 4|8|12|16, got %q", s)
}

// parseProbelNameFlags parses the shared flag set used by every
// name-family subcommand: <host:port> --matrix N --level N --size 4|8|12|16
// [--src N | --dst N] [--timeout DUR].
func parseProbelNameFlags(args []string, want struct{ level, src, dst bool }) (probelFlags, codec.NameLength, error) {
	fs := flag.NewFlagSet("probel-names", flag.ContinueOnError)
	pf := probelFlags{}
	size := fs.String("size", "8", "name length on the wire: 4 | 8 | 12 | 16 chars")
	fs.IntVar(&pf.matrix, "matrix", 0, "matrix id (0-255)")
	fs.IntVar(&pf.level, "level", 0, "level id (0-15); ignored by assoc-name variants")
	fs.IntVar(&pf.src, "src", 0, "source id (0-65535)")
	fs.IntVar(&pf.dst, "dst", 0, "destination id (0-65535)")
	fs.DurationVar(&pf.timeout, "timeout", 0, "operation timeout (0 = default 5s)")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return pf, 0, fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return pf, 0, err
	}
	pf.addr = addr
	if pf.timeout == 0 {
		pf.timeout = defaultProbelTimeout()
	}
	nameLen, err := parseNameLen(*size)
	if err != nil {
		return pf, 0, err
	}
	if pf.matrix < 0 || pf.matrix > 255 {
		return pf, 0, fmt.Errorf("--matrix out of range (0-255)")
	}
	if want.level && (pf.level < 0 || pf.level > 15) {
		return pf, 0, fmt.Errorf("--level out of range (0-15)")
	}
	if want.src && (pf.src < 0 || pf.src > 0xFFFF) {
		return pf, 0, fmt.Errorf("--src out of range (0-65535)")
	}
	if want.dst && (pf.dst < 0 || pf.dst > 0xFFFF) {
		return pf, 0, fmt.Errorf("--dst out of range (0-65535)")
	}
	return pf, nameLen, nil
}

// defaultProbelTimeout returns the default per-subcommand timeout used
// when --timeout isn't specified. 5s matches the other probel subcommands.
func defaultProbelTimeout() time.Duration { return 5 * time.Second }

func runProbelAllSourceNames(ctx context.Context, args []string) error {
	pf, nameLen, err := parseProbelNameFlags(args, struct{ level, src, dst bool }{level: true})
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, pf.timeout)
	defer cancel()
	p, closer, err := dialProbel(cctx, pf.addr)
	if err != nil {
		return err
	}
	defer closer()
	r, err := p.AllSourceNames(cctx, uint8(pf.matrix), uint8(pf.level), nameLen)
	if err != nil {
		return err
	}
	fmt.Printf("source names  matrix=%d level=%d size=%d first=%d count=%d\n",
		r.MatrixID, r.LevelID, nameLen.Bytes(), r.FirstSourceID, len(r.Names))
	for i, n := range r.Names {
		fmt.Printf("  src=%d  %q\n", int(r.FirstSourceID)+i, strings.TrimRight(n, "\x00 "))
	}
	return nil
}

func runProbelSingleSourceName(ctx context.Context, args []string) error {
	pf, nameLen, err := parseProbelNameFlags(args, struct{ level, src, dst bool }{level: true, src: true})
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, pf.timeout)
	defer cancel()
	p, closer, err := dialProbel(cctx, pf.addr)
	if err != nil {
		return err
	}
	defer closer()
	name, err := p.SingleSourceName(cctx, uint8(pf.matrix), uint8(pf.level), nameLen, uint16(pf.src))
	if err != nil {
		return err
	}
	fmt.Printf("source name  matrix=%d level=%d src=%d  %q\n",
		pf.matrix, pf.level, pf.src, strings.TrimRight(name, "\x00 "))
	return nil
}

func runProbelAllDestAssocNames(ctx context.Context, args []string) error {
	pf, nameLen, err := parseProbelNameFlags(args, struct{ level, src, dst bool }{})
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, pf.timeout)
	defer cancel()
	p, closer, err := dialProbel(cctx, pf.addr)
	if err != nil {
		return err
	}
	defer closer()
	r, err := p.AllDestAssocNames(cctx, uint8(pf.matrix), nameLen)
	if err != nil {
		return err
	}
	fmt.Printf("dest assoc names  matrix=%d size=%d first=%d count=%d\n",
		r.MatrixID, nameLen.Bytes(), r.FirstDestAssociationID, len(r.Names))
	for i, n := range r.Names {
		fmt.Printf("  dst=%d  %q\n", int(r.FirstDestAssociationID)+i, strings.TrimRight(n, "\x00 "))
	}
	return nil
}

func runProbelSingleDestAssocName(ctx context.Context, args []string) error {
	pf, nameLen, err := parseProbelNameFlags(args, struct{ level, src, dst bool }{dst: true})
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, pf.timeout)
	defer cancel()
	p, closer, err := dialProbel(cctx, pf.addr)
	if err != nil {
		return err
	}
	defer closer()
	name, err := p.SingleDestAssocName(cctx, uint8(pf.matrix), nameLen, uint16(pf.dst))
	if err != nil {
		return err
	}
	fmt.Printf("dest assoc name  matrix=%d dst=%d  %q\n",
		pf.matrix, pf.dst, strings.TrimRight(name, "\x00 "))
	return nil
}

func runProbelAllSourceAssocNames(ctx context.Context, args []string) error {
	pf, nameLen, err := parseProbelNameFlags(args, struct{ level, src, dst bool }{})
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, pf.timeout)
	defer cancel()
	p, closer, err := dialProbel(cctx, pf.addr)
	if err != nil {
		return err
	}
	defer closer()
	r, err := p.AllSourceAssocNames(cctx, uint8(pf.matrix), nameLen)
	if err != nil {
		return err
	}
	fmt.Printf("source assoc names  matrix=%d size=%d first=%d count=%d\n",
		r.MatrixID, nameLen.Bytes(), r.FirstSourceAssociationID, len(r.Names))
	for i, n := range r.Names {
		fmt.Printf("  src=%d  %q\n", int(r.FirstSourceAssociationID)+i, strings.TrimRight(n, "\x00 "))
	}
	return nil
}

func runProbelSingleSourceAssocName(ctx context.Context, args []string) error {
	pf, nameLen, err := parseProbelNameFlags(args, struct{ level, src, dst bool }{src: true})
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, pf.timeout)
	defer cancel()
	p, closer, err := dialProbel(cctx, pf.addr)
	if err != nil {
		return err
	}
	defer closer()
	name, err := p.SingleSourceAssocName(cctx, uint8(pf.matrix), nameLen, uint16(pf.src))
	if err != nil {
		return err
	}
	fmt.Printf("source assoc name  matrix=%d src=%d  %q\n",
		pf.matrix, pf.src, strings.TrimRight(name, "\x00 "))
	return nil
}

// runProbelDiscover fires every read-only discovery request against the
// target and prints a summary. Useful when pointing at an unknown matrix
// (e.g. VSM acting as SW-P-08 server) — you get dual-status, tally dumps,
// source + dest labels for M0 L0 in one go.
func runProbelDiscover(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-discover", flag.ContinueOnError)
	size := fs.String("size", "8", "name length on the wire: 4 | 8 | 12 | 16")
	matrix := fs.Int("matrix", 0, "matrix id to probe (0-255)")
	level := fs.Int("level", 0, "level id to probe (0-15)")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	nameLen, err := parseNameLen(*size)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	p, closer, err := dialProbel(cctx, addr)
	if err != nil {
		return err
	}
	defer closer()

	fmt.Printf("\n=== discover %s (M=%d L=%d size=%d) ===\n", addr, *matrix, *level, nameLen.Bytes())

	if ds, err := p.DualControllerStatus(cctx); err == nil {
		fmt.Printf("dual-status  master_active=%v active=%v idle_faulty=%v\n",
			!ds.SlaveActive, ds.Active, ds.IdleControllerFaulty)
	} else {
		fmt.Printf("dual-status  ERROR: %v\n", err)
	}

	if r, err := p.AllSourceNames(cctx, uint8(*matrix), uint8(*level), nameLen); err == nil {
		fmt.Printf("\nsource names  matrix=%d level=%d count=%d (first=%d)\n",
			r.MatrixID, r.LevelID, len(r.Names), r.FirstSourceID)
		for i, n := range r.Names {
			fmt.Printf("  src=%d  %q\n", int(r.FirstSourceID)+i, strings.TrimRight(n, "\x00 "))
		}
	} else {
		fmt.Printf("\nsource names  ERROR: %v\n", err)
	}

	if r, err := p.AllDestAssocNames(cctx, uint8(*matrix), nameLen); err == nil {
		fmt.Printf("\ndest names  matrix=%d count=%d (first=%d)\n",
			r.MatrixID, len(r.Names), r.FirstDestAssociationID)
		for i, n := range r.Names {
			fmt.Printf("  dst=%d  %q\n", int(r.FirstDestAssociationID)+i, strings.TrimRight(n, "\x00 "))
		}
	} else {
		fmt.Printf("\ndest names  ERROR: %v\n", err)
	}

	if r, err := p.CrosspointTallyDump(cctx, uint8(*matrix), uint8(*level)); err == nil {
		if r.IsWord {
			fmt.Printf("\ntally dump (word)  matrix=%d level=%d first=%d count=%d\n",
				r.Word.MatrixID, r.Word.LevelID, r.Word.FirstDestinationID, len(r.Word.SourceIDs))
			for i, s := range r.Word.SourceIDs {
				if int(s) != 0 || i < 4 {
					fmt.Printf("  dst=%d → src=%d\n", int(r.Word.FirstDestinationID)+i, s)
				}
				if i >= 15 {
					fmt.Printf("  ... (first 16)\n")
					break
				}
			}
		} else {
			fmt.Printf("\ntally dump (byte)  matrix=%d level=%d first=%d count=%d\n",
				r.Byte.MatrixID, r.Byte.LevelID, r.Byte.FirstDestinationID, len(r.Byte.SourceIDs))
			for i, s := range r.Byte.SourceIDs {
				if s != 0 || i < 4 {
					fmt.Printf("  dst=%d → src=%d\n", int(r.Byte.FirstDestinationID)+i, s)
				}
				if i >= 15 {
					fmt.Printf("  ... (first 16)\n")
					break
				}
			}
		}
	} else {
		fmt.Printf("\ntally dump  ERROR: %v\n", err)
	}

	return nil
}
