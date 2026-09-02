// BCP-008 monitor health engine — the runtime behind the
// NcReceiverMonitor / NcSenderMonitor objects in the IS-14/IS-12
// device model.
//
// The AMWA BCP-008 suites activate a monitored resource with a
// synthetic SDP and expect the monitor to tell the truth about it:
// transition to Healthy immediately on activation, hold that for
// statusReportingDelay (3p3), then — since a reference node has no
// media plane and no stream ever arrives — transition the stream
// domain (connectionStatus on receivers, transmissionStatus on
// senders) to Unhealthy, incrementing its transition counter and
// publishing a status message. Deactivation goes straight to Inactive
// with no intermediate unhealthy states and no delay. overallStatus is
// never written directly: it is Inactive while the stream domains are
// Inactive and otherwise the least healthy (max) of all domain
// statuses — the exact mapping the suites' check_overall_status
// enforces.
//
// Injected faults (the DhsFaultControl worker, vendor_fault.go) ride
// the same engine: a less-healthy report lands immediately once the
// post-activation delay window has passed (held to the window's end
// inside it), and a recovery to a more-healthy state is delayed by
// statusReportingDelay — the two timing rules BCP-008 states for
// status reporting.
package provider

import (
	"fmt"
	"time"

	"dhs/internal/amwa/codec/ms05"
)

// BCP-008 status enum values shared by NcOverallStatus,
// NcConnectionStatus, NcStreamStatus, NcTransmissionStatus,
// NcEssenceStatus (and, positionally, NcLinkStatus /
// NcSynchronizationStatus: higher is less healthy in every one).
const (
	monStatusInactive         = 0 // NotUsed for externalSynchronizationStatus
	monStatusHealthy          = 1 // AllUp for linkStatus
	monStatusPartiallyHealthy = 2
	monStatusUnhealthy        = 3
)

// monNoStreamMessage is the truthful stream-domain fault of a
// reference node: nothing is ever received or transmitted.
const monNoStreamMessage = "no stream on the synthetic media plane (reference node)"

// monGraceMargin pads the post-activation hold past statusReportingDelay.
// The delay is a MINIMUM ("delay the reporting ... for the duration,
// and then transition"), and an observer clocks the window from its
// RECEIPT of the Healthy notification — later than the activation
// instant the timer runs from. Firing at exactly the delay makes the
// degradation notification race the observer's window cutoff: the
// AMWA suites' check_overall_status aggregates mid-window
// notifications with no tolerance, so connectionStatus=Unhealthy
// could land inside the window while the overallStatus that follows
// lands outside it — scored as an overall-status mapping error
// (BCP-008 test_08, seen live on the first fleet run). Half a second
// clears the cutoff deterministically and stays well inside the
// suites' post-window observation sleep (delay + 2 s).
const monGraceMargin = 500 * time.Millisecond

// monitorFault is one injected domain override.
type monitorFault struct {
	status  int
	message string
}

// ncCounter is the monitoring feature set's NcCounter datatype (name /
// value / nullable description) in the wire shape the 4m1/4m2 counter
// getters return.
type ncCounter struct {
	Name        string  `json:"name"`
	Value       uint64  `json:"value"`
	Description *string `json:"description"`
}

// monitorHealth is the per-monitor runtime state, guarded by the
// configuration server's mutex.
type monitorHealth struct {
	active      bool
	graceEnd    time.Time   // end of the post-activation delay window
	graceTimer  *time.Timer // fires monitorGraceExpired; nil once fired
	clearTimers map[string]*time.Timer

	// faults are the injected domain overrides (domain property name →
	// fault). The no-stream fault is not stored here — it is the
	// baseline of the stream domain while active.
	faults map[string]monitorFault

	// syncSource mirrors synchronizationSourceId once a sync source
	// change was injected: from then on the monitor is "using external
	// synchronization" and its baseline extSync status is Healthy.
	syncSource string

	// packetCounters back the 4m1/4m2 counter getters, keyed by kind
	// ("late" / "lost" for receivers, "transmission" for senders).
	packetCounters map[string][]ncCounter
}

// streamDomainFor names the domain property BCP-008 calls the "stream
// status" of the class — the one that must go Unhealthy when no
// stream exists.
func streamDomainFor(classID ms05.NcClassId) string {
	if classDerivedFrom(classID, ms05.NcClassId{1, 2, 2, 1}) {
		return "connectionStatus"
	}
	return "transmissionStatus"
}

// inactiveableDomainsFor lists the domain properties with an Inactive
// option — the ones deactivation must zero (the suites'
// get_inactiveable_status_property_ids, minus overallStatus which the
// recompute owns).
func inactiveableDomainsFor(classID ms05.NcClassId) []string {
	if classDerivedFrom(classID, ms05.NcClassId{1, 2, 2, 1}) {
		return []string{"connectionStatus", "streamStatus"}
	}
	return []string{"transmissionStatus", "essenceStatus"}
}

// domainStatusNames lists every domain status feeding overallStatus.
func domainStatusNames(classID ms05.NcClassId) []string {
	return append([]string{"linkStatus", "externalSynchronizationStatus"},
		inactiveableDomainsFor(classID)...)
}

// propChange is one fired notification, collected under the lock and
// delivered outside it.
type propChange struct {
	oid ms05.NcOid
	id  ms05.NcPropertyId
	v   any
}

// health returns (allocating if needed) the runtime state for a
// monitor object key. Callers hold s.mu.
func (s *IS14ConfigurationServer) healthLocked(key string) *monitorHealth {
	if s.monHealth == nil {
		s.monHealth = map[string]*monitorHealth{}
	}
	h, ok := s.monHealth[key]
	if !ok {
		h = &monitorHealth{
			faults:         map[string]monitorFault{},
			clearTimers:    map[string]*time.Timer{},
			packetCounters: map[string][]ncCounter{},
		}
		s.monHealth[key] = h
	}
	return h
}

// findPropByName locates a property slot by descriptor name.
func findPropByName(o *configObject, name string) *configProperty {
	for _, p := range o.props {
		if p.desc.Name == name {
			return p
		}
	}
	return nil
}

// asInt reads a status value regardless of how it was stored (seeded
// int, JSON-decoded float64, or typed uint).
func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case uint32:
		return int(n)
	case uint64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// monitorDelayLocked reads statusReportingDelay (3p3) as a duration.
func monitorDelayLocked(obj *configObject) time.Duration {
	if p := obj.findProp("3p3"); p != nil {
		if d := asInt(p.value); d > 0 {
			return time.Duration(d) * time.Second
		}
	}
	return 3 * time.Second
}

// setDomainLocked writes one domain status (with its message and, on
// a transition to a strictly less healthy non-neutral state, its
// transition counter), collecting notifications. No overallStatus
// recompute here — callers batch that once per event.
func (s *IS14ConfigurationServer) setDomainLocked(obj *configObject, domain string, status int, message string) []propChange {
	p := findPropByName(obj, domain)
	if p == nil {
		return nil
	}
	old := asInt(p.value)
	if old == status {
		return nil
	}
	var out []propChange
	p.value = status
	out = append(out, propChange{obj.oid, p.desc.ID, status})

	// Status messages accompany degraded states and clear otherwise.
	if mp := findPropByName(obj, domain+"Message"); mp != nil {
		var mv any
		if status >= monStatusPartiallyHealthy && message != "" {
			mv = message
		}
		if mp.value != mv {
			mp.value = mv
			out = append(out, propChange{obj.oid, mp.desc.ID, mv})
		}
	}

	// "Transitions to less healthy states are counted" — any move to a
	// strictly higher, degraded value (NotUsed→PartiallyHealthy on the
	// sync domain counts too: the domain became degraded).
	if status > old && status >= monStatusPartiallyHealthy {
		if cp := findPropByName(obj, domain+"TransitionCounter"); cp != nil {
			n := uint64(asInt(cp.value)) + 1
			cp.value = n
			out = append(out, propChange{obj.oid, cp.desc.ID, n})
		}
	}
	return out
}

// recomputeOverallLocked derives overallStatus from the domains per
// the BCP-008 mapping rule.
func (s *IS14ConfigurationServer) recomputeOverallLocked(obj *configObject) []propChange {
	inactiveable := inactiveableDomainsFor(obj.classID)
	overall := monStatusInactive
	inactive := false
	for _, d := range inactiveable {
		if p := findPropByName(obj, d); p != nil && asInt(p.value) == monStatusInactive {
			inactive = true
		}
	}
	if !inactive {
		for _, d := range domainStatusNames(obj.classID) {
			if p := findPropByName(obj, d); p != nil {
				if v := asInt(p.value); v > overall {
					overall = v
				}
			}
		}
	}
	p := findPropByName(obj, "overallStatus")
	if p == nil || asInt(p.value) == overall {
		return nil
	}
	p.value = overall
	return []propChange{{obj.oid, p.desc.ID, overall}}
}

// fire delivers collected notifications outside the lock.
func (s *IS14ConfigurationServer) fire(changes []propChange) {
	if s.onPropertyChanged == nil {
		return
	}
	for _, c := range changes {
		s.onPropertyChanged(c.oid, c.id, c.v)
	}
}

// resetCountersLocked zeroes every transition counter, clears every
// nullable status message, and drops the injected packet counters —
// the shared body of ResetCountersAndMessages and autoResetCounters.
func (s *IS14ConfigurationServer) resetCountersLocked(key string, obj *configObject) []propChange {
	var out []propChange
	for _, p := range obj.props {
		if hasSuffix(p.desc.Name, "TransitionCounter") && asInt(p.value) != 0 {
			p.value = uint64(0)
			out = append(out, propChange{obj.oid, p.desc.ID, uint64(0)})
		}
		if hasSuffix(p.desc.Name, "Message") && p.desc.IsNullable && p.value != nil {
			p.value = nil
			out = append(out, propChange{obj.oid, p.desc.ID, nil})
		}
	}
	s.healthLocked(key).packetCounters = map[string][]ncCounter{}
	return out
}

func hasSuffix(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}

// SetMonitorActive drives the monitor tied to an IS-04 sender or
// receiver through IS-05 activation state — the engine's primary
// input, wired from the connection server at construction.
func (s *IS14ConfigurationServer) SetMonitorActive(resourceID string, active bool) {
	key, ok := s.monitorByResource[resourceID]
	if !ok {
		return
	}
	s.mu.Lock()
	obj := s.objects[key]
	if obj == nil {
		s.mu.Unlock()
		return
	}
	h := s.healthLocked(key)
	// Any state change invalidates pending transitions.
	if h.graceTimer != nil {
		h.graceTimer.Stop()
		h.graceTimer = nil
	}
	for d, t := range h.clearTimers {
		t.Stop()
		delete(h.clearTimers, d)
	}
	var changes []propChange
	if active {
		h.active = true
		h.faults = map[string]monitorFault{}
		// autoResetCountersAndMessages: reset on activation when armed.
		if p := findPropByName(obj, "autoResetCountersAndMessages"); p != nil {
			if b, ok := p.value.(bool); ok && b {
				changes = append(changes, s.resetCountersLocked(key, obj)...)
			}
		}
		// Domains with an Inactive option MUST transition immediately
		// to Healthy on activation.
		for _, d := range inactiveableDomainsFor(obj.classID) {
			changes = append(changes, s.setDomainLocked(obj, d, monStatusHealthy, "")...)
		}
		hold := monitorDelayLocked(obj) + monGraceMargin
		h.graceEnd = time.Now().Add(hold)
		h.graceTimer = time.AfterFunc(hold, func() { s.monitorGraceExpired(key) })
	} else {
		h.active = false
		h.faults = map[string]monitorFault{}
		// Clean disconnect: straight to Inactive, no intermediate
		// unhealthy states, no reporting delay.
		for _, d := range inactiveableDomainsFor(obj.classID) {
			changes = append(changes, s.setDomainLocked(obj, d, monStatusInactive, "")...)
		}
	}
	changes = append(changes, s.recomputeOverallLocked(obj)...)
	s.mu.Unlock()
	s.fire(changes)
}

// monitorGraceExpired ends the post-activation delay window: the
// stream domain reports the truth — no stream exists on a reference
// node — and any less-healthy faults injected during the window land.
func (s *IS14ConfigurationServer) monitorGraceExpired(key string) {
	s.mu.Lock()
	obj := s.objects[key]
	h := s.monHealth[key]
	if obj == nil || h == nil || !h.active {
		s.mu.Unlock()
		return
	}
	h.graceTimer = nil
	var changes []propChange
	stream := streamDomainFor(obj.classID)
	if f, ok := h.faults[stream]; ok {
		changes = append(changes, s.setDomainLocked(obj, stream, f.status, f.message)...)
	} else {
		changes = append(changes, s.setDomainLocked(obj, stream, monStatusUnhealthy, monNoStreamMessage)...)
	}
	for d, f := range h.faults {
		if d == stream {
			continue
		}
		if p := findPropByName(obj, d); p != nil && f.status > asInt(p.value) {
			changes = append(changes, s.setDomainLocked(obj, d, f.status, f.message)...)
		}
	}
	changes = append(changes, s.recomputeOverallLocked(obj)...)
	s.mu.Unlock()
	s.fire(changes)
}

// monitorObjLocked resolves "root.<Role>" (or a bare role) to the
// monitor object + its health state. Callers hold s.mu.
func (s *IS14ConfigurationServer) monitorObjLocked(role string) (*configObject, *monitorHealth, string, error) {
	key := role
	if _, ok := s.objects[key]; !ok {
		key = "root." + role
	}
	obj, ok := s.objects[key]
	if !ok {
		return nil, nil, "", fmt.Errorf("no model object at role %q", role)
	}
	if !classDerivedFrom(obj.classID, ms05.NcClassId{1, 2, 2}) {
		return nil, nil, "", fmt.Errorf("%q is not a status monitor", role)
	}
	return obj, s.healthLocked(key), key, nil
}

// SetMonitorFault injects (or, with status <= Healthy, clears) one
// domain fault — the DhsFaultControl entry point. Timing follows the
// BCP-008 reporting rules: degradations land immediately once the
// activation window has passed and are held to its end inside it;
// recoveries are delayed by statusReportingDelay.
func (s *IS14ConfigurationServer) SetMonitorFault(role, domain string, status int, message string) error {
	s.mu.Lock()
	obj, h, key, err := s.monitorObjLocked(role)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if !h.active {
		s.mu.Unlock()
		return fmt.Errorf("monitor %q is inactive; activate its IS-05 resource first", role)
	}
	p := findPropByName(obj, domain)
	if p == nil || findPropByName(obj, domain+"TransitionCounter") == nil {
		s.mu.Unlock()
		return fmt.Errorf("monitor %q has no status domain %q", role, domain)
	}
	if t, ok := h.clearTimers[domain]; ok {
		t.Stop()
		delete(h.clearTimers, domain)
	}
	var changes []propChange
	cur := asInt(p.value)
	switch {
	case status >= monStatusPartiallyHealthy && status > cur:
		if h.graceTimer != nil {
			// Inside the activation window: record; the window's end
			// applies it.
			h.faults[domain] = monitorFault{status: status, message: message}
		} else {
			h.faults[domain] = monitorFault{status: status, message: message}
			changes = append(changes, s.setDomainLocked(obj, domain, status, message)...)
			changes = append(changes, s.recomputeOverallLocked(obj)...)
		}
	default:
		// Recovery (or a no-op level): forget the fault and restore
		// the baseline after the reporting delay.
		delete(h.faults, domain)
		delay := monitorDelayLocked(obj)
		h.clearTimers[domain] = time.AfterFunc(delay, func() { s.monitorFaultCleared(key, domain) })
	}
	s.mu.Unlock()
	s.fire(changes)
	return nil
}

// monitorFaultCleared restores a domain's baseline after the
// reporting-delay hold on recoveries.
func (s *IS14ConfigurationServer) monitorFaultCleared(key, domain string) {
	s.mu.Lock()
	obj := s.objects[key]
	h := s.monHealth[key]
	if obj == nil || h == nil || !h.active {
		s.mu.Unlock()
		return
	}
	delete(h.clearTimers, domain)
	if _, refaulted := h.faults[domain]; refaulted {
		s.mu.Unlock()
		return
	}
	status, msg := s.baselineDomainLocked(obj, h, domain)
	var changes []propChange
	changes = append(changes, s.setDomainLocked(obj, domain, status, msg)...)
	changes = append(changes, s.recomputeOverallLocked(obj)...)
	s.mu.Unlock()
	s.fire(changes)
}

// baselineDomainLocked is a domain's truthful un-faulted state.
func (s *IS14ConfigurationServer) baselineDomainLocked(obj *configObject, h *monitorHealth, domain string) (int, string) {
	switch domain {
	case "linkStatus":
		return monStatusHealthy, "" // AllUp
	case "externalSynchronizationStatus":
		if h.syncSource != "" {
			return monStatusHealthy, ""
		}
		return monStatusInactive, "" // NotUsed
	case streamDomainFor(obj.classID):
		if h.active && h.graceTimer == nil {
			return monStatusUnhealthy, monNoStreamMessage
		}
	}
	if h.active {
		return monStatusHealthy, ""
	}
	return monStatusInactive, ""
}

// SetMonitorSyncSource injects a synchronization source change:
// synchronizationSourceId updates, the sync domain dips to
// PartiallyHealthy (counted), and recovers to Healthy after the
// reporting delay — the transition BCP-008 test_11 describes.
func (s *IS14ConfigurationServer) SetMonitorSyncSource(role, sourceID string) error {
	s.mu.Lock()
	obj, h, key, err := s.monitorObjLocked(role)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if !h.active {
		s.mu.Unlock()
		return fmt.Errorf("monitor %q is inactive; activate its IS-05 resource first", role)
	}
	h.syncSource = sourceID
	var changes []propChange
	if p := findPropByName(obj, "synchronizationSourceId"); p != nil {
		var v any
		if sourceID != "" {
			v = sourceID
		}
		if p.value != v {
			p.value = v
			changes = append(changes, propChange{obj.oid, p.desc.ID, v})
		}
	}
	const domain = "externalSynchronizationStatus"
	if t, ok := h.clearTimers[domain]; ok {
		t.Stop()
	}
	changes = append(changes, s.setDomainLocked(obj, domain, monStatusPartiallyHealthy,
		"synchronization source changed to "+sourceID)...)
	changes = append(changes, s.recomputeOverallLocked(obj)...)
	delay := monitorDelayLocked(obj)
	h.clearTimers[domain] = time.AfterFunc(delay, func() { s.monitorFaultCleared(key, domain) })
	s.mu.Unlock()
	s.fire(changes)
	return nil
}

// AddMonitorPacketCounters accumulates one named counter of the given
// kind ("late" / "lost" on receivers, "transmission" on senders) so
// the 4m1/4m2 getters answer real NcCounter data.
func (s *IS14ConfigurationServer) AddMonitorPacketCounters(role, kind, name string, value uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, h, _, err := s.monitorObjLocked(role)
	if err != nil {
		return err
	}
	for i := range h.packetCounters[kind] {
		if h.packetCounters[kind][i].Name == name {
			h.packetCounters[kind][i].Value += value
			return nil
		}
	}
	desc := kind + " packet counter (injected by DhsFaultControl)"
	h.packetCounters[kind] = append(h.packetCounters[kind],
		ncCounter{Name: name, Value: value, Description: &desc})
	return nil
}

// MonitorPacketCounters answers a monitor's injected counters for the
// IS-12 getter methods. The empty list is the truthful default.
func (s *IS14ConfigurationServer) MonitorPacketCounters(oid ms05.NcOid, kind string) []ncCounter {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for key, o := range s.objects {
		if o.oid == oid {
			if h := s.monHealth[key]; h != nil {
				return append([]ncCounter(nil), h.packetCounters[kind]...)
			}
			return nil
		}
	}
	return nil
}
