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
	"dhs/internal/amwa/codec/is05"
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
	// A channel re-map bumps every Device's IS-04 version. IS-04 §5
	// makes `version` how a controller learns anything changed, and a
	// Device whose version stands still through a re-map tells every
	// cached controller that nothing happened.
	s.channelMapping.onActivate = s.bumpDeviceVersions
	s.channelMapping.Mount(srv)
	host := s.controlHost()
	for i := range s.bundle.Devices {
		d := &s.bundle.Devices[i]
		for _, ver := range s.channelMapping.Versions() {
			ctrl := is04.DeviceControl{
				Type: controlTypeChannelMapping + ver,
				Href: "http://" + host + "/x-nmos/channelmapping/" + ver + "/",
			}
			upsertControl(&d.Controls, ctrl)
		}
	}
}

// controlTypeStreamCompat is the control URN a Device uses to point
// at its Stream Compatibility Management API (IS-11
// docs/Interoperability.md "Discovery").
const controlTypeStreamCompat = "urn:x-nmos:control:stream-compat/"

// attachStreamCompatAPI mounts IS-11 and advertises it on every
// Device, wiring the three cross-API duties Interoperability.md
// assigns: the sender-active gate for 423s, the Sender version bump
// on Active Constraints changes, and the Device version bump on
// Input/Output changes.
func (s *IS04NodeServer) attachStreamCompatAPI(srv *httpsession.Server) {
	if s.streamCompat == nil {
		return
	}
	if s.connection != nil {
		conn := s.connection
		s.streamCompat.SetSenderActiveFunc(func(id string) bool {
			e, err := conn.Store().get("senders", id)
			return err == nil && e.active.MasterEnable
		})
	}
	s.streamCompat.onSenderConstraintsChanged = s.bumpSenderVersion
	s.streamCompat.onDeviceChanged = func(string) { s.bumpDeviceVersions() }
	s.streamCompat.Mount(srv)
	host := s.controlHost()
	for i := range s.bundle.Devices {
		d := &s.bundle.Devices[i]
		for _, ver := range s.streamCompat.Versions() {
			ctrl := is04.DeviceControl{
				Type: controlTypeStreamCompat + ver,
				Href: "http://" + host + "/x-nmos/streamcompatibility/" + ver + "/",
			}
			upsertControl(&d.Controls, ctrl)
		}
	}
}

// bumpSenderVersion stamps a fresh IS-04 version on one Sender and
// queues it for re-registration — how an Active Constraints change
// reaches every Query API consumer (IS-11 Interoperability.md).
func (s *IS04NodeServer) bumpSenderVersion(id string) {
	if s.bundle == nil {
		return
	}
	for i := range s.bundle.Senders {
		if s.bundle.Senders[i].ID != id {
			continue
		}
		s.bundle.Senders[i].Version = is05.FormatTAINow(time.Now())
		if s.connection != nil {
			snap := s.bundle.Senders[i]
			s.connection.notifyChanged(is04.ResourceSender, &snap)
		}
		return
	}
}

// upsertControl adds ctrl, or REPLACES an existing control of the same
// Type with the fresh href. The old skip-if-present rule is how a
// stale href from a bundle file survived every restart: the fixture
// carried controls minted under a decommissioned address, the type
// matched, and the fresh reachable href was never written — so the
// device kept advertising a Connection API at a host that no longer
// exists. Controls are OURS to mint at attach time; a bundle's copy is
// at best yesterday's.
func upsertControl(controls *[]is04.DeviceControl, ctrl is04.DeviceControl) {
	for i := range *controls {
		if (*controls)[i].Type == ctrl.Type {
			(*controls)[i] = ctrl
			return
		}
	}
	*controls = append(*controls, ctrl)
}

// controlHost is the host:port a control href should name -- the
// address the Node actually answers on, falling back to the advertised
// name. See advertiseConnectionControls for why the IP wins.
func (s *IS04NodeServer) controlHost() string {
	host := s.cfg.AdvertiseHost
	if host == "" {
		host = "localhost"
	}
	// The operator's --advertise-host is authoritative when it is an
	// IP literal: it satisfies the IP-wins rule below AND cannot be
	// poisoned by stale endpoints riding in from a bundle file. That
	// poisoning is silent and expensive — every control href points
	// at a dead address, and the controller (Cerebrum) just shows
	// empty IS-05 panels with no error anywhere.
	if ah, port := splitHostPort(s.cfg.AdvertiseHost, s.cfg.Bind); ah != "" && net.ParseIP(ah) != nil {
		if port > 0 {
			return net.JoinHostPort(ah, strconv.Itoa(port))
		}
		return ah
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
			upsertControl(&d.Controls, ctrl)
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

// bumpDeviceVersions stamps every Device with a new IS-04 version.
//
// Called from inside the Channel Mapping server's lock, so it must not
// call back into it.
func (s *IS04NodeServer) bumpDeviceVersions() {
	now := is05.FormatTAINow(time.Now())
	for i := range s.bundle.Devices {
		s.bundle.Devices[i].Version = now
	}
	// The peer-to-peer TXT counter follows, so a Mode-D peer watching
	// mDNS re-fetches rather than waiting for a poll it never makes.
	s.verDevice.Add(1)
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
