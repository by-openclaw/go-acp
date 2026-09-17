package rollcall

import (
	"context"
	"strings"
	"testing"

	"dhs/internal/snell-rollcall/codec"
)

// The user level is the whole session's: a unit hides menu lines above it and
// refuses a factory-gated write below factory, and blind control cannot reach
// factory at all. So it is chosen before connecting, sent in every call, and
// part of the key a walk is filed under.

func TestSessionsAreOpenedAtTheChosenLevel(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.identity[1] = codec.ID{
			TypeID:  562,
			Version: codec.Version{Major: 5, Minor: 0, Alpha: ' ', CmdSet: 5},
			Name:    "EMB.06 (Nodal)",
		}
	})
	if err := h.plugin.SetUserLevel(codec.LevelFactory); err != nil {
		t.Fatalf("SetUserLevel: %v", err)
	}

	key, err := h.plugin.IdentityProbe(context.Background(), 1)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}

	// The call carried factory, and the walk is filed as a factory walk.
	h.device.mu.Lock()
	levels := append([]codec.UserLevel(nil), h.device.levelsSeen...)
	h.device.mu.Unlock()
	if len(levels) == 0 {
		t.Fatal("no call reached the device")
	}
	for _, lv := range levels {
		if lv != codec.LevelFactory {
			t.Errorf("a session was called at %s, want factory", lv)
		}
	}
	if !strings.HasSuffix(key, "@factory") {
		t.Errorf("a factory walk is filed as %q, want the level in the key", key)
	}
}

func TestTheDefaultLevelIsSupervisorAndKeysArePlain(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.identity[1] = codec.ID{
			TypeID:  562,
			Version: codec.Version{Major: 5, Minor: 0, Alpha: ' ', CmdSet: 5},
		}
	})
	key, err := h.plugin.IdentityProbe(context.Background(), 1)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}
	if key != "IQDBE00@5.0.cs5" {
		t.Errorf("a supervisor walk is filed as %q, want the plain key every DM on disk carries", key)
	}
	h.device.mu.Lock()
	defer h.device.mu.Unlock()
	for _, lv := range h.device.levelsSeen {
		if lv != codec.LevelSupervisor {
			t.Errorf("a session was called at %s, want supervisor by default", lv)
		}
	}
}

func TestALevelNoClientMayRequestIsRefused(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.plugin.SetUserLevel(codec.LevelAll); err == nil {
		t.Error("UL_ALL, a mask, was accepted as a session level")
	}
	if got := h.plugin.userLevel(); got != codec.LevelSupervisor {
		t.Errorf("the level changed to %s on a refused request", got)
	}
}
