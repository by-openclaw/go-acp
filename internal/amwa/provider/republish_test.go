package provider

import (
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
)

// TestActivationRepublishesChangedResource pins the disagreement that
// made a live route invisible to a Controller.
//
// An IS-05 activation rewrites the IS-04 Receiver's `subscription`.
// IS-04 §4.2 then requires the Node to re-POST that resource, because
// the Registry learns of a change no other way. Before this hook
// existed the Node reported subscription.active=true on its own API
// while the Query API still said false — and a Controller renders
// routing state from the Query API, so a route that had worked looked
// like it had never happened.
func TestActivationRepublishesChangedResource(t *testing.T) {
	senderID := "2c47bf5e-1b2c-4abc-9def-deadbeef0005"
	receiverID := "2c47bf5e-1b2c-4abc-9def-deadbeef0006"

	bundle := &NodeConfig{
		Receivers: []is04.Receiver{{
			ResourceCore: is04.ResourceCore{ID: receiverID, Version: "1700000000:0"},
			Transport:    "urn:x-nmos:transport:rtp",
		}},
	}
	srv := &IS05ConnectionServer{bundle: bundle, store: newConnectionStore()}

	var gotType is04.ResourceType
	var gotID string
	var gotActive bool
	srv.SetOnResourceChanged(func(t is04.ResourceType, data any) {
		gotType = t
		if r, ok := data.(*is04.Receiver); ok {
			gotID = r.ID
			gotActive = r.Subscription.Active
		}
	})

	srv.updateIS04Subscription("receivers", receiverID, is05.StagedSender{
		MasterEnableField: is05.MasterEnableField{MasterEnable: true},
		ReceiverID:        &senderID,
	})

	if gotType != is04.ResourceReceiver {
		t.Fatalf("republished type = %q, want receiver", gotType)
	}
	if gotID != receiverID {
		t.Fatalf("republished id = %q, want %q", gotID, receiverID)
	}
	if !gotActive {
		t.Fatal("the republished copy must carry subscription.active=true — " +
			"publishing the pre-activation state is the bug this guards")
	}
}

// TestRepublishedResourceIsASnapshot: the registration loop encodes the
// payload on another goroutine, so handing it a pointer into the bundle
// would let a later activation rewrite it mid-flight and publish a
// version that never existed.
func TestRepublishedResourceIsASnapshot(t *testing.T) {
	id := "2c47bf5e-1b2c-4abc-9def-deadbeef0006"
	first := "11111111-1111-4111-8111-111111111111"
	second := "22222222-2222-4222-8222-222222222222"

	bundle := &NodeConfig{
		Receivers: []is04.Receiver{{
			ResourceCore: is04.ResourceCore{ID: id, Version: "1700000000:0"},
			Transport:    "urn:x-nmos:transport:rtp",
		}},
	}
	srv := &IS05ConnectionServer{bundle: bundle, store: newConnectionStore()}

	var captured *is04.Receiver
	srv.SetOnResourceChanged(func(_ is04.ResourceType, data any) {
		if r, ok := data.(*is04.Receiver); ok && captured == nil {
			captured = r
		}
	})

	srv.updateIS04Subscription("receivers", id, is05.StagedSender{
		MasterEnableField: is05.MasterEnableField{MasterEnable: true},
		ReceiverID:        &first,
	})
	// A second activation lands before the first was encoded.
	srv.updateIS04Subscription("receivers", id, is05.StagedSender{
		MasterEnableField: is05.MasterEnableField{MasterEnable: true},
		ReceiverID:        &second,
	})

	if captured == nil {
		t.Fatal("no resource was republished")
	}
	if captured.Subscription.SenderID == nil || *captured.Subscription.SenderID != first {
		t.Fatalf("the first republish must still describe sender %s, got %v — "+
			"the payload was aliased to the live bundle", first, captured.Subscription.SenderID)
	}
}

// TestRepublishNeverBlocks: the hook runs inside the connection store's
// lock during an activation. Blocking there would deadlock the API that
// produced the change, so a full queue must drop, not wait.
func TestRepublishNeverBlocks(t *testing.T) {
	rc := NewRegistrationClient(nil, "http://127.0.0.1:1/", "v1.3", &NodeConfig{})
	r := &is04.Receiver{ResourceCore: is04.ResourceCore{ID: "x"}}
	// Far more than the queue depth, with nothing draining it.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10_000; i++ {
			rc.Republish(is04.ResourceReceiver, r)
		}
		close(done)
	}()
	<-done
}
