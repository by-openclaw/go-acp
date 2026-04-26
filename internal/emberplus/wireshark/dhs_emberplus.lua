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
--            internal/emberplus/assets/Ember+ Documentation.pdf
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

-- S101 flag bits (EmBER data frames, byte offset 4 of unescaped content).
local FLAG_SINGLE = 0xC0  -- first + last
local FLAG_FIRST  = 0x80
local FLAG_LAST   = 0x40
local FLAG_EMPTY  = 0x20

-- Per-TCP-stream reassembly state (first dissection pass only).
-- Key: "src:sport>dst:dport"   Value: { payload = ByteArray accumulating the Glow fragment bytes }
local s101_reassembly_state = {}

-- Per-packet result cache keyed by pktinfo.number, so re-dissection (filtering
-- / scrolling) is deterministic. Cleared at start of each capture file via
-- s101_proto.init below.
-- Value: { assembled = bool, payload = ByteArray, fragment_kind = "first"|"middle"|"last"|"orphan" }
local s101_packet_cache = {}

-- Per-conversation OID→identifier cache (issue #59, part 1).
-- Populated by element_summary whenever a leaf carries both a path and an
-- identifier; consumed when rendering the Info column to show the dotted
-- identifier chain (e.g. `1.2.3 router.oneToN.nToN`).
-- Keyed by "<conv_key>|<dotted_path>" — flat string to avoid nested-table
-- lookup cost in the hot summarize path.
local emberplus_id_cache = {}

-- Current-frame conversation key, set by the main S101 dissector right
-- before calling summarize_glow. Same side-channel pattern ACP2 uses
-- for acp2_current_slot — summarize_glow and element_summary are invoked
-- deep in the BER walker and cannot take pktinfo as a parameter without
-- threading it through many call sites.
local emberplus_current_conv_key = ""

local function s101_conv_key(pktinfo)
    return string.format("%s:%d>%s:%d",
        tostring(pktinfo.src), pktinfo.src_port,
        tostring(pktinfo.dst), pktinfo.dst_port)
end

local function cache_identifier(path, ident)
    if path == nil or path == "" or ident == nil or ident == "" then return end
    if emberplus_current_conv_key == "" then return end
    emberplus_id_cache[emberplus_current_conv_key .. "|" .. path] = ident
end

-- Resolve a dotted OID like "1.2.3" to the chain of cached identifiers
-- ("router.oneToN.nToN") by looking up every prefix. Returns nil if no
-- prefix has a cached identifier yet. Returns a partial chain (dots
-- for missing segments) when only some prefixes are cached, so the user
-- still sees progress as the walk accumulates.
local function resolve_identifier_chain(path)
    if emberplus_current_conv_key == "" or path == nil or path == "" then
        return nil
    end
    local segments = {}
    local parts = {}
    for seg in string.gmatch(path, "[^.]+") do
        table.insert(segments, seg)
        local prefix = table.concat(segments, ".")
        local id = emberplus_id_cache[emberplus_current_conv_key .. "|" .. prefix]
        table.insert(parts, id or "?")
    end
    -- Only return a chain when at least one segment resolves, otherwise
    -- the "?.?.?" noise is worse than showing no chain at all.
    local any_resolved = false
    for _, p in ipairs(parts) do
        if p ~= "?" then any_resolved = true; break end
    end
    if not any_resolved then return nil end
    return table.concat(parts, ".")
end

-- Per-conversation last-seen parameter value cache (issue #59 part 4).
-- Keyed by "<conv_key>|<path>" → the raw value string rendered by
-- decode_value_field. On every sighting, note_value_diff returns the
-- PREVIOUS value (or nil) and stores the new one. When they differ the
-- Info column shows `= new (was old)`.
local emberplus_value_cache = {}

local function note_value_diff(path, new_val)
    if emberplus_current_conv_key == "" then return nil end
    if path == nil or path == "" or new_val == nil or new_val == "" then return nil end
    local k = emberplus_current_conv_key .. "|" .. path
    local prev = emberplus_value_cache[k]
    emberplus_value_cache[k] = new_val
    return prev
end

-- Per-conversation matrix label cache (issue #59 part 2).
-- Structure (flat string key for speed):
--   emberplus_matrix_labels[conv_key .. "|" .. matrix_path] = {
--     targets = { [idx] = "name", ... },
--     sources = { [idx] = "name", ... },
--   }
-- Populated when a label-looking Parameter flows by (identifier matches
-- "t-N" or "s-N", has a string value). Consumed when a Matrix
-- Connection is rendered — the matrix's t=X ← [Y] gets enriched to
-- `target "name" (t=X) ← source "name" [Y]`.
local emberplus_matrix_labels = {}

-- parse_label_path extracts (matrix_path, kind, idx) from a label
-- parameter's (path, identifier). Only matches the `.labels.<level>.
-- (targets|sources).<idx>` convention used by TinyEmber+, our provider
-- fixture, and typical Lawo trees. Returns nil when the shape doesn't fit.
--
-- Example: path="1.2.1.1.1.0" identifier="t-0"
--   strip last 4 segments (.labels.Primary.targets.0) → "1.2"
--   kind = "targets", idx = 0
-- That "1.2" is usually the PARENT node of both the matrix and the
-- labels container, not the matrix itself. cache/lookup below handle
-- the fallback from matrix path → parent path.
local function parse_label_path(path, identifier)
    if identifier == nil or path == nil then return nil end
    local idx_str, kind
    idx_str = identifier:match("^t%-(%d+)$")
    if idx_str then
        kind = "targets"
    else
        idx_str = identifier:match("^s%-(%d+)$")
        if idx_str then kind = "sources" end
    end
    if idx_str == nil then return nil end
    local segs = {}
    for s in string.gmatch(path, "[^.]+") do table.insert(segs, s) end
    if #segs < 5 then return nil end
    for _ = 1, 4 do table.remove(segs) end
    return table.concat(segs, "."), kind, tonumber(idx_str)
end

local function cache_matrix_label(parent_path, kind, idx, label)
    if emberplus_current_conv_key == "" then return end
    if parent_path == nil or kind == nil or idx == nil or label == nil then return end
    local k = emberplus_current_conv_key .. "|" .. parent_path
    local bucket = emberplus_matrix_labels[k]
    if bucket == nil then
        bucket = { targets = {}, sources = {} }
        emberplus_matrix_labels[k] = bucket
    end
    bucket[kind][idx] = label
end

local function lookup_matrix_label(matrix_path, kind, idx)
    if emberplus_current_conv_key == "" or matrix_path == nil then return nil end
    -- Try matrix's own path first (Labels nested under the matrix).
    local b = emberplus_matrix_labels[emberplus_current_conv_key .. "|" .. matrix_path]
    if b and b[kind] and b[kind][idx] then return b[kind][idx] end
    -- Fall back to the matrix's parent (sibling `.labels` layout).
    local parent = matrix_path:match("^(.*)%.[^.]+$")
    if parent then
        b = emberplus_matrix_labels[emberplus_current_conv_key .. "|" .. parent]
        if b and b[kind] and b[kind][idx] then return b[kind][idx] end
    end
    return nil
end

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
    [0x60] = "Last multi-packet (empty)",  -- FLAG_LAST | FLAG_EMPTY, used by our provider to close sequences
    [0x80] = "First multi-packet",
    [0xA0] = "First multi-packet (empty)",
    [0xC0] = "Single packet",
    [0xE0] = "Single packet (empty)",
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

-- Sentinel scope set when we descend from Command [0] into its INTEGER
-- child so the primitive renderer can annotate the CommandType enum
-- (30=Subscribe, 31=Unsubscribe, 32=GetDirectory, 33=Invoke).
local scope_command_number = { [".marker"] = "cmd_num" }

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

local s101_proto = Proto("dhs_emberplus",      "Ember+ (S101)")
local glow_proto = Proto("dhs_emberplus_glow", "Ember+ Glow BER")

-------------------------------------------------------------------------------
-- ProtoFields
-------------------------------------------------------------------------------

local s101_f = {
    bof      = ProtoField.uint8 ("dhs_emberplus.s101.bof",     "BoF",      base.HEX),
    eof      = ProtoField.uint8 ("dhs_emberplus.s101.eof",     "EoF",      base.HEX),
    slot     = ProtoField.uint8 ("dhs_emberplus.s101.slot",    "Slot",     base.DEC),
    msgtype  = ProtoField.uint8 ("dhs_emberplus.s101.msgtype", "Msg Type", base.HEX, s101_msgtype_valstr),
    command  = ProtoField.uint8 ("dhs_emberplus.s101.command", "Command",  base.HEX, s101_cmd_valstr),
    version  = ProtoField.uint8 ("dhs_emberplus.s101.version", "Version",  base.DEC),
    flags    = ProtoField.uint8 ("dhs_emberplus.s101.flags",   "Flags",    base.HEX, s101_flags_valstr),
    dtd      = ProtoField.uint8 ("dhs_emberplus.s101.dtd",     "DTD Type", base.HEX, s101_dtd_valstr),
    appblen  = ProtoField.uint8 ("dhs_emberplus.s101.applen",  "App Bytes Length", base.DEC),
    appbytes = ProtoField.bytes ("dhs_emberplus.s101.appbytes","App Bytes"),
    crc      = ProtoField.uint16("dhs_emberplus.s101.crc",     "CRC",      base.HEX),
    crc_status = ProtoField.string("dhs_emberplus.s101.crc_status", "CRC Status"),
    escaped  = ProtoField.bytes ("dhs_emberplus.s101.escaped", "Escaped Payload"),
    unescaped= ProtoField.bytes ("dhs_emberplus.s101.unescaped","Unescaped Content"),
}
s101_proto.fields = s101_f

-- All decoded BER values are exposed as strings to avoid ProtoField type
-- size mismatches (INTEGER is variable-width, not a neat int32/int64; REAL
-- uses its own BER sub-encoding). Raw bytes are still attached separately
-- for binary inspection.
local glow_f = {
    tag_class = ProtoField.uint8 ("dhs_emberplus.glow.class",    "Class",     base.DEC, ber_class_valstr),
    tag_cons  = ProtoField.bool  ("dhs_emberplus.glow.constructed","Constructed"),
    tag_num   = ProtoField.uint32("dhs_emberplus.glow.tag_num",  "Tag Number",base.DEC),
    tag_name  = ProtoField.string("dhs_emberplus.glow.tag_name", "Tag Name"),
    length    = ProtoField.uint32("dhs_emberplus.glow.length",   "Length",    base.DEC),
    length_indef = ProtoField.bool ("dhs_emberplus.glow.length_indef", "Indefinite Length"),
    value_int   = ProtoField.string("dhs_emberplus.glow.int",    "Value (int)"),
    value_bool  = ProtoField.string("dhs_emberplus.glow.bool",   "Value (bool)"),
    value_real  = ProtoField.string("dhs_emberplus.glow.real",   "Value (real)"),
    value_utf8  = ProtoField.string("dhs_emberplus.glow.utf8",   "Value (UTF-8)"),
    value_oid   = ProtoField.string("dhs_emberplus.glow.reloid", "Value (RELATIVE-OID)"),
    value_null  = ProtoField.string("dhs_emberplus.glow.null",   "Value (NULL)"),
    value_octets= ProtoField.bytes ("dhs_emberplus.glow.octets", "Value (octets)"),
    value_raw   = ProtoField.bytes ("dhs_emberplus.glow.raw",    "Value (raw)"),
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
--
-- X.690 §8.5.7 reads the mantissa N as an unsigned integer with
-- value = N × 2^F × B^E. Every Ember+ stack in the wild
-- (libember, EmberViewer, EmberPlusView, Lawo VSM) instead reads N
-- as a normalised fraction with binary point implicit after the
-- leading 1 bit, so the wire exponent is biased by bitlen(N)-1.
-- This dissector mirrors the ecosystem reading; otherwise it shows
-- 50.0 as 3.125, 100.0 as 6.25, etc. See issue #68 (2026-04-26).
local function bitlen64(n)
    if n == 0 then return 0 end
    local b = 0
    while n > 0 do
        b = b + 1
        n = math.floor(n / 2)
    end
    return b
end

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
    local shift = mant > 0 and (bitlen64(mant) - 1) or 0
    return sign * mant * (2 ^ scale) * (base ^ exp) / (2 ^ shift)
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

-------------------------------------------------------------------------------
-- Typed-field decoders for Info-column content summary.
-- Extract path / identifier / value / connection details from a Glow leaf
-- so the packet list shows watch-style content at a glance:
--   "QMatrix 1.2.3 'router.nToN.3' conn t=3←[12] absolute modified"
--   "QParameter 1.4.0 'Volume' RW = 50"
--   "StreamEntry #3 = -12328"
-------------------------------------------------------------------------------

local function read_utf8(ba, off, len)
    if len <= 0 then return "" end
    local out = {}
    for i = 0, len - 1 do
        local b = ba:get_index(off + i)
        if b == 0 then break end
        out[#out + 1] = string.char(b)
    end
    return table.concat(out)
end

local function decode_utf8_field(ba, off, endpos)
    local c, _cn, t, h, l = peek_app_tag(ba, off, endpos)
    if not c or c ~= 0 or t ~= 0x0C or l == nil or l < 0 then return nil end
    return read_utf8(ba, off + h, l)
end

local function decode_integer_field(ba, off, endpos)
    local c, _cn, t, h, l = peek_app_tag(ba, off, endpos)
    if not c or c ~= 0 or t ~= 0x02 or l == nil or l < 0 then return nil end
    return decode_ber_integer(ba, off + h, l)
end

local function decode_reloid_field(ba, off, endpos)
    local c, _cn, t, h, l = peek_app_tag(ba, off, endpos)
    if not c or c ~= 0 or t ~= 0x0D or l == nil or l < 0 then return nil end
    return decode_relative_oid(ba, off + h, l)
end

local function decode_bool_field(ba, off, endpos)
    local c, _cn, t, h, l = peek_app_tag(ba, off, endpos)
    if not c or c ~= 0 or t ~= 0x01 or l == nil or l < 0 then return nil end
    return l > 0 and ba:get_index(off + h) ~= 0 or false
end

-- Value CHOICE — integer / real / string / boolean / octets / null.
local function decode_value_field(ba, off, endpos)
    local c, _cn, t, h, l = peek_app_tag(ba, off, endpos)
    if not c or c ~= 0 or l == nil or l < 0 then return nil end
    local v_off = off + h
    if t == 0x02 then return tostring(decode_ber_integer(ba, v_off, l))
    elseif t == 0x01 then
        return (l > 0 and ba:get_index(v_off) ~= 0) and "true" or "false"
    elseif t == 0x09 then
        local r = decode_ber_real(ba, v_off, l)
        return r and string.format("%g", r) or nil
    elseif t == 0x0C then return '"' .. read_utf8(ba, v_off, l) .. '"'
    elseif t == 0x04 then return string.format("<%dB>", l)
    elseif t == 0x05 then return "null"
    end
    return nil
end

local ACCESS_NAMES  = { [0] = "--", [1] = "R-", [2] = "-W", [3] = "RW" }
local CONN_OPS      = { [0] = "absolute", [1] = "connect", [2] = "disconnect" }
local CONN_DISPS    = { [0] = "tally", [1] = "modified", [2] = "pending", [3] = "locked" }

-- ParameterContents and NodeContents are BER SET OF { CTX-tagged fields }.
-- The CTX[1] contents wrapper holds a universal SET (tag 17) as its
-- immediate child, and the CTX-tagged identifier/value/access fields live
-- INSIDE that SET. Without descending into the SET, peek_app_tag sees a
-- universal constructed tag and the walk skips straight past the whole
-- block — so identifier/value/access come back nil.
local function step_through_set_wrapper(ba, off, endpos)
    local c, cn, t, h, l = peek_app_tag(ba, off, endpos)
    if c == 0 and cn == true and t == 17 and l ~= nil and l >= 0 then
        return off + h, math.min(off + h + l, endpos)
    end
    return off, endpos
end

local function decode_parameter_contents(ba, off, endpos)
    off, endpos = step_through_set_wrapper(ba, off, endpos)
    local out = {}
    local walk = off
    while walk < endpos do
        local c, _cn, t, h, l = peek_app_tag(ba, walk, endpos)
        if not c or l == nil or l < 0 then break end
        local v_off, v_end = walk + h, math.min(walk + h + l, endpos)
        if c == 2 then
            if     t == 0 then out.identifier = decode_utf8_field(ba, v_off, v_end)
            elseif t == 2 then out.value      = decode_value_field(ba, v_off, v_end)
            elseif t == 5 then
                local acc = decode_integer_field(ba, v_off, v_end)
                if acc then out.access = ACCESS_NAMES[acc] or tostring(acc) end
            end
        end
        walk = v_off + l
    end
    return out
end

local function decode_node_contents(ba, off, endpos)
    off, endpos = step_through_set_wrapper(ba, off, endpos)
    local out = {}
    local walk = off
    while walk < endpos do
        local c, _cn, t, h, l = peek_app_tag(ba, walk, endpos)
        if not c or l == nil or l < 0 then break end
        local v_off, v_end = walk + h, math.min(walk + h + l, endpos)
        if c == 2 and t == 0 then out.identifier = decode_utf8_field(ba, v_off, v_end) end
        walk = v_off + l
    end
    return out
end

local function decode_connection_seq(ba, off, endpos)
    local out = {}
    local walk = off
    while walk < endpos do
        local c, _cn, t, h, l = peek_app_tag(ba, walk, endpos)
        if not c or l == nil or l < 0 then break end
        local v_off, v_end = walk + h, math.min(walk + h + l, endpos)
        if c == 2 then
            if     t == 0 then out.target  = decode_integer_field(ba, v_off, v_end)
            elseif t == 1 then out.sources = decode_reloid_field(ba, v_off, v_end)
            elseif t == 2 then
                local n = decode_integer_field(ba, v_off, v_end)
                out.op = n and (CONN_OPS[n] or tostring(n))
            elseif t == 3 then
                local n = decode_integer_field(ba, v_off, v_end)
                out.disp = n and (CONN_DISPS[n] or tostring(n))
            end
        end
        walk = v_off + l
    end
    return out
end

-- ConnectionCollection: universal SEQUENCE OF { [0] Connection (APP 16) }.
-- Wire sample: a5 15 (ctx [5]) 30 13 (universal SEQUENCE) a0 11 (ctx [0]) 70 0f (APP 16).
local function decode_matrix_connections(ba, off, endpos)
    -- Step through an optional universal SEQUENCE wrapper.
    local c0, _cn0, t0, h0, l0 = peek_app_tag(ba, off, endpos)
    if c0 == 0 and t0 == 16 and l0 ~= nil and l0 >= 0 then
        off    = off + h0
        endpos = math.min(off + l0, endpos)
    end

    local walk = off
    while walk < endpos do
        local c, _cn, t, h, l = peek_app_tag(ba, walk, endpos)
        if not c or l == nil then break end
        local v_off = walk + h
        local v_end = (l < 0) and endpos or math.min(v_off + l, endpos)
        if c == 2 and t == 0 then
            local cc, _cn2, ct, ch, cl = peek_app_tag(ba, v_off, v_end)
            if cc == 1 and ct == 16 and cl ~= nil and cl >= 0 then
                return decode_connection_seq(ba, v_off + ch, v_off + ch + cl)
            end
        elseif c == 1 and t == 16 and l >= 0 then
            return decode_connection_seq(ba, v_off, v_end)
        end
        if l < 0 then break end
        walk = v_off + l
    end
    return nil
end

local function decode_stream_entry(ba, off, endpos)
    local out = {}
    local walk = off
    while walk < endpos do
        local c, _cn, t, h, l = peek_app_tag(ba, walk, endpos)
        if not c or l == nil or l < 0 then break end
        local v_off, v_end = walk + h, math.min(walk + h + l, endpos)
        if c == 2 then
            if     t == 0 then out.id    = decode_integer_field(ba, v_off, v_end)
            elseif t == 1 then out.value = decode_value_field(ba, v_off, v_end)
            end
        end
        walk = v_off + l
    end
    return out
end

-- Build a short human-readable content string for a single leaf element.
local function element_summary(ba, app_tag, off, endpos)
    local is_q      = (app_tag == 9) or (app_tag == 10) or (app_tag == 17) or (app_tag == 20) or (app_tag == 25)
    local is_matrix = (app_tag == 13) or (app_tag == 17)
    local is_param  = (app_tag == 1)  or (app_tag == 9)
    local path, number, identifier, value, access, conn
    local walk = off
    while walk < endpos do
        local c, _cn, t, h, l = peek_app_tag(ba, walk, endpos)
        if not c or l == nil then break end
        local v_off = walk + h
        local v_end = (l < 0) and endpos or math.min(v_off + l, endpos)
        if c == 2 then
            if t == 0 then
                if is_q then path = decode_reloid_field(ba, v_off, v_end)
                else number = decode_integer_field(ba, v_off, v_end) end
            elseif t == 1 then
                if is_param then
                    local pc = decode_parameter_contents(ba, v_off, v_end)
                    identifier, value, access = pc.identifier, pc.value, pc.access
                else
                    local nc = decode_node_contents(ba, v_off, v_end)
                    identifier = nc.identifier
                end
            elseif t == 5 and is_matrix then
                conn = decode_matrix_connections(ba, v_off, v_end)
            end
        end
        if l < 0 then break end
        walk = v_off + l
    end

    -- Issue #59 part 3: non-qualified Parameter / Node / Matrix / Function
    -- carry `number` instead of `path`. At the root level of a Glow payload
    -- their OID is literally the number (children of device-root). Promote
    -- `number` → synthetic `path` so parts 1, 2, 4 (which key on path)
    -- pick them up. Nested non-qualified elements (rare in modern trees)
    -- still get the `#N` fallback in the render block below.
    if path == nil and number ~= nil then
        path = tostring(number)
    end

    -- Cache this leaf's (path, identifier) so subsequent frames
    -- referencing the same path — or children that include it as a
    -- prefix — can render the dotted identifier chain in Info. Issue #59
    -- part 1.
    if path and identifier then
        cache_identifier(path, identifier)
    end

    -- Issue #59 part 2: label parameters cache into the per-matrix
    -- label table. Strip surrounding quotes decode_value_field renders.
    if path and identifier and value and is_param then
        local parent_path, kind, idx = parse_label_path(path, identifier)
        if parent_path then
            local stripped = value:match('^"(.-)"$') or value
            cache_matrix_label(parent_path, kind, idx, stripped)
        end
    end

    local parts = {}
    if path then
        table.insert(parts, path)
        local chain = resolve_identifier_chain(path)
        if chain and chain ~= identifier then
            -- Show the resolved dotted chain only when it adds something
            -- beyond the leaf's own identifier (which is already emitted
            -- below). Avoids `1.2.3 'nToN' router.oneToN.nToN` noise.
            table.insert(parts, chain)
        end
    end
    if identifier then table.insert(parts, "'" .. identifier .. "'") end
    if access then table.insert(parts, access) end
    if value then
        -- Issue #59 part 4: diff against the last-seen value for this
        -- path. First sighting renders as plain `= X`; subsequent
        -- sightings of a different value render as `= new (was old)`.
        if path then
            local prev = note_value_diff(path, value)
            if prev and prev ~= value then
                table.insert(parts, "= " .. value .. " (was " .. prev .. ")")
            else
                table.insert(parts, "= " .. value)
            end
        else
            table.insert(parts, "= " .. value)
        end
    end
    if conn then
        -- Issue #59 part 2: look up target + first-source labels from the
        -- per-matrix cache populated earlier in this conversation. Keeps
        -- the positional form `t=X` / `[Y]` alongside the resolved label
        -- so users can cross-reference and we never lose information.
        local t_num = tonumber(conn.target)
        local t_lbl
        if path and t_num ~= nil then
            t_lbl = lookup_matrix_label(path, "targets", t_num)
        end
        local s_lbl
        if path and conn.sources then
            local s_first = tonumber(conn.sources:match("^%s*(%-?%d+)"))
            if s_first ~= nil then
                s_lbl = lookup_matrix_label(path, "sources", s_first)
            end
        end
        local tgt_part = t_lbl
            and string.format("target \"%s\" (t=%s)", t_lbl, tostring(conn.target or "?"))
            or string.format("t=%s", tostring(conn.target or "?"))
        local src_part = s_lbl
            and string.format("← source \"%s\" [%s]", s_lbl, tostring(conn.sources or ""))
            or string.format("← [%s]", tostring(conn.sources or ""))
        local cparts = { tgt_part .. " " .. src_part }
        if conn.op   then table.insert(cparts, conn.op)   end
        if conn.disp then table.insert(cparts, conn.disp) end
        table.insert(parts, table.concat(cparts, " "))
    end
    return table.concat(parts, " ")
end

-- Recursively count interesting leaf types in a Glow subtree so the
-- Info column identifies Matrix / Function / Parameter content even when
-- it's nested below Node wrappers. Also captures the first leaf's
-- detailed summary (path + identifier + value or connection).
local LEAF_TAGS = {
    [1] = "Parameter", [9]  = "QParameter",
    [13] = "Matrix",   [17] = "QMatrix",
    [19] = "Function", [20] = "QFunction",
    [24] = "Template", [25] = "QTemplate",
    [23] = "InvocationResult",
    -- Issue #59 part 1: include Node / QualifiedNode so their
    -- (path, identifier) populate the OID cache. Our provider (and every
    -- spec-compliant one) sends nested GetDirectory replies FLAT at the
    -- root — children of a node come as sibling root elements, not as
    -- nested children of a QNode. So treating Node / QNode as "leaves"
    -- here loses no nested-children count in practice, and gains the
    -- identifier resolution that would otherwise never reach the cache.
    [3]  = "Node",     [10] = "QNode",
}

local function count_leaves(ba, off, endpos, counts, highlight, depth)
    if depth > 12 then return end
    while off < endpos do
        local c, _cn, t, h, l = peek_app_tag(ba, off, endpos)
        if not c or l == nil then return end
        local val_off = off + h
        local val_end = (l < 0) and endpos or math.min(val_off + l, endpos)
        if c == 1 and LEAF_TAGS[t] then
            local name = LEAF_TAGS[t]
            counts[name] = (counts[name] or 0) + 1
            if not highlight.set then
                local s = element_summary(ba, t, val_off, val_end)
                highlight.set  = true
                highlight.kind = name
                highlight.text = (s ~= "") and (name .. " " .. s) or name
            else
                -- Issue #59 parts 1, 2, 4: element_summary populates the
                -- OID → identifier cache, the matrix-label cache, and the
                -- value-diff cache. Call it on every leaf (not just the
                -- first) so multi-leaf frames — label batches, value
                -- dumps — populate the caches for later frames to use.
                -- Return value discarded.
                element_summary(ba, t, val_off, val_end)
            end
        else
            count_leaves(ba, val_off, val_end, counts, highlight, depth + 1)
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
            -- ElementCollection: recursively count leaves AND capture the
            -- first leaf's content detail (path, identifier, value, conn).
            local child_end = inner_off + h3 + (l3 < 0 and (inner_end - inner_off - h3) or l3)
            local counts = {}
            local highlight = {}
            count_leaves(ba, inner_off + h3, child_end, counts, highlight, 0)
            local parts = {}
            for name, n in pairs(counts) do
                table.insert(parts, (n > 1 and (n .. " ") or "") .. name)
            end
            if #parts == 0 then return "Root { " .. top .. " [Node only] }" end
            local tail = ""
            if highlight.text then tail = " → " .. highlight.text end
            return "Root { " .. top .. " [" .. table.concat(parts, ", ") .. "] }" .. tail
        elseif t3 == 6 then
            -- StreamCollection: each entry is wrapped in [0] CONTEXT
            -- (StreamElement CHOICE) → APP 5 StreamEntry.
            local child_end = inner_off + h3 + (l3 < 0 and (inner_end - inner_off - h3) or l3)
            local walk = inner_off + h3
            local n = 0
            local first
            while walk < child_end do
                local cc, _cn, ct, ch, cl = peek_app_tag(ba, walk, child_end)
                if not cc or cl == nil or cl < 0 then break end
                local entry_off, entry_end = walk + ch, walk + ch + cl
                -- Step into [0] context wrapper to reach the APP 5 tag.
                if cc == 2 and ct == 0 then
                    local ec, _cn2, et, eh, el = peek_app_tag(ba, entry_off, entry_end)
                    if ec == 1 and et == 5 and el ~= nil and el >= 0 then
                        n = n + 1
                        if not first then
                            first = decode_stream_entry(ba, entry_off + eh, entry_off + eh + el)
                        end
                    end
                elseif cc == 1 and ct == 5 then
                    n = n + 1
                    if not first then
                        first = decode_stream_entry(ba, entry_off, entry_end)
                    end
                end
                walk = entry_off + cl
            end
            local s = "Root { StreamCollection [" .. n .. "] }"
            if first and first.id ~= nil then
                s = s .. string.format(" → #%s = %s", tostring(first.id),
                                       tostring(first.value or "?"))
            end
            return s
        elseif t3 == 23 then
            -- InvocationResult: show invocationID + success flag.
            local rend = inner_off + h3 + (l3 < 0 and (inner_end - inner_off - h3) or l3)
            local walk = inner_off + h3
            local inv_id, success
            while walk < rend do
                local cc, _cn, ct, ch, cl = peek_app_tag(ba, walk, rend)
                if not cc or cl == nil or cl < 0 then break end
                local v_off, v_end = walk + ch, walk + ch + cl
                if cc == 2 and ct == 0 then inv_id  = decode_integer_field(ba, v_off, v_end)
                elseif cc == 2 and ct == 1 then success = decode_bool_field(ba, v_off, v_end) end
                walk = v_off + cl
            end
            local extras = {}
            if inv_id  ~= nil then table.insert(extras, "id=" .. tostring(inv_id)) end
            if success ~= nil then table.insert(extras, success and "ok" or "FAIL") end
            return "Root { InvocationResult"
                .. (#extras > 0 and (" " .. table.concat(extras, " ")) or "") .. " }"
        else
            return "Root { " .. top .. " }"
        end
    end
    return "Root"
end

local function walk_ber(ba, unesc_tvb, off, avail, tree, scope, depth)
    -- Each wrapper level (application wrapper + [1]/[2] context + inner
    -- universal SET/SEQUENCE + child element) burns ~4 depth steps, so a
    -- modestly-nested Function invocation tree legitimately reaches 25-30.
    -- 40 is a safety net, not an expected limit.
    if depth > 40 then
        tree:add_expert_info(PI_MALFORMED, PI_WARN, "Glow tree recursion limit")
        return 0
    end
    local start = off
    -- Clamp endpos to the actual ByteArray length. Callers occasionally
    -- pass `avail` larger than what the buffer holds (truncated or
    -- shorter-than-declared assembled payloads), and every downstream
    -- ba:get_index that uses endpos as its bound would throw "index out
    -- of range". One clamp here covers every check inside walk_ber.
    local endpos = math.min(off + avail, ba:len())
    while off < endpos do
        local first, class, cons, tag_num, t_consumed = read_tag(ba, off)
        if not first then break end

        -- End-of-contents sentinel (for indefinite-length parents).
        if first == 0 and (off + 1) < endpos and (off + 1) < ba:len()
            and ba:get_index(off + 1) == 0 then
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
            -- Bound the scan by the actual ByteArray length as well as
            -- endpos — callers sometimes pass endpos > ba:len() when the
            -- assembled payload is shorter than declared, and ba:get_index
            -- throws "index out of range" rather than returning nil.
            local scan = value_off
            local closed = false
            local ba_len = ba:len()
            local ba_end = math.min(endpos, ba_len)
            while scan >= 0 and (scan + 1) < ba_end do
                -- Belt-and-braces: direct index check on each get_index
                -- call. Some Wireshark builds throw "index out of range"
                -- even when the loop bound arithmetic should prevent it
                -- (observed in live captures on Windows); a local guard
                -- costs nothing and eliminates the class of error.
                if scan >= ba_len or (scan + 1) >= ba_len then break end
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
            elseif class == 2 and scope == scope_command and tag_num == 0 then
                -- [0] number — mark child scope so INTEGER renderer can
                -- annotate with the CommandType name (Subscribe/Unsubscribe/
                -- GetDirectory/Invoke).
                child_scope = scope_command_number
            elseif class == 2 and scope == scope_invocation and (tag_num == 0 or tag_num == 1) then
                -- Inside Invocation, [0] is invocationID (keep scope),
                -- [1] is arguments Tuple — descend with no scope so tuple
                -- items are NOT mislabelled as invocationID.
                if tag_num == 1 then child_scope = nil else child_scope = scope end
            elseif class == 2 and scope == scope_invocation_result and (tag_num == 0 or tag_num == 1 or tag_num == 2) then
                -- [0] invocationID, [1] success — keep scope.
                -- [2] result Tuple — drop scope. (Can't use `x and nil or scope`
                -- ternary — Lua's nil short-circuits `and`, so it resolves to
                -- scope instead of nil.)
                if tag_num == 2 then child_scope = nil else child_scope = scope end
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
                    -- If this is Command.number, annotate with the CommandType
                    -- enum name (Subscribe / Unsubscribe / GetDirectory / Invoke).
                    if scope == scope_command_number then
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

            -- Multi-packet S101 reassembly. Fragment accumulator runs only on
            -- first dissection pass; re-dissection (scroll / filter) reads
            -- from the per-packet cache.
            --
            -- Flag bits are independent: FIRST (0x80) and LAST (0x40) mark
            -- the sequence boundaries, EMPTY (0x20) is an orthogonal "no
            -- payload" marker that can combine with any boundary. Strict
            -- equality on the whole byte misses e.g. LAST+EMPTY (0x60)
            -- which providers use to close a multi-packet sequence with
            -- an empty tail frame. Use bitmask checks instead.
            local key = s101_conv_key(pktinfo)
            local has_first = band(flags, FLAG_FIRST) ~= 0
            local has_last  = band(flags, FLAG_LAST)  ~= 0
            local append_payload = function(buf)
                if payload_len > 0 then
                    buf:append(unesc_bytes:subset(payload_off, payload_len))
                end
            end
            if not pktinfo.visited then
                if has_first and has_last then
                    -- Single-packet message (FIRST+LAST). Payload may be
                    -- empty (FLAG_EMPTY additionally set) which is still a
                    -- valid complete message — e.g. a bare keep-alive.
                    s101_packet_cache[pktinfo.number] = {
                        assembled = true,
                        payload   = (payload_len > 0) and unesc_bytes:subset(payload_off, payload_len) or ByteArray.new(),
                    }
                elseif has_first then
                    local frag = (payload_len > 0) and unesc_bytes:subset(payload_off, payload_len) or ByteArray.new()
                    s101_reassembly_state[key] = { payload = frag }
                    s101_packet_cache[pktinfo.number] = { fragment_kind = "first" }
                elseif has_last then
                    local rb = s101_reassembly_state[key]
                    if rb then
                        append_payload(rb.payload)
                        s101_packet_cache[pktinfo.number] = { assembled = true, payload = rb.payload }
                        s101_reassembly_state[key] = nil
                    else
                        s101_packet_cache[pktinfo.number] = { fragment_kind = "last (orphan)" }
                    end
                else
                    -- Middle fragment — append to the buffer if we have one.
                    local rb = s101_reassembly_state[key]
                    if rb then
                        append_payload(rb.payload)
                        s101_packet_cache[pktinfo.number] = { fragment_kind = "middle" }
                    else
                        s101_packet_cache[pktinfo.number] = { fragment_kind = "orphan" }
                    end
                end
            end

            local cache     = s101_packet_cache[pktinfo.number]
            local flag_name = s101_flags_valstr[flags] or string.format("flags=0x%02X", flags)

            if cache and cache.assembled then
                local full     = cache.payload
                local full_len = full:len()
                -- Publish the conversation key for summarize_glow and its
                -- descendants (element_summary) to use when caching /
                -- resolving OID→identifier chains. Issue #59 part 1.
                emberplus_current_conv_key = s101_conv_key(pktinfo)
                local summary  = (full_len > 0) and summarize_glow(full, 0, full_len) or nil
                local label    = string.format("Glow Payload (%d bytes reassembled)%s",
                                               full_len, summary and (" — " .. summary) or "")
                local glow_tree
                if payload_len > 0 then
                    glow_tree = tree:add(glow_proto, unesc_tvb:range(payload_off, payload_len), label)
                else
                    glow_tree = tree:add(glow_proto, label)
                end
                if full_len > 0 then
                    local full_tvb = full:tvb("Reassembled Glow")
                    walk_ber(full, full_tvb, 0, full_len, glow_tree, nil, 0)
                end
                info = "Ember+ " .. flag_name .. (summary and (" " .. summary) or "")
                    .. string.format(" payload=%dB", full_len)
            else
                local kind = (cache and cache.fragment_kind) or "unknown"
                if payload_len > 0 then
                    tree:add(glow_proto, unesc_tvb:range(payload_off, payload_len),
                             string.format("Ember+ fragment (%s) — %d bytes pending reassembly", kind, payload_len))
                end
                info = string.format("Ember+ %s fragment %s (%dB)", flag_name, kind, payload_len)
            end
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
    -- find EoF, skipping escape sequences. A literal 0xFE inside a
    -- frame is a spec violation (S101 mandates 0xFE escape-stuffing as
    -- 0xFD 0xDE) but Lawo VSM-as-consumer emits a 15-byte non-S101
    -- preamble before its first real frame on every reconnect. Resync
    -- on the second BoF so we report CRC over the right bytes instead
    -- of failing CRC over preamble + real frame concatenated.
    local i = offset + 1
    while i < pktlen do
        local b = tvbuf:range(i, 1):uint()
        if b == S101_ESC then
            i = i + 2
        elseif b == S101_BOF then
            start = i
            i = i + 1
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

-- Reset per-capture-file state. Wireshark calls this before a new capture
-- file is loaded so packet-number caches from the previous file don't leak.
function s101_proto.init()
    s101_reassembly_state = {}
    s101_packet_cache     = {}
end

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

    -- Bounded EoF scan, skipping escape sequences. Resync on a second
    -- BoF mid-stream — Lawo VSM-as-consumer emits a 15-byte non-S101
    -- preamble before its first real frame; without resync the
    -- heuristic mis-validates against the preamble bytes and never
    -- claims the stream.
    local scan_limit = math.min(len, 4096)
    local bof_at = 1
    local eof_at
    local i = 1
    while i < scan_limit do
        local b = tvbuf:range(i, 1):uint()
        if b == S101_ESC then
            i = i + 2
        elseif b == S101_BOF then
            bof_at = i + 1
            i = i + 1
        elseif b == S101_EOF then
            eof_at = i
            break
        else
            i = i + 1
        end
    end
    if not eof_at then return false end

    -- Validate S101 header after unescape.
    local unesc = unescape_s101(tvbuf, bof_at, eof_at)
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
