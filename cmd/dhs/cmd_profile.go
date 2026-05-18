// dhs consumer <proto> profile — run a walk against an Ember+ /
// ACP1 / ACP2 provider and print the compliance classification +
// every tolerance event that fired. R22 #487 extends with:
//
//   --format text|json    machine-readable output
//   --since <duration>    time-window filter on event last-seen
//   --show-events         per-occurrence detail (ring buffer)
//   --by-session          group by remote peer (requires R24 admin
//                         endpoint; errors with
//                         plugin:by-session-unavailable until R24)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"dhs/internal/consumer"
	acp1 "dhs/internal/acp1/consumer"
	acp2 "dhs/internal/acp2/consumer"
	"dhs/internal/consumer/compliance"
	emberplus "dhs/internal/emberplus/consumer"
	"dhs/internal/errcode"
)

// R22 #487 validation codes raised by the profile verb.
var (
	errProfileInvalidFormat   = errcode.New(errcode.LayerValidation, "invalid-format", errcode.ClassUsage)
	errProfileInvalidDuration = errcode.New(errcode.LayerValidation, "invalid-duration", errcode.ClassUsage)
	errBySessionUnavailable   = errcode.New(errcode.LayerPlugin, "by-session-unavailable", errcode.ClassUsage)
)

// pluginProfile returns the compliance profile attached to the given
// plugin, or nil if the plugin does not expose one.
func pluginProfile(plug consumer.Protocol) *compliance.Profile {
	switch p := plug.(type) {
	case *emberplus.Plugin:
		return p.ComplianceProfile()
	case *acp1.Plugin:
		return p.ComplianceProfile()
	case *acp2.Plugin:
		return p.ComplianceProfile()
	}
	return nil
}

// profileReport is the machine-readable shape rendered by --format json
// (R22 #487 spec). The text mode renders the same fields in the
// legacy column form.
type profileReport struct {
	Host           string                  `json:"host"`
	ObjectsWalked  int                     `json:"objects_walked"`
	Classification string                  `json:"classification"`
	Window         string                  `json:"window,omitempty"`
	Events         []compliance.EventCount `json:"events"`
	Observations   []observationJSON       `json:"observations,omitempty"`
}

// observationJSON is the rendered form of a compliance.ObservedEvent.
// We flatten slog.Attr to plain k/v for JSON consumers.
type observationJSON struct {
	Kind  string         `json:"kind"`
	At    time.Time      `json:"at"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

func runProfile(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("profile", flag.ContinueOnError)
	cf := addCommonFlags(fs)
	var (
		format     = fs.String("format", "text", "output format: text (legacy column form) | json")
		sinceFlag  = fs.String("since", "", "filter events whose last_seen is older than this duration (e.g. 5m, 1h). Empty = no filter.")
		showEvents = fs.Bool("show-events", false, "print every recorded compliance.Event with detail (matrix path, target, source) — not just kind counts")
		bySession  = fs.Bool("by-session", false, "group events by remote peer — requires the producer's R24 admin endpoint")
	)
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer <proto> profile <host> [--port N] [--timeout DUR] [--format text|json] [--since 5m] [--show-events] [--by-session]")
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}

	switch *format {
	case "text", "json":
	default:
		return fmt.Errorf("%w: --format=%q must be text|json", errProfileInvalidFormat, *format)
	}

	var since time.Duration
	if *sinceFlag != "" {
		var perr error
		since, perr = time.ParseDuration(*sinceFlag)
		if perr != nil {
			return fmt.Errorf("%w: --since=%q: %v", errProfileInvalidDuration, *sinceFlag, perr)
		}
	}

	if *bySession {
		return fmt.Errorf("%w: --by-session needs the producer R24 admin endpoint (see issue #489); not yet reachable from this client", errBySessionUnavailable)
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	objs, err := plug.Walk(ctx, 0)
	if err != nil {
		return fmt.Errorf("walk: %w", err)
	}

	profile := pluginProfile(plug)
	if profile == nil {
		return fmt.Errorf("profile command not supported for protocol %q", cf.protocol)
	}

	report := profileReport{
		Host:           fmt.Sprintf("%s:%d", host, cf.port),
		ObjectsWalked:  len(objs),
		Classification: profile.Classification(),
		Events:         profile.SnapshotEventsSince(since),
	}
	if since > 0 {
		report.Window = since.String()
	}
	if *showEvents {
		obs := profile.Observations(since)
		report.Observations = make([]observationJSON, 0, len(obs))
		for _, o := range obs {
			attrs := make(map[string]any, len(o.Attrs))
			for _, a := range o.Attrs {
				attrs[a.Key] = a.Value.Any()
			}
			report.Observations = append(report.Observations, observationJSON{
				Kind: o.Kind, At: o.At, Attrs: attrs,
			})
		}
	}

	switch *format {
	case "json":
		return renderProfileJSON(report)
	default:
		return renderProfileText(report, *showEvents)
	}
}

// renderProfileJSON emits the machine-readable report; matches the
// schema documented in .audit/issue-R22.md.
func renderProfileJSON(r profileReport) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// renderProfileText keeps the legacy column form when no R22 flag
// alters the shape; otherwise extends with per-event-kind first /
// last timestamps and (when --show-events) per-occurrence detail.
func renderProfileText(r profileReport, showEvents bool) error {
	fmt.Printf("host             %s\n", r.Host)
	fmt.Printf("objects walked   %d\n", r.ObjectsWalked)
	fmt.Printf("classification   %s\n", r.Classification)
	if r.Window != "" {
		fmt.Printf("window           %s\n", r.Window)
	}
	if len(r.Events) == 0 {
		fmt.Println("\nno tolerance events observed — provider is fully spec-compliant")
		return nil
	}
	fmt.Println("\ntolerance events")
	// Compute the column width from the longest kind so the count
	// column stays right-aligned for any event set.
	maxKind := 0
	for _, e := range r.Events {
		if n := len(e.Kind); n > maxKind {
			maxKind = n
		}
	}
	if maxKind < 32 {
		maxKind = 32
	}
	for _, e := range r.Events {
		first := ""
		last := ""
		if !e.FirstSeen.IsZero() {
			first = e.FirstSeen.Format(time.RFC3339)
		}
		if !e.LastSeen.IsZero() {
			last = e.LastSeen.Format(time.RFC3339)
		}
		fmt.Printf("  %-*s %d", maxKind, e.Kind, e.Count)
		if first != "" || last != "" {
			fmt.Printf("  first=%s last=%s", first, last)
		}
		fmt.Println()
	}
	if showEvents {
		fmt.Println("\nrecorded observations (ring buffer, oldest first)")
		// Group observations by kind for legibility per R22 spec sample.
		byKind := map[string][]observationJSON{}
		kinds := []string{}
		for _, o := range r.Observations {
			if _, ok := byKind[o.Kind]; !ok {
				kinds = append(kinds, o.Kind)
			}
			byKind[o.Kind] = append(byKind[o.Kind], o)
		}
		sort.Strings(kinds)
		for _, k := range kinds {
			fmt.Printf("event %s count=%d\n", k, len(byKind[k]))
			for _, o := range byKind[k] {
				fmt.Printf("  %s  %s\n", o.At.Format(time.RFC3339Nano), formatAttrs(o.Attrs))
			}
		}
	}
	return nil
}

// formatAttrs renders the per-observation attrs as space-separated
// `key=value` pairs in deterministic key order.
func formatAttrs(attrs map[string]any) string {
	if len(attrs) == 0 {
		return ""
	}
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%s=%v", k, attrs[k])
	}
	return b.String()
}

// helpProfile prints the long-form help text for the profile verb.
func helpProfile() {
	fmt.Println(`dhs consumer <proto> profile — Ember+/ACP1/ACP2 compliance audit

USAGE
  dhs consumer <proto> profile <host> [flags]

OUTPUT (text mode)
  host             127.0.0.1:9000
  objects walked   1355
  classification   strict | partial
  tolerance events (when classification=partial):
    <kind>         <count>  first=<ts> last=<ts>

FLAGS
  --format text|json    text (default) | json (machine-readable per
                        schema in .audit/issue-R22.md)
  --since DURATION      filter events whose last_seen is older than this
                        window (e.g. 5m, 1h)
  --show-events         print every recorded compliance.Event with detail
                        (matrix path, target, source) rather than just
                        kind counts
  --by-session          group by remote peer — requires the producer R24
                        admin endpoint (#489); errors
                        plugin:by-session-unavailable until R24 lands
  [global flags: --port, --timeout, --verbose, -v / -vv / -vvv / -vvvv]

BEHAVIOUR
  Runs a full walk on slot 0 then prints the classification and every
  tolerance event the consumer's decoder absorbed. Use --show-events
  for per-occurrence detail (matrix path, target, source) so the
  operator can grep specific deviations.

EXAMPLES
  dhs consumer emberplus profile 127.0.0.1 --port 9000
  dhs consumer emberplus profile 10.41.40.195 --port 9092 --format json
  dhs consumer emberplus profile 127.0.0.1 --since 5m --show-events

References:
  internal/emberplus/docs/consumer.md §A9 (compliance event catalogue)
  Ember+ Documentation v2.50 §A9
  .audit/issue-R22.md (R22 spec)`)

	// Avoid noisy lint: the error sentinels are unused in this binary
	// only if the caller forgets to inspect them. Reference here to keep
	// linkers happy across guarded build tags.
	_ = errors.New
}
