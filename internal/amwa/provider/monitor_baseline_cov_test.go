package provider

import (
	"testing"
	"time"

	"dhs/internal/amwa/codec/ms05"
)

// A domain's baseline is its truthful un-faulted state, which depends
// on what the monitor is actually doing: link is always up on a
// software node, external sync is only "used" once a source was named,
// and the stream domain is honestly unhealthy while a receiver is
// active with no stream and past its grace window.
func TestBaselineDomainLocked(t *testing.T) {
	s, _, key := engineFixture(t)
	s.mu.Lock()
	obj := s.objects[key]
	h := s.healthLocked(key) // allocated on first use
	s.mu.Unlock()
	if obj == nil || h == nil {
		t.Fatal("the fixture must carry a receiver monitor with a health record")
	}
	stream := streamDomainFor(obj.classID)

	// Inactive: every domain reads Inactive except link, which is up
	// whatever the monitor is doing.
	h.active = false
	h.graceTimer = nil
	h.syncSource = ""
	for domain, want := range map[string]int{
		"linkStatus":                    monStatusHealthy,
		"externalSynchronizationStatus": monStatusInactive,
		stream:                          monStatusInactive,
		"someOtherStatus":               monStatusInactive,
	} {
		got, msg := s.baselineDomainLocked(obj, h, domain)
		if got != want {
			t.Errorf("inactive %s = %d, want %d", domain, got, want)
		}
		if msg != "" {
			t.Errorf("inactive %s carries a message %q", domain, msg)
		}
	}

	// Active, past the grace window and with no stream: the stream
	// domain says so, and says why.
	h.active = true
	h.graceTimer = nil
	got, msg := s.baselineDomainLocked(obj, h, stream)
	if got != monStatusUnhealthy || msg == "" {
		t.Errorf("an active monitor with no stream = %d %q, want unhealthy with a message", got, msg)
	}
	// Still inside the grace window, the same monitor is healthy —
	// BCP-008 gives it statusReportingDelay before degrading.
	h.graceTimer = time.NewTimer(time.Hour)
	defer h.graceTimer.Stop()
	if got, _ := s.baselineDomainLocked(obj, h, stream); got != monStatusHealthy {
		t.Errorf("inside the grace window = %d, want healthy", got)
	}

	// Once a sync source was named the monitor IS using external
	// synchronization, so its baseline flips from NotUsed to Healthy.
	if got, _ := s.baselineDomainLocked(obj, h, "externalSynchronizationStatus"); got != monStatusInactive {
		t.Error("with no sync source the domain reads NotUsed")
	}
	h.syncSource = "39-A7-94-FF-FE-07-CB-D0"
	if got, _ := s.baselineDomainLocked(obj, h, "externalSynchronizationStatus"); got != monStatusHealthy {
		t.Error("with a sync source the domain reads healthy")
	}

	// Any other domain follows the monitor's activity.
	if got, _ := s.baselineDomainLocked(obj, h, "someOtherStatus"); got != monStatusHealthy {
		t.Error("an active monitor's other domains are healthy")
	}
}

// The stream domain is named per monitor kind: a receiver monitor
// reports connectionStatus, a sender monitor transmissionStatus.
func TestStreamDomainFor(t *testing.T) {
	s := configFixture(t)
	receiver := objectOfClass(t, s, ms05.NcClassId{1, 2, 2, 1})
	if got := streamDomainFor(receiver.classID); got != "connectionStatus" {
		t.Errorf("a receiver monitor's stream domain = %q", got)
	}
	sender := objectOfClass(t, s, ms05.NcClassId{1, 2, 2, 2})
	if got := streamDomainFor(sender.classID); got != "transmissionStatus" {
		t.Errorf("a sender monitor's stream domain = %q", got)
	}
}
