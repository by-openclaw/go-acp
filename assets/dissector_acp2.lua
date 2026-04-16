-------------------------------------------------------------------------------
--
-- Dissector for Axon Control Protocol 2
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
local acp2_proto = Proto("acp2", "ACP2")

----------------------------------------
local types_valstr = {
    [0] = "REQUEST",
    [1] = "REPLY",
    [2] = "ANNOUNCE",
    [3] = "ERROR",
}

local funcs_valstr = {
    [0] = "get_version",
    [1] = "get_object",
    [2] = "get_property",
    [3] = "set_property",
}

local pids_valstr = {
     [1] = "object type",
     [2] = "label",
     [3] = "access",
     [4] = "announce delay",
     [5] = "number type",
     [6] = "string max length",
     [7] = "preset depth",
     [8] = "value",
     [9] = "default value",
    [10] = "min value",
    [11] = "max value",
    [12] = "step size",
    [13] = "unit",
    [14] = "children",
    [15] = "options",
    [16] = "event tag",
    [17] = "event prio",
    [18] = "event state",
    [19] = "event messages",
    [20] = "preset parent",
}

local access_valstr = {
    [1] = "read-only",
    [2] = "write-only",
    [3] = "read-write",
}

local objtype_valstr = {
    [0] = "node",
    [1] = "preset",
    [2] = "enum",
    [3] = "number",
    [4] = "ipv4",
    [5] = "string",
}

local errors_valstr = {
    [0] = "protocol error",
    [1] = "invalid object id",
    [2] = "invalid index",
    [3] = "invalid property id",
    [4] = "no access",
    [5] = "invalid value",
}

local vtype_valstr = {
    [0] = "S8",
    [1] = "S16",
    [2] = "S32",
    [3] = "S64",
    [4] = "U8",
    [5] = "U16",
    [6] = "U32",
    [7] = "U74",
    [8] = "float",
    [9] = "preset/enum",
    [10] = "ipv4",
    [11] = "string",
}

----------------------------------------

local field_prefix = "acp2.menu"
local prop_msg = Proto("acp2.menu.property", "ACP2 Property")
local prop_fields =
{
    pid = ProtoField.uint8(field_prefix .. ".property.pid", "Property ID", base.DEC, pids_valstr),
    obj_type = ProtoField.uint8(field_prefix .. ".property.obj_type", "Object_Type", base.DEC, objtype_valstr),
    access = ProtoField.uint8(field_prefix .. ".property.access", "Access", base.HEX, access_valstr),
    delay = ProtoField.uint32(field_prefix .. ".property.delay", "AnnounceDelay", base.DEC),
    vtype = ProtoField.uint8(field_prefix .. ".property.vtype", "ValueType", base.DEC, vtype_valstr),
    num_opts = ProtoField.uint8(field_prefix .. ".property.num_opts", "NumOptions", base.DEC),
    plen = ProtoField.uint16(field_prefix .. ".property.plen", "Length", base.DEC),
    -- TODO print as ipnr, how?
    value_ipnr = ProtoField.uint32(field_prefix .. ".property.value_ipnr", "value", base.HEX),
    value_s32 = ProtoField.int32(field_prefix .. ".property.value_s32", "value", base.DEC),
    value_u32 = ProtoField.uint32(field_prefix .. ".property.value_u32", "value", base.DEC),
}
prop_msg.fields = prop_fields

-- a table of all of our Protocol's fields
local hdr_fields =
{
    req_type = ProtoField.uint8("acp2.type", "Type", base.DEC, types_valstr),
    mtid = ProtoField.uint8("acp2.mtid", "MTID", base.DEC),
    func = ProtoField.uint8("acp2.func", "Function", base.DEC, funcs_valstr),
    errmsg = ProtoField.uint8("acp2.errmsg", "ErrorMsg", base.DEC, errors_valstr),
    pad = ProtoField.uint8("acp2.pad", "Padding", base.HEX),
    version = ProtoField.uint8("acp2.version", "Version", base.DEC),
    pid = ProtoField.uint8("acp2.pid", "Property", base.DEC, pids_valstr),
    obj_id = ProtoField.uint32("acp2.obj_id", "Object", base.DEC),
    index = ProtoField.uint32("acp2.index", "Index", base.DEC),
}


local function get_padding4(a)
    local times = math.floor(a/4)
    local rest = a - (4*times)

    local padding = 0
    if rest ~= 0 then padding = 4 - rest end
    return padding
end


-- register the ProtoFields
acp2_proto.fields = hdr_fields

-- this holds the plain "data" Dissector, used by default
local dissector_data = Dissector.get("data")

-- the dissector table holds all protocols
local dissectors = DissectorTable.new("acp2.mtype", "ACPv2 Message Type", ftypes.UINT8, base.HEX)

local function parse_value_property(tvbuf, tree, offset, vtype)
    if vtype >= 0 and vtype <= 2 then  -- 32bit signed
        tree:add(prop_fields.value_s32, tvbuf:range(offset, 4))
    elseif vtype == 3 then -- 64bit signed
        -- TODO
    elseif vtype >=4 and vtype <= 6 then -- 32bit unsigned
        tree:add(prop_fields.value_u32, tvbuf:range(offset, 4))
    elseif vtype == 7 then -- 64bit unsigned
        -- TODO
    elseif vtype == 8 then -- float
        -- TODO
    elseif vtype == 9 then -- enum/preset
        tree:add(prop_fields.value_u32, tvbuf:range(offset, 4))
    elseif vtype == 10 then -- ipnr
        tree:add(prop_fields.value_ipnr, tvbuf:range(offset, 4))
    elseif vtype == 11 then -- string
    end
end

local function parse_single_property(tvbuf, pktinfo, root, offset)
    local pktlen = tvbuf:reported_length_remaining()
    if pktlen < 4 then
        -- too short
        return
    end
    local start_offset = offset

    local pid_tvb = tvbuf:range(offset, 1)
    local pid = pid_tvb:uint()
    local data1_tvb = tvbuf:range(offset+1, 1)
    local data1 = data1_tvb:uint()
    local plen_tvb = tvbuf:range(offset+2, 2)
    local plen = plen_tvb:uint()
    --dprint("property " .. pid .. " offset=" .. offset .. " plen=" .. plen)
    offset = offset + 4

    local total_plen = plen + get_padding4(plen)

    local tree = root:add(prop_msg, tvbuf:range(start_offset, total_plen))
    tree:set_text("Property " .. pids_valstr[pid])

    tree:add(prop_fields.pid, pid_tvb)
    tree:add(prop_fields.plen, plen_tvb)
    if pid == 1 then -- obj_type
        tree:add(prop_fields.obj_type, data1_tvb)
    elseif pid == 2 then -- label
    elseif pid == 3 then -- access
        tree:add(prop_fields.access, data1_tvb)
    elseif pid == 4 then
        tree:add(prop_fields.delay, tvbuf:range(offset, 4))
        offset = offset + 4
    elseif pid >= 8 and pid <= 12 then
        tree:add(prop_fields.vtype, data1_tvb)
        parse_value_property(tvbuf, tree, offset, data1)
        offset = plen
    elseif pid == 15 then
        tree:add(prop_fields.num_opts, data1_tvb)
    end

    if total_plen > offset then
        local value_tvb = tvbuf(offset, total_plen - offset):tvb()
        dissector_data:call(value_tvb, pktinfo, tree)
    end
    return total_plen
end

local function parse_properties(tvbuf, pktinfo, tree, offset)
    local pktlen = tvbuf:reported_length_remaining()
    local new_offset = offset
    while pktlen > new_offset do
        new_offset = new_offset + parse_single_property(tvbuf, pktinfo, tree, new_offset)
    end
    return new_offset
end

function acp2_proto.dissector(tvbuf, pktinfo, root)
    dprint2("acp2_proto.dissector called")

    local pktlen = tvbuf:reported_length_remaining()
    local tree = root:add(acp2_proto, tvbuf:range(0, pktlen))

    -- now let's check it's not too short
    if pktlen < 4 then
        dprint("packet length", pktlen, "too short")
        return
    end

    local offset = 0

    local type_tvbr = tvbuf:range(offset, 1)
    tree:add(hdr_fields.req_type, type_tvbr)
    local type_val = type_tvbr:uint()
    offset = offset + 1

    tree:add(hdr_fields.mtid, tvbuf:range(offset, 1))
    offset = offset + 1

    local byte3 = tvbuf:range(offset, 1)
    offset = offset + 1
    local byte4 = tvbuf:range(offset, 1)
    offset = offset + 1

    -- TODO check sizes

    local msg = types_valstr[type_val]
    if msg == nil then msg = "UNKNOWN" end

    if type_val == 0 or type_val == 1 then -- request or reply
        tree:add(hdr_fields.func, byte3)
        local acp2_func = byte3:uint()
        msg = msg .. " " .. funcs_valstr[acp2_func]
        if acp2_func == 0 then -- get_version
            tree:add(hdr_fields.version, byte4)
        elseif acp2_func == 1 then -- get_object
            tree:add(hdr_fields.pad, byte4)
            local objid_tvb = tvbuf:range(4, 4)
            local objid_val = objid_tvb:uint()
            tree:add(hdr_fields.obj_id, objid_tvb)
            tree:add(hdr_fields.index, tvbuf:range(8, 4))
            msg = msg .. "(" .. objid_val .. ")"
            offset = offset + 8
            if type_val == 1 then -- reply
                offset = parse_properties(tvbuf, pktinfo, tree, offset)
            end
        elseif acp2_func == 2 then -- get_property
            tree:add(hdr_fields.pid, byte4)
            local objid_tvb = tvbuf:range(4, 4)
            local objid_val = objid_tvb:uint()
            tree:add(hdr_fields.obj_id, objid_tvb)
            tree:add(hdr_fields.index, tvbuf:range(8, 4))
            msg = msg .. "(" .. objid_val .. ")"
            offset = offset + 8
            if type_val == 1 then -- reply
                offset = parse_properties(tvbuf, pktinfo, tree, offset)
            end
        elseif acp2_func == 3 then -- set_property
            tree:add(hdr_fields.pid, byte4)
            local objid_tvb = tvbuf:range(4, 4)
            local objid_val = objid_tvb:uint()
            tree:add(hdr_fields.obj_id, objid_tvb)
            tree:add(hdr_fields.index, tvbuf:range(8, 4))
            msg = msg .. "(" .. objid_val .. ")"
            offset = offset + 8
            offset = parse_properties(tvbuf, pktinfo, tree, offset)
        else -- unknown
            -- TODO
        end
    elseif type_val == 2 then -- announcement
        tree:add(hdr_fields.pad, byte3)
        tree:add(hdr_fields.pid, byte4)
        local objid_tvb = tvbuf:range(4, 4)
        local objid_val = objid_tvb:uint()
        tree:add(hdr_fields.obj_id, objid_tvb)
        tree:add(hdr_fields.index, tvbuf:range(8, 4))
        offset = offset + 8
        offset = parse_single_property(tvbuf, pktinfo, tree, offset)
        local pid = byte4:uint()
        msg = msg .. " " .. pids_valstr[pid] .. "(" .. objid_val .. ")"
    elseif type_val == 3 then -- error
        tree:add(hdr_fields.errmsg, byte3)
        tree:add(hdr_fields.pad, byte4)
        local errmsg = errors_valstr[byte3:uint()]
        if errmsg == nil then errmsg = "UNKNOWN" end
        msg = msg .. " " .. errmsg
    end

    pktinfo.cols.info:append(msg)

    -- should be nothing remaining
    --dissector_data:call(tvbuf(offset):tvb(), pktinfo, tree)

    return pktlen
end

DissectorTable.get("axonnet2.proto"):add(2, acp2_proto)

