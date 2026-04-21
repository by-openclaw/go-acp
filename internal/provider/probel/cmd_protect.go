package probel

import (
	iprobel "acp/internal/probel"
)

// --- rx 010 / rx 0x8A : Protect Interrogate -> tx 011 Protect Tally --------

// handleProtectInterrogate replies with the current protect state of
// (matrix, level, dst). Unknown destinations report ProtectNone with
// deviceID=0 (canonical "no protect").
func (s *server) handleProtectInterrogate(f iprobel.Frame) (handlerResult, error) {
	p, err := iprobel.DecodeProtectInterrogate(f)
	if err != nil {
		return handlerResult{}, err
	}
	rec := s.tree.protectAt(p.MatrixID, p.LevelID, p.DestinationID)
	reply := iprobel.EncodeProtectTally(iprobel.ProtectTallyParams{
		MatrixID:      p.MatrixID,
		LevelID:       p.LevelID,
		DestinationID: p.DestinationID,
		DeviceID:      rec.deviceID,
		State:         iprobel.ProtectState(rec.state),
	})
	return handlerResult{reply: &reply}, nil
}

// --- rx 012 / rx 0x8C : Protect Connect -> tx 013 + tx 011 fan-out ---------

// handleProtectConnect installs a Probel-class protect on (matrix,
// level, dst) for the requesting device. Replies tx 013 Protect
// Connected to the originator and broadcasts tx 011 Protect Tally to
// other sessions so every controller sees the state change.
//
// Failure modes: out-of-range dst or an existing override-protect
// return the error through the handler; no broadcast happens.
func (s *server) handleProtectConnect(f iprobel.Frame) (handlerResult, error) {
	p, err := iprobel.DecodeProtectConnect(f)
	if err != nil {
		return handlerResult{}, err
	}
	state := uint8(iprobel.ProtectProbel)
	if err := s.tree.applyProtectConnect(p.MatrixID, p.LevelID, p.DestinationID, p.DeviceID, state, false); err != nil {
		return handlerResult{}, err
	}
	body := iprobel.ProtectTallyParams{
		MatrixID:      p.MatrixID,
		LevelID:       p.LevelID,
		DestinationID: p.DestinationID,
		DeviceID:      p.DeviceID,
		State:         iprobel.ProtectProbel,
	}
	reply := iprobel.EncodeProtectConnected(body)
	tally := iprobel.EncodeProtectTally(body)
	return handlerResult{reply: &reply, tallies: []iprobel.Frame{tally}}, nil
}

// --- rx 014 / rx 0x8E : Protect Disconnect -> tx 015 + tx 011 fan-out ------

// handleProtectDisconnect releases the protect iff the caller owns it.
// Replies tx 015 Protect Disconnected with the final (now-None) state
// and broadcasts tx 011 Protect Tally to other sessions so they see
// the clear.
func (s *server) handleProtectDisconnect(f iprobel.Frame) (handlerResult, error) {
	p, err := iprobel.DecodeProtectDisconnect(f)
	if err != nil {
		return handlerResult{}, err
	}
	if err := s.tree.applyProtectDisconnect(p.MatrixID, p.LevelID, p.DestinationID, p.DeviceID); err != nil {
		return handlerResult{}, err
	}
	body := iprobel.ProtectTallyParams{
		MatrixID:      p.MatrixID,
		LevelID:       p.LevelID,
		DestinationID: p.DestinationID,
		DeviceID:      p.DeviceID,
		State:         iprobel.ProtectNone,
	}
	reply := iprobel.EncodeProtectDisconnected(body)
	tally := iprobel.EncodeProtectTally(body)
	return handlerResult{reply: &reply, tallies: []iprobel.Frame{tally}}, nil
}

// --- rx 017 : Protect Device Name Request -> tx 018 ------------------------

// handleProtectDeviceNameRequest resolves the device-id to a name via
// the tree's process-wide device-name map. Unknown IDs get a positional
// default ("DEV 0042") — never an error, because the wire protocol has
// no failure reply for this request.
func (s *server) handleProtectDeviceNameRequest(f iprobel.Frame) (handlerResult, error) {
	p, err := iprobel.DecodeProtectDeviceNameRequest(f)
	if err != nil {
		return handlerResult{}, err
	}
	reply := iprobel.EncodeProtectDeviceNameResponse(iprobel.ProtectDeviceNameResponseParams{
		DeviceID:   p.DeviceID,
		DeviceName: s.tree.deviceName(p.DeviceID),
	})
	return handlerResult{reply: &reply}, nil
}

// --- rx 019 / rx 0x93 : Protect Tally Dump -> tx 020 ------------------------

// handleProtectTallyDumpRequest dumps the protect table starting at
// FirstDestinationID. For our demo matrix (targetCount ≤ 64) a single
// frame suffices; for wider matrices the handler caps at 64 items per
// frame (SW-P-88 §5.20 cap) and returns the first chunk. Multi-frame
// iteration is a future scope item.
func (s *server) handleProtectTallyDumpRequest(f iprobel.Frame) (handlerResult, error) {
	p, err := iprobel.DecodeProtectTallyDumpRequest(f)
	if err != nil {
		return handlerResult{}, err
	}
	st, ok := s.tree.lookup(p.MatrixID, p.LevelID)
	if !ok {
		empty := iprobel.EncodeProtectTallyDump(iprobel.ProtectTallyDumpParams{
			MatrixID: p.MatrixID, LevelID: p.LevelID,
			FirstDestinationID: p.DestinationID,
		})
		return handlerResult{reply: &empty}, nil
	}
	start := int(p.DestinationID)
	if start >= st.targetCount {
		empty := iprobel.EncodeProtectTallyDump(iprobel.ProtectTallyDumpParams{
			MatrixID: p.MatrixID, LevelID: p.LevelID,
			FirstDestinationID: p.DestinationID,
		})
		return handlerResult{reply: &empty}, nil
	}
	end := start + 64
	if end > st.targetCount {
		end = st.targetCount
	}
	items := make([]iprobel.ProtectTallyItem, 0, end-start)
	for d := start; d < end; d++ {
		rec := st.protects[uint16(d)]
		items = append(items, iprobel.ProtectTallyItem{
			State:    iprobel.ProtectState(rec.state),
			DeviceID: rec.deviceID,
		})
	}
	reply := iprobel.EncodeProtectTallyDump(iprobel.ProtectTallyDumpParams{
		MatrixID:           p.MatrixID,
		LevelID:            p.LevelID,
		FirstDestinationID: p.DestinationID,
		Items:              items,
	})
	return handlerResult{reply: &reply}, nil
}

// --- rx 029 : Master Protect Connect -> tx 013 -----------------------------

// handleMasterProtectConnect is Protect Connect with override=true —
// seizes the protect even from ProtectProbelOver. Emits the same
// tx 013 Protect Connected reply + tx 011 Protect Tally broadcast
// as the regular Protect Connect handler.
func (s *server) handleMasterProtectConnect(f iprobel.Frame) (handlerResult, error) {
	p, err := iprobel.DecodeMasterProtectConnect(f)
	if err != nil {
		return handlerResult{}, err
	}
	state := uint8(iprobel.ProtectProbelOver)
	if err := s.tree.applyProtectConnect(p.MatrixID, p.LevelID, p.DestinationID, p.DeviceID, state, true); err != nil {
		return handlerResult{}, err
	}
	body := iprobel.ProtectTallyParams{
		MatrixID:      p.MatrixID,
		LevelID:       p.LevelID,
		DestinationID: p.DestinationID,
		DeviceID:      p.DeviceID,
		State:         iprobel.ProtectProbelOver,
	}
	reply := iprobel.EncodeProtectConnected(body)
	tally := iprobel.EncodeProtectTally(body)
	return handlerResult{reply: &reply, tallies: []iprobel.Frame{tally}}, nil
}
