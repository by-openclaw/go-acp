# RollCall specification coverage

What the Go codec implements, checked against the RollCall Technical
Specification Revision 14, the 2014 long-string extension, the Full Control
Command Set, and the vendor headers `rc3comm.h`, `RC3FILE.H`, `RC3LOG.H`,
`RC3STRM.H` and `RC3TYPES.H`.

This file answers one question: is the wire format covered, or only the part we
happened to need? It is checked by `TestSpecCoverage` in the codec package,
which fails if a packet type loses its handling or a structure disappears, so
the table cannot drift away from the code without CI noticing.

Last verified 2026-09-07 against `internal/snell-rollcall/codec` at 100%
statement coverage.

---

## 1. Message types

All 72 defined types are named, and every one carries the dispatch properties
the vendor keeps per type: whether it may be addressed to the broadcast
address, whether it may answer a request made outside a session, and whether it
belongs to the 32-bit generation.

"Payload" means a decoded structure exists. A type with no payload carries
none on the wire, or carries a single scalar the caller reads directly.

### Session and link (spec §4, §9)

| # | Type | Payload | Status |
| ---: | --- | --- | --- |
| 0 | NACK | optional text | done |
| 1 | ACK | optional text | done |
| 2 | CALL | `Connect` | done |
| 3 | TERM | `TermSess` | done |
| 13 | RESET | none | done |
| 14 | INVCMD | none | done |
| 15 | BUSY | optional `ID` | done |
| 23 | INVSESS | none | done |
| 25 | WAIT | `Wait` | done |
| 26 | CLEARSESS | `ClearSess` | done |
| 27 | BKCHNREADY | one byte | done |
| 28 | KEEPALIVE | none | done |
| 38 | ROUTEERROR | opaque | **see §4** |
| 39 | BLOCKHEADER | `BlockHeader` | done |
| 35 | GETNEXTPKT | `GetNext` | done |
| 44 | RAW | opaque by definition | done |
| 45 | SETUSERLEVEL | one word | done |

### Identity, map and ports (spec §7.6, §7.7)

| # | Type | Payload | Status |
| ---: | --- | --- | --- |
| 4 | GETSTAT | none | done |
| 5 | RETSTAT | `UnitStatus` | done |
| 6 | GETID | none | done |
| 7 | RETID | `ID` | done |
| 19 | GETDEVLIST | none | done |
| 20 | RETDEVINFO | `DeviceInfo` | done |
| 21 | GETDEVINFO | none | done |
| 29 | GETLOCDEVMAP | none | done |
| 33 | IAM | `DeviceInfo` | done |
| 50 | GETSRVBYNAME | name string | done |

### Menu service, 16-bit (spec §7.2)

| # | Type | Payload | Status |
| ---: | --- | --- | --- |
| 8 | GETFUNC | base index | done |
| 9 | RETFUNC | `Func` | done |
| 18 | FUNCLISTCHG | none | done |
| 30 | FUNCSTYLECHG | `FuncStyle` | done |

### Control service, 16-bit (spec §7.1)

| # | Type | Payload | Status |
| ---: | --- | --- | --- |
| 11 | GETFSTAT | `GetFStat` | done |
| 12 | RETFSTAT | `FuncStatus` | done |
| 16 | SETPARAM | `FuncStatus` | done |
| 36 | REPFCHG | command number | done |
| 37 | STOPREPFCHG | command number | done |
| 58 | SETMULTI | `[]SetMulti` | done |

### 32-bit generation (2014 extension)

| # | Type | Payload | Status |
| ---: | --- | --- | --- |
| 65 | GETMENUCOUNT | `MenuReq` | done |
| 66 | RETMENUCOUNT | `MenuSize` | done |
| 67 | GETMENUITEM | `MenuReq` | done |
| 68 | RETMENUITEM | `MenuItem` | done |
| 69 | GETVALUE | `GetValue` | done |
| 70 | SETVALUE | `Value` | done |
| 71 | RETVALUE | `Value` | done |

### Display service (spec §7.3)

| # | Type | Payload | Status |
| ---: | --- | --- | --- |
| 10 | DISPDATA | `Disp` | done |
| 54 | GETDISPDATA | line number | done |
| 60 | GETDISPCAPS | none | done |
| 61 | RETDISPCAPS | `DisplayCaps` | done |
| 62 | DRAWBITMAP | `DrawBitmap` | done |
| 63 | DRAWTEXT | `DrawText` | done |

### File service (spec §7.4, `RC3FILE.H`)

| # | Type | Payload | Status |
| ---: | --- | --- | --- |
| 42 | FILEDIR | `File` + path | done |
| 43 | RETFILEDIR | `DirEntry` | done |
| 46 | FILEDELETE | `File` + path | done |
| 48 | FILERENAME | `File` + two paths | done |
| 51 | FILEOPEN | `File` + path | done |
| 52 | RETFILEOPEN | `File` | done |
| 53 | FILECLOSE | `File` | done |
| 55 | FILEREAD | `File` | done |
| 56 | RETFILEREAD | `File` + data | done |
| 57 | FILEWRITE | `File` + data | done |
| 59 | FILERET | `File` | done |
| 64 | MAKEDIRECTORY | `File` + path | done |

### Logging service (spec §7.8, `RC3LOG.H`)

| # | Type | Payload | Status |
| ---: | --- | --- | --- |
| 22 | LOGREQ | format word | done |
| 49 | LOGDATA | `LogPacket` | done |

### Stream service (`RC3STRM.H`)

| # | Type | Payload | Status |
| ---: | --- | --- | --- |
| 40 | STREAMMODE | `StreamMode` | done |
| 41 | STREAMDATA | `StreamHeader` | done |

### Time service

| # | Type | Payload | Status |
| ---: | --- | --- | --- |
| 17 | TIME | `SysTime` | done |
| 24 | REALTIME | `Timecode` | done |
| 47 | GETTIME | none | done |

### Group control (spec §7.1.3)

| # | Type | Payload | Status |
| ---: | --- | --- | --- |
| 31 | SETGROUP | `SetGroup` | done |
| 32 | STOPGROUP | none | done |
| 34 | SETGRPFUNC | `GroupParam` + `FuncStatus` | done |

---

## 2. Structures

Every structure declared in the vendor headers, with its wire size.

| Structure | Bytes | Go type |
| --- | ---: | --- |
| `MESSAGE_STR` + `ROLLHEADER_STR` | 16 | `Frame` |
| `FULLADDRESS_STR` | 6 | `Address` |
| `VERSION_STR` | 4 | `Version` |
| `ID_STR` | 28 | `ID` |
| `STATUS_STR` | 4 | `UnitStatus` |
| `DEVICEINFO_STR` | 40 | `DeviceInfo` |
| `CONNECT_STR` | 44 | `Connect` |
| `TERMSESS_STR` | 22 | `TermSess` |
| `CLEARSESS_STR` | 4 | `ClearSess` |
| `WAIT_STR` | 4 + text | `Wait` |
| `BLOCKHEADER_STR` | 8 | `BlockHeader` |
| `GETNEXT_STR` | 4 | `GetNext` |
| `FUNC_STR` | 58 | `Func` |
| `FUNCSTYLE_STR` | 6 | `FuncStyle` |
| `GETFSTAT_STR` | 2 | `GetFStat` |
| `FUNCSTATUS_STR` | 8 + tail | `FuncStatus` |
| `SETMULTI_STR` | 4 each | `SetMulti` |
| `DISP_STR` | 22 | `Disp` |
| `MENUREQ_STR` | 4 | `MenuReq` |
| `MENUSIZE_STR` | 8 | `MenuSize` |
| `MENUITEM_STR` | 22 + 2 strings | `MenuItem` |
| `GETVALUE_STR` | 4 | `GetValue` |
| `VALUE_STR` | 12 + tail | `Value` |
| `FILE_STR` | 10 | `File` |
| `FILEINFOHDR_STR` | 10 | `FileInfo` |
| `MODULE_FILEINFO_STR` | 24 | `DirEntry` |
| `TIME_STR` | 14 | `SysTime` |
| `TIMECODE_STR` | 6 | `Timecode` |
| `LOGPACKET_STR` | 38 + text | `LogPacket` |
| `STREAMMODE_STR` | 4 | `StreamMode` |
| `STREAMHDR_STR` | 8 + data | `StreamHeader` |
| `DISPLAYCAPS_STR` | 12 | `DisplayCaps` |
| `DRAWBITMAP_STR` | 2 + bitmap | `DrawBitmap` |
| `DRAWTEXT_STR` | 2 + text | `DrawText` |
| `SETGROUP_STR` | 2 | `SetGroup` |
| `GROUPPARAM_STR` | 2 | `GroupParam` |
| `DOWNLOAD_STR` | 2 | `Download` |

Every structure the vendor headers declare now has a Go equivalent.

---

## 3. Flag and value tables

| Table | Header | Go |
| --- | --- | --- |
| `ServiceFlags` | `rc3comm.h` | `Service` |
| `UserLevels` | `rc3comm.h` | `UserLevel` |
| `StatusFlags` | `rc3comm.h` | `Status` |
| `FuncModes` | `rc3comm.h` | `Mode` |
| `StyleFlags` | `rc3comm.h` | `Style` |
| `TermTypes` | `rc3comm.h` | `TermCode` |
| `BCStateTypes` | `rc3comm.h` | back-channel constants |
| `TimeModeFlags` | `rc3comm.h` | time mode constants |
| `TimeCodeFlags` | `rc3comm.h` | timecode constants |
| `AttributeFlags` | `RC3FILE.H` | attribute constants |
| `FileOpenFlags` | `RC3FILE.H` | open constants |
| `ErrorValues` | `RC3FILE.H` | file error constants |
| `StreamModeFlags` | `RC3STRM.H` | `StreamBinary` |
| colour formats | `rc3comm.h` | colour format constants |
| `UnitIDs` | `rc3id.h` | 770-entry generated table |
| routing interface | Full Control Command Set | `codec/router` |
| Data Transfer Params | Full Control Command Set | `codec/dtp` |

---

## 4. Known gaps

Two, both recorded rather than guessed at.

**`ROUTEERROR_STR` is never defined.** Packet type 38 refers to it in
`rc3comm.h`, and no shipped header declares it. Nothing in the vendor sources
or the specification gives its layout. The type is decoded as an opaque payload
and the frame is delivered intact, so a caller can inspect it; inventing a
structure for it would be a guess presented as fact.

**The router sub-table offsets are inferred.** The Full Control Command Set
numbers only its root table, stating that `CMD_INTERFACE_VERSION` is command
100 and the rest follow. The per-matrix, per-level, per-source and
per-destination tables are described as "defined in a similar fashion" with
their commands listed in order but never numbered, so the offsets are read from
that ordering.

This has not been checked against a router controller. The audit oracle answers
NACK to command 100 and so exposes no routing interface at all. Closing it
needs either a real router controller, or the simulator reconfigured with
`CENTRA_SIMULATED_ROUTER`, and belongs to the integration unit.

---

## 5. Out of scope, deliberately

| Not implemented | Why |
| --- | --- |
| RollNet, ArcNet, serial, I2C transports | The connector is TCP only, per the epic's scope. The wire structures above are transport-independent and would carry over unchanged. |
| `SV_NET` bridging | We are an endpoint, never a bridge. The addressing rules for bridging are implemented and tested, so a frame that crosses one is understood; we do not forward. |
| `SV_EXEC` beyond `DOWNLOAD_STR` | The executive service has no documented protocol beyond the one structure, which is itself labelled "No idea - DRAGONS" in the vendor header. |
| `SV_THUMBNAIL`, `SV_LOC3` | No specification and no observed traffic. The service bits are named so a peer advertising them is reported accurately. |
| `SV_FASTMENU` | The bit is named. The specification says nothing about the mechanism beyond "file-based fast menu upload", and the file service it would use is implemented. |
