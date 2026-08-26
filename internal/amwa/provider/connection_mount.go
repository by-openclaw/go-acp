// Layer-3 — joining the IS-05 Connection API to the IS-04 Node API.
//
// The two APIs are not independent services that happen to share a
// host. IS-05 §4.1 requires the resource ids to match, and IS-04
// §4.2 requires each Device to ADVERTISE its connection API in
// `controls[]`. A controller finds IS-05 by reading IS-04 — it never
// guesses the URL — so a Connection API that is served but not
// advertised is, to every controller, absent.
//
// That advertisement is what IS-05-02 ("Interaction with IS-04")
// exists to check.

package provider

import (
	"context"
	"time"

	"dhs/internal/amwa/codec/is04"
	httpsession "dhs/internal/amwa/session/http"
)

// controlTypeSRCtrl is the control URN a Device uses to point at its
// Connection API. The version suffix is the IS-05 minor, so a Device
// serving several minors carries one control entry per minor — that
// is how a v1.0-only controller and a v1.2 controller each find a
// tree they can speak.
const controlTypeSRCtrl = "urn:x-nmos:control:sr-ctrl/"

// attachConnectionAPI mounts the Connection API and advertises it on
// every Device in the bundle.
func (s *IS04NodeServer) attachConnectionAPI(srv *httpsession.Server) {
	if s.connection == nil {
		return
	}
	s.connection.Mount(srv)
	s.advertiseConnectionControls()
}

// advertiseConnectionControls adds one sr-ctrl entry per served IS-05
// minor to every Device.
//
// Existing controls are preserved and ours are added idempotently: a
// bundle may already declare controls for APIs we do not serve (IS-07,
// IS-08, a vendor URN), and silently replacing them would remove
// capability the operator declared on purpose.
func (s *IS04NodeServer) advertiseConnectionControls() {
	host := s.cfg.AdvertiseHost
	if host == "" {
		host = "localhost"
	}
	for i := range s.bundle.Devices {
		d := &s.bundle.Devices[i]
		for _, ver := range s.connection.Versions() {
			ctrl := is04.DeviceControl{
				Type: controlTypeSRCtrl + ver,
				Href: "http://" + host + "/x-nmos/connection/" + ver + "/",
			}
			if !hasControl(d.Controls, ctrl.Type) {
				d.Controls = append(d.Controls, ctrl)
			}
		}
	}
}

// ConnectionVersions lists the IS-05 minors this Node serves, or nil
// when the Connection API is disabled.
func (s *IS04NodeServer) ConnectionVersions() []string {
	if s.connection == nil {
		return nil
	}
	return s.connection.Versions()
}

func hasControl(controls []is04.DeviceControl, typ string) bool {
	for _, c := range controls {
		if c.Type == typ {
			return true
		}
	}
	return false
}

// runActivationScheduler promotes scheduled activations as their times
// arrive.
//
// The tick is what makes a coordinated switch real: several devices
// are staged, each given the same absolute TAI instant, and every one
// flips without further traffic. Without a scheduler the endpoint
// would accept a scheduled PATCH, answer 202, and then never act —
// the worst of the three possible behaviours, because it looks
// correct.
func (s *IS04NodeServer) runActivationScheduler(ctx context.Context, tick time.Duration) {
	if s.connection == nil {
		return
	}
	if tick <= 0 {
		tick = 100 * time.Millisecond
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n := s.connection.Store().runScheduled(); n > 0 {
				s.logger.Info("scheduled activation fired",
					"plugin", "amwa", "api", "is-05", "endpoints", n)
			}
		}
	}
}
