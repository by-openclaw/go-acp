package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"strconv"
	"strings"

	"dhs/internal/consumer"
)

// ensureValErr returns a client-side validation error — mapped to exit 2 by
// type in cmd/dhs (docs/protocols/error-codes.md).
func ensureValErr(reason string) *consumer.ValidationError {
	return &consumer.ValidationError{Field: "ensure", Reason: reason}
}

// runEnsure converges one object to a desired value, idempotently (ADR-0007,
// the Ansible primitive). It reads the current value, compares to --value, and
// writes only if they differ — so a second run reports changed:false and sends
// no write. `--check` is a dry-run (reports would_change, writes nothing).
//
// Per ADR-0007 the exit code reflects outcome, not change: 0 ok / 1 runtime /
// 2 validation (per docs/protocols/error-codes.md). Change is signalled by the
// `changed` / `would_change` field. ADR-0007's consumer `present` is realised
// here as value convergence; `absent` (session/service teardown) is a
// producer/registry concern and is rejected for the consumer value-ensure.
func runEnsure(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("ensure", flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", 0, "slot number (default 0)")
	group := fs.String("group", "", "object group name")
	label := fs.String("label", "", "object label")
	id := fs.Int("id", -1, "object id within group")
	pathFlag := fs.String("path", "", "tree path: dotted label OR numeric OID")
	valueStr := fs.String("value", "", "desired value to converge to")
	state := fs.String("state", "present", "present (converge --value) | absent (producer/registry only)")
	check := fs.Bool("check", false, "dry-run: report would_change, write nothing")
	asJSON := fs.Bool("json", false, "emit a JSON result (Ansible-friendly)")
	noWalk := fs.Bool("no-walk", false, "fail on cache miss instead of walking the slot to resolve --path/--label")
	dmIdentity := fs.String("dm", "", `Ember+ only: identity-keyed DM hot-load (e.g. "Tiny Ember+ Router@1.6.2").`)
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer <proto> ensure <host> --slot N (--path P | --label L | --id I) --value <v> [--state present] [--check] [--json]")
	}
	_ = fs.Parse(rest)

	if *state != "present" {
		return ensureValErr(fmt.Sprintf("--state %q: consumer ensure converges values with --state present only; absent (session/service teardown) is a producer/registry concern (ADR-0007)", *state))
	}
	valueSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "value" {
			valueSet = true
		}
	})
	if !valueSet {
		return ensureValErr("--value is required for --state present")
	}
	if *pathFlag == "" && *label == "" && *id < 0 {
		return ensureValErr("either --path, --label, or --id is required")
	}
	if *pathFlag != "" {
		if err := validatePathOrOID(*pathFlag); err != nil {
			return err
		}
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	// Resolve --path / --label to an object id (mirror cmd_set.go).
	resolvedFromCache := false
	if cf.protocol == "emberplus" {
		if err := ensureEmberplusTree(ctx, plug, host, cf.port, *dmIdentity, *slot, *noWalk); err != nil {
			return err
		}
		resolvedFromCache = true
	} else if *id < 0 {
		if *pathFlag != "" {
			if cachedID := resolvePathFromCache(host, cf.protocol, *slot, *pathFlag); cachedID >= 0 {
				*id = cachedID
				resolvedFromCache = true
			}
		}
		if !resolvedFromCache && *label != "" {
			if cachedID := resolveLabelFromCache(host, cf.protocol, *slot, *group, *label); cachedID >= 0 {
				*id = cachedID
				resolvedFromCache = true
			}
		}
	}
	if cf.protocol != "emberplus" && !resolvedFromCache && (*pathFlag != "" || *label != "") {
		if *noWalk {
			return ensureValErr(fmt.Sprintf("--no-walk: %q not found in cache for slot %d (run 'walk --slot %d' first or drop --no-walk)", orFirst(*pathFlag, *label), *slot, *slot))
		}
		if _, err := plug.Walk(ctx, *slot); err != nil {
			return fmt.Errorf("walk for resolution: %w", err)
		}
		if *id < 0 && *label != "" {
			if cachedID := resolveLabelFromCache(host, cf.protocol, *slot, *group, *label); cachedID >= 0 {
				*id = cachedID
			}
		}
	}

	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	req := consumer.ValueRequest{Slot: *slot, Path: *pathFlag, Group: *group, Label: *label, ID: *id}

	current, err := plug.GetValue(opCtx, req)
	if err != nil {
		return err
	}

	desired := strings.TrimSpace(*valueStr)

	// Coerce --value to the object's kind so numeric range / type checks see
	// a typed value (not a bare string), then pre-flight it through the
	// plugin's client-side validator. Bad input is a validation error
	// (exit 2 per error-codes.md) caught before any wire write — and before
	// --check reports, so a dry-run rejects bad values too.
	want, cerr := coerceDesired(current.Kind, desired)
	if cerr != nil {
		return cerr
	}
	if v, ok := plug.(consumer.ValueValidator); ok {
		if verr := v.ValidateValue(opCtx, req, want); verr != nil {
			return verr
		}
	}

	curStr := canonicalValueStr(current)
	changed := !valuesEqual(current, desired)

	if *check {
		return emitEnsure(*asJSON, ensureResult{WouldChange: &changed, Current: curStr, Target: desired})
	}
	if !changed {
		return emitEnsure(*asJSON, ensureResult{Changed: &changed, Previous: curStr, Current: curStr})
	}
	confirmed, err := plug.SetValue(opCtx, req, want)
	if err != nil {
		return err
	}
	newStr := canonicalValueStr(confirmed)
	return emitEnsure(*asJSON, ensureResult{Changed: &changed, Previous: curStr, Current: newStr})
}

// ensureResult is the structured outcome (ADR-0007 shape, simplified for the
// consumer value case). Pointers distinguish apply (changed) from check
// (would_change) and keep `false` from being omitted.
type ensureResult struct {
	Changed     *bool  `json:"changed,omitempty"`
	WouldChange *bool  `json:"would_change,omitempty"`
	Previous    string `json:"previous,omitempty"`
	Current     string `json:"current,omitempty"`
	Target      string `json:"target,omitempty"`
}

func emitEnsure(asJSON bool, r ensureResult) error {
	if asJSON {
		b, _ := json.Marshal(r)
		fmt.Println(string(b))
		return nil
	}
	switch {
	case r.WouldChange != nil:
		fmt.Printf("would_change=%t  current=%q  target=%q\n", *r.WouldChange, r.Current, r.Target)
	case r.Changed != nil && *r.Changed:
		fmt.Printf("changed=true  previous=%q  current=%q\n", r.Previous, r.Current)
	default:
		fmt.Printf("changed=false  current=%q (already converged)\n", r.Current)
	}
	return nil
}

// canonicalValueStr renders a Value to a comparable string per Kind — no unit
// suffix, no formatting flourish (unlike formatValue), so it round-trips
// against a user-supplied --value.
func canonicalValueStr(v consumer.Value) string {
	switch v.Kind {
	case consumer.KindEnum, consumer.KindString, consumer.KindAlarm:
		return v.Str
	case consumer.KindBool:
		return strconv.FormatBool(v.Bool)
	case consumer.KindInt:
		return strconv.FormatInt(v.Int, 10)
	case consumer.KindUint:
		return strconv.FormatUint(v.Uint, 10)
	case consumer.KindFloat:
		return strconv.FormatFloat(v.Float, 'g', -1, 64)
	case consumer.KindIPAddr:
		return net.IP(v.IPAddr[:]).String()
	default:
		return hex.EncodeToString(v.Raw)
	}
}

// valuesEqual reports whether the current value already equals the desired
// string, with numeric tolerance (so "3" == "3.0" for numeric kinds).
func valuesEqual(cur consumer.Value, desired string) bool {
	d := strings.TrimSpace(desired)
	if canonicalValueStr(cur) == d {
		return true
	}
	switch cur.Kind {
	case consumer.KindInt, consumer.KindUint, consumer.KindFloat:
		cf, e1 := strconv.ParseFloat(canonicalValueStr(cur), 64)
		df, e2 := strconv.ParseFloat(d, 64)
		return e1 == nil && e2 == nil && cf == df
	}
	return false
}

// coerceDesired converts the --value string into a typed Value of the object's
// kind so the client-side validator can range/type-check it. Str is always
// retained so SetValue's encoder still sees the original text. Unparseable
// input is a validation error (exit 2).
func coerceDesired(kind consumer.ValueKind, s string) (consumer.Value, error) {
	v := consumer.Value{Kind: kind, Str: s}
	switch kind {
	case consumer.KindInt:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return v, ensureValErr(fmt.Sprintf("invalid integer %q", s))
		}
		v.Int = n
	case consumer.KindUint:
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return v, ensureValErr(fmt.Sprintf("invalid unsigned integer %q", s))
		}
		v.Uint = n
	case consumer.KindFloat:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return v, ensureValErr(fmt.Sprintf("invalid number %q", s))
		}
		v.Float = f
	case consumer.KindBool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return v, ensureValErr(fmt.Sprintf("invalid boolean %q", s))
		}
		v.Bool = b
	}
	// enum / string / ipaddr / alarm: Str carries the value; the validator
	// checks it directly (enum membership, dotted-quad parse).
	return v, nil
}

func helpEnsure() {
	fmt.Println(`dhs consumer <proto> ensure <host> --slot N (--path P | --label L | --id I) --value <v> [--state present] [--check] [--json]

Idempotent value convergence (ADR-0007). Reads the object, sets it ONLY if it
differs from --value, and reports whether it changed. A second run is a no-op
(changed=false). Use --check for a dry-run (writes nothing). --json emits an
Ansible-friendly {changed|would_change, previous, current} result.

Exit codes (docs/protocols/error-codes.md): 0 ok · 1 runtime/wire · 2 validation.
Change is signalled by the field, never the exit code.

Examples:
  dhs consumer acp1 ensure 10.6.239.113 --slot 0 --group control --label Broadcasts --value On
  dhs consumer acp1 ensure 10.6.239.113 --slot 0 --group control --label Broadcasts --value On --check --json`)
}
