package codec

import (
	"errors"
	"testing"
)

// This file pins the error arms of every per-command Decode* helper:
// the wrong-command guard, the short-payload guard(s) on each wire form
// (general and extended), and the per-command structural guards
// (invalid NameLength, variable-length underrun, unknown enum). Expected
// errors come from the SW-P-08 decoder contract — ErrWrongCommand when
// the frame ID does not match, ErrShortPayload when the payload is
// shorter than the form's fixed minimum, and a descriptive error from
// the per-command guards. No production guard is exercised by accident:
// each case crafts the exact malformed frame that reaches one arm.

// wrongID is a command byte no Decode* in this package accepts as its
// own — used to drive the default ErrWrongCommand arm.
const wrongID CommandID = 0xEE

func wantWrong(t *testing.T, name string, err error) {
	t.Helper()
	if !errors.Is(err, ErrWrongCommand) {
		t.Errorf("%s: err = %v; want ErrWrongCommand", name, err)
	}
}

func wantShort(t *testing.T, name string, err error) {
	t.Helper()
	if !errors.Is(err, ErrShortPayload) {
		t.Errorf("%s: err = %v; want ErrShortPayload", name, err)
	}
}

func wantErr(t *testing.T, name string, err error) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: err = nil; want non-nil", name)
	}
}

// --- rx 002 / rx 130 Crosspoint Connect ------------------------------------

func TestDecodeCrosspointConnect_ErrorArms(t *testing.T) {
	_, err := DecodeCrosspointConnect(Frame{ID: RxCrosspointConnectExt, Payload: []byte{0, 0, 0, 0, 0}})
	wantShort(t, "connect ext short", err)
	_, err = DecodeCrosspointConnect(Frame{ID: wrongID})
	wantWrong(t, "connect wrong", err)
}

// --- rx 010 / rx 138 Protect Interrogate -----------------------------------

func TestDecodeProtectInterrogate_ErrorArms(t *testing.T) {
	_, err := DecodeProtectInterrogate(Frame{ID: RxProtectInterrogate, Payload: []byte{0, 0}})
	wantShort(t, "pi general short", err)
	_, err = DecodeProtectInterrogate(Frame{ID: RxProtectInterrogateExt, Payload: []byte{0, 0, 0}})
	wantShort(t, "pi ext short", err)
	_, err = DecodeProtectInterrogate(Frame{ID: wrongID})
	wantWrong(t, "pi wrong", err)
}

// --- rx 012 / rx 140 Protect Connect ---------------------------------------

func TestDecodeProtectConnect_ErrorArms(t *testing.T) {
	_, err := DecodeProtectConnect(Frame{ID: RxProtectConnect, Payload: []byte{0, 0, 0}})
	wantShort(t, "pc general short", err)
	_, err = DecodeProtectConnect(Frame{ID: RxProtectConnectExt, Payload: []byte{0, 0, 0, 0, 0}})
	wantShort(t, "pc ext short", err)
	_, err = DecodeProtectConnect(Frame{ID: wrongID})
	wantWrong(t, "pc wrong", err)
}

// --- rx 014 / rx 142 Protect Disconnect ------------------------------------

func TestDecodeProtectDisconnect_ErrorArms(t *testing.T) {
	// Wrong-command default arm.
	_, err := DecodeProtectDisconnect(Frame{ID: wrongID})
	wantWrong(t, "pd wrong", err)
	// General + ext short (rewritten to connect IDs internally).
	_, err = DecodeProtectDisconnect(Frame{ID: RxProtectDisconnect, Payload: []byte{0, 0, 0}})
	wantShort(t, "pd general short", err)
	_, err = DecodeProtectDisconnect(Frame{ID: RxProtectDisconnectExt, Payload: []byte{0, 0, 0, 0, 0}})
	wantShort(t, "pd ext short", err)
}

// --- rx 017 Protect Device Name Request ------------------------------------

func TestDecodeProtectDeviceNameRequest_ErrorArms(t *testing.T) {
	_, err := DecodeProtectDeviceNameRequest(Frame{ID: wrongID})
	wantWrong(t, "pdnr wrong", err)
	_, err = DecodeProtectDeviceNameRequest(Frame{ID: RxProtectDeviceNameRequest, Payload: []byte{0}})
	wantShort(t, "pdnr short", err)
}

// --- rx 029 Master Protect Connect -----------------------------------------

func TestDecodeMasterProtectConnect_ErrorArms(t *testing.T) {
	_, err := DecodeMasterProtectConnect(Frame{ID: wrongID})
	wantWrong(t, "mpc wrong", err)
	_, err = DecodeMasterProtectConnect(Frame{ID: RxMasterProtectConnect, Payload: []byte{0, 0, 0, 0, 0}})
	wantShort(t, "mpc short", err)
}

// --- rx 034 App Keepalive Response -----------------------------------------

func TestDecodeKeepaliveResponse_ErrorArms(t *testing.T) {
	if err := DecodeKeepaliveResponse(Frame{ID: wrongID}); err == nil {
		t.Error("keepalive resp wrong id: err = nil; want non-nil")
	}
	if err := DecodeKeepaliveResponse(Frame{ID: RxAppKeepaliveResponse, Payload: []byte{0x00}}); err == nil {
		t.Error("keepalive resp non-empty: err = nil; want non-nil")
	}
	// Happy path so the success return is exercised here too.
	if err := DecodeKeepaliveResponse(EncodeKeepaliveResponse()); err != nil {
		t.Errorf("keepalive resp valid: %v", err)
	}
}

// --- rx 019 / rx 147 Protect Tally Dump Request ----------------------------

func TestDecodeProtectTallyDumpRequest_ErrorArms(t *testing.T) {
	_, err := DecodeProtectTallyDumpRequest(Frame{ID: RxProtectTallyDumpRequest, Payload: []byte{0, 0}})
	wantShort(t, "ptdr general short", err)
	_, err = DecodeProtectTallyDumpRequest(Frame{ID: RxProtectTallyDumpRequestExt, Payload: []byte{0, 0, 0}})
	wantShort(t, "ptdr ext short", err)
	_, err = DecodeProtectTallyDumpRequest(Frame{ID: wrongID})
	wantWrong(t, "ptdr wrong", err)
}

// --- rx 021 / rx 149 Crosspoint Tally Dump Request -------------------------

func TestDecodeCrosspointTallyDumpRequest_ErrorArms(t *testing.T) {
	_, err := DecodeCrosspointTallyDumpRequest(Frame{ID: RxCrosspointTallyDumpRequest, Payload: nil})
	wantShort(t, "ctdr general short", err)
	_, err = DecodeCrosspointTallyDumpRequest(Frame{ID: RxCrosspointTallyDumpRequestExt, Payload: []byte{0}})
	wantShort(t, "ctdr ext short", err)
	_, err = DecodeCrosspointTallyDumpRequest(Frame{ID: wrongID})
	wantWrong(t, "ctdr wrong", err)
}

// --- rx 100 / rx 228 All Source Names Request ------------------------------

func TestDecodeAllSourceNamesRequest_ErrorArms(t *testing.T) {
	_, err := DecodeAllSourceNamesRequest(Frame{ID: RxAllSourceNamesRequest, Payload: []byte{0}})
	wantShort(t, "asn general short", err)
	// Invalid NameLength (0x05) on general form.
	_, err = DecodeAllSourceNamesRequest(Frame{ID: RxAllSourceNamesRequest, Payload: []byte{0, 0x05}})
	wantErr(t, "asn general bad namelen", err)
	_, err = DecodeAllSourceNamesRequest(Frame{ID: RxAllSourceNamesRequestExt, Payload: []byte{0, 0}})
	wantShort(t, "asn ext short", err)
	_, err = DecodeAllSourceNamesRequest(Frame{ID: RxAllSourceNamesRequestExt, Payload: []byte{0, 0, 0x05}})
	wantErr(t, "asn ext bad namelen", err)
	_, err = DecodeAllSourceNamesRequest(Frame{ID: wrongID})
	wantWrong(t, "asn wrong", err)
}

// --- rx 101 / rx 229 Single Source Name Request ----------------------------

func TestDecodeSingleSourceNameRequest_ErrorArms(t *testing.T) {
	_, err := DecodeSingleSourceNameRequest(Frame{ID: RxSingleSourceNameRequest, Payload: []byte{0, 0, 0}})
	wantShort(t, "ssn general short", err)
	_, err = DecodeSingleSourceNameRequest(Frame{ID: RxSingleSourceNameRequest, Payload: []byte{0, 0x05, 0, 0}})
	wantErr(t, "ssn general bad namelen", err)
	_, err = DecodeSingleSourceNameRequest(Frame{ID: RxSingleSourceNameRequestExt, Payload: []byte{0, 0, 0, 0}})
	wantShort(t, "ssn ext short", err)
	_, err = DecodeSingleSourceNameRequest(Frame{ID: RxSingleSourceNameRequestExt, Payload: []byte{0, 0, 0x05, 0, 0}})
	wantErr(t, "ssn ext bad namelen", err)
	_, err = DecodeSingleSourceNameRequest(Frame{ID: wrongID})
	wantWrong(t, "ssn wrong", err)
}

// --- rx 102 / rx 230 All Dest Assoc Names Request --------------------------

func TestDecodeAllDestAssocNamesRequest_ErrorArms(t *testing.T) {
	_, err := DecodeAllDestAssocNamesRequest(Frame{ID: RxAllDestNamesRequest, Payload: []byte{0}})
	wantShort(t, "adan general short", err)
	_, err = DecodeAllDestAssocNamesRequest(Frame{ID: RxAllDestNamesRequest, Payload: []byte{0, 0x05}})
	wantErr(t, "adan general bad namelen", err)
	_, err = DecodeAllDestAssocNamesRequest(Frame{ID: RxAllDestNamesRequestExt, Payload: []byte{0}})
	wantShort(t, "adan ext short", err)
	_, err = DecodeAllDestAssocNamesRequest(Frame{ID: RxAllDestNamesRequestExt, Payload: []byte{0, 0x05}})
	wantErr(t, "adan ext bad namelen", err)
	_, err = DecodeAllDestAssocNamesRequest(Frame{ID: wrongID})
	wantWrong(t, "adan wrong", err)
}

// --- rx 103 / rx 231 Single Dest Assoc Name Request ------------------------

func TestDecodeSingleDestAssocNameRequest_ErrorArms(t *testing.T) {
	_, err := DecodeSingleDestAssocNameRequest(Frame{ID: RxSingleDestNameRequest, Payload: []byte{0, 0, 0}})
	wantShort(t, "sdan general short", err)
	_, err = DecodeSingleDestAssocNameRequest(Frame{ID: RxSingleDestNameRequest, Payload: []byte{0, 0x05, 0, 0}})
	wantErr(t, "sdan general bad namelen", err)
	_, err = DecodeSingleDestAssocNameRequest(Frame{ID: RxSingleDestNameRequestExt, Payload: []byte{0, 0, 0}})
	wantShort(t, "sdan ext short", err)
	_, err = DecodeSingleDestAssocNameRequest(Frame{ID: RxSingleDestNameRequestExt, Payload: []byte{0, 0x05, 0, 0}})
	wantErr(t, "sdan ext bad namelen", err)
	_, err = DecodeSingleDestAssocNameRequest(Frame{ID: wrongID})
	wantWrong(t, "sdan wrong", err)
}

// --- rx 112 Crosspoint Tie Line Interrogate --------------------------------

func TestDecodeTieLineInterrogate_ErrorArms(t *testing.T) {
	_, err := DecodeTieLineInterrogate(Frame{ID: wrongID})
	wantWrong(t, "tli wrong", err)
	_, err = DecodeTieLineInterrogate(Frame{ID: RxCrosspointTieLineInterrogate, Payload: []byte{0, 0}})
	wantShort(t, "tli short", err)
}

// --- rx 114 All Source Assoc Names Request ---------------------------------

func TestDecodeAllSourceAssocNamesRequest_ErrorArms(t *testing.T) {
	_, err := DecodeAllSourceAssocNamesRequest(Frame{ID: wrongID})
	wantWrong(t, "asan wrong", err)
	_, err = DecodeAllSourceAssocNamesRequest(Frame{ID: RxAllSourceAssocNamesRequest, Payload: []byte{0}})
	wantShort(t, "asan short", err)
	_, err = DecodeAllSourceAssocNamesRequest(Frame{ID: RxAllSourceAssocNamesRequest, Payload: []byte{0, 0x05}})
	wantErr(t, "asan bad namelen", err)
}

// --- rx 115 Single Source Assoc Name Request -------------------------------

func TestDecodeSingleSourceAssocNameRequest_ErrorArms(t *testing.T) {
	_, err := DecodeSingleSourceAssocNameRequest(Frame{ID: wrongID})
	wantWrong(t, "ssan wrong", err)
	_, err = DecodeSingleSourceAssocNameRequest(Frame{ID: RxSingleSourceAssocNameRequest, Payload: []byte{0, 0, 0}})
	wantShort(t, "ssan short", err)
	_, err = DecodeSingleSourceAssocNameRequest(Frame{ID: RxSingleSourceAssocNameRequest, Payload: []byte{0, 0x05, 0, 0}})
	wantErr(t, "ssan bad namelen", err)
}

// --- rx 117 Update Name Request --------------------------------------------

func TestDecodeUpdateNameRequest_ErrorArms(t *testing.T) {
	_, err := DecodeUpdateNameRequest(Frame{ID: wrongID})
	wantWrong(t, "unr wrong", err)
	_, err = DecodeUpdateNameRequest(Frame{ID: RxUpdateNameRequest, Payload: []byte{0, 0, 0, 0, 0, 0}})
	wantShort(t, "unr short header", err)
	// Invalid NameLength (0x05) — validateNameLengthExt rejects.
	_, err = DecodeUpdateNameRequest(Frame{ID: RxUpdateNameRequest, Payload: []byte{0x00, 0x05, 0, 0, 0, 0, 0}})
	wantErr(t, "unr bad namelen", err)
	// Unknown NameType (0x09) with valid NameLength.
	_, err = DecodeUpdateNameRequest(Frame{ID: RxUpdateNameRequest, Payload: []byte{0x09, 0x00, 0, 0, 0, 0, 0}})
	wantErr(t, "unr bad nametype", err)
	// Variable-length underrun: count=2 names @ 4 bytes but payload too short.
	_, err = DecodeUpdateNameRequest(Frame{ID: RxUpdateNameRequest, Payload: []byte{0x00, 0x00, 0, 0, 0, 0, 0x02}})
	wantErr(t, "unr name underrun", err)
}

// --- rx 120 / rx 248 Salvo Connect On Go -----------------------------------

func TestDecodeSalvoConnectOnGo_ErrorArms(t *testing.T) {
	_, err := DecodeSalvoConnectOnGo(Frame{ID: RxCrosspointConnectOnGoSalvo, Payload: []byte{0, 0, 0, 0}})
	wantShort(t, "scog general short", err)
	_, err = DecodeSalvoConnectOnGo(Frame{ID: RxCrosspointConnectOnGoSalvoExt, Payload: []byte{0, 0, 0, 0, 0, 0}})
	wantShort(t, "scog ext short", err)
	_, err = DecodeSalvoConnectOnGo(Frame{ID: wrongID})
	wantWrong(t, "scog wrong", err)
}

// --- rx 121 Salvo Go -------------------------------------------------------

func TestDecodeSalvoGo_ErrorArms(t *testing.T) {
	_, err := DecodeSalvoGo(Frame{ID: wrongID})
	wantWrong(t, "sg wrong", err)
	_, err = DecodeSalvoGo(Frame{ID: RxCrosspointGoSalvo, Payload: []byte{0}})
	wantShort(t, "sg short", err)
	// Unknown op (0x07).
	_, err = DecodeSalvoGo(Frame{ID: RxCrosspointGoSalvo, Payload: []byte{0x07, 0x00}})
	wantErr(t, "sg bad op", err)
}

// --- rx 124 / rx 252 Salvo Group Interrogate -------------------------------

func TestDecodeSalvoGroupInterrogate_ErrorArms(t *testing.T) {
	_, err := DecodeSalvoGroupInterrogate(Frame{ID: RxCrosspointSalvoGroupInterrogate, Payload: []byte{0}})
	wantShort(t, "sgi general short", err)
	_, err = DecodeSalvoGroupInterrogate(Frame{ID: RxCrosspointSalvoGroupInterrogateExt, Payload: []byte{0, 0}})
	wantShort(t, "sgi ext short", err)
	_, err = DecodeSalvoGroupInterrogate(Frame{ID: wrongID})
	wantWrong(t, "sgi wrong", err)
}

// --- tx 003 / tx 131 Crosspoint Tally --------------------------------------

func TestDecodeCrosspointTally_ErrorArms(t *testing.T) {
	_, err := DecodeCrosspointTally(Frame{ID: TxCrosspointTally, Payload: []byte{0, 0, 0}})
	wantShort(t, "ct general short", err)
	_, err = DecodeCrosspointTally(Frame{ID: TxCrosspointTallyExt, Payload: []byte{0, 0, 0, 0, 0, 0}})
	wantShort(t, "ct ext short", err)
	_, err = DecodeCrosspointTally(Frame{ID: wrongID})
	wantWrong(t, "ct wrong", err)
}

// --- tx 004 / tx 132 Crosspoint Connected ----------------------------------

func TestDecodeCrosspointConnected_ErrorArms(t *testing.T) {
	_, err := DecodeCrosspointConnected(Frame{ID: TxCrosspointConnected, Payload: []byte{0, 0, 0}})
	wantShort(t, "cc general short", err)
	_, err = DecodeCrosspointConnected(Frame{ID: TxCrosspointConnectedExt, Payload: []byte{0, 0, 0, 0, 0, 0}})
	wantShort(t, "cc ext short", err)
	_, err = DecodeCrosspointConnected(Frame{ID: wrongID})
	wantWrong(t, "cc wrong", err)
}

// --- tx 009 Dual Controller Status Response --------------------------------

func TestDecodeDualControllerStatusResponse_ErrorArms(t *testing.T) {
	_, err := DecodeDualControllerStatusResponse(Frame{ID: wrongID})
	wantWrong(t, "dcs wrong", err)
	_, err = DecodeDualControllerStatusResponse(Frame{ID: TxDualControllerStatusResponse, Payload: []byte{0}})
	wantShort(t, "dcs short", err)
}

// --- tx 011 / tx 139 Protect Tally -----------------------------------------

func TestDecodeProtectTally_ErrorArms(t *testing.T) {
	_, err := DecodeProtectTally(Frame{ID: TxProtectTally, Payload: []byte{0, 0, 0, 0}})
	wantShort(t, "pt general short", err)
	_, err = DecodeProtectTally(Frame{ID: TxProtectTallyExt, Payload: []byte{0, 0, 0, 0, 0, 0}})
	wantShort(t, "pt ext short", err)
	_, err = DecodeProtectTally(Frame{ID: wrongID})
	wantWrong(t, "pt wrong", err)
}

// --- tx 013 / tx 141 Protect Connected -------------------------------------

func TestDecodeProtectConnected_ErrorArms(t *testing.T) {
	_, err := DecodeProtectConnected(Frame{ID: wrongID})
	wantWrong(t, "pcd wrong", err)
	_, err = DecodeProtectConnected(Frame{ID: TxProtectConnected, Payload: []byte{0, 0, 0, 0}})
	wantShort(t, "pcd general short", err)
	_, err = DecodeProtectConnected(Frame{ID: TxProtectConnectedExt, Payload: []byte{0, 0, 0, 0, 0, 0}})
	wantShort(t, "pcd ext short", err)
}

// --- tx 015 / tx 143 Protect Disconnected ----------------------------------

func TestDecodeProtectDisconnected_ErrorArms(t *testing.T) {
	_, err := DecodeProtectDisconnected(Frame{ID: wrongID})
	wantWrong(t, "pdd wrong", err)
	_, err = DecodeProtectDisconnected(Frame{ID: TxProtectDisconnected, Payload: []byte{0, 0, 0, 0}})
	wantShort(t, "pdd general short", err)
	_, err = DecodeProtectDisconnected(Frame{ID: TxProtectDisconnectedExt, Payload: []byte{0, 0, 0, 0, 0, 0}})
	wantShort(t, "pdd ext short", err)
}

// --- tx 018 Protect Device Name Response -----------------------------------

func TestDecodeProtectDeviceNameResponse_ErrorArms(t *testing.T) {
	_, err := DecodeProtectDeviceNameResponse(Frame{ID: wrongID})
	wantWrong(t, "pdnresp wrong", err)
	_, err = DecodeProtectDeviceNameResponse(Frame{ID: TxProtectDeviceNameResponse, Payload: []byte{0, 0, 0}})
	wantShort(t, "pdnresp short", err)
}

// --- tx 020 / tx 148 Protect Tally Dump ------------------------------------

func TestDecodeProtectTallyDump_ErrorArms(t *testing.T) {
	_, err := DecodeProtectTallyDump(Frame{ID: TxProtectTallyDump, Payload: []byte{0, 0, 0}})
	wantShort(t, "ptd general short header", err)
	_, err = DecodeProtectTallyDump(Frame{ID: TxProtectTallyDumpExt, Payload: []byte{0, 0, 0, 0}})
	wantShort(t, "ptd ext short header", err)
	_, err = DecodeProtectTallyDump(Frame{ID: wrongID})
	wantWrong(t, "ptd wrong", err)
	// Header says 2 items but body is truncated (general headerLen=4).
	_, err = DecodeProtectTallyDump(Frame{ID: TxProtectTallyDump, Payload: []byte{0, 0x02, 0, 0, 0, 0}})
	wantShort(t, "ptd items underrun", err)
}

// --- tx 022 Crosspoint Tally Dump (byte) -----------------------------------

func TestDecodeCrosspointTallyDumpByte_ErrorArms(t *testing.T) {
	_, err := DecodeCrosspointTallyDumpByte(Frame{ID: wrongID})
	wantWrong(t, "ctdb wrong", err)
	_, err = DecodeCrosspointTallyDumpByte(Frame{ID: TxCrosspointTallyDumpByte, Payload: []byte{0, 0}})
	wantShort(t, "ctdb short header", err)
}

// --- tx 023 / tx 151 Crosspoint Tally Dump (word) --------------------------

func TestDecodeCrosspointTallyDumpWord_ErrorArms(t *testing.T) {
	_, err := DecodeCrosspointTallyDumpWord(Frame{ID: TxCrosspointTallyDumpWord, Payload: []byte{0, 0, 0}})
	wantShort(t, "ctdw general short header", err)
	_, err = DecodeCrosspointTallyDumpWord(Frame{ID: TxCrosspointTallyDumpWordExt, Payload: []byte{0, 0, 0, 0}})
	wantShort(t, "ctdw ext short header", err)
	_, err = DecodeCrosspointTallyDumpWord(Frame{ID: wrongID})
	wantWrong(t, "ctdw wrong", err)
	// Header says 2 tallies but body truncated.
	_, err = DecodeCrosspointTallyDumpWord(Frame{ID: TxCrosspointTallyDumpWord, Payload: []byte{0, 0x02, 0, 0}})
	wantShort(t, "ctdw tallies underrun", err)
}

// --- tx 106 / tx 234 Source Names Response ---------------------------------

func TestDecodeSourceNamesResponse_ErrorArms(t *testing.T) {
	_, err := DecodeSourceNamesResponse(Frame{ID: TxSourceNamesResponse, Payload: []byte{0, 0, 0, 0}})
	wantShort(t, "snr general short", err)
	_, err = DecodeSourceNamesResponse(Frame{ID: TxSourceNamesResponse, Payload: []byte{0, 0x05, 0, 0, 0}})
	wantErr(t, "snr general bad namelen", err)
	// count=1 name @ 4 bytes but body truncated (general headerLen=5).
	_, err = DecodeSourceNamesResponse(Frame{ID: TxSourceNamesResponse, Payload: []byte{0, 0x00, 0, 0, 0x01}})
	wantErr(t, "snr general name underrun", err)
	_, err = DecodeSourceNamesResponse(Frame{ID: TxSourceNamesResponseExt, Payload: []byte{0, 0, 0, 0, 0}})
	wantShort(t, "snr ext short", err)
	_, err = DecodeSourceNamesResponse(Frame{ID: TxSourceNamesResponseExt, Payload: []byte{0, 0, 0x05, 0, 0, 0}})
	wantErr(t, "snr ext bad namelen", err)
	_, err = DecodeSourceNamesResponse(Frame{ID: TxSourceNamesResponseExt, Payload: []byte{0, 0, 0x00, 0, 0, 0x01}})
	wantErr(t, "snr ext name underrun", err)
	_, err = DecodeSourceNamesResponse(Frame{ID: wrongID})
	wantWrong(t, "snr wrong", err)
}

// --- tx 107 / tx 235 Dest Assoc Names Response -----------------------------

func TestDecodeDestAssocNamesResponse_ErrorArms(t *testing.T) {
	_, err := DecodeDestAssocNamesResponse(Frame{ID: TxDestAssocNamesResponse, Payload: []byte{0, 0, 0, 0}})
	wantShort(t, "danr general short", err)
	_, err = DecodeDestAssocNamesResponse(Frame{ID: TxDestAssocNamesResponse, Payload: []byte{0, 0x05, 0, 0, 0}})
	wantErr(t, "danr general bad namelen", err)
	_, err = DecodeDestAssocNamesResponse(Frame{ID: TxDestAssocNamesResponse, Payload: []byte{0, 0x00, 0, 0, 0x01}})
	wantErr(t, "danr general name underrun", err)
	_, err = DecodeDestAssocNamesResponse(Frame{ID: TxDestAssocNamesResponseExt, Payload: []byte{0, 0, 0, 0, 0}})
	wantShort(t, "danr ext short", err)
	_, err = DecodeDestAssocNamesResponse(Frame{ID: TxDestAssocNamesResponseExt, Payload: []byte{0, 0, 0x05, 0, 0, 0}})
	wantErr(t, "danr ext bad namelen", err)
	_, err = DecodeDestAssocNamesResponse(Frame{ID: TxDestAssocNamesResponseExt, Payload: []byte{0, 0, 0x00, 0, 0, 0x01}})
	wantErr(t, "danr ext name underrun", err)
	_, err = DecodeDestAssocNamesResponse(Frame{ID: wrongID})
	wantWrong(t, "danr wrong", err)
}

// --- tx 113 Crosspoint Tie Line Tally --------------------------------------

func TestDecodeTieLineTally_ErrorArms(t *testing.T) {
	_, err := DecodeTieLineTally(Frame{ID: wrongID})
	wantWrong(t, "tlt wrong", err)
	_, err = DecodeTieLineTally(Frame{ID: TxCrosspointTieLineTally, Payload: []byte{0, 0, 0}})
	wantShort(t, "tlt short header", err)
	// NumSrcs=2 but body truncated.
	_, err = DecodeTieLineTally(Frame{ID: TxCrosspointTieLineTally, Payload: []byte{0, 0, 0, 0x02}})
	wantErr(t, "tlt sources underrun", err)
}

// --- tx 116 Source Assoc Names Response ------------------------------------

func TestDecodeSourceAssocNamesResponse_ErrorArms(t *testing.T) {
	_, err := DecodeSourceAssocNamesResponse(Frame{ID: wrongID})
	wantWrong(t, "sanr wrong", err)
	_, err = DecodeSourceAssocNamesResponse(Frame{ID: TxSourceAssocNamesResponse, Payload: []byte{0, 0, 0, 0}})
	wantShort(t, "sanr short", err)
	_, err = DecodeSourceAssocNamesResponse(Frame{ID: TxSourceAssocNamesResponse, Payload: []byte{0, 0x05, 0, 0, 0}})
	wantErr(t, "sanr bad namelen", err)
	_, err = DecodeSourceAssocNamesResponse(Frame{ID: TxSourceAssocNamesResponse, Payload: []byte{0, 0x00, 0, 0, 0x01}})
	wantErr(t, "sanr name underrun", err)
}

// --- tx 122 / tx 250 Salvo Connect On Go Ack -------------------------------

func TestDecodeSalvoConnectOnGoAck_ErrorArms(t *testing.T) {
	_, err := DecodeSalvoConnectOnGoAck(Frame{ID: TxSalvoConnectOnGoAck, Payload: []byte{0, 0, 0, 0}})
	wantShort(t, "scoga general short", err)
	_, err = DecodeSalvoConnectOnGoAck(Frame{ID: TxSalvoConnectOnGoAckExt, Payload: []byte{0, 0, 0, 0, 0, 0}})
	wantShort(t, "scoga ext short", err)
	_, err = DecodeSalvoConnectOnGoAck(Frame{ID: wrongID})
	wantWrong(t, "scoga wrong", err)
}

// --- tx 123 Salvo Go Done Ack ----------------------------------------------

func TestDecodeSalvoGoDoneAck_ErrorArms(t *testing.T) {
	_, err := DecodeSalvoGoDoneAck(Frame{ID: wrongID})
	wantWrong(t, "sgda wrong", err)
	_, err = DecodeSalvoGoDoneAck(Frame{ID: TxSalvoGoDoneAck, Payload: []byte{0}})
	wantShort(t, "sgda short", err)
	// Unknown status (0x07).
	_, err = DecodeSalvoGoDoneAck(Frame{ID: TxSalvoGoDoneAck, Payload: []byte{0x07, 0x00}})
	wantErr(t, "sgda bad status", err)
}

// --- tx 125 / tx 253 Salvo Group Tally -------------------------------------

func TestDecodeSalvoGroupTally_ErrorArms(t *testing.T) {
	_, err := DecodeSalvoGroupTally(Frame{ID: TxSalvoGroupTally, Payload: []byte{0, 0, 0, 0, 0, 0}})
	wantShort(t, "sgt general short", err)
	// Unknown validity on general form (byte 6 = 0x07).
	_, err = DecodeSalvoGroupTally(Frame{ID: TxSalvoGroupTally, Payload: []byte{0, 0, 0, 0, 0, 0, 0x07}})
	wantErr(t, "sgt general bad validity", err)
	_, err = DecodeSalvoGroupTally(Frame{ID: TxSalvoGroupTallyExt, Payload: []byte{0, 0, 0, 0, 0, 0, 0, 0, 0}})
	wantShort(t, "sgt ext short", err)
	_, err = DecodeSalvoGroupTally(Frame{ID: TxSalvoGroupTallyExt, Payload: []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0x07}})
	wantErr(t, "sgt ext bad validity", err)
	_, err = DecodeSalvoGroupTally(Frame{ID: wrongID})
	wantWrong(t, "sgt wrong", err)
}
