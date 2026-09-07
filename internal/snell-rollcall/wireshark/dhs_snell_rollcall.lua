-------------------------------------------------------------------------------
--
-- Wireshark Lua dissector: Snell RollCall over IPShare (TCP, default 2050)
--
-- Decodes both wire generations from one dissector, because one connection
-- carries both: which one a session speaks is decided by the SV_LONGSTR bit in
-- its SP_CALL, not by the device, so a capture of a real controller has 16-bit
-- and 32-bit messages side by side.
--
-- Covered:
--   * The transmission header (spec 10.2.1) and TCP reassembly, including
--     several messages in one segment and one message across segments.
--   * MESSAGE_STR addressing: net / unit / port / session index, with the
--     unconnected index 255 named rather than printed as a number.
--   * All 72 packet types (spec 12.2), each one named.
--   * Per-type payload decode for everything a live device actually sends:
--     sessions, identity, menus in both generations, values in both
--     generations, displays, files, device maps, block transfers.
--   * The router command space (Full Control Command Set): commands 100+ are
--     named, and Data Transfer Params are decoded, so a crosspoint set reads
--     as a source pin rather than as opaque bytes.
--
-- The Info column names the message and the arguments that identify it: the
-- session index, the command number, the slot, the file, the crosspoint. Two
-- frames that differ in what they do differ in the Info column.
--
-- Byte authority: internal/snell-rollcall/codec (frame.go, addr.go, pkttype.go,
-- payload_*.go) and the vendor headers those cite. Behaviour measured against
-- the vendor Centra controller: internal/snell-rollcall/docs/oracle-centra.md.
--
-- Compatible with Wireshark 4.x.
--
-------------------------------------------------------------------------------

local default_ports = "2050-2060,2057"

-- Wire sizes, from codec/frame.go.
local TX_HEADER_SIZE   = 4
local MSG_HEADER_SIZE  = 14
local ROLL_HEADER_SIZE = 2
local HEADER_SIZE      = TX_HEADER_SIZE + MSG_HEADER_SIZE + ROLL_HEADER_SIZE
local TX_FLAGS_MODE3   = 12
local SPEC_MAX_TX_LEN  = 1570
local INDEX_UNKNOWN    = 255

-------------------------------------------------------------------------------
-- Catalogues
-------------------------------------------------------------------------------

-- Packet types, mirrored from codec/pkttype.go.
local packet_type = {
    [0] = "NACK",
    [1] = "ACK",
    [2] = "CALL",
    [3] = "TERM",
    [4] = "GETSTAT",
    [5] = "RETSTAT",
    [6] = "GETID",
    [7] = "RETID",
    [8] = "GETFUNC",
    [9] = "RETFUNC",
    [10] = "DISPDATA",
    [11] = "GETFSTAT",
    [12] = "RETFSTAT",
    [13] = "RESET",
    [14] = "INVCMD",
    [15] = "BUSY",
    [16] = "SETPARAM",
    [17] = "TIME",
    [18] = "FUNCLISTCHG",
    [19] = "GETDEVLIST",
    [20] = "RETDEVINFO",
    [21] = "GETDEVINFO",
    [22] = "LOGREQ",
    [23] = "INVSESS",
    [24] = "REALTIME",
    [25] = "WAIT",
    [26] = "CLEARSESS",
    [27] = "BKCHNREADY",
    [28] = "KEEPALIVE",
    [29] = "GETLOCDEVMAP",
    [30] = "FUNCSTYLECHG",
    [31] = "SETGROUP",
    [32] = "STOPGROUP",
    [33] = "IAM",
    [34] = "SETGRPFUNC",
    [35] = "GETNEXTPKT",
    [36] = "REPFCHG",
    [37] = "STOPREPFCHG",
    [38] = "ROUTEERROR",
    [39] = "BLOCKHEADER",
    [40] = "STREAMMODE",
    [41] = "STREAMDATA",
    [42] = "FILEDIR",
    [43] = "RETFILEDIR",
    [44] = "RAW",
    [45] = "SETUSERLEVEL",
    [46] = "FILEDELETE",
    [47] = "GETTIME",
    [48] = "FILERENAME",
    [49] = "LOGDATA",
    [50] = "GETSRVBYNAME",
    [51] = "FILEOPEN",
    [52] = "RETFILEOPEN",
    [53] = "FILECLOSE",
    [54] = "GETDISPDATA",
    [55] = "FILEREAD",
    [56] = "RETFILEREAD",
    [57] = "FILEWRITE",
    [58] = "SETMULTI",
    [59] = "FILERET",
    [60] = "GETDISPCAPS",
    [61] = "RETDISPCAPS",
    [62] = "DRAWBITMAP",
    [63] = "DRAWTEXT",
    [64] = "MAKEDIRECTORY",
    [65] = "GETMENUCOUNT",
    [66] = "RETMENUCOUNT",
    [67] = "GETMENUITEM",
    [68] = "RETMENUITEM",
    [69] = "GETVALUE",
    [70] = "SETVALUE",
    [71] = "RETVALUE",
}

-- Service bits (spec 12.1). SV_LONGSTR is the capability that decides the
-- generation, which is why it is worth seeing in the tree.
local service_bits = {
    { 0x0001, "Menus" },   { 0x0002, "Control" }, { 0x0004, "Display" },
    { 0x0008, "File" },    { 0x0010, "Logging" }, { 0x0020, "Stream" },
    { 0x0040, "Map" },     { 0x0080, "Ports" },   { 0x0100, "Net" },
    { 0x0200, "Exec" },    { 0x0400, "Time" },    { 0x0800, "Res2" },
    { 0x1000, "Thumbnail" }, { 0x2000, "FastMenu" }, { 0x4000, "Loc3" },
    { 0x8000, "LongStr" },
}

local user_level = {
    [0] = "Engineer", [1] = "Operator", [2] = "Supervisor", [3] = "Factory",
}

local term_code = {
    [0] = "user", [1] = "timeout", [2] = "net error", [3] = "remote",
}

-- Menu styles (spec 12.7). The kind is the low nibble; the rest are flags.
local style_kind = {
    [0] = "List", [1] = "Number", [2] = "EditString", [3] = "Checkbox",
    [4] = "Display", [5] = "Button", [6] = "VGraph", [7] = "HGraph",
    [8] = "VLevel", [9] = "HLevel", [10] = "Tiled", [11] = "Partial",
}

local mode_bits = {
    { 0x0001, "VALUE" }, { 0x0002, "STRING" }, { 0x0004, "DATA" },
    { 0x0008, "MATCHID" }, { 0x0010, "PRESET" },
}

local status_bits = {
    { 0x0002, "online" }, { 0x0004, "multi-level" },
    { 0x0008, "present" }, { 0x0020, "local" },
}

-- File-service errno values (RC3FILE.H).
local file_error = {
    [0] = "ok", [2] = "no such file", [13] = "access denied",
    [17] = "exists", [22] = "invalid", [24] = "too many open",
    [28] = "no space", [129] = "bad file type",
}

-- The routing interface's fixed root (Full Control Command Set). Everything
-- above 119 is worked out from what these publish, so it cannot be named from
-- a single frame — but these can, and they are what a client reads first.
local router_command = {
    [100] = "CMD_INTERFACE_VERSION", [101] = "CMD_ROUTER_NAME",
    [102] = "CMD_NUM_MATRICES",      [103] = "CMD_MATRIX_BASE",
    [104] = "CMD_MATRIX_STEP",       [105] = "CMD_NUM_CATEGORIES",
    [106] = "CMD_CATEGORY_BASE",     [107] = "CMD_CATEGORY_STEP",
    [108] = "CMD_ASSOC_MAKE_ROUTE",  [109] = "CMD_NUM_TRACK_TEMPLATES",
    [110] = "CMD_GET_TRACK_TEMPLATE", [111] = "CMD_NUM_AUDIO_GROUPS",
    [112] = "CMD_GET_AUDIO_GROUP",   [113] = "CMD_NUM_SALVOS",
    [114] = "CMD_SALVO_NAMES_8_FILENAME", [115] = "CMD_SALVO_NAMES_32_FILENAME",
    [116] = "CMD_FIRE_SALVO",        [117] = "CMD_NUM_DEVICES",
    [118] = "CMD_DEVICE_NAMES_FILENAME", [119] = "CMD_GET_AHP_NODE",
}

-------------------------------------------------------------------------------
-- Protocol and fields
-------------------------------------------------------------------------------

local p_rc = Proto("dhs_snell_rollcall", "Snell RollCall")

local f = {
    -- Transmission header.
    tx_flags  = ProtoField.uint16("dhs_snell_rollcall.tx_flags", "TX flags", base.DEC),
    tx_length = ProtoField.uint16("dhs_snell_rollcall.tx_length", "TX length", base.DEC),

    -- Addresses.
    dst       = ProtoField.string("dhs_snell_rollcall.dst", "Destination"),
    dst_net   = ProtoField.uint16("dhs_snell_rollcall.dst.net", "Net", base.HEX),
    dst_unit  = ProtoField.uint8("dhs_snell_rollcall.dst.unit", "Unit", base.HEX),
    dst_port  = ProtoField.uint8("dhs_snell_rollcall.dst.port", "Port", base.HEX),
    dst_index = ProtoField.uint16("dhs_snell_rollcall.dst.index", "Session index", base.DEC),

    src       = ProtoField.string("dhs_snell_rollcall.src", "Source"),
    src_net   = ProtoField.uint16("dhs_snell_rollcall.src.net", "Net", base.HEX),
    src_unit  = ProtoField.uint8("dhs_snell_rollcall.src.unit", "Unit", base.HEX),
    src_port  = ProtoField.uint8("dhs_snell_rollcall.src.port", "Port", base.HEX),
    src_index = ProtoField.uint16("dhs_snell_rollcall.src.index", "Session index", base.DEC),

    -- RollCall header.
    length    = ProtoField.uint16("dhs_snell_rollcall.length", "RollCall length", base.DEC),
    ptype     = ProtoField.uint8("dhs_snell_rollcall.type", "Type", base.DEC),
    flags     = ProtoField.uint8("dhs_snell_rollcall.flags", "Flags", base.HEX),
    back      = ProtoField.bool("dhs_snell_rollcall.flags.back_channel", "Back channel", 8, nil, 0x80),
    wide      = ProtoField.bool("dhs_snell_rollcall.flags.wide_area", "Wide area", 8, nil, 0x40),

    payload   = ProtoField.bytes("dhs_snell_rollcall.payload", "Payload"),

    -- Session.
    services  = ProtoField.uint16("dhs_snell_rollcall.services", "Services", base.HEX),
    level     = ProtoField.uint16("dhs_snell_rollcall.user_level", "User level", base.DEC),
    term_code = ProtoField.uint16("dhs_snell_rollcall.term_code", "Termination", base.DEC),
    reason    = ProtoField.string("dhs_snell_rollcall.reason", "Reason"),
    wait_ms   = ProtoField.uint32("dhs_snell_rollcall.wait_ms", "Wait (ms)", base.DEC),

    -- Identity.
    name      = ProtoField.string("dhs_snell_rollcall.name", "Name"),
    type_id   = ProtoField.uint16("dhs_snell_rollcall.type_id", "Unit type", base.DEC),
    version   = ProtoField.string("dhs_snell_rollcall.version", "Version"),
    status    = ProtoField.uint16("dhs_snell_rollcall.status", "Status", base.HEX),
    protocol  = ProtoField.uint16("dhs_snell_rollcall.protocol_version", "Protocol version", base.DEC),

    -- Menus and values.
    menu_index = ProtoField.uint32("dhs_snell_rollcall.menu_index", "Menu index", base.DEC),
    menu_count = ProtoField.uint32("dhs_snell_rollcall.menu_count", "Menu count", base.DEC),
    command    = ProtoField.uint32("dhs_snell_rollcall.command", "Command", base.DEC),
    style      = ProtoField.uint16("dhs_snell_rollcall.style", "Style", base.HEX),
    min_range  = ProtoField.int32("dhs_snell_rollcall.min_range", "Minimum", base.DEC),
    max_range  = ProtoField.int32("dhs_snell_rollcall.max_range", "Maximum", base.DEC),
    step       = ProtoField.uint32("dhs_snell_rollcall.step", "Step", base.DEC),
    divisor    = ProtoField.uint16("dhs_snell_rollcall.divisor", "Divisor", base.DEC),
    text       = ProtoField.string("dhs_snell_rollcall.text", "Text"),
    param      = ProtoField.string("dhs_snell_rollcall.param", "Format"),
    mode       = ProtoField.uint16("dhs_snell_rollcall.mode", "Mode", base.HEX),
    value      = ProtoField.int32("dhs_snell_rollcall.value", "Value", base.DEC),
    match_id   = ProtoField.uint16("dhs_snell_rollcall.match_id", "Match unit type", base.DEC),

    -- Display.
    disp_line  = ProtoField.int16("dhs_snell_rollcall.display_line", "Display line", base.DEC),

    -- Files.
    src_handle  = ProtoField.int16("dhs_snell_rollcall.file.src_handle", "Client handle", base.DEC),
    file_handle = ProtoField.int16("dhs_snell_rollcall.file.handle", "Server handle", base.DEC),
    file_offset = ProtoField.int32("dhs_snell_rollcall.file.offset", "Offset", base.DEC),
    file_extra  = ProtoField.int16("dhs_snell_rollcall.file.extra", "Extra", base.DEC),
    file_path   = ProtoField.string("dhs_snell_rollcall.file.path", "Path"),
    file_error  = ProtoField.int16("dhs_snell_rollcall.file.error", "Error", base.DEC),
    file_data   = ProtoField.bytes("dhs_snell_rollcall.file.data", "Data"),
    file_time   = ProtoField.uint32("dhs_snell_rollcall.file.time", "Modified", base.DEC),
    file_attrib = ProtoField.uint16("dhs_snell_rollcall.file.attrib", "Attributes", base.HEX),
    file_length = ProtoField.int32("dhs_snell_rollcall.file.length", "Length", base.DEC),

    -- Block transfers.
    block_type  = ProtoField.uint8("dhs_snell_rollcall.block.type", "Block of", base.DEC),
    block_count = ProtoField.uint16("dhs_snell_rollcall.block.count", "Items", base.DEC),
    block_max   = ProtoField.uint16("dhs_snell_rollcall.block.max_size", "Max item size", base.DEC),
    next_index  = ProtoField.uint16("dhs_snell_rollcall.next.index", "Item", base.DEC),

    -- Back channel.
    bkchn_state = ProtoField.uint8("dhs_snell_rollcall.back_channel_state", "Back channel", base.DEC),

    -- Data Transfer Params, which is how the routing interface carries
    -- anything that is not a plain number.
    dtp_count = ProtoField.uint8("dhs_snell_rollcall.dtp.count", "Items", base.DEC),
    dtp_type  = ProtoField.uint8("dhs_snell_rollcall.dtp.type", "Item type", base.DEC),
    dtp_uint  = ProtoField.uint32("dhs_snell_rollcall.dtp.uint", "uint", base.DEC),
    dtp_str   = ProtoField.string("dhs_snell_rollcall.dtp.string", "string"),
    dtp_bool  = ProtoField.bool("dhs_snell_rollcall.dtp.bool", "bool"),

    -- The router's own shapes, decoded out of the params above.
    pin        = ProtoField.string("dhs_snell_rollcall.source_pin", "Source pin"),
    pin_matrix = ProtoField.uint8("dhs_snell_rollcall.source_pin.matrix", "Matrix", base.DEC),
    pin_level  = ProtoField.uint8("dhs_snell_rollcall.source_pin.level", "Level", base.DEC),
    pin_source = ProtoField.uint16("dhs_snell_rollcall.source_pin.source", "Source", base.DEC),
    route_result = ProtoField.uint32("dhs_snell_rollcall.route_result", "Route result", base.DEC),
    router_cmd = ProtoField.string("dhs_snell_rollcall.router_command", "Routing command"),
}

p_rc.fields = {}
for _, field in pairs(f) do
    table.insert(p_rc.fields, field)
end

local ef_bad_tx_flags = ProtoExpert.new("dhs_snell_rollcall.bad_tx_flags",
    "Transmission header flags are not mode 3", expert.group.MALFORMED, expert.severity.ERROR)
local ef_length_mismatch = ProtoExpert.new("dhs_snell_rollcall.length_mismatch",
    "The two length fields disagree", expert.group.MALFORMED, expert.severity.ERROR)
local ef_unknown_type = ProtoExpert.new("dhs_snell_rollcall.unknown_type",
    "Packet type is not one the specification defines", expert.group.UNDECODED, expert.severity.WARN)
local ef_short_payload = ProtoExpert.new("dhs_snell_rollcall.short_payload",
    "Payload is shorter than the structure it carries", expert.group.MALFORMED, expert.severity.WARN)
local ef_bad_dtp = ProtoExpert.new("dhs_snell_rollcall.bad_dtp",
    "Data Transfer Params are malformed", expert.group.MALFORMED, expert.severity.WARN)

p_rc.experts = {
    ef_bad_tx_flags, ef_length_mismatch, ef_unknown_type, ef_short_payload, ef_bad_dtp,
}

-------------------------------------------------------------------------------
-- Helpers
-------------------------------------------------------------------------------

local function type_name(t)
    return packet_type[t] or string.format("UNKNOWN(%d)", t)
end

-- index_name spells the unconnected index rather than printing 255, because
-- that number is a state and not an identifier.
local function index_name(idx)
    if idx == INDEX_UNKNOWN then
        return "none"
    end
    return tostring(idx)
end

local function address_string(tvb, off)
    local net = tvb(off, 2):uint()
    local unit = tvb(off + 2, 1):uint()
    local port = tvb(off + 3, 1):uint()
    local index = tvb(off + 4, 2):uint()
    return string.format("%04X-%02X-%02X:%s", net, unit, port, index_name(index))
end

local function bits_string(value, bits)
    local out = {}
    for _, b in ipairs(bits) do
        if bit.band(value, b[1]) ~= 0 then
            table.insert(out, b[2])
        end
    end
    if #out == 0 then
        return "-"
    end
    return table.concat(out, "|")
end

local function style_string(v)
    local kind = style_kind[bit.band(v, 0x0F)] or string.format("kind%d", bit.band(v, 0x0F))
    local flags = {}
    if bit.band(v, 0x0010) ~= 0 then table.insert(flags, "hidden") end
    if bit.band(v, 0x0020) ~= 0 then table.insert(flags, "disabled") end
    if bit.band(v, 0x0040) ~= 0 then table.insert(flags, "cacheable") end
    if #flags == 0 then
        return kind
    end
    return kind .. "+" .. table.concat(flags, "+")
end

-- cstring reads a NUL-terminated string, and accepts one that runs to the end
-- of the buffer without a terminator: the vendor emits that when a value
-- exactly fills its field.
local function cstring(tvb, off)
    local len = tvb:len()
    if off >= len then
        return "", 0
    end
    for i = off, len - 1 do
        if tvb(i, 1):uint() == 0 then
            if i == off then
                return "", 1
            end
            return tvb(off, i - off):string(), i - off + 1
        end
    end
    return tvb(off, len - off):string(), len - off
end

-- fixed_string reads a fixed-width field up to its first NUL. Bytes after the
-- terminator are undefined and real devices leave rubbish there, so they are
-- never shown.
local function fixed_string(tvb, off, width)
    local avail = tvb:len() - off
    if avail <= 0 then
        return ""
    end
    if width > avail then
        width = avail
    end
    for i = 0, width - 1 do
        if tvb(off + i, 1):uint() == 0 then
            if i == 0 then
                return ""
            end
            return tvb(off, i):string()
        end
    end
    return tvb(off, width):string()
end

local function pin_string(v)
    local matrix = bit.rshift(v, 24)
    local level = bit.band(bit.rshift(v, 16), 0xFF)
    local source = bit.band(v, 0xFFFF)
    return string.format("m%d/l%d/s%d", matrix, level, source), matrix, level, source
end

local function router_command_name(cmd)
    if router_command[cmd] then
        return router_command[cmd]
    end
    if cmd >= 120 then
        -- Above the root table the numbers are allocated by the controller, so
        -- a single frame cannot say which entity one belongs to.
        return "routing interface"
    end
    return nil
end

-------------------------------------------------------------------------------
-- Data Transfer Params (Full Control Command Set §5.1)
--
-- A length-prefixed list of typed items, used wherever the routing interface
-- has to carry more than one number: a filename with its checksum, a source
-- pin with the result of setting it.
-------------------------------------------------------------------------------

-- varint reads the little-endian 7-bit-continuation integer the format uses.
local function dtp_varint(tvb, off)
    local value, shift, used = 0, 0, 0
    while off + used < tvb:len() do
        local b = tvb(off + used, 1):uint()
        used = used + 1
        value = value + bit.lshift(bit.band(b, 0x7F), shift)
        shift = shift + 7
        if bit.band(b, 0x80) == 0 then
            return value, used
        end
        if shift > 28 then
            return nil, used
        end
    end
    return nil, used
end

-- dtp_dissect walks the params and returns a one-line summary for the Info
-- column, which is where they earn their keep.
local function dtp_dissect(tvb, tree, pinfo)
    if tvb:len() < 1 then
        return nil
    end
    local subtree = tree:add(p_rc, tvb(), "Data Transfer Params")
    local count = bit.band(tvb(0, 1):uint(), 0x7F)
    subtree:add(f.dtp_count, tvb(0, 1), count)

    local off = 1
    local parts = {}

    for _ = 1, count do
        if off >= tvb:len() then
            subtree:add_proto_expert_info(ef_bad_dtp)
            break
        end
        local itype = tvb(off, 1):uint()
        local start = off
        off = off + 1

        if itype == 5 or itype == 1 then
            -- uint, and the uint-array's first form: a varint value.
            local v, used = dtp_varint(tvb, off)
            if v == nil then
                subtree:add_proto_expert_info(ef_bad_dtp)
                break
            end
            off = off + used
            local item = subtree:add(f.dtp_uint, tvb(start, off - start), v)
            item:set_text(string.format("uint: %d (0x%08X)", v, v))
            table.insert(parts, tostring(v))
        elseif itype == 6 then
            -- string: a length then the bytes.
            local n, used = dtp_varint(tvb, off)
            if n == nil or off + used + n > tvb:len() then
                subtree:add_proto_expert_info(ef_bad_dtp)
                break
            end
            off = off + used
            local s = tvb(off, n):string()
            subtree:add(f.dtp_str, tvb(start, off + n - start), s)
            off = off + n
            table.insert(parts, string.format("%q", s))
        elseif itype == 3 or itype == 4 then
            subtree:add(f.dtp_bool, tvb(start, 1), itype == 4)
            table.insert(parts, itype == 4 and "true" or "false")
        elseif itype == 2 then
            -- bitmap: a length in bits, then the bytes holding them.
            local n, used = dtp_varint(tvb, off)
            if n == nil then
                subtree:add_proto_expert_info(ef_bad_dtp)
                break
            end
            off = off + used
            local bytes = math.floor((n + 7) / 8)
            if off + bytes > tvb:len() then
                subtree:add_proto_expert_info(ef_bad_dtp)
                break
            end
            subtree:add(p_rc, tvb(start, off + bytes - start),
                string.format("bitmap: %d bits", n))
            off = off + bytes
            table.insert(parts, string.format("bitmap(%d)", n))
        else
            subtree:add_proto_expert_info(ef_bad_dtp)
            break
        end
    end

    if #parts == 0 then
        return nil
    end
    return table.concat(parts, " ")
end

-------------------------------------------------------------------------------
-- Payload dissectors
--
-- Each returns the text to append to the Info column, which is the part that
-- makes two frames of the same type tell themselves apart.
-------------------------------------------------------------------------------

local function need(tvb, n, tree)
    if tvb:len() < n then
        tree:add_proto_expert_info(ef_short_payload)
        return false
    end
    return true
end

-- ID_STR (spec 11.3.2): what a unit says it is.
local function dissect_id(tvb, tree, off)
    if tvb:len() < off + 28 then
        return nil
    end
    local services = tvb(off, 2):uint()
    tree:add(f.services, tvb(off, 2)):append_text(" (" .. bits_string(services, service_bits) .. ")")
    tree:add(f.type_id, tvb(off + 2, 2))

    -- VERSION_STR is four bytes, not two words: major, minor, an alpha
    -- character and the command set. The command set is the half that matters,
    -- because a unit's type plus its command set is what identifies a model.
    local major = tvb(off + 4, 1):uint()
    local minor = tvb(off + 5, 1):uint()
    local alpha = tvb(off + 6, 1):uint()
    local cmdset = tvb(off + 7, 1):uint()
    if alpha < 0x20 or alpha > 0x7E then
        alpha = 0x20
    end
    tree:add(f.version, tvb(off + 4, 4),
        string.format("%d.%d%s cs%d", major, minor,
            alpha == 0x20 and "" or string.char(alpha), cmdset))

    local name = fixed_string(tvb, off + 8, 20)
    tree:add(f.name, tvb(off + 8, math.min(20, tvb:len() - off - 8)), name)
    return name, services
end

local function dissect_call(tvb, tree)
    if not need(tvb, 4, tree) then return nil end
    local services = tvb(0, 2):uint()
    tree:add(f.services, tvb(0, 2)):append_text(" (" .. bits_string(services, service_bits) .. ")")
    local level = tvb(2, 2):uint()
    tree:add(f.level, tvb(2, 2)):append_text(" (" .. (user_level[level] or "?") .. ")")

    -- The caller is a whole DEVICEINFO_STR, so its identity starts past the
    -- protocol version and the address.
    local caller = dissect_id(tvb, tree, 12)
    local generation = bit.band(services, 0x8000) ~= 0 and "32-bit" or "16-bit"
    return string.format("%s %s level=%s%s", bits_string(services, service_bits), generation,
        user_level[level] or level, caller and (" from " .. caller) or "")
end

local function dissect_term(tvb, tree)
    if not need(tvb, 2, tree) then return nil end
    local code = tvb(0, 2):uint()
    tree:add(f.term_code, tvb(0, 2)):append_text(" (" .. (term_code[code] or "?") .. ")")
    local reason = ""
    if tvb:len() > 2 then
        reason = cstring(tvb, 2)
        if reason ~= "" then
            tree:add(f.reason, tvb(2, tvb:len() - 2), reason)
        end
    end
    if reason ~= "" then
        return string.format("%s: %s", term_code[code] or code, reason)
    end
    return term_code[code] or tostring(code)
end

-- STATUS_STR (spec 11.3.1): which services are busy, then the unit's state.
local function dissect_status(tvb, tree, off)
    off = off or 0
    if tvb:len() < off + 4 then
        tree:add_proto_expert_info(ef_short_payload)
        return nil
    end
    local busy = tvb(off, 2):uint()
    if busy ~= 0 then
        tree:add(f.services, tvb(off, 2)):set_text(
            "Busy services: " .. bits_string(busy, service_bits))
    end
    local status = tvb(off + 2, 2):uint()
    tree:add(f.status, tvb(off + 2, 2)):append_text(" (" .. bits_string(status, status_bits) .. ")")
    return bits_string(status, status_bits)
end

local function dissect_devinfo(tvb, tree)
    if not need(tvb, 10, tree) then return nil end
    tree:add(f.protocol, tvb(0, 2))
    local addr = address_string(tvb, 2)
    tree:add(f.dst, tvb(2, 6), addr):set_text("Address: " .. addr)
    local name = dissect_id(tvb, tree, 8)
    local status = nil
    if tvb:len() >= 40 then
        status = dissect_status(tvb, tree, 36)
    end
    return string.format("%s %q%s", addr, name or "",
        status and (" [" .. status .. "]") or "")
end

-- FUNC_STR (spec 11.4.1): one menu line in the 16-bit generation.
local function dissect_func(tvb, tree)
    if not need(tvb, 18, tree) then return nil end
    local index = tvb(0, 2):uint()
    local style = tvb(2, 2):uint()
    local command = tvb(4, 2):uint()
    tree:add(f.menu_index, tvb(0, 2), index)
    tree:add(f.style, tvb(2, 2)):append_text(" (" .. style_string(style) .. ")")
    tree:add(f.command, tvb(4, 2), command)
    tree:add(f.min_range, tvb(6, 4))
    tree:add(f.max_range, tvb(10, 4))
    tree:add(f.step, tvb(14, 2), tvb(14, 2):uint())
    tree:add(f.divisor, tvb(16, 2))
    local text = fixed_string(tvb, 18, 20)
    if text ~= "" then
        tree:add(f.text, tvb(18, math.min(20, tvb:len() - 18)), text)
    end
    return string.format("line %d cmd=%d %s %q", index, command, style_string(style), text)
end

-- MENUITEM_STR: one menu line in the 32-bit generation.
-- MENUITEM_STR: one menu line in the 32-bit generation. Twenty-two bytes of
-- fixed fields, then the label and the format as NUL-terminated strings.
local function dissect_menu_item(tvb, tree)
    if not need(tvb, 22, tree) then return nil end
    local index = tvb(0, 4):uint()
    local style = tvb(4, 2):uint()
    local command = tvb(6, 4):uint()
    tree:add(f.menu_index, tvb(0, 4), index)
    tree:add(f.style, tvb(4, 2)):append_text(" (" .. style_string(style) .. ")")
    tree:add(f.command, tvb(6, 4), command)
    tree:add(f.min_range, tvb(10, 4))
    tree:add(f.max_range, tvb(14, 4))
    tree:add(f.step, tvb(18, 2), tvb(18, 2):uint())
    tree:add(f.divisor, tvb(20, 2))

    local text, used = "", 0
    if tvb:len() > 22 then
        text, used = cstring(tvb, 22)
        if text ~= "" then
            tree:add(f.text, tvb(22, used), text)
        end
    end
    if tvb:len() > 22 + used then
        local param = cstring(tvb, 22 + used)
        if param ~= "" then
            tree:add(f.param, tvb(22 + used, tvb:len() - 22 - used), param)
        end
    end
    return string.format("line %d cmd=%d %s %q", index, command, style_string(style), text)
end

local function dissect_menu_req(tvb, tree)
    if not need(tvb, 4, tree) then return nil end
    local index = tvb(0, 4):uint()
    tree:add(f.menu_index, tvb(0, 4), index)
    return string.format("from %d", index)
end

local function dissect_menu_size(tvb, tree)
    if not need(tvb, 8, tree) then return nil end
    local index, count = tvb(0, 4):uint(), tvb(4, 4):uint()
    tree:add(f.menu_index, tvb(0, 4), index)
    tree:add(f.menu_count, tvb(4, 4), count)
    return string.format("%d lines from %d", count, index)
end

-- The value structures, and the routing arithmetic that rides on them.
local function annotate_command(tree, tvb, range, command)
    local name = router_command_name(command)
    if name then
        tree:add(f.router_cmd, range, name)
    end
    return name
end

local function dissect_value_tail(tvb, tree, off, mode, value, command, info)
    if bit.band(mode, 0x0002) ~= 0 and tvb:len() > off then
        local text, used = cstring(tvb, off)
        tree:add(f.text, tvb(off, math.max(used, 1)), text)
        off = off + used
        table.insert(info, string.format("%q", text))
    end
    if bit.band(mode, 0x0004) ~= 0 and tvb:len() > off then
        local n = math.min(value, tvb:len() - off)
        if n > 0 then
            local data = tvb(off, n)
            tree:add(f.file_data, data):set_text(string.format("Data (%d bytes)", n))

            -- Data on the routing interface is Data Transfer Params, and a
            -- crosspoint read is the commonest frame on a busy router.
            local summary = dtp_dissect(data:tvb(), tree, nil)
            if summary then
                table.insert(info, summary)
            end
            if command and command >= 120 and n >= 2 then
                local sub = data:tvb()
                if sub:len() >= 3 and sub(0, 1):uint() >= 1 and sub(1, 1):uint() == 5 then
                    local v = dtp_varint(sub, 2)
                    if v then
                        local pin = pin_string(v)
                        tree:add(f.pin, data, pin)
                        table.insert(info, "pin=" .. pin)
                    end
                end
            end
        end
    end
    return off
end

-- FUNCSTATUS_STR (spec 11.5.2): a value in the 16-bit generation.
local function dissect_func_status(tvb, tree)
    if not need(tvb, 8, tree) then return nil end
    local command = tvb(0, 2):uint()
    local mode = tvb(2, 2):uint()
    local value = tvb(4, 4):int()
    tree:add(f.command, tvb(0, 2), command)
    annotate_command(tree, tvb, tvb(0, 2), command)
    tree:add(f.mode, tvb(2, 2)):append_text(" (" .. bits_string(mode, mode_bits) .. ")")
    tree:add(f.value, tvb(4, 4), value)

    local info = { string.format("cmd=%d", command) }
    if bit.band(mode, 0x0001) ~= 0 then
        table.insert(info, tostring(value))
    end
    dissect_value_tail(tvb, tree, 8, mode, value, command, info)
    return table.concat(info, " ")
end

-- VALUE_STR: a value in the 32-bit generation.
local function dissect_value(tvb, tree)
    if not need(tvb, 12, tree) then return nil end
    local command = tvb(0, 4):uint()
    local match = tvb(4, 2):uint()
    local mode = tvb(6, 2):uint()
    local value = tvb(8, 4):int()
    tree:add(f.command, tvb(0, 4), command)
    annotate_command(tree, tvb, tvb(0, 4), command)
    tree:add(f.match_id, tvb(4, 2), match)
    tree:add(f.mode, tvb(6, 2)):append_text(" (" .. bits_string(mode, mode_bits) .. ")")
    tree:add(f.value, tvb(8, 4), value)

    local info = { string.format("cmd=%d", command) }
    if bit.band(mode, 0x0001) ~= 0 then
        table.insert(info, tostring(value))
    end
    dissect_value_tail(tvb, tree, 12, mode, value, command, info)
    return table.concat(info, " ")
end

local function dissect_get_fstat(tvb, tree)
    if not need(tvb, 2, tree) then return nil end
    local command = tvb(0, 2):uint()
    tree:add(f.command, tvb(0, 2), command)
    annotate_command(tree, tvb, tvb(0, 2), command)
    return string.format("cmd=%d", command)
end

local function dissect_get_value(tvb, tree)
    if not need(tvb, 4, tree) then return nil end
    local command = tvb(0, 4):uint()
    tree:add(f.command, tvb(0, 4), command)
    local named = annotate_command(tree, tvb, tvb(0, 4), command)
    if named and router_command[command] then
        return string.format("cmd=%d %s", command, named)
    end
    return string.format("cmd=%d", command)
end

local function dissect_disp(tvb, tree)
    if not need(tvb, 2, tree) then return nil end
    local line = tvb(0, 2):int()
    tree:add(f.disp_line, tvb(0, 2), line)
    local text = ""
    if tvb:len() > 2 then
        text = fixed_string(tvb, 2, 20)
        tree:add(f.text, tvb(2, math.min(20, tvb:len() - 2)), text)
    end
    return string.format("line %d %q", line, text)
end

-- BLOCKHEADER_STR: what a multi-packet transfer is about to send. The spare
-- byte after the type is why the count is at offset two and not one.
local function dissect_block_header(tvb, tree)
    if not need(tvb, 6, tree) then return nil end
    local of = tvb(0, 1):uint()
    local count = tvb(2, 2):uint()
    tree:add(f.block_type, tvb(0, 1), of):append_text(" (" .. type_name(of) .. ")")
    tree:add(f.block_count, tvb(2, 2), count)
    tree:add(f.block_max, tvb(4, 2))
    return string.format("%d items of %s", count, type_name(of))
end

local function dissect_get_next(tvb, tree)
    if not need(tvb, 3, tree) then return nil end
    local index = tvb(0, 2):uint()
    local of = tvb(2, 1):uint()
    tree:add(f.next_index, tvb(0, 2), index)
    tree:add(f.block_type, tvb(2, 1), of):append_text(" (" .. type_name(of) .. ")")
    return string.format("item %d of %s", index, type_name(of))
end

-- FILE_STR: the structure every file operation carries. Offset and Extra
-- change meaning between a request and its own reply, which is the mistake
-- this service punishes silently, so each is labelled for the message it is in.
local function dissect_file(tvb, tree, ptype)
    if not need(tvb, 10, tree) then return nil end
    local src = tvb(0, 2):int()
    local handle = tvb(2, 2):int()
    local offset = tvb(4, 4):int()
    local extra = tvb(8, 2):int()

    tree:add(f.src_handle, tvb(0, 2), src)
    tree:add(f.file_handle, tvb(2, 2), handle)

    local info
    if ptype == 51 then -- FILEOPEN
        tree:add(f.file_extra, tvb(8, 2), extra):set_text(
            string.format("Open flags: 0x%04X", bit.band(extra, 0xFFFF)))
        local path = cstring(tvb, 10)
        if path ~= "" then
            tree:add(f.file_path, tvb(10, tvb:len() - 10), path)
        end
        info = string.format("open %q flags=0x%04X", path, bit.band(extra, 0xFFFF))
    elseif ptype == 52 then -- RETFILEOPEN
        tree:add(f.file_offset, tvb(4, 4), offset):set_text("Block size: " .. offset)
        tree:add(f.file_error, tvb(8, 2), extra):append_text(
            " (" .. (file_error[extra] or "?") .. ")")
        if tvb:len() >= 20 then
            tree:add(f.file_time, tvb(10, 4))
            tree:add(f.file_attrib, tvb(14, 2))
            tree:add(f.file_length, tvb(16, 4))
            info = string.format("handle=%d block=%d len=%d %s",
                handle, offset, tvb(16, 4):int(), file_error[extra] or extra)
        else
            info = string.format("handle=%d block=%d %s", handle, offset, file_error[extra] or extra)
        end
    elseif ptype == 55 then -- FILEREAD
        tree:add(f.file_offset, tvb(4, 4), offset):set_text("Read from: " .. offset)
        tree:add(f.file_extra, tvb(8, 2), extra):set_text("Bytes wanted: " .. extra)
        info = string.format("read handle=%d at %d want %d", handle, offset, extra)
    elseif ptype == 56 then -- RETFILEREAD
        tree:add(f.file_offset, tvb(4, 4), offset):set_text("Bytes read: " .. offset)
        tree:add(f.file_error, tvb(8, 2), extra):append_text(
            " (" .. (file_error[extra] or "?") .. ")")
        if tvb:len() > 10 then
            tree:add(f.file_data, tvb(10, tvb:len() - 10))
        end
        info = string.format("read %d bytes handle=%d", offset, handle)
    elseif ptype == 57 then -- FILEWRITE
        tree:add(f.file_offset, tvb(4, 4), offset):set_text("Write at: " .. offset)
        tree:add(f.file_extra, tvb(8, 2), extra):set_text("Bytes: " .. extra)
        if tvb:len() > 10 then
            tree:add(f.file_data, tvb(10, tvb:len() - 10))
        end
        info = string.format("write %d bytes handle=%d at %d", extra, handle, offset)
    elseif ptype == 42 or ptype == 46 or ptype == 64 then -- FILEDIR / DELETE / MKDIR
        local path = cstring(tvb, 10)
        if path ~= "" then
            tree:add(f.file_path, tvb(10, tvb:len() - 10), path)
        end
        info = string.format("%q", path)
    else
        tree:add(f.file_offset, tvb(4, 4), offset)
        tree:add(f.file_extra, tvb(8, 2), extra)
        info = string.format("handle=%d", handle)
    end
    return info
end

-- MODULE_FILEINFO_STR: one directory entry. The name is whatever follows the
-- header, not the fixed field the structure declares — the vendor sends only
-- the bytes it uses, so an entry for "." arrives in twelve bytes.
local function dissect_dir_entry(tvb, tree)
    if not need(tvb, 11, tree) then return nil end
    tree:add(f.file_time, tvb(0, 4))
    local attrib = tvb(4, 2):uint()
    tree:add(f.file_attrib, tvb(4, 2))
    tree:add(f.file_length, tvb(6, 4))
    local name = cstring(tvb, 10)
    tree:add(f.file_path, tvb(10, tvb:len() - 10), name)
    local kind = bit.band(attrib, 0x0010) ~= 0 and "dir" or "file"
    return string.format("%s %q %d bytes", kind, name, tvb(6, 4):int())
end

local function dissect_back_channel(tvb, tree)
    if not need(tvb, 1, tree) then return nil end
    local state = tvb(0, 1):uint()
    local names = { [0] = "close", [1] = "open and flush", [2] = "open, future only" }
    tree:add(f.bkchn_state, tvb(0, 1), state):append_text(" (" .. (names[state] or "?") .. ")")
    return names[state] or tostring(state)
end

local function dissect_wait(tvb, tree)
    if not need(tvb, 4, tree) then return nil end
    local ms = tvb(0, 4):uint()
    tree:add(f.wait_ms, tvb(0, 4), ms)
    return string.format("%d ms", ms)
end

local function dissect_nack(tvb, tree)
    if tvb:len() == 0 then
        return nil
    end
    local reason = cstring(tvb, 0)
    if reason == "" then
        return nil
    end
    tree:add(f.reason, tvb(0, tvb:len()), reason)
    return reason
end

-- payload_dissector picks the decoder for a type. Types with no payload, and
-- the ones whose payload is a single scalar the caller reads directly, are
-- absent on purpose rather than by omission.
local function dissect_payload(ptype, tvb, tree)
    if ptype == 2 then return dissect_call(tvb, tree) end
    if ptype == 3 then return dissect_term(tvb, tree) end
    if ptype == 0 or ptype == 15 then return dissect_nack(tvb, tree) end
    if ptype == 25 then return dissect_wait(tvb, tree) end
    if ptype == 5 then return dissect_status(tvb, tree, 0) end
    if ptype == 7 then local n = dissect_id(tvb, tree, 0); return n end
    if ptype == 20 or ptype == 33 then return dissect_devinfo(tvb, tree) end
    if ptype == 9 then return dissect_func(tvb, tree) end
    if ptype == 68 then return dissect_menu_item(tvb, tree) end
    if ptype == 65 or ptype == 67 then return dissect_menu_req(tvb, tree) end
    if ptype == 66 then return dissect_menu_size(tvb, tree) end
    if ptype == 11 then return dissect_get_fstat(tvb, tree) end
    if ptype == 12 or ptype == 16 then return dissect_func_status(tvb, tree) end
    if ptype == 69 then return dissect_get_value(tvb, tree) end
    if ptype == 70 or ptype == 71 then return dissect_value(tvb, tree) end
    if ptype == 10 then return dissect_disp(tvb, tree) end
    if ptype == 39 then return dissect_block_header(tvb, tree) end
    if ptype == 35 then return dissect_get_next(tvb, tree) end
    if ptype == 43 then return dissect_dir_entry(tvb, tree) end
    if ptype == 27 then return dissect_back_channel(tvb, tree) end
    if ptype == 42 or ptype == 46 or ptype == 51 or ptype == 52 or ptype == 53
        or ptype == 55 or ptype == 56 or ptype == 57 or ptype == 59 or ptype == 64 then
        return dissect_file(tvb, tree, ptype)
    end
    if ptype == 54 and tvb:len() >= 2 then
        local line = tvb(0, 2):int()
        tree:add(f.disp_line, tvb(0, 2), line)
        return string.format("line %d", line)
    end
    return nil
end

-------------------------------------------------------------------------------
-- Frame dissection
-------------------------------------------------------------------------------

local function dissect_message(tvb, pinfo, tree)
    local txlen = tvb(2, 2):uint()
    local total = TX_HEADER_SIZE + txlen

    local item = tree:add(p_rc, tvb(0, total), "Snell RollCall")
    local root = item

    local txflags = tvb(0, 2):uint()
    local flags_item = root:add(f.tx_flags, tvb(0, 2))
    if txflags ~= TX_FLAGS_MODE3 then
        flags_item:add_proto_expert_info(ef_bad_tx_flags)
    end
    root:add(f.tx_length, tvb(2, 2))

    local dst = address_string(tvb, 4)
    local src = address_string(tvb, 10)

    local dtree = root:add(f.dst, tvb(4, 6), dst)
    dtree:add(f.dst_net, tvb(4, 2))
    dtree:add(f.dst_unit, tvb(6, 1))
    dtree:add(f.dst_port, tvb(7, 1))
    dtree:add(f.dst_index, tvb(8, 2)):append_text(
        tvb(8, 2):uint() == INDEX_UNKNOWN and " (unconnected)" or "")

    local stree = root:add(f.src, tvb(10, 6), src)
    stree:add(f.src_net, tvb(10, 2))
    stree:add(f.src_unit, tvb(12, 1))
    stree:add(f.src_port, tvb(13, 1))
    stree:add(f.src_index, tvb(14, 2)):append_text(
        tvb(14, 2):uint() == INDEX_UNKNOWN and " (unconnected)" or "")

    local rlength = tvb(16, 2):uint()
    local len_item = root:add(f.length, tvb(16, 2))
    if rlength + MSG_HEADER_SIZE ~= txlen then
        len_item:add_proto_expert_info(ef_length_mismatch)
    end

    local ptype = tvb(18, 1):uint()
    local type_item = root:add(f.ptype, tvb(18, 1)):append_text(" (" .. type_name(ptype) .. ")")
    if packet_type[ptype] == nil then
        type_item:add_proto_expert_info(ef_unknown_type)
    end

    local pflags = tvb(19, 1):uint()
    local pf = root:add(f.flags, tvb(19, 1))
    pf:add(f.back, tvb(19, 1))
    pf:add(f.wide, tvb(19, 1))

    local payload_len = total - HEADER_SIZE
    local detail = nil
    if payload_len > 0 then
        local payload = tvb(HEADER_SIZE, payload_len)
        local ptree = root:add(f.payload, payload)
        detail = dissect_payload(ptype, payload:tvb(), ptree)
    end

    -- The Info column: what it is, who it is between, and what it says.
    local channel = bit.band(pflags, 0x80) ~= 0 and " [back]" or ""
    local line = string.format("%s%s %s → %s", type_name(ptype), channel, src, dst)
    if detail then
        line = line .. "  " .. detail
    end
    root:append_text(string.format(": %s%s", type_name(ptype), detail and (" " .. detail) or ""))

    return total, line
end

function p_rc.dissector(tvb, pinfo, tree)
    local offset = 0
    local lines = {}

    while offset < tvb:len() do
        -- Enough for the transmission header?
        if tvb:len() - offset < TX_HEADER_SIZE then
            pinfo.desegment_offset = offset
            pinfo.desegment_len = DESEGMENT_ONE_MORE_SEGMENT
            return offset
        end

        local txlen = tvb(offset + 2, 2):uint()
        if txlen < MSG_HEADER_SIZE + ROLL_HEADER_SIZE or txlen > SPEC_MAX_TX_LEN then
            -- Not a RollCall frame, or the stream has slipped. Leave it to
            -- whoever else wants it rather than inventing a decode.
            if offset == 0 then
                return 0
            end
            break
        end

        local total = TX_HEADER_SIZE + txlen
        if tvb:len() - offset < total then
            pinfo.desegment_offset = offset
            pinfo.desegment_len = total - (tvb:len() - offset)
            return tvb:len()
        end

        local used, line = dissect_message(tvb(offset, total):tvb(), pinfo, tree)
        table.insert(lines, line)
        offset = offset + used
    end

    if #lines > 0 then
        pinfo.cols.protocol = "RollCall"
        if #lines == 1 then
            pinfo.cols.info = lines[1]
        else
            pinfo.cols.info = string.format("%d messages: %s", #lines, table.concat(lines, " | "))
        end
    end
    return offset
end

-------------------------------------------------------------------------------
-- Registration
-------------------------------------------------------------------------------

p_rc.prefs.ports = Pref.range("TCP ports", default_ports,
    "TCP ports carrying RollCall over IPShare", 65535)

local registered_ports = ""

local function register_ports()
    local tcp = DissectorTable.get("tcp.port")
    if registered_ports ~= "" then
        tcp:remove(registered_ports, p_rc)
    end
    registered_ports = p_rc.prefs.ports
    tcp:add(registered_ports, p_rc)
end

function p_rc.init()
    register_ports()
end

function p_rc.prefs_changed()
    register_ports()
end
