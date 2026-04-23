-------------------------------------------------------------------------------
--
-- Wireshark Lua Dissector: ACP2 over AN2/TCP (port 2072)
--
-- Standalone dissector handling:
--   - AN2 transport framing with TCP reassembly
--   - AN2 internal protocol (proto=0)
--   - ACP2 protocol messages (proto=2, AN2 type=4 data frames)
--   - ACP2 property TLV parsing with 4-byte alignment
--
-- Compatible with Wireshark 4.x
--
-------------------------------------------------------------------------------

-- AN2 frame header size
local AN2_HDR_LEN = 8
local AN2_MAGIC   = 0xC635

-- Side-channel from the ACP2 dissector back to its AN2 caller.
-- Wireshark's Proto:dissector wrapper drops multi-return values, so we
-- can't rely on `local consumed, info = acp2_proto.dissector(...)`.
-- Module-local string set by acp2_proto.dissector on every invocation,
-- read by the AN2 dissector right after the nested call.
local acp2_last_info = ""

-- Slot lives in the AN2 frame header, not the ACP2 payload. AN2 writes
-- it here before invoking acp2_proto.dissector so the ACP2 side can
-- build dotted paths like "0.5.value" (slot.obj_id.pid) per issue #58
-- — matches the Ember+ OID-dotted Info column style.
local acp2_current_slot = 0

-- Per-conversation label cache for Info-column resolution (issue #58).
-- Keyed by "conv_id|slot|obj_id" → label string. Populated whenever a
-- pid=2 (label) property value flows by as a reply or announce payload;
-- read on every subsequent frame that references the same (slot, obj_id)
-- so the Info column can show `0.5.value "Input A"` rather than `0.5`.
-- Keyed by a composite string to avoid Lua table-key weirdness with
-- nested tables. Scoped per-conversation so two concurrent Axon captures
-- do not cross-pollute.
local acp2_label_cache = {}

local function conv_key(pktinfo)
    -- Wireshark's pinfo.conversation_id is not universally exposed in
    -- Lua 5.2. Fall back to src+dst+ports which are stable per TCP
    -- conversation within a capture.
    return tostring(pktinfo.src) .. ":" .. tostring(pktinfo.src_port) ..
        "->" .. tostring(pktinfo.dst) .. ":" .. tostring(pktinfo.dst_port)
end

local function label_key(pktinfo, slot, obj_id)
    return conv_key(pktinfo) .. "|" .. slot .. "|" .. obj_id
end

local function cache_label(pktinfo, slot, obj_id, label)
    if label == nil or label == "" then return end
    acp2_label_cache[label_key(pktinfo, slot, obj_id)] = label
end

local function lookup_label(pktinfo, slot, obj_id)
    return acp2_label_cache[label_key(pktinfo, slot, obj_id)]
end

-- Format the dotted slot.obj_id path for the Info column, appending a
-- quoted label if one was cached by an earlier frame in this conversation.
local function path_with_label(pktinfo, slot, obj_id)
    local path = string.format("%d.%d", slot, obj_id)
    local lbl = lookup_label(pktinfo, slot, obj_id)
    if lbl ~= nil and lbl ~= "" then
        path = path .. " \"" .. lbl .. "\""
    end
    return path
end

-------------------------------------------------------------------------------
-- Value-string tables
-------------------------------------------------------------------------------

local an2_proto_valstr = {
    [0] = "AN2",
    [1] = "ACP1",
    [2] = "ACP2",
    [3] = "ACMP",
}

local an2_type_valstr = {
    [0] = "Request",
    [1] = "Reply",
    [2] = "Event",
    [3] = "Error",
    [4] = "Data",
}

local an2_func_valstr = {
    [0] = "GetVersion",
    [1] = "GetDeviceInfo",
    [2] = "GetSlotInfo",
    [3] = "EnableProtocolEvents",
}

local acp2_type_valstr = {
    [0] = "Request",
    [1] = "Reply",
    [2] = "Announce",
    [3] = "Error",
}

local acp2_func_valstr = {
    [0] = "GetVersion",
    [1] = "GetObject",
    [2] = "GetProperty",
    [3] = "SetProperty",
}

local acp2_error_valstr = {
    [0] = "Protocol error",
    [1] = "Invalid obj-id",
    [2] = "Invalid idx",
    [3] = "Invalid pid",
    [4] = "No access",
    [5] = "Invalid value",
}

local acp2_pid_valstr = {
    [1]  = "object_type",
    [2]  = "label",
    [3]  = "access",
    [4]  = "announce_delay",
    [5]  = "number_type",
    [6]  = "string_max_length",
    [7]  = "preset_depth",
    [8]  = "value",
    [9]  = "default_value",
    [10] = "min_value",
    [11] = "max_value",
    [12] = "step_size",
    [13] = "unit",
    [14] = "children",
    [15] = "options",
    [16] = "event_tag",
    [17] = "event_prio",
    [18] = "event_state",
    [19] = "event_messages",
    [20] = "preset_parent",
}

local acp2_objtype_valstr = {
    [0] = "node",
    [1] = "preset",
    [2] = "enum",
    [3] = "number",
    [4] = "ipv4",
    [5] = "string",
}

local acp2_access_valstr = {
    [1] = "read-only",
    [2] = "write-only",
    [3] = "read-write",
}

local acp2_numtype_valstr = {
    [0]  = "s8",
    [1]  = "s16",
    [2]  = "s32",
    [3]  = "s64",
    [4]  = "u8",
    [5]  = "u16",
    [6]  = "u32",
    [7]  = "u64",
    [8]  = "float",
    [9]  = "preset/enum",
    [10] = "ipv4",
    [11] = "string",
}

-------------------------------------------------------------------------------
-- Protocol declarations
-------------------------------------------------------------------------------

local an2_proto  = Proto("an2_acp2", "AN2 Transport (ACP2)")
local acp2_proto = Proto("acp2_msg", "ACP2 Protocol")
local acp2_prop_proto = Proto("acp2_prop", "ACP2 Property")

-------------------------------------------------------------------------------
-- AN2 ProtoFields
-------------------------------------------------------------------------------

local an2_f = {
    magic = ProtoField.uint16("an2.magic",  "Magic",    base.HEX),
    proto = ProtoField.uint8 ("an2.proto",  "Protocol", base.DEC, an2_proto_valstr),
    slot  = ProtoField.uint8 ("an2.slot",   "Slot",     base.DEC),
    mtid  = ProtoField.uint8 ("an2.mtid",   "MTID",     base.DEC),
    type  = ProtoField.uint8 ("an2.type",   "Type",     base.DEC, an2_type_valstr),
    dlen  = ProtoField.uint16("an2.dlen",   "Data Length", base.DEC),
    -- AN2 internal fields
    func     = ProtoField.uint8 ("an2.func",     "Function",  base.DEC, an2_func_valstr),
    version  = ProtoField.uint8 ("an2.version",  "Version",   base.DEC),
    payload  = ProtoField.bytes ("an2.payload",  "Payload"),
}
an2_proto.fields = an2_f

-------------------------------------------------------------------------------
-- ACP2 ProtoFields
-------------------------------------------------------------------------------

local acp2_f = {
    type    = ProtoField.uint8 ("acp2.type",   "Type",     base.DEC, acp2_type_valstr),
    mtid    = ProtoField.uint8 ("acp2.mtid",   "MTID",     base.DEC),
    func    = ProtoField.uint8 ("acp2.func",   "Function", base.DEC, acp2_func_valstr),
    stat    = ProtoField.uint8 ("acp2.stat",   "Status",   base.DEC, acp2_error_valstr),
    pid     = ProtoField.uint8 ("acp2.pid",    "PID",      base.DEC, acp2_pid_valstr),
    pad     = ProtoField.uint8 ("acp2.pad",    "Padding",  base.HEX),
    version = ProtoField.uint8 ("acp2.version","Version",  base.DEC),
    obj_id  = ProtoField.uint32("acp2.obj_id", "Object ID",base.DEC),
    idx     = ProtoField.uint32("acp2.idx",    "Index",    base.DEC),
}
acp2_proto.fields = acp2_f

-------------------------------------------------------------------------------
-- ACP2 Property ProtoFields
-------------------------------------------------------------------------------

local prop_f = {
    pid       = ProtoField.uint8 ("acp2.prop.pid",       "Property ID",      base.DEC, acp2_pid_valstr),
    data_byte = ProtoField.uint8 ("acp2.prop.data",      "Data/VType",       base.DEC),
    plen      = ProtoField.uint16("acp2.prop.plen",      "Length (plen)",    base.DEC),
    padding   = ProtoField.bytes ("acp2.prop.padding",   "Alignment Padding"),
    -- typed value fields
    obj_type  = ProtoField.uint8 ("acp2.prop.obj_type",  "Object Type",   base.DEC, acp2_objtype_valstr),
    access    = ProtoField.uint8 ("acp2.prop.access",    "Access",        base.DEC, acp2_access_valstr),
    numtype   = ProtoField.uint8 ("acp2.prop.numtype",   "Number Type",   base.DEC, acp2_numtype_valstr),
    delay     = ProtoField.uint32("acp2.prop.delay",     "Announce Delay (ms)", base.DEC),
    strmax    = ProtoField.uint16("acp2.prop.strmax",    "String Max Length",   base.DEC),
    val_s32   = ProtoField.int32 ("acp2.prop.val_s32",   "Value (s32)",   base.DEC),
    val_u32   = ProtoField.uint32("acp2.prop.val_u32",   "Value (u32)",   base.DEC),
    val_s64   = ProtoField.int64 ("acp2.prop.val_s64",   "Value (s64)",   base.DEC),
    val_u64   = ProtoField.uint64("acp2.prop.val_u64",   "Value (u64)",   base.DEC),
    val_float = ProtoField.float ("acp2.prop.val_float", "Value (float)"),
    val_ipv4  = ProtoField.ipv4  ("acp2.prop.val_ipv4",  "Value (IPv4)"),
    val_str   = ProtoField.string("acp2.prop.val_str",   "Value (string)"),
    child_id  = ProtoField.uint32("acp2.prop.child_id",  "Child Object ID", base.DEC),
    opt_idx   = ProtoField.uint32("acp2.prop.opt_idx",   "Option Index",    base.DEC),
    opt_str   = ProtoField.string("acp2.prop.opt_str",   "Option String"),
    tag       = ProtoField.uint16("acp2.prop.tag",       "Event Tag",     base.DEC),
    prio      = ProtoField.uint8 ("acp2.prop.prio",      "Event Priority",base.DEC),
    state     = ProtoField.uint8 ("acp2.prop.state",     "Event State",   base.DEC),
    parent_id = ProtoField.uint32("acp2.prop.parent_id", "Preset Parent", base.DEC),
    depth_val = ProtoField.uint32("acp2.prop.depth_val", "Preset Index",  base.DEC),
    raw       = ProtoField.bytes ("acp2.prop.raw",       "Raw Value"),
}
acp2_prop_proto.fields = prop_f

-------------------------------------------------------------------------------
-- Helpers
-------------------------------------------------------------------------------

local function align4_pad(plen)
    local rem = plen % 4
    if rem == 0 then return 0 end
    return 4 - rem
end

-------------------------------------------------------------------------------
-- Parse a numeric value property (pids 8-12, also 4, 16-18, 20)
-- vtype comes from data byte (byte 1 of property header)
-- Returns nothing; adds fields to tree
-------------------------------------------------------------------------------
local function parse_numeric_value(tvbuf, tree, offset, vtype, remaining)
    if remaining < 4 then return end

    -- Spec §5.4 "Property value wire sizes":
    --   s8/s16/s32 all stored as 4-byte signed (sign-extended)
    --   u8/u16/u32 all stored as 4-byte unsigned (zero-extended)
    --   s64/u64    stored as 8 bytes
    --   float      stored as 4 bytes
    -- The Wireshark ProtoField types are fixed at declaration, so we
    -- always read 4/8 bytes and override the displayed label via
    -- set_text() to reflect the DECLARED type (s8/s16/s32/u8/...) —
    -- matches the "[Number.sX]" annotation in the property header.
    local type_label = acp2_numtype_valstr[vtype] or ("vtype=" .. vtype)

    if vtype >= 0 and vtype <= 2 then
        local v = tvbuf:range(offset, 4):int()
        tree:add(prop_f.val_s32, tvbuf:range(offset, 4))
            :set_text(string.format("Value (%s): %d", type_label, v))
    elseif vtype == 3 then
        if remaining >= 8 then
            tree:add(prop_f.val_s64, tvbuf:range(offset, 8))
                :set_text("Value (s64): " .. tostring(tvbuf:range(offset, 8):int64()))
        end
    elseif vtype >= 4 and vtype <= 6 then
        local v = tvbuf:range(offset, 4):uint()
        tree:add(prop_f.val_u32, tvbuf:range(offset, 4))
            :set_text(string.format("Value (%s): %d", type_label, v))
    elseif vtype == 7 then
        if remaining >= 8 then
            tree:add(prop_f.val_u64, tvbuf:range(offset, 8))
                :set_text("Value (u64): " .. tostring(tvbuf:range(offset, 8):uint64()))
        end
    elseif vtype == 8 then
        local v = tvbuf:range(offset, 4):float()
        tree:add(prop_f.val_float, tvbuf:range(offset, 4))
            :set_text(string.format("Value (float): %g", v))
    elseif vtype == 9 then
        local v = tvbuf:range(offset, 4):uint()
        tree:add(prop_f.val_u32, tvbuf:range(offset, 4))
            :set_text(string.format("Value (enum/preset index): %d", v))
    elseif vtype == 10 then
        tree:add(prop_f.val_ipv4, tvbuf:range(offset, 4))
    elseif vtype == 11 then
        local str_bytes = tvbuf:range(offset, remaining):bytes()
        local str_len = remaining
        for i = 0, remaining - 1 do
            if str_bytes:get_index(i) == 0 then
                str_len = i
                break
            end
        end
        if str_len > 0 then
            local s = tvbuf:range(offset, str_len):string()
            tree:add(prop_f.val_str, tvbuf:range(offset, str_len))
                :set_text(string.format("Value (string): \"%s\"", s))
        end
    end
end

-------------------------------------------------------------------------------
-- Parse a single ACP2 property TLV
-- Returns total bytes consumed (plen + alignment padding)
-------------------------------------------------------------------------------
local function parse_property(tvbuf, pktinfo, parent_tree, offset)
    local buf_remaining = tvbuf:reported_length_remaining(offset)
    if buf_remaining < 4 then
        return buf_remaining  -- not enough for a header
    end

    local pid_val  = tvbuf:range(offset, 1):uint()
    local data_val = tvbuf:range(offset + 1, 1):uint()
    local plen_val = tvbuf:range(offset + 2, 2):uint()

    if plen_val < 4 then plen_val = 4 end  -- safety

    local pad = align4_pad(plen_val)
    local total = plen_val + pad

    -- clamp to available buffer
    if total > buf_remaining then
        total = buf_remaining
    end

    local pid_name = acp2_pid_valstr[pid_val] or ("pid=" .. pid_val)
    local tree = parent_tree:add(acp2_prop_proto, tvbuf:range(offset, total))
    tree:set_text("Property: " .. pid_name .. " (pid=" .. pid_val .. ", plen=" .. plen_val .. ")")

    tree:add(prop_f.pid,       tvbuf:range(offset, 1))
    tree:add(prop_f.data_byte, tvbuf:range(offset + 1, 1))
    tree:add(prop_f.plen,      tvbuf:range(offset + 2, 2))

    local val_offset = offset + 4
    local val_len    = plen_val - 4
    if val_len < 0 then val_len = 0 end

    -- Per-PID decoding
    if pid_val == 1 then
        -- object_type: inline in data byte
        tree:add(prop_f.obj_type, tvbuf:range(offset + 1, 1))

    elseif pid_val == 2 then
        -- label: null-terminated string in value area
        if val_len > 0 then
            -- find null terminator
            local str_end = val_len
            for i = 0, val_len - 1 do
                if tvbuf:range(val_offset + i, 1):uint() == 0 then
                    str_end = i
                    break
                end
            end
            if str_end > 0 then
                tree:add(prop_f.val_str, tvbuf:range(val_offset, str_end))
            else
                tree:add(prop_f.val_str, tvbuf:range(val_offset, 1)):set_text("Label: (empty)")
            end
        end

    elseif pid_val == 3 then
        -- access: inline in data byte
        tree:add(prop_f.access, tvbuf:range(offset + 1, 1))

    elseif pid_val == 4 then
        -- announce_delay: u32 in value area
        if val_len >= 4 then
            tree:add(prop_f.delay, tvbuf:range(val_offset, 4))
        end

    elseif pid_val == 5 then
        -- number_type: inline in data byte
        tree:add(prop_f.numtype, tvbuf:range(offset + 1, 1))

    elseif pid_val == 6 then
        -- string_max_length: u16 in value area (or stored as u32)
        if val_len >= 4 then
            tree:add(prop_f.val_u32, tvbuf:range(val_offset, 4)):set_text("String Max Length: " .. tvbuf:range(val_offset, 4):uint())
        elseif val_len >= 2 then
            tree:add(prop_f.strmax, tvbuf:range(val_offset, 2))
        end

    elseif pid_val == 7 then
        -- preset_depth: list of u32 valid idx values
        local pos = val_offset
        local idx_num = 0
        while (pos + 4) <= (val_offset + val_len) do
            tree:add(prop_f.depth_val, tvbuf:range(pos, 4))
            pos = pos + 4
            idx_num = idx_num + 1
        end
        tree:append_text(" (" .. idx_num .. " indices)")

    elseif pid_val >= 8 and pid_val <= 12 then
        -- value, default_value, min_value, max_value, step_size
        -- vtype is in data byte. Derive the containing object type from
        -- vtype (spec §5.2.2 "Property value type") so the property tree
        -- shows both the object category (Number/Enum/IPv4/String) and
        -- the numeric subtype: e.g. "[Number.float]" instead of just
        -- "[float]". This matters for announces, which only carry pid 8
        -- and never pid 1 (object_type) — otherwise a consumer has to
        -- remember per-obj context across frames to know what kind of
        -- object just changed.
        local vtype = data_val
        local vtype_name = acp2_numtype_valstr[vtype] or ("vtype=" .. vtype)
        local obj_cat
        if vtype <= 8 then
            obj_cat = "Number"         -- s8/s16/s32/s64/u8/u16/u32/u64/float
        elseif vtype == 9 then
            obj_cat = "Enum/Preset"    -- u32 index
        elseif vtype == 10 then
            obj_cat = "IPv4"           -- 4 bytes
        elseif vtype == 11 then
            obj_cat = "String"         -- NUL-terminated UTF-8
        else
            obj_cat = "Unknown"
        end
        tree:append_text(" [" .. obj_cat .. "." .. vtype_name .. "]")
        -- Expose the derived object category as a discrete field so
        -- users can filter and the Detail panel shows it on its own row.
        tree:add(prop_f.obj_type, tvbuf:range(offset + 1, 1))
            :set_text("Object Category (derived): " .. obj_cat)
        if val_len > 0 then
            parse_numeric_value(tvbuf, tree, val_offset, vtype, val_len)
        end

    elseif pid_val == 13 then
        -- unit: null-terminated string
        if val_len > 0 then
            local str_end = val_len
            for i = 0, val_len - 1 do
                if tvbuf:range(val_offset + i, 1):uint() == 0 then
                    str_end = i
                    break
                end
            end
            if str_end > 0 then
                tree:add(prop_f.val_str, tvbuf:range(val_offset, str_end))
            end
        end

    elseif pid_val == 14 then
        -- children: array of u32 child obj-ids
        local pos = val_offset
        local count = 0
        while (pos + 4) <= (val_offset + val_len) do
            tree:add(prop_f.child_id, tvbuf:range(pos, 4))
            pos = pos + 4
            count = count + 1
        end
        tree:append_text(" (" .. count .. " children)")

    elseif pid_val == 15 then
        -- options: each option = u32 index + null-terminated string + pad
        -- data byte = num_options
        tree:append_text(" (" .. data_val .. " options)")
        local pos = val_offset
        local opt_num = 0
        while pos < (val_offset + val_len) and opt_num < data_val do
            if (pos + 4) > (val_offset + val_len) then break end
            tree:add(prop_f.opt_idx, tvbuf:range(pos, 4))
            pos = pos + 4
            -- find null-terminated string
            local str_start = pos
            while pos < (val_offset + val_len) and tvbuf:range(pos, 1):uint() ~= 0 do
                pos = pos + 1
            end
            if pos > str_start then
                tree:add(prop_f.opt_str, tvbuf:range(str_start, pos - str_start))
            end
            -- skip null terminator
            if pos < (val_offset + val_len) then
                pos = pos + 1
            end
            -- skip padding to 4-byte boundary within option block
            -- options are 72 bytes each per spec, but we parse dynamically
            opt_num = opt_num + 1
        end

    elseif pid_val == 16 then
        -- event_tag: u16
        if val_len >= 2 then
            tree:add(prop_f.tag, tvbuf:range(val_offset, 2))
        elseif val_len >= 4 then
            tree:add(prop_f.val_u32, tvbuf:range(val_offset, 4)):set_text("Event Tag: " .. tvbuf:range(val_offset, 4):uint())
        end

    elseif pid_val == 17 then
        -- event_prio: inline in data byte or in value
        tree:add(prop_f.prio, tvbuf:range(offset + 1, 1))
        if val_len >= 4 then
            tree:add(prop_f.val_u32, tvbuf:range(val_offset, 4))
        end

    elseif pid_val == 18 then
        -- event_state: inline in data byte or in value
        tree:add(prop_f.state, tvbuf:range(offset + 1, 1))
        if val_len >= 4 then
            tree:add(prop_f.val_u32, tvbuf:range(val_offset, 4))
        end

    elseif pid_val == 19 then
        -- event_messages: two null-terminated strings
        if val_len > 0 then
            local pos = val_offset
            local limit = val_offset + val_len
            -- first string (on message)
            local s1_start = pos
            while pos < limit and tvbuf:range(pos, 1):uint() ~= 0 do
                pos = pos + 1
            end
            if pos > s1_start then
                tree:add(prop_f.val_str, tvbuf:range(s1_start, pos - s1_start)):set_text("Event On: " .. tvbuf:range(s1_start, pos - s1_start):string())
            end
            if pos < limit then pos = pos + 1 end  -- skip null
            -- second string (off message)
            local s2_start = pos
            while pos < limit and tvbuf:range(pos, 1):uint() ~= 0 do
                pos = pos + 1
            end
            if pos > s2_start then
                tree:add(prop_f.val_str, tvbuf:range(s2_start, pos - s2_start)):set_text("Event Off: " .. tvbuf:range(s2_start, pos - s2_start):string())
            end
        end

    elseif pid_val == 20 then
        -- preset_parent: u32 parent obj-id
        if val_len >= 4 then
            tree:add(prop_f.parent_id, tvbuf:range(val_offset, 4))
        end

    else
        -- unknown pid: show raw bytes
        if val_len > 0 then
            tree:add(prop_f.raw, tvbuf:range(val_offset, val_len))
        end
    end

    -- show alignment padding bytes if present
    if pad > 0 and (plen_val + pad) <= buf_remaining then
        tree:add(prop_f.padding, tvbuf:range(offset + plen_val, pad))
    end

    return total
end

-------------------------------------------------------------------------------
-- Parse all property TLVs from offset to end of buffer
-------------------------------------------------------------------------------
local function parse_properties(tvbuf, pktinfo, tree, offset)
    local limit = tvbuf:reported_length_remaining()
    while offset < limit do
        local remaining = limit - offset
        if remaining < 4 then break end
        local consumed = parse_property(tvbuf, pktinfo, tree, offset)
        if consumed == nil or consumed <= 0 then break end
        offset = offset + consumed
    end
    return offset
end

-------------------------------------------------------------------------------
-- ACP2 dissector (called for AN2 proto=2, type=4 data frames)
-------------------------------------------------------------------------------
-- Peek the value bytes of the first property at `offset` in tvbuf and
-- return a short text summary for the Info column: `value=42`,
-- `value="ACP2-Frame"`, `value=10.4.210.100`, etc. Returns "" when
-- nothing meaningful can be extracted.
local function summarize_first_prop(tvbuf, offset, pktinfo, slot, obj_id)
    local remaining = tvbuf:reported_length_remaining(offset)
    if remaining < 4 then return "" end
    local pid = tvbuf:range(offset, 1):uint()
    local data = tvbuf:range(offset + 1, 1):uint()
    local plen = tvbuf:range(offset + 2, 2):uint()
    if plen < 4 then return "" end
    local vlen = plen - 4
    if vlen < 0 then vlen = 0 end
    local voff = offset + 4

    -- inline-in-data pids (value rides the header's data byte itself)
    if pid == 1 then
        local t = acp2_objtype_valstr[data] or ("type=" .. data)
        return "type=" .. t
    elseif pid == 3 then
        return "access=" .. data
    elseif pid == 5 then
        local t = acp2_numtype_valstr[data] or ("nt=" .. data)
        return "numtype=" .. t
    elseif pid == 15 then
        return "options=" .. data
    end

    if vlen == 0 then return "" end

    if pid == 2 or pid == 13 or pid == 19 then
        -- label / unit / event_messages = NUL-terminated UTF-8
        local strlen = vlen
        for i = 0, vlen - 1 do
            if tvbuf:range(voff + i, 1):uint() == 0 then strlen = i; break end
        end
        if strlen == 0 then return "" end
        local s = tvbuf:range(voff, strlen):string()
        -- Cache labels (pid=2) keyed by (conversation, slot, obj_id) so
        -- subsequent frames referring to the same (slot, obj_id) can show
        -- the human-readable label in the Info column (issue #58).
        if pid == 2 and pktinfo ~= nil and slot ~= nil and obj_id ~= nil then
            cache_label(pktinfo, slot, obj_id, s)
        end
        return "\"" .. s .. "\""
    elseif pid == 6 then
        if vlen >= 2 then
            return "maxlen=" .. tvbuf:range(voff, 2):uint()
        end
    elseif pid == 14 then
        -- children array
        local n = math.floor(vlen / 4)
        return n .. " children"
    elseif pid >= 8 and pid <= 12 then
        -- value / default / min / max / step — decode by vtype in data
        -- byte. Tag each summary with the declared type so the Info
        -- column reads e.g. `value(s8)=-40`, `value(float)=-35.3073`,
        -- `value(string)="Input-A"` — no guessing needed.
        local vtype = data
        local t = acp2_numtype_valstr[vtype] or ("vtype=" .. vtype)
        if vtype <= 2 then
            return string.format("value(%s)=%d", t, tvbuf:range(voff, 4):int())
        elseif vtype == 3 and vlen >= 8 then
            return string.format("value(s64)=%s", tostring(tvbuf:range(voff, 8):int64()))
        elseif vtype >= 4 and vtype <= 6 then
            return string.format("value(%s)=%d", t, tvbuf:range(voff, 4):uint())
        elseif vtype == 7 and vlen >= 8 then
            return string.format("value(u64)=%s", tostring(tvbuf:range(voff, 8):uint64()))
        elseif vtype == 8 then
            return string.format("value(float)=%g", tvbuf:range(voff, 4):float())
        elseif vtype == 9 then
            return string.format("value(enum)=%d", tvbuf:range(voff, 4):uint())
        elseif vtype == 10 then
            return "value(ipv4)=" .. tostring(tvbuf:range(voff, 4):ipv4())
        elseif vtype == 11 then
            local strlen = vlen
            for i = 0, vlen - 1 do
                if tvbuf:range(voff + i, 1):uint() == 0 then strlen = i; break end
            end
            if strlen == 0 then return "value(string)=\"\"" end
            return string.format("value(string)=\"%s\"", tvbuf:range(voff, strlen):string())
        end
    end
    return ""
end

function acp2_proto.dissector(tvbuf, pktinfo, root)
    local pktlen = tvbuf:reported_length_remaining()
    if pktlen < 4 then return 0 end

    local tree = root:add(acp2_proto, tvbuf:range(0, pktlen))

    local type_val = tvbuf:range(0, 1):uint()
    local mtid_val = tvbuf:range(1, 1):uint()
    local byte2    = tvbuf:range(2, 1):uint()
    local byte3    = tvbuf:range(3, 1):uint()

    tree:add(acp2_f.type, tvbuf:range(0, 1))
    tree:add(acp2_f.mtid, tvbuf:range(1, 1))

    local type_short = { [0]="Req", [1]="Rep", [2]="Evt", [3]="Err" }
    local type_str = type_short[type_val] or ("T" .. type_val)
    local info_parts = {}
    table.insert(info_parts, type_str)
    table.insert(info_parts, "mtid=" .. mtid_val)

    if type_val == 0 or type_val == 1 then
        -- Request or Reply
        tree:add(acp2_f.func, tvbuf:range(2, 1))
        local func_str = acp2_func_valstr[byte2] or ("func=" .. byte2)
        table.insert(info_parts, func_str)

        if byte2 == 0 then
            -- GetVersion
            tree:add(acp2_f.version, tvbuf:range(3, 1))
            if type_val == 1 then
                table.insert(info_parts, "v=" .. byte3)
            end

        elseif byte2 == 1 then
            -- GetObject
            tree:add(acp2_f.pad, tvbuf:range(3, 1))
            if pktlen >= 12 then
                local obj_id = tvbuf:range(4, 4):uint()
                local idx    = tvbuf:range(8, 4):uint()
                tree:add(acp2_f.obj_id, tvbuf:range(4, 4))
                tree:add(acp2_f.idx,    tvbuf:range(8, 4))
                -- Dotted slot.obj path per issue #58 (match Ember+ OID style).
                -- path_with_label substitutes the cached label when one was
                -- learned earlier in this TCP conversation.
                table.insert(info_parts, path_with_label(pktinfo, acp2_current_slot, obj_id))
                if idx ~= 0 then
                    table.insert(info_parts, "idx=" .. idx)
                end
                if type_val == 1 and pktlen > 12 then
                    parse_properties(tvbuf, pktinfo, tree, 12)
                end
            end

        elseif byte2 == 2 then
            -- GetProperty
            tree:add(acp2_f.pid, tvbuf:range(3, 1))
            local pid_name = acp2_pid_valstr[byte3] or ("pid=" .. byte3)
            if pktlen >= 12 then
                local obj_id = tvbuf:range(4, 4):uint()
                local idx    = tvbuf:range(8, 4):uint()
                tree:add(acp2_f.obj_id, tvbuf:range(4, 4))
                tree:add(acp2_f.idx,    tvbuf:range(8, 4))
                table.insert(info_parts, path_with_label(pktinfo, acp2_current_slot, obj_id))
                table.insert(info_parts, "pid=" .. pid_name)
                if idx ~= 0 then
                    table.insert(info_parts, "idx=" .. idx)
                end
                if type_val == 1 and pktlen > 12 then
                    local vs = summarize_first_prop(tvbuf, 12, pktinfo, acp2_current_slot, obj_id)
                    if vs ~= "" then table.insert(info_parts, vs) end
                    parse_properties(tvbuf, pktinfo, tree, 12)
                end
            end

        elseif byte2 == 3 then
            -- SetProperty
            tree:add(acp2_f.pid, tvbuf:range(3, 1))
            local pid_name = acp2_pid_valstr[byte3] or ("pid=" .. byte3)
            if pktlen >= 12 then
                local obj_id = tvbuf:range(4, 4):uint()
                local idx    = tvbuf:range(8, 4):uint()
                tree:add(acp2_f.obj_id, tvbuf:range(4, 4))
                tree:add(acp2_f.idx,    tvbuf:range(8, 4))
                table.insert(info_parts, path_with_label(pktinfo, acp2_current_slot, obj_id))
                table.insert(info_parts, "pid=" .. pid_name)
                if idx ~= 0 then
                    table.insert(info_parts, "idx=" .. idx)
                end
                if pktlen > 12 then
                    local vs = summarize_first_prop(tvbuf, 12, pktinfo, acp2_current_slot, obj_id)
                    if vs ~= "" then table.insert(info_parts, vs) end
                    parse_properties(tvbuf, pktinfo, tree, 12)
                end
            end
        end

    elseif type_val == 2 then
        -- Announce
        tree:add(acp2_f.pad, tvbuf:range(2, 1))
        tree:add(acp2_f.pid, tvbuf:range(3, 1))
        local pid_name = acp2_pid_valstr[byte3] or ("pid=" .. byte3)
        table.insert(info_parts, "Announce")
        table.insert(info_parts, "pid=" .. pid_name)

        if pktlen >= 12 then
            local obj_id = tvbuf:range(4, 4):uint()
            local idx    = tvbuf:range(8, 4):uint()
            tree:add(acp2_f.obj_id, tvbuf:range(4, 4))
            tree:add(acp2_f.idx,    tvbuf:range(8, 4))
            table.insert(info_parts, path_with_label(pktinfo, acp2_current_slot, obj_id))
            if idx ~= 0 then
                table.insert(info_parts, "idx=" .. idx)
            end
            if pktlen > 12 then
                local vs = summarize_first_prop(tvbuf, 12, pktinfo, acp2_current_slot, obj_id)
                if vs ~= "" then table.insert(info_parts, vs) end
                parse_properties(tvbuf, pktinfo, tree, 12)
            end
        end

    elseif type_val == 3 then
        -- Error
        tree:add(acp2_f.stat, tvbuf:range(2, 1))
        tree:add(acp2_f.pad,  tvbuf:range(3, 1))
        local err_str = acp2_error_valstr[byte2] or ("stat=" .. byte2)
        table.insert(info_parts, "ERROR")
        table.insert(info_parts, err_str)

        if pktlen >= 12 then
            tree:add(acp2_f.obj_id, tvbuf:range(4, 4))
            tree:add(acp2_f.idx,    tvbuf:range(8, 4))
            local obj_id = tvbuf:range(4, 4):uint()
            table.insert(info_parts, path_with_label(pktinfo, acp2_current_slot, obj_id))
        end
    end

    -- Write composed info text to the module-local side-channel so the
    -- AN2 caller can render it into pktinfo.cols.info. Multi-return here
    -- is unreliable because Wireshark's Proto.dissector wrapper drops
    -- extra values.
    acp2_last_info = table.concat(info_parts, " ")
    return pktlen
end

-------------------------------------------------------------------------------
-- AN2 internal (proto=0) dissection helper
-- Returns info string
-------------------------------------------------------------------------------
local function dissect_an2_internal(tvbuf, pktinfo, tree, an2_type, dlen)
    if dlen < 1 then return "" end

    local func_val = tvbuf:range(0, 1):uint()
    tree:add(an2_f.func, tvbuf:range(0, 1))
    local func_str = an2_func_valstr[func_val] or ("func=" .. func_val)

    local info = "AN2 "
    if an2_type == 0 then
        info = info .. "Req "
    elseif an2_type == 1 then
        info = info .. "Reply "
    elseif an2_type == 2 then
        info = info .. "Event "
    elseif an2_type == 3 then
        info = info .. "Error "
    end
    info = info .. func_str

    if func_val == 0 then
        -- GetVersion
        if an2_type == 1 and dlen >= 2 then
            local ver = tvbuf:range(1, 1):uint()
            tree:add(an2_f.version, tvbuf:range(1, 1))
            info = info .. " v=" .. ver
        end
    elseif func_val == 1 then
        -- GetDeviceInfo
        if dlen > 1 then
            tree:add(an2_f.payload, tvbuf:range(1, dlen - 1))
        end
    elseif func_val == 2 then
        -- GetSlotInfo
        if dlen > 1 then
            tree:add(an2_f.payload, tvbuf:range(1, dlen - 1))
        end
    elseif func_val == 3 then
        -- EnableProtocolEvents
        if dlen > 1 then
            tree:add(an2_f.payload, tvbuf:range(1, dlen - 1))
        end
    else
        if dlen > 1 then
            tree:add(an2_f.payload, tvbuf:range(1, dlen - 1))
        end
    end

    return info
end

-------------------------------------------------------------------------------
-- Dissect a single AN2 frame starting at `offset` in tvbuf.
-- Returns bytes consumed (AN2_HDR_LEN + dlen), or negative DESEGMENT value,
-- or 0 if not enough data.
-------------------------------------------------------------------------------
local function dissect_one_an2(tvbuf, pktinfo, root, offset)
    local remaining = tvbuf:reported_length_remaining(offset)

    -- Need at least the 8-byte AN2 header
    if remaining < AN2_HDR_LEN then
        return -(DESEGMENT_ONE_MORE_SEGMENT)
    end

    -- Validate magic
    local magic = tvbuf:range(offset, 2):uint()
    if magic ~= AN2_MAGIC then
        -- Not a valid AN2 frame - skip one byte and let Wireshark retry
        return 0
    end

    local proto_val = tvbuf:range(offset + 2, 1):uint()
    local slot_val  = tvbuf:range(offset + 3, 1):uint()
    local mtid_val  = tvbuf:range(offset + 4, 1):uint()
    local type_val  = tvbuf:range(offset + 5, 1):uint()
    local dlen      = tvbuf:range(offset + 6, 2):uint()

    local frame_len = AN2_HDR_LEN + dlen

    -- Need full frame
    if remaining < frame_len then
        -- Request reassembly
        pktinfo.desegment_offset = offset
        pktinfo.desegment_len    = frame_len - remaining
        return -(frame_len - remaining)
    end

    -- Build AN2 tree
    local an2_tree = root:add(an2_proto, tvbuf:range(offset, frame_len))

    an2_tree:add(an2_f.magic, tvbuf:range(offset, 2))
    an2_tree:add(an2_f.proto, tvbuf:range(offset + 2, 1))
    an2_tree:add(an2_f.slot,  tvbuf:range(offset + 3, 1))
    an2_tree:add(an2_f.mtid,  tvbuf:range(offset + 4, 1))
    an2_tree:add(an2_f.type,  tvbuf:range(offset + 5, 1))
    an2_tree:add(an2_f.dlen,  tvbuf:range(offset + 6, 2))

    local proto_str = an2_proto_valstr[proto_val] or ("proto=" .. proto_val)
    local type_str  = an2_type_valstr[type_val]   or ("type=" .. type_val)
    local slot_str  = ""
    if slot_val == 255 then
        slot_str = "broadcast"
    else
        slot_str = "slot=" .. slot_val
    end

    local info_str = ""

    if proto_val == 2 and type_val == 4 and dlen > 0 then
        -- ACP2 data frame: hand off to ACP2 dissector. We read the
        -- composed info text from the module-local side-channel
        -- `acp2_last_info`, because Wireshark's Proto.dissector wrapper
        -- drops multi-return values when called via Lua. Slot is
        -- forwarded the same way so ACP2 can build dotted paths.
        acp2_last_info = ""
        acp2_current_slot = slot_val
        local payload_tvb = tvbuf:range(offset + AN2_HDR_LEN, dlen):tvb()
        acp2_proto.dissector(payload_tvb, pktinfo, an2_tree)
        info_str = "AN2 > ACP2 " .. acp2_last_info

    elseif proto_val == 2 and dlen > 0 then
        -- ACP2 non-data frame (req/reply/event/error at AN2 level)
        acp2_last_info = ""
        acp2_current_slot = slot_val
        local payload_tvb = tvbuf:range(offset + AN2_HDR_LEN, dlen):tvb()
        acp2_proto.dissector(payload_tvb, pktinfo, an2_tree)
        info_str = "AN2 " .. type_str .. " > ACP2 " .. acp2_last_info

    elseif proto_val == 0 and dlen > 0 then
        -- AN2 internal protocol
        local payload_tvb = tvbuf:range(offset + AN2_HDR_LEN, dlen):tvb()
        info_str = dissect_an2_internal(payload_tvb, pktinfo, an2_tree, type_val, dlen)
        info_str = info_str .. " " .. slot_str

    elseif proto_val == 1 and dlen > 0 then
        -- ACP1 over AN2 - just label it, don't decode
        info_str = "AN2 > ACP1 " .. type_str .. " " .. slot_str
        if dlen > 0 then
            an2_tree:add(an2_f.payload, tvbuf:range(offset + AN2_HDR_LEN, dlen))
        end

    else
        info_str = "AN2 " .. proto_str .. " " .. type_str .. " " .. slot_str
        if dlen > 0 then
            an2_tree:add(an2_f.payload, tvbuf:range(offset + AN2_HDR_LEN, dlen))
        end
    end

    an2_tree:set_text("AN2 [" .. proto_str .. "] " .. type_str .. " " .. slot_str .. " dlen=" .. dlen)

    -- Set info column
    pktinfo.cols.protocol:set("AN2/ACP2")
    pktinfo.cols.info:set(info_str)

    return frame_len
end

-------------------------------------------------------------------------------
-- Main AN2 dissector entry point with TCP reassembly
-------------------------------------------------------------------------------
function an2_proto.dissector(tvbuf, pktinfo, root)
    local pktlen = tvbuf:reported_length_remaining()
    if pktlen == 0 then return 0 end

    local offset = 0
    local frames = 0

    while offset < pktlen do
        local result = dissect_one_an2(tvbuf, pktinfo, root, offset)

        if result < 0 then
            -- Reassembly requested: dissect_one_an2 already set desegment fields
            return offset  -- return bytes consumed so far
        elseif result == 0 then
            -- Invalid magic or other error, skip remaining
            return offset
        else
            offset = offset + result
            frames = frames + 1
        end
    end

    -- If multiple frames in one TCP segment, update info
    if frames > 1 then
        pktinfo.cols.info:prepend("[" .. frames .. " AN2 frames] ")
    end

    return offset
end

-------------------------------------------------------------------------------
-- Register on TCP port 2072
-------------------------------------------------------------------------------
local tcp_port = DissectorTable.get("tcp.port")
tcp_port:add(2072, an2_proto)
