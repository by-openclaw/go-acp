package codec

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// ADR-0025 deliverable 6: the wire this codec claims to speak, as a
// real agent actually sent it. The frames come from the ATEME Kyrion
// DR5000 (10.6.255.114, v1 on 161) on 2026-09-24 — one capture per PDU
// kind under testdata/protocol_types/, and the whole session under
// testdata/fixtures/.
//
// The point is not that the decoder runs: it is that the decoder runs
// on bytes nobody here typed. A vendor that encodes a length the long
// way, or answers v1 to a v2c request, breaks this offline and in CI
// rather than on a walk at 03:00.

const replayRoot = "../testdata"

// pcapngUDPPayloads reads the UDP payloads out of a pcapng file. It
// understands exactly as much of the format as this needs: Section
// Header, Interface Description, Enhanced Packet Blocks, Ethernet or
// Linux cooked framing, IPv4, UDP. Anything else is skipped rather
// than guessed at.
func pcapngUDPPayloads(t *testing.T, path string) [][]byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}

	var (
		out    [][]byte
		linkTy uint16
		off    int
		bo     = binary.LittleEndian
	)
	for off+12 <= len(raw) {
		blockType := bo.Uint32(raw[off:])
		if blockType == 0x0A0D0D0A { // Section Header: it carries the byte order
			if binary.BigEndian.Uint32(raw[off+8:]) == 0x1A2B3C4D {
				t.Fatalf("%s: big-endian pcapng is not read here", path)
			}
		}
		blockLen := int(bo.Uint32(raw[off+4:]))
		if blockLen < 12 || off+blockLen > len(raw) {
			break
		}
		body := raw[off+8 : off+blockLen-4]
		switch blockType {
		case 0x00000001: // Interface Description
			if len(body) >= 2 {
				linkTy = bo.Uint16(body)
			}
		case 0x00000006: // Enhanced Packet Block
			if len(body) < 20 {
				break
			}
			capLen := int(bo.Uint32(body[12:]))
			if 20+capLen > len(body) {
				break
			}
			if p := udpPayload(body[20:20+capLen], linkTy); p != nil {
				out = append(out, p)
			}
		}
		off += blockLen
	}
	if len(out) == 0 {
		t.Fatalf("%s: no UDP payloads in the capture", path)
	}
	return out
}

// udpPayload strips the link, IP and UDP headers of one frame.
func udpPayload(frame []byte, linkType uint16) []byte {
	var ipOff int
	switch linkType {
	case 1: // Ethernet
		if len(frame) < 14 || binary.BigEndian.Uint16(frame[12:]) != 0x0800 {
			return nil
		}
		ipOff = 14
	case 113: // Linux cooked (what "-i any" produces)
		if len(frame) < 16 || binary.BigEndian.Uint16(frame[14:]) != 0x0800 {
			return nil
		}
		ipOff = 16
	default:
		return nil
	}
	if len(frame) < ipOff+20 || frame[ipOff]>>4 != 4 || frame[ipOff+9] != 17 { // IPv4, UDP
		return nil
	}
	ihl := int(frame[ipOff]&0x0F) * 4
	udpOff := ipOff + ihl
	if len(frame) < udpOff+8 {
		return nil
	}
	udpLen := int(binary.BigEndian.Uint16(frame[udpOff+4:]))
	end := udpOff + udpLen
	if udpLen < 8 || end > len(frame) {
		end = len(frame)
	}
	return frame[udpOff+8 : end]
}

// decoded is what one captured message turned out to be.
type decoded struct {
	version   Version
	community string
	pduType   PDUType
	errStatus int
	varBinds  int
}

func decodeAll(t *testing.T, path string) []decoded {
	t.Helper()
	var out []decoded
	for i, payload := range pcapngUDPPayloads(t, path) {
		m, err := Decode(payload)
		if err != nil {
			t.Fatalf("%s: message %d does not decode: %v", path, i, err)
		}
		d := decoded{version: m.Version, community: m.Community}
		if m.PDU != nil {
			d.pduType = m.PDU.Type
			d.errStatus = int(m.PDU.ErrorStatus)
			d.varBinds = len(m.PDU.VarBinds)
		}
		out = append(out, d)
	}
	return out
}

func TestEveryCapturedPDUKindDecodes(t *testing.T) {
	// A folder is one PDU KIND. Versions mix inside most of them,
	// because the same read was made in v1 and in v2c and the kind is
	// what the folder is about; only v2c-exchange is about a version.
	cases := []struct {
		folder  string
		want    PDUType
		version Version // 0 = either
	}{
		{"get-request", PDUTypeGet, 0},
		{"get-next-request", PDUTypeGetNext, 0},
		{"get-response", PDUTypeResponse, 0},
		{"set-request", PDUTypeSet, 0},
		{"error-response", PDUTypeResponse, 0},
		{"v2c-exchange", 0, Version2c},
	}
	for _, c := range cases {
		t.Run(c.folder, func(t *testing.T) {
			msgs := decodeAll(t, filepath.Join(replayRoot, "protocol_types", c.folder, "wire.pcapng"))
			for _, m := range msgs {
				if m.version != Version1 && m.version != Version2c {
					t.Errorf("version = %v", m.version)
				}
				if c.version != 0 && m.version != c.version {
					t.Errorf("version = %v, want %v", m.version, c.version)
				}
				if m.community == "" {
					t.Error("no community in a v1/v2c message")
				}
				if c.want != 0 && m.pduType != c.want {
					t.Errorf("PDU = %v, want %v", m.pduType, c.want)
				}
				if m.varBinds == 0 {
					t.Error("a PDU with no varbinds in it")
				}
			}
			if c.folder == "error-response" {
				// The whole point of this folder: the agent said no,
				// and the decoder must carry that rather than
				// presenting an empty value as data.
				var sawError bool
				for _, m := range msgs {
					if m.errStatus != 0 {
						sawError = true
					}
				}
				if !sawError {
					t.Error("no error-status in the refusals capture")
				}
			}
		})
	}
}

func TestTheWholeSessionDecodes(t *testing.T) {
	// The golden scenario: get, walk, set, refused set, a v2c read.
	msgs := decodeAll(t, filepath.Join(replayRoot, "fixtures", "dr5000-snmp.pcapng"))
	if len(msgs) < 30 {
		t.Fatalf("the captured session is %d messages", len(msgs))
	}
	seen := map[PDUType]int{}
	versions := map[Version]int{}
	for _, m := range msgs {
		seen[m.pduType]++
		versions[m.version]++
	}
	for _, want := range []PDUType{PDUTypeGet, PDUTypeGetNext, PDUTypeResponse, PDUTypeSet} {
		if seen[want] == 0 {
			t.Errorf("the session carries no %v", want)
		}
	}
	if versions[Version1] == 0 || versions[Version2c] == 0 {
		t.Errorf("the session should exercise both versions: %v", versions)
	}
}

func TestACapturedMessageReEncodesToItself(t *testing.T) {
	// A codec that decodes but cannot reproduce the bytes is half a
	// codec: the provider has to put the same message back on the wire.
	for _, payload := range pcapngUDPPayloads(t, filepath.Join(replayRoot, "protocol_types", "get-request", "wire.pcapng")) {
		m, err := Decode(payload)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		again, err := Encode(m)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if got, want := fmt.Sprintf("%x", again), fmt.Sprintf("%x", payload); got != want {
			t.Errorf("re-encode differs:\n got %s\nwant %s", got, want)
		}
	}
}
