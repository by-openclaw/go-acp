package main

// `dhs consumer rcp routes | take | mnemonics | set-mnemonic | ensure` —
// the router half of the RCP connector: crosspoints, mnemonic tables,
// and the plan an Ansible play converges on.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"dhs/internal/rcp/codec"
	rcpc "dhs/internal/rcp/consumer"
)

// rcpRouter adds --device / --index; the default is the RouteMaster.
func rcpRouter(fs *flag.FlagSet) (device *string, index *int) {
	return fs.String("device", rcpc.RouteMaster.Device, "router device name in Cerebrum's System View"),
		fs.Int("index", rcpc.RouteMaster.Index, "sub-device index")
}

// sortByKeyID orders "<name>_<id>" keys by their id.
func sortByKeyID(keys []string) {
	sort.Slice(keys, func(i, j int) bool {
		a, _ := codec.KeyID(keys[i])
		b, _ := codec.KeyID(keys[j])
		return a < b
	})
}

func runRCPRoutes(ctx context.Context, args []string) error {
	f := newRCPFlags("routes")
	device, index := rcpRouter(f.fs)
	dest := f.fs.Int64("dest", 0, "one destination id (default: all)")
	asJSON := f.fs.Bool("json", false, "print the table as JSON")
	c, p, err := f.client(args)
	if err != nil {
		return err
	}
	return f.session(ctx, c, p, func() error {
		table, err := c.Routes(ctx, rcpc.Router{Device: *device, Index: *index}, *dest)
		if err != nil {
			return err
		}
		if *asJSON {
			return printJSON(table)
		}
		keys := make([]string, 0, len(table))
		for k := range table {
			keys = append(keys, k)
		}
		sortByKeyID(keys)
		for _, k := range keys {
			d := table[k]
			id, _ := codec.KeyID(k)
			fmt.Printf("%-6d %-20q", id, d.Name)
			levels := make([]string, 0, len(d.Levels))
			for l := range d.Levels {
				levels = append(levels, l)
			}
			sortByKeyID(levels)
			for _, l := range levels {
				lid, _ := codec.KeyID(l)
				s := d.Levels[l].Source
				if s.ID == 0 {
					fmt.Printf(" | L%d -", lid)
					continue
				}
				fmt.Printf(" | L%d <- %d %q", lid, s.ID, s.Name)
			}
			fmt.Println()
		}
		return nil
	})
}

func runRCPTake(ctx context.Context, args []string) error {
	f := newRCPFlags("take")
	device, index := rcpRouter(f.fs)
	dest := f.fs.Int64("dest", 0, "destination id")
	src := f.fs.Int64("src", 0, "source id")
	level := f.fs.Int("level", 0, "one level (default 0: every level the destination exists on)")
	c, p, err := f.client(args)
	if err != nil {
		return err
	}
	return f.session(ctx, c, p, func() error {
		if err := c.Take(ctx, rcpc.Router{Device: *device, Index: *index}, *dest, *src, *level); err != nil {
			return err
		}
		fmt.Printf("accepted: destination %d <- source %d\n", *dest, *src)
		return nil
	})
}

func runRCPMnemonics(ctx context.Context, args []string) error {
	f := newRCPFlags("mnemonics")
	device, index := rcpRouter(f.fs)
	kind := f.fs.String("kind", "source", "source | destination | level")
	c, p, err := f.client(args)
	if err != nil {
		return err
	}
	k, err := codec.ParseMnemonicKind(*kind)
	if err != nil {
		return err
	}
	return f.session(ctx, c, p, func() error {
		table, err := c.Mnemonics(ctx, rcpc.Router{Device: *device, Index: *index}, k)
		if err != nil {
			return err
		}
		ids := make([]int64, 0, len(table))
		for id := range table {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			e := table[id]
			fmt.Printf("%-6d %-20q", id, e.Original)
			alts := make([]string, 0, len(e.Alternates))
			for name, v := range e.Alternates {
				alts = append(alts, name+"="+v)
			}
			sort.Strings(alts)
			if len(alts) > 0 {
				fmt.Printf(" alt: %s", strings.Join(alts, ", "))
			}
			fmt.Println()
		}
		return nil
	})
}

// stringList is a repeatable string flag.
type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func runRCPSetMnemonic(ctx context.Context, args []string) error {
	f := newRCPFlags("set-mnemonic")
	device, index := rcpRouter(f.fs)
	kind := f.fs.String("kind", "source", "source | destination | level")
	id := f.fs.Int64("id", 0, "id of the source, destination or level")
	mnemonic := f.fs.String("mnemonic", "", "original mnemonic")
	var alts stringList
	f.fs.Var(&alts, "alt", "alternate mnemonic as NAME=VALUE (repeatable; NAME must exist in Cerebrum)")
	c, p, err := f.client(args)
	if err != nil {
		return err
	}
	k, err := codec.ParseMnemonicKind(*kind)
	if err != nil {
		return err
	}
	var u codec.MnemonicUpdate
	f.fs.Visit(func(fl *flag.Flag) {
		if fl.Name == "mnemonic" {
			u.Original = mnemonic
		}
	})
	for _, a := range alts {
		name, value, ok := strings.Cut(a, "=")
		if !ok || name == "" {
			return fmt.Errorf("consumer rcp set-mnemonic: --alt %q is not NAME=VALUE", a)
		}
		if u.Alternates == nil {
			u.Alternates = map[string]string{}
		}
		u.Alternates[name] = value
	}
	return f.session(ctx, c, p, func() error {
		if err := c.SetMnemonic(ctx, rcpc.Router{Device: *device, Index: *index}, k, *id, u); err != nil {
			return err
		}
		fmt.Printf("accepted: %s %d mnemonic updated\n", k, *id)
		return nil
	})
}

func runRCPEnsure(ctx context.Context, args []string) error {
	f := newRCPFlags("ensure")
	planFile := f.fs.String("plan", "", "the plan as a JSON file")
	check := f.fs.Bool("check", false, "report what would change, write nothing")
	state := f.fs.String("state", "present", "present | absent (absent removes everything the plan names)")
	fedOptional := f.fs.Bool("federation-optional", false, "a federation entry the server accepts and does not create is reported as pending, not as a failure")
	c, p, err := f.client(args)
	if err != nil {
		return err
	}
	if *planFile == "" {
		return errors.New("consumer rcp ensure: --plan is required")
	}
	if *state != "present" && *state != "absent" {
		return fmt.Errorf("consumer rcp ensure: --state %q (want present or absent)", *state)
	}
	raw, err := os.ReadFile(*planFile)
	if err != nil {
		return err
	}
	var plan rcpc.Plan
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(&plan); err != nil {
		return fmt.Errorf("rcp: %s: %w", *planFile, err)
	}
	var report rcpc.Report
	err = f.session(ctx, c, p, func() error {
		var err error
		report, err = c.Ensure(ctx, plan, rcpc.EnsureOptions{Check: *check, Absent: *state == "absent", FederationOptional: *fedOptional})
		return err
	})
	for _, ch := range report.Changes {
		line := ch.Action + "  " + ch.Target
		if ch.Detail != "" {
			line += "  (" + ch.Detail + ")"
		}
		fmt.Println(line)
	}
	for _, pending := range report.Pending {
		fmt.Println("pending  " + pending + "  (the server accepted the create and created nothing: no federation)")
	}
	// The last line is the verdict a play reads.
	word := "changed"
	if *check {
		word = "would_change"
	}
	fmt.Printf("%s=%d pending=%d\n", word, len(report.Changes), len(report.Pending))
	return err
}

func runRCPDevices(ctx context.Context, args []string) error {
	f := newRCPFlags("devices")
	c, p, err := f.client(args)
	if err != nil {
		return err
	}
	return f.session(ctx, c, p, func() error {
		devs, err := c.Devices(ctx)
		if err != nil {
			return err
		}
		for _, d := range devs {
			slots := make([]string, 0, len(d.SubDevices))
			for _, s := range d.SubDevices {
				slots = append(slots, fmt.Sprint(s.Index))
			}
			kinds := make([]string, 0, len(d.Resources))
			for k := range d.Resources {
				kinds = append(kinds, k)
			}
			sort.Strings(kinds)
			fmt.Printf("%-28q %-22q %-15s slots=[%s] resources=%s\n", d.Name, d.Model, d.IP, strings.Join(slots, ","), strings.Join(kinds, ","))
		}
		return nil
	})
}

func runRCPObject(ctx context.Context, verb string, args []string) error {
	f := newRCPFlags(verb)
	device := f.fs.String("device", rcpc.RouteMaster.Device, "device name in Cerebrum's System View")
	index := f.fs.Int("index", 0, "sub-device (slot) index")
	path := f.fs.String("path", "", "dotted object path, e.g. License.License_Type")
	value := f.fs.String("value", "", "value to write (set-object)")
	c, p, err := f.client(args)
	if err != nil {
		return err
	}
	if *path == "" {
		return fmt.Errorf("consumer rcp %s: --path is required", verb)
	}
	return f.session(ctx, c, p, func() error {
		if verb == "set-object" {
			if err := c.SetObject(ctx, *device, *index, *path, *value); err != nil {
				return err
			}
			fmt.Printf("accepted: %s/%d %s = %s\n", *device, *index, *path, *value)
			return nil
		}
		a, err := c.GetObject(ctx, *device, *index, *path)
		if err != nil {
			return err
		}
		switch {
		case a.IsTable:
			fmt.Printf("table  indices=[%s]\n", strings.Join(a.Indices, ","))
		case a.Value == nil:
			// The server answers the same for a group node and for a
			// path that does not exist.
			return fmt.Errorf("rcp: %s/%d %s holds no value (a group node, or no such object)", *device, *index, *path)
		default:
			fmt.Printf("value = %v\n", a.Value.Current)
			fmt.Printf("writable = %v\n", a.Value.Writable)
			if len(a.Value.AllowedValues) > 0 {
				fmt.Printf("allowed = %s\n", strings.Join(a.Value.AllowedValues, " | "))
			}
		}
		return nil
	})
}
