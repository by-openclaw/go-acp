package codec

import "strconv"

// PacketType is the 8-bit message type in ROLLHEADER_STR (spec 8, 9).
type PacketType uint8

// All defined packet types.
//
// Numbers 0..64 are the original set (spec 9). Numbers 65..71 were added by
// the 2014 long-string extension. Values the specification lists as Reserved
// are named here from the vendor header rc3comm.h, which defines several of
// them; a value with no name at all decodes as unknown and must be answered
// with InvCmd, not Nack (spec 9.15).
const (
	MsgNack          PacketType = 0
	MsgAck           PacketType = 1
	MsgCall          PacketType = 2
	MsgTerm          PacketType = 3
	MsgGetStat       PacketType = 4
	MsgRetStat       PacketType = 5
	MsgGetID         PacketType = 6
	MsgRetID         PacketType = 7
	MsgGetFunc       PacketType = 8
	MsgRetFunc       PacketType = 9
	MsgDispData      PacketType = 10
	MsgGetFStat      PacketType = 11
	MsgRetFStat      PacketType = 12
	MsgReset         PacketType = 13
	MsgInvCmd        PacketType = 14
	MsgBusy          PacketType = 15
	MsgSetParam      PacketType = 16
	MsgTime          PacketType = 17
	MsgFuncListChg   PacketType = 18
	MsgGetDevList    PacketType = 19
	MsgRetDevInfo    PacketType = 20
	MsgGetDevInfo    PacketType = 21
	MsgLogReq        PacketType = 22
	MsgInvSess       PacketType = 23
	MsgRealTime      PacketType = 24
	MsgWait          PacketType = 25
	MsgClearSess     PacketType = 26
	MsgBkChnReady    PacketType = 27
	MsgKeepAlive     PacketType = 28
	MsgGetLocDevMap  PacketType = 29
	MsgFuncStyleChg  PacketType = 30
	MsgSetGroup      PacketType = 31
	MsgStopGroup     PacketType = 32
	MsgIam           PacketType = 33
	MsgSetGrpFunc    PacketType = 34
	MsgGetNextPkt    PacketType = 35
	MsgRepFChg       PacketType = 36
	MsgStopRepFChg   PacketType = 37
	MsgRouteError    PacketType = 38
	MsgBlockHeader   PacketType = 39
	MsgStreamMode    PacketType = 40
	MsgStreamData    PacketType = 41
	MsgFileDir       PacketType = 42
	MsgRetFileDir    PacketType = 43
	MsgRaw           PacketType = 44
	MsgSetUserLevel  PacketType = 45
	MsgFileDelete    PacketType = 46
	MsgGetTime       PacketType = 47
	MsgFileRename    PacketType = 48
	MsgLogData       PacketType = 49
	MsgGetSrvByName  PacketType = 50
	MsgFileOpen      PacketType = 51
	MsgRetFileOpen   PacketType = 52
	MsgFileClose     PacketType = 53
	MsgGetDispData   PacketType = 54
	MsgFileRead      PacketType = 55
	MsgRetFileRead   PacketType = 56
	MsgFileWrite     PacketType = 57
	MsgSetMulti      PacketType = 58
	MsgFileRet       PacketType = 59
	MsgGetDispCaps   PacketType = 60
	MsgRetDispCaps   PacketType = 61
	MsgDrawBitmap    PacketType = 62
	MsgDrawText      PacketType = 63
	MsgMakeDirectory PacketType = 64
	MsgGetMenuCount  PacketType = 65
	MsgRetMenuCount  PacketType = 66
	MsgGetMenuItem   PacketType = 67
	MsgRetMenuItem   PacketType = 68
	MsgGetValue      PacketType = 69
	MsgSetValue      PacketType = 70
	MsgRetValue      PacketType = 71

	// NumPacketTypes is one past the highest defined type. A received type
	// at or above this is unknown (vendor MsgHandlers.c replies InvCmd).
	NumPacketTypes = 72
)

// props carries the dispatch properties the vendor library keeps per type in
// its handler table (Core/MsgHandlers.c).
type props struct {
	name string

	// broadcast marks a type that may legitimately arrive addressed to the
	// broadcast address. Only Time and Iam qualify.
	broadcast bool

	// blindReply marks a type that may arrive as the reply to a request
	// issued on the blind or unknown session, where replies are matched by
	// address rather than by session.
	blindReply bool

	// gen32 marks a type that only exists in the 2014 long-string
	// extension. A 16-bit-only unit answers these with Nack.
	gen32 bool
}

var packetProps = [NumPacketTypes]props{
	MsgNack:          {name: "NACK", blindReply: true},
	MsgAck:           {name: "ACK", blindReply: true},
	MsgCall:          {name: "CALL"},
	MsgTerm:          {name: "TERM"},
	MsgGetStat:       {name: "GETSTAT"},
	MsgRetStat:       {name: "RETSTAT", blindReply: true},
	MsgGetID:         {name: "GETID"},
	MsgRetID:         {name: "RETID", blindReply: true},
	MsgGetFunc:       {name: "GETFUNC"},
	MsgRetFunc:       {name: "RETFUNC"},
	MsgDispData:      {name: "DISPDATA"},
	MsgGetFStat:      {name: "GETFSTAT"},
	MsgRetFStat:      {name: "RETFSTAT", blindReply: true},
	MsgReset:         {name: "RESET"},
	MsgInvCmd:        {name: "INVCMD", blindReply: true},
	MsgBusy:          {name: "BUSY", blindReply: true},
	MsgSetParam:      {name: "SETPARAM"},
	MsgTime:          {name: "TIME", broadcast: true, blindReply: true},
	MsgFuncListChg:   {name: "FUNCLISTCHG"},
	MsgGetDevList:    {name: "GETDEVLIST"},
	MsgRetDevInfo:    {name: "RETDEVINFO", blindReply: true},
	MsgGetDevInfo:    {name: "GETDEVINFO"},
	MsgLogReq:        {name: "LOGREQ"},
	MsgInvSess:       {name: "INVSESS", blindReply: true},
	MsgRealTime:      {name: "REALTIME"},
	MsgWait:          {name: "WAIT", blindReply: true},
	MsgClearSess:     {name: "CLEARSESS"},
	MsgBkChnReady:    {name: "BKCHNREADY"},
	MsgKeepAlive:     {name: "KEEPALIVE"},
	MsgGetLocDevMap:  {name: "GETLOCDEVMAP"},
	MsgFuncStyleChg:  {name: "FUNCSTYLECHG"},
	MsgSetGroup:      {name: "SETGROUP"},
	MsgStopGroup:     {name: "STOPGROUP"},
	MsgIam:           {name: "IAM", broadcast: true},
	MsgSetGrpFunc:    {name: "SETGRPFUNC"},
	MsgGetNextPkt:    {name: "GETNEXTPKT"},
	MsgRepFChg:       {name: "REPFCHG"},
	MsgStopRepFChg:   {name: "STOPREPFCHG"},
	MsgRouteError:    {name: "ROUTEERROR"},
	MsgBlockHeader:   {name: "BLOCKHEADER", blindReply: true},
	MsgStreamMode:    {name: "STREAMMODE"},
	MsgStreamData:    {name: "STREAMDATA"},
	MsgFileDir:       {name: "FILEDIR"},
	MsgRetFileDir:    {name: "RETFILEDIR"},
	MsgRaw:           {name: "RAW"},
	MsgSetUserLevel:  {name: "SETUSERLEVEL"},
	MsgFileDelete:    {name: "FILEDELETE"},
	MsgGetTime:       {name: "GETTIME"},
	MsgFileRename:    {name: "FILERENAME"},
	MsgLogData:       {name: "LOGDATA"},
	MsgGetSrvByName:  {name: "GETSRVBYNAME"},
	MsgFileOpen:      {name: "FILEOPEN"},
	MsgRetFileOpen:   {name: "RETFILEOPEN"},
	MsgFileClose:     {name: "FILECLOSE"},
	MsgGetDispData:   {name: "GETDISPDATA"},
	MsgFileRead:      {name: "FILEREAD"},
	MsgRetFileRead:   {name: "RETFILEREAD"},
	MsgFileWrite:     {name: "FILEWRITE"},
	MsgSetMulti:      {name: "SETMULTI"},
	MsgFileRet:       {name: "FILERET"},
	MsgGetDispCaps:   {name: "GETDISPCAPS"},
	MsgRetDispCaps:   {name: "RETDISPCAPS", blindReply: true},
	MsgDrawBitmap:    {name: "DRAWBITMAP"},
	MsgDrawText:      {name: "DRAWTEXT"},
	MsgMakeDirectory: {name: "MAKEDIRECTORY"},
	MsgGetMenuCount:  {name: "GETMENUCOUNT", gen32: true},
	MsgRetMenuCount:  {name: "RETMENUCOUNT", gen32: true},
	MsgGetMenuItem:   {name: "GETMENUITEM", gen32: true},
	MsgRetMenuItem:   {name: "RETMENUITEM", gen32: true},
	MsgGetValue:      {name: "GETVALUE", gen32: true},
	MsgSetValue:      {name: "SETVALUE", gen32: true},
	MsgRetValue:      {name: "RETVALUE", gen32: true, blindReply: true},
}

// Known reports whether the type is defined by the specification or the
// vendor header. An unknown type must be answered with InvCmd (spec 9.15),
// which is distinct from Nack: InvCmd means "I do not understand this",
// Nack means "I understand it but cannot do it".
func (t PacketType) Known() bool {
	return int(t) < NumPacketTypes && packetProps[t].name != ""
}

// String returns the vendor name, e.g. "SETVALUE", or the decimal value in
// angle brackets when the type is unknown.
func (t PacketType) String() string {
	if t.Known() {
		return packetProps[t].name
	}
	return "<" + strconv.Itoa(int(t)) + ">"
}

// Broadcastable reports whether the type may arrive addressed to the
// broadcast address. Only Time and Iam may.
func (t PacketType) Broadcastable() bool {
	return t.Known() && packetProps[t].broadcast
}

// ValidBlindReply reports whether the type may be accepted as the reply to a
// request made on the blind or unknown session, where the session index
// cannot be used to correlate and matching is by address instead.
func (t PacketType) ValidBlindReply() bool {
	return t.Known() && packetProps[t].blindReply
}

// Gen32 reports whether the type belongs to the 2014 long-string extension.
// A unit that does not advertise SvcLongStr answers these with Nack.
func (t PacketType) Gen32() bool {
	return t.Known() && packetProps[t].gen32
}

// PacketTypeByName looks up a type by its vendor name, case-sensitively, with
// or without the "SP_" prefix the specification uses. It exists so that CLI
// flags and trace filters can name types the way the documents do.
func PacketTypeByName(name string) (PacketType, bool) {
	n := name
	if len(n) > 3 && n[:3] == "SP_" {
		n = n[3:]
	}
	for i := 0; i < NumPacketTypes; i++ {
		if packetProps[i].name == n {
			return PacketType(i), true
		}
	}
	return 0, false
}
