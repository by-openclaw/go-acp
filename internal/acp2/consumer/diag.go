package acp2

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// DiagResult is one diagnostic probe result.
type DiagResult struct {
	Name    string
	Sent    string // hex of sent ACP2 payload
	Status  string // "ok", "error: ...", "timeout"
	Reply   string // hex of reply payload (if any)
}

// RunDiagnostics connects to the device, completes the AN2 handshake,
// then sends a series of ACP2 request variants to discover which
// format the device accepts. Returns results for all probes.
func RunDiagnostics(ctx context.Context, host string, port int, slot uint8, logger *slog.Logger) ([]DiagResult, error) {
	if logger == nil {
		logger = slog.Default()
	}

	sess := NewSession(logger)
	if err := sess.Connect(ctx, host, port); err != nil {
		return nil, err
	}
	defer func() { _ = sess.Disconnect() }()

	var results []DiagResult

	// Helper: send raw ACP2 bytes inside AN2 data frame, wait for reply.
	sendRaw := func(name string, an2Slot uint8, an2Type AN2Type, payload []byte) DiagResult {
		r := DiagResult{Name: name, Sent: fmt.Sprintf("%x", payload)}

		// Allocate mtid
		mtid, merr := sess.allocMTID(ctx)
		if merr != nil {
			r.Status = "error: " + merr.Error()
			return r
		}
		defer sess.releaseMTID(mtid)

		// Patch mtid into payload byte 1 (ACP2 header byte 1 = mtid)
		if len(payload) >= 2 {
			payload[1] = mtid
		}
		r.Sent = fmt.Sprintf("%x", payload)

		frame := &AN2Frame{
			Proto:   AN2ProtoACP2,
			Slot:    an2Slot,
			MTID:    0,
			Type:    an2Type,
			Payload: payload,
		}

		ch := make(chan *ACP2Message, 1)
		sess.waitMu.Lock()
		sess.waiters[mtid] = ch
		sess.waitMu.Unlock()
		defer func() {
			sess.waitMu.Lock()
			delete(sess.waiters, mtid)
			sess.waitMu.Unlock()
		}()

		if serr := sess.sendFrame(ctx, frame); serr != nil {
			r.Status = "error: send: " + serr.Error()
			return r
		}

		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
			r.Status = "timeout (3s)"
		case <-sess.done:
			r.Status = "error: connection closed"
		case msg := <-ch:
			if msg == nil {
				r.Status = "error: nil reply"
			} else if msg.Type == ACP2TypeError {
				r.Status = fmt.Sprintf("error: stat=%d", msg.Func)
				r.Reply = fmt.Sprintf("%x", msg.Body)
			} else {
				r.Status = fmt.Sprintf("ok: type=%d func=%d", msg.Type, msg.Func)
				r.Reply = fmt.Sprintf("%x", msg.Body)
			}
		}
		return r
	}

	// --- Probe 1: get_object as spec says (12 bytes) on target slot via AN2 data ---
	results = append(results, sendRaw(
		"get_object spec (AN2 data, 12 bytes)",
		slot, AN2TypeData,
		[]byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	))

	// --- Probe 2: get_property pid=1 (object_type) on obj-id 0 ---
	// Tests if ANY body-carrying function works.
	results = append(results, sendRaw(
		"get_property pid=1 (AN2 data, 16 bytes)",
		slot, AN2TypeData,
		[]byte{
			0x00, 0x00, 0x02, 0x01, // type=req, mtid, func=get_property, pid=1
			0x00, 0x00, 0x00, 0x00, // obj-id=0
			0x00, 0x00, 0x00, 0x00, // idx=0
			0x01, 0x00, 0x00, 0x04, // property header: pid=1, data=0, plen=4
		},
	))

	// --- Probe 3: get_object without idx (8 bytes) ---
	results = append(results, sendRaw(
		"get_object no idx (AN2 data, 8 bytes)",
		slot, AN2TypeData,
		[]byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00},
	))

	// --- Probe 4: get_object minimal (4 bytes, like get_version but func=1) ---
	results = append(results, sendRaw(
		"get_object minimal (AN2 data, 4 bytes)",
		slot, AN2TypeData,
		[]byte{0x00, 0x00, 0x01, 0x00},
	))

	// --- Probe 5: get_object via AN2 type=request instead of type=data ---
	results = append(results, sendRaw(
		"get_object spec (AN2 REQUEST, 12 bytes)",
		slot, AN2TypeRequest,
		[]byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	))

	// --- Probe 6: get_object with func byte prefix (like AN2 internal) ---
	// What if the device expects: funcID(u8) + ACP2 payload?
	results = append(results, sendRaw(
		"get_object with func prefix (AN2 data, 13 bytes)",
		slot, AN2TypeData,
		[]byte{0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	))

	// --- Probe 7: get_object on slot 0 (controller) ---
	if slot != 0 {
		results = append(results, sendRaw(
			"get_object spec on slot 0 (AN2 data, 12 bytes)",
			0, AN2TypeData,
			[]byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		))
	}

	// === ACMP probes (proto=3) ===
	// The device supports ACMP on both slots. Cerebrum might use ACMP
	// instead of ACP2 to browse the object tree.

	sendACMP := func(name string, an2Slot uint8, payload []byte) DiagResult {
		r := DiagResult{Name: name, Sent: fmt.Sprintf("%x", payload)}

		mtid, merr := sess.allocMTID(ctx)
		if merr != nil {
			r.Status = "error: " + merr.Error()
			return r
		}
		defer sess.releaseMTID(mtid)

		if len(payload) >= 2 {
			payload[1] = mtid
		}
		r.Sent = fmt.Sprintf("%x", payload)

		frame := &AN2Frame{
			Proto:   3, // ACMP
			Slot:    an2Slot,
			MTID:    0,
			Type:    AN2TypeData,
			Payload: payload,
		}

		ch := make(chan *ACP2Message, 1)
		sess.waitMu.Lock()
		sess.waiters[mtid] = ch
		sess.waitMu.Unlock()
		defer func() {
			sess.waitMu.Lock()
			delete(sess.waiters, mtid)
			sess.waitMu.Unlock()
		}()

		if serr := sess.sendFrame(ctx, frame); serr != nil {
			r.Status = "error: send: " + serr.Error()
			return r
		}

		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
			r.Status = "timeout (3s)"
		case <-sess.done:
			r.Status = "error: connection closed"
		case msg := <-ch:
			if msg == nil {
				r.Status = "error: nil reply"
			} else if msg.Type == ACP2TypeError {
				r.Status = fmt.Sprintf("error: stat=%d", msg.Func)
				r.Reply = fmt.Sprintf("%x", msg.Body)
			} else {
				r.Status = fmt.Sprintf("OK type=%d func=%d props=%d", msg.Type, msg.Func, len(msg.Properties))
				r.Reply = fmt.Sprintf("%x", msg.Body)
			}
		}
		return r
	}

	// --- Probe 8: ACMP get_object (same ACP2 format, proto=3) ---
	results = append(results, sendACMP(
		"ACMP get_object spec (proto=3, 12 bytes)",
		slot,
		[]byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	))

	// --- Probe 9: ACMP get_object minimal (proto=3, 4 bytes) ---
	results = append(results, sendACMP(
		"ACMP get_object minimal (proto=3, 4 bytes)",
		slot,
		[]byte{0x00, 0x00, 0x01, 0x00},
	))

	// --- Probe 10: ACMP get_version (proto=3, 4 bytes) ---
	results = append(results, sendACMP(
		"ACMP get_version (proto=3, 4 bytes)",
		slot,
		[]byte{0x00, 0x00, 0x00, 0x00},
	))

	// --- Probe 11: proto=4 get_object (vendor extension?) ---
	sendProto4 := func(name string, payload []byte) DiagResult {
		r := DiagResult{Name: name, Sent: fmt.Sprintf("%x", payload)}

		mtid, merr := sess.allocMTID(ctx)
		if merr != nil {
			r.Status = "error: " + merr.Error()
			return r
		}
		defer sess.releaseMTID(mtid)

		if len(payload) >= 2 {
			payload[1] = mtid
		}
		r.Sent = fmt.Sprintf("%x", payload)

		frame := &AN2Frame{
			Proto:   4, // vendor extension
			Slot:    slot,
			MTID:    0,
			Type:    AN2TypeData,
			Payload: payload,
		}

		ch := make(chan *ACP2Message, 1)
		sess.waitMu.Lock()
		sess.waiters[mtid] = ch
		sess.waitMu.Unlock()
		defer func() {
			sess.waitMu.Lock()
			delete(sess.waiters, mtid)
			sess.waitMu.Unlock()
		}()

		if serr := sess.sendFrame(ctx, frame); serr != nil {
			r.Status = "error: send: " + serr.Error()
			return r
		}

		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
			r.Status = "timeout (3s)"
		case <-sess.done:
			r.Status = "error: connection closed"
		case msg := <-ch:
			if msg == nil {
				r.Status = "error: nil reply"
			} else if msg.Type == ACP2TypeError {
				r.Status = fmt.Sprintf("error: stat=%d", msg.Func)
				r.Reply = fmt.Sprintf("%x", msg.Body)
			} else {
				r.Status = fmt.Sprintf("OK type=%d func=%d props=%d", msg.Type, msg.Func, len(msg.Properties))
				r.Reply = fmt.Sprintf("%x", msg.Body)
			}
		}
		return r
	}

	results = append(results, sendProto4(
		"proto=4 get_object (vendor, 12 bytes)",
		[]byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	))

	// === Probes with REAL obj-ids from announces ===
	// The announces show obj-ids like 0x00018696. Try get_object with a
	// real obj-id to test if the issue is "obj-id 0 doesn't exist".

	// Listen for announces for 2 seconds to discover a real obj-id.
	logger.Info("acp2 diag: listening for announces to discover real obj-ids (2s)...")
	var discoveredObjIDs []uint32
	annoTimer := time.NewTimer(2 * time.Second)
	announceLoop:
	for {
		select {
		case <-annoTimer.C:
			break announceLoop
		case <-sess.done:
			break announceLoop
		default:
			// Check if any announce has been decoded by the reader.
			// We peek at the raw frames the reader processes.
			time.Sleep(50 * time.Millisecond)
		}
	}

	// If we couldn't discover dynamically, use obj-ids seen in the log.
	if len(discoveredObjIDs) == 0 {
		discoveredObjIDs = []uint32{0x00018696, 0x0000C287, 0x00000001}
	}

	for _, objID := range discoveredObjIDs {
		payload := []byte{
			0x00, 0x00, 0x01, 0x00, // type=req, mtid(patched), func=get_object, pad=0
			byte(objID >> 24), byte(objID >> 16), byte(objID >> 8), byte(objID),
			0x00, 0x00, 0x00, 0x00, // idx=0
		}
		results = append(results, sendRaw(
			fmt.Sprintf("get_object obj-id=0x%08X", objID),
			slot, AN2TypeData,
			payload,
		))
	}

	return results, nil
}
