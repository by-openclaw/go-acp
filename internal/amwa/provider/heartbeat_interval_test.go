package provider

// IS-09 taking effect (the AMWA suite's IS-09-02 test_05): a recorded
// /global's is04.heartbeat_interval drives the registration client's
// live heartbeat cadence; without one — or with a non-positive value —
// the IS-04 §6.1 default (5 s) stays in force.

import (
	"testing"
	"time"

	"dhs/internal/amwa/codec/is09"
)

func TestSystemGlobalHeartbeatIntervalApplied(t *testing.T) {
	s := newTestNodeServer(t)
	rc := NewRegistrationClient(nil, "http://reg.invalid:8235", "v1.3", validBundle())
	rc.SetHeartbeatIntervalFn(s.systemHeartbeatInterval)

	// No /global recorded yet: the spec default.
	if got := rc.heartbeatInterval(); got != HeartbeatInterval {
		t.Fatalf("without a global: interval = %v, want %v", got, HeartbeatInterval)
	}

	// A recorded global takes effect — read live, so a /global that
	// arrives after the loop started still changes the cadence.
	s.applySystemGlobal(&is09.Global{
		ID: "0d0a0f0e-0000-4000-8000-000000000005", Version: "1:0",
		Label: "lab", Description: "lab system global",
		IS04: is09.IS04Config{HeartbeatInterval: 3},
	}, "http://sys.test/x-nmos/system/v1.0/global")
	if got := rc.heartbeatInterval(); got != 3*time.Second {
		t.Fatalf("with heartbeat_interval=3: interval = %v, want 3s", got)
	}

	// A global carrying no positive value keeps the default in force.
	s.applySystemGlobal(&is09.Global{
		ID: "0d0a0f0e-0000-4000-8000-000000000006", Version: "2:0",
		IS04: is09.IS04Config{HeartbeatInterval: 0},
	}, "http://sys.test/x-nmos/system/v1.0/global")
	if got := rc.heartbeatInterval(); got != HeartbeatInterval {
		t.Fatalf("with heartbeat_interval=0: interval = %v, want the %v default", got, HeartbeatInterval)
	}
}
