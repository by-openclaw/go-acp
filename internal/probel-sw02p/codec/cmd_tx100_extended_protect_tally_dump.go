package codec

// ExtendedProtectTallyDumpEntry is one (destination, device, protect)
// tuple packed in a tx 100 Extended PROTECT TALLY DUMP message. See
// §3.2.64.
//
// Per-entry wire layout (4 bytes):
//
//	| Offset | Field                                                  |
//	|--------|--------------------------------------------------------|
//	|   0    | Destination DIV 128 (§3.2.47 form, bits 0-6, bit 7=0)  |
//	|   1    | Destination MOD 128                                    |
//	|   2    | bits 0-6 Device MOD 128, bit 7 = 0                     |
//	|   3    | bits 0-2 Device DIV 128, bit 3 = 0 (undefined),        |
//	|        | bits 4-6 ProtectState, bit 7 = 0                       |
//
// Note: the packed Device field carries only 3 DIV 128 bits, so the
// per-entry Device range is 0-1023 — tighter than the 16383 range
// tx 96 / 97 / 98 carry in their 7-bit DIV 128 byte. Callers needing
// wider Device numbering use the individual protect commands instead
// of the dump.
type ExtendedProtectTallyDumpEntry struct {
	Destination uint16       // 0-16383 per §3.2.47
	Device      uint16       // 0-1023 per §3.2.64 packed layout
	Protect     ProtectState // bits[12-14] of the Device/Protect word
}

// ExtendedProtectTallyDumpParams carries tx 100 Extended PROTECT TALLY
// DUMP fields. Variable length: 1 status byte + N * 4-byte entries.
//
//	| Byte | Field       | Notes                                     |
//	|------|-------------|-------------------------------------------|
//	|  1   | Count       | 0-32  = number of entries                 |
//	|      |             | 127   = controller (AURORA) reset sentinel|
//	|      |             |         (no entries follow)               |
//	|  ... | Entries[N]  | 4 bytes each, layout above                |
//
// Per §3.2.64 the message is capped at 132 bytes (1 + 4 * 32 + 3 ?);
// controllers split larger dumps into multiple messages. This codec
// accepts any Count up to ExtendedProtectTallyDumpMaxCount per message.
//
// Spec: SW-P-02 Issue 26 §3.2.64.
type ExtendedProtectTallyDumpParams struct {
	// Reset signals the §3.2.64 AURORA-reset sentinel: Count = 127,
	// no entries follow. Encoded as Count=127 on the wire. When true,
	// Entries is ignored.
	Reset bool

	// Entries is the sequence of (dst, device, protect) tuples. Empty
	// slice with Reset=false encodes as Count=0 (empty dump).
	Entries []ExtendedProtectTallyDumpEntry
}

// ExtendedProtectTallyDumpMaxCount is the §3.2.64 maximum entry count
// per message (Count byte = 32 means 32 × 4 = 128 entry bytes + 1
// count byte = 129-byte MESSAGE).
const ExtendedProtectTallyDumpMaxCount = 32

// ExtendedProtectTallyDumpResetSentinel is the Count-byte value that
// signals "controller reset" per §3.2.64.
const ExtendedProtectTallyDumpResetSentinel byte = 127

// EncodeExtendedProtectTallyDump builds tx 100 wire bytes. If Reset is
// true the MESSAGE is a single Count=127 byte and Entries is ignored.
// Otherwise Count = len(Entries); callers should cap Entries at
// ExtendedProtectTallyDumpMaxCount (this encoder does not split).
func EncodeExtendedProtectTallyDump(p ExtendedProtectTallyDumpParams) Frame {
	if p.Reset {
		return Frame{
			ID:      TxExtendedProtectTallyDump,
			Payload: []byte{ExtendedProtectTallyDumpResetSentinel},
		}
	}
	count := len(p.Entries)
	payload := make([]byte, 1+count*4)
	payload[0] = byte(count)
	for i, e := range p.Entries {
		off := 1 + i*4
		payload[off+0] = byte((e.Destination / 128) & 0x7F)
		payload[off+1] = byte(e.Destination % 128)
		payload[off+2] = byte(e.Device%128) & 0x7F
		payload[off+3] = (byte(e.Device/128) & 0x07) | ((byte(e.Protect) & 0x07) << 4)
	}
	return Frame{ID: TxExtendedProtectTallyDump, Payload: payload}
}

// DecodeExtendedProtectTallyDump parses tx 100. The caller must have
// provided a frame whose MESSAGE length is consistent with the leading
// Count byte (Unpack does this via PayloadSize). A Count byte of 127
// decodes to ExtendedProtectTallyDumpParams{Reset: true}; 0 to an
// empty-but-non-reset dump.
func DecodeExtendedProtectTallyDump(f Frame) (ExtendedProtectTallyDumpParams, error) {
	if f.ID != TxExtendedProtectTallyDump {
		return ExtendedProtectTallyDumpParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 1 {
		return ExtendedProtectTallyDumpParams{}, ErrShortPayload
	}
	count := f.Payload[0]
	if count == ExtendedProtectTallyDumpResetSentinel {
		return ExtendedProtectTallyDumpParams{Reset: true}, nil
	}
	if len(f.Payload) < 1+4*int(count) {
		return ExtendedProtectTallyDumpParams{}, ErrShortPayload
	}
	entries := make([]ExtendedProtectTallyDumpEntry, count)
	for i := 0; i < int(count); i++ {
		off := 1 + i*4
		entries[i] = ExtendedProtectTallyDumpEntry{
			Destination: (uint16(f.Payload[off+0]) & 0x7F) * 128 + uint16(f.Payload[off+1]),
			Device:      (uint16(f.Payload[off+3]) & 0x07) * 128 + uint16(f.Payload[off+2]&0x7F),
			Protect:     ProtectState((f.Payload[off+3] >> 4) & 0x07),
		}
	}
	return ExtendedProtectTallyDumpParams{Entries: entries}, nil
}

// tallyDumpPayloadSize returns the MESSAGE byte count for a tx 100
// frame given a peek at the buffered MESSAGE bytes. Returns (0, false)
// when the caller has not yet buffered enough bytes to determine the
// length (the Count byte is missing).
//
// Used by the stream scanner (Unpack) — tx 100 is the only SW-P-02
// command with variable-length MESSAGE.
func tallyDumpPayloadSize(peek []byte) (int, bool) {
	if len(peek) < 1 {
		return 0, false
	}
	c := peek[0]
	if c == 0 || c == ExtendedProtectTallyDumpResetSentinel {
		return 1, true
	}
	return 1 + 4*int(c), true
}
