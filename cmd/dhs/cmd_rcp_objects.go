package main

// `dhs consumer rcp objects <host>` — several device objects read in ONE
// session. A caller that waits on a condition (a node moved to its slot,
// say) reads all it needs with one login instead of one login per object.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	rcpc "dhs/internal/rcp/consumer"
)

// rcpObjectRef is one object to read: the sub-device (slot) index and the
// dotted path.
type rcpObjectRef struct {
	Index int
	Path  string
}

// parseRCPObjectRef reads "INDEX PATH": the index, one space, the path.
// The path may itself hold spaces ("Devices.[IP Output 8].Senders").
func parseRCPObjectRef(s string) (rcpObjectRef, error) {
	idx, path, ok := strings.Cut(s, " ")
	n, err := strconv.Atoi(idx)
	if !ok || err != nil || n < 0 || strings.TrimSpace(path) == "" {
		return rcpObjectRef{}, fmt.Errorf("--read %q is not \"INDEX PATH\"", s)
	}
	return rcpObjectRef{Index: n, Path: path}, nil
}

// rcpObjectLine is the one line `objects` prints for one read, fields
// separated by tabs: index, path, kind, value.
//
//	value   the current value
//	table   the number of rows, then the row indices separated by commas
//	none    the path holds no value (a group node, or no such object)
//	error   the read failed; the text of the error
func rcpObjectLine(ref rcpObjectRef, a rcpc.ObjectAnswer, err error) string {
	kind, value := "none", ""
	switch {
	case err != nil:
		kind, value = "error", strings.ReplaceAll(err.Error(), "\n", " ")
	case a.IsTable:
		kind, value = "table", strconv.Itoa(len(a.Indices))+"\t"+strings.Join(a.Indices, ",")
	case a.Value != nil:
		kind, value = "value", fmt.Sprint(a.Value.Current)
	}
	return strconv.Itoa(ref.Index) + "\t" + ref.Path + "\t" + kind + "\t" + value
}

func runRCPObjects(ctx context.Context, args []string) error {
	f := newRCPFlags("objects")
	device := f.fs.String("device", rcpc.RouteMaster.Device, "device name in Cerebrum's System View")
	var reads stringList
	f.fs.Var(&reads, "read", "one object as \"INDEX PATH\" (repeatable)")
	c, p, err := f.client(args)
	if err != nil {
		return err
	}
	if len(reads) == 0 {
		return errors.New("consumer rcp objects: at least one --read \"INDEX PATH\" is required")
	}
	refs := make([]rcpObjectRef, 0, len(reads))
	for _, r := range reads {
		ref, err := parseRCPObjectRef(r)
		if err != nil {
			return fmt.Errorf("consumer rcp objects: %w", err)
		}
		refs = append(refs, ref)
	}
	return f.session(ctx, c, p, func() error {
		// A read that fails is reported on its own line and the others
		// are still read: a slot that does not exist yet is an answer.
		for _, ref := range refs {
			a, err := c.GetObject(ctx, *device, ref.Index, ref.Path)
			fmt.Println(rcpObjectLine(ref, a, err))
		}
		return nil
	})
}
