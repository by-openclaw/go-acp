-------------------------------------------------------------------------------
--
-- Dissector for Axon Control Protocol version 1
--
-------------------------------------------------------------------------------

----------------------------------------
-- do not modify this table
local debug_level = {
    DISABLED = 0,
    LEVEL_1 = 1,
    LEVEL_2 = 2
}

----------------------------------------
-- set this DEBUG to debug_level.LEVEL_1 to enable printing debug_level info
-- set it to debug_level.LEVEL_2 to enable really verbose printing
-- set it to debug_level.DISABLED to disable debug printing
-- note: this will be overridden by user's preference settings
local DEBUG = debug_level.LEVEL_1

-- a table of our default settings - these can be changed by changing
-- the preferences through the GUI or command-line; the Lua-side of that
-- preference handling is at the end of this script file
local default_settings =
{
    debug_level = DEBUG,
    enabled = true, -- whether this dissector is enabled or not
    subdissect = true, -- whether to call sub-dissector or not
}


local dprint = function() end
local dprint2 = function() end
local function resetDebugLevel()
    if default_settings.debug_level > debug_level.DISABLED then
        dprint = function(...)
            info(table.concat({ "Lua: ", ... }, " "))
        end

        if default_settings.debug_level > debug_level.LEVEL_1 then
            dprint2 = dprint
        end
    else
        dprint = function() end
        dprint2 = dprint
    end
end

-- call it now
resetDebugLevel()

--------------------------------------------------------------------------------
-- creates a Proto object, but doesn't register it yet
local acpv1_proto = Proto("acpv1", "Axon Control Protocol V1")

----------------------------------------
-- a function to convert tables of enumerated types to value-string tables
-- i.e., from { "name" = number } to { number = "name" }
local function makeValString(enumTable)
    local t = {}
    for name, num in pairs(enumTable) do
        t[num] = name
    end
    return t
end

local protocols = {
    ACPV1 = 0x01,
    ACPV2 = 0x02,
    NEGOTIATE = 0x00,
}
local protocols_valstr = makeValString(protocols)

local mtype = {
    ANNOUNCEMENT_MESSAGE = 0x00,
    REQUEST_MESSAGE = 0x01,
    REPLY_MESSAGE = 0x02,
    ERROR_REPLY_MESSAGE = 0x03,
}
local mtype_valstr = makeValString(mtype)

local mids = {
    GET_VAL = 0x00,
    SET_VAL = 0x01,
    INCR_VAL = 0x02,
    DECR_VAL = 0x03,
    SET_DEF_VAL = 0x04,
    GET_OBJECTS = 0x05,
    GET_PRESET_IDX = 0x06,
    SET_PRESET_IDX = 0x07,
    GET_PRP = 0x08,
    SET_PRP = 0x09
}
local mids_valstr = makeValString(mids)

local objgrp = {
    ROOT = 0x00,
    IDENTITY = 0x01,
    CONTROL = 0x02,
    STATUS = 0x03,
    ALARM = 0x04,
    FILE = 0x05,
    FRAME = 0x06,
}
local objgrp_valstr = makeValString(objgrp)

----------------------------------------
-- a table of all of our Protocol's fields
local hdr_fields =
{
    mtid = ProtoField.uint32("acpv1.mtid", "Msg Transaction ID", base.HEX),
    pver = ProtoField.uint8("acpv1.pver", "Protocol", base.HEX, protocols_valstr),
    mtype = ProtoField.uint8("acpv1.mtype", "Msg Type", base.HEX, mtype_valstr),
}

-- register the ProtoFields
acpv1_proto.fields = hdr_fields

local ACPV1_HDR_LEN = 10

-- this holds the plain "data" Dissector, used by default
local dissector_data = Dissector.get("data")

-- the dissector table holds all protocols
local dissectors = DissectorTable.new("acpv1.mtype", "ACPv Message Type", ftypes.UINT8, base.HEX)

function acpv1_proto.dissector(tvbuf, pktinfo, root)
    dprint2("acpv1_proto.dissector called")

    local pktlen = tvbuf:reported_length_remaining()
    local tree = root:add(acpv1_proto, tvbuf:range(0, pktlen))

    -- now let's check it's not too short
    if pktlen < ACPV1_HDR_LEN then
        dprint("packet length", pktlen, "too short")
        return
    end

    local offset = 0
    tree:add(hdr_fields.mtid, tvbuf:range(offset, 4))
    offset = offset + 4

    tree:add(hdr_fields.pver, tvbuf:range(offset, 1))
    offset = offset + 1

    local mtype_tvbr = tvbuf:range(offset, 1)
    local mtype_val = mtype_tvbr:uint()
    tree:add(hdr_fields.mtype, mtype_tvbr)
    offset = offset + 1

    -- Append MTYPE to INFO column
    local mtype_str = mtype_valstr[mtype_val]
    if mtype_str == nil then
        mtype_str = tonumber(mtype_val, 16)
    end

    pktinfo.cols.info:append("MType=" .. mtype_str .. " ")

    -- Pass the remaining payload to the next dissector
    local dissector_proto = dissectors:get_dissector(mtype_val)
    if dissector_proto == nil then
        dissector_proto = dissector_data
    end
    dissector_proto:call(tvbuf(offset):tvb(), pktinfo, tree)

    return pktlen
end

DissectorTable.get("axonnet2.proto"):add(1, acpv1_proto)
