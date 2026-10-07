package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"dhs/internal/rrcs/codec"
	rrcs "dhs/internal/rrcs/consumer"
)

// rrcsReadOnlyMethod reports whether a method only reads: the names of
// the specification that start with Get or Is. Everything else changes
// something on the gateway or on the intercom and goes through the write
// guard.
func rrcsReadOnlyMethod(method string) bool {
	// LineStatus (§8.17) reads without saying so in its name.
	return strings.HasPrefix(method, "Get") || strings.HasPrefix(method, "Is") || method == "LineStatus"
}

// rrcsValueOfJSON reads one JSON document into an XML-RPC value, keeping
// the order of the members of an object: whole numbers are integers,
// other numbers doubles, objects structs, arrays arrays.
func rrcsValueOfJSON(text string) (codec.Value, error) {
	dec := json.NewDecoder(bytes.NewReader([]byte(text)))
	dec.UseNumber()
	v, err := rrcsDecodeJSON(dec)
	if err != nil {
		return codec.Value{}, err
	}
	if dec.More() {
		return codec.Value{}, fmt.Errorf("more than one JSON value in %q", text)
	}
	return v, nil
}

func rrcsDecodeJSON(dec *json.Decoder) (codec.Value, error) {
	tok, err := dec.Token()
	if err != nil {
		return codec.Value{}, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			var members []codec.Member
			for dec.More() {
				key, err := dec.Token()
				if err != nil {
					return codec.Value{}, err
				}
				name, _ := key.(string)
				v, err := rrcsDecodeJSON(dec)
				if err != nil {
					return codec.Value{}, err
				}
				members = append(members, codec.Member{Name: name, Value: v})
			}
			_, err := dec.Token()
			return codec.Struct(members...), err
		case '[':
			items := []codec.Value{}
			for dec.More() {
				v, err := rrcsDecodeJSON(dec)
				if err != nil {
					return codec.Value{}, err
				}
				items = append(items, v)
			}
			_, err := dec.Token()
			return codec.Array(items...), err
		}
		return codec.Value{}, fmt.Errorf("unexpected %v", t)
	case json.Number:
		if n, err := t.Int64(); err == nil {
			if n < math.MinInt32 || n > math.MaxInt32 {
				return codec.Value{}, fmt.Errorf("%s does not fit a 32-bit integer", t)
			}
			return codec.Int(int32(n)), nil
		}
		f, err := t.Float64()
		if err != nil {
			return codec.Value{}, err
		}
		return codec.Double(f), nil
	case bool:
		return codec.Bool(t), nil
	case string:
		return codec.String(t), nil
	case nil:
		return codec.Value{}, fmt.Errorf("null has no XML-RPC form")
	}
	return codec.Value{}, fmt.Errorf("unexpected %v", tok)
}

// rrcsCall sends any method of the specification with the parameters
// given, and prints the answer as received.
func rrcsCall(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rrcs call", flag.ContinueOnError)
	fs.Usage = func() {
		_, _ = fmt.Fprint(fs.Output(), `Usage: dhs consumer rrcs call <host>[:port] METHOD [--arg JSON ...] [flags]

Sends one request of the RRCS specification and prints the answer as
received. The transaction key is added in front. Each --arg is one
parameter, written as JSON, in the order of the specification:
  --arg 1  --arg true  --arg '"text"'  --arg '{"Node":66,"Port":1045}'  --arg '[1,2]'

On Windows PowerShell write the parameters in a file and use --args-file:
the shell removes the double quotes of JSON given on the command line.

A method whose name starts with Get or Is only reads. Any other method
changes something and is refused without --write-to HOST.

`)
		fs.PrintDefaults()
	}
	cf := newRRCSFlags(fs)
	var params rrcsProps
	fs.Var(&params, "arg", "one parameter as JSON; repeat in the order of the specification")
	argsFile := fs.String("args-file", "", "read the parameters from this file instead of --arg: one JSON array, each element one parameter. Spares the quoting of JSON on a command line (Windows PowerShell removes the double quotes)")
	noKey := fs.String("key", "yes", "yes = put the transaction key in front, as every method but GetAlive has it | no")
	writeTo := fs.String("write-to", "", rrcsWriteToHelp)
	if err := parseVerbFlags(fs, reorderFlagsFirst(args)); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return rrcsValErr("call", "want a host[:port] and a method name")
	}
	host, method := fs.Arg(0), fs.Arg(1)
	if *noKey != "yes" && *noKey != "no" {
		return rrcsValErr("call", "--key must be yes or no")
	}
	values := make([]codec.Value, 0, len(params))
	if *argsFile != "" {
		if len(params) > 0 {
			return rrcsValErr("call", "give --arg or --args-file, not both")
		}
		raw, err := os.ReadFile(*argsFile)
		if err != nil {
			return fmt.Errorf("rrcs call: %w", err)
		}
		list, err := rrcsValueOfJSON(strings.TrimPrefix(string(raw), "ï»¿"))
		if err != nil || list.Kind != codec.KindArray {
			return rrcsValErr("call", "--args-file "+*argsFile+": want one JSON array of parameters")
		}
		values = list.Items
	}
	for _, p := range params {
		v, err := rrcsValueOfJSON(p)
		if err != nil {
			return rrcsValErr("call", "--arg "+p+": "+err.Error())
		}
		values = append(values, v)
	}
	if !rrcsReadOnlyMethod(method) {
		if err := rrcsWriteGuard("call", host, *writeTo); err != nil {
			return err
		}
	}
	client, _, closeFn, err := cf.open("call", host)
	if err != nil {
		return err
	}
	defer closeFn()

	cf.say("rrcs call: %s %s", method, rrcsCompact(codec.Array(values...)))
	call := client.Call
	if *noKey == "no" {
		call = client.CallNoKey
	}
	reply, err := call(ctx, method, values...)
	if err != nil {
		cf.say("rrcs call: %v", err)
		if reply.Value.Kind == codec.KindArray || reply.Value.Kind == codec.KindStruct {
			fmt.Println(rrcsCompact(reply.Value))
		}
		return fmt.Errorf("rrcs call: %w", err)
	}
	if cf.output == "json" {
		fmt.Println(rrcsCompact(reply.Value))
		return nil
	}
	fmt.Println(rrcsCompact(reply.Payload()))
	return nil
}

// rrcsXpVerb sets or removes one crosspoint (§8.1 SetXp, KillXp) and
// reads its state back (GetXpStatus).
func rrcsXpVerb(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rrcs xp", flag.ContinueOnError)
	cf := newRRCSFlags(fs)
	src := fs.String("src", "", "source: the path of a port that has an input, as list sources prints it")
	dst := fs.String("dst", "", "destination: the path of a port that has an output, as list dests prints it")
	state := fs.String("state", "", "on = SetXp | off = KillXp | empty = only read the state (GetXpStatus)")
	writeTo := fs.String("write-to", "", rrcsWriteToHelp)
	level := fs.String("level", "no", "yes = also read the level of the crosspoint. GetXpVolume does not work on an Artist-1024 (§8.2; a real RRCS 9.0 answers \"Node address invalid\"), so the level is read the way watch gets it: a registration for this crosspoint, whose first notification is its current level, removed at once. It needs --listen free: not while a watch runs on this machine")
	listen := fs.String("listen", ":8195", "with --level yes: local [ip]:port RRCS sends the level to")
	if err := parseVerbFlags(fs, reorderFlagsFirst(args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return rrcsValErr("xp", "want exactly one host[:port] argument")
	}
	if *level != "yes" && *level != "no" {
		return rrcsValErr("xp", "--level must be yes or no")
	}
	if *state != "" && *state != "on" && *state != "off" {
		return rrcsValErr("xp", "--state must be on, off or empty")
	}
	sNode, sPort, _, ok1 := rrcsPortOfPath(*src)
	dNode, dPort, _, ok2 := rrcsPortOfPath(*dst)
	if !ok1 || !ok2 {
		return rrcsValErr("xp", "want --src and --dst as port paths (net.N.node.N.port.P)")
	}
	net := func(path string) int32 {
		var n int32 = 1
		_, _ = fmt.Sscanf(path, "net.%d.", &n)
		return n
	}
	if *state != "" {
		if err := rrcsWriteGuard("xp", fs.Arg(0), *writeTo); err != nil {
			return err
		}
	}
	client, _, closeFn, err := cf.open("xp", fs.Arg(0))
	if err != nil {
		return err
	}
	defer closeFn()
	address := []codec.Value{
		codec.Int(net(*src)), codec.Int(int32(sNode)), codec.Int(int32(sPort)),
		codec.Int(net(*dst)), codec.Int(int32(dNode)), codec.Int(int32(dPort)),
	}
	if *state != "" {
		method := map[string]string{"on": "SetXp", "off": "KillXp"}[*state]
		cf.say("rrcs xp: %s %s > %s", method, *src, *dst)
		if _, err := client.Call(ctx, method, address...); err != nil {
			cf.say("rrcs xp: refused: %v", err)
			return fmt.Errorf("rrcs xp: %w", err)
		}
	}
	reply, err := client.Call(ctx, "GetXpStatus", address...)
	now := "off"
	switch {
	case err != nil && *level == "yes" && *state == "":
		// The level does not depend on the state read: say why the
		// state is missing and go on.
		cf.say("rrcs xp: state not read: %v", err)
		now = ""
	case err != nil:
		return fmt.Errorf("rrcs xp: %w", err)
	default:
		if p := reply.Payload(); len(p.Items) > 0 && p.Items[0].Kind == codec.KindBool && p.Items[0].Bool {
			now = "on"
		}
	}
	if now != "" {
		fmt.Printf("xp.%s>%s State = %s\n", *src, *dst, now)
	}
	if *state != "" && now != *state {
		return fmt.Errorf("rrcs xp: RRCS accepted the request and the crosspoint reads %s", now)
	}
	if *level == "yes" {
		x := rrcs.Crosspoint{SrcNode: sNode, SrcPort: sPort, DstNode: dNode, DstPort: dPort}
		return rrcsXpLevel(ctx, cf, client, *listen, x, "xp."+*src+">"+*dst)
	}
	return nil
}

// rrcsXpLevel reads the level of one crosspoint through the volume
// registration (§8.15): RRCS sends the current level of a crosspoint as
// soon as it is added to the registry. Everything it registers is removed
// before it returns.
func rrcsXpLevel(ctx context.Context, cf *rrcsFlags, client *rrcs.Client, listen string, x rrcs.Crosspoint, name string) error {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("rrcs xp: listen %s: %w (is a watch running on this machine?)", listen, err)
	}
	got := make(chan codec.Value, 1)
	listener := &rrcs.Listener{AnyPath: true, OnEvent: func(e rrcs.Event) {
		if e.Method != rrcs.MethodXpVolumeChange || len(e.Params) == 0 {
			return
		}
		list := e.Params[len(e.Params)-1]
		for _, ch := range list.Items {
			s, _ := ch.Field("Source")
			d, _ := ch.Field("Destination")
			if rrcsMemberInt(s, "Node") == x.SrcNode && rrcsMemberInt(s, "Port") == x.SrcPort &&
				rrcsMemberInt(d, "Node") == x.DstNode && rrcsMemberInt(d, "Port") == x.DstPort {
				select {
				case got <- ch:
				default:
				}
			}
		}
	}}
	srv := &http.Server{Handler: listener, ReadHeaderTimeout: rrcsListenerWait}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	reg := rrcs.Registration{Port: ln.Addr().(*net.TCPAddr).Port}
	ip := rrcsLocalIP(listen, client.Peer())
	if _, err := client.Register(ctx, reg); err != nil {
		return fmt.Errorf("rrcs xp: level: %w", err)
	}
	// The goodbye runs whatever happens next, on a context of its own.
	defer func() {
		bye, cancel := context.WithTimeout(context.Background(), cf.timeout)
		defer cancel()
		if _, err := client.UnregisterVolumeEvents(bye, ip); err != nil {
			cf.say("rrcs xp: level: unregister: %v", err)
		}
		if _, err := client.Unregister(bye, reg); err != nil {
			cf.say("rrcs xp: level: unregister: %v", err)
		}
	}()
	if _, err := client.RegisterVolumeEvents(ctx, ip, reg.Port); err != nil {
		return fmt.Errorf("rrcs xp: level: %w", err)
	}
	if _, err := client.FollowVolumes(ctx, ip, reg.Port, x); err != nil {
		return fmt.Errorf("rrcs xp: level: %w", err)
	}
	select {
	case ch := <-got:
		meta := rrcsMetaOf["Xp.Volume"]
		for _, member := range []string{"SingleVolume", "ConferenceVolume"} {
			v, ok := ch.Field(member)
			if !ok {
				continue
			}
			value, unit := rrcsVolume(int(v.Int))
			if unit != "" {
				value += " " + unit
			}
			fmt.Printf("%s %s = %s [%s..%s]  (raw %d)\n", name, member, value, meta.Min, meta.Max, v.Int)
		}
		return nil
	case <-time.After(cf.timeout):
		return fmt.Errorf("rrcs xp: level: RRCS accepted the registration and sent no level within %s", cf.timeout)
	case <-ctx.Done():
		return ctx.Err()
	}
}
