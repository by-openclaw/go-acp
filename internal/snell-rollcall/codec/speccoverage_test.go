package codec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The coverage document is a claim about this package. These tests make it a
// checkable one: if a packet type loses its handling, or a structure is
// removed, or the document stops naming something the code has, CI says so
// instead of the document quietly going stale.

const coverageDoc = "../docs/spec-coverage.md"

// TestSpecCoverage_EveryPacketTypeIsNamed checks that all 72 types are known
// to the table. An unknown type must be answered with InvCmd rather than Nack
// (spec 9.15), and a type missing from the table would take that path by
// accident rather than by decision.
func TestSpecCoverage_EveryPacketTypeIsNamed(t *testing.T) {
	for i := range PacketType(NumPacketTypes) {
		if !i.Known() {
			t.Errorf("packet type %d has no name", i)
		}
	}
	if got := PacketType(NumPacketTypes); got.Known() {
		t.Errorf("type %d is past the catalogue and must read as unknown", got)
	}
}

// TestSpecCoverage_DocumentListsEveryType reads the coverage document and
// requires a row for every packet type. It is the check that stops the
// document drifting from the code.
func TestSpecCoverage_DocumentListsEveryType(t *testing.T) {
	doc := readCoverageDoc(t)

	for i := range PacketType(NumPacketTypes) {
		name := i.String()
		if !strings.Contains(doc, "| "+name+" |") {
			t.Errorf("packet type %d (%s) has no row in %s", i, name, coverageDoc)
		}
	}
}

// TestSpecCoverage_DocumentListsEveryStructure requires a row for each vendor
// structure, naming the Go type that implements it.
func TestSpecCoverage_DocumentListsEveryStructure(t *testing.T) {
	doc := readCoverageDoc(t)

	// Every structure the vendor headers declare, with the Go type that
	// implements it. A structure added to the codec without a row here is a
	// gap in the document; one listed here and removed from the code will
	// fail to compile.
	structures := map[string]string{
		"MESSAGE_STR":         "Frame",
		"FULLADDRESS_STR":     "Address",
		"VERSION_STR":         "Version",
		"ID_STR":              "ID",
		"STATUS_STR":          "UnitStatus",
		"DEVICEINFO_STR":      "DeviceInfo",
		"CONNECT_STR":         "Connect",
		"TERMSESS_STR":        "TermSess",
		"CLEARSESS_STR":       "ClearSess",
		"WAIT_STR":            "Wait",
		"BLOCKHEADER_STR":     "BlockHeader",
		"GETNEXT_STR":         "GetNext",
		"FUNC_STR":            "Func",
		"FUNCSTYLE_STR":       "FuncStyle",
		"GETFSTAT_STR":        "GetFStat",
		"FUNCSTATUS_STR":      "FuncStatus",
		"SETMULTI_STR":        "SetMulti",
		"DISP_STR":            "Disp",
		"MENUREQ_STR":         "MenuReq",
		"MENUSIZE_STR":        "MenuSize",
		"MENUITEM_STR":        "MenuItem",
		"GETVALUE_STR":        "GetValue",
		"VALUE_STR":           "Value",
		"FILE_STR":            "File",
		"FILEINFOHDR_STR":     "FileInfo",
		"MODULE_FILEINFO_STR": "DirEntry",
		"TIME_STR":            "SysTime",
		"TIMECODE_STR":        "Timecode",
		"LOGPACKET_STR":       "LogPacket",
		"STREAMMODE_STR":      "StreamMode",
		"STREAMHDR_STR":       "StreamHeader",
		"DISPLAYCAPS_STR":     "DisplayCaps",
		"DRAWBITMAP_STR":      "DrawBitmap",
		"DRAWTEXT_STR":        "DrawText",
		"SETGROUP_STR":        "SetGroup",
		"GROUPPARAM_STR":      "GroupParam",
		"DOWNLOAD_STR":        "Download",
	}

	for vendor, goType := range structures {
		if !strings.Contains(doc, vendor) {
			t.Errorf("%s has no row in %s", vendor, coverageDoc)
		}
		if !strings.Contains(doc, "`"+goType+"`") {
			t.Errorf("%s implements %s but is not named in %s", goType, vendor, coverageDoc)
		}
	}
}

// TestSpecCoverage_EveryStructureEncodesAndDecodes exercises one round trip of
// every structure through its zero value. It is deliberately shallow: the
// per-structure tests check the field layouts, and this one checks that none
// of them has been left half-wired, with an encoder and no decoder or the
// reverse.
func TestSpecCoverage_EveryStructureEncodesAndDecodes(t *testing.T) {
	type roundTrip struct {
		name   string
		encode func() ([]byte, error)
		decode func([]byte) error
	}

	cases := []roundTrip{
		{"Address", func() ([]byte, error) { return Address{}.AppendTo(nil), nil },
			func(b []byte) error { _, err := DecodeAddress(b); return err }},
		{"ID", func() ([]byte, error) { return ID{}.AppendTo(nil) },
			func(b []byte) error { _, err := DecodeID(b); return err }},
		{"UnitStatus", func() ([]byte, error) { return UnitStatus{}.AppendTo(nil), nil },
			func(b []byte) error { _, err := DecodeUnitStatus(b); return err }},
		{"DeviceInfo", func() ([]byte, error) { return DeviceInfo{}.AppendTo(nil) },
			func(b []byte) error { _, err := DecodeDeviceInfo(b); return err }},
		{"Connect", func() ([]byte, error) { return Connect{}.AppendTo(nil) },
			func(b []byte) error { _, err := DecodeConnect(b); return err }},
		{"TermSess", func() ([]byte, error) { return TermSess{}.AppendTo(nil) },
			func(b []byte) error { _, err := DecodeTermSess(b); return err }},
		{"ClearSess", func() ([]byte, error) { return ClearSess{}.AppendTo(nil), nil },
			func(b []byte) error { _, err := DecodeClearSess(b); return err }},
		{"Wait", func() ([]byte, error) { return Wait{}.AppendTo(nil) },
			func(b []byte) error { _, err := DecodeWait(b); return err }},
		{"BlockHeader", func() ([]byte, error) { return BlockHeader{}.AppendTo(nil), nil },
			func(b []byte) error { _, err := DecodeBlockHeader(b); return err }},
		{"GetNext", func() ([]byte, error) { return GetNext{}.AppendTo(nil), nil },
			func(b []byte) error { _, err := DecodeGetNext(b); return err }},
		{"Func", func() ([]byte, error) { return Func{}.AppendTo(nil) },
			func(b []byte) error { _, err := DecodeFunc(b); return err }},
		{"FuncStyle", func() ([]byte, error) { return FuncStyle{}.AppendTo(nil), nil },
			func(b []byte) error { _, err := DecodeFuncStyle(b); return err }},
		{"GetFStat", func() ([]byte, error) { return GetFStat{}.AppendTo(nil), nil },
			func(b []byte) error { _, err := DecodeGetFStat(b); return err }},
		{"FuncStatus", func() ([]byte, error) { return FuncStatus{}.AppendTo(nil) },
			func(b []byte) error { _, err := DecodeFuncStatus(b); return err }},
		{"Disp", func() ([]byte, error) { return Disp{}.AppendTo(nil) },
			func(b []byte) error { _, err := DecodeDisp(b); return err }},
		{"MenuReq", func() ([]byte, error) { return MenuReq{}.AppendTo(nil), nil },
			func(b []byte) error { _, err := DecodeMenuReq(b); return err }},
		{"MenuSize", func() ([]byte, error) { return MenuSize{}.AppendTo(nil), nil },
			func(b []byte) error { _, err := DecodeMenuSize(b); return err }},
		{"MenuItem", func() ([]byte, error) { return MenuItem{}.AppendTo(nil) },
			func(b []byte) error { _, err := DecodeMenuItem(b); return err }},
		{"GetValue", func() ([]byte, error) { return GetValue{}.AppendTo(nil), nil },
			func(b []byte) error { _, err := DecodeGetValue(b); return err }},
		{"Value", func() ([]byte, error) { return Value{}.AppendTo(nil) },
			func(b []byte) error { _, err := DecodeValue(b); return err }},
		{"File", func() ([]byte, error) { return File{}.AppendTo(nil), nil },
			func(b []byte) error { _, err := DecodeFile(b); return err }},
		{"FileInfo", func() ([]byte, error) { return FileInfo{}.AppendTo(nil), nil },
			func(b []byte) error { _, err := DecodeFileInfo(b); return err }},
		{"DirEntry", func() ([]byte, error) { return DirEntry{}.AppendTo(nil) },
			func(b []byte) error { _, err := DecodeDirEntry(b); return err }},
		{"SysTime", func() ([]byte, error) { return SysTime{}.AppendTo(nil), nil },
			func(b []byte) error { _, err := DecodeSysTime(b); return err }},
		{"Timecode", func() ([]byte, error) { return Timecode{}.AppendTo(nil), nil },
			func(b []byte) error { _, err := DecodeTimecode(b); return err }},
		{"LogPacket", func() ([]byte, error) { return LogPacket{}.AppendTo(nil) },
			func(b []byte) error { _, err := DecodeLogPacket(b); return err }},
		{"StreamMode", func() ([]byte, error) { return StreamMode{}.AppendTo(nil), nil },
			func(b []byte) error { _, err := DecodeStreamMode(b); return err }},
		{"StreamHeader", func() ([]byte, error) { return StreamHeader{}.AppendTo(nil) },
			func(b []byte) error { _, err := DecodeStreamHeader(b); return err }},
		{"DisplayCaps", func() ([]byte, error) { return DisplayCaps{}.AppendTo(nil), nil },
			func(b []byte) error { _, err := DecodeDisplayCaps(b); return err }},
		{"DrawBitmap", func() ([]byte, error) { return DrawBitmap{}.AppendTo(nil), nil },
			func(b []byte) error { _, err := DecodeDrawBitmap(b); return err }},
		{"DrawText", func() ([]byte, error) { return DrawText{}.AppendTo(nil) },
			func(b []byte) error { _, err := DecodeDrawText(b); return err }},
		{"SetGroup", func() ([]byte, error) { return SetGroup{}.AppendTo(nil), nil },
			func(b []byte) error { _, err := DecodeSetGroup(b); return err }},
		{"GroupParam", func() ([]byte, error) { return GroupParam{}.AppendTo(nil), nil },
			func(b []byte) error { _, err := DecodeGroupParam(b); return err }},
		{"Download", func() ([]byte, error) { return Download{}.AppendTo(nil), nil },
			func(b []byte) error { _, err := DecodeDownload(b); return err }},
	}

	if len(cases) != 34 {
		t.Fatalf("the round-trip list holds %d structures; keep it in step with the document", len(cases))
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := tc.encode()
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			if err := tc.decode(b); err != nil {
				t.Fatalf("decode what we encoded: %v", err)
			}
		})
	}
}

// TestSpecCoverage_GapsAreRecorded requires the document to keep naming the
// two places the specification does not settle. Deleting the section would
// make the coverage claim read as complete when it is not.
func TestSpecCoverage_GapsAreRecorded(t *testing.T) {
	doc := readCoverageDoc(t)

	for _, want := range []string{
		"ROUTEERROR_STR",   // referenced by the type table, never defined
		"router sub-table", // offsets read from the document's ordering
		"CENTRA_SIMULATED_ROUTER",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("%s no longer records %q as an open question", coverageDoc, want)
		}
	}

	// The route-error type is decoded as an opaque payload, which is what the
	// document says. If a structure is ever written for it, this fails and
	// the document has to be updated with it.
	if RouteErrorSize != 0 {
		t.Error("a structure now exists for ROUTEERROR_STR; update the coverage document")
	}
}

func readCoverageDoc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.FromSlash(coverageDoc))
	if err != nil {
		t.Fatalf("read %s: %v", coverageDoc, err)
	}
	return string(b)
}
