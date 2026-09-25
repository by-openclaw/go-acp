package metrics

import (
	"fmt"
	"sort"

	"github.com/prometheus/client_golang/prometheus"
)

// Alarm verdicts on the Prometheus surface. The engine that produces
// them (internal/consumer/alarm) is not imported here: a caller hands
// in a function returning the snapshot below, so the exposition stays
// neutral and one protocol's alarms look exactly like another's.
//
// Four series, and they answer the four questions an operator asks:
//
//	dhs_alarm_severity{...,path}   what is wrong, per object
//	dhs_alarm_active{...,severity} how much is wrong, per severity
//	dhs_alarm_transitions_total    how often it has changed
//	dhs_alarm_rules{...,model}     is anything judging this device at all
//
// Every series carries the device label, and the last two exist even
// while the device is healthy — so a dashboard variable built on
// label_values(dhs_alarm_rules, device) lists every watched device,
// not only the ones currently in trouble.

// AlarmSample is one object whose verdict is above normal.
type AlarmSample struct {
	Slot int
	Path string
	// Label is the human name the device gives the object, Band the
	// rule that fired ("high major", "stalled", "drift").
	Label string
	Band  string
	// Severity is the verdict's name, Code its rank on the ladder
	// (the number a graph can threshold on).
	Severity string
	Code     float64
}

// AlarmSnapshot is everything one watched device shows at scrape time.
type AlarmSnapshot struct {
	// Model is the template's identity ("FusioN6@0x68cd783f"), empty
	// when no template is in force.
	Model string
	Rules int

	// Active lists the objects above normal, Counts how many objects
	// sit at each severity name, Transitions how many verdict changes
	// each severity has seen since the process started.
	Active      []AlarmSample
	Counts      map[string]int
	Transitions map[string]uint64
}

// AttachAlarms registers an alarm view under the given label set —
// typically {"proto": "acp2", "device": "10.6.255.102", "role":
// "consumer"}. src is called on every scrape and must not block.
func (p *PromRegistry) AttachAlarms(src func() AlarmSnapshot, labels map[string]string) error {
	if src == nil {
		return fmt.Errorf("metrics: AttachAlarms nil source")
	}
	return p.reg.Register(&alarmCollector{src: src, labels: labels})
}

type alarmCollector struct {
	src    func() AlarmSnapshot
	labels map[string]string
}

func (ac *alarmCollector) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(ac, ch)
}

func (ac *alarmCollector) Collect(ch chan<- prometheus.Metric) {
	s := ac.src()
	lblNames, lblValues := labelsFor(ac.labels)

	emit := func(name, help string, kind prometheus.ValueType, v float64, extraNames, extraVals []string) {
		desc := prometheus.NewDesc("dhs_alarm_"+name, help,
			append(append([]string{}, lblNames...), extraNames...), nil)
		ch <- prometheus.MustNewConstMetric(desc, kind, v,
			append(append([]string{}, lblValues...), extraVals...)...)
	}

	emit("rules", "Alarm rules in force for this device.", prometheus.GaugeValue,
		float64(s.Rules), []string{"model"}, []string{s.Model})

	for _, name := range sortedCountKeys(s.Counts) {
		emit("active", "Objects currently at this severity.", prometheus.GaugeValue,
			float64(s.Counts[name]), []string{"severity"}, []string{name})
	}
	for _, name := range sortedTransitionKeys(s.Transitions) {
		emit("transitions_total", "Verdict changes adopted, by the severity they moved to.",
			prometheus.CounterValue, float64(s.Transitions[name]),
			[]string{"severity"}, []string{name})
	}

	// One series per object above normal. A cleared object stops
	// being exported, which is what "no alarm" looks like in
	// Prometheus; dhs_alarm_active keeps the zero visible. The order
	// here does not matter — client_golang sorts each family by its
	// label values before it writes the exposition.
	for _, a := range s.Active {
		emit("severity", "Verdict rank of one object (info 0 … error 5).",
			prometheus.GaugeValue, a.Code,
			[]string{"slot", "path", "label", "band", "severity"},
			[]string{fmt.Sprint(a.Slot), a.Path, a.Label, a.Band, a.Severity})
	}
}

func sortedCountKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedTransitionKeys(m map[string]uint64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
