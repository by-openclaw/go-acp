-------------------------------------------------------------------------------
--
-- Wireshark Lua Dissector: Ember+ (S101 framing + Glow BER)
--
-- Handles:
--   - S101 framing (BoF 0xFE, EoF 0xFF, escape 0xFD xor 0x20, CCITT16 CRC)
--   - S101 header (slot, msgType, command, version, flags, DTD, app-bytes)
--   - Keep-alive request / response frames
--   - BER payload walk (universal + application + context class tags, short +
--     long-form + indefinite length, end-of-contents sentinel)
--   - Glow application tag naming (Parameter, Node, Matrix, Function, ...)
--
-- Reference: Ember+ Protocol Specification v2.50 (Lawo, rev. 15, 2017-11-09),
--            assets/emberplus/Ember+ Documentation.pdf
--
-- Compatible with Wireshark 4.x
--
-- Default registered TCP ports: 9000, 9090, 9092 (TinyEmber+ / DHD / ember
-- providers). Change via "Decode As ..." if your provider uses a different
-- port.
--
-------------------------------------------------------------------------------

local S101_BOF = 0xFE
local S101_EOF = 0xFF
local S101_ESC = 0xFD

-- Default TCP ports where Ember+ providers listen. Multiple known vendor
-- defaults; adjust via Decode As if needed.
local DEFAULT_TCP_PORTS = { 9000, 9090, 9092 }

-- bit ops (Wireshark exposes `bit` as a module in Lua 5.2 builds).
local bxor = bit.bxor
local band = bit.band
local bor  = bit.bor
local rshift = bit.rshift
local lshift = bit.lshift

-------------------------------------------------------------------------------
-- Value-string tables
-------------------------------------------------------------------------------

local s101_msgtype_valstr = {
    [0x0E] = "EmBER / KeepAlive",
}

local s101_cmd_valstr = {
    [0x00] = "EmBER (Glow data)",
    [0x01] = "Keep-alive request",
    [0x02] = "Keep-alive response",
    [0x0E] = "Provider state",
}

local s101_flags_valstr = {
    [0x20] = "Empty",
    [0x40] = "Last multi-packet",
    [0x80] = "First multi-packet",
    [0xC0] = "Single packet",
}

local s101_dtd_valstr = {
    [0x01] = "Glow",
}

-- BER class (top 2 bits of the identifier octet).
local ber_class_valstr = {
    [0] = "Universal",
    [1] = "Application",
    [2] = "Context",
    [3] = "Private",
}

-- Universal tag names (spec subset used by Glow).
local ber_universal_valstr = {
    [0x02] = "INTEGER",
    [0x03] = "BIT STRING",
    [0x04] = "OCTET STRING",
    [0x05] = "NULL",
    [0x06] = "OBJECT IDENTIFIER",
    [0x09] = "REAL",
    [0x0A] = "ENUMERATED",
    [0x0C] = "UTF8String",
    [0x0D] = "RELATIVE-OID",
    [0x10] = "SEQUENCE",
    [0x11] = "SET",
    [0x13] = "PrintableString",
}

-- Glow APPLICATION tags (tags.go, Ember+ spec pp. 83–93).
local glow_app_valstr = {
    [0]  = "Root",
    [1]  = "Parameter",
    [2]  = "Command",
    [3]  = "Node",
    [4]  = "ElementCollection",
    [5]  = "StreamEntry",
    [6]  = "StreamCollection",
    [7]  = "StringIntegerPair",
    [8]  = "StringIntegerCollection",
    [9]  = "QualifiedParameter",
    [10] = "QualifiedNode",
    [11] = "RootElementCollection",
    [12] = "StreamDescription",
    [13] = "Matrix",
    [14] = "Target",
    [15] = "Source",
    [16] = "Connection",
    [17] = "QualifiedMatrix",
    [18] = "Label",
    [19] = "Function",
    [20] = "QualifiedFunction",
    [21] = "TupleItemDescription",
    [22] = "Invocation",
    [23] = "InvocationResult",
    [24] = "Template",
    [25] = "QualifiedTemplate",
}

-- Command number semantics (CommandType enum, spec p.31/86).
local glow_cmd_number_valstr = {
    [30] = "Subscribe",
    [31] = "Unsubscribe",
    [32] = "GetDirectory",
    [33] = "Invoke",
}

-- Context-tag naming for the well-known parent types. Each entry maps
-- a context tag number to a human-readable label. The walker picks the
-- correct table via the "scope" passed down recursively.
local scope_node_contents = {
    [0] = "identifier",
    [1] = "description",
    [2] = "isRoot",
    [3] = "isOnline",
    [4] = "schemaIdentifiers",
    [5] = "templateReference",
}

local scope_param_contents = {
    [0] = "identifier", [1] = "description", [2] = "value",
    [3] = "minimum",    [4] = "maximum",     [5] = "access",
    [6] = "format",     [7] = "enumeration", [8] = "factor",
    [9] = "isOnline",   [10] = "formula",    [11] = "step",
    [12] = "default",   [13] = "type",       [14] = "streamIdentifier",
    [15] = "enumMap",   [16] = "streamDescriptor",
    [17] = "schemaIdentifiers", [18] = "templateReference",
}

local scope_matrix_wrapper = {
    [0] = "number", [1] = "contents", [2] = "children",
    [3] = "targets", [4] = "sources", [5] = "connections",
}

local scope_matrix_contents = {
    [0] = "identifier", [1] = "description", [2] = "type",
    [3] = "addressingMode", [4] = "targetCount", [5] = "sourceCount",
    [6] = "maximumTotalConnects", [7] = "maximumConnectsPerTarget",
    [8] = "parametersLocation", [9] = "gainParameterNumber",
    [10] = "labels", [11] = "schemaIdentifiers", [12] = "templateReference",
}

local scope_connection = {
    [0] = "target", [1] = "sources", [2] = "operation", [3] = "disposition",
}

local scope_function_contents = {
    [0] = "identifier", [1] = "description",
    [2] = "arguments",  [3] = "result", [4] = "templateReference",
}

local scope_invocation = {
    [0] = "invocationID", [1] = "arguments",
}

local scope_invocation_result = {
    [0] = "invocationID", [1] = "success", [2] = "result",
}

local scope_command = {
    [0] = "number", [1] = "dirFieldMask", [2] = "invocation",
}

local scope_label = {
    [0] = "basePath", [1] = "description",
}

local scope_stream_entry = {
    [0] = "streamIdentifier", [1] = "streamValue",
}

local scope_tuple_item = {
    [0] = "type", [1] = "name",
}

local scope_signal = { [0] = "number" }

local scope_template = { [0] = "number/path", [1] = "element", [2] = "description" }

-- Map an APPLICATION tag to the context-tag name-map for its wrapper.
-- (Outer wrapper, e.g. Parameter[APP 1] has number/contents/children contexts.)
local wrapper_scope_by_app = {
    [1]  = { [0] = "number",   [1] = "contents", [2] = "children" }, -- Parameter
    [3]  = { [0] = "number",   [1] = "contents", [2] = "children" }, -- Node
    [9]  = { [0] = "path",     [1] = "contents", [2] = "children" }, -- QParameter
    [10] = { [0] = "path",     [1] = "contents", [2] = "children" }, -- QNode
    [19] = { [0] = "number",   [1] = "contents", [2] = "children" }, -- Function
    [20] = { [0] = "path",     [1] = "contents", [2] = "children" }, -- QFunction
    [13] = scope_matrix_wrapper,  -- Matrix
    [17] = scope_matrix_wrapper,  -- QMatrix (same context layout + path instead of number)
    [16] = scope_connection,       -- Connection
    [18] = scope_label,            -- Label
    [22] = scope_invocation,       -- Invocation
    [23] = scope_invocation_result,-- InvocationResult
    [2]  = scope_command,          -- Command
    [5]  = scope_stream_entry,     -- StreamEntry
    [14] = scope_signal,           -- Target
    [15] = scope_signal,           -- Source
    [21] = scope_tuple_item,       -- TupleItemDescription
    [24] = scope_template,         -- Template
    [25] = scope_template,         -- QualifiedTemplate
}

-- Inner SET scopes selected by the [1] contents context of each wrapper.
-- Keyed by the APP wrapper tag that produced the contents SET.
local contents_scope_by_app = {
    [1]  = scope_param_contents,
    [3]  = scope_node_contents,
    [9]  = scope_param_contents,
    [10] = scope_node_contents,
    [13] = scope_matrix_contents,
    [17] = scope_matrix_contents,
    [19] = scope_function_contents,
    [20] = scope_function_contents,
}

-------------------------------------------------------------------------------
-- Protocol declarations
-------------------------------------------------------------------------------

local s101_proto = Proto("emberplus",      "Ember+ (S101)")
local glow_proto = Proto("emberplus_glow", "Ember+ Glow BER")

-------------------------------------------------------------------------------
-- ProtoFields
-------------------------------------------------------------------------------

local s101_f = {
    bof      = ProtoField.uint8 ("emberplus.s101.bof",     "BoF",      base.HEX),
    eof      = ProtoField.uint8 ("emberplus.s101.eof",     "EoF",      base.HEX),
    slot     = ProtoField.uint8 ("emberplus.s101.slot",    "Slot",     base.DEC),
    msgtype  = ProtoField.uint8 ("emberplus.s101.msgtype", "Msg Type", base.HEX, s101_msgtype_valstr),
    command  = ProtoField.uint8 ("emberplus.s101.command", "Command",  base.HEX, s101_cmd_valstr),
    version  = ProtoField.uint8 ("emberplus.s101.version", "Version",  base.DEC),
    flags    = ProtoField.uint8 ("emberplus.s101.flags",   "Flags",    base.HEX, s101_flags_valstr),
    dtd      = ProtoField.uint8 ("emberplus.s101.dtd",     "DTD Type", base.HEX, s101_dtd_valstr),
    appblen  = ProtoField.uint8 ("emberplus.s101.applen",  "App Bytes Length", base.DEC),
    appbytes = ProtoField.bytes ("emberplus.s101.appbytes","App Bytes"),
    crc      = ProtoField.uint16("emberplus.s101.crc",     "CRC",      base.HEX),
    crc_status = ProtoField.string("emberplus.s101.crc_status", "CRC Status"),
    escaped  = ProtoField.bytes ("emberplus.s101.escaped", "Escaped Payload"),
    unescaped= ProtoField.bytes ("emberplus.s101.unescaped","Unescaped Content"),
}
s101_proto.fields = s101_f

-- All decoded BER values are exposed as strings to avoid ProtoField type
-- size mismatches (INTEGER is variable-width, not a neat int32/int64; REAL
-- uses its own BER sub-encoding). Raw bytes are still attached separately
-- for binary inspection.
local glow_f = {
    tag_class = ProtoField.uint8 ("emberplus.glow.class",    "Class",     base.DEC, ber_class_valstr),
    tag_cons  = ProtoField.bool  ("emberplus.glow.constructed","Constructed"),
    tag_num   = ProtoField.uint32("emberplus.glow.tag_num",  "Tag Number",base.DEC),
    tag_name  = ProtoField.string("emberplus.glow.tag_name", "Tag Name"),
    length    = ProtoField.uint32("emberplus.glow.length",   "Length",    base.DEC),
    length_indef = ProtoField.bool ("emberplus.glow.length_indef", "Indefinite Length"),
    value_int   = ProtoField.string("emberplus.glow.int",    "Value (int)"),
    value_bool  = ProtoField.string("emberplus.glow.bool",   "Value (bool)"),
    value_real  = ProtoField.string("emberplus.glow.real",   "Value (real)"),
    value_utf8  = ProtoField.string("emberplus.glow.utf8",   "Value (UTF-8)"),
    value_oid   = ProtoField.string("emberplus.glow.reloid", "Value (RELATIVE-OID)"),
    value_null  = ProtoField.string("emberplus.glow.null",   "Value (NULL)"),
    value_octets= ProtoField.bytes ("emberplus.glow.octets", "Value (octets)"),
    value_raw   = ProtoField.bytes ("emberplus.glow.raw",    "Value (raw)"),
}
glow_proto.fields = glow_f

-------------------------------------------------------------------------------
-- S101 CRC-CCITT (reflected, poly 0x8408, init 0xFFFF, result inverted)
-------------------------------------------------------------------------------

local crcTable = {
    0x0000, 0x1189, 0x2312, 0x329b, 0x4624, 0x57ad, 0x6536, 0x74bf,
    0x8c48, 0x9dc1, 0xaf5a, 0xbed3, 0xca6c, 0xdbe5, 0xe97e, 0xf8f7,
    0x1081, 0x0108, 0x3393, 0x221a, 0x56a5, 0x472c, 0x75b7, 0x643e,
    0x9cc9, 0x8d40, 0xbfdb, 0xae52, 0xdaed, 0xcb64, 0xf9ff, 0xe876,
    0x2102, 0x308b, 0x0210, 0x1399, 0x6726, 0x76af, 0x4434, 0x55bd,
    0xad4a, 0xbcc3, 0x8e58, 0x9fd1, 0xeb6e, 0xfae7, 0xc87c, 0xd9f5,
    0x3183, 0x200a, 0x1291, 0x0318, 0x77a7, 0x662e, 0x54b5, 0x453c,
    0xbdcb, 0xac42, 0x9ed9, 0x8f50, 0xfbef, 0xea66, 0xd8fd, 0xc974,
    0x4204, 0x538d, 0x6116, 0x709f, 0x0420, 0x15a9, 0x2732, 0x36bb,
    0xce4c, 0xdfc5, 0xed5e, 0xfcd7, 0x8868, 0x99e1, 0xab7a, 0xbaf3,
    0x5285, 0x430c, 0x7197, 0x601e, 0x14a1, 0x0528, 0x37b3, 0x263a,
    0xdecd, 0xcf44, 0xfddf, 0xec56, 0x98e9, 0x8960, 0xbbfb, 0xaa72,
    0x6306, 0x728f, 0x4014, 0x519d, 0x2522, 0x34ab, 0x0630, 0x17b9,
    0xef4e, 0xfec7, 0xcc5c, 0xddd5, 0xa96a, 0xb8e3, 0x8a78, 0x9bf1,
    0x7387, 0x620e, 0x5095, 0x411c, 0x35a3, 0x242a, 0x16b1, 0x0738,
    0xffcf, 0xee46, 0xdcdd, 0xcd54, 0xb9eb, 0xa862, 0x9af9, 0x8b70,
    0x8408, 0x9581, 0xa71a, 0xb693, 0xc22c, 0xd3a5, 0xe13e, 0xf0b7,
    0x0840, 0x19c9, 0x2b52, 0x3adb, 0x4e64, 0x5fed, 0x6d76, 0x7cff,
    0x9489, 0x8500, 0xb79b, 0xa612, 0xd2ad, 0xc324, 0xf1bf, 0xe036,
    0x18c1, 0x0948, 0x3bd3, 0x2a5a, 0x5ee5, 0x4f6c, 0x7df7, 0x6c7e,
    0xa50a, 0xb483, 0x8618, 0x9791, 0xe32e, 0xf2a7, 0xc03c, 0xd1b5,
    0x2942, 0x38cb, 0x0a50, 0x1bd9, 0x6f66, 0x7eef, 0x4c74, 0x5dfd,
    0xb58b, 0xa402, 0x9699, 0x8710, 0xf3af, 0xe226, 0xd0bd, 0xc134,
    0x39c3, 0x284a, 0x1ad1, 0x0b58, 0x7fe7, 0x6e6e, 0x5cf5, 0x4d7c,
    0xc60c, 0xd785, 0xe51e, 0xf497, 0x8028, 0x91a1, 0xa33a, 0xb2b3,
    0x4a44, 0x5bcd, 0x6956, 0x78df, 0x0c60, 0x1de9, 0x2f72, 0x3efb,
    0xd68d, 0xc704, 0xf59f, 0xe416, 0x90a9, 0x8120, 0xb3bb, 0xa232,
    0x5ac5, 0x4b4c, 0x79d7, 0x685e, 0x1ce1, 0x0d68, 0x3ff3, 0x2e7a,
    0xe70e, 0xf687, 0xc41c, 0xd595, 0xa12a, 0xb0a3, 0x8238, 0x93b1,
    0x6b46, 0x7acf, 0x4854, 0x59dd, 0x2d62, 0x3ceb, 0x0e70, 0x1ff9,
    0xf78f, 0xe606, 0xd49d, 0xc514, 0xb1ab, 0xa022, 0x92b9, 0x8330,
    0x7bc7, 0x6a4e, 0x58d5, 0x495c, 0x3de3, 0x2c6a, 0x1ef1, 0x0f78,
}

local function crc_ccitt16(bytes)
    local crc = 0xFFFF
    for i = 0, bytes:len() - 1 do
        local b = bytes:get_index(i)
        local idx = band(bxor(crc, b), 0xFF) + 1
        crc = bxor(rshift(crc, 8), crcTable[idx])
    end
    return band(bxor(crc, 0xFFFF), 0xFFFF)
end

-------------------------------------------------------------------------------
-- SLIP-style unescape: 0xFD XX -> (XX xor 0x20)
-- Returns: (ByteArray unescaped, number of escape errors encountered)
-------------------------------------------------------------------------------

local function unescape_s101(tvbuf, offset, endpos)
    local src_range = tvbuf:range(offset, endpos - offset)
    local src = src_range:bytes()
    local dst = ByteArray.new()
    dst:set_size(src:len())
    local di = 0
    local si = 0
    local errs = 0
    while si < src:len() do
        local b = src:get_index(si)
        if b == S101_ESC then
            if si + 1 < src:len() then
                dst:set_index(di, bxor(src:get_index(si + 1), 0x20))
                di = di + 1
                si = si + 2
            else
                errs = errs + 1
                si = si + 1
            end
        else
            dst:set_index(di, b)
            di = di + 1
            si = si + 1
        end
    end
    dst:set_size(di)
    return dst, errs
end

-------------------------------------------------------------------------------
-- BER tag/length readers
-- Read tag at offset. Returns:
--   tag_first  (raw first byte of identifier)
--   tag_class  (0-3)
--   constructed (bool)
--   tag_num    (integer)
--   consumed   (bytes used by identifier)
-------------------------------------------------------------------------------

local function read_tag(ba, off)
    if off >= ba:len() then return nil end
    local first = ba:get_index(off)
    local class = rshift(band(first, 0xC0), 6)
    local cons  = band(first, 0x20) ~= 0
    local short = band(first, 0x1F)
    local consumed = 1
    local tag_num
    if short < 0x1F then
        tag_num = short
    else
        -- multi-byte tag
        tag_num = 0
        local i = off + 1
        while i < ba:len() do
            local b = ba:get_index(i)
            tag_num = bor(lshift(tag_num, 7), band(b, 0x7F))
            i = i + 1
            consumed = consumed + 1
            if band(b, 0x80) == 0 then break end
        end
    end
    return first, class, cons, tag_num, consumed
end

-- Read BER length. Returns length (int) and bytes consumed.
-- length = -1 indicates indefinite form.
local function read_length(ba, off)
    if off >= ba:len() then return nil end
    local first = ba:get_index(off)
    if band(first, 0x80) == 0 then
        return first, 1
    end
    local n = band(first, 0x7F)
    if n == 0 then
        return -1, 1
    end
    if off + n >= ba:len() then return nil end
    local len = 0
    for i = 1, n do
        len = bor(lshift(len, 8), ba:get_index(off + i))
    end
    return len, n + 1
end

-------------------------------------------------------------------------------
-- Primitive value pretty-printers
-- All operate on a ByteArray+Tvb range and populate the tree node.
-------------------------------------------------------------------------------

local function decode_ber_integer(ba, off, len)
    if len == 0 then return 0 end
    local v = ba:get_index(off)
    -- sign-extend from top bit
    if v >= 0x80 then v = v - 0x100 end
    for i = 1, len - 1 do
        v = v * 256 + ba:get_index(off + i)
    end
    return v
end

local function decode_ber_uint(ba, off, len)
    local v = 0
    for i = 0, len - 1 do
        v = v * 256 + ba:get_index(off + i)
    end
    return v
end

-- BER REAL decoder (Ember+ uses binary encoding only for this field).
-- Returns a Lua number or nil.
local function decode_ber_real(ba, off, len)
    if len == 0 then return 0.0 end
    local first = ba:get_index(off)
    -- Binary encoding: bit 8 = 1
    if band(first, 0x80) == 0 then
        -- ISO 6093 decimal or special — not rendered, fall through.
        return nil
    end
    local sign = band(first, 0x40) ~= 0 and -1.0 or 1.0
    local base_bits = rshift(band(first, 0x30), 4)
    local base = ({[0]=2, [1]=8, [2]=16})[base_bits] or 2
    local scale = rshift(band(first, 0x0C), 2)
    local exp_format = band(first, 0x03)
    local exp_len
    local exp_start = off + 1
    if exp_format == 0 then exp_len = 1
    elseif exp_format == 1 then exp_len = 2
    elseif exp_format == 2 then exp_len = 3
    else
        exp_len = ba:get_index(off + 1)
        exp_start = off + 2
    end
    if exp_start + exp_len > off + len then return nil end
    local exp = decode_ber_integer(ba, exp_start, exp_len)
    local mant_start = exp_start + exp_len
    local mant_len   = (off + len) - mant_start
    if mant_len < 0 then return nil end
    local mant = decode_ber_uint(ba, mant_start, mant_len)
    return sign * mant * (2 ^ scale) * (base ^ exp)
end

-- Decode RELATIVE-OID: sequence of base-128 subidentifiers, high bit = "more".
local function decode_relative_oid(ba, off, len)
    local parts = {}
    local i = 0
    while i < len do
        local val = 0
        while i < len do
            local b = ba:get_index(off + i)
            val = val * 128 + band(b, 0x7F)
            i = i + 1
            if band(b, 0x80) == 0 then break end
        end
        table.insert(parts, tostring(val))
    end
    return table.concat(parts, ".")
end

-------------------------------------------------------------------------------
-- Ember-container peek: after each APP tag the payload starts with a
-- constructed SET/SEQUENCE universal tag ([UNIVERSAL 16/17]) whose value holds
-- the actual context-tagged elements. The walker transparently steps through
-- this inner tag (a.k.a. "wrapped SET / SEQUENCE" in the spec).
-------------------------------------------------------------------------------

-------------------------------------------------------------------------------
-- Recursive Glow tree walker.
-- ba:       ByteArray backing the unescaped payload
-- tvb:      Tvb over the escaped payload (same range — used for tree bytes).
--           Because of SLIP escaping, byte positions differ; we use `tvb`
--           only for the synthetic tvb sourced from the unescaped ByteArray.
-- off, len: window inside ba to parse
-- tree:     parent tree node to attach children to
-- scope:    table [ctx-number]->name for context-tagged children, or nil
-------------------------------------------------------------------------------

-------------------------------------------------------------------------------
-- summarize_glow: quick-scan the top-level Glow structure and produce a
-- human-readable hint for the Info column (e.g. "Root { 3 Parameter, 1 Matrix }",
-- "Root { StreamCollection [5] }", "Root { InvocationResult }").
-- No tree side-effects. Returns a short string or nil.
-------------------------------------------------------------------------------

local function peek_app_tag(ba, off, endpos)
    if off >= endpos then return nil end
    local first, class, cons, tag_num, t_consumed = read_tag(ba, off)
    if not first then return nil end
    local len, l_consumed = read_length(ba, off + t_consumed)
    if len == nil then return nil end
    return class, cons, tag_num, t_consumed + l_consumed, len
end

-- Recursively count interesting leaf types in a Glow subtree so the
-- Info column identifies Matrix / Function / Parameter content even when
-- it's nested below Node wrappers. Container types (Node, ElementCollection)
-- are walked through, not counted.
local LEAF_TAGS = {
    [1] = "Parameter", [9]  = "QParameter",
    [13] = "Matrix",   [17] = "QMatrix",
    [19] = "Function", [20] = "QFunction",
    [24] = "Template", [25] = "QTemplate",
    [23] = "InvocationResult",
}

local function count_leaves(ba, off, endpos, counts, depth)
    if depth > 12 then return end
    while off < endpos do
        local c, _cn, t, h, l = peek_app_tag(ba, off, endpos)
        if not c or l == nil then return end
        local val_off = off + h
        local val_end = (l < 0) and endpos or math.min(val_off + l, endpos)
        if c == 1 and LEAF_TAGS[t] then
            local name = LEAF_TAGS[t]
            counts[name] = (counts[name] or 0) + 1
            -- Do NOT recurse into leaves — the tree below is contents/children
            -- of this element, not additional siblings worth counting.
        else
            -- Recurse into containers: Root (0), ElementCollection (4/11),
            -- Node (3/10), CONTEXT wrappers ([0], [1], [2]), universal
            -- SET/SEQUENCE. StreamCollection (6) handled by caller.
            count_leaves(ba, val_off, val_end, counts, depth + 1)
        end
        if l < 0 then return end
        off = val_off + l
    end
end

local function summarize_glow(ba, off, avail)
    local endpos = off + avail
    -- Expect [APP 0] Root wrapper first.
    local class, cons, tag_num, hdr, len = peek_app_tag(ba, off, endpos)
    if not class then return nil end
    if class ~= 1 or tag_num ~= 0 then
        local n = glow_app_valstr[tag_num] or ("APP " .. tostring(tag_num))
        return n
    end
    local inner_off = off + hdr
    local inner_end = (len < 0) and endpos or math.min(inner_off + len, endpos)
    local c2, cn2, t2, h2 = peek_app_tag(ba, inner_off, inner_end)
    if c2 == 0 and cn2 then
        -- skip universal "Ember container" wrapper if present
        inner_off = inner_off + h2
    end

    -- Peek the first child of Root to pick the summary shape.
    local c3, cn3, t3, h3, l3 = peek_app_tag(ba, inner_off, inner_end)
    if not c3 then return "Root { empty }" end

    if c3 == 1 then
        local top = glow_app_valstr[t3] or ("APP " .. t3)
        if t3 == 11 or t3 == 4 then
            -- ElementCollection: recursively count leaves (Matrix, Function,
            -- Parameter, Q* variants) across all nested Node wrappers.
            local child_end = inner_off + h3 + (l3 < 0 and (inner_end - inner_off - h3) or l3)
            local counts = {}
            count_leaves(ba, inner_off + h3, child_end, counts, 0)
            local parts = {}
            for name, n in pairs(counts) do
                table.insert(parts, (n > 1 and (n .. " ") or "") .. name)
            end
            if #parts == 0 then return "Root { " .. top .. " [Node only] }" end
            return "Root { " .. top .. " [" .. table.concat(parts, ", ") .. "] }"
        elseif t3 == 6 then
            -- StreamCollection: count entries.
            local child_end = inner_off + h3 + (l3 < 0 and (inner_end - inner_off - h3) or l3)
            local walk = inner_off + h3
            local n = 0
            while walk < child_end do
                local cc, _cn, ct, ch, cl = peek_app_tag(ba, walk, child_end)
                if not cc then break end
                if cc == 2 or (cc == 1 and ct == 5) then n = n + 1 end
                if cl == nil or cl < 0 then break end
                walk = walk + ch + cl
            end
            return "Root { StreamCollection [" .. n .. "] }"
        else
            return "Root { " .. top .. " }"
        end
    end
    return "Root"
end

local function walk_ber(ba, unesc_tvb, off, avail, tree, scope, depth)
    if depth > 20 then
        tree:add_expert_info(PI_MALFORMED, PI_WARN, "Glow tree recursion limit")
        return 0
    end
    local start = off
    local endpos = off + avail
    while off < endpos do
        local first, class, cons, tag_num, t_consumed = read_tag(ba, off)
        if not first then break end

        -- End-of-contents sentinel (for indefinite-length parents).
        if first == 0 and off + 1 < endpos and ba:get_index(off + 1) == 0 then
            -- Caller is responsible for detecting EoC; stop here.
            return off - start + 2
        end

        local len, l_consumed = read_length(ba, off + t_consumed)
        if len == nil then break end

        local hdr_len = t_consumed + l_consumed
        local indefinite = (len == -1)
        local value_off = off + hdr_len
        local value_len
        if indefinite then
            -- scan forward to find matching EoC (00 00) at this depth.
            -- Use a simple sub-scan; malformed -> consume to endpos.
            local scan = value_off
            local closed = false
            while scan < endpos - 1 do
                if ba:get_index(scan) == 0 and ba:get_index(scan + 1) == 0 then
                    value_len = scan - value_off
                    closed = true
                    break
                end
                -- advance by parsing nested TLV length
                local _, _, _, _, t2 = read_tag(ba, scan)
                if not t2 then break end
                local l2, lc2 = read_length(ba, scan + t2)
                if l2 == nil then break end
                if l2 == -1 then
                    -- nested indefinite: conservative single-byte advance
                    scan = scan + 1
                else
                    scan = scan + t2 + lc2 + l2
                end
            end
            if not closed then
                value_len = endpos - value_off
            end
        else
            value_len = len
        end

        if value_off + value_len > endpos then
            value_len = endpos - value_off
        end

        local total = hdr_len + value_len + (indefinite and 2 or 0)

        -- Build a label for this element.
        local tag_label
        local ctx_label
        if class == 1 then
            -- Application
            local gname = glow_app_valstr[tag_num] or ("APP " .. tag_num)
            tag_label = "[APPLICATION " .. tag_num .. "] " .. gname
        elseif class == 2 then
            -- Context
            local name = scope and scope[tag_num]
            ctx_label = name
            if name then
                tag_label = "[" .. tag_num .. "] " .. name
            else
                tag_label = "[CONTEXT " .. tag_num .. "]"
            end
        elseif class == 0 then
            -- Universal
            local uname = ber_universal_valstr[tag_num] or ("UNIVERSAL " .. tag_num)
            tag_label = uname
        else
            tag_label = "[PRIVATE " .. tag_num .. "]"
        end

        if cons then tag_label = tag_label .. " {}" end

        -- Backing tvb slice (only valid if unesc_tvb covers this region).
        local node_tvb_range
        if unesc_tvb and off < unesc_tvb:len() then
            local node_len = math.min(total, unesc_tvb:len() - off)
            node_tvb_range = unesc_tvb:range(off, node_len)
        end

        local node
        if node_tvb_range then
            node = tree:add(glow_proto, node_tvb_range, tag_label)
        else
            node = tree:add(glow_proto, tag_label)
        end

        -- Tag / length detail fields
        if node_tvb_range then
            local tag_range = unesc_tvb:range(off, t_consumed)
            node:add(glow_f.tag_class, tag_range, class)
            node:add(glow_f.tag_cons,  tag_range, cons and 1 or 0)
            node:add(glow_f.tag_num,   tag_range, tag_num)
            if class == 1 and glow_app_valstr[tag_num] then
                node:add(glow_f.tag_name, tag_range, glow_app_valstr[tag_num])
            elseif class == 0 and ber_universal_valstr[tag_num] then
                node:add(glow_f.tag_name, tag_range, ber_universal_valstr[tag_num])
            elseif ctx_label then
                node:add(glow_f.tag_name, tag_range, ctx_label)
            end
            local len_range = unesc_tvb:range(off + t_consumed, l_consumed)
            if indefinite then
                node:add(glow_f.length_indef, len_range, 1)
            else
                node:add(glow_f.length, len_range, len)
            end
        end

        -- Recurse or render primitive.
        if cons then
            -- Pick child scope:
            --  Application -> wrapper_scope_by_app[app]
            --  Context inside a known wrapper -> contents_scope_by_app when ctx=1
            --  Otherwise keep current scope for universal SET/SEQUENCE transit.
            local child_scope = scope
            if class == 1 then
                child_scope = wrapper_scope_by_app[tag_num]
            elseif class == 2 and scope and scope == wrapper_scope_by_app[1] and tag_num == 1 then
                child_scope = contents_scope_by_app[1]
            elseif class == 2 and scope == wrapper_scope_by_app[3] and tag_num == 1 then
                child_scope = contents_scope_by_app[3]
            elseif class == 2 and scope == wrapper_scope_by_app[9] and tag_num == 1 then
                child_scope = contents_scope_by_app[9]
            elseif class == 2 and scope == wrapper_scope_by_app[10] and tag_num == 1 then
                child_scope = contents_scope_by_app[10]
            elseif class == 2 and scope == scope_matrix_wrapper and tag_num == 1 then
                child_scope = scope_matrix_contents
            elseif class == 2 and scope == wrapper_scope_by_app[19] and tag_num == 1 then
                child_scope = scope_function_contents
            elseif class == 2 and scope == wrapper_scope_by_app[20] and tag_num == 1 then
                child_scope = scope_function_contents
            end
            walk_ber(ba, unesc_tvb, value_off, value_len, node, child_scope, depth + 1)
        else
            -- Primitive — decode by tag class/number.
            if class == 0 then
                if tag_num == 0x02 then
                    -- INTEGER
                    local v = decode_ber_integer(ba, value_off, value_len)
                    if node_tvb_range and value_len > 0 and value_off < unesc_tvb:len() then
                        node:add(glow_f.value_int, unesc_tvb:range(value_off, math.min(value_len, unesc_tvb:len() - value_off)), tostring(v))
                    else
                        node:add(glow_f.value_int, tostring(v))
                    end
                    node:append_text(" = " .. tostring(v))
                    -- If this was Command.number, annotate the name.
                    if scope == scope_command and ctx_label == "number" then
                        local cn = glow_cmd_number_valstr[v]
                        if cn then node:append_text(" (" .. cn .. ")") end
                    end
                elseif tag_num == 0x01 then
                    -- BOOLEAN
                    local v = value_len > 0 and ba:get_index(value_off) ~= 0 or false
                    node:add(glow_f.value_bool, tostring(v))
                    node:append_text(" = " .. tostring(v))
                elseif tag_num == 0x09 then
                    -- REAL
                    local v = decode_ber_real(ba, value_off, value_len)
                    if v then
                        node:add(glow_f.value_real, string.format("%g", v))
                        node:append_text(" = " .. string.format("%g", v))
                    else
                        if node_tvb_range and value_off < unesc_tvb:len() then
                            node:add(glow_f.value_raw, unesc_tvb:range(value_off, math.min(value_len, unesc_tvb:len() - value_off)))
                        end
                    end
                elseif tag_num == 0x0C then
                    -- UTF8String
                    if value_len > 0 and node_tvb_range and value_off < unesc_tvb:len() then
                        local s = unesc_tvb:range(value_off, math.min(value_len, unesc_tvb:len() - value_off)):string()
                        node:add(glow_f.value_utf8, unesc_tvb:range(value_off, math.min(value_len, unesc_tvb:len() - value_off)), s)
                        node:append_text(' = "' .. s .. '"')
                    end
                elseif tag_num == 0x04 then
                    -- OCTET STRING
                    if node_tvb_range and value_len > 0 and value_off < unesc_tvb:len() then
                        node:add(glow_f.value_octets, unesc_tvb:range(value_off, math.min(value_len, unesc_tvb:len() - value_off)))
                    end
                elseif tag_num == 0x05 then
                    -- NULL
                    node:add(glow_f.value_null, "NULL")
                elseif tag_num == 0x0D then
                    -- RELATIVE-OID
                    local s = decode_relative_oid(ba, value_off, value_len)
                    node:add(glow_f.value_oid, s)
                    node:append_text(" = " .. s)
                elseif tag_num == 0x0A then
                    -- ENUMERATED
                    local v = decode_ber_integer(ba, value_off, value_len)
                    node:add(glow_f.value_int, tostring(v))
                    node:append_text(" = " .. tostring(v))
                else
                    if node_tvb_range and value_len > 0 and value_off < unesc_tvb:len() then
                        node:add(glow_f.value_raw, unesc_tvb:range(value_off, math.min(value_len, unesc_tvb:len() - value_off)))
                    end
                end
            else
                if node_tvb_range and value_len > 0 and value_off < unesc_tvb:len() then
                    node:add(glow_f.value_raw, unesc_tvb:range(value_off, math.min(value_len, unesc_tvb:len() - value_off)))
                end
            end
        end

        off = off + total
    end
    return off - start
end

-------------------------------------------------------------------------------
-- Decode one S101 frame spanning [frame_start .. frame_end] inclusive in
-- tvbuf (markers BoF/EoF included). Adds tree nodes, returns info string.
-------------------------------------------------------------------------------

local function dissect_s101_frame(tvbuf, pktinfo, root, frame_start, frame_end)
    local frame_len = frame_end - frame_start + 1
    local tree = root:add(s101_proto, tvbuf:range(frame_start, frame_len))

    tree:add(s101_f.bof, tvbuf:range(frame_start, 1))
    tree:add(s101_f.eof, tvbuf:range(frame_end, 1))

    -- Unescape content between BoF and EoF.
    local unesc_bytes, escape_errs = unescape_s101(tvbuf, frame_start + 1, frame_end)
    if escape_errs > 0 then
        tree:add_expert_info(PI_MALFORMED, PI_WARN, "Truncated S101 escape (0xFD at frame boundary)")
    end

    local unesc_tvb = unesc_bytes:tvb("S101 Unescaped")
    tree:add(s101_f.unescaped, unesc_tvb:range(0, unesc_bytes:len()))

    local min = 6 -- 4 header + 2 CRC
    if unesc_bytes:len() < min then
        pktinfo.cols.info:set("S101 truncated")
        return "S101 truncated"
    end

    local command = unesc_bytes:get_index(2)

    tree:add(s101_f.slot,    unesc_tvb:range(0, 1))
    tree:add(s101_f.msgtype, unesc_tvb:range(1, 1))
    tree:add(s101_f.command, unesc_tvb:range(2, 1))
    tree:add(s101_f.version, unesc_tvb:range(3, 1))

    -- CRC (last two bytes of unescaped content, little-endian).
    local clen = unesc_bytes:len()
    local crc_got = unesc_bytes:get_index(clen - 2) + lshift(unesc_bytes:get_index(clen - 1), 8)
    -- Compute expected CRC over content bytes [0..clen-3].
    local ba_core = unesc_bytes:subset(0, clen - 2)
    local crc_want = crc_ccitt16(ba_core)
    tree:add(s101_f.crc, unesc_tvb:range(clen - 2, 2), crc_got)
    if crc_got == crc_want then
        tree:add(s101_f.crc_status, "OK"):set_generated()
    else
        tree:add(s101_f.crc_status, string.format("BAD (got 0x%04X expected 0x%04X)", crc_got, crc_want)):set_generated()
        tree:add_expert_info(PI_CHECKSUM, PI_WARN, "S101 CRC mismatch")
    end

    local info
    if command == 0x01 then
        info = "S101 KeepAlive Request"
    elseif command == 0x02 then
        info = "S101 KeepAlive Response"
    elseif command == 0x00 then
        -- EmBER data frame — expect 9-byte header.
        if clen < 11 then
            info = "S101 EmBER (truncated header)"
        else
            local flags    = unesc_bytes:get_index(4)
            local appblen  = unesc_bytes:get_index(6)
            tree:add(s101_f.flags,   unesc_tvb:range(4, 1))
            tree:add(s101_f.dtd,     unesc_tvb:range(5, 1))
            tree:add(s101_f.appblen, unesc_tvb:range(6, 1))
            if appblen > 0 and 7 + appblen <= clen - 2 then
                tree:add(s101_f.appbytes, unesc_tvb:range(7, appblen))
            end

            local payload_off = 7 + appblen
            local payload_len = clen - 2 - payload_off
            local summary
            if payload_len > 0 then
                summary = summarize_glow(unesc_bytes, payload_off, payload_len)
                local glow_tree = tree:add(glow_proto, unesc_tvb:range(payload_off, payload_len),
                                           "Glow Payload (" .. payload_len .. " bytes)" ..
                                           (summary and " — " .. summary or ""))
                walk_ber(unesc_bytes, unesc_tvb, payload_off, payload_len, glow_tree, nil, 0)
            end

            local flag_name = s101_flags_valstr[flags] or string.format("flags=0x%02X", flags)
            info = "Ember+ " .. flag_name .. (summary and " " .. summary or "") ..
                   " payload=" .. payload_len .. "B"
        end
    else
        info = "S101 cmd=0x" .. string.format("%02X", command)
    end

    return info
end

-------------------------------------------------------------------------------
-- Find next S101 frame start/end in the TCP segment, handling escapes.
-- Returns (frame_start, frame_end) or nil when no complete frame is present.
-- Also sets pktinfo.desegment_len when reassembly is needed.
-------------------------------------------------------------------------------

local function find_next_frame(tvbuf, offset)
    local pktlen = tvbuf:reported_length_remaining()
    -- find BoF
    while offset < pktlen do
        if tvbuf:range(offset, 1):uint() == S101_BOF then break end
        offset = offset + 1
    end
    if offset >= pktlen then return nil end

    local start = offset
    -- find EoF, skipping escape sequences.
    local i = offset + 1
    while i < pktlen do
        local b = tvbuf:range(i, 1):uint()
        if b == S101_ESC then
            i = i + 2
        elseif b == S101_EOF then
            return start, i
        else
            i = i + 1
        end
    end

    -- Not found. Caller handles reassembly (we don't set desegment fields
    -- here — doing so from a helper breaks the TCP layer's save/compare
    -- invariant when multiple frames live in the same segment).
    return start, nil
end

-------------------------------------------------------------------------------
-- Main dissector entry point with TCP reassembly.
--
-- The dissector must either fully consume the buffer OR set BOTH
-- desegment_offset and desegment_len atomically, exactly once per call.
-- Setting those fields more than once across nested helpers violates
-- the TCP dissector's invariant ("save_desegment_*" assertion at
-- packet-tcp.c:8139) and causes Wireshark to abort with a dissector bug.
-------------------------------------------------------------------------------

function s101_proto.dissector(tvbuf, pktinfo, root)
    local pktlen = tvbuf:reported_length_remaining()
    if pktlen == 0 then return 0 end

    local offset = 0
    local frames = 0
    local info_parts = {}
    while offset < pktlen do
        local start, endpos = find_next_frame(tvbuf, offset)
        if not start then
            if frames == 0 then
                return 0  -- no BoF, not our protocol (or mid-stream garbage)
            end
            break
        end
        if not endpos then
            -- Partial frame at tail — ask TCP layer for more data.
            -- Only set these once, here, at the true reassembly boundary.
            pktinfo.desegment_offset = start
            pktinfo.desegment_len    = DESEGMENT_ONE_MORE_SEGMENT
            return pktlen
        end
        local info = dissect_s101_frame(tvbuf, pktinfo, root, start, endpos)
        table.insert(info_parts, info)
        offset = endpos + 1
        frames = frames + 1
    end

    -- Only set protocol column after we've confirmed at least one valid frame.
    pktinfo.cols.protocol:set("Ember+")

    if #info_parts > 0 then
        if frames > 1 then
            pktinfo.cols.info:set("[" .. frames .. " frames] " .. table.concat(info_parts, " | "))
        else
            pktinfo.cols.info:set(info_parts[1])
        end
    end

    return offset
end

-------------------------------------------------------------------------------
-- Register on default TCP ports. Users can override with Decode As ...
-------------------------------------------------------------------------------

local tcp_port_table = DissectorTable.get("tcp.port")
for _, p in ipairs(DEFAULT_TCP_PORTS) do
    tcp_port_table:add(p, s101_proto)
end

-- Heuristic fallback so the dissector fires on any TCP port, not only the
-- three defaults. To avoid claiming unrelated TCP streams we validate the
-- S101 header fields (msgType / command / version) after unescaping the
-- first frame.
local function heuristic(tvbuf, pktinfo, root)
    local len = tvbuf:reported_length_remaining()
    if len < 6 then return false end
    if tvbuf:range(0, 1):uint() ~= S101_BOF then return false end

    -- Bounded EoF scan, skipping escape sequences.
    local scan_limit = math.min(len, 4096)
    local eof_at
    local i = 1
    while i < scan_limit do
        local b = tvbuf:range(i, 1):uint()
        if b == S101_ESC then
            i = i + 2
        elseif b == S101_EOF then
            eof_at = i
            break
        else
            i = i + 1
        end
    end
    if not eof_at then return false end

    -- Validate S101 header after unescape.
    local unesc = unescape_s101(tvbuf, 1, eof_at)
    if unesc:len() < 6 then return false end
    local msg_type = unesc:get_index(1)
    local command  = unesc:get_index(2)
    local version  = unesc:get_index(3)
    if msg_type ~= 0x0E then return false end
    if command ~= 0x00 and command ~= 0x01 and command ~= 0x02 and command ~= 0x0E then
        return false
    end
    if version ~= 0x01 then return false end

    s101_proto.dissector(tvbuf, pktinfo, root)
    return true
end

s101_proto:register_heuristic("tcp", heuristic)
