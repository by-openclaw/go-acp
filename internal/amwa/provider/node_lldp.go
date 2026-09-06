package provider

import (
	"context"
	"errors"
	"time"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/lldp"
)

// lldpResolveTimeout bounds the neighbour lookup during Serve.
//
// Serve must not stall on it: a capture Source waits for frames that arrive
// on the SENDER's schedule (30s by default), and a Node that refused to start
// until a switch spoke would be down for half a minute after every restart.
// An unanswered lookup leaves attached_network_device unset, which is what
// the field means when the Node does not know.
const lldpResolveTimeout = 3 * time.Second

// applyLLDPLocked fills interfaces[].attached_network_device from the injected
// LLDP source.
//
// IS-04 v1.3 defines that object as "the Chassis ID … as signalled in LLDP
// received by this Node", so it can only come from outside — the Node cannot
// know which switch port it is patched into by looking at itself. The source
// is injected rather than constructed here: a device that reports its own LLDP
// over an API is as valid as a local capture, and on Windows it is the only
// option there is.
//
// Never fatal. ADR-0016's stdlib floor says a missing host capability degrades
// a field, it does not stop the Node — a plant where LLDP is switched off must
// still get a registered, routable Node.
//
// Caller holds s.mu.
func (s *IS04NodeServer) applyLLDPLocked(ctx context.Context) {
	if s.cfg.LLDP == nil || len(s.bundle.Node.Interfaces) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, lldpResolveTimeout)
	defer cancel()

	seen, err := s.cfg.LLDP.Neighbors(ctx)
	if err != nil {
		// Not being able to capture is a property of the platform, not a
		// fault: logging it at Warn on every Windows start would train
		// operators to ignore the level that matters.
		if errors.Is(err, lldp.ErrCaptureUnsupported) {
			s.logger.Debug("provider/node: no local LLDP capture on this platform; "+
				"attached_network_device unset unless another source supplies it",
				"err", err)
		} else {
			s.logger.Warn("provider/node: LLDP lookup failed; "+
				"attached_network_device left unset", "err", err)
		}
		// A partial result is still worth using: a Cache serves its stale
		// view alongside the error rather than nothing.
		if len(seen) == 0 {
			return
		}
	}

	filled := 0
	for i := range s.bundle.Node.Interfaces {
		iface := &s.bundle.Node.Interfaces[i]
		n, ok := seen[iface.Name]
		if !ok {
			continue
		}
		// Both members are REQUIRED by the schema once the object exists,
		// so a neighbour missing either is skipped entirely. Emitting a
		// half-filled object would fail validation for every reader.
		if n.ChassisID == "" || n.PortID == "" {
			s.logger.Debug("provider/node: LLDP neighbour lacks a required id; skipping",
				"interface", iface.Name, "chassis_id", n.ChassisID, "port_id", n.PortID)
			continue
		}
		iface.AttachedNetworkDevice = &is04.AttachedNetworkDevice{
			ChassisID: n.ChassisID,
			PortID:    n.PortID,
		}
		filled++
		s.logger.Info("provider/node: attached network device from LLDP",
			"interface", iface.Name, "chassis_id", n.ChassisID, "port_id", n.PortID,
			"switch", n.SysName, "port", n.PortDesc)
	}
	if filled == 0 && len(seen) > 0 {
		// The source saw neighbours but on interface names this Node does
		// not list — almost always a bundle whose interface names do not
		// match the host's. Silence here would look like "no LLDP".
		s.logger.Warn("provider/node: LLDP reported neighbours on interfaces this Node does not declare",
			"lldp_interfaces", keysOf(seen), "node_interfaces", ifaceNames(s.bundle.Node.Interfaces))
	}
}

func keysOf(m map[string]lldp.Neighbor) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func ifaceNames(ifs []is04.NodeIface) []string {
	out := make([]string, 0, len(ifs))
	for _, i := range ifs {
		out = append(out, i.Name)
	}
	return out
}
