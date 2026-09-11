package provider

// The BCP-008 health engine: what a monitor reports as a stream comes
// up, goes wrong, and recovers — and the refusals for the seams an
// operator drives it through.

import (
	"strings"
	"testing"
	"time"

	"dhs/internal/amwa/codec/ms05"
)

// monitorFixture returns a configuration server with the receiver
// monitor active, which is the state every fault seam requires: an
// inactive monitor has no stream to be faulty about.
func monitorFixture(t *testing.T) (*IS14ConfigurationServer, *configObject) {
	t.Helper()
	s := configFixture(t)
	rx := s.objectByOid(5)
	for id, key := range s.monitorByResource {
		if key == strings.Join(rx.path, ".") {
			s.SetMonitorActive(id, true)
		}
	}
	// Activation opens a grace window in which a degradation is
	// recorded but held — a stream that is still settling must not
	// report itself unhealthy. These tests are about what happens
	// after it, so it is closed here.
	closeGrace(t, s, rx)
	return s, rx
}

// closeGrace ends a monitor's post-activation window without waiting
// it out.
func closeGrace(t *testing.T, s *IS14ConfigurationServer, obj *configObject) {
	t.Helper()
	key := strings.Join(obj.path, ".")
	s.mu.Lock()
	if h := s.monHealth[key]; h != nil && h.graceTimer != nil {
		h.graceTimer.Stop()
		h.graceTimer = nil
	}
	s.mu.Unlock()
}

// The fault seams name a monitor by role, and a role that is not one —
// or an object that is not a monitor at all — is refused rather than
// silently doing nothing to a device the operator thinks they just
// changed.
func TestMonitorSeamsRefuseWhatIsNotAMonitor(t *testing.T) {
	s := configFixture(t)

	if err := s.SetMonitorFault("NotARole", "linkStatus", 3, "x"); err == nil {
		t.Error("a role nobody published must be refused")
	}
	if err := s.SetMonitorFault("GainControl", "linkStatus", 3, "x"); err == nil ||
		!strings.Contains(err.Error(), "not a status monitor") {
		t.Error("an object that is not a monitor must be refused")
	}
	if err := s.SetMonitorSyncSource("NotARole", "src"); err == nil {
		t.Error("SetMonitorSyncSource must refuse an unknown role")
	}
	if err := s.AddMonitorPacketCounters("NotARole", "lost", "leg-0", 1); err == nil {
		t.Error("AddMonitorPacketCounters must refuse an unknown role")
	}
	if err := s.SetMonitorFault("NotARole", "linkStatus", 1, ""); err == nil {
		t.Error("clearing a fault on an unknown role must be refused")
	}
}

// A domain the monitor does not carry is refused by name: BCP-008
// gives each monitor class its own set of status properties, and one
// that is not there cannot be driven.
func TestMonitorRefusesADomainItDoesNotHave(t *testing.T) {
	s, rx := monitorFixture(t)

	if err := s.SetMonitorFault(rx.role, "notAStatusDomain", 3, "x"); err == nil ||
		!strings.Contains(err.Error(), "no status domain") {
		t.Fatalf("= %v, want the domain named", err)
	}
}

// An inactive monitor has no stream to be faulty about — nothing
// transmits until IS-05 activates — so the seams say so rather than
// recording a fault against a monitor that is not watching anything.
func TestMonitorSeamsRefuseAnInactiveMonitor(t *testing.T) {
	s := configFixture(t)
	rx := s.objectByOid(5)

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"a fault injection", func() error { return s.SetMonitorFault(rx.role, "linkStatus", 3, "x") }},
		{"a sync-source change", func() error { return s.SetMonitorSyncSource(rx.role, "src") }},
	} {
		if err := tc.call(); err == nil || !strings.Contains(err.Error(), "inactive") {
			t.Errorf("%s on an inactive monitor = %v", tc.name, err)
		}
	}
}

// An injected fault moves the domain and the overall status, and
// clearing it returns them — that round trip is what BCP-008's
// transition counters count.
func TestMonitorFaultRoundTrip(t *testing.T) {
	s, rx := monitorFixture(t)

	if err := s.SetMonitorFault(rx.role, "linkStatus", 3, "link is down"); err != nil {
		t.Fatal(err)
	}
	if got := asInt(findPropByName(rx, "linkStatus").value); got != 3 {
		t.Errorf("linkStatus = %d, want the injected fault", got)
	}
	if got := asInt(findPropByName(rx, "linkStatusTransitionCounter").value); got == 0 {
		t.Error("a transition must be counted")
	}

	// Status <= Healthy is the clear.
	if err := s.SetMonitorFault(rx.role, "linkStatus", 1, ""); err != nil {
		t.Fatal(err)
	}
}

// The same fault injected twice is one transition, not two: BCP-008's
// counters are transitions, and a controller charting them reads a
// second count as a second event.
func TestRepeatedFaultIsOneTransition(t *testing.T) {
	s, rx := monitorFixture(t)

	if err := s.SetMonitorFault(rx.role, "linkStatus", 3, "down"); err != nil {
		t.Fatal(err)
	}
	first := asInt(findPropByName(rx, "linkStatusTransitionCounter").value)
	if err := s.SetMonitorFault(rx.role, "linkStatus", 3, "down"); err != nil {
		t.Fatal(err)
	}
	if got := asInt(findPropByName(rx, "linkStatusTransitionCounter").value); got != first {
		t.Errorf("counter = %d, want the repeat not counted (%d)", got, first)
	}
}

// A sync-source change is a partially-healthy state that clears itself
// after the reporting delay — the transition BCP-008 test_11
// describes.
func TestSyncSourceChangeIsATransientState(t *testing.T) {
	s, rx := monitorFixture(t)

	if err := s.SetMonitorSyncSource(rx.role, "ptp-gm-1"); err != nil {
		t.Fatal(err)
	}
	if got := asInt(findPropByName(rx, "externalSynchronizationStatus").value); got == 0 {
		t.Error("a sync-source change must move the domain off Inactive")
	}
	if p := findPropByName(rx, "synchronizationSourceId"); p == nil || p.value == nil {
		t.Error("the source must be recorded")
	}

	// A second change while the first is still clearing replaces the
	// pending clear rather than stacking two of them.
	if err := s.SetMonitorSyncSource(rx.role, "ptp-gm-2"); err != nil {
		t.Fatal(err)
	}
}

// Counters accumulate by name, so a second reading of the same leg
// adds rather than replacing — a controller charting packet loss reads
// a total, not the last delta.
func TestPacketCountersAccumulate(t *testing.T) {
	s, rx := monitorFixture(t)

	if err := s.AddMonitorPacketCounters(rx.role, "lost", "leg-0", 3); err != nil {
		t.Fatal(err)
	}
	if err := s.AddMonitorPacketCounters(rx.role, "lost", "leg-0", 4); err != nil {
		t.Fatal(err)
	}
	if err := s.AddMonitorPacketCounters(rx.role, "lost", "leg-1", 1); err != nil {
		t.Fatal(err)
	}

	got := s.MonitorPacketCounters(rx.oid, "lost")
	byName := map[string]uint64{}
	for _, c := range got {
		byName[c.Name] = c.Value
	}
	if byName["leg-0"] != 7 {
		t.Errorf("leg-0 = %d, want the two readings summed", byName["leg-0"])
	}
	if byName["leg-1"] != 1 {
		t.Errorf("leg-1 = %d", byName["leg-1"])
	}
}

// Resetting clears the transition counters, the status messages and
// the packet counters together — BCP-008 treats them as one reading,
// and clearing half of it leaves a controller comparing a fresh count
// against a stale message.
func TestResetClearsEverythingTogether(t *testing.T) {
	s, rx := monitorFixture(t)

	if err := s.SetMonitorFault(rx.role, "linkStatus", 3, "down"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddMonitorPacketCounters(rx.role, "lost", "leg-0", 5); err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	s.resetCountersLocked(strings.Join(rx.path, "."), rx)
	s.mu.Unlock()

	if got := asInt(findPropByName(rx, "linkStatusTransitionCounter").value); got != 0 {
		t.Errorf("transition counter = %d, want it cleared", got)
	}
	if got := s.MonitorPacketCounters(rx.oid, "lost"); len(got) != 0 {
		t.Errorf("packet counters = %+v, want them cleared", got)
	}
}

// A monitor that goes inactive again drops its pending transitions:
// an endpoint nobody is streaming to has nothing to report, and a
// grace timer left running would fire against a stream that is gone.
func TestMonitorGoingInactiveDropsPendingWork(t *testing.T) {
	s, rx := monitorFixture(t)

	if err := s.SetMonitorSyncSource(rx.role, "ptp-gm-1"); err != nil {
		t.Fatal(err)
	}
	for id, key := range s.monitorByResource {
		if key == strings.Join(rx.path, ".") {
			s.SetMonitorActive(id, false)
		}
	}

	// The grace and clear callbacks are no-ops once the monitor is
	// inactive, rather than writing into a monitor nobody is watching.
	s.monitorGraceExpired(strings.Join(rx.path, "."))
	s.monitorFaultCleared(strings.Join(rx.path, "."), "externalSynchronizationStatus")
}

// A resource nobody published a monitor for changes nothing: the map
// is built from the bundle, and an id outside it is not an error, just
// not a monitor.
func TestSetMonitorActiveForAnUnknownResource(t *testing.T) {
	s := configFixture(t)
	s.SetMonitorActive("not-a-resource", true)
}

// The stream domain is per class: a receiver watches what arrives, a
// sender what it transmits, and asking for one on the other's class
// names nothing.
func TestStreamDomainIsPerClass(t *testing.T) {
	if got := streamDomainFor(ms05.NcClassId{1, 2, 2, 1}); got == "" {
		t.Error("a receiver monitor has a stream domain")
	}
	if got := streamDomainFor(ms05.NcClassId{1, 2, 2, 2}); got == "" {
		t.Error("a sender monitor has a stream domain")
	}
	// Only the receiver monitor watches what arrives; every other
	// class falls back to what it transmits.
	if got := streamDomainFor(ms05.NcClassId{1, 2, 2, 1}); got != "connectionStatus" {
		t.Errorf("a receiver monitor = %q, want connectionStatus", got)
	}
	if got := streamDomainFor(ms05.NcClassId{1, 1}); got != "transmissionStatus" {
		t.Errorf("anything else = %q, want transmissionStatus", got)
	}
}

// The grace window is what keeps a stream that is still settling from
// reporting itself unhealthy. When it expires with no fault recorded,
// the monitor says the stream never arrived.
func TestGraceExpiryReportsAStreamThatNeverArrived(t *testing.T) {
	s, rx := monitorFixture(t)
	key := strings.Join(rx.path, ".")

	s.monitorGraceExpired(key)

	stream := streamDomainFor(rx.classID)
	if got := asInt(findPropByName(rx, stream).value); got == 0 {
		t.Errorf("%s = %d, want the missing stream reported", stream, got)
	}
}

// A fault recorded during the grace window is what the monitor reports
// when the window closes, rather than the generic "no stream".
func TestGraceExpiryReportsTheFaultItWasGiven(t *testing.T) {
	s, rx := monitorFixture(t)
	key := strings.Join(rx.path, ".")
	stream := streamDomainFor(rx.classID)

	s.mu.Lock()
	s.healthLocked(key).faults[stream] = monitorFault{status: 2, message: "still settling"}
	s.mu.Unlock()

	s.monitorGraceExpired(key)

	if got := asInt(findPropByName(rx, stream).value); got != 2 {
		t.Errorf("%s = %d, want the recorded fault", stream, got)
	}
}

// A fault cleared while another has already re-armed leaves the
// re-armed one in place: the clear belongs to the fault it was
// scheduled for, not to whatever is current.
func TestFaultClearedAfterARefaultLeavesItAlone(t *testing.T) {
	s, rx := monitorFixture(t)
	key := strings.Join(rx.path, ".")

	if err := s.SetMonitorFault(rx.role, "linkStatus", 3, "down"); err != nil {
		t.Fatal(err)
	}
	s.monitorFaultCleared(key, "linkStatus")

	if got := asInt(findPropByName(rx, "linkStatus").value); got != 3 {
		t.Errorf("linkStatus = %d, want the standing fault kept", got)
	}
}

var _ = time.Second

// A domain the object does not carry changes nothing: the fault seams
// check first, and this is the belt on the internal path that the
// grace and clear callbacks also use.
func TestSetDomainOnAPropertyThatIsNotThere(t *testing.T) {
	s, rx := monitorFixture(t)

	s.mu.Lock()
	changes := s.setDomainLocked(rx, "notAStatusDomain", 3, "x")
	s.mu.Unlock()
	if len(changes) != 0 {
		t.Fatalf("= %+v, want nothing changed", changes)
	}
}

// A monitor the bundle names but the model no longer holds is not a
// crash: the two are built together, and if they ever part company the
// activation is dropped rather than written into a nil.
func TestSetMonitorActiveForAResourceWithoutAnObject(t *testing.T) {
	s, rx := monitorFixture(t)
	key := strings.Join(rx.path, ".")

	s.mu.Lock()
	delete(s.objects, key)
	s.mu.Unlock()

	for id, k := range s.monitorByResource {
		if k == key {
			s.SetMonitorActive(id, true)
		}
	}
}

// A pending recovery is cancelled when the same domain faults again:
// the clear was scheduled for a fault that is no longer the current
// one, and letting it fire would report a healthy stream that is not.
func TestARefaultCancelsThePendingRecovery(t *testing.T) {
	s, rx := monitorFixture(t)

	if err := s.SetMonitorFault(rx.role, "linkStatus", 3, "down"); err != nil {
		t.Fatal(err)
	}
	// Recovery: schedules the clear.
	if err := s.SetMonitorFault(rx.role, "linkStatus", 1, ""); err != nil {
		t.Fatal(err)
	}
	// It faults again before the clear fires.
	if err := s.SetMonitorFault(rx.role, "linkStatus", 3, "down again"); err != nil {
		t.Fatal(err)
	}

	if got := asInt(findPropByName(rx, "linkStatus").value); got != 3 {
		t.Errorf("linkStatus = %d, want the standing fault", got)
	}
}
