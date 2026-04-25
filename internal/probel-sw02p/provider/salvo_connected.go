package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// salvoConnectedFrame builds the CONNECTED broadcast frame for a slot
// committed via rx 06 GO / rx 36 GO GROUP SALVO. Narrow-staged slots
// (from rx 05 / rx 35) get tx 04 CONNECTED (§3.2.6); extended-staged
// slots (from rx 69 / rx 71) get tx 68 Extended CONNECTED (§3.2.50)
// so the wire form matches the addressing range the controller used
// to stage the slot — a listener subscribed to tx 04 would otherwise
// see a truncated dst / src for any extended slot with either axis
// > 1023.
func salvoConnectedFrame(slot pendingSlot) codec.Frame {
	if slot.Extended {
		return codec.EncodeExtendedConnected(codec.ExtendedConnectedParams{
			Destination: slot.Destination,
			Source:      slot.Source,
		})
	}
	return codec.EncodeConnected(codec.ConnectedParams{
		Destination: slot.Destination,
		Source:      slot.Source,
	})
}
