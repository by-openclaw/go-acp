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
	"net"
	"strconv"
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

// controlTypeChannelMapping is the control URN a Device uses to point
// at its Channel Mapping API.
const controlTypeChannelMapping = "urn:x-nmos:control:cm-ctrl/"

// attachChannelMappingAPI mounts IS-08 and advertises it the same way
// IS-05 is advertised -- a Channel Mapping API that is served but not
// named in device.controls[] is invisible to every controller.
func (s *IS04NodeServer) attachChannelMappingAPI(srv *httpsession.Server) {
	if s.channelMapping == nil {
		return
	}
	s.channelMapping.Mount(srv)
	host := s.controlHost()
	for i := range s.bundle.Devices {
		d := &s.bundle.Devices[i]
		for _, ver := range s.channelMapping.Versions() {
			ctrl := is04.DeviceControl{
				Type: controlTypeChannelMapping + ver,
				Href: "http://" + host + "/x-nmos/channelmapping/" + ver + "/",
			}
			if !hasControl(d.Controls, ctrl.Type) {
				d.Controls = append(d.Controls, ctrl)
			}
		}
	}
}

// controlHost is the host:port a control href should name -- the
// address the Node actually answers on, falling back to the advertised
// name. See advertiseConnectionControls for why the IP wins.
func (s *IS04NodeServer) controlHost() string {
	host := s.cfg.AdvertiseHost
	if host == "" {
		host = "localhost"
	}
	if ip := firstNodeIP(s.bundle); ip != "" {
		if _, port := splitHostPort(s.cfg.AdvertiseHost, s.cfg.Bind); port > 0 {
			return net.JoinHostPort(ip, strconv.Itoa(port))
		}
		return ip
	}
	return host
}

// advertiseConnectionControls adds one sr-ctrl entry per served IS-05
// minor to every Device.
//
// Existing controls are preserved and ours are added idempotently: a
// bundle may already declare controls for APIs we do not serve (IS-07,
// IS-08, a vendor URN), and silently replacing them would remove
// capability the operator declared on purpose.
func (s *IS04NodeServer) advertiseConnectionControls() {
	// The control href names the ADDRESS the Node is reachable on, not
	// the name it was started with.
	//
	// A controller matches this href against the Connection API it is
	// talking to. When the Node is launched as `--advertise-host
	// dhs-node:18080` the href says "dhs-node", the controller reached
	// us at an IP, and the two do not compare equal -- so the Device
	// looks like it advertises somebody else's Connection API and the
	// controller reports no sr-ctrl control at all (IS-05-02 test_02).
	// The Node's own published endpoint list is the address it really
	// answers on, so prefer that and keep the advertised port.
	host := s.controlHost()
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
	if s.connection == nil && s.channelMapping == nil {
		return
	}
	if tick <= 0 {
		// 20ms, not 100.
		//
		// The tick is the worst-case lateness of every scheduled
		// switch, and IS-05-01 test_27 schedules 200ms out and checks
		// the result within 200ms of that. At a 100ms tick half the
		// runs land outside the window -- not a failure, but a warning
		// that the device is slower than asked, which for a
		// frame-accurate switch is the thing being measured.
		tick = 20 * time.Millisecond
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s.connection != nil {
				if n := s.connection.Store().runScheduled(); n > 0 {
					s.logger.Info("scheduled activation fired",
						"plugin", "amwa", "api", "is-05", "endpoints", n)
				}
			}
			// IS-08 queues scheduled re-maps the same way and needs
			// the same pump. One ticker drives both: two would drift
			// against each other, and a controller that schedules a
			// route and a channel map for the same instant expects
			// them to land together.
			if s.channelMapping != nil {
				if n := s.channelMapping.runActivations(); n > 0 {
					s.logger.Info("scheduled channel map fired",
						"plugin", "amwa", "api", "is-08", "activations", n)
				}
			}
		}
	}
}
