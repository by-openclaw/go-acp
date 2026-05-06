package acp1

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"time"

	"dhs/internal/export"
	"dhs/internal/protocol"
	"dhs/internal/wiretrace"
	"dhs/internal/acp1/codec"
)

// Validate decodes captured ACP1 wire-trace records (Trames per ADR-0021)
// through the message decoder, asserts PVER=1 and MTID rules, and returns
// a report with per-direction counts plus any decode failures or
// invariant violations.
//
// This is the offline counterpart to Walk: same codec, no live peer.
//
// When --out-tree is set, every successful getObject reply is converted
// via toProtocolObject and accumulated into a per-slot map. After the
// scan completes, the map is rendered as an export.Snapshot and written
// to disk. The result is byte-equivalent (modulo timestamp + generator)
// to the snapshot a live Walk would produce against the same device,
// so capture / replay round-trips can be asserted in CI.
//
// --out-params remains a follow-up.
func (p *Plugin) Validate(ctx context.Context, trames []wiretrace.Trame, opts protocol.ValidateOpts) (*protocol.ValidateReport, error) {
	report := &protocol.ValidateReport{
		PerDirection: map[wiretrace.Direction]int{},
	}

	if opts.OutParams != "" {
		return report, fmt.Errorf("acp1.Validate: --out-params not implemented yet (follow-up PR)")
	}

	// Per-(slot, group, id) latest decoded object — getObject replies
	// can repeat (e.g. the consumer re-walked); the latest wins.
	type objKey struct {
		slot  int
		group codec.ObjGroup
		id    uint8
	}
	objects := map[objKey]protocol.Object{}
	// frameStatuses captures the latest decoded frame-status reply
	// (slot 0, group=frame, id=0) so the replay tree can stamp each
	// SlotDump with the per-slot status it had at capture time —
	// matching what GetSlotInfo populates on a live walk.
	var frameStatuses []protocol.SlotStatus

	var lastReqMTID uint32
	for i, t := range trames {
		if err := ctx.Err(); err != nil {
			return report, err
		}

		raw, err := hex.DecodeString(t.Hex)
		if err != nil {
			report.Errors = append(report.Errors, protocol.ValidateError{
				TrameIndex: i,
				Direction:  t.Direction,
				Err:        fmt.Sprintf("hex decode: %v", err),
			})
			continue
		}

		msg, err := codec.Decode(raw)
		if err != nil {
			report.Errors = append(report.Errors, protocol.ValidateError{
				TrameIndex: i,
				Direction:  t.Direction,
				HexPrefix:  shortHex(raw),
				Err:        fmt.Sprintf("acp1 decode: %v", err),
			})
			continue
		}

		report.TramesProcessed++
		report.PerDirection[t.Direction]++

		// PVER must always be 1 for v1.4 devices.
		if msg.PVER != 1 {
			report.Invariants = append(report.Invariants,
				fmt.Sprintf("trame %d: codec.PVER %d, want 1", i, msg.PVER))
		}

		switch msg.MType {
		case codec.MTypeRequest:
			if msg.MTID == 0 {
				report.Invariants = append(report.Invariants,
					fmt.Sprintf("trame %d: request with MTID=0 (spec: must be non-zero)", i))
			}
			lastReqMTID = msg.MTID
		case codec.MTypeReply:
			if msg.MTID != lastReqMTID {
				report.Invariants = append(report.Invariants,
					fmt.Sprintf("trame %d: reply MTID=%d does not match last request MTID=%d", i, msg.MTID, lastReqMTID))
			}
			// Capture getObject replies into the per-slot tree map
			// when --out-tree is requested. We can decode every reply
			// regardless and just discard the result if --out-tree is
			// off, but the decode is non-trivial — gate it.
			if opts.OutTree != "" && msg.MCode == byte(codec.MethodGetObject) {
				// Skip root probes: the live walker reads root only for
				// per-group counts (browser.go) and never emits it as
				// an Object. The replay tree must match.
				if msg.ObjGroup == codec.GroupRoot {
					break
				}
				if d, derr := codec.DecodeObject(msg.Value); derr == nil {
					obj := toProtocolObject(d, int(msg.MAddr), msg.ObjGroup, msg.ObjID)
					objects[objKey{int(msg.MAddr), msg.ObjGroup, msg.ObjID}] = obj
				}
			}
			// Capture frame-status getValue replies so the replay tree
			// can stamp each SlotDump.Status. Format per spec p.24:
			// MDATA = [num_slots, status_0, status_1, ...].
			if opts.OutTree != "" &&
				msg.MCode == byte(codec.MethodGetValue) &&
				msg.ObjGroup == codec.GroupFrame && msg.ObjID == 0 &&
				len(msg.Value) >= 1 {
				num := int(msg.Value[0])
				if num > 0 && len(msg.Value) >= 1+num {
					frameStatuses = make([]protocol.SlotStatus, num)
					for i := 0; i < num; i++ {
						frameStatuses[i] = protocol.SlotStatus(msg.Value[1+i])
					}
				}
			}
		}

		if opts.StopAt != "" && t.Note == opts.StopAt {
			report.StoppedAt = t.Note
			break
		}
	}

	if opts.OutTree != "" {
		if err := writeReplayTree(opts.OutTree, objects, frameStatuses); err != nil {
			return report, fmt.Errorf("write out-tree: %w", err)
		}
	}

	return report, nil
}

// writeReplayTree emits the captured-getObject map as an export.Snapshot
// JSON. The format matches what `dhs consumer acp1 export --format json`
// produces from a live walk so capture/replay round-trips compare with
// byte-equality (after timestamp normalisation).
//
// frameStatuses is the per-slot status array captured from the
// getValue(slot=0, group=frame, id=0) reply, used to stamp each
// SlotDump.Status (matches what live GetSlotInfo populates).
func writeReplayTree[K comparable](path string, objects map[K]protocol.Object, frameStatuses []protocol.SlotStatus) error {
	bySlot := map[int][]protocol.Object{}
	for _, o := range objects {
		bySlot[o.Slot] = append(bySlot[o.Slot], o)
	}
	slots := make([]int, 0, len(bySlot))
	for s := range bySlot {
		slots = append(slots, s)
	}
	sort.Ints(slots)
	now := time.Now().UTC()
	snap := &export.Snapshot{
		Generator: "dhs validate --out-tree (acp1)",
		CreatedAt: now,
		Device: export.DeviceInfo{
			Protocol: "acp1",
			NumSlots: len(slots),
		},
	}
	for _, s := range slots {
		objs := bySlot[s]
		sort.SliceStable(objs, func(i, j int) bool {
			if objs[i].Group != objs[j].Group {
				return objs[i].Group < objs[j].Group
			}
			return objs[i].ID < objs[j].ID
		})
		dump := export.SlotDump{
			Slot:     s,
			WalkedAt: now,
			Objects:  objs,
		}
		if s >= 0 && s < len(frameStatuses) {
			dump.Status = frameStatuses[s].String()
		}
		snap.Slots = append(snap.Slots, dump)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return export.WriteJSON(f, snap)
}

func shortHex(b []byte) string {
	if len(b) > 16 {
		b = b[:16]
	}
	return hex.EncodeToString(b)
}
