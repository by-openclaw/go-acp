package dnssd

import (
	"encoding/binary"
	"errors"
	"testing"
)

// An SRV whose target is the root encodes as the single root byte —
// the shape a device with no hostname would advertise.
func TestSRVWithRootTarget(t *testing.T) {
	msg := Message{Answers: []RR{{
		Name: "a._nmos-query._tcp.local", Type: TypeSRV, Class: ClassIN,
		SRV: &SRVData{Port: 8235, Target: ""},
	}}}
	raw, err := msg.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	back, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(back.Answers) != 1 || back.Answers[0].SRV == nil || back.Answers[0].SRV.Target != "" {
		t.Errorf("round-tripped %+v", back.Answers)
	}
}

// A record whose owner name is malformed is refused before its rdata
// is read — the name is the first thing on the wire.
func TestDecodeRefusesRecordWithMalformedOwnerName(t *testing.T) {
	buf := header(0, 1)
	buf = append(buf, 0xC0, 0xFF) // a forward pointer where the name goes
	if _, err := Decode(buf); !errors.Is(err, ErrBadPointer) {
		t.Errorf("= %v, want ErrBadPointer", err)
	}
}

// A name that runs off the end of the buffer with no terminator is
// truncated, not read past.
func TestDecodeRefusesUnterminatedName(t *testing.T) {
	buf := header(1, 0)
	buf = append(buf, 1, 'a') // one label, no root byte, buffer ends
	if _, err := Decode(buf); !errors.Is(err, ErrTruncated) {
		t.Errorf("= %v, want ErrTruncated", err)
	}
}

// A chain of backward compression pointers is bounded: a packet
// crafted to make the decoder walk a long chain is refused after 32
// jumps. (Backward-only pointers cannot form a true cycle, so the
// bound is what stops a long enough chain from being a denial of
// service.) The chain is hidden in one record's rdata and entered
// from the next record's owner name, since every pointer must aim at
// an earlier offset than itself.
func TestDecodeRefusesLongPointerChain(t *testing.T) {
	const chainLen = 34

	buf := header(0, 2)
	nameAt := len(buf)
	buf = append(buf, 1, 'a', 0) // the name the chain terminates on

	// A record of a type the codec does not model, so its rdata is
	// copied verbatim rather than parsed — that rdata IS the chain.
	rr := make([]byte, 10)
	binary.BigEndian.PutUint16(rr[0:], 99) // an unmodelled type
	binary.BigEndian.PutUint16(rr[2:], ClassIN)
	binary.BigEndian.PutUint16(rr[8:], chainLen*2)
	buf = append(buf, rr...)

	chainStart := len(buf)
	for i := 0; i < chainLen; i++ {
		target := nameAt
		if i > 0 {
			target = chainStart + (i-1)*2
		}
		var pb [2]byte
		binary.BigEndian.PutUint16(pb[:], 0xC000|uint16(target))
		buf = append(buf, pb[:]...)
	}

	// The second record enters the chain at its far end.
	var ab [2]byte
	binary.BigEndian.PutUint16(ab[:], 0xC000|uint16(chainStart+(chainLen-1)*2))
	buf = append(buf, ab[:]...)
	buf = append(buf, 0, 99, 0, 1, 0, 0, 0, 0, 0, 0) // type, class, TTL, rdlen 0

	if _, err := Decode(buf); !errors.Is(err, ErrPointerLoop) {
		t.Errorf("= %v, want ErrPointerLoop", err)
	}
}
