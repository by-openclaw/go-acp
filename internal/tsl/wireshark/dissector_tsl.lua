-------------------------------------------------------------------------------
--
-- Wireshark Lua Dissector: TSL UMD protocol v3.1 / v4.0 / v5.0
--
-- Handles:
--   - v3.1 (18-byte UDP datagram): HEADER | CTRL | DATA(16 ASCII)
--   - v4.0 (v3.1 + CHKSUM + VBC + XDATA[2] bytes)
--   - v5.0 UDP: PBC(2LE) | VER(1) | FLAGS(1) | SCREEN(2LE) | DMSG+
--   - v5.0 TCP wrapper: DLE(0xFE) STX(0x02) + byte-stuffed packet
--
-- Default ports (configurable via `tsl.udp_v31_v40_port`, `tsl.udp_v50_port`,
-- `tsl.tcp_v50_port` preferences):
--   v3.1 / v4.0 : UDP 4000
--   v5.0        : UDP 8901, TCP 8901
--
-- Compatible with Wireshark 4.x (Lua 5.2 / 5.3).
-- Spec authority: internal/tsl/assets/tsl-umd-protocol.txt
--
-------------------------------------------------------------------------------

local tsl = Proto("tsl", "TSL UMD Protocol (v3.1 / v4.0 / v5.0)")

-- ===== User preferences =====
tsl.prefs.udp_v31_v40_port = Pref.uint("UDP port — v3.1 / v4.0", 4000, "Default 4000")
tsl.prefs.udp_v50_port     = Pref.uint("UDP port — v5.0",       8901, "Default 8901 (Kaleido)")
tsl.prefs.tcp_v50_port     = Pref.uint("TCP port — v5.0 (DLE/STX)", 8901, "Default 8901")

-- ===== Field definitions =====
local f = tsl.fields

-- Common
f.version    = ProtoField.string("tsl.version", "Version")
f.address    = ProtoField.uint8("tsl.address", "Address",    base.DEC)
f.screen     = ProtoField.uint16("tsl.screen", "Screen",     base.DEC)
f.v50_index  = ProtoField.uint16("tsl.v50.index",  "Display Index", base.DEC)
f.v50_screen = ProtoField.uint16("tsl.v50.screen", "Screen",        base.DEC)
f.text       = ProtoField.string("tsl.text", "UMD Text")

-- v3.1 CTRL
f.v31_ctrl     = ProtoField.uint8("tsl.v31.ctrl", "CTRL", base.HEX)
f.v31_tally1   = ProtoField.bool("tsl.v31.tally1", "Tally 1", 8, nil, 0x01)
f.v31_tally2   = ProtoField.bool("tsl.v31.tally2", "Tally 2", 8, nil, 0x02)
f.v31_tally3   = ProtoField.bool("tsl.v31.tally3", "Tally 3", 8, nil, 0x04)
f.v31_tally4   = ProtoField.bool("tsl.v31.tally4", "Tally 4", 8, nil, 0x08)
f.v31_bright   = ProtoField.uint8("tsl.v31.brightness", "Brightness", base.DEC, {[0]="off", [1]="1/7", [2]="1/2", [3]="full"}, 0x30)
f.v31_rsvd6    = ProtoField.bool("tsl.v31.reserved6", "CTRL bit 6 reserved (must be 0)", 8, nil, 0x40)
f.v31_bit7     = ProtoField.bool("tsl.v31.bit7", "CTRL bit 7 (must be 0)", 8, nil, 0x80)

-- v4.0 extras
f.v40_chksum     = ProtoField.uint8("tsl.v40.chksum", "CHKSUM (2's-complement mod 128)", base.HEX)
f.v40_vbc        = ProtoField.uint8("tsl.v40.vbc", "VBC", base.HEX)
f.v40_vbc_ver    = ProtoField.uint8("tsl.v40.vbc_version", "VBC minor version", base.DEC, nil, 0x70)
f.v40_vbc_count  = ProtoField.uint8("tsl.v40.vbc_xdata_count", "VBC XDATA byte count", base.DEC, nil, 0x0F)
f.v40_xbyte      = ProtoField.uint8("tsl.v40.xbyte", "XDATA byte", base.HEX)
f.v40_xb_lh      = ProtoField.uint8("tsl.v40.xbyte.lh", "LH tally", base.DEC,   {[0]="off", [1]="red", [2]="green", [3]="amber"}, 0x30)
f.v40_xb_text    = ProtoField.uint8("tsl.v40.xbyte.text", "Text tally", base.DEC, {[0]="off", [1]="red", [2]="green", [3]="amber"}, 0x0C)
f.v40_xb_rh      = ProtoField.uint8("tsl.v40.xbyte.rh", "RH tally", base.DEC,   {[0]="off", [1]="red", [2]="green", [3]="amber"}, 0x03)

-- v5.0 envelope
f.v50_pbc    = ProtoField.uint16("tsl.v50.pbc", "PBC (body byte count)", base.DEC)
f.v50_ver    = ProtoField.uint8("tsl.v50.ver", "VER (minor version)", base.DEC)
f.v50_flags  = ProtoField.uint8("tsl.v50.flags", "FLAGS", base.HEX)
f.v50_flags_utf16   = ProtoField.bool("tsl.v50.flags.utf16", "UTF-16LE text",     8, nil, 0x01)
f.v50_flags_scontrol= ProtoField.bool("tsl.v50.flags.scontrol", "SCONTROL mode",  8, nil, 0x02)
f.v50_flags_rsvd    = ProtoField.uint8("tsl.v50.flags.reserved", "Reserved bits 2-7", base.HEX, nil, 0xFC)

-- v5.0 DMSG
f.v50_dmsg_ctrl = ProtoField.uint16("tsl.v50.dmsg.control", "CONTROL", base.HEX)
f.v50_dmsg_rh   = ProtoField.uint16("tsl.v50.dmsg.rh", "RH tally", base.DEC,   {[0]="off", [1]="red", [2]="green", [3]="amber"}, 0x0003)
f.v50_dmsg_text = ProtoField.uint16("tsl.v50.dmsg.text_tally", "Text tally", base.DEC, {[0]="off", [1]="red", [2]="green", [3]="amber"}, 0x000C)
f.v50_dmsg_lh   = ProtoField.uint16("tsl.v50.dmsg.lh", "LH tally", base.DEC,   {[0]="off", [1]="red", [2]="green", [3]="amber"}, 0x0030)
f.v50_dmsg_brt  = ProtoField.uint16("tsl.v50.dmsg.brightness", "Brightness", base.DEC, {[0]="0", [1]="1", [2]="2", [3]="3"}, 0x00C0)
f.v50_dmsg_rsvd = ProtoField.uint16("tsl.v50.dmsg.reserved", "Reserved bits 8-14", base.HEX, nil, 0x7F00)
f.v50_dmsg_cd   = ProtoField.bool("tsl.v50.dmsg.control_data", "Control Data flag", 16, nil, 0x8000)
f.v50_dmsg_len  = ProtoField.uint16("tsl.v50.dmsg.length", "LENGTH", base.DEC)

-- Expert-info anchors
local ef_v31_rsvd_bit = ProtoExpert.new("tsl.v31.reserved_bit", "v3.1 CTRL reserved bit set", expert.group.PROTOCOL, expert.severity.WARN)
local ef_v40_chksum   = ProtoExpert.new("tsl.v40.chksum_fail", "v4.0 CHKSUM mismatch", expert.group.CHECKSUM, expert.severity.WARN)
local ef_v40_ver      = ProtoExpert.new("tsl.v40.version_mismatch", "v4.0 VBC minor version != 0", expert.group.PROTOCOL, expert.severity.NOTE)
local ef_v50_rsvd     = ProtoExpert.new("tsl.v50.reserved_bit", "v5.0 reserved bit set", expert.group.PROTOCOL, expert.severity.NOTE)
local ef_v50_control  = ProtoExpert.new("tsl.v50.control_data", "v5.0 Control-Data flag set (undefined in this version)", expert.group.PROTOCOL, expert.severity.NOTE)
local ef_v50_bcast    = ProtoExpert.new("tsl.v50.broadcast", "v5.0 broadcast address (0xFFFF)", expert.group.COMMENTS_GROUP, expert.severity.CHAT)
local ef_unknown      = ProtoExpert.new("tsl.unknown", "Unrecognised TSL payload shape", expert.group.PROTOCOL, expert.severity.WARN)

tsl.experts = { ef_v31_rsvd_bit, ef_v40_chksum, ef_v40_ver, ef_v50_rsvd, ef_v50_control, ef_v50_bcast, ef_unknown }

-------------------------------------------------------------------------------
-- Helpers
-------------------------------------------------------------------------------

local DLE = 0xFE
local STX = 0x02

-- Arithmetic bit helpers (portable across Lua 5.2/5.3/5.4; no bit32/bit).
local function has_bit(x, mask) return (x % (mask * 2)) >= mask end

-- v3.1 CHKSUM: 2's-comp of sum(HEADER+CTRL+DATA) mod 128
local function v40_compute_chksum(buf)
    local sum = 0
    for i = 0, 17 do
        sum = sum + buf(i, 1):uint()
    end
    return ((-sum) % 128) % 128
end

-- Unstuff a DLE/STX-wrapped TCP segment in-place into a lua string.
-- Returns (unstuffed bytes, bytes consumed from input) or nil if the
-- segment doesn't start with DLE/STX or doesn't contain a complete PBC-
-- length body.
local function unstuff_tcp(buf, offset, length)
    if length < 6 then return nil end
    if buf(offset, 1):uint() ~= DLE then return nil end
    if buf(offset+1, 1):uint() ~= STX then return nil end

    local out = {}
    local i = offset + 2
    local stop = offset + length
    -- Read 2 unstuffed bytes for PBC
    local function read_unstuffed()
        if i >= stop then return nil end
        local b = buf(i, 1):uint()
        i = i + 1
        if b == DLE then
            if i >= stop then return nil end
            local nxt = buf(i, 1):uint()
            i = i + 1
            if nxt ~= DLE then return nil end
        end
        return b
    end

    local p0 = read_unstuffed(); if not p0 then return nil end
    local p1 = read_unstuffed(); if not p1 then return nil end
    out[#out+1] = string.char(p0)
    out[#out+1] = string.char(p1)
    local pbc = p0 + p1 * 256
    for _ = 1, pbc do
        local b = read_unstuffed(); if not b then return nil end
        out[#out+1] = string.char(b)
    end
    return table.concat(out), (i - offset)
end

-------------------------------------------------------------------------------
-- v3.1 / v4.0 dissector (UDP)
-------------------------------------------------------------------------------

local function dissect_v31(tvb, pinfo, tree)
    local t = tree:add(tsl, tvb(0, 18), "TSL UMD v3.1")
    t:add(f.version, "3.1"):set_generated()
    local hdr = tvb(0, 1):uint()
    local addr = hdr % 128
    t:add(f.address, tvb(0, 1), addr)

    local ctrl_item = t:add(f.v31_ctrl, tvb(1, 1))
    ctrl_item:add(f.v31_tally1, tvb(1, 1))
    ctrl_item:add(f.v31_tally2, tvb(1, 1))
    ctrl_item:add(f.v31_tally3, tvb(1, 1))
    ctrl_item:add(f.v31_tally4, tvb(1, 1))
    ctrl_item:add(f.v31_bright, tvb(1, 1))
    local ctrl = tvb(1, 1):uint()
    if has_bit(ctrl, 0x40) then
        ctrl_item:add(f.v31_rsvd6, tvb(1, 1)):add_expert_info(ef_v31_rsvd_bit)
    end
    if has_bit(ctrl, 0x80) then
        ctrl_item:add(f.v31_bit7, tvb(1, 1)):add_expert_info(ef_v31_rsvd_bit)
    end

    t:add(f.text, tvb(2, 16), tvb(2, 16):string())
    pinfo.cols.info:set(string.format("v3.1 addr=%d tallies=%s%s%s%s",
        addr,
        has_bit(ctrl, 0x01) and "1" or "-",
        has_bit(ctrl, 0x02) and "2" or "-",
        has_bit(ctrl, 0x04) and "3" or "-",
        has_bit(ctrl, 0x08) and "4" or "-"))
end

local function dissect_v40(tvb, pinfo, tree)
    local t = tree:add(tsl, tvb(), "TSL UMD v4.0")
    t:add(f.version, "4.0"):set_generated()
    local hdr = tvb(0, 1):uint()
    t:add(f.address, tvb(0, 1), hdr % 128)

    local ctrl_item = t:add(f.v31_ctrl, tvb(1, 1))
    ctrl_item:add(f.v31_tally1, tvb(1, 1))
    ctrl_item:add(f.v31_tally2, tvb(1, 1))
    ctrl_item:add(f.v31_tally3, tvb(1, 1))
    ctrl_item:add(f.v31_tally4, tvb(1, 1))
    ctrl_item:add(f.v31_bright, tvb(1, 1))

    t:add(f.text, tvb(2, 16), tvb(2, 16):string())

    -- CHKSUM
    local chksum = tvb(18, 1):uint()
    local expected = v40_compute_chksum(tvb)
    local chk_item = t:add(f.v40_chksum, tvb(18, 1))
    if chksum ~= expected then
        chk_item:add_expert_info(ef_v40_chksum, string.format("got 0x%02x, expected 0x%02x", chksum, expected))
    end

    -- VBC
    local vbc = tvb(19, 1):uint()
    local vbc_item = t:add(f.v40_vbc, tvb(19, 1))
    vbc_item:add(f.v40_vbc_ver, tvb(19, 1))
    vbc_item:add(f.v40_vbc_count, tvb(19, 1))
    local minor = math.floor(vbc / 16) % 8
    if minor ~= 0 then
        vbc_item:add_expert_info(ef_v40_ver, string.format("minor=%d", minor))
    end

    -- XDATA
    local xcount = vbc % 16
    if xcount >= 1 and tvb:len() >= 21 then
        local xb = tvb(20, 1)
        local xi = t:add(f.v40_xbyte, xb, xb:uint(), "XDATA Display L")
        xi:add(f.v40_xb_lh, xb)
        xi:add(f.v40_xb_text, xb)
        xi:add(f.v40_xb_rh, xb)
    end
    if xcount >= 2 and tvb:len() >= 22 then
        local xb = tvb(21, 1)
        local xi = t:add(f.v40_xbyte, xb, xb:uint(), "XDATA Display R")
        xi:add(f.v40_xb_lh, xb)
        xi:add(f.v40_xb_text, xb)
        xi:add(f.v40_xb_rh, xb)
    end

    pinfo.cols.info:set(string.format("v4.0 addr=%d (XDATA=%d)", hdr % 128, xcount))
end

-------------------------------------------------------------------------------
-- v5.0 dissector (UDP or un-stuffed TCP body)
-------------------------------------------------------------------------------

local function dissect_v50(tvb, pinfo, tree)
    local t = tree:add(tsl, tvb(), "TSL UMD v5.0")
    t:add(f.version, "5.0"):set_generated()

    local pbc = tvb(0, 2):le_uint()
    local ver = tvb(2, 1):uint()
    local flags = tvb(3, 1):uint()
    local screen = tvb(4, 2):le_uint()

    t:add_le(f.v50_pbc, tvb(0, 2))
    t:add(f.v50_ver, tvb(2, 1))
    local fi = t:add(f.v50_flags, tvb(3, 1))
    fi:add(f.v50_flags_utf16, tvb(3, 1))
    fi:add(f.v50_flags_scontrol, tvb(3, 1))
    fi:add(f.v50_flags_rsvd, tvb(3, 1))
    if math.floor(flags / 4) > 0 then
        fi:add_expert_info(ef_v50_rsvd)
    end

    local si = t:add_le(f.v50_screen, tvb(4, 2))
    if screen == 0xFFFF then si:add_expert_info(ef_v50_bcast) end

    local cursor = 6
    local idx = 0
    local utf16 = has_bit(flags, 0x01)
    while cursor < tvb:len() do
        if cursor + 4 > tvb:len() then break end
        local index = tvb(cursor, 2):le_uint()
        local control = tvb(cursor+2, 2):le_uint()

        local dmsg_item = tree:add(tsl, tvb(cursor, tvb:len() - cursor), string.format("DMSG[%d] index=%d", idx, index))
        dmsg_item:add_le(f.v50_index, tvb(cursor, 2))
        if index == 0xFFFF then dmsg_item:add_expert_info(ef_v50_bcast) end
        local ci = dmsg_item:add_le(f.v50_dmsg_ctrl, tvb(cursor+2, 2))
        ci:add_le(f.v50_dmsg_rh, tvb(cursor+2, 2))
        ci:add_le(f.v50_dmsg_text, tvb(cursor+2, 2))
        ci:add_le(f.v50_dmsg_lh, tvb(cursor+2, 2))
        ci:add_le(f.v50_dmsg_brt, tvb(cursor+2, 2))
        ci:add_le(f.v50_dmsg_rsvd, tvb(cursor+2, 2))
        ci:add_le(f.v50_dmsg_cd, tvb(cursor+2, 2))
        if math.floor(control / 256) % 128 > 0 then ci:add_expert_info(ef_v50_rsvd) end
        if control >= 0x8000 then
            ci:add_expert_info(ef_v50_control)
            cursor = cursor + 4
            idx = idx + 1
            break
        end

        if cursor + 6 > tvb:len() then break end
        local len = tvb(cursor+4, 2):le_uint()
        dmsg_item:add_le(f.v50_dmsg_len, tvb(cursor+4, 2))
        local text_end = cursor + 6 + len
        if text_end <= tvb:len() then
            local text_buf = tvb(cursor + 6, len)
            local text_str
            if utf16 then
                -- Decode UTF-16LE code units as a raw byte string; Wireshark
                -- renders the tvb as ASCII — we prefer a "[utf-16le]" tag.
                text_str = "[utf-16le " .. len .. "B]"
            else
                text_str = text_buf:string()
            end
            dmsg_item:add(f.text, text_buf, text_str)
        end
        cursor = text_end
        idx = idx + 1
    end

    pinfo.cols.info:set(string.format("v5.0 PBC=%d VER=%d screen=%d dmsgs=%d flags=0x%02x",
        pbc, ver, screen, idx, flags))
end

-------------------------------------------------------------------------------
-- Heuristic top-level dispatcher (UDP)
-------------------------------------------------------------------------------

local function udp_heuristic(tvb, pinfo, tree)
    local len = tvb:len()
    -- v5.0 UDP: PBC(2LE) + 4 header bytes minimum, then body of PBC bytes
    if len >= 6 then
        local pbc = tvb(0, 2):le_uint()
        if pbc + 2 == len and tvb(2, 1):uint() == 0 then
            pinfo.cols.protocol = "TSL-v5"
            dissect_v50(tvb, pinfo, tree)
            return
        end
    end
    -- v3.1: exactly 18 bytes, HEADER bit 7 set
    if len == 18 and has_bit(tvb(0, 1):uint(), 0x80) then
        pinfo.cols.protocol = "TSL-v3.1"
        dissect_v31(tvb, pinfo, tree)
        return
    end
    -- v4.0: 20-22 bytes, HEADER bit 7 set
    if len >= 20 and len <= 22 and has_bit(tvb(0, 1):uint(), 0x80) then
        pinfo.cols.protocol = "TSL-v4"
        dissect_v40(tvb, pinfo, tree)
        return
    end
    pinfo.cols.protocol = "TSL?"
    tree:add(tsl, tvb(), "Unrecognised TSL payload"):add_expert_info(ef_unknown)
end

-------------------------------------------------------------------------------
-- TCP v5.0 DLE/STX dissector (PDU-oriented)
-------------------------------------------------------------------------------

local function tcp_dissect(tvb, pinfo, tree)
    local pkt_bytes, consumed = unstuff_tcp(tvb, 0, tvb:len())
    if not pkt_bytes then
        -- Incomplete — ask Wireshark to wait for more data.
        pinfo.desegment_offset = 0
        pinfo.desegment_len = DESEGMENT_ONE_MORE_SEGMENT
        return 0
    end
    pinfo.cols.protocol = "TSL-v5/TCP"
    -- Build a synthetic Tvb from the un-stuffed body and dissect.
    local inner_tvb = ByteArray.new(pkt_bytes, true):tvb("un-stuffed TSL v5.0")
    dissect_v50(inner_tvb, pinfo, tree)
    return consumed
end

-------------------------------------------------------------------------------
-- Entry point
-------------------------------------------------------------------------------

function tsl.dissector(tvb, pinfo, tree)
    if tvb:len() >= 2 and tvb(0, 1):uint() == DLE and tvb(1, 1):uint() == STX then
        return tcp_dissect(tvb, pinfo, tree)
    end
    udp_heuristic(tvb, pinfo, tree)
end

-- Port registration (apply current preferences).
local function register_ports()
    local udp = DissectorTable.get("udp.port")
    local tcp = DissectorTable.get("tcp.port")
    udp:add(tsl.prefs.udp_v31_v40_port, tsl)
    udp:add(tsl.prefs.udp_v50_port, tsl)
    tcp:add(tsl.prefs.tcp_v50_port, tsl)
end

register_ports()

function tsl.prefs_changed()
    -- Re-register on port change. Wireshark handles removal of stale
    -- entries when the port differs from the previous binding.
    register_ports()
end
