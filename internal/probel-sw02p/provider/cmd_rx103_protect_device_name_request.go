package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// handleProtectDeviceNameRequest processes rx 103 PROTECT DEVICE
// NAME REQUEST (§3.2.67). Either peer can issue this to resolve the
// 8-char ASCII name of the device whose Device Number appears in
// the request. This plugin responds by matching the queried Device
// Number:
//
//   - If the queried number == server's configured selfDeviceNumber,
//     reply with tx 099 carrying the server's selfDeviceName.
//   - Otherwise scan protect entries for the first whose OwnerDevice
//     matches the queried number and reply with its captured
//     OwnerName (may be "" until the peer has itself answered rx 103
//     at some earlier point — the name layer is advisory only; the
//     §3.2.60 authority ladder uses Device NUMBERS, not names, so an
//     empty Name does not block the owner-only rule).
//   - Fallback: reply with an empty name so the peer observes a
//     well-formed but empty tx 099.
//
// Names are clamped to 8 characters on the wire by the encoder.
func (s *server) handleProtectDeviceNameRequest(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeProtectDeviceNameRequest(f)
	if err != nil {
		return handlerResult{}, err
	}
	s.mu.Lock()
	selfNum := s.selfDeviceNumber
	selfName := s.selfDeviceName
	s.mu.Unlock()

	name := ""
	if p.Device == selfNum {
		name = selfName
	} else {
		// Scan protect entries for an owner match.
		for _, pe := range s.tree.protectEntriesByDevice(p.Device) {
			if pe.OwnerName != "" {
				name = pe.OwnerName
				break
			}
		}
	}
	reply := codec.EncodeProtectDeviceNameResponse(codec.ProtectDeviceNameResponseParams{
		Device: p.Device,
		Name:   name,
	})
	return handlerResult{reply: &reply}, nil
}
