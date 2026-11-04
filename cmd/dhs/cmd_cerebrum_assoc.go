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
	"strconv"
	"strings"
	"time"

	"dhs/internal/cerebrum-nb/codec"
	cerebrum "dhs/internal/cerebrum-nb/consumer"
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
	federation := fs.Bool("federation", false, "bind the federation source/destination instead of the local one (v0.17 FEDERATION=1)")
	wf := addCerebrumWriteFlags(fs)
	bare := fs.Bool("bare", false, "send the association exactly as §4.1.7-§4.2.0 show it: no IP_ADDRESS / DEVICE_NAME / DEVICE_TYPE router addressing on the action")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	a, err := assocAction(strings.ToUpper(*kind), *srce, *dest, *level, *targetDevice, *targetType, *targetLevel, *targetIO, *targetName, *subDevice)
	if err != nil {
		return err
	}
	if *federation {
		a.Federation = "1"
	}
	if err := wf.guard("assoc", fs.Args(), *wf.dryRun); err != nil {
		return err
	}
	if *wf.dryRun {
		wf.dryRunNote("assoc", fmt.Sprintf("%s %s level=%s -> %s (federation=%t)", a.Type, a.LogicalSrceID+a.LogicalDestID, a.LogicalLevelID, assocTarget(a), *federation))
		return nil
	}
	p, sess, _, err := dialCerebrumAuth(cf, fs.Args(), "assoc")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	ctx, cancel := context.WithTimeout(context.Background(), cf.timeout)
	defer cancel()
	target := routeTargetFromFlags(*router, *deviceName)
	if *bare {
		target = cerebrum.RouteTarget{}
	}
	if err := sess.Assoc(ctx, target, a); err != nil {
		return fmt.Errorf("cerebrum-nb assoc: %w", err)
	}
	// The server answers ACK to any string and to a level with no device, so
	// the ACK says nothing. The federation resource is not in the local list.
	if *federation {
		fmt.Printf("[assoc] ACK %s %s level=%s -> %s (federation: no read-back)\n", a.Type, a.LogicalSrceID+a.LogicalDestID, a.LogicalLevelID, assocTarget(a))
		return nil
	}
	if err := cerebrumAwaitAssoc(ctx, sess, a); err != nil {
		return err
	}
	fmt.Printf("[assoc] OK %s %s level=%s -> %s\n", a.Type, a.LogicalSrceID+a.LogicalDestID, a.LogicalLevelID, assocTarget(a))
	return nil
}

// cerebrumAwaitAssoc reads the resource's ASSOCIATION rows until the binding
// is there, or says what the level holds instead.
func cerebrumAwaitAssoc(ctx context.Context, sess *cerebrum.Session, a codec.RoutingAction) error {
	want := cerebrumStateWant{SrcMne: true, Verb: "assoc"}
	id := a.LogicalSrceID
	if strings.HasPrefix(a.Type, "DEST") {
		want = cerebrumStateWant{DstMne: true, Verb: "assoc"}
		id = a.LogicalDestID
	}
	deadline := time.Now().Add(cerebrumDeviceSettle)
	for {
		st, err := cerebrumObtainState(ctx, sess, "0.0.0.0", "ROUTER", 15*time.Second, want)
		if err != nil {
			return err
		}
		row, ok := cerebrumAssocRow(append(st.Src, st.Dst...), id, a.LogicalLevelID)
		if ok && cerebrumAssocMatches(row, a) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("cerebrum-nb assoc: acknowledged, but %s level %s reads device=%q port=%q ip_uid=%q after %s: is the level's default device set in the Cerebrum user interface?",
				id, a.LogicalLevelID, row.RMDeviceName, row.SrceID+row.DestID, row.RMIPUID, cerebrumDeviceSettle)
		}
		time.Sleep(time.Second)
	}
}

// cerebrumAssocRow is the ASSOCIATION row of one level: its N is the level.
func cerebrumAssocRow(rows []cerebrumMneRow, id, level string) (codec.RoutingAssociation, bool) {
	for _, r := range rows {
		if r.ID != id {
			continue
		}
		for _, as := range r.Assocs {
			if strconv.Itoa(as.Index) == level {
				return as, true
			}
		}
	}
	return codec.RoutingAssociation{}, false
}

// cerebrumAssocMatches compares a row with the action: an IP binding reads
// back as RM_IP_UID "<device>|<sub-device>|<name>", a router one as the device
// and its port.
func cerebrumAssocMatches(row codec.RoutingAssociation, a codec.RoutingAction) bool {
	if strings.HasSuffix(a.Type, "_IP") {
		parts := []string{a.TargetDeviceName}
		if a.SubDevice != "" {
			parts = append(parts, a.SubDevice)
		}
		return row.RMIPUID == strings.Join(append(parts, a.TargetSenderName+a.TargetReceiverName), "|")
	}
	return row.RMDeviceName == a.TargetDeviceName && row.SrceID+row.DestID == a.TargetSrceID+a.TargetDestID
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
