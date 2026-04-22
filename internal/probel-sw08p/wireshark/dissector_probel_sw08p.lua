-------------------------------------------------------------------------------
--
-- Wireshark Lua Dissector: Probel SW-P-08 / SW-P-88 over TCP (default 2008)
--
-- Standalone dissector handling:
--   - §2 framing: DLE STX <data> <btc> <chk> DLE ETX with DLE-stuffing
--   - §2 link-level: DLE ACK / DLE NAK (two-byte pseudo-frames)
--   - §3.2 / §3.3 command catalogue (general + extended)
--   - Per-cmd decode for the high-traffic bytes: crosspoint interrogate /
--     connect / tally / tally-dump (byte+word), name requests/responses,
--     salvo build/fire/ack, protect tally
--   - Checksum validation (8-bit two's complement over DATA || BTC)
--   - BTC (byte count) validation against decoded DATA length
--   - TCP reassembly for frames split across segments
--
-- Compatible with Wireshark 4.x.
-- Spec authority: internal/probel-sw08p/assets/probel-sw08p/SW-P-08 Issue 30.doc
--
-------------------------------------------------------------------------------

local PROBEL_TCP_PORT = 2008

-- §2 framing bytes
local DLE = 0x10
local STX = 0x02
local ETX = 0x03
local ACK = 0x06
local NAK = 0x15

-------------------------------------------------------------------------------
-- Command-byte catalogue (direction-overloaded: 0x11 is rx protect-name on
-- the controller→matrix direction AND tx app-keepalive on matrix→controller).
-- Per SW-P-08 §3.2 / §3.3 (mirrored from internal/probel-sw08p/codec/types.go).
-------------------------------------------------------------------------------

local cmd_name = {
    -- RX general (controller → matrix)
    [0x01] = "rx 001 Crosspoint Interrogate",
    [0x02] = "rx 002 Crosspoint Connect",
    [0x07] = "rx 007 Maintenance",
    [0x08] = "rx 008 Dual Controller Status Request",
    [0x0A] = "rx 010 Protect Interrogate",
    [0x0C] = "rx 012 Protect Connect",
    [0x0E] = "rx 014 Protect Disconnect",
    [0x11] = "0x11 Protect Device Name Req (rx) / App Keepalive Req (tx)",
    [0x13] = "rx 019 Protect Tally Dump Request",
    [0x15] = "rx 021 Crosspoint Tally Dump Request",
    [0x1D] = "rx 029 Master Protect Connect",
    [0x22] = "rx 034 App Keepalive Response",
    [0x64] = "rx 100 All Source Names Request",
    [0x65] = "rx 101 Single Source Name Request",
    [0x66] = "rx 102 All Dest Assoc Names Request",
    [0x67] = "rx 103 Single Dest Assoc Name Request",
    [0x70] = "rx 112 Crosspoint Tie-Line Interrogate",
    [0x72] = "rx 114 All Source Assoc Names Request",
    [0x73] = "rx 115 Single Source Assoc Name Request",
    [0x75] = "rx 117 Update Name Request",
    [0x78] = "rx 120 Crosspoint Connect-On-Go Salvo",
    [0x79] = "rx 121 Crosspoint Go Salvo",
    [0x7C] = "rx 124 Crosspoint Salvo Group Interrogate",

    -- RX extended (general | 0x80)
    [0x81] = "rx 129 Crosspoint Interrogate (ext)",
    [0x82] = "rx 130 Crosspoint Connect (ext)",
    [0x8A] = "rx 138 Protect Interrogate (ext)",
    [0x8C] = "rx 140 Protect Connect (ext)",
    [0x8E] = "rx 142 Protect Disconnect (ext)",
    [0x93] = "rx 147 Protect Tally Dump Request (ext)",
    [0x95] = "rx 149 Crosspoint Tally Dump Request (ext)",
    [0xE4] = "rx 228 All Source Names Request (ext)",
    [0xE5] = "rx 229 Single Source Name Request (ext)",
    [0xE6] = "rx 230 All Dest Assoc Names Request (ext)",
    [0xE7] = "rx 231 Single Dest Assoc Name Request (ext)",
    [0xF8] = "rx 248 Crosspoint Connect-On-Go Salvo (ext)",
    [0xFC] = "rx 252 Crosspoint Salvo Group Interrogate (ext)",

    -- TX general (matrix → controller)
    [0x03] = "tx 003 Crosspoint Tally",
    [0x04] = "tx 004 Crosspoint Connected",
    [0x09] = "tx 009 Dual Controller Status Response",
    [0x0B] = "tx 011 Protect Tally",
    [0x0D] = "tx 013 Protect Connected",
    [0x0F] = "tx 015 Protect Disconnected",
    [0x12] = "tx 018 Protect Device Name Response",
    [0x14] = "tx 020 Protect Tally Dump",
    [0x16] = "tx 022 Crosspoint Tally Dump (byte)",
    [0x17] = "tx 023 Crosspoint Tally Dump (word)",
    [0x6A] = "tx 106 Source Names Response",
    [0x6B] = "tx 107 Dest Assoc Names Response",
    [0x71] = "tx 113 Crosspoint Tie-Line Tally",
    [0x74] = "tx 116 Source Assoc Names Response",
    [0x7A] = "tx 122 Salvo Connect-On-Go Ack",
    [0x7B] = "tx 123 Salvo Go-Done Ack",
    [0x7D] = "tx 125 Salvo Group Tally",

    -- TX extended (general | 0x80)
    [0x83] = "tx 131 Crosspoint Tally (ext)",
    [0x84] = "tx 132 Crosspoint Connected (ext)",
    [0x8B] = "tx 139 Protect Tally (ext)",
    [0x8D] = "tx 141 Protect Connected (ext)",
    [0x8F] = "tx 143 Protect Disconnected (ext)",
    [0x94] = "tx 148 Protect Tally Dump (ext)",
    [0x97] = "tx 151 Crosspoint Tally Dump Word (ext)",
    [0xEA] = "tx 234 Source Names Response (ext)",
    [0xEB] = "tx 235 Dest Assoc Names Response (ext)",
    [0xFA] = "tx 250 Salvo Connect-On-Go Ack (ext)",
    [0xFD] = "tx 253 Salvo Group Tally (ext)",
}

-- Short Info-column label for each cmd (keeps the column narrow).
local cmd_short = {
    [0x01] = "INTERROGATE",  [0x81] = "INTERROGATE",
    [0x02] = "CONNECT",      [0x82] = "CONNECT",
    [0x03] = "TALLY",        [0x83] = "TALLY",
    [0x04] = "CONNECTED",    [0x84] = "CONNECTED",
    [0x07] = "MAINTENANCE",
    [0x08] = "DUALCTL-REQ",  [0x09] = "DUALCTL-RESP",
    [0x0A] = "PROT-INT",     [0x8A] = "PROT-INT",
    [0x0B] = "PROT-TALLY",   [0x8B] = "PROT-TALLY",
    [0x0C] = "PROT-CONN",    [0x8C] = "PROT-CONN",
    [0x0D] = "PROT-CONN'D",  [0x8D] = "PROT-CONN'D",
    [0x0E] = "PROT-DISC",    [0x8E] = "PROT-DISC",
    [0x0F] = "PROT-DISC'D",  [0x8F] = "PROT-DISC'D",
    [0x11] = "PROT-NAME|KEEPALIVE-REQ",
    [0x12] = "PROT-NAME-RESP",
    [0x13] = "PROT-DUMP-REQ", [0x93] = "PROT-DUMP-REQ",
    [0x14] = "PROT-DUMP",     [0x94] = "PROT-DUMP",
    [0x15] = "DUMP-REQ",     [0x95] = "DUMP-REQ",
    [0x16] = "DUMP(byte)",
    [0x17] = "DUMP(word)",    [0x97] = "DUMP(word)",
    [0x1D] = "MASTER-PROT",
    [0x22] = "KEEPALIVE-RESP",
    [0x64] = "SRC-NAMES",    [0xE4] = "SRC-NAMES",
    [0x65] = "SRC-NAME-1",   [0xE5] = "SRC-NAME-1",
    [0x66] = "DST-NAMES",    [0xE6] = "DST-NAMES",
    [0x67] = "DST-NAME-1",   [0xE7] = "DST-NAME-1",
    [0x6A] = "SRC-NAMES-R",  [0xEA] = "SRC-NAMES-R",
    [0x6B] = "DST-NAMES-R",  [0xEB] = "DST-NAMES-R",
    [0x70] = "TIELINE-INT",
    [0x71] = "TIELINE-TALLY",
    [0x72] = "SRC-ASSOC",
    [0x73] = "SRC-ASSOC-1",
    [0x74] = "SRC-ASSOC-R",
    [0x75] = "UPDATE-NAME",
    [0x78] = "SALVO-BUILD", [0xF8] = "SALVO-BUILD",
    [0x79] = "SALVO-GO",
    [0x7A] = "SALVO-ACK",   [0xFA] = "SALVO-ACK",
    [0x7B] = "SALVO-DONE",
    [0x7C] = "SALVO-GRP-INT", [0xFC] = "SALVO-GRP-INT",
    [0x7D] = "SALVO-GRP-TALLY", [0xFD] = "SALVO-GRP-TALLY",
}

local namelen_valstr = {
    [0] = "4-char",
    [1] = "8-char",
    [2] = "12-char",
    [3] = "16-char",
}

local protect_state_valstr = {
    [0] = "not-protected",
    [1] = "pro-bel",
    [2] = "pro-bel-override",
    [3] = "oem",
}

-------------------------------------------------------------------------------
-- Protocol declaration
-------------------------------------------------------------------------------

local p_probel = Proto("probel_sw08p", "Probel SW-P-08/88")

local f = {
    som        = ProtoField.bytes  ("probel.som",        "Start-of-Message (DLE STX)"),
    eom        = ProtoField.bytes  ("probel.eom",        "End-of-Message (DLE ETX)"),
    data       = ProtoField.bytes  ("probel.data",       "DATA (unescaped)"),
    raw        = ProtoField.bytes  ("probel.raw",        "Raw frame (on-wire, escaped)"),
    cmd        = ProtoField.uint8  ("probel.cmd",        "Command",      base.HEX, cmd_name),
    cmd_dec    = ProtoField.uint8  ("probel.cmd_dec",    "Command (dec)",base.DEC),
    btc        = ProtoField.uint8  ("probel.btc",        "Byte Count",   base.DEC),
    chk        = ProtoField.uint8  ("probel.chk",        "Checksum",     base.HEX),
    chk_good   = ProtoField.bool   ("probel.chk_good",   "Checksum OK"),
    chk_calc   = ProtoField.uint8  ("probel.chk_calc",   "Checksum (calc)", base.HEX),
    btc_good   = ProtoField.bool   ("probel.btc_good",   "BTC OK"),
    extended   = ProtoField.bool   ("probel.extended",   "Extended command"),
    matrix     = ProtoField.uint8  ("probel.matrix",     "Matrix ID",    base.DEC),
    level      = ProtoField.uint8  ("probel.level",      "Level ID",     base.DEC),
    dst        = ProtoField.uint16 ("probel.dst",        "Destination",  base.DEC),
    src        = ProtoField.uint16 ("probel.src",        "Source",       base.DEC),
    first_dst  = ProtoField.uint16 ("probel.first_dst",  "First Destination",  base.DEC),
    first_src  = ProtoField.uint16 ("probel.first_src",  "First Source",       base.DEC),
    tally_n    = ProtoField.uint16 ("probel.tallies",    "Tallies",      base.DEC),
    namelen    = ProtoField.uint8  ("probel.namelen",    "Name Length",  base.DEC, namelen_valstr),
    name_count = ProtoField.uint8  ("probel.names",      "Names",        base.DEC),
    name_item  = ProtoField.string ("probel.name",       "Name"),
    protect    = ProtoField.uint8  ("probel.protect",    "Protect State",base.DEC, protect_state_valstr),
    status     = ProtoField.uint8  ("probel.status",     "Status",       base.HEX),
    salvo_grp  = ProtoField.uint16 ("probel.salvo.grp",  "Salvo Group",  base.DEC),
    salvo_cnt  = ProtoField.uint16 ("probel.salvo.count","Salvo Elements",base.DEC),
    payload    = ProtoField.bytes  ("probel.payload",    "Payload"),
    ackframe   = ProtoField.bool   ("probel.ack",        "DLE ACK"),
    nakframe   = ProtoField.bool   ("probel.nak",        "DLE NAK"),
}
p_probel.fields = f

-- Expert infos
local ef_bad_chk = ProtoExpert.new("probel.bad_checksum.expert",
    "Checksum mismatch", expert.group.CHECKSUM, expert.severity.ERROR)
local ef_bad_btc = ProtoExpert.new("probel.bad_btc.expert",
    "BTC mismatch", expert.group.MALFORMED, expert.severity.ERROR)
local ef_nak = ProtoExpert.new("probel.nak.expert",
    "Peer NAK", expert.group.RESPONSE_CODE, expert.severity.NOTE)
local ef_unsupported = ProtoExpert.new("probel.unsupported.expert",
    "Unknown command byte", expert.group.PROTOCOL, expert.severity.NOTE)
p_probel.experts = { ef_bad_chk, ef_bad_btc, ef_nak, ef_unsupported }

-------------------------------------------------------------------------------
-- Helpers
-------------------------------------------------------------------------------

-- DLE-unstuff raw[0..len-1] into a byte array; returns {bytes, consumed}
-- where consumed is how many source bytes were eaten. Returns nil if not
-- enough data (caller should request reassembly).
local function unstuff(tvbuf, start, limit)
    -- Walk DATA+BTC+CHK region which lives between DLE STX and DLE ETX.
    -- Caller passes the byte offset where the payload region starts and
    -- the offset where DLE ETX is known to begin.
    local out = {}
    local i = start
    while i < limit do
        local b = tvbuf:range(i, 1):uint()
        if b == DLE and (i + 1) < limit then
            local n = tvbuf:range(i + 1, 1):uint()
            if n == DLE then
                out[#out + 1] = DLE
                i = i + 2
            else
                -- Shouldn't happen — scan_frame would have found the
                -- DLE ETX already. Be defensive: stop here.
                break
            end
        else
            out[#out + 1] = b
            i = i + 1
        end
    end
    return out
end

-- 8-bit two's-complement checksum over bytes[] (DATA || BTC). Uses
-- pure arithmetic instead of Lua 5.3 bit operators so the dissector
-- also loads under Wireshark builds that ship with Lua 5.2.
local function checksum8(bytes)
    local s = 0
    for _, v in ipairs(bytes) do
        s = (s + v) % 256
    end
    return (256 - s) % 256
end

-- Locate the end-of-message DLE ETX starting at offset `start` in tvbuf.
-- Skips doubled-DLE pairs inside DATA/BTC/CHK. Returns the byte offset of
-- the DLE (first byte of DLE ETX), or -1 if not found (need more data),
-- or -2 if a stray DLE X was seen (frame error).
local function find_eom(tvbuf, start)
    local limit = tvbuf:reported_length_remaining()
    local i = start
    while i < limit do
        if tvbuf:range(i, 1):uint() ~= DLE then
            i = i + 1
        else
            if (i + 1) >= limit then
                return -1
            end
            local nxt = tvbuf:range(i + 1, 1):uint()
            if nxt == DLE then
                i = i + 2
            elseif nxt == ETX then
                return i
            else
                return -2
            end
        end
    end
    return -1
end

-- Decode matrix/level byte: hi nibble = matrix, low = level. Pure
-- arithmetic so the file loads under Lua 5.1/5.2 (no & / >> yet).
local function decode_mtxlvl(byte)
    return math.floor(byte / 16), byte % 16
end

-- Right-trim trailing space + NUL for name display.
local function trim_name(s)
    if s == nil then return "" end
    -- strip trailing NULs then spaces
    return (s:gsub("[%z ]+$", ""))
end

-- Read a fixed-width name from a byte array starting at index `idx`
-- (1-based into `bytes`), width `w`. Returns the string.
local function read_name(bytes, idx, w)
    local buf = {}
    for i = 0, w - 1 do
        local b = bytes[idx + i]
        if b == nil then break end
        if b == 0 then
            b = 32 -- show NULs as spaces for the Detail pane
        end
        buf[#buf + 1] = string.char(b)
    end
    return trim_name(table.concat(buf))
end

-- Returns 4/8/12/16 for NameLength enum, default 8.
local function namelen_bytes(nl)
    if nl == 0 then return 4
    elseif nl == 1 then return 8
    elseif nl == 2 then return 12
    elseif nl == 3 then return 16
    end
    return 8
end

-------------------------------------------------------------------------------
-- Per-command decoders: data[1..] (zero-based) is the payload after the
-- command byte. Each returns an Info-column suffix string.
-------------------------------------------------------------------------------

-- General crosspoint header: (matrix<<4|level, (dst/128)<<4|(src/128), dst%128, src%128)
local function decode_xpoint_general(tree, data, with_src)
    if #data < 3 then return "" end
    local ml   = data[1]
    local mult = data[2]
    local dst  = (math.floor(mult / 16) % 8) * 128 + data[3]
    local m, l = decode_mtxlvl(ml)
    tree:add(f.matrix, m)
    tree:add(f.level,  l)
    tree:add(f.dst,    dst)
    local info = string.format("mtx=%d lvl=%d dst=%d", m, l, dst)
    if with_src and #data >= 4 then
        local src = (mult % 8) * 128 + data[4]
        tree:add(f.src, src)
        info = info .. string.format(" src=%d", src)
    end
    return info
end

-- Extended crosspoint header: (matrix, level, dst_hi, dst_lo[, src_hi, src_lo[, status]])
local function decode_xpoint_ext(tree, data, with_src)
    if #data < 4 then return "" end
    local m = data[1]
    local l = data[2]
    local dst = data[3] * 256 + data[4]
    tree:add(f.matrix, m)
    tree:add(f.level,  l)
    tree:add(f.dst,    dst)
    local info = string.format("mtx=%d lvl=%d dst=%d", m, l, dst)
    if with_src and #data >= 6 then
        local src = data[5] * 256 + data[6]
        tree:add(f.src, src)
        info = info .. string.format(" src=%d", src)
        if #data >= 7 then
            tree:add(f.status, data[7])
        end
    end
    return info
end

-- rx/tx tally-dump-byte: data = [ml, tallies, first_dst, src0, src1, ..., srcN-1]
local function decode_dump_byte(tree, data)
    if #data < 3 then return "" end
    local m, l = decode_mtxlvl(data[1])
    local n    = data[2]
    local fd   = data[3]
    tree:add(f.matrix, m)
    tree:add(f.level,  l)
    tree:add(f.tally_n, n)
    tree:add(f.first_dst, fd)
    return string.format("mtx=%d lvl=%d tallies=%d first_dst=%d", m, l, n, fd)
end

-- tx tally-dump-word (general 0x17): data = [ml, n, fd_hi, fd_lo, src0_hi,src0_lo, ...]
local function decode_dump_word_general(tree, data)
    if #data < 4 then return "" end
    local m, l = decode_mtxlvl(data[1])
    local n    = data[2]
    local fd   = data[3] * 256 + data[4]
    tree:add(f.matrix, m)
    tree:add(f.level,  l)
    tree:add(f.tally_n, n)
    tree:add(f.first_dst, fd)
    return string.format("mtx=%d lvl=%d tallies=%d first_dst=%d", m, l, n, fd)
end

-- tx tally-dump-word extended (0x97): data = [matrix, level, n, fd_hi, fd_lo, ...]
local function decode_dump_word_ext(tree, data)
    if #data < 5 then return "" end
    local m = data[1]
    local l = data[2]
    local n = data[3]
    local fd = data[4] * 256 + data[5]
    tree:add(f.matrix, m)
    tree:add(f.level,  l)
    tree:add(f.tally_n, n)
    tree:add(f.first_dst, fd)
    return string.format("mtx=%d lvl=%d tallies=%d first_dst=%d", m, l, n, fd)
end

-- rx 021 / 0x95 tally-dump request: byte-mode (general) = [ml]; ext = [m, l].
local function decode_dump_request(tree, data, extended)
    if extended then
        if #data < 2 then return "" end
        tree:add(f.matrix, data[1])
        tree:add(f.level,  data[2])
        return string.format("mtx=%d lvl=%d", data[1], data[2])
    end
    if #data < 1 then return "" end
    local m, l = decode_mtxlvl(data[1])
    tree:add(f.matrix, m)
    tree:add(f.level,  l)
    return string.format("mtx=%d lvl=%d", m, l)
end

-- rx 100/102/114 ALL * NAMES REQUEST: [ml, namelen]
local function decode_all_names_req(tree, data)
    if #data < 2 then return "" end
    local m, l = decode_mtxlvl(data[1])
    local nl = data[2]
    tree:add(f.matrix, m)
    tree:add(f.level,  l)
    tree:add(f.namelen, nl)
    return string.format("mtx=%d lvl=%d namelen=%s", m, l, namelen_valstr[nl] or nl)
end

-- rx 101/103/115 SINGLE * NAME REQUEST: [ml, namelen, idx_hi, idx_lo]
local function decode_single_name_req(tree, data, kind)
    if #data < 4 then return "" end
    local m, l = decode_mtxlvl(data[1])
    local nl = data[2]
    local idx = data[3] * 256 + data[4]
    tree:add(f.matrix, m)
    tree:add(f.level,  l)
    tree:add(f.namelen, nl)
    if kind == "src" then
        tree:add(f.src, idx)
        return string.format("mtx=%d lvl=%d namelen=%s src=%d", m, l, namelen_valstr[nl] or nl, idx)
    else
        tree:add(f.dst, idx)
        return string.format("mtx=%d lvl=%d namelen=%s dst=%d", m, l, namelen_valstr[nl] or nl, idx)
    end
end

-- tx 106/107/116: [ml, namelen, first_hi, first_lo, count, name*count]
local function decode_names_response(tree, data, kind)
    if #data < 5 then return "" end
    local m, l = decode_mtxlvl(data[1])
    local nl = data[2]
    local first = data[3] * 256 + data[4]
    local count = data[5]
    local w = namelen_bytes(nl)
    tree:add(f.matrix, m)
    tree:add(f.level,  l)
    tree:add(f.namelen, nl)
    if kind == "src" then
        tree:add(f.first_src, first)
    else
        tree:add(f.first_dst, first)
    end
    tree:add(f.name_count, count)
    -- Append name entries (bounded)
    local idx = 6 -- 1-based first name byte
    local emitted = 0
    for i = 1, count do
        if idx + w - 1 > #data then break end
        local nm = read_name(data, idx, w)
        tree:add(f.name_item, nm):set_text(string.format("Name[%d]: \"%s\"", first + (i - 1), nm))
        idx = idx + w
        emitted = emitted + 1
    end
    return string.format("mtx=%d lvl=%d namelen=%s first=%d count=%d",
        m, l, namelen_valstr[nl] or nl, first, emitted)
end

-- tx 011 Protect Tally: [ml, mult_dst, dst_lo, state]
local function decode_protect_tally(tree, data)
    if #data < 4 then return "" end
    local m, l = decode_mtxlvl(data[1])
    local dst = (math.floor(data[2] / 16) % 8) * 128 + data[3]
    local st = data[4]
    tree:add(f.matrix, m); tree:add(f.level, l); tree:add(f.dst, dst); tree:add(f.protect, st)
    return string.format("mtx=%d lvl=%d dst=%d state=%s", m, l, dst, protect_state_valstr[st] or st)
end

-- rx 120 / 0xF8 Salvo Build on Go, rx 124 Salvo Group Interrogate.
-- Payloads are rich (group id + element list); show top-level numbers.
-- General form: [ml, count_hi, count_lo, <element triplets>]
local function decode_salvo_general(tree, data)
    if #data < 3 then return "" end
    local m, l = decode_mtxlvl(data[1])
    local count = data[2] * 256 + data[3]
    tree:add(f.matrix, m); tree:add(f.level, l); tree:add(f.salvo_cnt, count)
    return string.format("mtx=%d lvl=%d elements=%d", m, l, count)
end

-------------------------------------------------------------------------------
-- Dissect one fully-framed SW-P-08 message whose raw bytes occupy
-- tvbuf[start .. eom+1] (inclusive of DLE STX at start and DLE ETX at eom).
-- Returns (info_string, bytes_consumed).
-------------------------------------------------------------------------------
local function dissect_frame(tvbuf, pktinfo, root, start, eom)
    local frame_len = (eom + 2) - start -- includes DLE ETX
    local tree = root:add(p_probel, tvbuf:range(start, frame_len))
    tree:set_text("Probel SW-P-08 frame")

    tree:add(f.raw, tvbuf:range(start, frame_len))
    tree:add(f.som, tvbuf:range(start, 2))

    -- Unescape the DATA+BTC+CHK region (between SOM and EOM).
    local bytes = unstuff(tvbuf, start + 2, eom)
    if #bytes < 3 then
        tree:add_proto_expert_info(ef_bad_btc, "Frame too short (need ID + BTC + CHK)")
        tree:add(f.eom, tvbuf:range(eom, 2))
        pktinfo.cols.info:set("Probel: runt frame")
        return "runt", frame_len
    end

    local chk = bytes[#bytes]
    local btc = bytes[#bytes - 1]
    local data_len = #bytes - 2
    local data_bytes = {}
    for i = 1, data_len do data_bytes[i] = bytes[i] end

    -- Expose the unescaped data region as a bytes field. Use the escaped
    -- source bytes (tvbuf range) so highlighting lines up with the packet
    -- byte view; the Detail pane "bytes" column shows the unescaped form.
    tree:add(f.data, tvbuf:range(start + 2, eom - (start + 2)))

    -- Command byte + BTC + CHK
    local cmd_byte = data_bytes[1]
    local ctree = tree:add(f.cmd, cmd_byte)
    ctree:append_text(string.format(" — %s", cmd_name[cmd_byte] or "Unknown"))
    tree:add(f.cmd_dec, cmd_byte)
    tree:add(f.extended, cmd_byte >= 0x80)

    local btc_tree = tree:add(f.btc, btc)
    tree:add(f.btc_good, btc == data_len):set_generated()
    if btc ~= data_len then
        btc_tree:add_proto_expert_info(ef_bad_btc,
            string.format("BTC=%d but DATA length=%d", btc, data_len))
    end

    -- Verify checksum over DATA || BTC.
    local chk_in = {}
    for i = 1, data_len do chk_in[i] = data_bytes[i] end
    chk_in[data_len + 1] = btc
    local calc = checksum8(chk_in)
    local chk_tree = tree:add(f.chk, chk)
    tree:add(f.chk_calc, calc):set_generated()
    tree:add(f.chk_good, calc == chk):set_generated()
    if calc ~= chk then
        chk_tree:add_proto_expert_info(ef_bad_chk,
            string.format("Checksum mismatch: got 0x%02x, expected 0x%02x", chk, calc))
    end

    tree:add(f.eom, tvbuf:range(eom, 2))

    -- Build Info-column suffix by command.
    local info_suffix = ""

    if cmd_byte == 0x01 then
        info_suffix = decode_xpoint_general(tree, data_bytes, false)
    elseif cmd_byte == 0x81 then
        info_suffix = decode_xpoint_ext(tree, data_bytes, false)
    elseif cmd_byte == 0x02 then
        info_suffix = decode_xpoint_general(tree, data_bytes, true)
    elseif cmd_byte == 0x82 then
        info_suffix = decode_xpoint_ext(tree, data_bytes, true)
    elseif cmd_byte == 0x03 then
        info_suffix = decode_xpoint_general(tree, data_bytes, true)
    elseif cmd_byte == 0x83 then
        info_suffix = decode_xpoint_ext(tree, data_bytes, true)
    elseif cmd_byte == 0x04 then
        info_suffix = decode_xpoint_general(tree, data_bytes, true)
    elseif cmd_byte == 0x84 then
        info_suffix = decode_xpoint_ext(tree, data_bytes, true)
    elseif cmd_byte == 0x0B or cmd_byte == 0x0D or cmd_byte == 0x0F then
        info_suffix = decode_protect_tally(tree, data_bytes)
    elseif cmd_byte == 0x15 then
        info_suffix = decode_dump_request(tree, data_bytes, false)
    elseif cmd_byte == 0x95 then
        info_suffix = decode_dump_request(tree, data_bytes, true)
    elseif cmd_byte == 0x16 then
        info_suffix = decode_dump_byte(tree, data_bytes)
    elseif cmd_byte == 0x17 then
        info_suffix = decode_dump_word_general(tree, data_bytes)
    elseif cmd_byte == 0x97 then
        info_suffix = decode_dump_word_ext(tree, data_bytes)
    elseif cmd_byte == 0x64 or cmd_byte == 0x66 or cmd_byte == 0x72 then
        info_suffix = decode_all_names_req(tree, data_bytes)
    elseif cmd_byte == 0x65 or cmd_byte == 0x73 then
        info_suffix = decode_single_name_req(tree, data_bytes, "src")
    elseif cmd_byte == 0x67 then
        info_suffix = decode_single_name_req(tree, data_bytes, "dst")
    elseif cmd_byte == 0x6A then
        info_suffix = decode_names_response(tree, data_bytes, "src")
    elseif cmd_byte == 0x6B then
        info_suffix = decode_names_response(tree, data_bytes, "dst")
    elseif cmd_byte == 0x74 then
        info_suffix = decode_names_response(tree, data_bytes, "src")
    elseif cmd_byte == 0x78 or cmd_byte == 0x7C then
        info_suffix = decode_salvo_general(tree, data_bytes)
    elseif cmd_byte == 0x79 then
        -- Salvo fire: [ml, group_hi, group_lo]
        if #data_bytes >= 3 then
            local m, l = decode_mtxlvl(data_bytes[1])
            local grp = data_bytes[2] * 256 + data_bytes[3]
            tree:add(f.matrix, m); tree:add(f.level, l); tree:add(f.salvo_grp, grp)
            info_suffix = string.format("mtx=%d lvl=%d grp=%d", m, l, grp)
        end
    elseif cmd_byte == 0x07 or cmd_byte == 0x08 or cmd_byte == 0x09 or
           cmd_byte == 0x11 or cmd_byte == 0x12 or cmd_byte == 0x22 then
        -- Commands whose payload layout is small and vendor-specific;
        -- dump raw bytes only.
        if data_len > 1 then
            tree:add(f.payload, tvbuf:range(start + 2, eom - (start + 2)))
        end
    else
        if data_len > 1 then
            tree:add(f.payload, tvbuf:range(start + 2, eom - (start + 2)))
        end
        if cmd_name[cmd_byte] == nil then
            ctree:add_proto_expert_info(ef_unsupported,
                string.format("Unknown command byte 0x%02x", cmd_byte))
        end
    end

    local short = cmd_short[cmd_byte] or string.format("cmd=0x%02x", cmd_byte)
    local info = string.format("%s%s",
        short,
        (info_suffix ~= "" and (" " .. info_suffix) or ""))

    if calc ~= chk then
        info = info .. " [bad chk]"
    end
    if btc ~= data_len then
        info = info .. " [bad btc]"
    end

    return info, frame_len
end

-------------------------------------------------------------------------------
-- Top-level dissector: walk the TCP segment emitting ACK/NAK pseudo-frames
-- and regular SW-P-08 frames.
-------------------------------------------------------------------------------
function p_probel.dissector(tvbuf, pktinfo, root)
    local total = tvbuf:reported_length_remaining()
    if total == 0 then return 0 end

    pktinfo.cols.protocol:set("Probel")

    local offset = 0
    local info_parts = {}

    while offset < total do
        if (total - offset) < 2 then
            -- Need at least 2 bytes to tell ACK/NAK from DLE STX.
            pktinfo.desegment_offset = offset
            pktinfo.desegment_len    = DESEGMENT_ONE_MORE_SEGMENT
            return offset
        end

        local b0 = tvbuf:range(offset, 1):uint()
        local b1 = tvbuf:range(offset + 1, 1):uint()

        if b0 == DLE and b1 == ACK then
            local ack_tree = root:add(p_probel, tvbuf:range(offset, 2))
            ack_tree:set_text("Probel DLE ACK")
            ack_tree:add(f.ackframe, true):set_generated()
            table.insert(info_parts, "ACK")
            offset = offset + 2

        elseif b0 == DLE and b1 == NAK then
            local nak_tree = root:add(p_probel, tvbuf:range(offset, 2))
            nak_tree:set_text("Probel DLE NAK")
            nak_tree:add(f.nakframe, true):set_generated()
            nak_tree:add_proto_expert_info(ef_nak, "Peer returned NAK (unsupported or malformed)")
            table.insert(info_parts, "NAK")
            offset = offset + 2

        elseif b0 == DLE and b1 == STX then
            local eom = find_eom(tvbuf, offset + 2)
            if eom == -1 then
                -- Need more bytes to see the full frame.
                pktinfo.desegment_offset = offset
                pktinfo.desegment_len    = DESEGMENT_ONE_MORE_SEGMENT
                return offset
            end
            if eom == -2 then
                -- Stray DLE X — skip one byte and keep hunting.
                offset = offset + 1
            else
                local info, consumed = dissect_frame(tvbuf, pktinfo, root, offset, eom)
                if info and info ~= "" then
                    table.insert(info_parts, info)
                end
                offset = offset + consumed
            end

        else
            -- Desync: drop one byte and retry. Show a minimal warning.
            local d = root:add(p_probel, tvbuf:range(offset, 1))
            d:set_text(string.format("Probel desync: drop 0x%02x", b0))
            offset = offset + 1
        end
    end

    if #info_parts > 0 then
        pktinfo.cols.info:set(table.concat(info_parts, " | "))
    end
    return offset
end

-------------------------------------------------------------------------------
-- Register on TCP port 2008 (SW-P-08 default).
-------------------------------------------------------------------------------
local tcp_port = DissectorTable.get("tcp.port")
tcp_port:add(PROBEL_TCP_PORT, p_probel)
