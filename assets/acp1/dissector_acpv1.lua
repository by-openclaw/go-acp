-------------------------------------------------------------------------------
--
-- Wireshark Lua Dissector for Axon Control Protocol version 1 (ACP1)
--
-- Handles two transport modes:
--   Mode A: UDP direct on port 2071 (no framing prefix)
--   Mode B: TCP direct on port 2071 (MLEN u32 BE prefix, TCP reassembly)
--
-- Compatible with Wireshark 4.x Lua API
--
-------------------------------------------------------------------------------

local ACP1_UDP_PORT = 2071
local ACP1_TCP_PORT = 2071
local ACP1_HDR_LEN = 7  -- MTID(4) + PVER(1) + MTYPE(1) + MADDR(1)

-------------------------------------------------------------------------------
-- Value-string tables
-------------------------------------------------------------------------------

local mtype_valstr = {
    [0] = "Announce",
    [1] = "Request",
    [2] = "Reply",
    [3] = "Error",
}

local mcode_method_valstr = {
    [0] = "getValue",
    [1] = "setValue",
    [2] = "setIncValue",
    [3] = "setDecValue",
    [4] = "setDefValue",
    [5] = "getObject",
}

local objgrp_valstr = {
    [0] = "root",
    [1] = "identity",
    [2] = "control",
    [3] = "status",
    [4] = "alarm",
    [5] = "file",
    [6] = "frame",
}

local objtype_valstr = {
    [0]  = "root",
    [1]  = "integer",
    [2]  = "ipaddr",
    [3]  = "float",
    [4]  = "enum",
    [5]  = "string",
    [6]  = "frame",
    [7]  = "alarm",
    [8]  = "file",
    [9]  = "long",
    [10] = "byte",
}

local transport_error_valstr = {
    [0] = "Undefined",
    [1] = "Internal bus communication error",
    [2] = "Internal bus timeout",
    [3] = "Transaction timeout",
    [4] = "Out of resources",
}

local object_error_valstr = {
    [16] = "Object group does not exist",
    [17] = "Object instance does not exist",
    [18] = "Object property does not exist",
    [19] = "No write access",
    [20] = "No read access",
    [21] = "No setDefault access",
    [22] = "Object type does not exist",
    [23] = "Illegal method",
    [24] = "Illegal method for this object type",
    [32] = "File error",
    [39] = "SPF file constraint violation",
    [40] = "SPF buffer full - retry fragment later",
}

local slot_status_valstr = {
    [0] = "no card",
    [1] = "powerup",
    [2] = "present",
    [3] = "error",
    [4] = "removed",
    [5] = "boot",
}

local pver_valstr = {
    [1] = "ACP1",
}

-------------------------------------------------------------------------------
-- Protocol declaration
-------------------------------------------------------------------------------

local acpv1 = Proto("acpv1_full", "Axon Control Protocol V1")

-------------------------------------------------------------------------------
-- ProtoFields
-------------------------------------------------------------------------------

-- TCP framing
local f_mlen = ProtoField.uint32("acpv1.mlen", "Message Length", base.DEC)

-- ACP1 header
local f_mtid   = ProtoField.uint32("acpv1.mtid",  "Transaction ID", base.HEX)
local f_pver   = ProtoField.uint8("acpv1.pver",   "Protocol Version", base.DEC, pver_valstr)
local f_mtype  = ProtoField.uint8("acpv1.mtype",  "Message Type", base.DEC, mtype_valstr)
local f_maddr  = ProtoField.uint8("acpv1.maddr",  "Slot Address", base.DEC)

-- MDATA common
local f_mcode  = ProtoField.uint8("acpv1.mcode",  "Method/Error Code", base.DEC)
local f_objgrp = ProtoField.uint8("acpv1.objgrp", "Object Group", base.DEC, objgrp_valstr)
local f_objid  = ProtoField.uint8("acpv1.objid",  "Object ID", base.DEC)
local f_value  = ProtoField.bytes("acpv1.value",   "Value")

-- Error fields
local f_err_transport = ProtoField.uint8("acpv1.err_transport", "Transport Error", base.DEC, transport_error_valstr)
local f_err_object    = ProtoField.uint8("acpv1.err_object",    "Object Error", base.DEC, object_error_valstr)

-- Object property fields (getObject decode)
local f_obj_type       = ProtoField.uint8("acpv1.obj.type",       "Object Type", base.DEC, objtype_valstr)
local f_obj_numprops   = ProtoField.uint8("acpv1.obj.numprops",   "Num Properties", base.DEC)
local f_obj_access     = ProtoField.uint8("acpv1.obj.access",     "Access", base.HEX)
local f_obj_access_r   = ProtoField.bool("acpv1.obj.access.read",      "Read",       8, nil, 0x01)
local f_obj_access_w   = ProtoField.bool("acpv1.obj.access.write",     "Write",      8, nil, 0x02)
local f_obj_access_d   = ProtoField.bool("acpv1.obj.access.setdef",    "SetDefault", 8, nil, 0x04)

-- Root
local f_root_bootmode   = ProtoField.uint8("acpv1.obj.root.bootmode",    "Boot Mode", base.DEC)
local f_root_numident   = ProtoField.uint8("acpv1.obj.root.numident",    "Num Identity", base.DEC)
local f_root_numcontrol = ProtoField.uint8("acpv1.obj.root.numcontrol",  "Num Control", base.DEC)
local f_root_numstatus  = ProtoField.uint8("acpv1.obj.root.numstatus",   "Num Status", base.DEC)
local f_root_numalarm   = ProtoField.uint8("acpv1.obj.root.numalarm",    "Num Alarm", base.DEC)
local f_root_numfile    = ProtoField.uint8("acpv1.obj.root.numfile",     "Num File", base.DEC)

-- Integer / Long / Byte / IPAddr / Float common numeric fields
local f_int_value_s16   = ProtoField.int16("acpv1.obj.int.value",       "Value", base.DEC)
local f_int_default_s16 = ProtoField.int16("acpv1.obj.int.default",     "Default Value", base.DEC)
local f_int_step_s16    = ProtoField.int16("acpv1.obj.int.step",        "Step Size", base.DEC)
local f_int_min_s16     = ProtoField.int16("acpv1.obj.int.min",         "Min Value", base.DEC)
local f_int_max_s16     = ProtoField.int16("acpv1.obj.int.max",         "Max Value", base.DEC)

local f_long_value_s32   = ProtoField.int32("acpv1.obj.long.value",     "Value", base.DEC)
local f_long_default_s32 = ProtoField.int32("acpv1.obj.long.default",   "Default Value", base.DEC)
local f_long_step_s32    = ProtoField.int32("acpv1.obj.long.step",      "Step Size", base.DEC)
local f_long_min_s32     = ProtoField.int32("acpv1.obj.long.min",       "Min Value", base.DEC)
local f_long_max_s32     = ProtoField.int32("acpv1.obj.long.max",       "Max Value", base.DEC)

local f_byte_value_u8   = ProtoField.uint8("acpv1.obj.byte.value",     "Value", base.DEC)
local f_byte_default_u8 = ProtoField.uint8("acpv1.obj.byte.default",   "Default Value", base.DEC)
local f_byte_step_u8    = ProtoField.uint8("acpv1.obj.byte.step",      "Step Size", base.DEC)
local f_byte_min_u8     = ProtoField.uint8("acpv1.obj.byte.min",       "Min Value", base.DEC)
local f_byte_max_u8     = ProtoField.uint8("acpv1.obj.byte.max",       "Max Value", base.DEC)

local f_ip_value_u32   = ProtoField.ipv4("acpv1.obj.ip.value",         "Value")
local f_ip_default_u32 = ProtoField.ipv4("acpv1.obj.ip.default",       "Default Value")
local f_ip_step_u32    = ProtoField.uint32("acpv1.obj.ip.step",        "Step Size", base.HEX)
local f_ip_min_u32     = ProtoField.ipv4("acpv1.obj.ip.min",           "Min Value")
local f_ip_max_u32     = ProtoField.ipv4("acpv1.obj.ip.max",           "Max Value")

local f_float_value    = ProtoField.float("acpv1.obj.float.value",     "Value")
local f_float_default  = ProtoField.float("acpv1.obj.float.default",   "Default Value")
local f_float_step     = ProtoField.float("acpv1.obj.float.step",      "Step Size")
local f_float_min      = ProtoField.float("acpv1.obj.float.min",       "Min Value")
local f_float_max      = ProtoField.float("acpv1.obj.float.max",       "Max Value")

-- Enum
local f_enum_value     = ProtoField.uint8("acpv1.obj.enum.value",      "Value (index)", base.DEC)
local f_enum_numitems  = ProtoField.uint8("acpv1.obj.enum.numitems",   "Num Items", base.DEC)
local f_enum_default   = ProtoField.uint8("acpv1.obj.enum.default",    "Default Value", base.DEC)
local f_enum_items     = ProtoField.string("acpv1.obj.enum.items",     "Item List")

-- String
local f_str_value      = ProtoField.string("acpv1.obj.str.value",      "Value")
local f_str_maxlen     = ProtoField.uint8("acpv1.obj.str.maxlen",      "Max Length", base.DEC)

-- Labels and units (shared)
local f_label          = ProtoField.string("acpv1.obj.label",          "Label")
local f_unit           = ProtoField.string("acpv1.obj.unit",           "Unit")

-- Frame status
local f_frame_numslots = ProtoField.uint8("acpv1.obj.frame.numslots",  "Num Slots", base.DEC)
local f_frame_slot     = ProtoField.uint8("acpv1.obj.frame.slot",      "Slot Status", base.DEC, slot_status_valstr)

-- Alarm
local f_alarm_priority = ProtoField.uint8("acpv1.obj.alarm.priority",  "Priority", base.DEC)
local f_alarm_tag      = ProtoField.uint8("acpv1.obj.alarm.tag",       "Tag", base.DEC)
local f_alarm_on_msg   = ProtoField.string("acpv1.obj.alarm.on_msg",   "Event On Message")
local f_alarm_off_msg  = ProtoField.string("acpv1.obj.alarm.off_msg",  "Event Off Message")

-- File
local f_file_numfrags  = ProtoField.int16("acpv1.obj.file.numfrags",   "Num Fragments", base.DEC)
local f_file_name      = ProtoField.string("acpv1.obj.file.name",      "File Name")

acpv1.fields = {
    f_mlen,
    f_mtid, f_pver, f_mtype, f_maddr,
    f_mcode, f_objgrp, f_objid, f_value,
    f_err_transport, f_err_object,
    f_obj_type, f_obj_numprops, f_obj_access, f_obj_access_r, f_obj_access_w, f_obj_access_d,
    f_root_bootmode, f_root_numident, f_root_numcontrol, f_root_numstatus, f_root_numalarm, f_root_numfile,
    f_int_value_s16, f_int_default_s16, f_int_step_s16, f_int_min_s16, f_int_max_s16,
    f_long_value_s32, f_long_default_s32, f_long_step_s32, f_long_min_s32, f_long_max_s32,
    f_byte_value_u8, f_byte_default_u8, f_byte_step_u8, f_byte_min_u8, f_byte_max_u8,
    f_ip_value_u32, f_ip_default_u32, f_ip_step_u32, f_ip_min_u32, f_ip_max_u32,
    f_float_value, f_float_default, f_float_step, f_float_min, f_float_max,
    f_enum_value, f_enum_numitems, f_enum_default, f_enum_items,
    f_str_value, f_str_maxlen,
    f_label, f_unit,
    f_frame_numslots, f_frame_slot,
    f_alarm_priority, f_alarm_tag, f_alarm_on_msg, f_alarm_off_msg,
    f_file_numfrags, f_file_name,
}

-------------------------------------------------------------------------------
-- Helper: read null-terminated string from tvb at offset
-- Returns (string, bytes_consumed) where bytes_consumed includes the NUL
-------------------------------------------------------------------------------
local function read_cstring(tvbuf, offset, maxlen)
    local remaining = tvbuf:reported_length_remaining(offset)
    if remaining <= 0 then return "", 0 end
    local limit = math.min(remaining, maxlen or remaining)
    for i = 0, limit - 1 do
        if tvbuf:range(offset + i, 1):uint() == 0 then
            local s = tvbuf:range(offset, i):string()
            return s, i + 1  -- +1 for the NUL
        end
    end
    -- no NUL found, take what we have
    local s = tvbuf:range(offset, limit):string()
    return s, limit
end

-------------------------------------------------------------------------------
-- Helper: format access byte as string
-------------------------------------------------------------------------------
local function access_string(access_val)
    local parts = {}
    if bit.band(access_val, 0x01) ~= 0 then parts[#parts+1] = "R" end
    if bit.band(access_val, 0x02) ~= 0 then parts[#parts+1] = "W" end
    if bit.band(access_val, 0x04) ~= 0 then parts[#parts+1] = "D" end
    if #parts == 0 then return "none" end
    return table.concat(parts, "+")
end

-------------------------------------------------------------------------------
-- Decode getObject reply value based on object type
-- tvbuf starts at MDATA[3] (the value bytes)
-- Returns label string or nil
-------------------------------------------------------------------------------
local function decode_getobject_value(tvbuf, tree, offset, datalen)
    if datalen < 3 then return nil end

    local obj_type = tvbuf:range(offset, 1):uint()
    local num_props = tvbuf:range(offset + 1, 1):uint()
    local access_val = tvbuf:range(offset + 2, 1):uint()

    local obj_tree = tree:add(tvbuf:range(offset, datalen),
        string.format("Object Properties [%s, %d props, access=%s]",
            objtype_valstr[obj_type] or "unknown", num_props, access_string(access_val)))

    obj_tree:add(f_obj_type, tvbuf:range(offset, 1))
    obj_tree:add(f_obj_numprops, tvbuf:range(offset + 1, 1))

    local access_item = obj_tree:add(f_obj_access, tvbuf:range(offset + 2, 1))
    access_item:add(f_obj_access_r, tvbuf:range(offset + 2, 1))
    access_item:add(f_obj_access_w, tvbuf:range(offset + 2, 1))
    access_item:add(f_obj_access_d, tvbuf:range(offset + 2, 1))

    local pos = offset + 3
    local label_str = nil

    -- ROOT (type=0): boot_mode, num_identity, num_control, num_status, num_alarm, num_file
    if obj_type == 0 then
        if datalen >= 9 then
            obj_tree:add(f_root_bootmode,   tvbuf:range(pos, 1)); pos = pos + 1
            obj_tree:add(f_root_numident,   tvbuf:range(pos, 1)); pos = pos + 1
            obj_tree:add(f_root_numcontrol, tvbuf:range(pos, 1)); pos = pos + 1
            obj_tree:add(f_root_numstatus,  tvbuf:range(pos, 1)); pos = pos + 1
            obj_tree:add(f_root_numalarm,   tvbuf:range(pos, 1)); pos = pos + 1
            obj_tree:add(f_root_numfile,    tvbuf:range(pos, 1)); pos = pos + 1
        end
        label_str = "root"

    -- INTEGER (type=1): int16 value, default, step, min, max, label, unit
    elseif obj_type == 1 then
        if datalen >= 13 then
            obj_tree:add(f_int_value_s16,   tvbuf:range(pos, 2)); pos = pos + 2
            obj_tree:add(f_int_default_s16, tvbuf:range(pos, 2)); pos = pos + 2
            obj_tree:add(f_int_step_s16,    tvbuf:range(pos, 2)); pos = pos + 2
            obj_tree:add(f_int_min_s16,     tvbuf:range(pos, 2)); pos = pos + 2
            obj_tree:add(f_int_max_s16,     tvbuf:range(pos, 2)); pos = pos + 2
            local s, n = read_cstring(tvbuf, pos, 17)
            obj_tree:add(f_label, tvbuf:range(pos, n), s); pos = pos + n
            label_str = s
            local u, m = read_cstring(tvbuf, pos, 5)
            if m > 0 then
                obj_tree:add(f_unit, tvbuf:range(pos, m), u); pos = pos + m
            end
        end

    -- IP ADDRESS (type=2): uint32 value, default, step, min, max, label, unit
    elseif obj_type == 2 then
        if datalen >= 23 then
            obj_tree:add(f_ip_value_u32,   tvbuf:range(pos, 4)); pos = pos + 4
            obj_tree:add(f_ip_default_u32, tvbuf:range(pos, 4)); pos = pos + 4
            obj_tree:add(f_ip_step_u32,    tvbuf:range(pos, 4)); pos = pos + 4
            obj_tree:add(f_ip_min_u32,     tvbuf:range(pos, 4)); pos = pos + 4
            obj_tree:add(f_ip_max_u32,     tvbuf:range(pos, 4)); pos = pos + 4
            local s, n = read_cstring(tvbuf, pos, 17)
            obj_tree:add(f_label, tvbuf:range(pos, n), s); pos = pos + n
            label_str = s
            local u, m = read_cstring(tvbuf, pos, 5)
            if m > 0 then
                obj_tree:add(f_unit, tvbuf:range(pos, m), u); pos = pos + m
            end
        end

    -- FLOAT (type=3): float32 value, default, step, min, max, label, unit
    elseif obj_type == 3 then
        if datalen >= 23 then
            obj_tree:add(f_float_value,   tvbuf:range(pos, 4)); pos = pos + 4
            obj_tree:add(f_float_default, tvbuf:range(pos, 4)); pos = pos + 4
            obj_tree:add(f_float_step,    tvbuf:range(pos, 4)); pos = pos + 4
            obj_tree:add(f_float_min,     tvbuf:range(pos, 4)); pos = pos + 4
            obj_tree:add(f_float_max,     tvbuf:range(pos, 4)); pos = pos + 4
            local s, n = read_cstring(tvbuf, pos, 17)
            obj_tree:add(f_label, tvbuf:range(pos, n), s); pos = pos + n
            label_str = s
            local u, m = read_cstring(tvbuf, pos, 5)
            if m > 0 then
                obj_tree:add(f_unit, tvbuf:range(pos, m), u); pos = pos + m
            end
        end

    -- ENUM (type=4): value, num_items, default, label, item_list
    elseif obj_type == 4 then
        if datalen >= 6 then
            obj_tree:add(f_enum_value,    tvbuf:range(pos, 1)); pos = pos + 1
            obj_tree:add(f_enum_numitems, tvbuf:range(pos, 1)); pos = pos + 1
            obj_tree:add(f_enum_default,  tvbuf:range(pos, 1)); pos = pos + 1
            local s, n = read_cstring(tvbuf, pos, 17)
            obj_tree:add(f_label, tvbuf:range(pos, n), s); pos = pos + n
            label_str = s
            local items, m = read_cstring(tvbuf, pos, 131)
            if m > 0 then
                obj_tree:add(f_enum_items, tvbuf:range(pos, m), items); pos = pos + m
            end
        end

    -- STRING (type=5): value (null-term string), max_len, label
    elseif obj_type == 5 then
        if datalen >= 4 then
            local s, n = read_cstring(tvbuf, pos, 131)
            obj_tree:add(f_str_value, tvbuf:range(pos, n), s); pos = pos + n
            if pos < offset + datalen then
                obj_tree:add(f_str_maxlen, tvbuf:range(pos, 1)); pos = pos + 1
            end
            local lbl, m = read_cstring(tvbuf, pos, 17)
            if m > 0 then
                obj_tree:add(f_label, tvbuf:range(pos, m), lbl); pos = pos + m
                label_str = lbl
            end
        end

    -- FRAME STATUS (type=6): num_slots + slot_status_array
    elseif obj_type == 6 then
        if datalen >= 4 then
            local num_slots = tvbuf:range(pos, 1):uint()
            obj_tree:add(f_frame_numslots, tvbuf:range(pos, 1)); pos = pos + 1
            for i = 0, num_slots - 1 do
                if pos < offset + datalen then
                    local status_val = tvbuf:range(pos, 1):uint()
                    local status_name = slot_status_valstr[status_val] or "unknown"
                    obj_tree:add(f_frame_slot, tvbuf:range(pos, 1)):set_text(
                        string.format("Slot %d: %s (%d)", i, status_name, status_val))
                    pos = pos + 1
                end
            end
        end
        label_str = "frame"

    -- ALARM (type=7): priority, tag, label, event_on_msg, event_off_msg
    elseif obj_type == 7 then
        if datalen >= 5 then
            obj_tree:add(f_alarm_priority, tvbuf:range(pos, 1)); pos = pos + 1
            obj_tree:add(f_alarm_tag,      tvbuf:range(pos, 1)); pos = pos + 1
            local s, n = read_cstring(tvbuf, pos, 17)
            obj_tree:add(f_label, tvbuf:range(pos, n), s); pos = pos + n
            label_str = s
            local on_msg, m1 = read_cstring(tvbuf, pos, 33)
            if m1 > 0 then
                obj_tree:add(f_alarm_on_msg, tvbuf:range(pos, m1), on_msg); pos = pos + m1
            end
            local off_msg, m2 = read_cstring(tvbuf, pos, 33)
            if m2 > 0 then
                obj_tree:add(f_alarm_off_msg, tvbuf:range(pos, m2), off_msg); pos = pos + m2
            end
        end

    -- FILE (type=8): num_fragments (int16), file_name (string)
    elseif obj_type == 8 then
        if datalen >= 5 then
            obj_tree:add(f_file_numfrags, tvbuf:range(pos, 2)); pos = pos + 2
            local s, n = read_cstring(tvbuf, pos, 17)
            obj_tree:add(f_file_name, tvbuf:range(pos, n), s); pos = pos + n
            label_str = s
        end

    -- LONG (type=9): int32 value, default, step, min, max, label, unit
    elseif obj_type == 9 then
        if datalen >= 23 then
            obj_tree:add(f_long_value_s32,   tvbuf:range(pos, 4)); pos = pos + 4
            obj_tree:add(f_long_default_s32, tvbuf:range(pos, 4)); pos = pos + 4
            obj_tree:add(f_long_step_s32,    tvbuf:range(pos, 4)); pos = pos + 4
            obj_tree:add(f_long_min_s32,     tvbuf:range(pos, 4)); pos = pos + 4
            obj_tree:add(f_long_max_s32,     tvbuf:range(pos, 4)); pos = pos + 4
            local s, n = read_cstring(tvbuf, pos, 17)
            obj_tree:add(f_label, tvbuf:range(pos, n), s); pos = pos + n
            label_str = s
            local u, m = read_cstring(tvbuf, pos, 5)
            if m > 0 then
                obj_tree:add(f_unit, tvbuf:range(pos, m), u); pos = pos + m
            end
        end

    -- BYTE (type=10): uint8 value, default, step, min, max, label, unit
    elseif obj_type == 10 then
        if datalen >= 8 then
            obj_tree:add(f_byte_value_u8,   tvbuf:range(pos, 1)); pos = pos + 1
            obj_tree:add(f_byte_default_u8, tvbuf:range(pos, 1)); pos = pos + 1
            obj_tree:add(f_byte_step_u8,    tvbuf:range(pos, 1)); pos = pos + 1
            obj_tree:add(f_byte_min_u8,     tvbuf:range(pos, 1)); pos = pos + 1
            obj_tree:add(f_byte_max_u8,     tvbuf:range(pos, 1)); pos = pos + 1
            local s, n = read_cstring(tvbuf, pos, 17)
            obj_tree:add(f_label, tvbuf:range(pos, n), s); pos = pos + n
            label_str = s
            local u, m = read_cstring(tvbuf, pos, 5)
            if m > 0 then
                obj_tree:add(f_unit, tvbuf:range(pos, m), u); pos = pos + m
            end
        end
    end

    return label_str
end

-------------------------------------------------------------------------------
-- acp1_value_preview returns a compact decoded-value string for the
-- Info column when we're looking at a getValue reply / setValue request
-- without type context. The ACP1 protocol doesn't carry a type byte
-- with the value (unlike ACP2's vtype in the property header), so the
-- type is derived from the OBJECT definition — which requires prior
-- getObject context we don't have here. We do a length-based guess:
--   1 byte  -> u8  (byte/enum)
--   2 bytes -> s16 (integer)
--   4 bytes -> s32 (long) + hex (covers u32/float/ipaddr ambiguity)
--   varies  -> string (if printable) or raw byte count
-- The "/0x..." hex on 4-byte values lets the reader recognise float or
-- ipaddr patterns without the dissector having to pick a type.
-------------------------------------------------------------------------------
local function acp1_value_preview(tvbuf, offset, datalen)
    if datalen <= 0 then return "" end
    if datalen == 1 then
        return "value(u8)=" .. tvbuf:range(offset, 1):uint()
    elseif datalen == 2 then
        return "value(s16)=" .. tvbuf:range(offset, 2):int()
    elseif datalen == 4 then
        local u = tvbuf:range(offset, 4):uint()
        local s = tvbuf:range(offset, 4):int()
        return string.format("value(s32/u32)=%d/0x%X", s, u)
    elseif datalen <= 32 then
        local s = tvbuf:range(offset, datalen):string()
        local printable = s:match("^[%w%s%._%-/:%+%(%)]*$") and #s > 0
        if printable then
            return "value(string)=\"" .. s:gsub("\0", "") .. "\""
        end
        return "value=" .. datalen .. "B"
    end
    return "value=" .. datalen .. "B"
end

-------------------------------------------------------------------------------
-- Core ACP1 dissection (shared by UDP and TCP paths)
-- tvbuf: starts at MTID (no MLEN prefix)
-- Returns bytes consumed
-------------------------------------------------------------------------------
local function dissect_acpv1_message(tvbuf, pktinfo, root)
    local pktlen = tvbuf:reported_length_remaining()
    if pktlen < ACP1_HDR_LEN then return 0 end

    local tree = root:add(acpv1, tvbuf:range(0, pktlen))

    pktinfo.cols.protocol:set("ACP1")

    -- Header fields
    local mtid_val  = tvbuf:range(0, 4):uint()
    local pver_val  = tvbuf:range(4, 1):uint()
    local mtype_val = tvbuf:range(5, 1):uint()
    local maddr_val = tvbuf:range(6, 1):uint()

    tree:add(f_mtid,  tvbuf:range(0, 4))
    tree:add(f_pver,  tvbuf:range(4, 1))
    tree:add(f_mtype, tvbuf:range(5, 1))
    tree:add(f_maddr, tvbuf:range(6, 1))

    -- Short type names: Ann/Req/Rep/Err (concise, matches ACP2 dissector style)
    local mtype_short = { [0] = "Ann", [1] = "Req", [2] = "Rep", [3] = "Err" }
    local mtype_str = mtype_short[mtype_val] or ("T" .. mtype_val)
    local is_announce = (mtid_val == 0)

    -- MDATA starts at offset 7
    local mdata_offset = 7
    local mdata_len = pktlen - mdata_offset

    -- Build info incrementally so every packet gets the common prefix:
    -- "ACP1 <type> slot=N [mtid=...]"
    local info_parts = { "ACP1", mtype_str, "slot=" .. maddr_val }
    if not is_announce then
        table.insert(info_parts, string.format("mtid=0x%X", mtid_val))
    end

    if mdata_len <= 0 then
        table.insert(info_parts, "(no mdata)")
        pktinfo.cols.info:set(table.concat(info_parts, " "))
        return pktlen
    end

    local mcode_val = tvbuf:range(mdata_offset, 1):uint()

    if mtype_val == 3 then
        -- Error reply: MCODE is the error code
        local err_str
        if mcode_val < 16 then
            tree:add(f_err_transport, tvbuf:range(mdata_offset, 1))
            err_str = transport_error_valstr[mcode_val] or "Unknown transport error"
        else
            tree:add(f_err_object, tvbuf:range(mdata_offset, 1))
            err_str = object_error_valstr[mcode_val] or "Unknown object error"
        end
        table.insert(info_parts, string.format("%s(code=%d)", err_str, mcode_val))

        -- Error may have ObjGrp/ObjId after MCODE
        if mdata_len >= 3 then
            local grp = tvbuf:range(mdata_offset + 1, 1):uint()
            local oid = tvbuf:range(mdata_offset + 2, 1):uint()
            tree:add(f_objgrp, tvbuf:range(mdata_offset + 1, 1))
            tree:add(f_objid,  tvbuf:range(mdata_offset + 2, 1))
            local grp_str = objgrp_valstr[grp] or tostring(grp)
            table.insert(info_parts, string.format("%s[%d]", grp_str, oid))
        end
        if mdata_len > 3 then
            tree:add(f_value, tvbuf:range(mdata_offset + 3, mdata_len - 3))
        end
    else
        -- Non-error: MCODE is method ID
        tree:add(f_mcode, tvbuf:range(mdata_offset, 1))
        local method_str = mcode_method_valstr[mcode_val] or string.format("method(%d)", mcode_val)
        table.insert(info_parts, method_str)

        if mdata_len >= 3 then
            local grp = tvbuf:range(mdata_offset + 1, 1):uint()
            local oid = tvbuf:range(mdata_offset + 2, 1):uint()
            tree:add(f_objgrp, tvbuf:range(mdata_offset + 1, 1))
            tree:add(f_objid,  tvbuf:range(mdata_offset + 2, 1))

            local grp_str = objgrp_valstr[grp] or tostring(grp)
            table.insert(info_parts, string.format("%s[%d]", grp_str, oid))

            local value_len = mdata_len - 3
            local label_str = nil
            if value_len > 0 then
                if mcode_val == 5 and (mtype_val == 2 or mtype_val == 0) then
                    -- getObject reply or announce: full property decode.
                    -- The first byte of the object payload is obj_type
                    -- (spec Property Layouts table) — expose it in the
                    -- Info column so you can see "type=integer" /
                    -- "type=enum" / "type=string" at a glance without
                    -- expanding the tree. decode_getobject_value reads
                    -- the full property list and returns the label.
                    local obj_type = tvbuf:range(mdata_offset + 3, 1):uint()
                    local type_name = objtype_valstr[obj_type] or ("type=" .. obj_type)
                    table.insert(info_parts, "type=" .. type_name)
                    label_str = decode_getobject_value(tvbuf, tree, mdata_offset + 3, value_len)
                else
                    -- setValue/getValue/setInc/setDec/setDef: we don't
                    -- know the object type without state (prior
                    -- getObject reply). Length-based heuristic in
                    -- acp1_value_preview tags with best-guess type
                    -- ("value(u8)=", "value(s16)=", "value(s32/u32)=").
                    tree:add(f_value, tvbuf:range(mdata_offset + 3, value_len))
                    local preview = acp1_value_preview(tvbuf, mdata_offset + 3, value_len)
                    if preview ~= "" then
                        table.insert(info_parts, preview)
                    end
                end
            end

            if label_str and label_str ~= "" then
                table.insert(info_parts, string.format("\"%s\"", label_str))
            end
        end
    end

    pktinfo.cols.info:set(table.concat(info_parts, " "))
    return pktlen
end


-------------------------------------------------------------------------------
-- UDP dissector (Mode A) — each datagram is one ACP1 message
-------------------------------------------------------------------------------
function acpv1.dissector(tvbuf, pktinfo, root)
    return dissect_acpv1_message(tvbuf, pktinfo, root)
end

-------------------------------------------------------------------------------
-- TCP dissector (Mode B) — MLEN(u32 BE) prefix + TCP reassembly
-------------------------------------------------------------------------------
local acpv1_tcp = Proto("acpv1_tcp", "Axon Control Protocol V1 (TCP)")
acpv1_tcp.fields = {} -- shares fields with main proto via tree:add(acpv1, ...)

local function dissect_tcp_pdu(tvbuf, pktinfo, root)
    -- tvbuf starts at MLEN
    local pktlen = tvbuf:reported_length_remaining()
    if pktlen < 4 then return 0 end

    local mlen = tvbuf:range(0, 4):uint()

    -- Add MLEN to the tree under the main protocol
    local tree = root:add(acpv1, tvbuf:range(0, 4 + mlen))
    tree:add(f_mlen, tvbuf:range(0, 4)):append_text(
        string.format(" (payload = %d bytes)", mlen))

    -- Dissect the ACP1 message after the 4-byte MLEN prefix
    if mlen >= ACP1_HDR_LEN then
        local msg_tvb = tvbuf:range(4, mlen):tvb()
        dissect_acpv1_message(msg_tvb, pktinfo, root)
    end

    return 4 + mlen
end

local function get_tcp_pdu_len(tvbuf, pktinfo, offset)
    -- We need at least 4 bytes for MLEN
    if tvbuf:reported_length_remaining(offset) < 4 then
        return -DESEGMENT_ONE_MORE_SEGMENT
    end
    local mlen = tvbuf:range(offset, 4):uint()
    return 4 + mlen  -- MLEN prefix + payload
end

function acpv1_tcp.dissector(tvbuf, pktinfo, root)
    dissect_tcp_pdu_streaming(tvbuf, pktinfo, root, 4, get_tcp_pdu_len, dissect_tcp_pdu)
end

-------------------------------------------------------------------------------
-- Heuristic check: is this actually ACP1?
-- Validates PVER=1 and MTYPE in 0..3
-------------------------------------------------------------------------------
local function heuristic_check_acpv1(tvbuf, pktinfo, root)
    local pktlen = tvbuf:reported_length_remaining()
    if pktlen < ACP1_HDR_LEN then return false end

    local pver = tvbuf:range(4, 1):uint()
    if pver ~= 1 then return false end

    local mtype = tvbuf:range(5, 1):uint()
    if mtype > 3 then return false end

    local maddr = tvbuf:range(6, 1):uint()
    if maddr > 31 then return false end

    return true
end

-------------------------------------------------------------------------------
-- UDP heuristic dissector
-------------------------------------------------------------------------------
local function heuristic_dissect_udp(tvbuf, pktinfo, root)
    if not heuristic_check_acpv1(tvbuf, pktinfo, root) then
        return false
    end
    dissect_acpv1_message(tvbuf, pktinfo, root)
    pktinfo.conversation = acpv1
    return true
end

-------------------------------------------------------------------------------
-- TCP heuristic dissector
-------------------------------------------------------------------------------
local function heuristic_dissect_tcp(tvbuf, pktinfo, root)
    local pktlen = tvbuf:reported_length_remaining()
    if pktlen < 4 + ACP1_HDR_LEN then return false end

    -- Check MLEN is reasonable
    local mlen = tvbuf:range(0, 4):uint()
    if mlen < ACP1_HDR_LEN or mlen > 141 then return false end

    -- Check ACP1 header after MLEN prefix
    local pver = tvbuf:range(4 + 4, 1):uint()
    if pver ~= 1 then return false end

    local mtype = tvbuf:range(4 + 5, 1):uint()
    if mtype > 3 then return false end

    acpv1_tcp.dissector(tvbuf, pktinfo, root)
    pktinfo.conversation = acpv1_tcp
    return true
end

-------------------------------------------------------------------------------
-- Registration
-------------------------------------------------------------------------------

-- Fixed port registration
local udp_table = DissectorTable.get("udp.port")
udp_table:add(ACP1_UDP_PORT, acpv1)

local tcp_table = DissectorTable.get("tcp.port")
tcp_table:add(ACP1_TCP_PORT, acpv1_tcp)

-- Heuristic registration (fallback when port is not 2071)
acpv1:register_heuristic("udp", heuristic_dissect_udp)
acpv1_tcp:register_heuristic("tcp", heuristic_dissect_tcp)
