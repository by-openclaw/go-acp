package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"

	acp1provider "dhs/internal/acp1/provider"
)

// runACP1Admin handles `dhs producer acp1 admin <verb> [flags]`. The
// concrete verb dispatches to the right helper which assembles the
// AdminRequest map and calls into the running provider's admin RPC.
//
// Discovery: the CLI reads $TMPDIR/dhs-acp1-<name>.json (default name
// "dhs-acp1") to find the admin TCP loopback address.
func runACP1Admin(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printACP1AdminHelp()
		return nil
	}
	verb := args[0]
	rest := args[1:]

	switch verb {
	case "ping":
		return acp1AdminPing(ctx, rest)
	case "slot":
		return acp1AdminSlot(ctx, rest)
	case "value":
		return acp1AdminValue(ctx, rest)
	case "reload":
		return acp1AdminReload(ctx, rest)
	case "help", "-h", "--help":
		printACP1AdminHelp()
		return nil
	}
	return fmt.Errorf("dhs producer acp1 admin: unknown verb %q", verb)
}

func printACP1AdminHelp() {
	fmt.Println(`usage: dhs producer acp1 admin <verb> [flags]

verbs:
  ping                            liveness probe; returns slot count
  slot load   --slot N --card P   load a DM-library schema into slot N (#260)
  slot unload --slot N            unload slot N (#260)
  slot state  --slot N --state S  jump slot N to state (no_card/powerup/boot/present/error/removed)
  value set   --path P --value V  set object value (path = "1.<slot+1>.<group>.<id>")
  value get   --slot N --group G --id I
                                  read object value
  reload                          reload tree from disk (#259)

common flags:
  --name NAME    instance name to address (default "dhs-acp1")`)
}

func adminClient(name string) (*acp1provider.AdminClient, error) {
	if name == "" {
		name = "dhs-acp1"
	}
	return acp1provider.NewAdminClient(name)
}

func adminCall(ctx context.Context, name, verb string, args map[string]any) (*acp1provider.AdminResponse, error) {
	c, err := adminClient(name)
	if err != nil {
		return nil, fmt.Errorf("admin: locate provider %q: %w", name, err)
	}
	return c.Call(ctx, &acp1provider.AdminRequest{Verb: verb, Args: args})
}

func printAdminResp(resp *acp1provider.AdminResponse) error {
	if resp.Status != "ok" {
		return fmt.Errorf("admin: %s", resp.Message)
	}
	if len(resp.Result) == 0 {
		fmt.Println("ok")
		return nil
	}
	out, err := json.MarshalIndent(resp.Result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

func acp1AdminPing(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("admin ping", flag.ExitOnError)
	name := fs.String("name", "dhs-acp1", "provider instance name")
	_ = parseVerbFlags(fs, args)
	resp, err := adminCall(ctx, *name, "ping", nil)
	if err != nil {
		return err
	}
	return printAdminResp(resp)
}

func acp1AdminSlot(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("dhs producer acp1 admin slot: missing sub-verb (load / unload / state)")
	}
	sub := args[0]
	rest := args[1:]
	fs := flag.NewFlagSet("admin slot "+sub, flag.ExitOnError)
	name := fs.String("name", "dhs-acp1", "provider instance name")
	slot := fs.Int("slot", -1, "slot number")
	switch sub {
	case "load":
		card := fs.String("card", "", "DM-library card path: vendor/product/model-rev/proto")
		_ = parseVerbFlags(fs, rest)
		if *slot < 0 || *card == "" {
			return fmt.Errorf("admin slot load: --slot and --card are required")
		}
		resp, err := adminCall(ctx, *name, "slot.load", map[string]any{
			"slot": *slot,
			"card": *card,
		})
		if err != nil {
			return err
		}
		return printAdminResp(resp)
	case "unload":
		_ = parseVerbFlags(fs, rest)
		if *slot < 0 {
			return fmt.Errorf("admin slot unload: --slot is required")
		}
		resp, err := adminCall(ctx, *name, "slot.unload", map[string]any{"slot": *slot})
		if err != nil {
			return err
		}
		return printAdminResp(resp)
	case "state":
		state := fs.String("state", "", "target state: no_card / powerup / boot / present / error / removed")
		_ = parseVerbFlags(fs, rest)
		if *slot < 0 || *state == "" {
			return fmt.Errorf("admin slot state: --slot and --state are required")
		}
		resp, err := adminCall(ctx, *name, "slot.state", map[string]any{
			"slot":  *slot,
			"state": *state,
		})
		if err != nil {
			return err
		}
		return printAdminResp(resp)
	}
	return fmt.Errorf("admin slot: unknown sub-verb %q (load / unload / state)", sub)
}

func acp1AdminValue(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("dhs producer acp1 admin value: missing sub-verb (set / get)")
	}
	sub := args[0]
	rest := args[1:]
	fs := flag.NewFlagSet("admin value "+sub, flag.ExitOnError)
	name := fs.String("name", "dhs-acp1", "provider instance name")
	switch sub {
	case "set":
		path := fs.String("path", "", `object path "1.<slot+1>.<group>.<id>"`)
		valStr := fs.String("value", "", "new value (parsed as int if numeric, else string)")
		_ = parseVerbFlags(fs, rest)
		if *path == "" || *valStr == "" {
			return fmt.Errorf("admin value set: --path and --value are required")
		}
		var v any
		if n, err := strconv.ParseInt(*valStr, 10, 64); err == nil {
			v = n
		} else {
			v = *valStr
		}
		resp, err := adminCall(ctx, *name, "value.set", map[string]any{
			"path":  *path,
			"value": v,
		})
		if err != nil {
			return err
		}
		return printAdminResp(resp)
	case "get":
		slot := fs.Int("slot", -1, "slot number")
		group := fs.Int("group", -1, "object group (0..6)")
		id := fs.Int("id", -1, "object id (0..255)")
		_ = parseVerbFlags(fs, rest)
		if *slot < 0 || *group < 0 || *id < 0 {
			return fmt.Errorf("admin value get: --slot --group --id all required")
		}
		resp, err := adminCall(ctx, *name, "value.get", map[string]any{
			"slot":  *slot,
			"group": *group,
			"id":    *id,
		})
		if err != nil {
			return err
		}
		return printAdminResp(resp)
	}
	return fmt.Errorf("admin value: unknown sub-verb %q (set / get)", sub)
}

func acp1AdminReload(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("admin reload", flag.ExitOnError)
	name := fs.String("name", "dhs-acp1", "provider instance name")
	_ = parseVerbFlags(fs, args)
	resp, err := adminCall(ctx, *name, "reload", nil)
	if err != nil {
		return err
	}
	return printAdminResp(resp)
}

// Silence unused-import warnings if the test build is partial.
var _ = os.Getenv
