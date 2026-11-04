package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	rrcs "dhs/internal/rrcs/consumer"
)

// rrcsWalk reads everything the gateway offers for reading and writes it
// to one JSON document.
func rrcsWalk(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rrcs snapshot", flag.ContinueOnError)
	cf := newRRCSFlags(fs)
	out := fs.String("out", "auto", "snapshot FILE the walk is written to. Literal \"auto\" = snapshots/rrcs/<host>/snapshot-<utcstamp>.json (ADR-0028)")
	skip := fs.String("skip", "", "leave out parts, comma-separated: properties (one request per object) | commands (one request per port) | values (label, alias and gains: up to four requests per port) | singles (the reads that address one node, card, port, key or crosspoint)")
	online := fs.String("online", "yes", "yes = first learn which ports are on line, by a registration of a second or two that is removed at once, and do not ask the others for gain and level (RRCS refuses those and logs a warning each time) | no = ask every port")
	listen := fs.String("listen", ":8196", "with --online yes: local [ip]:port RRCS sends the port states to. Not the port of watch, so a walk can run beside it")
	if err := parseVerbFlags(fs, reorderFlagsFirst(args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return rrcsValErr("snapshot", "want exactly one host[:port] argument")
	}
	if *online != "yes" && *online != "no" {
		return rrcsValErr("snapshot", "--online must be yes or no")
	}
	skips := map[string]bool{}
	for _, part := range strings.Split(*skip, ",") {
		switch part = strings.TrimSpace(part); part {
		case "":
		case "both": // kept from the first release
			skips["properties"], skips["commands"] = true, true
		case "properties", "commands", "values", "singles":
			skips[part] = true
		default:
			return rrcsValErr("snapshot", "--skip takes properties, commands, values, singles, comma-separated")
		}
	}
	client, _, closeFn, err := cf.open("snapshot", fs.Arg(0))
	if err != nil {
		return err
	}
	defer closeFn()
	started := time.Now()
	if *out == "auto" {
		*out = filepath.Join(snapshotDir(rrcsProto, deviceKeyFor(rrcsProto, fs.Arg(0))),
			"snapshot-"+started.UTC().Format("20060102T1504Z")+".json")
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return fmt.Errorf("rrcs snapshot: %w", err)
	}

	// Which ports are on line, when something that depends on it is asked.
	var onLine map[[2]int]bool
	if *online == "yes" && (!skips["values"] || !skips["singles"]) {
		ports, err := rrcsOnlinePorts(ctx, cf, client, *listen)
		if err != nil {
			cf.say("rrcs snapshot: the ports on line could not be learnt, every port is asked: %v", err)
		} else {
			onLine = ports
			cf.say("rrcs snapshot: %d ports on line; the others are not asked for gain and level", len(ports))
		}
	}

	snap, portCount, err := rrcsCollect(ctx, client, rrcsCollectOpts{
		Online:     onLine,
		Full:       true,
		Properties: !skips["properties"],
		Commands:   !skips["commands"],
		Values:     !skips["values"],
		Singles:    !skips["singles"],
		Step:       func(format string, a ...any) { cf.say("rrcs snapshot: "+format, a...) },
	})
	if err != nil {
		return err
	}
	step := func(format string, a ...any) { cf.say("rrcs snapshot: "+format, a...) }

	doc, err := json.MarshalIndent(snap, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, doc, 0o644); err != nil {
		return fmt.Errorf("rrcs snapshot: %w", err)
	}
	step("%d requests, %d failed, %s — written to %s", snap.Requests, snap.Failed,
		time.Since(started).Round(time.Millisecond), *out)
	if !snap.Complete {
		return fmt.Errorf("rrcs snapshot: interrupted, the snapshot is partial")
	}
	if cf.output == "json" {
		fmt.Println(string(doc))
		return nil
	}
	fmt.Printf("%-20s %s\n", "target", snap.Target)
	for _, s := range snap.Status {
		if s.Error == "" {
			b, _ := json.Marshal(s.Payload)
			fmt.Printf("%-20s %s\n", s.Method, b)
		}
	}
	for _, typ := range rrcsObjectTypes {
		if objs, ok := snap.Objects[typ]; ok {
			fmt.Printf("%-20s %d\n", typ, len(objs))
		}
	}
	fmt.Printf("%-20s %d\n", "ports", portCount)
	fmt.Printf("%-20s %d\n", "command lists", len(snap.Commands))
	fmt.Printf("%-20s %d\n", "port values", len(snap.PortValues))
	fmt.Printf("%-20s %d\n", "key configurations", len(snap.KeyConfigs))
	fmt.Printf("%-20s %d\n", "port not online", snap.NotOnline)
	fmt.Printf("%-20s %d\n", "port without gain", snap.NoGain)
	fmt.Printf("%-20s %d\n", "single reads", len(snap.Singles))
	fmt.Printf("%-20s %d of %d\n", "failed requests", snap.Failed, snap.Requests)
	fmt.Printf("%-20s %s\n", "snapshot", *out)
	return nil
}

// rrcsOnlinePorts learns which ports are on line the only way RRCS tells
// it: a registration is answered with one PortActive per port on line
// (§8.15.1, §9.7). It registers, takes what comes until RRCS has been
// silent for half a second, and unregisters.
func rrcsOnlinePorts(ctx context.Context, cf *rrcsFlags, client *rrcs.Client, listen string) (map[[2]int]bool, error) {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w (is another dhs command listening there? --listen takes another port)", listen, err)
	}
	var mu sync.Mutex
	ports := map[[2]int]bool{}
	last := time.Now()
	listener := &rrcs.Listener{OnEvent: func(e rrcs.Event) {
		if e.Method != "PortActive" && e.Method != "PortInactive" {
			return
		}
		p := e.Params
		if e.TransKey != "" {
			p = p[1:]
		}
		if len(p) < 3 {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		ports[[2]int{rrcsParamInt(p, 1), rrcsParamInt(p, 2)}] = e.Method == "PortActive"
		last = time.Now()
	}}
	srv := &http.Server{Handler: listener, ReadHeaderTimeout: rrcsListenerWait}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	reg := rrcs.Registration{Port: ln.Addr().(*net.TCPAddr).Port}
	if _, err := client.Register(ctx, reg); err != nil {
		return nil, err
	}
	defer func() {
		bye, cancel := context.WithTimeout(context.Background(), cf.timeout)
		defer cancel()
		if _, err := client.Unregister(bye, reg); err != nil {
			cf.say("rrcs: ports on line: unregister: %v", err)
		}
	}()
	mu.Lock()
	last = time.Now()
	mu.Unlock()
	deadline := time.After(cf.timeout)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline:
		case <-tick.C:
			mu.Lock()
			quiet := time.Since(last) > 500*time.Millisecond
			mu.Unlock()
			if !quiet {
				continue
			}
		}
		break
	}
	mu.Lock()
	defer mu.Unlock()
	out := map[[2]int]bool{}
	for k, on := range ports {
		if on {
			out[k] = true
		}
	}
	return out, nil
}
