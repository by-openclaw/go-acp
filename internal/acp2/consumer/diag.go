package acp2

import (
	"context"
	"fmt"
	"log/slog"
	"time"
	"dhs/internal/acp2/codec"
)

// DiagResult is one diagnostic probe result.
type DiagResult struct {
	Name    string
	Sent    string // hex of sent ACP2 payload
	Status  string // "ok", "error: ...", "timeout"
	Reply   string // hex of reply payload (if any)
}

// diagTimings bounds the per-probe reply wait and the announce-listen
// window. Injectable so unit tests run in milliseconds; RunDiagnostics
// always uses the production defaults.
type diagTimings struct {
	probe    time.Duration // per-probe reply timeout
	announce time.Duration // announce-listen window before the obj-id probes
}

func defaultDiagTimings() diagTimings {
	return diagTimings{probe: 3 * time.Second, announce: 2 * time.Second}
}

// diagProbe sends raw ACP2-shaped bytes inside one AN2 frame on any AN2
// proto and resolves the outcome deterministically: a real reply, the
// nil sentinel (connection died — see Session.failWaiters), the probe
// timeout, or a fail-fast when the read loop already exited. Note that
// the session readLoop only routes proto 0 (internal) and proto 2 (ACP2)
// to waiters, so probes on proto 3 (ACMP) / proto 4 (vendor) can never
// receive a real reply — only the timeout or the connection-closed
// sentinel.
func diagProbe(ctx context.Context, sess *Session, name string, proto codec.AN2Proto, an2Slot uint8, an2Type codec.AN2Type, payload []byte, probeTimeout time.Duration) DiagResult {
	r := DiagResult{Name: name, Sent: fmt.Sprintf("%x", payload)}

	// unreachable: allocMTID only errors on ctx cancellation. The diag
	// probes run sequentially, each releasing its mtid before the next,
	// so the 255-slot pool is never exhausted and (with a live ctx)
	// alloc always succeeds.
	mtid, _ := sess.allocMTID(ctx)
	defer sess.releaseMTID(mtid)

	// Patch mtid into payload byte 1 (ACP2 header byte 1 = mtid)
	if len(payload) >= 2 {
		payload[1] = mtid
	}
	r.Sent = fmt.Sprintf("%x", payload)

	frame := &codec.AN2Frame{
		Proto:   proto,
		Slot:    an2Slot,
		MTID:    0,
		Type:    an2Type,
		Payload: payload,
	}

	ch, werr := sess.addWaiter(mtid)
	if werr != nil {
		// The read loop already exited — no reply can ever arrive.
		r.Status = "error: connection closed"
		return r
	}
	defer sess.removeWaiter(mtid)

	if serr := sess.sendFrame(ctx, frame); serr != nil {
		r.Status = "error: send: " + serr.Error()
		return r
	}

	timer := time.NewTimer(probeTimeout)
	defer timer.Stop()
	select {
	case <-timer.C:
		r.Status = fmt.Sprintf("timeout (%s)", probeTimeout)
	case msg := <-ch:
		if msg == nil {
			r.Status = "error: connection closed"
			return r
		}
		if msg.Type == codec.ACP2TypeError {
			r.Status = fmt.Sprintf("error: stat=%d", msg.Func)
		} else {
			r.Status = fmt.Sprintf("ok: type=%d func=%d", msg.Type, msg.Func)
		}
		r.Reply = fmt.Sprintf("%x", msg.Body)
	}
	return r
}

// RunDiagnostics connects to the device, completes the AN2 handshake,
// then sends a series of ACP2 request variants to discover which
// format the device accepts. Returns results for all probes.
func RunDiagnostics(ctx context.Context, host string, port int, slot uint8, logger *slog.Logger) ([]DiagResult, error) {
	return runDiagnostics(ctx, host, port, slot, logger, defaultDiagTimings())
}

func runDiagnostics(ctx context.Context, host string, port int, slot uint8, logger *slog.Logger, timings diagTimings) ([]DiagResult, error) {
	if logger == nil {
		logger = slog.Default()
	}

	sess := NewSession(logger)
	if err := sess.Connect(ctx, host, port); err != nil {
		return nil, err
	}
	defer func() { _ = sess.Disconnect() }()

	var results []DiagResult

	sendProbe := func(name string, proto codec.AN2Proto, an2Slot uint8, an2Type codec.AN2Type, payload []byte) DiagResult {
		return diagProbe(ctx, sess, name, proto, an2Slot, an2Type, payload, timings.probe)
	}

	// --- Probe 1: get_object as spec says (12 bytes) on target slot via AN2 data ---
	results = append(results, sendProbe(
		"get_object spec (AN2 data, 12 bytes)",
		codec.AN2ProtoACP2, slot, codec.AN2TypeData,
		[]byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	))

	// --- Probe 2: get_property pid=1 (object_type) on obj-id 0 ---
	// Tests if ANY body-carrying function works.
	results = append(results, sendProbe(
		"get_property pid=1 (AN2 data, 16 bytes)",
		codec.AN2ProtoACP2, slot, codec.AN2TypeData,
		[]byte{
			0x00, 0x00, 0x02, 0x01, // type=req, mtid, func=get_property, pid=1
			0x00, 0x00, 0x00, 0x00, // obj-id=0
			0x00, 0x00, 0x00, 0x00, // idx=0
			0x01, 0x00, 0x00, 0x04, // property header: pid=1, data=0, plen=4
		},
	))

	// --- Probe 3: get_object without idx (8 bytes) ---
	results = append(results, sendProbe(
		"get_object no idx (AN2 data, 8 bytes)",
		codec.AN2ProtoACP2, slot, codec.AN2TypeData,
		[]byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00},
	))

	// --- Probe 4: get_object minimal (4 bytes, like get_version but func=1) ---
	results = append(results, sendProbe(
		"get_object minimal (AN2 data, 4 bytes)",
		codec.AN2ProtoACP2, slot, codec.AN2TypeData,
		[]byte{0x00, 0x00, 0x01, 0x00},
	))

	// --- Probe 5: get_object via AN2 type=request instead of type=data ---
	results = append(results, sendProbe(
		"get_object spec (AN2 REQUEST, 12 bytes)",
		codec.AN2ProtoACP2, slot, codec.AN2TypeRequest,
		[]byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	))

	// --- Probe 6: get_object with func byte prefix (like AN2 internal) ---
	// What if the device expects: funcID(u8) + ACP2 payload?
	results = append(results, sendProbe(
		"get_object with func prefix (AN2 data, 13 bytes)",
		codec.AN2ProtoACP2, slot, codec.AN2TypeData,
		[]byte{0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	))

	// --- Probe 7: get_object on slot 0 (controller) ---
	if slot != 0 {
		results = append(results, sendProbe(
			"get_object spec on slot 0 (AN2 data, 12 bytes)",
			codec.AN2ProtoACP2, 0, codec.AN2TypeData,
			[]byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		))
	}

	// --- Probe 8: unknown function code -> stat=0 protocol-error ---
	// func=0xFF is outside the spec's {0, 1, 2, 3} range. The provider's
	// dispatch falls through to the default case in handlers.go and
	// replies with ErrProtocol. Covers the stat=0 fixture that no
	// legal-framed probe can produce.
	results = append(results, sendProbe(
		"unknown func=0xFF (AN2 data, 4 bytes)",
		codec.AN2ProtoACP2, slot, codec.AN2TypeData,
		[]byte{0x00, 0x00, 0xFF, 0x00},
	))

	// === ACMP probes (proto=3) ===
	// The device supports ACMP on both slots. Cerebrum might use ACMP
	// instead of ACP2 to browse the object tree.

	// --- Probe 8: ACMP get_object (same ACP2 format, proto=3) ---
	results = append(results, sendProbe(
		"ACMP get_object spec (proto=3, 12 bytes)",
		3, slot, codec.AN2TypeData,
		[]byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	))

	// --- Probe 9: ACMP get_object minimal (proto=3, 4 bytes) ---
	results = append(results, sendProbe(
		"ACMP get_object minimal (proto=3, 4 bytes)",
		3, slot, codec.AN2TypeData,
		[]byte{0x00, 0x00, 0x01, 0x00},
	))

	// --- Probe 10: ACMP get_version (proto=3, 4 bytes) ---
	results = append(results, sendProbe(
		"ACMP get_version (proto=3, 4 bytes)",
		3, slot, codec.AN2TypeData,
		[]byte{0x00, 0x00, 0x00, 0x00},
	))

	// --- Probe 11: proto=4 get_object (vendor extension?) ---
	results = append(results, sendProbe(
		"proto=4 get_object (vendor, 12 bytes)",
		4, slot, codec.AN2TypeData,
		[]byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	))

	// === Probes with REAL obj-ids from announces ===
	// The announces show obj-ids like 0x00018696. Try get_object with a
	// real obj-id to test if the issue is "obj-id 0 doesn't exist".

	// Give a chatty device an announce window before the obj-id probes.
	// Announce decoding happens in the session readLoop; this is a pure
	// wait, cut short if the connection dies.
	logger.Info("acp2 diag: listening for announces to discover real obj-ids...",
		"window", timings.announce)
	var discoveredObjIDs []uint32
	annoTimer := time.NewTimer(timings.announce)
	defer annoTimer.Stop()
	select {
	case <-annoTimer.C:
	case <-sess.done:
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
		results = append(results, sendProbe(
			fmt.Sprintf("get_object obj-id=0x%08X", objID),
			codec.AN2ProtoACP2, slot, codec.AN2TypeData,
			payload,
		))
	}

	return results, nil
}
