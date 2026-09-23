package main

import (
	"context"
	"log/slog"
	"sync"

	"dhs/internal/consumer"
	"dhs/internal/consumer/alarm"
	"dhs/internal/metrics"
)

// alarmMeter counts adopted verdict changes by the severity they moved
// to. The evaluator holds the current state; this holds the history a
// counter needs (rate() over dhs_alarm_transitions_total is "how often
// is this device changing its mind", which is what a flapping card
// looks like from Grafana).
type alarmMeter struct {
	mu sync.Mutex
	n  map[string]uint64
}

func (m *alarmMeter) observe(severity string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.n == nil {
		m.n = map[string]uint64{}
	}
	m.n[severity]++
}

func (m *alarmMeter) snapshot() map[string]uint64 {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]uint64, len(m.n))
	for k, v := range m.n {
		out[k] = v
	}
	return out
}

// serveWatchMetrics mounts the scrape endpoint for one watched device:
// the verdicts this watch produced (dhs_alarm_*) and, when the plugin
// keeps them, the traffic its connector moved (dhs_connector_*). The
// labels are the same for every protocol — proto, device, role — so a
// dashboard variable over `device` lists the plant, not one wire
// format's corner of it.
//
// The device label is the host as the operator typed it, which is the
// address they will type into Grafana.
func serveWatchMetrics(ctx context.Context, addr string, plug consumer.Protocol, ev *alarm.Evaluator, meter *alarmMeter, proto, host string) {
	labels := map[string]string{"proto": proto, "device": host, "role": "consumer"}

	// A connector that counts its frames exposes them; one that does
	// not still exposes its alarms. No protocol is left out because it
	// has not got round to instrumenting its session.
	var met *metrics.Connector
	if mp, ok := plug.(interface{ Metrics() *metrics.Connector }); ok {
		met = mp.Metrics()
	}

	serveMetricsEndpoint(ctx, slog.Default(), addr, met, labels,
		func() metrics.AlarmSnapshot { return alarmSnapshot(ev, meter) })
}

// alarmSnapshot is the scrape-time view of one evaluator. A watch with
// no template still answers — with no rules and nothing active, which
// is how "nobody is judging this device" reaches the dashboard.
func alarmSnapshot(ev *alarm.Evaluator, meter *alarmMeter) metrics.AlarmSnapshot {
	snap := metrics.AlarmSnapshot{Transitions: meter.snapshot()}
	if ev == nil {
		return snap
	}
	if tpl := ev.Template(); tpl != nil {
		snap.Model, snap.Rules = tpl.Model, len(tpl.Rows)
	}
	snap.Counts = ev.Counts()
	for _, tr := range ev.Active() {
		snap.Active = append(snap.Active, metrics.AlarmSample{
			Slot:     tr.Slot,
			Path:     tr.Path,
			Label:    tr.Label,
			Band:     tr.Band,
			Severity: tr.Severity.String(),
			Code:     float64(tr.Severity),
		})
	}
	return snap
}
