package main

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"dhs/internal/probel-sw08p/codec"
)

// runProbelUpdateName issues rx 117 UPDATE NAME REQUEST — the SW-P-08
// write-label command. It pushes one or more source / source-assoc /
// dest-assoc / UMD labels starting at --first-id on (matrix[, level]).
// Fire-and-forget: §3.2.26 says the matrix sends no reply, so the call
// returns as soon as the frame is ACKed on the wire.
//
// Spec note worth surfacing (carried from the consumer doc-comment):
// pushing labels to an Aurora/system-editor-managed controller causes a
// database mismatch the next time its config editor connects online.
// Use deliberately.
func runProbelUpdateName(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-update-name", flag.ContinueOnError)
	typ := fs.String("type", "source",
		"name class: source | source-assoc | dest-assoc | umd")
	width := fs.Int("width", 16, "name width in chars: 4 | 8 | 12 | 16")
	matrix := fs.Int("matrix", 0, "matrix id (0-255)")
	level := fs.Int("level", 0, "level id (0-255; source-name only)")
	firstID := fs.Int("first-id", 0, "id of the first name (0-65535)")
	names := fs.String("names", "",
		"comma-separated labels, applied from --first-id upward")
	timeout := fs.Duration("timeout", 5*time.Second, "operation timeout")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}

	nameType, err := parseUpdateNameType(*typ)
	if err != nil {
		return err
	}
	nameLen, err := parseUpdateNameWidth(*width)
	if err != nil {
		return err
	}
	if *matrix < 0 || *matrix > 255 {
		return fmt.Errorf("--matrix out of range (0-255)")
	}
	if *level < 0 || *level > 255 {
		return fmt.Errorf("--level out of range (0-255)")
	}
	if *firstID < 0 || *firstID > 0xFFFF {
		return fmt.Errorf("--first-id out of range (0-65535)")
	}
	if strings.TrimSpace(*names) == "" {
		return fmt.Errorf("--names requires at least one label")
	}
	labels := strings.Split(*names, ",")

	params := codec.UpdateNameRequestParams{
		NameType:   nameType,
		NameLength: nameLen,
		MatrixID:   uint8(*matrix),
		LevelID:    uint8(*level),
		FirstID:    uint16(*firstID),
		Names:      labels,
	}

	cctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	p, closer, err := dialProbel(cctx, addr)
	if err != nil {
		return err
	}
	defer closer()
	if err := p.UpdateNameRequest(cctx, params); err != nil {
		return err
	}
	fmt.Printf("update-name sent  type=%s width=%d matrix=%d level=%d first_id=%d count=%d\n",
		*typ, nameLen.Bytes(), *matrix, *level, *firstID, len(labels))
	return nil
}

// parseUpdateNameType maps the CLI --type string to the codec enum.
func parseUpdateNameType(s string) (codec.UpdateNameType, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "source", "src":
		return codec.UpdateNameSource, nil
	case "source-assoc", "src-assoc":
		return codec.UpdateNameSourceAssoc, nil
	case "dest-assoc", "dst-assoc":
		return codec.UpdateNameDestAssoc, nil
	case "umd", "umd-label":
		return codec.UpdateNameUMDLabel, nil
	default:
		return 0, fmt.Errorf("unknown --type %q (want source|source-assoc|dest-assoc|umd)", s)
	}
}

// parseUpdateNameWidth maps the CLI --width int to the codec NameLength enum.
func parseUpdateNameWidth(w int) (codec.NameLength, error) {
	switch w {
	case 4:
		return codec.NameLen4, nil
	case 8:
		return codec.NameLen8, nil
	case 12:
		return codec.NameLen12, nil
	case 16:
		return codec.NameLen16, nil
	default:
		return 0, fmt.Errorf("unknown --width %d (want 4|8|12|16)", w)
	}
}
