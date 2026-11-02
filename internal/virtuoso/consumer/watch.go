package virtuoso

import (
	"context"
	"fmt"
	"strings"
	"time"

	dhsc "dhs/internal/consumer"
	"dhs/internal/consumer/monitor"
	"dhs/internal/virtuoso/codec"
)

// The guide documents no push channel, so a watch polls.

// Subscribe starts polling the request's slot, narrowed by --path.
func (p *Plugin) Subscribe(req dhsc.ValueRequest, fn dhsc.EventFunc) error {
	return p.poller.Subscribe(req, fn)
}

// Unsubscribe stops that polling.
func (p *Plugin) Unsubscribe(req dhsc.ValueRequest) error {
	return p.poller.Unsubscribe(req)
}

// SetPollInterval sets how often a watch re-reads each object (--interval).
func (p *Plugin) SetPollInterval(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.interval = d
}

// paced is the Plugin as the poller sees it: every poll read waits its
// gap, so a watch on thousands of objects stays a trickle on the device.
type paced struct{ *Plugin }

// GetValue reads one object after the pacing gap.
func (s paced) GetValue(ctx context.Context, req dhsc.ValueRequest) (dhsc.Value, error) {
	if err := s.Clock().Sleep(ctx, s.gap); err != nil {
		return dhsc.Value{}, err
	}
	return s.Plugin.GetValue(ctx, req)
}

// pollProfileFor builds the poll plan: every readable field of the slot,
// narrowed to the --path prefixes (comma-separated). Methods and fields
// the data does not hold are left out. The interval is stretched to what
// the gap can serve, so reads never queue up behind each other.
func (p *Plugin) pollProfileFor(ctx context.Context, req dhsc.ValueRequest) (*monitor.Profile, error) {
	slot := max(req.Slot, 0)
	tree, err := p.Walk(ctx, slot)
	if err != nil {
		return nil, err
	}
	var scopes [][]string
	for _, s := range strings.Split(req.Path, ",") {
		if path := codec.SplitPath(s); len(path) > 0 {
			scopes = append(scopes, path)
		}
	}
	onChange := true
	var picked []monitor.Entry
	for _, o := range tree {
		if !o.HasRead() || o.Meta["noValue"] == true || !under(o.Path, scopes) {
			continue
		}
		picked = append(picked, monitor.Entry{Path: codec.JoinPath(o.Path), Slot: slot, OnChange: &onChange})
	}
	if len(picked) == 0 {
		return nil, fmt.Errorf("%w: virtuoso: nothing to watch on slot %d under %q", dhsc.ErrObjectNotFound, slot, req.Path)
	}
	p.mu.Lock()
	interval := p.interval
	p.mu.Unlock()
	interval = max(interval, time.Duration(len(picked))*p.gap)
	for i := range picked {
		picked[i].Interval = monitor.Duration(interval)
	}
	return &monitor.Profile{
		Defaults: monitor.Defaults{Interval: monitor.Duration(interval), OnChange: true},
		Entries:  picked,
	}, nil
}

func under(path []string, scopes [][]string) bool {
	if len(scopes) == 0 {
		return true
	}
	for _, s := range scopes {
		if len(path) >= len(s) && strings.Join(path[:len(s)], "\x00") == strings.Join(s, "\x00") {
			return true
		}
	}
	return false
}
