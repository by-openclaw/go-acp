package codec

// RouterConfigResponse1LevelEntry is the per-level sub-record carried
// in tx 076 ROUTER CONFIGURATION RESPONSE - 1 — destination and
// source counts for one level that is present in the level map.
//
//	| Bytes | Field                       | Notes                |
//	|-------|-----------------------------|----------------------|
//	|  5-6  | No of Destinations Mult+Mod | 0-16383              |
//	|  7-8  | No of Sources Mult+Mod      | 0-16383              |
//
// Spec: SW-P-02 Issue 26 §3.2.58.
type RouterConfigResponse1LevelEntry struct {
	NumDestinations uint16 // 0-16383
	NumSources      uint16 // 0-16383
}

// RouterConfigResponse1Params carries tx 076 fields. LevelMap is the
// 28-bit bit map (bit 0 = level 0); Levels holds one entry per set
// bit in LevelMap, in bit-0 → bit-27 order. §3.2.58 notes "the first
// destination and source size returned may not be for level 0 (if
// bit 0 is not set in the levels bit map)".
//
// Spec: SW-P-02 Issue 26 §3.2.58.
type RouterConfigResponse1Params struct {
	LevelMap uint32 // bits 0-27 significant
	Levels   []RouterConfigResponse1LevelEntry
}

// EncodeRouterConfigResponse1 builds tx 076 wire bytes. len(Levels)
// must equal popcount(LevelMap & 0x0FFF_FFFF); callers are
// responsible for the two agreeing before calling.
func EncodeRouterConfigResponse1(p RouterConfigResponse1Params) Frame {
	hdr := packLevelMap(p.LevelMap)
	payload := make([]byte, 0, RouterConfigLevelMapBytes+4*len(p.Levels))
	payload = append(payload, hdr[:]...)
	for _, lvl := range p.Levels {
		payload = append(payload,
			byte((lvl.NumDestinations/128)&0x7F),
			byte(lvl.NumDestinations%128),
			byte((lvl.NumSources/128)&0x7F),
			byte(lvl.NumSources%128),
		)
	}
	return Frame{ID: TxRouterConfigResponse1, Payload: payload}
}

// DecodeRouterConfigResponse1 parses tx 076. The caller must hand in
// a frame whose MESSAGE length is consistent with the level map
// (Unpack guarantees this via PayloadSize).
func DecodeRouterConfigResponse1(f Frame) (RouterConfigResponse1Params, error) {
	if f.ID != TxRouterConfigResponse1 {
		return RouterConfigResponse1Params{}, ErrWrongCommand
	}
	if len(f.Payload) < RouterConfigLevelMapBytes {
		return RouterConfigResponse1Params{}, ErrShortPayload
	}
	hdr := [RouterConfigLevelMapBytes]byte{f.Payload[0], f.Payload[1], f.Payload[2], f.Payload[3]}
	m := unpackLevelMap(hdr)
	n := levelMapCount(m)
	if len(f.Payload) < RouterConfigLevelMapBytes+4*n {
		return RouterConfigResponse1Params{}, ErrShortPayload
	}
	levels := make([]RouterConfigResponse1LevelEntry, n)
	for i := 0; i < n; i++ {
		off := RouterConfigLevelMapBytes + 4*i
		levels[i] = RouterConfigResponse1LevelEntry{
			NumDestinations: (uint16(f.Payload[off])&0x7F)*128 + uint16(f.Payload[off+1]),
			NumSources:      (uint16(f.Payload[off+2])&0x7F)*128 + uint16(f.Payload[off+3]),
		}
	}
	return RouterConfigResponse1Params{LevelMap: m, Levels: levels}, nil
}

// routerConfigResponse1PayloadSize sizes a tx 076 MESSAGE from a peek
// at the buffered bytes — the 4-byte level map drives the count of
// 4-byte per-level entries. Returns (0, false) when the peek is
// shorter than the header.
func routerConfigResponse1PayloadSize(peek []byte) (int, bool) {
	if len(peek) < RouterConfigLevelMapBytes {
		return 0, false
	}
	m := unpackLevelMap([RouterConfigLevelMapBytes]byte{peek[0], peek[1], peek[2], peek[3]})
	return RouterConfigLevelMapBytes + 4*levelMapCount(m), true
}
