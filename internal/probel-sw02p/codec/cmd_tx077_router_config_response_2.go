package codec

// RouterConfigResponse2LevelEntry is the per-level sub-record carried
// in tx 077 ROUTER CONFIGURATION RESPONSE - 2 — adds Start Dst /
// Start Src (for non-contiguous level addressing) plus two reserved
// bytes to the RESPONSE-1 fields.
//
//	| Bytes  | Field                           | Notes               |
//	|--------|---------------------------------|---------------------|
//	|  5- 6  | No of Destinations DIV + MOD    | 0-16383             |
//	|  7- 8  | No of Sources DIV + MOD         | 0-16383             |
//	|  9-10  | Start Destination DIV + MOD     | 0-16383             |
//	| 11-12  | Start Source DIV + MOD          | 0-16383             |
//	| 13-14  | Reserved (always 0)             |                     |
//
// Spec: SW-P-02 Issue 26 §3.2.59.
type RouterConfigResponse2LevelEntry struct {
	NumDestinations  uint16
	NumSources       uint16
	StartDestination uint16
	StartSource      uint16
}

// RouterConfigResponse2Params carries tx 077 fields. Same level-map
// convention as RESPONSE-1 but each set bit corresponds to a 10-byte
// entry with Start Dst / Start Src + reserved bytes.
//
// Spec: SW-P-02 Issue 26 §3.2.59.
type RouterConfigResponse2Params struct {
	LevelMap uint32 // bits 0-27 significant
	Levels   []RouterConfigResponse2LevelEntry
}

// RouterConfigResponse2EntrySize is the per-level entry byte count
// (8 addressing + 2 reserved = 10).
const RouterConfigResponse2EntrySize = 10

// EncodeRouterConfigResponse2 builds tx 077 wire bytes. Reserved
// bytes are forced to 0 per spec. len(Levels) must equal
// popcount(LevelMap & 0x0FFF_FFFF).
func EncodeRouterConfigResponse2(p RouterConfigResponse2Params) Frame {
	hdr := packLevelMap(p.LevelMap)
	payload := make([]byte, 0, RouterConfigLevelMapBytes+RouterConfigResponse2EntrySize*len(p.Levels))
	payload = append(payload, hdr[:]...)
	for _, lvl := range p.Levels {
		payload = append(payload,
			byte((lvl.NumDestinations/128)&0x7F),
			byte(lvl.NumDestinations%128),
			byte((lvl.NumSources/128)&0x7F),
			byte(lvl.NumSources%128),
			byte((lvl.StartDestination/128)&0x7F),
			byte(lvl.StartDestination%128),
			byte((lvl.StartSource/128)&0x7F),
			byte(lvl.StartSource%128),
			0x00, // reserved
			0x00, // reserved
		)
	}
	return Frame{ID: TxRouterConfigResponse2, Payload: payload}
}

// DecodeRouterConfigResponse2 parses tx 077.
func DecodeRouterConfigResponse2(f Frame) (RouterConfigResponse2Params, error) {
	if f.ID != TxRouterConfigResponse2 {
		return RouterConfigResponse2Params{}, ErrWrongCommand
	}
	if len(f.Payload) < RouterConfigLevelMapBytes {
		return RouterConfigResponse2Params{}, ErrShortPayload
	}
	hdr := [RouterConfigLevelMapBytes]byte{f.Payload[0], f.Payload[1], f.Payload[2], f.Payload[3]}
	m := unpackLevelMap(hdr)
	n := levelMapCount(m)
	if len(f.Payload) < RouterConfigLevelMapBytes+RouterConfigResponse2EntrySize*n {
		return RouterConfigResponse2Params{}, ErrShortPayload
	}
	levels := make([]RouterConfigResponse2LevelEntry, n)
	for i := 0; i < n; i++ {
		off := RouterConfigLevelMapBytes + RouterConfigResponse2EntrySize*i
		levels[i] = RouterConfigResponse2LevelEntry{
			NumDestinations:  (uint16(f.Payload[off+0])&0x7F)*128 + uint16(f.Payload[off+1]),
			NumSources:       (uint16(f.Payload[off+2])&0x7F)*128 + uint16(f.Payload[off+3]),
			StartDestination: (uint16(f.Payload[off+4])&0x7F)*128 + uint16(f.Payload[off+5]),
			StartSource:      (uint16(f.Payload[off+6])&0x7F)*128 + uint16(f.Payload[off+7]),
			// f.Payload[off+8 / off+9] are reserved; ignored on decode.
		}
	}
	return RouterConfigResponse2Params{LevelMap: m, Levels: levels}, nil
}

// routerConfigResponse2PayloadSize sizes a tx 077 MESSAGE from a peek
// at the buffered bytes.
func routerConfigResponse2PayloadSize(peek []byte) (int, bool) {
	if len(peek) < RouterConfigLevelMapBytes {
		return 0, false
	}
	m := unpackLevelMap([RouterConfigLevelMapBytes]byte{peek[0], peek[1], peek[2], peek[3]})
	return RouterConfigLevelMapBytes + RouterConfigResponse2EntrySize*levelMapCount(m), true
}
