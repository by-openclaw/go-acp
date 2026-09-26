//go:build !linux && !windows && !darwin

package lldp

import (
	"context"
	"time"
)

// Capture is the no-capture build of the capture Source, for every OS
// without a capture path of its own (Linux: AF_PACKET, macOS: BPF, Windows:
// Npcap).
//
// It exists with the same shape on every OS so callers wire it identically
// and branch on the ERROR, not on runtime.GOOS. A build-tag check scattered
// through calling code is how platform divergence becomes invisible.
type Capture struct {
	// Iface limits capture to one interface. Empty means every interface.
	Iface string
	// Window is how long Neighbors waits for frames. Zero means 2s.
	Window time.Duration
	// Until is accepted for the same shape as every other build.
	Until func(map[string]Neighbor) bool
}

// Neighbors always fails with [ErrCaptureUnsupported].
//
// It returns the typed error rather than an empty map on purpose: silently
// reporting "no neighbours" on a host that is structurally incapable of
// hearing any would tell an operator their switch is misconfigured when the
// truth is that this platform never looked.
func (Capture) Neighbors(context.Context) (map[string]Neighbor, error) {
	return nil, ErrCaptureUnsupported
}

// Supported reports whether this build can capture LLDP locally.
func Supported() bool { return false }
