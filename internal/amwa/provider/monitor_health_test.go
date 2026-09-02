// The BCP-008 health engine, pinned at the exact behaviors the AMWA
// suites score (BCP008Test._check_monitor_status_changes and friends):
// Healthy immediately on activation, honest no-stream degradation only
// after statusReportingDelay, counted less-healthy transitions, clean
// immediate deactivation, delayed recovery, and the overallStatus
// mapping rule.
package provider

import (
	"testing"
	"time"
)

// engineFixture builds a configuration server over the audio bundle
// with a 1-second reporting delay on the first receiver monitor so
// timing tests stay fast.
func engineFixture(t *testing.T) (*IS14ConfigurationServer, string, string) {
	t.Helper()
	bundle := audioBundle()
	s := NewIS14ConfigurationServer(nil, bundle, IS14ConfigurationConfig{})
	rid := bundle.Receivers[0].ID
	key := s.monitorByResource[rid]
	s.mu.Lock()
	if p := s.objects[key].findProp("3p3"); p != nil {
		p.value = uint32(1)
	}
	s.mu.Unlock()
	return s, rid, key
}

func (s *IS14ConfigurationServer) propVal(t *testing.T, key, name string) any {
	t.Helper()
	s.mu.RLock()
	defer s.mu.RUnlock()
	p := findPropByName(s.objects[key], name)
	if p == nil {
		t.Fatalf("no property %q on %s", name, key)
	}
	return p.value
}

func waitStatus(t *testing.T, s *IS14ConfigurationServer, key, name string, want int, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if asInt(s.propVal(t, key, name)) == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s.%s = %v, want %d within %v", key, name, s.propVal(t, key, name), want, within)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestMonitorHealthActivationTimeline(t *testing.T) {
	s, rid, key := engineFixture(t)

	// Activation: Healthy immediately, overall included.
	s.SetMonitorActive(rid, true)
	for _, name := range []string{"connectionStatus", "streamStatus", "overallStatus"} {
		if got := asInt(s.propVal(t, key, name)); got != monStatusHealthy {
			t.Errorf("%s right after activation = %d, want Healthy", name, got)
		}
	}
	if got := asInt(s.propVal(t, key, "connectionStatusTransitionCounter")); got != 0 {
		t.Errorf("counter after Inactive->Healthy = %d, want 0 (not a less-healthy transition)", got)
	}

	// Reporting delay passes: the stream domain tells the truth.
	waitStatus(t, s, key, "connectionStatus", monStatusUnhealthy, 3*time.Second)
	if got := asInt(s.propVal(t, key, "overallStatus")); got != monStatusUnhealthy {
		t.Errorf("overallStatus after no-stream degradation = %d, want Unhealthy (max rule)", got)
	}
	if got := asInt(s.propVal(t, key, "connectionStatusTransitionCounter")); got != 1 {
		t.Errorf("counter after Healthy->Unhealthy = %d, want 1", got)
	}
	if msg := s.propVal(t, key, "connectionStatusMessage"); msg != monNoStreamMessage {
		t.Errorf("connectionStatusMessage = %v, want the no-stream message", msg)
	}

	// Deactivation: straight to Inactive, no delay.
	s.SetMonitorActive(rid, false)
	for _, name := range []string{"connectionStatus", "streamStatus", "overallStatus"} {
		if got := asInt(s.propVal(t, key, name)); got != monStatusInactive {
			t.Errorf("%s after deactivation = %d, want Inactive", name, got)
		}
	}
}

func TestMonitorHealthDeactivateInsideGraceStaysClean(t *testing.T) {
	s, rid, key := engineFixture(t)
	s.SetMonitorActive(rid, true)
	s.SetMonitorActive(rid, false)

	// The grace timer must be cancelled: no unhealthy transition may
	// arrive after a clean deactivation.
	time.Sleep(1500 * time.Millisecond)
	if got := asInt(s.propVal(t, key, "connectionStatus")); got != monStatusInactive {
		t.Fatalf("connectionStatus after deactivate-inside-grace = %d, want Inactive", got)
	}
	if got := asInt(s.propVal(t, key, "connectionStatusTransitionCounter")); got != 0 {
		t.Fatalf("counter after clean deactivation = %d, want 0", got)
	}
}

func TestMonitorHealthAutoResetOnActivation(t *testing.T) {
	s, rid, key := engineFixture(t)
	s.SetMonitorActive(rid, true)
	waitStatus(t, s, key, "connectionStatus", monStatusUnhealthy, 3*time.Second)
	s.SetMonitorActive(rid, false)

	// autoResetCountersAndMessages seeds true: re-activation clears the
	// counter and the message from the previous session.
	s.SetMonitorActive(rid, true)
	if got := asInt(s.propVal(t, key, "connectionStatusTransitionCounter")); got != 0 {
		t.Errorf("counter after auto-reset activation = %d, want 0", got)
	}
	if msg := s.propVal(t, key, "connectionStatusMessage"); msg != nil {
		t.Errorf("message after auto-reset activation = %v, want null", msg)
	}
}

func TestMonitorHealthFaultInjectAndDelayedRecovery(t *testing.T) {
	s, rid, key := engineFixture(t)
	role := "ReceiverMonitor-00"
	s.SetMonitorActive(rid, true)
	waitStatus(t, s, key, "connectionStatus", monStatusUnhealthy, 3*time.Second)

	// Past the window: a link fault lands immediately and is counted.
	if err := s.SetMonitorFault(role, "linkStatus", monStatusPartiallyHealthy, "one path down"); err != nil {
		t.Fatalf("inject: %v", err)
	}
	if got := asInt(s.propVal(t, key, "linkStatus")); got != monStatusPartiallyHealthy {
		t.Fatalf("linkStatus after inject = %d, want PartiallyHealthy", got)
	}
	if got := asInt(s.propVal(t, key, "linkStatusTransitionCounter")); got != 1 {
		t.Errorf("link counter after inject = %d, want 1", got)
	}

	// Recovery is delayed by statusReportingDelay — still degraded
	// right after the clear, back to AllUp only once the delay passed.
	if err := s.SetMonitorFault(role, "linkStatus", monStatusHealthy, ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := asInt(s.propVal(t, key, "linkStatus")); got != monStatusPartiallyHealthy {
		t.Fatalf("linkStatus immediately after clear = %d, want still PartiallyHealthy", got)
	}
	waitStatus(t, s, key, "linkStatus", monStatusHealthy, 3*time.Second)
	if msg := s.propVal(t, key, "linkStatusMessage"); msg != nil {
		t.Errorf("linkStatusMessage after recovery = %v, want null", msg)
	}

	// Inactive monitors refuse injection.
	s.SetMonitorActive(rid, false)
	if err := s.SetMonitorFault(role, "linkStatus", monStatusUnhealthy, "x"); err == nil {
		t.Error("inject on an inactive monitor must fail")
	}
}

func TestMonitorHealthFaultDuringGraceHeldToWindowEnd(t *testing.T) {
	s, rid, key := engineFixture(t)
	role := "ReceiverMonitor-00"
	s.SetMonitorActive(rid, true)

	// Inside the window: the degradation must NOT report yet.
	if err := s.SetMonitorFault(role, "linkStatus", monStatusUnhealthy, "cable pulled"); err != nil {
		t.Fatalf("inject: %v", err)
	}
	if got := asInt(s.propVal(t, key, "linkStatus")); got != monStatusHealthy {
		t.Fatalf("linkStatus inside grace = %d, want still AllUp", got)
	}
	// At window end both the no-stream truth and the held fault land.
	waitStatus(t, s, key, "linkStatus", monStatusUnhealthy, 3*time.Second)
	waitStatus(t, s, key, "connectionStatus", monStatusUnhealthy, time.Second)
}

func TestMonitorHealthSyncSourceChange(t *testing.T) {
	s, rid, key := engineFixture(t)
	role := "ReceiverMonitor-00"
	s.SetMonitorActive(rid, true)

	if err := s.SetMonitorSyncSource(role, "ptp-gm-1"); err != nil {
		t.Fatalf("sync source: %v", err)
	}
	if got := s.propVal(t, key, "synchronizationSourceId"); got != "ptp-gm-1" {
		t.Errorf("synchronizationSourceId = %v", got)
	}
	if got := asInt(s.propVal(t, key, "externalSynchronizationStatus")); got != monStatusPartiallyHealthy {
		t.Fatalf("extSync right after change = %d, want PartiallyHealthy", got)
	}
	if got := asInt(s.propVal(t, key, "externalSynchronizationStatusTransitionCounter")); got != 1 {
		t.Errorf("extSync counter = %d, want 1", got)
	}
	// Recovery lands Healthy (a sync source is in use now, not
	// NotUsed) after the reporting delay.
	waitStatus(t, s, key, "externalSynchronizationStatus", monStatusHealthy, 3*time.Second)
}

func TestMonitorHealthPacketCountersAndReset(t *testing.T) {
	s, rid, key := engineFixture(t)
	role := "ReceiverMonitor-00"
	s.SetMonitorActive(rid, true)

	if err := s.AddMonitorPacketCounters(role, "late", "session", 7); err != nil {
		t.Fatalf("add counters: %v", err)
	}
	if err := s.AddMonitorPacketCounters(role, "late", "session", 3); err != nil {
		t.Fatalf("add counters: %v", err)
	}
	s.mu.RLock()
	oid := s.objects[key].oid
	s.mu.RUnlock()
	got := s.MonitorPacketCounters(oid, "late")
	if len(got) != 1 || got[0].Name != "session" || got[0].Value != 10 {
		t.Fatalf("late counters = %+v, want one 'session' entry at 10", got)
	}

	// Reset clears injected packet counters with everything else.
	s.mu.Lock()
	changes := s.resetCountersLocked(key, s.objects[key])
	s.mu.Unlock()
	_ = changes
	if got := s.MonitorPacketCounters(oid, "late"); len(got) != 0 {
		t.Fatalf("late counters after reset = %+v, want empty", got)
	}
}

func TestFaultMethodArgValidation(t *testing.T) {
	s, rid, _ := engineFixture(t)
	s.SetMonitorActive(rid, true)

	cases := []struct {
		name string
		args string
	}{
		{"InjectMonitorFault", `{}`},
		{"InjectMonitorFault", `{"monitorRole":"ReceiverMonitor-00","domain":"linkStatus","status":7}`},
		{"InjectMonitorFault", `{"monitorRole":"nope","domain":"linkStatus","status":3}`},
		{"InjectMonitorFault", `{"monitorRole":"ReceiverMonitor-00","domain":"gainDb","status":3}`},
		{"ClearMonitorFault", `{"monitorRole":"ReceiverMonitor-00"}`},
		{"SetMonitorSyncSource", `{"monitorRole":"ReceiverMonitor-00"}`},
		{"AddMonitorPacketCounters", `{"monitorRole":"ReceiverMonitor-00","counter":"weird","name":"x"}`},
		{"NoSuchMethod", `{"monitorRole":"ReceiverMonitor-00"}`},
	}
	for _, tc := range cases {
		if err := s.invokeFaultMethod(tc.name, []byte(tc.args)); err == nil {
			t.Errorf("%s(%s) accepted, want error", tc.name, tc.args)
		}
	}

	if err := s.invokeFaultMethod("InjectMonitorFault",
		[]byte(`{"monitorRole":"ReceiverMonitor-00","domain":"linkStatus","status":3,"message":"drill"}`)); err != nil {
		t.Errorf("valid inject rejected: %v", err)
	}
}
