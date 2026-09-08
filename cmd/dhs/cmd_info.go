package main

import (
	"context"
	"flag"
	"fmt"

	"dhs/internal/consumer"
)

func runInfo(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("info", flag.ExitOnError)
	fs.Usage = verbUsageFn(fs, helpInfo) // #751 G5: -h = rich help + all flags
	cf := addCommonFlags(fs)
	output := fs.String("output", "text", "output format: text | json (ADR-0002; #751 G1)")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer <proto> info <host> [--port N] [--timeout DUR]")
	}
	_ = parseVerbFlags(fs, rest)
	jsonOut, oerr := resolveEnsureOutput(*output, false)
	if oerr != nil {
		return oerr
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	info, err := plug.GetDeviceInfo(opCtx)
	if err != nil {
		return err
	}
	if jsonOut {
		return emitReadJSON(buildInfoJSON(opCtx, plug, info, cf.protocol))
	}
	fmt.Printf("device       %s:%d\n", info.IP, info.Port)
	fmt.Printf("protocol     %s v%d\n", cf.protocol, info.ProtocolVersion)
	// R6 #470: surface the wire-level DTD revision the device advertises.
	// "unknown" preserves a fixed-width column when the plugin has no
	// equivalent surface (ACP1/ACP2 leave DeviceInfo.DtdVersion="") or
	// the device's first frame carried no app-bytes and the identity
	// fallback found nothing.
	dtd := info.DtdVersion
	if dtd == "" {
		dtd = "unknown"
	}
	fmt.Printf("dtd_version  %s\n", dtd)
	fmt.Printf("slots        %d\n", info.NumSlots)
	fmt.Println()
	fmt.Println("per-slot status:")
	for slot := 0; slot < info.NumSlots; slot++ {
		si, err := plug.GetSlotInfo(opCtx, slot)
		if err != nil {
			fmt.Printf("  slot %2d   <error: %v>\n", slot, err)
			continue
		}
		fmt.Printf("  slot %2d   status=%-10s online=%t\n", slot, si.Status, si.IsOnline)
	}
	return nil
}

// buildInfoJSON assembles the machine shape of `info` by asking the plugin
// about every slot. A slot that cannot be read carries its error rather than
// disappearing: a frame with one unreadable card still describes the rest.
func buildInfoJSON(ctx context.Context, plug consumer.Protocol, info consumer.DeviceInfo, protocol string) deviceInfoJSON {
	out := deviceInfoJSON{
		Device: fmt.Sprintf("%s:%d", info.IP, info.Port), Protocol: protocol,
		ProtocolVersion: info.ProtocolVersion, DtdVersion: info.DtdVersion,
		Slots: info.NumSlots,
	}
	for slot := 0; slot < info.NumSlots; slot++ {
		si, err := plug.GetSlotInfo(ctx, slot)
		s := slotInfoJSON{Slot: slot}
		if err != nil {
			s.Error = err.Error()
		} else {
			s.Status, s.Online, s.Identity = si.Status.String(), si.IsOnline, si.Identity
		}
		out.SlotStatus = append(out.SlotStatus, s)
	}
	return out
}

// deviceInfoJSON / slotInfoJSON are the machine shape of `info`
// (#751 G1c). Per-slot errors are carried, never swallowed.
type deviceInfoJSON struct {
	Device          string         `json:"device"`
	Protocol        string         `json:"protocol"`
	ProtocolVersion int            `json:"protocol_version"`
	DtdVersion      string         `json:"dtd_version,omitempty"`
	Slots           int            `json:"slots"`
	SlotStatus      []slotInfoJSON `json:"slot_status"`
}

type slotInfoJSON struct {
	Slot   int    `json:"slot"`
	Status string `json:"status,omitempty"`
	Online bool   `json:"online"`
	Error  string `json:"error,omitempty"`

	// Identity is whatever the plugin knows about what is in the slot. On a
	// frame a slot number names a place and the identity is a convenience; on
	// a controller a slot number is a position in a list and the identity —
	// the node's address above all — is the only thing that names the node.
	// Omitted when the plugin fills nothing in, so protocols without the
	// notion keep the shape they had.
	Identity map[string]string `json:"identity,omitempty"`
}
