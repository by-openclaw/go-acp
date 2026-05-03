package registry

import (
	"sync"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
)

func validNode(id string) is04.Node {
	chassis := "ff-ff-ff-ff-ff-ff"
	return is04.Node{
		ResourceCore: is04.ResourceCore{
			ID: id, Version: "0:0",
			Label: "n", Description: "x", Tags: map[string][]string{},
		},
		Href: "http://10.6.239.113:8080/",
		Caps: map[string]any{},
		API: is04.NodeAPI{
			Versions:  []string{"v1.3"},
			Endpoints: []is04.NodeEndpoint{{Host: "10.6.239.113", Port: 8080, Protocol: "http"}},
		},
		Services: []is04.NodeService{},
		Clocks:   []is04.NodeClock{{Name: "clk0", RefType: "internal"}},
		Interfaces: []is04.NodeIface{
			{ChassisID: &chassis, PortID: "ff-ff-ff-ff-ff-ff", Name: "eth0"},
		},
	}
}

func validDevice(id, nodeID string) is04.Device {
	return is04.Device{
		ResourceCore: is04.ResourceCore{
			ID: id, Version: "0:0",
			Label: "d", Description: "x", Tags: map[string][]string{},
		},
		Type:      "urn:x-nmos:device:generic",
		NodeID:    nodeID,
		Senders:   []string{},
		Receivers: []string{},
		Controls:  []is04.DeviceControl{},
	}
}

func TestStorePutGetNode(t *testing.T) {
	s := NewStore()
	n := validNode("f47ac10b-58cc-4372-a567-0e02b2c3d479")
	if err := s.PutNode(n); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	got, err := s.GetNode(n.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.ID != n.ID {
		t.Fatalf("id mismatch")
	}
	if len(s.ListNodes()) != 1 {
		t.Fatalf("ListNodes: %v", s.ListNodes())
	}
}

func TestStoreDeviceRequiresNode(t *testing.T) {
	s := NewStore()
	d := validDevice("12345678-1234-4abc-9def-1234567890ab", "f47ac10b-58cc-4372-a567-0e02b2c3d479")
	if err := s.PutDevice(d); err == nil {
		t.Fatal("expected ref-integrity error: no parent node")
	}
}

func TestStoreCascadeDelete(t *testing.T) {
	s := NewStore()
	n := validNode("f47ac10b-58cc-4372-a567-0e02b2c3d479")
	if err := s.PutNode(n); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	d := validDevice("12345678-1234-4abc-9def-1234567890ab", n.ID)
	if err := s.PutDevice(d); err != nil {
		t.Fatalf("PutDevice: %v", err)
	}
	if len(s.ListDevices()) != 1 {
		t.Fatalf("expected 1 device pre-delete")
	}

	s.DeleteNode(n.ID)
	if len(s.ListNodes()) != 0 || len(s.ListDevices()) != 0 {
		t.Fatalf("cascade delete failed: nodes=%v devices=%v", s.ListNodes(), s.ListDevices())
	}
}

func TestStoreHeartbeat(t *testing.T) {
	s := NewStore()
	n := validNode("f47ac10b-58cc-4372-a567-0e02b2c3d479")
	if err := s.PutNode(n); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	if err := s.Heartbeat(n.ID); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if err := s.Heartbeat("not-a-real-id"); err == nil {
		t.Fatal("expected ErrNotFound for unknown id")
	}
}

func TestStoreEvictStale(t *testing.T) {
	s := NewStore()
	n := validNode("f47ac10b-58cc-4372-a567-0e02b2c3d479")
	if err := s.PutNode(n); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	// Force last-seen into the past.
	s.mu.Lock()
	s.health[n.ID] = time.Now().Add(-30 * time.Second)
	s.mu.Unlock()
	if got := s.EvictStale(12 * time.Second); got != 1 {
		t.Fatalf("EvictStale = %d, want 1", got)
	}
	if len(s.ListNodes()) != 0 {
		t.Fatalf("post-evict nodes = %v", s.ListNodes())
	}
}

func TestStoreEmitsChangeEvents(t *testing.T) {
	s := NewStore()
	var (
		mu      sync.Mutex
		changes []Change
	)
	s.AddListener(func(c Change) {
		mu.Lock()
		changes = append(changes, c)
		mu.Unlock()
	})
	n := validNode("f47ac10b-58cc-4372-a567-0e02b2c3d479")
	_ = s.PutNode(n) // created
	n.Label = "n2"
	_ = s.PutNode(n) // updated
	s.DeleteNode(n.ID) // deleted

	mu.Lock()
	defer mu.Unlock()
	kinds := make([]ChangeKind, len(changes))
	for i, c := range changes {
		kinds[i] = c.Kind
	}
	want := []ChangeKind{ChangeCreated, ChangeUpdated, ChangeDeleted}
	if len(kinds) != len(want) {
		t.Fatalf("emitted kinds = %v, want %v", kinds, want)
	}
	for i, k := range want {
		if kinds[i] != k {
			t.Fatalf("emitted[%d] = %s, want %s", i, kinds[i], k)
		}
	}
}

func TestStoreSnapshotChanges(t *testing.T) {
	s := NewStore()
	_ = s.PutNode(validNode("f47ac10b-58cc-4372-a567-0e02b2c3d479"))
	snaps := s.SnapshotChanges("")
	if len(snaps) != 1 {
		t.Fatalf("snap = %v", snaps)
	}
	if snaps[0].Kind != ChangeSync {
		t.Fatalf("snap kind = %s", snaps[0].Kind)
	}
}
