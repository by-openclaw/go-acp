package cerebrumnb

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"dhs/internal/cerebrum-nb/codec"
)

// This file adds the §4 write-action Session methods that wrap the codec
// action encoders. Each is a thin, typed convenience over Session.Action
// (ACK/NACK round-trip) so the CLI write verbs in cmd/dhs read uniformly.
//
// Scope note (0v16 codec on disk): every method below maps onto an action
// body the codec actually encodes today — RoutingAction (§4.1),
// CategoryAction (§4.2), SalvoAction (§4.3), DeviceAction (§4.4), plus the
// 0v16 top-level DeviceConfiguration (§4.5). The codec's LockKind enum now
// carries the full §3.2 five-value set (UNLOCKED / LOCKED / PROTECTED /
// LOCKED_PATH / PROTECTED_PATH) alongside the deprecated PROTECT / RELEASE
// action verbs; Lock takes any of them. DeviceAction still carries only
// Type/DeviceName/SubDevice/Object/Value — it does NOT expose IS_ENUM or a
// MODE (SET/ADD_TAIL/INSERT_AT_HEAD/REMOVE) field, so the 0v16 SET_VALUE
// enum/mode surface is a codec gap and is not wired here (see DeviceConfig
// below + the gap note returned to the caller).

// Lock applies a routing lock. routingType selects SRCE_LOCK / DEST_LOCK
// per spec §4.1; mode is the LockKind (PROTECT to lock, RELEASE to clear).
// addr identifies the router target (IPAddress "0.0.0.0" + DEVICE_TYPE
// "ROUTER" is the route-master sentinel) and the source/destination + level
// addressing. duration is optional (timed locks).
func (s *Session) Lock(ctx context.Context, routingType string, mode codec.LockKind, addr RouteTarget, srceID, destID, levelID, duration string) error {
	body := &codec.RoutingAction{
		Type:       routingType,
		IPAddress:  addr.IPAddress,
		DeviceName: addr.DeviceName,
		DeviceType: addr.DeviceType,
		SrceID:     srceID,
		DestID:     destID,
		LevelID:    levelID,
		Lock:       mode,
		Duration:   duration,
	}
	return s.Action(ctx, body)
}

// SetMnemonic writes a mnemonic via a §4.1 routing MNE action. mneType is
// one of LEVEL_MNE / SRCE_MNE / DEST_MNE. The mnemonic string goes in
// MNEMONIC; altSlot (optional) goes in ALT_MNE to address an alternate
// mnemonic slot per §4.1.4-§4.1.6.
func (s *Session) SetMnemonic(ctx context.Context, mneType string, addr RouteTarget, srceID, destID, levelID, mnemonic, altSlot string) error {
	body := &codec.RoutingAction{
		Type:       mneType,
		IPAddress:  addr.IPAddress,
		DeviceName: addr.DeviceName,
		DeviceType: addr.DeviceType,
		SrceID:     srceID,
		DestID:     destID,
		LevelID:    levelID,
		Mnemonic:   mnemonic,
		AltMne:     altSlot,
		// An explicitly empty mnemonic is the CLEAR form: force MNEMONIC=""
		// onto the wire (callers gate this behind --allow-clear).
		EmptyMnemonic: mnemonic == "",
	}
	return s.Action(ctx, body)
}

// SetTags removes tags from a source or destination via the §4.1
// RM_SRCE_TAGS / RM_DEST_TAGS routing action. tagsType selects which.
// The spec only documents tag REMOVAL on the routing path; tags carries
// the comma-separated tag list. federation sends the change to the
// federation source / destination instead of the local one (v0.17
// §4.2.1-§4.2.2 FEDERATION="1"; absent = local, as before v0.17).
func (s *Session) SetTags(ctx context.Context, tagsType string, addr RouteTarget, srceID, destID, levelID, tags string, federation bool) error {
	body := &codec.RoutingAction{
		Type:       tagsType,
		IPAddress:  addr.IPAddress,
		DeviceName: addr.DeviceName,
		DeviceType: addr.DeviceType,
		SrceID:     srceID,
		DestID:     destID,
		LevelID:    levelID,
		Tags:       tags,
		Federation: federationAttr(federation),
	}
	return s.Action(ctx, body)
}

// federationAttr is the FEDERATION attribute of the ASSOC and RM_*_TAGS
// actions: "1" when the change targets the federation IO, absent when
// local, so a pre-v0.17 server sees the frame it always saw.
func federationAttr(on bool) string {
	if on {
		return "1"
	}
	return ""
}

// flag01 is the explicit "0" / "1" form the v0.17 §4.2.3-§4.2.4 examples
// use for VIRTUAL and FEDERATION on the RM_* add and delete actions.
func flag01(on bool) string {
	if on {
		return "1"
	}
	return "0"
}

// RMKind selects the RouteMaster collection of the v0.17 §4.2.3-§4.2.4
// RM_* actions: sources (SRCE) or destinations (DEST).
type RMKind string

const (
	RMSources      RMKind = "SRCE"
	RMDestinations RMKind = "DEST"
)

// RMAdd issues RM_SRCE_ADD / RM_DEST_ADD (v0.17 §4.2.3): count IOs
// (1-1000) appended to the RouteMaster, virtual ones when virtual is set,
// in the federation when federation is set. The reply is the §4.2.5
// <ROUTING><RESULT VALUE="ACCEPTED|FAILED"/></ROUTING> envelope: ACCEPTED
// means validated and queued, not applied — the caller reads the
// collection back. A server older than v0.17 answers a plain ACK, which
// is reported as accepted.
func (s *Session) RMAdd(ctx context.Context, kind RMKind, count int, virtual, federation bool) (*codec.RoutingResult, error) {
	if count < 1 || count > 1000 {
		return nil, fmt.Errorf("cerebrum-nb: rm-add: count %d outside 1-1000", count)
	}
	body := &codec.RoutingAction{
		Type:       "RM_" + string(kind) + "_ADD",
		Count:      strconv.Itoa(count),
		Virtual:    flag01(virtual),
		Federation: flag01(federation),
	}
	return s.routingResult(ctx, "rm-add", body)
}

// RMDelete issues RM_SRCE_DELETE / RM_DEST_DELETE (v0.17 §4.2.4) for the
// given existing numeric ids (the server rejects empty elements and 0),
// in the federation when federation is set.
func (s *Session) RMDelete(ctx context.Context, kind RMKind, ids []string, federation bool) (*codec.RoutingResult, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("cerebrum-nb: rm-delete: no id given")
	}
	body := &codec.RoutingAction{
		Type:       "RM_" + string(kind) + "_DELETE",
		Federation: flag01(federation),
	}
	list := strings.Join(ids, ",")
	if kind == RMSources {
		body.SrceID = list
	} else {
		body.DestID = list
	}
	return s.routingResult(ctx, "rm-delete", body)
}

// routingResult sends one RM_* action and returns its §4.2.5 result.
func (s *Session) routingResult(ctx context.Context, verb string, body codec.ActionBody) (*codec.RoutingResult, error) {
	mtid := s.nextMTID()
	f, err := s.roundTrip(ctx, mtid, codec.EncodeAction(mtid, body))
	if err != nil {
		return nil, err
	}
	switch f.Kind {
	case codec.KindRoutingResult:
		if !f.RoutingResult.Accepted {
			s.compliance.Note("cerebrum_rm_failed")
		}
		return f.RoutingResult, nil
	case codec.KindAck:
		return &codec.RoutingResult{Value: "ACCEPTED", Accepted: true}, nil
	case codec.KindBusy:
		return nil, fmt.Errorf("cerebrum-nb: %s: server busy (mtid %d)", verb, mtid)
	default:
		return nil, fmt.Errorf("cerebrum-nb: %s: unexpected reply %s", verb, f.Kind)
	}
}

// Assoc issues a §4.1 association action — SRCE_ASSOC / DEST_ASSOC (a
// device input or output) or SRCE_ASSOC_IP / DEST_ASSOC_IP (an IP sender
// or receiver): it binds one RouteMaster source or destination, on one
// RouteMaster level, to the device IO behind it. The caller fills the
// LOGICAL_* and TARGET_* fields of the action; the router addressing is
// taken from addr.
func (s *Session) Assoc(ctx context.Context, addr RouteTarget, a codec.RoutingAction) error {
	a.IPAddress, a.DeviceName, a.DeviceType = addr.IPAddress, addr.DeviceName, addr.DeviceType
	return s.Action(ctx, &a)
}

// RouteTarget is the common router-addressing tuple shared by the routing
// write actions. Use IPAddress="0.0.0.0" + DeviceType="ROUTER" for the
// route-master sentinel, or a physical Router IP / DeviceName.
type RouteTarget struct {
	IPAddress  string
	DeviceName string
	DeviceType codec.DeviceType
}

// DefaultRouteTarget is the route-master sentinel used by every routing
// write verb when the caller doesn't override the target.
func DefaultRouteTarget() RouteTarget {
	return RouteTarget{IPAddress: "0.0.0.0", DeviceType: codec.DeviceType("ROUTER")}
}

// Salvo issues a §4.3 salvo action: RUN / SAVE / RENAME / DESCRIPTION /
// DELETE. group/instance address the salvo; newName is used by RENAME;
// description by SAVE / DESCRIPTION.
func (s *Session) Salvo(ctx context.Context, salvoType, group, instance, newName, description string) error {
	body := &codec.SalvoAction{
		Type:        salvoType,
		Group:       group,
		Instance:    instance,
		NewName:     newName,
		Description: description,
	}
	return s.Action(ctx, body)
}

// Category issues a §4.2 category action: CREATE / DELETE / DELETE_ITEM /
// MODIFY_ITEM / MODIFY_ALL / MODIFY_DESC. The CategoryAction fields used
// depend on TYPE; the encoder only emits the non-empty ones.
func (s *Session) Category(ctx context.Context, act *codec.CategoryAction) error {
	if act == nil {
		return fmt.Errorf("cerebrum-nb: category: nil action")
	}
	return s.Action(ctx, act)
}

// SetDeviceValue issues a §4.4 DEVICE SET_VALUE action against a
// (device, sub_device, object) tuple.
func (s *Session) SetDeviceValue(ctx context.Context, deviceName, subDevice, object, value string) error {
	body := &codec.DeviceAction{
		Type:       "SET_VALUE",
		DeviceName: deviceName,
		SubDevice:  subDevice,
		Object:     object,
		Value:      value,
	}
	return s.Action(ctx, body)
}

// DeviceConfig issues a §4.5 DEVICE_CONFIGURATION command (the headline
// 0v16 device CRUD verb) and returns the decoded <RESULT VALUE="…"/> reply.
//
// Unlike §4.1-§4.4 actions, DEVICE_CONFIGURATION is a top-level command (not
// wrapped in <ACTION>), so it bypasses Session.Action and round-trips the
// raw codec.EncodeDeviceConfiguration bytes directly. The server echoes the
// request envelope wrapping a single <RESULT VALUE="ACCEPTED|FAILED"/>,
// decoded as codec.KindDeviceConfigResult. A NACK is returned as the
// NackError; a non-result/non-nack reply is an error.
//
// dc selects the verb (ADD/MODIFY/REMOVE) and the body builder (Generic /
// Panel / Router / SNMP). REMOVE carries no body.
func (s *Session) DeviceConfig(ctx context.Context, dc *codec.DeviceConfiguration) (*codec.DeviceConfigResult, error) {
	if dc == nil {
		return nil, fmt.Errorf("cerebrum-nb: device-config: nil configuration")
	}
	mtid := s.nextMTID()
	payload := codec.EncodeDeviceConfiguration(mtid, dc)
	f, err := s.roundTrip(ctx, mtid, payload)
	if err != nil {
		return nil, err
	}
	switch f.Kind {
	case codec.KindDeviceConfigResult:
		// The decoder always populates DeviceConfig for a
		// device_configuration root (the <RESULT> child may be absent, in
		// which case Value is empty and Accepted is false).
		if !f.DeviceConfig.Accepted {
			s.compliance.Note("cerebrum_device_config_failed")
		}
		return f.DeviceConfig, nil
	case codec.KindBusy:
		return nil, fmt.Errorf("cerebrum-nb: device-config: server busy (mtid %d)", mtid)
	default:
		return nil, fmt.Errorf("cerebrum-nb: device-config: unexpected reply %s", f.Kind)
	}
}

// Unsubscribe sends a §2 UNSUBSCRIBE for the given items (the children
// must match a prior SUBSCRIBE per spec) and waits for the ACK.
func (s *Session) Unsubscribe(ctx context.Context, items []codec.SubItem) error {
	mtid := s.nextMTID()
	f, err := s.roundTrip(ctx, mtid, codec.EncodeUnsubscribe(mtid, items))
	if err != nil {
		return err
	}
	if f.Kind != codec.KindAck {
		return fmt.Errorf("cerebrum-nb: unsubscribe: unexpected %s", f.Kind)
	}
	return nil
}

// ObtainDatastore issues an OBTAIN for a §5.5 datastore path. Snapshot
// rows arrive asynchronously through the dispatcher (register an OnEvent
// for codec.KindDatastoreChange first). Returns nil on the ACK.
func (s *Session) ObtainDatastore(ctx context.Context, name string) error {
	return s.Obtain(ctx, []codec.SubItem{&codec.DatastoreChange{Name: name}})
}

// SubscribeDatastore subscribes to changes for a §5.5 datastore path.
func (s *Session) SubscribeDatastore(ctx context.Context, name string) error {
	return s.Subscribe(ctx, []codec.SubItem{&codec.DatastoreChange{Name: name}})
}
