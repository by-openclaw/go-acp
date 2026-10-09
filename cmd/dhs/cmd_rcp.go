package main

// `dhs consumer rcp <verb> <host>` — the EVS Cerebrum RCP connector
// (internal/rcp): the session, and RouteMaster sources and destinations
// in the four collections (local and federation; virtual is a flag on
// an IO, not a collection).
//
//   dhs consumer rcp info   <host>
//   dhs consumer rcp list   <host> [--kind sources]
//   dhs consumer rcp get    <host> --kind destinations --id 1
//   dhs consumer rcp create <host> --kind sources --count 2 --mnemonic DHS-TEST-0001
//   dhs consumer rcp set    <host> --kind sources --id 7 --mnemonic CAM-7
//   dhs consumer rcp delete <host> --kind sources --id 7

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"dhs/internal/consumer/compliance"
	"dhs/internal/rcp/codec"
	rcpc "dhs/internal/rcp/consumer"
)

const rcpUsage = `usage: dhs consumer rcp <verb> <host> [flags]

  info                    API version (no login), then the logged-in user and the
                          back-channel port
  list                    ids of one RouteMaster collection, or of all four
  get                     one IO in full            --kind K --id N [--json]
  export                  every IO of every collection as JSON   [--out-dir D]
  create                  append IOs                --kind K [--count N] [body flags]
  set                     change one IO             --kind K --id N [body flags]
  delete                  remove one IO             --kind K --id N

  routes                  the crosspoint table      [--dest N] [--json]
  take                    make a crosspoint         --dest N --src N [--level L]
  mnemonics               a router mnemonic table   --kind source|destination|level
  set-mnemonic            change one mnemonic       --kind K --id N [--mnemonic M] [--alt NAME=VALUE]
  ensure                  converge on a plan        --plan FILE [--check] [--state absent]
                          [--federation-optional]   last line: changed=N pending=N

  devices                 the devices registered in Cerebrum, with their slots
  object                  read one device object    --device D --index N --path A.B.C
  set-object              write one device object   --device D --index N --path A.B.C --value V

  --device D   device for routes/take/mnemonics/object (default Cerebrum: the RouteMaster)
  --index N    its sub-device (slot) index (default 0)

  --kind K     sources | destinations | federation-sources | federation-destinations
  body flags   --mnemonic M  --virtual[=false]  --tie-line-inhibit[=false]
               --tie-line-group N (destinations)  --federation-uid N (local IOs; 0 unlinks)
               --body FILE   the whole body as JSON — levels, tags, device bindings
                             (flags given beside it override its fields)

  --port N     HTTP port set in Cerebrum's RCP configuration (default 8080)
  --tls        https            --verify-tls   verify the certificate
  --user U     (or $DHS_CEREBRUM_USER)        --pass P   (or $DHS_CEREBRUM_PASS)
  --timeout D  per-request timeout (default 8s)

create answers no ids: the server allocates them. The verb lists the collection
before and after and prints the ids that appeared.`

// rcpFlags are the settings every verb takes.
type rcpFlags struct {
	fs        *flag.FlagSet
	port      int
	tls       bool
	verifyTLS bool
	user      string
	pass      string
	timeout   time.Duration
}

func newRCPFlags(verb string) *rcpFlags {
	f := &rcpFlags{fs: flag.NewFlagSet("consumer rcp "+verb, flag.ContinueOnError)}
	f.fs.IntVar(&f.port, "port", rcpc.DefaultPort, "HTTP port of Cerebrum's RCP configuration")
	f.fs.BoolVar(&f.tls, "tls", false, "use https")
	f.fs.BoolVar(&f.verifyTLS, "verify-tls", false, "verify the server certificate (default: skip)")
	f.fs.StringVar(&f.user, "user", os.Getenv("DHS_CEREBRUM_USER"), "username (or $DHS_CEREBRUM_USER)")
	f.fs.StringVar(&f.pass, "pass", os.Getenv("DHS_CEREBRUM_PASS"), "password (or $DHS_CEREBRUM_PASS)")
	f.fs.DurationVar(&f.timeout, "timeout", 0, "per-request timeout (default 8s)")
	return f
}

// client parses the arguments and returns a client for the named host.
func (f *rcpFlags) client(args []string) (*rcpc.Client, *compliance.Profile, error) {
	if err := parseVerbFlags(f.fs, args); err != nil {
		return nil, nil, err
	}
	host := f.fs.Arg(0)
	if host == "" {
		return nil, nil, fmt.Errorf("%s: a host is required (e.g. 10.6.250.5)", f.fs.Name())
	}
	if !strings.Contains(host, ":") {
		host += ":" + strconv.Itoa(f.port)
	}
	p := &compliance.Profile{}
	return rcpc.New(rcpc.Options{Host: host, TLS: f.tls, VerifyTLS: f.verifyTLS, Timeout: f.timeout, Profile: p}), p, nil
}

// session logs in, runs fn, and logs out whatever fn returned.
func (f *rcpFlags) session(ctx context.Context, c *rcpc.Client, p *compliance.Profile, fn func() error) error {
	if f.user == "" || f.pass == "" {
		return errors.New("rcp: a user and a password are required (--user/--pass or $DHS_CEREBRUM_USER/$DHS_CEREBRUM_PASS)")
	}
	if err := c.Login(ctx, f.user, f.pass); err != nil {
		return err
	}
	err := fn()
	if lerr := c.Logout(ctx); lerr != nil && err == nil {
		err = lerr
	}
	if len(p.Snapshot()) > 0 {
		slog.Info("rcp compliance", "summary", p.SummaryLine())
	}
	return err
}

// runRCP dispatches `dhs consumer rcp <verb>`.
func runRCP(ctx context.Context, args []string) error {
	if len(args) == 0 || isHelpToken(args[0]) {
		fmt.Println(rcpUsage)
		return nil
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "info":
		return runRCPInfo(ctx, rest)
	case "list":
		return runRCPList(ctx, rest)
	case "get":
		return runRCPGet(ctx, rest)
	case "export":
		return runRCPExport(ctx, rest)
	case "create", "set":
		return runRCPWrite(ctx, verb, rest)
	case "delete":
		return runRCPDelete(ctx, rest)
	case "routes":
		return runRCPRoutes(ctx, rest)
	case "take":
		return runRCPTake(ctx, rest)
	case "mnemonics":
		return runRCPMnemonics(ctx, rest)
	case "set-mnemonic":
		return runRCPSetMnemonic(ctx, rest)
	case "ensure":
		return runRCPEnsure(ctx, rest)
	case "devices":
		return runRCPDevices(ctx, rest)
	case "object", "set-object":
		return runRCPObject(ctx, verb, rest)
	}
	return fmt.Errorf("consumer rcp: unknown verb %q\n%s", verb, rcpUsage)
}

func runRCPInfo(ctx context.Context, args []string) error {
	f := newRCPFlags("info")
	c, p, err := f.client(args)
	if err != nil {
		return err
	}
	v, err := c.API(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("server       %s\n", c.Base())
	fmt.Printf("protocol     rcp\n")
	fmt.Printf("api_version  %s\n", v)
	if f.user == "" || f.pass == "" {
		fmt.Println("user         (no credentials given — not logged in)")
		return nil
	}
	return f.session(ctx, c, p, func() error {
		who, err := c.WhoAmI(ctx)
		if err != nil {
			return err
		}
		if err := c.Heartbeat(ctx); err != nil {
			return err
		}
		fmt.Printf("user         %s\n", who)
		fmt.Printf("websocket    %d\n", c.WebsocketPort())
		for _, col := range codec.Collections {
			ids, err := c.List(ctx, col)
			if err != nil {
				return err
			}
			fmt.Printf("%-24s %d\n", col, len(ids))
		}
		return nil
	})
}

// rcpKinds resolves --kind: one collection, or all four when empty.
func rcpKinds(kind string) ([]codec.Collection, error) {
	if kind == "" {
		return codec.Collections, nil
	}
	col, err := codec.ParseCollection(kind)
	if err != nil {
		return nil, err
	}
	return []codec.Collection{col}, nil
}

func runRCPList(ctx context.Context, args []string) error {
	f := newRCPFlags("list")
	kind := f.fs.String("kind", "", "collection (default: all four)")
	c, p, err := f.client(args)
	if err != nil {
		return err
	}
	cols, err := rcpKinds(*kind)
	if err != nil {
		return err
	}
	return f.session(ctx, c, p, func() error {
		for _, col := range cols {
			ids, err := c.List(ctx, col)
			if err != nil {
				return err
			}
			fmt.Printf("%s (%d)\n", col, len(ids))
			for _, id := range ids {
				io, err := c.Get(ctx, col, id)
				if err != nil {
					return err
				}
				fmt.Println("  " + rcpLine(col, io))
			}
		}
		return nil
	})
}

// rcpLine is one IO on one line.
func rcpLine(col codec.Collection, io codec.IO) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-6d %-20q", io.ID, io.Mnemonic.Original)
	if io.Virtual {
		b.WriteString(" virtual")
	}
	if io.FederationUID != 0 {
		fmt.Fprintf(&b, " federation=%d", io.FederationUID)
	}
	if io.TieLineInhibit {
		b.WriteString(" tie-line-inhibit")
	}
	if col.IsDestination() && io.TieLineGroup != 0 {
		fmt.Fprintf(&b, " tie-line-group=%d", io.TieLineGroup)
	}
	names := make([]string, 0, len(io.Levels))
	for name := range io.Levels {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return io.Levels[names[i]].ID < io.Levels[names[j]].ID })
	for _, name := range names {
		l := io.Levels[name]
		fmt.Fprintf(&b, " | L%d", l.ID)
		if d := l.Device; d != nil {
			switch {
			case d.SenderReceiver != "":
				fmt.Fprintf(&b, " %q", d.SenderReceiver)
			case d.Name != "":
				fmt.Fprintf(&b, " %q/%d io %d", d.Name, d.DeviceLevel, d.IO)
			default:
				fmt.Fprintf(&b, " io %d", d.IO)
			}
		}
		if len(l.Tags) > 0 {
			fmt.Fprintf(&b, " tags=%s", strings.Join(l.Tags, ","))
		}
	}
	return b.String()
}

func runRCPGet(ctx context.Context, args []string) error {
	f := newRCPFlags("get")
	kind := f.fs.String("kind", "", "collection")
	id := f.fs.String("id", "", "id of the IO")
	asJSON := f.fs.Bool("json", false, "print the IO as JSON")
	c, p, err := f.client(args)
	if err != nil {
		return err
	}
	col, err := codec.ParseCollection(*kind)
	if err != nil {
		return err
	}
	return f.session(ctx, c, p, func() error {
		io, err := c.Get(ctx, col, *id)
		if err != nil {
			return err
		}
		if *asJSON {
			return printJSON(io)
		}
		fmt.Println(rcpLine(col, io))
		return nil
	})
}

func printJSON(v any) error {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

func runRCPExport(ctx context.Context, args []string) error {
	f := newRCPFlags("export")
	outDir := f.fs.String("out-dir", "", "write one <collection>.json per collection here (default: one document on stdout)")
	c, p, err := f.client(args)
	if err != nil {
		return err
	}
	all := map[string][]codec.IO{}
	err = f.session(ctx, c, p, func() error {
		for _, col := range codec.Collections {
			ids, err := c.List(ctx, col)
			if err != nil {
				return err
			}
			ios := make([]codec.IO, 0, len(ids))
			for _, id := range ids {
				io, err := c.Get(ctx, col, id)
				if err != nil {
					return err
				}
				ios = append(ios, io)
			}
			all[string(col)] = ios
		}
		return nil
	})
	if err != nil {
		return err
	}
	if *outDir == "" {
		return printJSON(all)
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	for _, col := range codec.Collections {
		raw, err := json.MarshalIndent(all[string(col)], "", "  ")
		if err != nil {
			return err
		}
		path := filepath.Join(*outDir, string(col)+".json")
		if err := writeFileAtomic(path, append(raw, '\n')); err != nil {
			return err
		}
		fmt.Printf("%-24s %d  -> %s\n", col, len(all[string(col)]), path)
	}
	return nil
}

// rcpBody builds the body of a create or a set from --body and the body
// flags. Only flags the operator actually gave reach the body: "false"
// and "0" are values here, not absences.
func rcpBody(fs *flag.FlagSet, bodyFile, mnemonic string, virtual, tieInhibit bool, tieGroup int, fedUID int64, count int) (codec.Update, error) {
	var u codec.Update
	if bodyFile != "" {
		raw, err := os.ReadFile(bodyFile)
		if err != nil {
			return u, err
		}
		d := json.NewDecoder(strings.NewReader(string(raw)))
		d.DisallowUnknownFields()
		if err := d.Decode(&u); err != nil {
			return u, fmt.Errorf("rcp: %s: %w", bodyFile, err)
		}
	}
	fs.Visit(func(fl *flag.Flag) {
		switch fl.Name {
		case "mnemonic":
			u.Mnemonic = &mnemonic
		case "virtual":
			u.Virtual = &virtual
		case "tie-line-inhibit":
			u.TieLineInhibit = &tieInhibit
		case "tie-line-group":
			u.TieLineGroup = &tieGroup
		case "federation-uid":
			u.FederationUID = &fedUID
		case "count":
			u.Count = &count
		}
	})
	return u, nil
}

func runRCPWrite(ctx context.Context, verb string, args []string) error {
	f := newRCPFlags(verb)
	kind := f.fs.String("kind", "", "collection")
	id := f.fs.String("id", "", "id of the IO (set)")
	count := f.fs.Int("count", 1, "how many IOs to create")
	bodyFile := f.fs.String("body", "", "the body as a JSON file")
	mnemonic := f.fs.String("mnemonic", "", "original mnemonic")
	virtual := f.fs.Bool("virtual", false, "virtual rather than backed by a device IO")
	tieInhibit := f.fs.Bool("tie-line-inhibit", false, "inhibit from tie lines")
	tieGroup := f.fs.Int("tie-line-group", 0, "tie line group (destinations)")
	fedUID := f.fs.Int64("federation-uid", 0, "link to this federation IO (0 unlinks)")
	c, p, err := f.client(args)
	if err != nil {
		return err
	}
	col, err := codec.ParseCollection(*kind)
	if err != nil {
		return err
	}
	u, err := rcpBody(f.fs, *bodyFile, *mnemonic, *virtual, *tieInhibit, *tieGroup, *fedUID, *count)
	if err != nil {
		return err
	}
	create := verb == "create"
	if create && u.Count == nil {
		u.Count = count
	}
	if err := u.Validate(col, create); err != nil {
		return err
	}
	if !create && *id == "" {
		return errors.New("consumer rcp set: --id is required")
	}

	return f.session(ctx, c, p, func() error {
		if !create {
			if err := c.Update(ctx, col, *id, u); err != nil {
				return err
			}
			fmt.Printf("accepted: %s %s updated\n", col, *id)
			return nil
		}
		before, err := c.List(ctx, col)
		if err != nil {
			return err
		}
		if err := c.Create(ctx, col, u); err != nil {
			return err
		}
		// The create answers 202 and no ids; they show up in the list
		// once the server has applied it.
		created, err := rcpAppeared(ctx, c, col, before, *u.Count)
		if err != nil {
			return err
		}
		fmt.Printf("accepted: %d created in %s: %s\n", len(created), col, strings.Join(created, " "))
		return nil
	})
}

// rcpAppeared polls the collection until want new ids are listed, and
// returns them. It fails rather than report a creation nobody saw.
func rcpAppeared(ctx context.Context, c *rcpc.Client, col codec.Collection, before []string, want int) ([]string, error) {
	had := make(map[string]bool, len(before))
	for _, id := range before {
		had[id] = true
	}
	var created []string
	for attempt := 0; attempt < 20; attempt++ {
		after, err := c.List(ctx, col)
		if err != nil {
			return nil, err
		}
		created = created[:0]
		for _, id := range after {
			if !had[id] {
				created = append(created, id)
			}
		}
		if len(created) >= want {
			return created, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("rcp: the server accepted the create but %d of %d new id(s) appeared in %s", len(created), want, col)
}

func runRCPDelete(ctx context.Context, args []string) error {
	f := newRCPFlags("delete")
	kind := f.fs.String("kind", "", "collection")
	id := f.fs.String("id", "", "id of the IO")
	c, p, err := f.client(args)
	if err != nil {
		return err
	}
	col, err := codec.ParseCollection(*kind)
	if err != nil {
		return err
	}
	if *id == "" {
		return errors.New("consumer rcp delete: --id is required")
	}
	return f.session(ctx, c, p, func() error {
		if err := c.Delete(ctx, col, *id); err != nil {
			return err
		}
		fmt.Printf("accepted: %s %s deleted\n", col, *id)
		return nil
	})
}
