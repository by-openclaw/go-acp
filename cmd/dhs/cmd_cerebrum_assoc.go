package main

// `dhs consumer cerebrum-nb assoc` — §4.1 SRCE_ASSOC / DEST_ASSOC /
// SRCE_ASSOC_IP / DEST_ASSOC_IP: bind one RouteMaster source or
// destination, on one RouteMaster level, to the device IO behind it.
//
// The codec has always encoded these four; no verb sent them. On a level
// created empty over northbound (LEVEL_MNE on a new id) this is how a
// resource is put on it.

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"dhs/internal/cerebrum-nb/codec"
)

func cerebrumAssoc(_ context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb assoc", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	kind := fs.String("kind", "", "SRCE_ASSOC | DEST_ASSOC | SRCE_ASSOC_IP | DEST_ASSOC_IP")
	router := fs.String("router", "0.0.0.0", "router IP target")
	deviceName := fs.String("device-name", "", "address by DEVICE_NAME")
	srce := fs.String("srce", "", "RouteMaster source ID (SRCE_ASSOC*)")
	dest := fs.String("dest", "", "RouteMaster destination ID (DEST_ASSOC*)")
	level := fs.String("level", "", "RouteMaster level ID")
	targetDevice := fs.String("target-device", "", "name of the device the IO is on")
	targetType := fs.String("target-device-type", "ROUTER", "its device type (SRCE_ASSOC / DEST_ASSOC)")
	targetLevel := fs.String("target-level", "", "level on that device (SRCE_ASSOC / DEST_ASSOC)")
	targetIO := fs.String("target-io", "", "input (SRCE_ASSOC) or output (DEST_ASSOC) number on that device")
	targetName := fs.String("target-name", "", "IP sender (SRCE_ASSOC_IP) or receiver (DEST_ASSOC_IP) name")
	subDevice := fs.String("sub-device", "", "sub-device of the target (the *_IP kinds)")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	a, err := assocAction(strings.ToUpper(*kind), *srce, *dest, *level, *targetDevice, *targetType, *targetLevel, *targetIO, *targetName, *subDevice)
	if err != nil {
		return err
	}
	p, sess, _, err := dialCerebrumAuth(cf, fs.Args(), "assoc")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	ctx, cancel := context.WithTimeout(context.Background(), cf.timeout)
	defer cancel()
	if err := sess.Assoc(ctx, routeTargetFromFlags(*router, *deviceName), a); err != nil {
		return fmt.Errorf("cerebrum-nb assoc: %w", err)
	}
	fmt.Printf("[assoc] OK %s %s level=%s -> %s\n", a.Type, a.LogicalSrceID+a.LogicalDestID, a.LogicalLevelID, assocTarget(a))
	return nil
}

// assocAction builds the action from the flags, refusing what the server
// would only answer with an opaque NACK.
func assocAction(kind, srce, dest, level, targetDevice, targetType, targetLevel, targetIO, targetName, subDevice string) (codec.RoutingAction, error) {
	a := codec.RoutingAction{Type: kind, LogicalLevelID: level, TargetDeviceName: targetDevice}
	if err := requireKind(kind, "assoc", "SRCE_ASSOC", "DEST_ASSOC", "SRCE_ASSOC_IP", "DEST_ASSOC_IP"); err != nil {
		return a, err
	}
	source := strings.HasPrefix(kind, "SRCE")
	id := dest
	if source {
		id = srce
	}
	switch {
	case id == "":
		return a, cerebrumValErr("assoc", "the RouteMaster ID is required (--srce for SRCE_ASSOC*, --dest for DEST_ASSOC*)")
	case level == "":
		return a, cerebrumValErr("assoc", "--level is required")
	case targetDevice == "":
		return a, cerebrumValErr("assoc", "--target-device is required")
	}
	if source {
		a.LogicalSrceID = id
	} else {
		a.LogicalDestID = id
	}
	if strings.HasSuffix(kind, "_IP") {
		if targetName == "" {
			return a, cerebrumValErr("assoc", "--target-name (the IP sender or receiver) is required for "+kind)
		}
		a.SubDevice = subDevice
		if source {
			a.TargetSenderName = targetName
		} else {
			a.TargetReceiverName = targetName
		}
		return a, nil
	}
	if targetIO == "" {
		return a, cerebrumValErr("assoc", "--target-io is required for "+kind)
	}
	a.TargetDeviceType = codec.DeviceType(targetType)
	a.TargetLevelID = targetLevel
	if source {
		a.TargetSrceID = targetIO
	} else {
		a.TargetDestID = targetIO
	}
	return a, nil
}

// assocTarget words the target of an association for the terminal.
func assocTarget(a codec.RoutingAction) string {
	if name := a.TargetSenderName + a.TargetReceiverName; name != "" {
		return fmt.Sprintf("%q %q", a.TargetDeviceName, name)
	}
	return fmt.Sprintf("%q level=%s io=%s", a.TargetDeviceName, a.TargetLevelID, a.TargetSrceID+a.TargetDestID)
}
