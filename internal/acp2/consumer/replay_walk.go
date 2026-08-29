package acp2

import (
	"bytes"
	"encoding/hex"
	"fmt"

	"dhs/internal/acp2/codec"
	"dhs/internal/wiretrace"
)

// replayKey indexes recorded get_object replies by (slot, obj-id).
type replayKey struct {
	slot  int
	objID uint32
}

// ReplayWalkFromTrace replays a wire trace (`raw.an2.jsonl` per
// ADR-0021) through the ACP2 codec to reconstruct the in-plugin
// WalkedTree state per slot, exactly as if `Walk` had been called
// live for every slot present in the trace. After this returns, call
// `Plugin.Canonicalize` to emit `tree.json`.
//
// This is the offline counterpart to live Walk. It uses the same
// `parseObjectProperties` decoder the live walker uses; the only
// difference is that get_object replies come from the recorded trace
// instead of a live device. No new code path forks the codec.
func (p *Plugin) ReplayWalkFromTrace(trames []wiretrace.Trame) error {
	// 1. Index get_object replies by (slot, obj-id).
	replies := map[replayKey]*codec.ACP2Message{}
	for _, t := range trames {
		if t.Direction != wiretrace.DirectionRx {
			continue
		}
		raw, err := hex.DecodeString(t.Hex)
		if err != nil {
			continue
		}
		frame, err := codec.ReadAN2Frame(bytes.NewReader(raw))
		if err != nil {
			continue
		}
		if frame.Proto != codec.AN2ProtoACP2 || frame.Type != codec.AN2TypeData {
			continue
		}
		msg, err := codec.DecodeACP2Message(frame.Payload)
		if err != nil {
			continue
		}
		if msg.Type != codec.ACP2TypeReply || msg.Func != codec.ACP2FuncGetObject {
			continue
		}
		replies[replayKey{int(frame.Slot), msg.ObjID}] = msg
	}

	if len(replies) == 0 {
		return fmt.Errorf("acp2 replay: no get_object replies in trace")
	}

	// 2. Determine slots from reply set.
	slotSet := map[int]bool{}
	for k := range replies {
		slotSet[k.slot] = true
	}

	if p.trees == nil {
		p.trees = newWalkedTreeCache(32, 0) // no TTL during replay
	}

	walker := &Walker{logger: p.logger}

	// 3. Walk each slot via offline DFS. Try root obj-id 1 first
	//    (real devices), fall back to 0 (spec default).
	for slot := range slotSet {
		tree := newReplayTree(slot)
		if err := replayWalkObject(walker, slot, 1, nil, tree, replies); err != nil {
			tree = newReplayTree(slot)
			if err := replayWalkObject(walker, slot, 0, nil, tree, replies); err != nil {
				return fmt.Errorf("acp2 replay slot %d: no root obj_id 1 or 0 in trace", slot)
			}
		}
		p.trees.Put(slot, tree)
	}
	return nil
}

func newReplayTree(slot int) *WalkedTree {
	return &WalkedTree{
		Slot:   slot,
		Labels: make(map[string]int),
	}
}

// replayWalkObject mirrors Walker.walkObject but pulls the get_object
// reply from a pre-indexed replay map instead of a live session. Same
// decoder, same recursion, same state mutation.
func replayWalkObject(w *Walker, slot int, objID uint32, parentPath []string, tree *WalkedTree, replies map[replayKey]*codec.ACP2Message) error {
	msg, ok := replies[replayKey{slot, objID}]
	if !ok {
		return fmt.Errorf("no reply for slot=%d obj_id=%d", slot, objID)
	}

	obj, objType, numType, optMap, children := w.parseObjectProperties(msg.Properties, slot, objID, parentPath)

	idx := len(tree.Objects)
	tree.Objects = append(tree.Objects, obj)
	tree.ObjTypes = append(tree.ObjTypes, objType)
	tree.NumTypes = append(tree.NumTypes, numType)
	tree.OptionsMaps = append(tree.OptionsMaps, optMap)
	if obj.Label != "" {
		tree.Labels[obj.Label] = idx
	}

	for _, childID := range children {
		childPath := make([]string, len(obj.Path))
		copy(childPath, obj.Path)
		// Best-effort: a missing child reply isn't fatal — partial walk
		// is better than no walk, matches live walker semantics.
		_ = replayWalkObject(w, slot, childID, childPath, tree, replies)
	}
	return nil
}
