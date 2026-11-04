package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"dhs/internal/consumer"
)

// siteLimitsHelp is the help of --site-limits on every writing verb.
const siteLimitsHelp = "the plant's limits, narrower than the factory's (ADR-0036, ADR-0037 P7): a CSV with the header path,min,max,allowed. Default: snapshots/<proto>/<host>/site-limits.csv when that file exists; a path names another file; \"off\" applies none"

// siteLimitsFor loads the site limits a write is checked against: the
// file named by the flag, the default home when it exists, none with
// "off". A file that cannot be read or that holds a bad rule refuses the
// run: a plant rule that is not applied is worse than a stop.
func siteLimitsFor(proto, host, flag string) (*consumer.SiteLimits, error) {
	switch flag {
	case "off":
		return nil, nil
	case "":
		path := filepath.Join(snapshotDir(proto, deviceKeyFor(proto, host)), "site-limits.csv")
		if _, err := os.Stat(path); err != nil {
			return nil, nil
		}
		flag = path
	}
	s, err := consumer.LoadSiteLimits(flag)
	if err != nil {
		return nil, &consumer.ValidationError{Field: "site-limits", Reason: err.Error()}
	}
	fmt.Fprintf(os.Stderr, "site limits: %d rules from %s\n", s.Len(), flag)
	return s, nil
}

// factoryMetaFor is the schema of a point for a site-limit check: the
// walked meta when there is one, else what the plugin describes.
func factoryMetaFor(ctx context.Context, plug consumer.Protocol, req consumer.ValueRequest, meta *consumer.Object) *consumer.Object {
	if meta != nil {
		return meta
	}
	if d, ok := plug.(consumer.Describer); ok {
		if o, _, err := d.Describe(ctx, req); err == nil {
			return &o
		}
	}
	return nil
}

// valueText is a value as the site limits read it: a number in decimal,
// an enum by its index, a bool as true or false, text as it is.
func valueText(v consumer.Value) string {
	switch v.Kind {
	case consumer.KindInt:
		return strconv.FormatInt(v.Int, 10)
	case consumer.KindUint:
		return strconv.FormatUint(v.Uint, 10)
	case consumer.KindFloat:
		return strconv.FormatFloat(v.Float, 'f', -1, 64)
	case consumer.KindBool:
		return strconv.FormatBool(v.Bool)
	case consumer.KindEnum:
		return strconv.Itoa(int(v.Enum))
	}
	return v.Str
}

// pointName is the path of a point for the site limits: the --path when
// given, else the label (a rule may name a label as a suffix ".Label").
func pointName(req consumer.ValueRequest) string {
	if req.Path != "" {
		return req.Path
	}
	if req.Label != "" {
		return "." + req.Label
	}
	return ""
}
