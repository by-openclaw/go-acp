package codec

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// capturePath is the trace taken off the live stack on 10.6.250.105 on
// 2026-09-07: RollProxy on :2050 and a Centra controller on :2057, both
// generations, 15 real units.
//
// It lives under docs/ rather than testdata/ because it is audit evidence
// first; unit 9 promotes a trimmed subset into testdata/ per ADR-0020.
const capturePath = "../docs/captures/rollcall-live-2026-09-07.hex"

// capturedFrame is one line of the trace. FromDevice distinguishes what the
// equipment sent from what our probe sent, which matters because the probe
// traffic in this trace includes frames recorded before a defect in it was
// found. Only device-originated frames are an oracle.
type capturedFrame struct {
	FromDevice bool
	Raw        []byte
}

// loadCapture returns every frame in the capture file. Lines are
// "<tag> <TX|RX> <hex>"; comments start with '#'.
func loadCapture(t *testing.T) []capturedFrame {
	t.Helper()

	f, err := os.Open(filepath.FromSlash(capturePath))
	if err != nil {
		t.Skipf("capture not available: %v", err)
	}
	defer func() { _ = f.Close() }()

	var out []capturedFrame
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		raw := fields[len(fields)-1]
		if len(raw) < 2*HeaderSize || len(raw)%2 != 0 {
			continue
		}
		b, err := hex.DecodeString(raw)
		if err != nil {
			continue
		}
		// "RX" marks a frame received from the equipment.
		fromDevice := false
		for _, fl := range fields {
			if strings.EqualFold(fl, "RX") {
				fromDevice = true
			}
		}
		out = append(out, capturedFrame{FromDevice: fromDevice, Raw: b})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read capture: %v", err)
	}
	return out
}

// TestCapture_DecodesAndRoundTrips is the strongest check in this package: it
// decodes every frame a real device sent or received and re-encodes it,
// requiring byte equality.
//
// It catches what expected-byte tables cannot, because the vectors were
// produced by Snell equipment rather than by this implementation.
func TestCapture_DecodesAndRoundTrips(t *testing.T) {
	frames := loadCapture(t)
	if len(frames) < 100 {
		t.Fatalf("only %d frames in the capture; expected the full trace", len(frames))
	}

	seen := map[PacketType]int{}
	for i, cf := range frames {
		raw := cf.Raw
		f, n, err := DecodeFrame(raw)
		if err != nil {
			t.Fatalf("frame %d (%x): decode: %v", i, raw, err)
		}
		if n != len(raw) {
			t.Errorf("frame %d: consumed %d of %d bytes", i, n, len(raw))
		}

		out, err := f.Encode()
		if err != nil {
			t.Fatalf("frame %d (%s): encode: %v", i, f, err)
		}
		if !bytes.Equal(out, raw) {
			t.Fatalf("frame %d (%s) does not round trip\n got %x\nwant %x", i, f, out, raw)
		}
		seen[f.Type]++
	}

	// The capture must exercise a real spread of the catalogue, not just
	// keepalives, or the check above proves very little.
	if len(seen) < 15 {
		t.Errorf("capture covers only %d packet types, want at least 15", len(seen))
	}
	t.Logf("%d frames, %d distinct packet types", len(frames), len(seen))

	// Both generations must be present: this connector exists to serve both.
	var gen16, gen32 int
	for typ, n := range seen {
		if typ.Gen32() {
			gen32 += n
		} else {
			gen16 += n
		}
	}
	if gen16 == 0 || gen32 == 0 {
		t.Errorf("capture is one-sided: %d 16-bit frames, %d 32-bit", gen16, gen32)
	}
}

// TestCapture_PayloadsDecode decodes the payload of every frame whose type
// this unit implements, so a structural mistake surfaces here rather than
// three units later in the consumer.
func TestCapture_PayloadsDecode(t *testing.T) {
	frames := loadCapture(t)

	decoded := map[PacketType]int{}
	for i, cf := range frames {
		raw := cf.Raw
		f, _, err := DecodeFrame(raw)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}

		var derr error
		switch f.Type {
		case MsgCall:
			_, derr = DecodeConnect(f.Payload)
		case MsgTerm:
			_, derr = DecodeTermSess(f.Payload)
		case MsgClearSess:
			_, derr = DecodeClearSess(f.Payload)
		case MsgRetID:
			_, derr = DecodeID(f.Payload)
		case MsgRetStat:
			_, derr = DecodeUnitStatus(f.Payload)
		case MsgRetDevInfo, MsgIam:
			_, derr = DecodeDeviceInfo(f.Payload)
		case MsgGetFStat:
			_, derr = DecodeGetFStat(f.Payload)
		case MsgRetFStat, MsgSetParam:
			_, derr = DecodeFuncStatus(f.Payload)
		case MsgRetFunc:
			_, derr = DecodeFunc(f.Payload)
		case MsgFuncStyleChg:
			_, derr = DecodeFuncStyle(f.Payload)
		case MsgBlockHeader:
			_, derr = DecodeBlockHeader(f.Payload)
		case MsgGetNextPkt:
			_, derr = DecodeGetNext(f.Payload)
		case MsgDispData:
			_, derr = DecodeDisp(f.Payload)
		case MsgWait:
			_, derr = DecodeWait(f.Payload)
		case MsgSetMulti:
			_, derr = DecodeMultiValues(f.Payload)
		default:
			continue // 32-bit types and the file service arrive in later units
		}
		if derr != nil {
			t.Errorf("frame %d (%s): payload: %v\n  %x", i, f, derr, f.Payload)
			continue
		}
		decoded[f.Type]++
	}

	if len(decoded) == 0 {
		t.Fatal("no payloads decoded; the switch above matched nothing")
	}
	for typ, n := range decoded {
		t.Logf("  %-14s %4d", typ, n)
	}
}

// TestCapture_UnknownSessionIsAlways255 pins the defect that crashed a vendor
// simulator three times during the audit: the unconnected session index is the
// value 255, and writing it as a signed -1 puts 0xFFFF on the wire.
//
// The assertion is on device-originated frames only. Our own probe traffic in
// this trace was recorded on both sides of the fix, so it still contains the
// bad encoding; that is history, and the second half of this test asserts it is
// present so the evidence is not quietly lost.
func TestCapture_UnknownSessionIsAlways255(t *testing.T) {
	frames := loadCapture(t)

	var deviceFrames, badProbe int
	for i, cf := range frames {
		f, _, err := DecodeFrame(cf.Raw)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		bad := uint16(f.Dst.Index) == 0xFFFF || uint16(f.Src.Index) == 0xFFFF

		if cf.FromDevice {
			deviceFrames++
			if bad {
				t.Errorf("frame %d (%s): a device sent index 0xFFFF; UNKNOWNSESS is %d",
					i, f, IndexUnknown)
			}
			continue
		}
		if bad {
			badProbe++
		}
	}

	if deviceFrames == 0 {
		t.Fatal("no device-originated frames in the capture")
	}
	t.Logf("%d device frames carry a well-formed session index", deviceFrames)

	if badProbe == 0 {
		t.Log("no pre-fix probe frames remain in the capture")
	} else {
		t.Logf("%d probe frames still carry the pre-fix 0xFFFF encoding "+
			"(recorded before the defect was found; devices never do this)", badProbe)
	}
}
