package cerebrumnb

import (
	"context"
	"fmt"

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
	}
	return s.Action(ctx, body)
}

// SetTags removes tags from a source or destination via the §4.1
// RM_SRCE_TAGS / RM_DEST_TAGS routing action. tagsType selects which.
// The spec only documents tag REMOVAL on the routing path; tags carries
// the comma-separated tag list.
func (s *Session) SetTags(ctx context.Context, tagsType string, addr RouteTarget, srceID, destID, tags string) error {
	body := &codec.RoutingAction{
		Type:       tagsType,
		IPAddress:  addr.IPAddress,
		DeviceName: addr.DeviceName,
		DeviceType: addr.DeviceType,
		SrceID:     srceID,
		DestID:     destID,
		Tags:       tags,
	}
	return s.Action(ctx, body)
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
			s.compliance.Event("cerebrum_device_config_failed")
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
