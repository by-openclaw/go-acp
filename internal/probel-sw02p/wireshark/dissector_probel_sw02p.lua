-------------------------------------------------------------------------------
--
-- Wireshark Lua Dissector: Probel SW-P-02 over TCP (default 2002)
--
-- Standalone dissector handling:
--   - §3.1 framing: SOM (0xFF) + COMMAND + MESSAGE + CHECKSUM
--   - 7-bit two's-complement CHECKSUM validation over COMMAND || MESSAGE
--   - §3.2 command catalogue — the 23 bytes implemented on this branch:
--       salvo (§3.2.6/7/8/14/15/36-39/53/54)    + VSM-supported bulk
--       (§3.2.3/4/5/6/9/11/47-50/60-62/64).
--   - Per-cmd decode: narrow §3.2.3 Multiplier bit-packing (bits 4-6
--     DestDIV128, bit 3 BadSource, bits 0-2 SrcDIV128), extended
--     Multiplier pair (§3.2.47/48), status byte, SalvoID.
--   - Variable-length tx 100 Extended PROTECT TALLY DUMP (§3.2.64):
--     Count byte drives the per-entry (4-byte) iteration; Count=127
--     = "controller reset" sentinel with no entries.
--   - TCP reassembly for frames split across segments.
--
-- Compatible with Wireshark 4.x (and 5.x — pure arithmetic, no Lua 5.3
-- bitops so it loads on the embedded Lua 5.2 too).
-- Spec authority:
--   internal/probel-sw02p/assets/probel-sw02/SW-P-02_issue_26.txt
--
-------------------------------------------------------------------------------

local SWP02_TCP_PORT = 2002

-- §3.1 framing byte
local SOM = 0xFF

-------------------------------------------------------------------------------
-- Command-byte catalogue (decimal comments per §3 convention).
-- Mirrored from internal/probel-sw02p/codec/commands.go +
-- command_names.go — keep the two in lockstep when new commands land.
-------------------------------------------------------------------------------

local cmd_name = {
    [0x01] = "rx 001 Interrogate",
    [0x02] = "rx 002 Connect",
    [0x03] = "tx 003 Tally",
    [0x04] = "tx 004 Crosspoint Connected",
    [0x05] = "rx 005 Connect On Go",
    [0x06] = "rx 006 Go",
    [0x07] = "rx 007 Status Request",
    [0x09] = "tx 009 Status Response - 2",
    [0x0C] = "tx 012 Connect On Go Ack",
    [0x0D] = "tx 013 Go Done Ack",
    [0x0E] = "rx 014 Source Lock Status Request",
    [0x0F] = "tx 015 Source Lock Status Response",
    [0x23] = "rx 035 Connect On Go Group Salvo",
    [0x24] = "rx 036 Go Group Salvo",
    [0x25] = "tx 037 Connect On Go Group Salvo Ack",
    [0x26] = "tx 038 Go Done Group Salvo Ack",
    [0x32] = "rx 050 Dual Controller Status Request",
    [0x33] = "tx 051 Dual Controller Status Response",
    [0x41] = "rx 065 Extended Interrogate",
    [0x42] = "rx 066 Extended Connect",
    [0x43] = "tx 067 Extended Tally",
    [0x44] = "tx 068 Extended Connected",
    [0x45] = "rx 069 Extended Connect On Go",
    [0x46] = "tx 070 Extended Connect On Go Ack",
    [0x47] = "rx 071 Extended Connect On Go Group Salvo",
    [0x48] = "tx 072 Extended Connect On Go Group Salvo Ack",
    [0x4B] = "rx 075 Router Config Request",
    [0x4C] = "tx 076 Router Config Response - 1",
    [0x4D] = "tx 077 Router Config Response - 2",
    [0x60] = "tx 096 Extended Protect Tally",
    [0x61] = "tx 097 Extended Protect Connected",
    [0x62] = "tx 098 Extended Protect Disconnected",
    [0x63] = "tx 099 Protect Device Name Response",
    [0x64] = "tx 100 Extended Protect Tally Dump",
    [0x65] = "rx 101 Extended Protect Interrogate",
    [0x66] = "rx 102 Extended Protect Connect",
    [0x67] = "rx 103 Protect Device Name Request",
    [0x68] = "rx 104 Extended Protect Disconnect",
    [0x69] = "rx 105 Extended Protect Tally Dump Request",
}

-- Fixed MESSAGE byte counts per command. Variable-length commands
-- (currently only 0x64) are sized by the dissector's per-command
-- dissector function.
local payload_len = {
    [0x01] = 2,  -- rx 001 Interrogate
    [0x02] = 3,  -- rx 002 Connect
    [0x03] = 3,  -- tx 003 Tally
    [0x04] = 3,  -- tx 004 Connected
    [0x05] = 3,  -- rx 005 Connect On Go
    [0x06] = 1,  -- rx 006 Go
    [0x07] = 1,  -- rx 007 Status Request
    [0x09] = 1,  -- tx 009 Status Response - 2
    [0x0C] = 3,  -- tx 012 Connect On Go Ack
    [0x0D] = 1,  -- tx 013 Go Done Ack
    [0x23] = 4,  -- rx 035 Connect On Go Group Salvo (dst/src narrow + salvo)
    [0x24] = 2,  -- rx 036 Go Group Salvo (op + salvo)
    [0x25] = 4,  -- tx 037 Connect On Go Group Salvo Ack
    [0x26] = 2,  -- tx 038 Go Done Group Salvo Ack (result + salvo)
    [0x41] = 2,  -- rx 065 Extended Interrogate
    [0x42] = 4,  -- rx 066 Extended Connect
    [0x43] = 5,  -- tx 067 Extended Tally
    [0x44] = 5,  -- tx 068 Extended Connected
    [0x47] = 5,  -- rx 071 Extended Connect On Go Group Salvo
    [0x48] = 5,  -- tx 072 Extended Connect On Go Group Salvo Ack
    [0x0E] = 1,  -- rx 014 Source Lock Status Request
    -- 0x0F tx 015 Source Lock Status Response — variable, see below
    [0x32] = 0,  -- rx 050 Dual Controller Status Request (no MESSAGE)
    [0x33] = 2,  -- tx 051 Dual Controller Status Response
    [0x45] = 4,  -- rx 069 Extended Connect On Go
    [0x46] = 4,  -- tx 070 Extended Connect On Go Ack
    [0x4B] = 0,  -- rx 075 Router Config Request (no MESSAGE)
    -- 0x4C tx 076 Router Config Response - 1 — variable, see below
    -- 0x4D tx 077 Router Config Response - 2 — variable, see below
    [0x60] = 5,  -- tx 096 Extended PROTECT TALLY
    [0x61] = 5,  -- tx 097 Extended PROTECT CONNECTED
    [0x62] = 5,  -- tx 098 Extended PROTECT DIS-CONNECTED
    [0x63] = 10, -- tx 099 Protect Device Name Response (2 hdr + 8 ASCII)
    -- 0x64 tx 100 Extended PROTECT TALLY DUMP — variable, see below
    [0x65] = 2,  -- rx 101 Extended PROTECT INTERROGATE
    [0x66] = 4,  -- rx 102 Extended PROTECT CONNECT
    [0x67] = 2,  -- rx 103 PROTECT DEVICE NAME REQUEST
    [0x68] = 4,  -- rx 104 Extended PROTECT DIS-CONNECT
    [0x69] = 3,  -- rx 105 Extended PROTECT TALLY DUMP REQUEST
}

local protect_state_name = {
    [0] = "Not Protected",
    [1] = "Pro-Bel Protected",
    [2] = "Pro-Bel Override Protected",
    [3] = "OEM / Router Protected",
}

-------------------------------------------------------------------------------
-- Proto + ProtoFields
-------------------------------------------------------------------------------

local p_swp02 = Proto("probel_sw02p", "Probel SW-P-02")

local f = {
    som      = ProtoField.uint8("probel_sw02p.som", "SOM", base.HEX),
    cmd      = ProtoField.uint8("probel_sw02p.cmd", "Command", base.HEX_DEC, cmd_name),
    data     = ProtoField.bytes("probel_sw02p.data", "MESSAGE"),
    cksum    = ProtoField.uint8("probel_sw02p.cksum", "Checksum", base.HEX),
    cksum_ok = ProtoField.bool("probel_sw02p.cksum_ok", "Checksum OK"),

    -- Narrow §3.2.3 Multiplier
    mult_n        = ProtoField.uint8("probel_sw02p.mult",         "Multiplier (§3.2.3)", base.HEX),
    mult_dest_div = ProtoField.uint8("probel_sw02p.mult.dest_div", "Dest DIV 128", base.DEC, nil, 0x70),
    mult_bad_src  = ProtoField.bool ("probel_sw02p.mult.bad_src",  "Bad Source / Update Disabled", 8, nil, 0x08),
    mult_src_div  = ProtoField.uint8("probel_sw02p.mult.src_div",  "Src DIV 128",  base.DEC, nil, 0x07),

    -- Common decoded fields
    dest       = ProtoField.uint16("probel_sw02p.dest",   "Destination"),
    src        = ProtoField.uint16("probel_sw02p.src",    "Source"),
    dest_mod   = ProtoField.uint8 ("probel_sw02p.dest_mod", "Dest MOD 128", base.DEC),
    src_mod    = ProtoField.uint8 ("probel_sw02p.src_mod",  "Src MOD 128",  base.DEC),

    -- Extended §3.2.47 / §3.2.48 Multiplier bytes
    dest_mult_ext = ProtoField.uint8("probel_sw02p.dest_mult_ext", "Dest Mult (§3.2.47)", base.HEX, nil, 0x7F),
    src_mult_ext  = ProtoField.uint8("probel_sw02p.src_mult_ext",  "Src Mult (§3.2.48)",  base.HEX, nil, 0x7F),

    -- Status byte (§3.2.49 / §3.2.50)
    status_ext      = ProtoField.uint8("probel_sw02p.status_ext", "Status", base.HEX),
    status_upd_off  = ProtoField.bool ("probel_sw02p.status_ext.upd_off",  "Crosspoint update disabled", 8, nil, 0x01),
    status_bad_src  = ProtoField.bool ("probel_sw02p.status_ext.bad_src",  "Bad Source",                 8, nil, 0x02),

    -- rx 006 Go + rx 036 Go Group Salvo operation byte
    go_op   = ProtoField.uint8("probel_sw02p.go_op",  "Operation", base.HEX, {[0x00]="Set", [0x01]="Clear"}),
    go_res  = ProtoField.uint8("probel_sw02p.go_res", "Result",    base.HEX, {[0x00]="Set", [0x01]="Cleared", [0x02]="Empty"}),

    -- SalvoID (§3.2.36 / §3.2.53)
    salvo_id = ProtoField.uint8("probel_sw02p.salvo_id", "SalvoID", base.DEC, nil, 0x7F),

    -- rx 007 Status Request controller
    controller = ProtoField.uint8("probel_sw02p.controller", "Controller", base.HEX, {[0]="LH", [1]="RH"}),

    -- tx 009 Status Response - 2 fields
    status2      = ProtoField.uint8("probel_sw02p.status2", "Status", base.HEX),
    status2_idle = ProtoField.bool ("probel_sw02p.status2.idle",     "Idle system",   8, nil, 0x40),
    status2_bus  = ProtoField.bool ("probel_sw02p.status2.bus_fault", "Bus fault",    8, nil, 0x20),
    status2_hot  = ProtoField.bool ("probel_sw02p.status2.overheat",  "Overheat",     8, nil, 0x10),

    -- Extended PROTECT common
    protect_details = ProtoField.uint8 ("probel_sw02p.protect", "Protect details", base.HEX),
    protect_state   = ProtoField.uint8 ("probel_sw02p.protect.state", "Protect state",
                        base.DEC, protect_state_name, 0x03),
    device          = ProtoField.uint16("probel_sw02p.device", "Device number"),

    -- tx 100 tally dump
    dump_count    = ProtoField.uint8 ("probel_sw02p.dump.count",   "Entry count / sentinel", base.DEC),
    dump_entry    = ProtoField.bytes ("probel_sw02p.dump.entry",    "Entry"),
    dump_entry_dest   = ProtoField.uint16("probel_sw02p.dump.entry.dest",   "Entry dest"),
    dump_entry_device = ProtoField.uint16("probel_sw02p.dump.entry.device", "Entry device (0-1023)"),
    dump_entry_protect = ProtoField.uint8 ("probel_sw02p.dump.entry.protect", "Entry protect",
                          base.DEC, protect_state_name),
}
p_swp02.fields = {
    f.som, f.cmd, f.data, f.cksum, f.cksum_ok,
    f.mult_n, f.mult_dest_div, f.mult_bad_src, f.mult_src_div,
    f.dest, f.src, f.dest_mod, f.src_mod,
    f.dest_mult_ext, f.src_mult_ext,
    f.status_ext, f.status_upd_off, f.status_bad_src,
    f.go_op, f.go_res, f.salvo_id,
    f.controller,
    f.status2, f.status2_idle, f.status2_bus, f.status2_hot,
    f.protect_details, f.protect_state, f.device,
    f.dump_count, f.dump_entry, f.dump_entry_dest, f.dump_entry_device, f.dump_entry_protect,
}

local ef_bad_som = ProtoExpert.new("probel_sw02p.bad_som",     "Bad SOM (expected 0xFF)",
                                    expert.group.MALFORMED, expert.severity.ERROR)
local ef_bad_cksum = ProtoExpert.new("probel_sw02p.bad_cksum", "Bad checksum",
                                      expert.group.CHECKSUM, expert.severity.ERROR)
local ef_unknown_cmd = ProtoExpert.new("probel_sw02p.unknown_cmd", "Unknown command byte",
                                        expert.group.PROTOCOL, expert.severity.WARN)
p_swp02.experts = { ef_bad_som, ef_bad_cksum, ef_unknown_cmd }

-------------------------------------------------------------------------------
-- Helpers
-------------------------------------------------------------------------------

-- 7-bit two's-complement checksum over bytes in tvb from off for len.
-- Pure arithmetic; no Lua 5.3 bitops.
local function checksum7(tvb, off, len)
    local sum = 0
    for i = 0, len - 1 do
        sum = sum + tvb(off + i, 1):uint()
    end
    local neg = (-sum) % 256
    return neg % 128
end

-- Multiplier §3.2.3 decode helpers (dst/src DIV 128 from byte).
local function mult_dest_hi(m) return math.floor(m / 16) % 8 end
local function mult_src_hi(m)  return m % 8 end
local function mult_bad_src(m) return (math.floor(m / 8) % 2) == 1 end

-- Extended multiplier §3.2.47 / §3.2.48 — low 7 bits.
local function ext_mult(m) return m % 128 end

-------------------------------------------------------------------------------
-- Per-command dissectors
-------------------------------------------------------------------------------

-- Narrow §3.2.3 Multiplier + Dest + (optional) Src on a payload of 2-3 bytes.
local function dissect_narrow_multiplier_pair(tree, tvb, off, plen)
    local m = tvb(off, 1):uint()
    local dest_mod = tvb(off + 1, 1):uint()
    local dest = mult_dest_hi(m) * 128 + dest_mod

    local mt = tree:add(f.mult_n, tvb(off, 1))
    mt:add(f.mult_dest_div, tvb(off, 1))
    mt:add(f.mult_bad_src,  tvb(off, 1))
    mt:add(f.mult_src_div,  tvb(off, 1))

    tree:add(f.dest_mod, tvb(off + 1, 1))
    tree:add(f.dest,     tvb(off, 2), dest):set_generated()

    if plen >= 3 then
        local src_mod = tvb(off + 2, 1):uint()
        local src = mult_src_hi(m) * 128 + src_mod
        tree:add(f.src_mod, tvb(off + 2, 1))
        tree:add(f.src,     tvb(off + 2, 1), src):set_generated()
        return string.format("dst=%d src=%d%s", dest, src,
            mult_bad_src(m) and " [bad_src]" or "")
    end
    return string.format("dst=%d", dest)
end

-- Extended §3.2.47/48 (DestMult+DestMod [+SrcMult+SrcMod [+Status]]) on a
-- payload of 2, 4, or 5 bytes.
local function dissect_extended_fields(tree, tvb, off, plen)
    local dm = tvb(off, 1):uint()
    local dest_mod = tvb(off + 1, 1):uint()
    local dest = ext_mult(dm) * 128 + dest_mod
    tree:add(f.dest_mult_ext, tvb(off, 1))
    tree:add(f.dest_mod,      tvb(off + 1, 1))
    tree:add(f.dest,          tvb(off, 2), dest):set_generated()

    if plen < 4 then
        return string.format("dst=%d", dest)
    end

    local sm = tvb(off + 2, 1):uint()
    local src_mod = tvb(off + 3, 1):uint()
    local src = ext_mult(sm) * 128 + src_mod
    tree:add(f.src_mult_ext, tvb(off + 2, 1))
    tree:add(f.src_mod,      tvb(off + 3, 1))
    tree:add(f.src,          tvb(off + 2, 2), src):set_generated()

    local info = string.format("dst=%d src=%d", dest, src)

    if plen >= 5 then
        local s = tvb(off + 4, 1):uint()
        local st = tree:add(f.status_ext, tvb(off + 4, 1))
        st:add(f.status_upd_off, tvb(off + 4, 1))
        st:add(f.status_bad_src, tvb(off + 4, 1))
        local flags = {}
        if (s % 2) == 1 then flags[#flags+1] = "upd_off" end
        if math.floor(s / 2) % 2 == 1 then flags[#flags+1] = "bad_src" end
        if #flags > 0 then info = info .. " [" .. table.concat(flags, ",") .. "]" end
    end
    return info
end

-- Extended PROTECT §3.2.60-62 shared 5-byte layout.
local function dissect_extended_protect(tree, tvb, off)
    local p = tvb(off, 1):uint()
    local state = p % 4
    local pt = tree:add(f.protect_details, tvb(off, 1))
    pt:add(f.protect_state, tvb(off, 1))

    local dm_d = tvb(off + 1, 1):uint()
    local dm_o = tvb(off + 2, 1):uint()
    local dest = ext_mult(dm_d) * 128 + dm_o
    tree:add(f.dest_mult_ext, tvb(off + 1, 1))
    tree:add(f.dest_mod,      tvb(off + 2, 1))
    tree:add(f.dest,          tvb(off + 1, 2), dest):set_generated()

    local dv_d = tvb(off + 3, 1):uint()
    local dv_o = tvb(off + 4, 1):uint()
    local device = ext_mult(dv_d) * 128 + dv_o
    tree:add(f.dest_mult_ext, tvb(off + 3, 1))  -- same field class, different role
    tree:add(f.device,        tvb(off + 3, 2), device):set_generated()

    return string.format("dst=%d device=%d protect=%s", dest, device,
        protect_state_name[state] or tostring(state))
end

-- tx 100 Extended PROTECT TALLY DUMP — variable-length (§3.2.64).
local function dissect_tally_dump(tree, tvb, off, plen)
    local count = tvb(off, 1):uint()
    tree:add(f.dump_count, tvb(off, 1))
    if count == 127 then
        return "controller-reset sentinel (Count=127)"
    end
    if count == 0 then
        return "empty"
    end
    local entries_off = off + 1
    local entries_end = off + plen
    local i = 0
    while entries_off + 4 <= entries_end + 0 and i < count do
        local e_tree = tree:add(f.dump_entry, tvb(entries_off, 4))
        local dv_d = tvb(entries_off, 1):uint()
        local dv_o = tvb(entries_off + 1, 1):uint()
        local dest = ext_mult(dv_d) * 128 + dv_o
        e_tree:add(f.dump_entry_dest, tvb(entries_off, 2), dest):set_generated()

        -- Device packed: low byte bits 0-6 = MOD 128, high byte bits 0-2 = DIV 128.
        -- High byte bits 4-6 = protect state.
        local dev_lo = tvb(entries_off + 2, 1):uint()
        local dev_hi = tvb(entries_off + 3, 1):uint()
        local device = (dev_hi % 8) * 128 + (dev_lo % 128)
        local protect = math.floor(dev_hi / 16) % 8
        e_tree:add(f.dump_entry_device,  tvb(entries_off + 2, 2), device):set_generated()
        e_tree:add(f.dump_entry_protect, tvb(entries_off + 3, 1), protect):set_generated()
        e_tree:append_text(string.format(" #%d: dst=%d device=%d protect=%s",
            i, dest, device, protect_state_name[protect] or tostring(protect)))
        entries_off = entries_off + 4
        i = i + 1
    end
    return string.format("%d entries", count)
end

-- Dispatcher: given CMD byte, fixed MESSAGE length, and MESSAGE offset,
-- add per-cmd fields to the subtree and return a short Info-column suffix.
local function dissect_message(tree, tvb, cmd, msg_off, msg_len)
    if cmd == 0x01 then
        return dissect_narrow_multiplier_pair(tree, tvb, msg_off, 2)
    elseif cmd == 0x02 or cmd == 0x03 or cmd == 0x04 or cmd == 0x05 or cmd == 0x0C then
        return dissect_narrow_multiplier_pair(tree, tvb, msg_off, 3)
    elseif cmd == 0x06 then
        tree:add(f.go_op, tvb(msg_off, 1))
        local op = tvb(msg_off, 1):uint()
        return (op == 0x00) and "Set" or ((op == 0x01) and "Clear" or string.format("op=%#x", op))
    elseif cmd == 0x07 then
        tree:add(f.controller, tvb(msg_off, 1))
        local c = tvb(msg_off, 1):uint()
        return (c == 0) and "LH" or ((c == 1) and "RH" or string.format("ctl=%#x", c))
    elseif cmd == 0x09 then
        local s = tvb(msg_off, 1):uint()
        local st = tree:add(f.status2, tvb(msg_off, 1))
        st:add(f.status2_idle, tvb(msg_off, 1))
        st:add(f.status2_bus,  tvb(msg_off, 1))
        st:add(f.status2_hot,  tvb(msg_off, 1))
        local flags = {}
        if math.floor(s / 64) % 2 == 1 then flags[#flags+1] = "idle" end
        if math.floor(s / 32) % 2 == 1 then flags[#flags+1] = "bus_fault" end
        if math.floor(s / 16) % 2 == 1 then flags[#flags+1] = "overheat" end
        if #flags == 0 then return "healthy" end
        return table.concat(flags, ",")
    elseif cmd == 0x0D then
        tree:add(f.go_res, tvb(msg_off, 1))
        local r = tvb(msg_off, 1):uint()
        if r == 0 then return "Set" elseif r == 1 then return "Cleared" else return string.format("res=%#x", r) end
    elseif cmd == 0x23 or cmd == 0x25 then -- rx 035 / tx 037 — narrow dst/src + salvo
        -- Layout: DestMult (carrying DstDIV128 + SrcDIV128) + DestMod + SrcMod + SalvoID.
        -- Same Multiplier semantics as rx 05.
        local desc = dissect_narrow_multiplier_pair(tree, tvb, msg_off, 3)
        tree:add(f.salvo_id, tvb(msg_off + 3, 1))
        return string.format("%s salvo=%d", desc, tvb(msg_off + 3, 1):uint() % 128)
    elseif cmd == 0x24 then -- rx 036 Go Group Salvo: op + salvo
        tree:add(f.go_op,   tvb(msg_off, 1))
        tree:add(f.salvo_id, tvb(msg_off + 1, 1))
        local op = tvb(msg_off, 1):uint()
        return string.format("%s salvo=%d",
            (op == 0) and "Set" or ((op == 1) and "Clear" or string.format("op=%#x", op)),
            tvb(msg_off + 1, 1):uint() % 128)
    elseif cmd == 0x26 then -- tx 038
        tree:add(f.go_res,  tvb(msg_off, 1))
        tree:add(f.salvo_id, tvb(msg_off + 1, 1))
        local r = tvb(msg_off, 1):uint()
        local label = (r == 0) and "Set" or ((r == 1) and "Cleared" or ((r == 2) and "Empty" or string.format("res=%#x", r)))
        return string.format("%s salvo=%d", label, tvb(msg_off + 1, 1):uint() % 128)
    elseif cmd == 0x41 then -- rx 065 Ext Interrogate
        return dissect_extended_fields(tree, tvb, msg_off, 2)
    elseif cmd == 0x42 then -- rx 066 Ext Connect
        return dissect_extended_fields(tree, tvb, msg_off, 4)
    elseif cmd == 0x43 or cmd == 0x44 then -- tx 067 / tx 068
        return dissect_extended_fields(tree, tvb, msg_off, 5)
    elseif cmd == 0x47 or cmd == 0x48 then -- rx 071 / tx 072 — Ext dst/src + salvo
        local desc = dissect_extended_fields(tree, tvb, msg_off, 4)
        tree:add(f.salvo_id, tvb(msg_off + 4, 1))
        return string.format("%s salvo=%d", desc, tvb(msg_off + 4, 1):uint() % 128)
    elseif cmd == 0x60 or cmd == 0x61 or cmd == 0x62 then -- tx 096 / 097 / 098
        return dissect_extended_protect(tree, tvb, msg_off)
    elseif cmd == 0x64 then -- tx 100 variable
        return dissect_tally_dump(tree, tvb, msg_off, msg_len)
    end
    return nil
end

-------------------------------------------------------------------------------
-- Variable-length sizer for tx 100 (need the Count byte buffered).
-- Returns the full frame length (SOM+CMD+MESSAGE+CHECKSUM) or 0 if we
-- need more bytes to decide.
-------------------------------------------------------------------------------

-- Count set bits in the low 28 bits of n using pure arithmetic (no
-- Lua 5.3 bitops). Used by the tx 076 / tx 077 variable sizers.
local function popcount28(n)
    local c = 0
    for i = 0, 27 do
        if math.floor(n / 2 ^ i) % 2 == 1 then
            c = c + 1
        end
    end
    return c
end

-- Parse the 4-byte level bitmap used by tx 076 / tx 077. Returns
-- the 28-bit value as a plain integer (bits 0-27 significant).
local function level_bitmap(tvb, off)
    local b1 = tvb(off, 1):uint() % 128
    local b2 = tvb(off + 1, 1):uint() % 128
    local b3 = tvb(off + 2, 1):uint() % 128
    local b4 = tvb(off + 3, 1):uint() % 128
    return b1 * (2 ^ 21) + b2 * (2 ^ 14) + b3 * (2 ^ 7) + b4
end

local function full_frame_len(tvb, off)
    -- Need SOM + CMD minimum to classify.
    if tvb:len() < off + 2 then return 0 end
    local cmd = tvb(off + 1, 1):uint()
    local plen = payload_len[cmd]
    if plen ~= nil then
        return 1 + 1 + plen + 1
    end
    if cmd == 0x0F then
        -- tx 015 Source Lock Status Response: self-declared 2-byte
        -- length header (bytes 1-2 = MESSAGE DIV 128 + MOD 128).
        if tvb:len() < off + 4 then return 0 end
        local msg = tvb(off + 2, 1):uint() * 128 + tvb(off + 3, 1):uint()
        return 1 + 1 + msg + 1
    end
    if cmd == 0x4C then
        -- tx 076 Router Config Response - 1: 4-byte bitmap + 4 bytes
        -- per set bit.
        if tvb:len() < off + 2 + 4 then return 0 end
        local m = level_bitmap(tvb, off + 2)
        local n = popcount28(m)
        return 1 + 1 + 4 + 4 * n + 1
    end
    if cmd == 0x4D then
        -- tx 077 Router Config Response - 2: 4-byte bitmap + 10 bytes
        -- per set bit.
        if tvb:len() < off + 2 + 4 then return 0 end
        local m = level_bitmap(tvb, off + 2)
        local n = popcount28(m)
        return 1 + 1 + 4 + 10 * n + 1
    end
    if cmd == 0x64 then
        if tvb:len() < off + 3 then return 0 end
        local count = tvb(off + 2, 1):uint()
        if count == 0 or count == 127 then
            return 1 + 1 + 1 + 1
        end
        return 1 + 1 + 1 + 4 * count + 1
    end
    -- Unknown command: we have no way to peel the MESSAGE; fall back
    -- to consuming the minimum 3 bytes (SOM+CMD+checksum) and let the
    -- upper layer flag the expert info.
    return 3
end

-------------------------------------------------------------------------------
-- Main dissection — one frame at a time, fed by dissect_tcp_pdus.
-------------------------------------------------------------------------------

local function dissect_one_frame(tvb, pinfo, tree)
    local len = tvb:len()
    local subtree = tree:add(p_swp02, tvb(), "Probel SW-P-02")
    pinfo.cols.protocol = "Probel SW-P-02"

    if len < 3 then
        pinfo.cols.info:set("[incomplete frame]")
        return len
    end

    local som = tvb(0, 1):uint()
    subtree:add(f.som, tvb(0, 1))
    if som ~= SOM then
        subtree:add_proto_expert_info(ef_bad_som, string.format("SOM=0x%02X", som))
        pinfo.cols.info:set(string.format("Bad SOM 0x%02X", som))
        return len
    end

    local cmd = tvb(1, 1):uint()
    subtree:add(f.cmd, tvb(1, 1))

    local plen = payload_len[cmd]
    local msg_len
    if plen ~= nil then
        msg_len = plen
    elseif cmd == 0x0F then
        -- tx 015 self-declared 2-byte length header.
        msg_len = tvb(2, 1):uint() * 128 + tvb(3, 1):uint()
    elseif cmd == 0x4C then
        -- tx 076: 4-byte bitmap + 4 bytes per set bit.
        msg_len = 4 + 4 * popcount28(level_bitmap(tvb, 2))
    elseif cmd == 0x4D then
        -- tx 077: 4-byte bitmap + 10 bytes per set bit.
        msg_len = 4 + 10 * popcount28(level_bitmap(tvb, 2))
    elseif cmd == 0x64 then
        -- tx 100 variable
        local count = tvb(2, 1):uint()
        if count == 0 or count == 127 then
            msg_len = 1
        else
            msg_len = 1 + 4 * count
        end
    else
        -- Unknown command — treat the whole body as opaque.
        msg_len = len - 3
        subtree:add_proto_expert_info(ef_unknown_cmd,
            string.format("Unknown command 0x%02X", cmd))
    end

    local msg_off = 2
    if msg_len > 0 then
        subtree:add(f.data, tvb(msg_off, msg_len))
    end
    local cksum_off = msg_off + msg_len
    if cksum_off + 1 > len then
        pinfo.cols.info:set(string.format("%s [truncated]",
            cmd_name[cmd] or string.format("cmd 0x%02X", cmd)))
        return len
    end
    local chk = tvb(cksum_off, 1):uint()
    subtree:add(f.cksum, tvb(cksum_off, 1))

    local want_chk = checksum7(tvb, 1, 1 + msg_len)
    local ok = (chk == want_chk)
    subtree:add(f.cksum_ok, tvb(cksum_off, 1), ok):set_generated()
    if not ok then
        subtree:add_proto_expert_info(ef_bad_cksum,
            string.format("got 0x%02X, want 0x%02X", chk, want_chk))
    end

    local info_suffix
    if plen ~= nil or cmd == 0x64 then
        info_suffix = dissect_message(subtree, tvb, cmd, msg_off, msg_len)
    end

    local label = cmd_name[cmd] or string.format("cmd 0x%02X", cmd)
    if info_suffix and info_suffix ~= "" then
        label = label .. ": " .. info_suffix
    end
    if not ok then
        label = label .. " [BAD CHECKSUM]"
    end
    pinfo.cols.info:set(label)
    return msg_off + msg_len + 1
end

-- PDU length delegate for dissect_tcp_pdus.
local function get_pdu_length(tvb, pinfo, offset)
    if tvb:len() - offset < 2 then return 0 end
    if tvb(offset, 1):uint() ~= SOM then
        -- Resync: consume 1 byte so Wireshark advances past the bad byte.
        return 1
    end
    return full_frame_len(tvb, offset)
end

function p_swp02.dissector(tvb, pinfo, tree)
    dissect_tcp_pdus(tvb, tree, 3, get_pdu_length, dissect_one_frame)
    return tvb:len()
end

-------------------------------------------------------------------------------
-- Register on the default SW-P-02 port. Users can decode-as other ports.
-------------------------------------------------------------------------------

local tcp_table = DissectorTable.get("tcp.port")
tcp_table:add(SWP02_TCP_PORT, p_swp02)
