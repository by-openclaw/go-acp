package registry

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"dhs/internal/amwa/codec/is04"
)

// The six-resource fixture chain: every id below is a real v4 UUID, so
// each Put runs the codec's own Validate rather than short-circuiting
// on a malformed identifier.
const (
	fxNode     = "11111111-1111-4111-8111-111111111111"
	fxDevice   = "22222222-2222-4222-8222-222222222222"
	fxSource   = "33333333-3333-4333-8333-333333333333"
	fxFlow     = "44444444-4444-4444-8444-444444444444"
	fxSender   = "55555555-5555-4555-8555-555555555555"
	fxReceiver = "66666666-6666-4666-8666-666666666666"
	fxAbsent   = "99999999-9999-4999-8999-999999999999"
)

func validSource(id, deviceID string) is04.Source {
	clock := "clk0"
	return is04.Source{
		ResourceCore: is04.ResourceCore{
			ID: id, Version: "0:0", Label: "s", Description: "x", Tags: map[string][]string{},
		},
		Caps:      map[string]any{},
		DeviceID:  deviceID,
		Parents:   []string{},
		ClockName: &clock,
		Format:    is04.FormatVideo,
	}
}

// validFlow is a raw video Flow: the IS-04 flow schema is a union of
// the concrete media shapes, so a Flow that names only its format
// matches none of them and never reaches the per-minor encoder.
func validFlow(id, sourceID, deviceID string) is04.Flow {
	return is04.Flow{
		ResourceCore: is04.ResourceCore{
			ID: id, Version: "0:0", Label: "f", Description: "x", Tags: map[string][]string{},
		},
		SourceID:    sourceID,
		DeviceID:    deviceID,
		Parents:     []string{},
		Format:      is04.FormatVideo,
		MediaType:   "video/raw",
		FrameWidth:  1920,
		FrameHeight: 1080,
		Interlace:   "progressive",
		ColorSpace:  "BT709",
		Components: []is04.FlowVideoComponent{
			{Name: "Y", Width: 1920, Height: 1080, BitDepth: 10},
			{Name: "Cb", Width: 960, Height: 1080, BitDepth: 10},
			{Name: "Cr", Width: 960, Height: 1080, BitDepth: 10},
		},
	}
}

func validSender(id, deviceID string) is04.Sender {
	flow := fxFlow
	href := "http://10.6.239.113:8080/x-nmos/node/v1.3/senders/" + id + "/manifest"
	return is04.Sender{
		ResourceCore: is04.ResourceCore{
			ID: id, Version: "0:0", Label: "snd", Description: "x", Tags: map[string][]string{},
		},
		FlowID:            &flow,
		Transport:         is04.TransportRTPMcast,
		DeviceID:          deviceID,
		ManifestHref:      &href,
		InterfaceBindings: []string{"eth0"},
	}
}

func validReceiver(id, deviceID string) is04.Receiver {
	return is04.Receiver{
		ResourceCore: is04.ResourceCore{
			ID: id, Version: "0:0", Label: "rx", Description: "x", Tags: map[string][]string{},
		},
		DeviceID:          deviceID,
		Transport:         is04.TransportRTPMcast,
		InterfaceBindings: []string{"eth0"},
		Format:            is04.FormatVideo,
	}
}

// populated returns a Store holding one of every resource type, wired
// into the IS-04 §3 parent chain.
func populated(t *testing.T) *Store {
	t.Helper()
	s := NewStore()
	must := func(what string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("Put%s: %v", what, err)
		}
	}
	must("Node", s.PutNode(validNode(fxNode)))
	must("Device", s.PutDevice(validDevice(fxDevice, fxNode)))
	must("Source", s.PutSource(validSource(fxSource, fxDevice)))
	must("Flow", s.PutFlow(validFlow(fxFlow, fxSource, fxDevice)))
	must("Sender", s.PutSender(validSender(fxSender, fxDevice)))
	must("Receiver", s.PutReceiver(validReceiver(fxReceiver, fxDevice)))
	return s
}

// IS-04 §3 makes every non-Node resource a child: one whose parent
// isn't registered is refused, and so is one that fails its own codec
// validation.
func TestStoreReferentialIntegrity(t *testing.T) {
	s := NewStore()
	for name, err := range map[string]error{
		"source":   s.PutSource(validSource(fxSource, fxDevice)),
		"sender":   s.PutSender(validSender(fxSender, fxDevice)),
		"receiver": s.PutReceiver(validReceiver(fxReceiver, fxDevice)),
		"flow":     s.PutFlow(validFlow(fxFlow, fxSource, fxDevice)),
	} {
		if err == nil || !strings.Contains(err.Error(), "device_id") {
			t.Errorf("%s without a device = %v", name, err)
		}
	}

	// With the Device registered, the Flow's source_id is the one that
	// still has to resolve — in every minor, including v1.0, where the
	// Flow carries no device_id at all.
	if err := s.PutNode(validNode(fxNode)); err != nil {
		t.Fatal(err)
	}
	if err := s.PutDevice(validDevice(fxDevice, fxNode)); err != nil {
		t.Fatal(err)
	}
	if err := s.PutFlow(validFlow(fxFlow, fxSource, fxDevice)); err == nil ||
		!strings.Contains(err.Error(), "source_id") {
		t.Errorf("flow without a source = %v", err)
	}
	if err := s.PutFlow(validFlow(fxFlow, fxSource, "")); err == nil ||
		!strings.Contains(err.Error(), "source_id") {
		t.Errorf("v1.0-shaped flow without a source = %v", err)
	}

	for name, err := range map[string]error{
		"node":     s.PutNode(validNode("not-a-uuid")),
		"device":   s.PutDevice(validDevice("not-a-uuid", fxNode)),
		"source":   s.PutSource(validSource("not-a-uuid", fxDevice)),
		"flow":     s.PutFlow(validFlow("not-a-uuid", fxSource, fxDevice)),
		"sender":   s.PutSender(validSender("not-a-uuid", fxDevice)),
		"receiver": s.PutReceiver(validReceiver("not-a-uuid", fxDevice)),
	} {
		if err == nil || !strings.Contains(err.Error(), "validation failed") {
			t.Errorf("invalid %s accepted: %v", name, err)
		}
	}
}

// A v1.0 Flow has no device_id — its parent is the Source. The store
// takes that shape and keeps the referential check on source_id.
func TestStorePutFlowWithoutDeviceID(t *testing.T) {
	s := populated(t)
	f := validFlow("77777777-7777-4777-8777-777777777777", fxSource, "")
	if err := s.PutFlow(f); err != nil {
		t.Fatalf("v1.0-shaped flow: %v", err)
	}
	if got, err := s.GetFlow(f.ID); err != nil || got.DeviceID != "" {
		t.Errorf("GetFlow = %+v, %v", got, err)
	}
}

// firstErr drops a getter's value so a table can compare errors alone.
func firstErr[T any](_ T, err error) error { return err }

// Every getter answers with the stored document, and ErrNotFound for
// an id nobody registered; every lister answers with the whole type.
func TestStoreGettersAndListers(t *testing.T) {
	s := populated(t)

	if got, err := s.GetDevice(fxDevice); err != nil || got.NodeID != fxNode {
		t.Errorf("GetDevice = %+v, %v", got, err)
	}
	if got, err := s.GetSource(fxSource); err != nil || got.DeviceID != fxDevice {
		t.Errorf("GetSource = %+v, %v", got, err)
	}
	if got, err := s.GetFlow(fxFlow); err != nil || got.SourceID != fxSource {
		t.Errorf("GetFlow = %+v, %v", got, err)
	}
	if got, err := s.GetSender(fxSender); err != nil || got.DeviceID != fxDevice {
		t.Errorf("GetSender = %+v, %v", got, err)
	}
	if got, err := s.GetReceiver(fxReceiver); err != nil || got.DeviceID != fxDevice {
		t.Errorf("GetReceiver = %+v, %v", got, err)
	}

	for name, err := range map[string]error{
		"Node":     firstErr(s.GetNode(fxAbsent)),
		"Device":   firstErr(s.GetDevice(fxAbsent)),
		"Source":   firstErr(s.GetSource(fxAbsent)),
		"Flow":     firstErr(s.GetFlow(fxAbsent)),
		"Sender":   firstErr(s.GetSender(fxAbsent)),
		"Receiver": firstErr(s.GetReceiver(fxAbsent)),
	} {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("Get%s(absent) = %v, want ErrNotFound", name, err)
		}
	}

	for name, n := range map[string]int{
		"nodes":     len(s.ListNodes()),
		"devices":   len(s.ListDevices()),
		"sources":   len(s.ListSources()),
		"flows":     len(s.ListFlows()),
		"senders":   len(s.ListSenders()),
		"receivers": len(s.ListReceivers()),
	} {
		if n != 1 {
			t.Errorf("List %s = %d, want 1", name, n)
		}
	}
}

// Re-registering the same document is a no-op — no grain, per IS-04
// §5.2 (a same-body `modified` grain carries a `pre` the AMWA tool
// rejects). A changed document emits `updated` with both halves.
func TestStorePutIsIdempotentPerType(t *testing.T) {
	s := populated(t)
	var (
		mu      sync.Mutex
		changes []Change
	)
	s.AddListener(func(c Change) {
		mu.Lock()
		changes = append(changes, c)
		mu.Unlock()
	})

	for _, err := range []error{
		s.PutNode(validNode(fxNode)),
		s.PutDevice(validDevice(fxDevice, fxNode)),
		s.PutSource(validSource(fxSource, fxDevice)),
		s.PutFlow(validFlow(fxFlow, fxSource, fxDevice)),
		s.PutSender(validSender(fxSender, fxDevice)),
		s.PutReceiver(validReceiver(fxReceiver, fxDevice)),
	} {
		if err != nil {
			t.Fatalf("identical re-registration: %v", err)
		}
	}
	mu.Lock()
	quiet := len(changes)
	mu.Unlock()
	if quiet != 0 {
		t.Fatalf("identical re-registration emitted %d grains, want none", quiet)
	}

	// A real change per type: `updated`, carrying the previous document.
	dev := validDevice(fxDevice, fxNode)
	dev.Label = "d2"
	src := validSource(fxSource, fxDevice)
	src.Label = "s2"
	flow := validFlow(fxFlow, fxSource, fxDevice)
	flow.Label = "f2"
	snd := validSender(fxSender, fxDevice)
	snd.Label = "snd2"
	rx := validReceiver(fxReceiver, fxDevice)
	rx.Label = "rx2"
	for _, err := range []error{
		s.PutDevice(dev), s.PutSource(src), s.PutFlow(flow),
		s.PutSender(snd), s.PutReceiver(rx),
	} {
		if err != nil {
			t.Fatalf("update: %v", err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(changes) != 5 {
		t.Fatalf("emitted %d grains, want one per updated type", len(changes))
	}
	for _, c := range changes {
		if c.Kind != ChangeUpdated {
			t.Errorf("%s grain kind = %s, want updated", c.ResourceType, c.Kind)
		}
		if len(c.Pre) == 0 || len(c.Post) == 0 {
			t.Errorf("%s update grain must carry both halves", c.ResourceType)
		}
	}
}

// DeleteResource removes one resource of any non-Node type, reports
// ErrNotFound for an id it doesn't hold, routes the Node type to the
// cascade, and refuses a type IS-04 doesn't define.
func TestStoreDeleteResourcePerType(t *testing.T) {
	for _, tc := range []struct {
		t  is04.ResourceType
		id string
	}{
		{is04.ResourceReceiver, fxReceiver},
		{is04.ResourceSender, fxSender},
		{is04.ResourceFlow, fxFlow},
		{is04.ResourceSource, fxSource},
		{is04.ResourceDevice, fxDevice},
	} {
		t.Run(string(tc.t), func(t *testing.T) {
			s := populated(t)
			var got []Change
			s.AddListener(func(c Change) { got = append(got, c) })

			if err := s.DeleteResource(tc.t, tc.id); err != nil {
				t.Fatalf("DeleteResource: %v", err)
			}
			if len(got) != 1 || got[0].Kind != ChangeDeleted || got[0].ID != tc.id {
				t.Fatalf("emitted %+v, want one deleted grain for %s", got, tc.id)
			}
			if len(got[0].Pre) == 0 {
				t.Error("a deletion grain carries the removed document as `pre`")
			}
			if err := s.DeleteResource(tc.t, fxAbsent); !errors.Is(err, ErrNotFound) {
				t.Errorf("deleting what is not there = %v, want ErrNotFound", err)
			}
		})
	}

	s := populated(t)
	if err := s.DeleteResource(is04.ResourceNode, fxNode); err != nil {
		t.Errorf("DeleteResource(node) = %v", err)
	}
	if len(s.ListNodes()) != 0 || len(s.ListDevices()) != 0 || len(s.ListSenders()) != 0 {
		t.Error("DeleteResource(node) must run the cascade")
	}
	if err := s.DeleteResource(is04.ResourceType("subscription"), fxNode); err == nil ||
		!strings.Contains(err.Error(), "invalid resource type") {
		t.Errorf("unknown type = %v", err)
	}
}

// Removing a Node evicts everything beneath it, and a v1.0 Flow —
// which names no device — is cascaded through its Source.
func TestStoreCascadeReachesEveryType(t *testing.T) {
	s := populated(t)
	if err := s.PutFlow(validFlow("77777777-7777-4777-8777-777777777777", fxSource, "")); err != nil {
		t.Fatal(err)
	}

	seen := map[is04.ResourceType]int{}
	s.AddListener(func(c Change) {
		if c.Kind == ChangeDeleted {
			seen[c.ResourceType]++
		}
	})
	s.DeleteNode(fxNode)

	for rt, n := range map[is04.ResourceType]int{
		is04.ResourceNode: 1, is04.ResourceDevice: 1, is04.ResourceSource: 1,
		is04.ResourceFlow: 2, is04.ResourceSender: 1, is04.ResourceReceiver: 1,
	} {
		if seen[rt] != n {
			t.Errorf("deleted %d %s, want %d", seen[rt], rt, n)
		}
	}
	if len(s.ListFlows()) != 0 || len(s.ListSources()) != 0 || len(s.ListReceivers()) != 0 {
		t.Error("the cascade left orphans behind")
	}

	// Deleting a Node that was never registered is a no-op, not a panic.
	s.DeleteNode(fxNode)
}

// The cascade stops at the Node boundary: a second Node's Device and
// its children survive the first Node's removal.
func TestStoreCascadeSpansOneNodeOnly(t *testing.T) {
	const (
		otherNode   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		otherDevice = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		otherSender = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	)
	s := populated(t)
	if err := s.PutNode(validNode(otherNode)); err != nil {
		t.Fatal(err)
	}
	if err := s.PutDevice(validDevice(otherDevice, otherNode)); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSender(validSender(otherSender, otherDevice)); err != nil {
		t.Fatal(err)
	}

	s.DeleteNode(fxNode)

	if _, err := s.GetDevice(otherDevice); err != nil {
		t.Errorf("the other Node's Device was evicted: %v", err)
	}
	if _, err := s.GetSender(otherSender); err != nil {
		t.Errorf("the other Node's Sender was evicted: %v", err)
	}
	if _, err := s.GetDevice(fxDevice); !errors.Is(err, ErrNotFound) {
		t.Errorf("the deleted Node's Device survived: %v", err)
	}
}

// An unsubscribed listener stops hearing grains; the others keep theirs.
func TestStoreAddListenerUnsubscribe(t *testing.T) {
	s := NewStore()
	var first, second int
	stop := s.AddListener(func(Change) { first++ })
	s.AddListener(func(Change) { second++ })

	if err := s.PutNode(validNode(fxNode)); err != nil {
		t.Fatal(err)
	}
	stop()
	n := validNode(fxNode)
	n.Label = "n2"
	if err := s.PutNode(n); err != nil {
		t.Fatal(err)
	}
	if first != 1 {
		t.Errorf("unsubscribed listener heard %d grains, want 1", first)
	}
	if second != 2 {
		t.Errorf("live listener heard %d grains, want 2", second)
	}
}

// SnapshotChanges bootstraps a subscriber with one `sync` grain per
// resource, encoded for the wire version the socket was opened at —
// and, when no codec is registered for that version, in the canonical
// shape rather than not at all.
func TestStoreSnapshotChangesEveryType(t *testing.T) {
	s := populated(t)
	for _, ver := range []string{"", "v1.3", "v9.9"} {
		snaps := s.SnapshotChanges(ver)
		if len(snaps) != 6 {
			t.Fatalf("SnapshotChanges(%q) = %d grains, want one per resource", ver, len(snaps))
		}
		byType := map[is04.ResourceType]Change{}
		for _, c := range snaps {
			if c.Kind != ChangeSync {
				t.Errorf("grain kind = %s, want sync", c.Kind)
			}
			if !json.Valid(c.Post) {
				t.Errorf("%s sync payload is not JSON: %s", c.ResourceType, c.Post)
			}
			byType[c.ResourceType] = c
		}
		if len(byType) != 6 {
			t.Errorf("SnapshotChanges(%q) covered %d types, want 6", ver, len(byType))
		}
	}

	// A v1.3 socket sees the v1.3 wire shape: the Node carries the
	// `api` object that minor added.
	for _, c := range s.SnapshotChanges("v1.3") {
		if c.ResourceType != is04.ResourceNode {
			continue
		}
		var body map[string]any
		if err := json.Unmarshal(c.Post, &body); err != nil {
			t.Fatal(err)
		}
		if _, ok := body["api"]; !ok {
			t.Errorf("a v1.3 Node carries `api`: %s", c.Post)
		}
	}
}

func mustJSONBytes(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// envelope wraps a resource the way the Registration API delivers it.
func envelope(t *testing.T, rt is04.ResourceType, v any) *is04.RegistrationRequest {
	t.Helper()
	return &is04.RegistrationRequest{Type: rt, Data: mustJSONBytes(t, v)}
}

// IngestRegistration decodes an envelope of any type and writes it
// through the same referential rules as the typed Put.
func TestIngestRegistrationEveryType(t *testing.T) {
	s := NewStore()
	for _, env := range []*is04.RegistrationRequest{
		envelope(t, is04.ResourceNode, validNode(fxNode)),
		envelope(t, is04.ResourceDevice, validDevice(fxDevice, fxNode)),
		envelope(t, is04.ResourceSource, validSource(fxSource, fxDevice)),
		envelope(t, is04.ResourceFlow, validFlow(fxFlow, fxSource, fxDevice)),
		envelope(t, is04.ResourceSender, validSender(fxSender, fxDevice)),
		envelope(t, is04.ResourceReceiver, validReceiver(fxReceiver, fxDevice)),
	} {
		if err := s.IngestRegistration(env); err != nil {
			t.Fatalf("Ingest %s: %v", env.Type, err)
		}
	}
	if len(s.ListReceivers()) != 1 || len(s.ListFlows()) != 1 {
		t.Error("the whole chain must be stored")
	}

	// A type IS-04 doesn't define is refused.
	if err := s.IngestRegistration(envelope(t, is04.ResourceType("gizmo"), validNode(fxNode))); err == nil ||
		!strings.Contains(err.Error(), "invalid resource type") {
		t.Errorf("unknown envelope type = %v", err)
	}
}

// A `data` that is not an object, and one missing a resource_core key,
// are both refused before any typed decode — that is the HTTP 400 the
// AMWA tool's do_400_check family asserts.
func TestIngestRegistrationPresenceRules(t *testing.T) {
	s := NewStore()
	notAnObject := &is04.RegistrationRequest{Type: is04.ResourceNode, Data: json.RawMessage(`[]`)}
	if err := s.IngestRegistration(notAnObject); err == nil ||
		!strings.Contains(err.Error(), "not a JSON object") {
		t.Errorf("array body = %v", err)
	}

	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(mustJSONBytes(t, validNode(fxNode)), &raw); err != nil {
		t.Fatal(err)
	}
	delete(raw, "label")
	env := &is04.RegistrationRequest{Type: is04.ResourceNode, Data: mustJSONBytes(t, raw)}
	if err := s.IngestRegistration(env); err == nil || !strings.Contains(err.Error(), "label") {
		t.Errorf("node without a label = %v", err)
	}

	// v1.0 predates `tags` and `description`, so the same body without
	// them is legal on that minor and rejected on v1.3.
	raw["label"] = json.RawMessage(`"n"`)
	delete(raw, "tags")
	delete(raw, "description")
	env = &is04.RegistrationRequest{Type: is04.ResourceNode, Data: mustJSONBytes(t, raw)}
	if err := s.IngestRegistrationVersioned(env, "v1.0"); err != nil {
		t.Errorf("v1.0 body without tags/description: %v", err)
	}
	if err := s.IngestRegistrationVersioned(env, "v1.3"); err == nil ||
		!strings.Contains(err.Error(), "required (resource_core)") {
		t.Errorf("v1.3 body without description/tags = %v", err)
	}
}

// Every typed decode failure is reported against its own type, and
// leaves no api_ver stamp behind for the retry.
func TestIngestRegistrationDecodeFailures(t *testing.T) {
	s := NewStore()
	core := `"id":"` + fxNode + `","version":"0:0","label":"x","description":"d","tags":{}`
	for _, tc := range []struct {
		rt   is04.ResourceType
		body string
		want string
	}{
		{is04.ResourceNode, `{` + core + `,"href":9}`, "decode node"},
		{is04.ResourceDevice, `{` + core + `,"node_id":9}`, "decode device"},
		{is04.ResourceSource, `{` + core + `,"device_id":9}`, "decode source"},
		{is04.ResourceFlow, `{` + core + `,"source_id":9}`, "decode flow"},
		{is04.ResourceSender, `{` + core + `,"device_id":9}`, "decode sender"},
		{is04.ResourceReceiver, `{` + core + `,"device_id":9}`, "decode receiver"},
	} {
		env := &is04.RegistrationRequest{Type: tc.rt, Data: json.RawMessage(tc.body)}
		if err := s.IngestRegistrationVersioned(env, "v1.3"); err == nil ||
			!strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s decode failure = %v, want %q", tc.rt, err, tc.want)
		}
		if got := s.APIVerOf(tc.rt, fxNode); got != "" {
			t.Errorf("a failed ingest left an api_ver stamp on %s: %q", tc.rt, got)
		}
	}
}

// IS-04 §6.1.1: a resource already registered at one minor cannot be
// re-registered at another — the handler turns this into HTTP 409.
func TestIngestRegistrationAPIVerConflict(t *testing.T) {
	s := NewStore()
	node := envelope(t, is04.ResourceNode, validNode(fxNode))
	if err := s.IngestRegistrationVersioned(node, "v1.2"); err != nil {
		t.Fatal(err)
	}
	if got := s.APIVerOf(is04.ResourceNode, fxNode); got != "v1.2" {
		t.Errorf("APIVerOf = %q, want v1.2", got)
	}
	if err := s.IngestRegistrationVersioned(node, "v1.3"); !errors.Is(err, ErrAPIVerConflict) {
		t.Errorf("re-register at another minor = %v, want ErrAPIVerConflict", err)
	}
	if err := s.IngestRegistrationVersioned(node, "v1.2"); err != nil {
		t.Errorf("re-register at the same minor = %v", err)
	}
	// An unversioned ingest is version-agnostic, and restamps nothing.
	if err := s.IngestRegistrationVersioned(node, ""); err != nil {
		t.Errorf("unversioned ingest = %v", err)
	}
	if got := s.APIVerOf(is04.ResourceNode, fxNode); got != "v1.2" {
		t.Errorf("an unversioned ingest must not restamp: %q", got)
	}

	// A refused resource leaves no stamp behind, so the same document
	// registers cleanly at any minor once its parent exists.
	orphan := envelope(t, is04.ResourceDevice, validDevice(fxDevice, fxAbsent))
	if err := s.IngestRegistrationVersioned(orphan, "v1.3"); err == nil {
		t.Fatal("a device whose node is absent must be refused")
	}
	if got := s.APIVerOf(is04.ResourceDevice, fxDevice); got != "" {
		t.Errorf("the api_ver stamp was not rolled back: %q", got)
	}
	if err := s.IngestRegistrationVersioned(
		envelope(t, is04.ResourceDevice, validDevice(fxDevice, fxNode)), "v1.0"); err != nil {
		t.Errorf("re-registering after the rollback: %v", err)
	}
}

// markAPIVer with an empty version clears the stamp, which is how a
// test-populated resource becomes visible on every minor.
func TestStoreMarkAPIVerClears(t *testing.T) {
	s := populated(t)
	s.mu.Lock()
	s.markAPIVer(is04.ResourceNode, fxNode, "v1.1")
	s.mu.Unlock()
	if got := s.APIVerOf(is04.ResourceNode, fxNode); got != "v1.1" {
		t.Fatalf("APIVerOf = %q", got)
	}
	s.mu.Lock()
	s.markAPIVer(is04.ResourceNode, fxNode, "")
	s.markAPIVer(is04.ResourceSender, fxSender, "") // never stamped: a no-op
	s.mu.Unlock()
	if got := s.APIVerOf(is04.ResourceNode, fxNode); got != "" {
		t.Errorf("cleared stamp = %q, want empty", got)
	}
}

// HealthFor answers with the last heartbeat, and ErrNotFound for a
// Node the registry doesn't hold.
func TestStoreHealthForUnknownNode(t *testing.T) {
	s := populated(t)
	if _, err := s.HealthFor(fxNode); err != nil {
		t.Errorf("HealthFor = %v", err)
	}
	if _, err := s.HealthFor(fxAbsent); !errors.Is(err, ErrNotFound) {
		t.Errorf("HealthFor(absent) = %v, want ErrNotFound", err)
	}
}
