package codec

import "testing"

// TestPacketType_Catalogue pins the numbering of the whole message catalogue
// against spec 9. A wrong number here is the kind of defect that only shows up
// as a device answering INVCMD to a command it does implement, so every value
// is written out rather than derived.
func TestPacketType_Catalogue(t *testing.T) {
	want := map[PacketType]string{
		0: "NACK", 1: "ACK", 2: "CALL", 3: "TERM", 4: "GETSTAT", 5: "RETSTAT",
		6: "GETID", 7: "RETID", 8: "GETFUNC", 9: "RETFUNC", 10: "DISPDATA",
		11: "GETFSTAT", 12: "RETFSTAT", 13: "RESET", 14: "INVCMD", 15: "BUSY",
		16: "SETPARAM", 17: "TIME", 18: "FUNCLISTCHG", 19: "GETDEVLIST",
		20: "RETDEVINFO", 21: "GETDEVINFO", 22: "LOGREQ", 23: "INVSESS",
		24: "REALTIME", 25: "WAIT", 26: "CLEARSESS", 27: "BKCHNREADY",
		28: "KEEPALIVE", 29: "GETLOCDEVMAP", 30: "FUNCSTYLECHG", 31: "SETGROUP",
		32: "STOPGROUP", 33: "IAM", 34: "SETGRPFUNC", 35: "GETNEXTPKT",
		36: "REPFCHG", 37: "STOPREPFCHG", 38: "ROUTEERROR", 39: "BLOCKHEADER",
		40: "STREAMMODE", 41: "STREAMDATA", 42: "FILEDIR", 43: "RETFILEDIR",
		44: "RAW", 45: "SETUSERLEVEL", 46: "FILEDELETE", 47: "GETTIME",
		48: "FILERENAME", 49: "LOGDATA", 50: "GETSRVBYNAME", 51: "FILEOPEN",
		52: "RETFILEOPEN", 53: "FILECLOSE", 54: "GETDISPDATA", 55: "FILEREAD",
		56: "RETFILEREAD", 57: "FILEWRITE", 58: "SETMULTI", 59: "FILERET",
		60: "GETDISPCAPS", 61: "RETDISPCAPS", 62: "DRAWBITMAP", 63: "DRAWTEXT",
		64: "MAKEDIRECTORY", 65: "GETMENUCOUNT", 66: "RETMENUCOUNT",
		67: "GETMENUITEM", 68: "RETMENUITEM", 69: "GETVALUE", 70: "SETVALUE",
		71: "RETVALUE",
	}

	if len(want) != NumPacketTypes {
		t.Fatalf("table has %d entries, NumPacketTypes is %d", len(want), NumPacketTypes)
	}
	for typ, name := range want {
		if got := typ.String(); got != name {
			t.Errorf("type %d = %q, want %q", typ, got, name)
		}
		if !typ.Known() {
			t.Errorf("type %d (%s) reports unknown", typ, name)
		}
	}
}

func TestPacketType_Unknown(t *testing.T) {
	for _, typ := range []PacketType{NumPacketTypes, 100, 255} {
		if typ.Known() {
			t.Errorf("type %d reports known", typ)
		}
		if got, want := typ.String(), "<"+itoaInt(int(typ))+">"; got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
		// An unknown type has no properties, so every predicate is false.
		if typ.Broadcastable() || typ.ValidBlindReply() || typ.Gen32() {
			t.Errorf("type %d claims a property it cannot have", typ)
		}
	}
}

func itoaInt(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [4]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// TestPacketType_Broadcastable pins spec 5.5: only the two announcement types
// may be addressed to 0000-00-00. Accepting any other type from the broadcast
// address is how a rogue frame reaches a session it was never part of.
func TestPacketType_Broadcastable(t *testing.T) {
	broadcast := map[PacketType]bool{MsgTime: true, MsgIam: true}
	for i := range PacketType(NumPacketTypes) {
		if got := i.Broadcastable(); got != broadcast[i] {
			t.Errorf("%s Broadcastable = %v, want %v", i, got, broadcast[i])
		}
	}
}

// TestPacketType_Gen32 pins the 2014 extension boundary: types 65..71 exist
// only when both ends negotiated SvcLongStr.
func TestPacketType_Gen32(t *testing.T) {
	for i := range PacketType(NumPacketTypes) {
		want := i >= MsgGetMenuCount
		if got := i.Gen32(); got != want {
			t.Errorf("%s Gen32 = %v, want %v", i, got, want)
		}
	}
}

// TestPacketType_ValidBlindReply pins which replies may be correlated by
// address when there is no session to correlate by (spec 5.4, 9.17).
func TestPacketType_ValidBlindReply(t *testing.T) {
	want := map[PacketType]bool{
		MsgNack: true, MsgAck: true, MsgRetStat: true, MsgRetID: true,
		MsgRetFStat: true, MsgInvCmd: true, MsgBusy: true, MsgTime: true,
		MsgRetDevInfo: true, MsgInvSess: true, MsgWait: true,
		MsgBlockHeader: true, MsgRetDispCaps: true, MsgRetValue: true,
	}
	for i := range PacketType(NumPacketTypes) {
		if got := i.ValidBlindReply(); got != want[i] {
			t.Errorf("%s ValidBlindReply = %v, want %v", i, got, want[i])
		}
	}
	// A request is never a reply.
	for _, typ := range []PacketType{MsgCall, MsgGetStat, MsgSetParam, MsgGetValue} {
		if typ.ValidBlindReply() {
			t.Errorf("%s is a request, not a blind reply", typ)
		}
	}
}

func TestPacketTypeByName(t *testing.T) {
	tests := []struct {
		in   string
		want PacketType
		ok   bool
	}{
		{"CALL", MsgCall, true},
		{"SP_CALL", MsgCall, true}, // the spelling used in the spec
		{"SP_RETVALUE", MsgRetValue, true},
		{"NACK", MsgNack, true}, // type 0 must not read as "not found"
		{"call", 0, false},      // lookup is case-sensitive
		{"SP_", 0, false},
		{"", 0, false},
		{"NOSUCHTYPE", 0, false},
	}
	for _, tc := range tests {
		got, ok := PacketTypeByName(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("PacketTypeByName(%q) = %d,%v, want %d,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// TestPacketTypeByName_RoundTripsEveryName guarantees the name table has no
// duplicates and that every type can be named on a CLI flag or trace filter.
func TestPacketTypeByName_RoundTripsEveryName(t *testing.T) {
	for i := range PacketType(NumPacketTypes) {
		got, ok := PacketTypeByName(i.String())
		if !ok {
			t.Errorf("%s does not resolve back to a type", i)
			continue
		}
		if got != i {
			t.Errorf("name %q resolves to %d, want %d (duplicate name)", i.String(), got, i)
		}
	}
}
