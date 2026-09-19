package monitor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"time"

	"dhs/internal/consumer"
)

// Duration wraps time.Duration so a poll profile can spell an interval
// as "1s", "30s", "5m" in JSON. The repo imports config as JSON only
// (stdlib, no YAML parser — see internal/export/yaml.go), so the
// on-disk profile is JSON even though ADR-0030 shows it as YAML for
// readability.
type Duration time.Duration

// MarshalJSON renders the duration as a Go duration string.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// UnmarshalJSON accepts either a duration string ("30s") or a bare
// number of nanoseconds.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch x := v.(type) {
	case string:
		p, err := time.ParseDuration(x)
		if err != nil {
			return fmt.Errorf("monitor: bad duration %q: %w", x, err)
		}
		*d = Duration(p)
	case float64:
		*d = Duration(time.Duration(x))
	default:
		return fmt.Errorf(`monitor: interval must be a string like "30s"`)
	}
	return nil
}

// Profile is a device's poll plan, keyed to a card model like the
// manifest (ADR-0022). Interval is a field on each entry, never encoded
// in a filename.
type Profile struct {
	Model    string   `json:"model"`
	Defaults Defaults `json:"defaults"`
	// Entries are the addresses to poll. JSON key "oids" matches the
	// operator's mental model; the addresses themselves are neutral.
	Entries []Entry `json:"oids"`
}

// Defaults apply to any entry that does not set its own interval or
// on_change.
type Defaults struct {
	Interval Duration `json:"interval"`
	OnChange bool     `json:"on_change"`
}

// Entry addresses one object to poll. OID is a convenience that feeds
// Path when neither Path nor Label is set, so an SNMP profile can list
// dotted OIDs directly while the type stays protocol-neutral.
type Entry struct {
	OID   string `json:"oid,omitempty"`
	Path  string `json:"path,omitempty"`
	Label string `json:"label,omitempty"`
	Slot  int    `json:"slot,omitempty"`
	Group string `json:"group,omitempty"`
	ID    int    `json:"id,omitempty"`

	Interval Duration `json:"interval,omitempty"`
	OnChange *bool    `json:"on_change,omitempty"`
}

// req builds the neutral request for this entry.
func (e Entry) req() consumer.ValueRequest {
	r := consumer.ValueRequest{Slot: e.Slot, Path: e.Path, Label: e.Label, Group: e.Group, ID: e.ID}
	if r.Path == "" && r.Label == "" && e.OID != "" {
		r.Path = e.OID
	}
	return r
}

// addrKey is the canonical identity of a request, used for
// change-detection keys, duplicate rejection and jitter seeding.
func addrKey(r consumer.ValueRequest) string {
	return fmt.Sprintf("s=%d|p=%s|l=%s|g=%s|id=%d|pid=%d|idx=%d",
		r.Slot, r.Path, r.Label, r.Group, r.ID, r.PID, r.Idx)
}

func (p *Profile) effInterval(e Entry) time.Duration {
	if e.Interval > 0 {
		return time.Duration(e.Interval)
	}
	return time.Duration(p.Defaults.Interval)
}

func (p *Profile) effOnChange(e Entry) bool {
	if e.OnChange != nil {
		return *e.OnChange
	}
	return p.Defaults.OnChange
}

// Load parses and validates a JSON poll profile.
func Load(data []byte) (*Profile, error) {
	var p Profile
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("monitor: decode profile: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// Validate enforces ADR-0030's load-time guards: every entry has an
// address and a positive effective interval, and no two entries address
// the same object (fail fast, no silent last-wins).
func (p *Profile) Validate() error {
	if len(p.Entries) == 0 {
		return fmt.Errorf("monitor: profile has no entries")
	}
	seen := make(map[string]int, len(p.Entries))
	for i, e := range p.Entries {
		r := e.req()
		if r.Path == "" && r.Label == "" && r.Group == "" {
			return fmt.Errorf("monitor: entry %d has no address (set oid, path, label or group)", i)
		}
		if p.effInterval(e) <= 0 {
			return fmt.Errorf("monitor: entry %d (%s) has no positive interval and no default", i, addrKey(r))
		}
		k := addrKey(r)
		if j, dup := seen[k]; dup {
			return fmt.Errorf("monitor: entries %d and %d address the same object %s", j, i, k)
		}
		seen[k] = i
	}
	return nil
}

// Intervals returns the distinct effective intervals in the profile,
// ascending. Reporting helper; the runtime schedules by next-due.
func (p *Profile) Intervals() []time.Duration {
	set := map[time.Duration]struct{}{}
	for _, e := range p.Entries {
		set[p.effInterval(e)] = struct{}{}
	}
	out := make([]time.Duration, 0, len(set))
	for iv := range set {
		out = append(out, iv)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// jitter returns a deterministic offset in [0, interval) derived from
// the address key, so same-cadence reads spread across the window
// instead of firing on one edge. Same seed every run.
func jitter(key string, interval time.Duration) time.Duration {
	if interval <= 0 {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return time.Duration(h.Sum64() % uint64(interval))
}
