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
	fs.Usage = verbUsageFn(fs, helpEnsure) // #751 G5: -h = rich help + all flags
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", 0, "slot number (default 0)")
	group := fs.String("group", "", "object group name")
	label := fs.String("label", "", "object label")
	id := fs.Int("id", -1, "object id within group")
	pathFlag := fs.String("path", "", "tree path: dotted label OR numeric OID")
	valueStr := fs.String("value", "", "desired value to converge to")
	state := fs.String("state", "present", "present (converge --value) | absent (producer/registry only)")
	check := fs.Bool("check", false, "dry-run: report would_change, write nothing")
	asJSON := fs.Bool("json", false, "deprecated alias for --output json")
	output := fs.String("output", "text", "output format: text | json (ADR-0002; yaml reserved, not yet supported)")
	noWalk := fs.Bool("no-walk", false, "fail on cache miss instead of walking the slot to resolve --path/--label")
	dmIdentity := fs.String("dm", "", `Ember+ only: identity-keyed DM hot-load (e.g. "Tiny Ember+ Router@1.6.2").`)
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer <proto> ensure <host> --slot N (--path P | --label L | --id I) --value <v> [--state present] [--check] [--json]")
	}
	_ = fs.Parse(rest)

	jsonOut, oerr := resolveEnsureOutput(*output, *asJSON)
	if oerr != nil {
		return oerr
	}

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

	// Predict the value the device will actually store. ACP1 clamps an
	// out-of-range numeric setValue to [min,max] (spec p.28; emulator-verified:
	// NetwPrefix max=32 stores 32 for a requested 100) rather than rejecting
	// it. Comparing the raw --value would make `ensure --value 100` on a 0..32
	// object report changed on every run (100 != stored 32). Clamping to the
	// object's range client-side keeps the idempotency decision — and the
	// --check dry-run — consistent with what the device will do.
	target := desired
	if meta := findObjectMeta(plug, *slot, *group, *label, *id); meta != nil {
		target = predictStored(meta, current.Kind, desired)
	}

	curStr := canonicalValueStr(current)
	changed := !valuesEqual(current, target)

	if *check {
		return emitEnsure(jsonOut, ensureResult{WouldChange: &changed, Current: curStr, Target: target, Diff: ensureValueDiff(changed, curStr, target)})
	}
	if !changed {
		return emitEnsure(jsonOut, ensureResult{Changed: &changed, Previous: curStr, Current: curStr, Diff: ensureValueDiff(changed, curStr, curStr)})
	}
	confirmed, err := plug.SetValue(opCtx, req, want)
	if err != nil {
		return err
	}
	newStr := canonicalValueStr(confirmed)
	// Report the change actually observed (current -> confirmed), not the
	// pre-set prediction, so a write that the device clamps back onto the
	// current value still reports changed=false.
	applied := !valuesEqual(current, newStr)
	return emitEnsure(jsonOut, ensureResult{Changed: &applied, Previous: curStr, Current: newStr, Diff: ensureValueDiff(applied, curStr, newStr)})
}

// resolveEnsureOutput maps the canonical --output flag (ADR-0002) and the
// deprecated --json alias to a single json-vs-text decision. --output json (or
// the --json alias) selects JSON; text is the default. yaml is reserved by
// ADR-0002 but not yet implemented; anything else is a validation error (exit 2
// per error-codes.md).
func resolveEnsureOutput(output string, jsonAlias bool) (bool, error) {
	switch output {
	case "text":
		return jsonAlias, nil
	case "json":
		return true, nil
	case "yaml":
		return false, ensureValErr("--output yaml: not yet supported (use --output json)")
	default:
		return false, ensureValErr(fmt.Sprintf("--output %q: expected text | json", output))
	}
}

// ensureResult is the structured outcome (ADR-0007 shape, simplified for the
// consumer value case). Pointers distinguish apply (changed) from check
// (would_change) and keep `false` from being omitted.
type ensureResult struct {
	Changed     *bool        `json:"changed,omitempty"`
	WouldChange *bool        `json:"would_change,omitempty"`
	Previous    string       `json:"previous,omitempty"`
	Current     string       `json:"current,omitempty"`
	Target      string       `json:"target,omitempty"`
	Diff        []ensureDiff `json:"diff"`
}

// ensureDiff is one field-level change in the ADR-0007 diff[] array.
type ensureDiff struct {
	Field string `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// ensureValueDiff builds the ADR-0007 diff[] for the single-value ensure case:
// one {field:"value", from, to} entry when the value changes, an empty but
// non-nil slice otherwise. ADR-0007 §Forbidden requires diff to always be
// emitted (even []), so this never returns nil.
func ensureValueDiff(changed bool, from, to string) []ensureDiff {
	if !changed {
		return []ensureDiff{}
	}
	return []ensureDiff{{Field: "value", From: from, To: to}}
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

// findObjectMeta returns the cached walker Object for the resolved target so
// ensure can read its numeric range. Matches by (group, id) first — ensure has
// already resolved --path/--label to an id — then falls back to label. The
// plugin's Walk is cached per slot, so this is a lookup, not a device hit.
// Returns nil on miss (e.g. Ember+, where ensure doesn't pre-clamp).
func findObjectMeta(plug consumer.Protocol, slot int, group, label string, id int) *consumer.Object {
	objs, err := plug.Walk(context.Background(), slot)
	if err != nil {
		return nil
	}
	if id >= 0 {
		for i := range objs {
			if objs[i].ID == id && (group == "" || strings.EqualFold(objs[i].Group, group)) {
				return &objs[i]
			}
		}
	}
	if label != "" {
		for i := range objs {
			if objs[i].Label == label && (group == "" || strings.EqualFold(objs[i].Group, group)) {
				return &objs[i]
			}
		}
	}
	return nil
}

// predictStored returns the canonical string the device will store for a
// setValue of `desired` on meta — clamped to the object's [min,max]. ACP1
// clamps out-of-range numerics rather than rejecting them (spec p.28), so
// mirroring that here keeps `ensure` idempotent and `--check` accurate. Only
// numeric kinds are clamped; enum/string/ipaddr pass through unchanged (enums
// are validated by membership, strings are length-checked client-side, and the
// device echoes them verbatim).
func predictStored(meta *consumer.Object, kind consumer.ValueKind, desired string) string {
	s := strings.TrimSpace(desired)
	if meta == nil {
		return s
	}
	switch kind {
	case consumer.KindInt, consumer.KindUint:
		iv, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			f, ferr := strconv.ParseFloat(s, 64)
			if ferr != nil {
				return s
			}
			iv = int64(f)
		}
		if lo, ok := anyToFloat(meta.Min); ok && float64(iv) < lo {
			iv = int64(lo)
		}
		if hi, ok := anyToFloat(meta.Max); ok && float64(iv) > hi {
			iv = int64(hi)
		}
		return strconv.FormatInt(iv, 10)
	case consumer.KindFloat:
		fv, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return s
		}
		if lo, ok := anyToFloat(meta.Min); ok && fv < lo {
			fv = lo
		}
		if hi, ok := anyToFloat(meta.Max); ok && fv > hi {
			fv = hi
		}
		return strconv.FormatFloat(fv, 'g', -1, 64)
	}
	return s
}

// anyToFloat coerces a numeric `any` (consumer.Object.Min/Max are int64 /
// uint64 / float64 depending on the object's ACP1 wire type) to float64 for
// range comparison. Returns false for non-numeric or nil.
func anyToFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
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
