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
--   * The router command space, in all four of the node types that carry one:
--     the Full Control tables on the panel node, the level command set on
--     each Router Level, the tieline node, and the matrix node that carries
--     none. A number means nothing until the node is known — 100 is the
--     interface version to one and the selected destination to another — so
--     each node's type is taken from the RETID it answers with and every
--     command is named through it.
--   * The tables above command 119, which have no fixed numbers at all: the
--     bases and steps a controller publishes are remembered as the capture
--     goes past, so a crosspoint reads as "matrix 1 level 2 destination 40"
--     rather than as command 2043. Nothing is guessed; a command in no
--     learned table is shown as unresolved.
--   * Data Transfer Params, including the uint arrays a route travels in and
--     the string-safe escaping a block wears when it rides in a string. Where
--     the command says what its parameters mean they are named: a crosspoint
--     set reads as a source pin and a result, a salvo as a salvo and a count.
--
-- The Info column names the message and the arguments that identify it: the
-- session index, the command number, the slot, the file, the crosspoint. Two
-- frames that differ in what they do differ in the Info column.
--
-- Byte authority: internal/snell-rollcall/codec (frame.go, addr.go, pkttype.go,
-- payload_*.go, router/, dtp/) and the vendor headers those cite. Behaviour
-- measured against the vendor Centra controller:
-- internal/snell-rollcall/docs/oracle-centra.md.
--
-- What has been read off a wire, and what has only been read off the
-- specification, is worth separating. Measured against live captures: both
-- generations, the four node types, the learned matrix / level / source /
-- destination / category / group tables, crosspoint read, set and the reply
-- that carries the pin from before, protect, salvo firing, the level command
-- set including the direct routing and reference blocks, and the tieline node.
-- Decoded from the specification and this repository's own encoder, but not
-- yet seen on a wire because no client here sends one: CMD_ASSOC_MAKE_ROUTE
-- and CMD_GET_AHP_NODE.
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

-------------------------------------------------------------------------------
-- The router command space
--
-- A router is not one node. Measured on a vendor Centra, a plant appears as a
-- Router Matrix per matrix, a Router Level per level beneath it, a TIELINES
-- node, and one node serving the Full Control tables:
--
--   0000-11-00  636  Router Matrix  "Matrix 1"
--   0000-11-01  637  Router Level   "Level 1"
--   0000-80-00  731  TIELINES
--   0000-81-00  734  XY Panel        the Full Control command set
--
-- The four use overlapping numbers for unrelated things. Command 100 is the
-- interface version on the Full Control node and the selected destination on a
-- level, and nothing in the bytes tells them apart — only the type of the node
-- the command was asked of. So this dissector learns each node's type from the
-- RETID it answers with, and names commands accordingly. Until a node has
-- identified itself its numbers are shown as unresolved rather than guessed.
-------------------------------------------------------------------------------

-- Unit types that carry a command space of their own (codec/unittype.go).
local ROUTER_MATRIX, ROUTER_LEVEL, TIELINES, XY_PANEL = 636, 637, 731, 734

local router_node_type = {
    [ROUTER_MATRIX] = "Router Matrix",
    [ROUTER_LEVEL]  = "Router Level",
    [TIELINES]      = "TIELINES",
    [XY_PANEL]      = "XY Panel (Full Control)",
}

-- The routing interface's fixed root (Full Control Command Set §Routing
-- Interface). These are the only numbers in the set the specification pins;
-- everything above them is arithmetic from values these publish, which is why
-- the tables below are learned from the capture rather than tabulated here.
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

-- Offsets inside each sub-table, mirrored from codec/router/commands.go. The
-- role beside each name is what this dissector does with the value: `base`,
-- `step` and `count` are learned, so that the next table down can be resolved.
local matrix_field = {
    [0]  = { "MatrixName" },
    [1]  = { "NumLevels", role = "count", of = "levels" },
    [2]  = { "LevelBase", role = "base", of = "levels" },
    [3]  = { "LevelStep", role = "step", of = "levels" },
    [4]  = { "NumSrcAssocs", role = "count", of = "srcassoc" },
    [5]  = { "SrcAssocBase", role = "base", of = "srcassoc" },
    [6]  = { "SrcAssocStep", role = "step", of = "srcassoc" },
    [7]  = { "NumDstAssocs", role = "count", of = "dstassoc" },
    [8]  = { "DstAssocBase", role = "base", of = "dstassoc" },
    [9]  = { "DstAssocStep", role = "step", of = "dstassoc" },
    [10] = { "AssocNames8File", role = "namesfile" },
    [11] = { "AssocNames32File", role = "namesfile" },
    [12] = { "AssocNamesAltFile", role = "namesfile" },
    [13] = { "ControllerNumber" },
    [14] = { "AssocMappingsFile", role = "namesfile" },
}

local level_field = {
    [0]  = { "LevelName" },
    [1]  = { "LevelType" },
    [2]  = { "NumSrcs", role = "count", of = "srcs" },
    [3]  = { "SrcBase", role = "base", of = "srcs" },
    [4]  = { "SrcStep", role = "step", of = "srcs" },
    [5]  = { "NumDsts", role = "count", of = "dsts" },
    [6]  = { "DstBase", role = "base", of = "dsts" },
    [7]  = { "DstStep", role = "step", of = "dsts" },
    [8]  = { "SrcDstNames8File", role = "namesfile" },
    [9]  = { "SrcDstNames32File", role = "namesfile" },
    [10] = { "SrcDstNamesAltFile", role = "namesfile" },
    [11] = { "SrcDstMCDataFile", role = "namesfile" },
}

local source_field = {
    [0] = { "SrcName8" }, [1] = { "SrcName32" }, [2] = { "SrcAltName" },
}

-- A destination's routed source is the busiest command on a live router, and
-- the one worth decoding properly: it carries a packed source pin and the
-- result of the last set, as Data Transfer Params rather than as a number.
local dest_field = {
    [0] = { "DestName8" },
    [1] = { "DestName32" },
    [2] = { "DestAltName" },
    [3] = { "DestRoutedSrc", role = "crosspoint" },
    [4] = { "DestProtect", role = "protect" },
    [5] = { "DestMCSrcs" },
    [6] = { "DestMCProtects" },
    [7] = { "DestMCProtect", role = "protect" },
}

local category_field = {
    [0] = { "CategoryName" },
    [1] = { "CategoryExclusive" },
    [2] = { "CategorySortIndex" },
    [3] = { "NumGroups", role = "count", of = "groups" },
    [4] = { "GroupBase", role = "base", of = "groups" },
    [5] = { "GroupStep", role = "step", of = "groups" },
}

local group_field = {
    [0] = { "GroupName" }, [1] = { "GroupSearchString" }, [2] = { "GroupSearchStart" },
}

-- An association's table is as long as its matrix has levels: three fixed
-- entries, then one entity per level (codec/router/assoc.go).
local assoc_field = {
    [0] = { "AssocName8" }, [1] = { "AssocName32" }, [2] = { "AssocAltName" },
}

-- The command set a Router Level serves (codec/router/level.go). This is not
-- the Full Control set: the two agree on nothing and overlap in every number
-- they use.
local level_command = {
    [100] = "SelectedDestination", [101] = "SrcNameIndex", [102] = "DstNameIndex",
    [110] = "SelectedSource",      [111] = "SrcName",      [112] = "DstName",
    [113] = "DestProtect",
    [120] = "TakeMode",            [121] = "Take",         [122] = "Cancel",
    [130] = "SourceCount",         [131] = "DestCount",
}

-- The monitor readouts, four of each, addressed as base + monitor number.
local level_monitor_base = {
    [400] = "MonitorKind", [410] = "MonitorIndex", [420] = "MonitorName",
    [430] = "MonitorSourceAddress", [440] = "MonitorDestAddress",
}

local LVL_ROUTE_BASE, LVL_PROTECT_BASE, LVL_REFSRC_BASE = 10000, 20000, 30000

-- The TIELINES node (codec type 731). Its numbers were read off a Centra's own
-- menu; they collide with nothing because nothing else lives on that node.
local tieline_command = {
    [300] = "MakeRoute", [302] = "Status", [303] = "SelectedTieline",
    [304] = "Clear",     [306] = "UsedBy",
}

-- Commands this connector's own provider adds to the Full Control node, above
-- everything the specification allocates. They are ours, not the vendor's, and
-- are named as such so a capture of our provider does not read as a capture of
-- a Centra (provider/template_router.go).
local dhs_panel_command = {
    [99]    = "Status (dhs)",
    [90000] = "LastSalvo (dhs)", [90001] = "SelectedSalvo (dhs)",
}
local DHS_CATEGORY_BASE, DHS_GROUP_SELECT, DHS_GROUP_MATCH = 91000, 92000, 93000

-- What a controller answers a route request with (codec/router/pin.go).
local route_result = {
    [0] = "ok", [1] = "controller idle", [2] = "source or destination not installed",
    [3] = "route inhibited", [4] = "destination protected",
    [5] = "source or destination in use", [6] = "no tieline available",
    [7] = "configuration error", [8] = "invalid parameters", [9] = "not connected",
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
    dtp_string_safe = ProtoField.bool("dhs_snell_rollcall.dtp.string_safe", "String-safe"),
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
    router_node = ProtoField.string("dhs_snell_rollcall.router_node", "Node type"),
    router_matrix = ProtoField.uint32("dhs_snell_rollcall.matrix", "Matrix", base.DEC),
    router_level = ProtoField.uint32("dhs_snell_rollcall.level", "Level", base.DEC),
    router_source = ProtoField.uint32("dhs_snell_rollcall.source", "Source", base.DEC),
    router_dest = ProtoField.uint32("dhs_snell_rollcall.destination", "Destination", base.DEC),
    router_category = ProtoField.uint32("dhs_snell_rollcall.category", "Category", base.DEC),
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

local function address_fields(tvb, off)
    return {
        net = tvb(off, 2):uint(),
        unit = tvb(off + 2, 1):uint(),
        port = tvb(off + 3, 1):uint(),
        index = tvb(off + 4, 2):uint(),
    }
end

local function address_string(tvb, off)
    local a = address_fields(tvb, off)
    return string.format("%04X-%02X-%02X:%s", a.net, a.unit, a.port, index_name(a.index))
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
    if source == 0 then
        return "unrouted", matrix, level, source
    end
    return string.format("m%d/l%d/s%d", matrix, level, source), matrix, level, source
end

-- protect_string reads the protect word: a flag, the id of the panel holding
-- it, and whether that panel is a master (codec/router/pin.go).
local function protect_string(v)
    if bit.band(v, 1) == 0 then
        return "unprotected"
    end
    local id = bit.band(bit.rshift(v, 8), 0xFFFF)
    local s = string.format("protected by %d", id)
    if bit.band(bit.rshift(v, 24), 1) == 1 then
        s = s .. " (master)"
    end
    return s
end

local function result_string(v)
    return route_result[v] or string.format("result(%d)", v)
end

-------------------------------------------------------------------------------
-- What a node is, and what its numbers mean
--
-- Above command 119 the Full Control command space has no fixed numbers at
-- all: a controller publishes a base and a step for matrices, each matrix
-- publishes one for its levels, each level one for its sources and
-- destinations. A single frame therefore cannot say what command 2043 is.
--
-- A capture can. The client reads those bases and steps before it reads
-- anything addressed by them, so a dissector that remembers the replies knows,
-- by the time the crosspoints go past, that 2043 is the routed source of
-- destination 40 on level 2 of matrix 1. That is what this section does, and
-- it is the difference between a capture of a router that reads as numbers and
-- one that reads as a plant.
--
-- Nothing is guessed. A command that falls in no learned table is shown as
-- unresolved, and a node that has not identified itself has no command space.
-------------------------------------------------------------------------------

-- Learned facts, and what a command resolved to the first time it was seen.
-- Both are cleared whenever Wireshark starts a capture or reloads a file.
local nodes = {}
local resolved_cache = {}

local function addr_key(a)
    return string.format("%04X-%02X-%02X", a.net, a.unit, a.port)
end

-- Which types name the node in Src rather than in Dst: everything a device
-- answers with. A capture may hold two plants, and unit 11 in one is not unit
-- 11 in the other, so what is learned is filed per TCP conversation as well as
-- per address. The key is the same read from either direction.
local node_is_src = {
    [5] = true, [7] = true, [9] = true, [12] = true, [20] = true, [33] = true,
    [66] = true, [68] = true, [71] = true,
}

local function conv_key(pinfo)
    local a = string.format("%s:%s", tostring(pinfo.src), tostring(pinfo.src_port))
    local b = string.format("%s:%s", tostring(pinfo.dst), tostring(pinfo.dst_port))
    if a < b then
        return a .. "|" .. b
    end
    return b .. "|" .. a
end

local function node_state(ctx, a)
    local key = ctx.conv .. "#" .. addr_key(a)
    local n = nodes[key]
    if n == nil then
        n = { mtx = {}, lvl = {}, cat = {}, matrices = {}, categories = {} }
        nodes[key] = n
    end
    return n
end

-- span reports which entity of a learned table a command belongs to, counting
-- from one, and its offset within that entity.
--
-- A table with no count is not searched. A controller that has published a
-- base and a step but not yet how many there are could be asked about anything
-- above the base, and answering "destination 4 million" to a stray number is
-- worse than answering nothing.
local function span(t, cmd)
    if not t or not t.base or not t.step or t.step == 0 or not t.count or t.count == 0 then
        return nil
    end
    if cmd < t.base or cmd >= t.base + t.count * t.step then
        return nil
    end
    local d = cmd - t.base
    return math.floor(d / t.step) + 1, d % t.step
end

local function sub(parent, name)
    if parent[name] == nil then
        parent[name] = {}
    end
    return parent[name]
end

-- field_result turns a table entry into what the rest of the dissector needs:
-- what to call the command, what the value means, and where to file it if the
-- value is one of the bases, steps or counts the next table down is built on.
local function field_result(where, fields, offset, bucket_owner)
    local fld = fields[offset]
    if fld == nil then
        return { what = string.format("%s offset %d", where, offset), scope = where }
    end
    local r = { what = where .. " " .. fld[1], scope = where, role = fld.role }
    if fld.of and bucket_owner then
        r.bucket = sub(bucket_owner, fld.of)
    end
    return r
end

-- resolve_full_control names a command on the node that serves the Full
-- Control tables, using everything that node has said so far.
local function resolve_full_control(node, cmd)
    if router_command[cmd] then
        local r = { what = router_command[cmd], scope = "routing interface" }
        if cmd == 102 then r.role, r.bucket = "count", node.matrices end
        if cmd == 103 then r.role, r.bucket = "base", node.matrices end
        if cmd == 104 then r.role, r.bucket = "step", node.matrices end
        if cmd == 105 then r.role, r.bucket = "count", node.categories end
        if cmd == 106 then r.role, r.bucket = "base", node.categories end
        if cmd == 107 then r.role, r.bucket = "step", node.categories end
        if cmd == 108 then r.role = "makeroute" end
        if cmd == 116 then r.role = "firesalvo" end
        if cmd == 119 then r.role = "ahpnode" end
        if cmd == 114 or cmd == 115 or cmd == 118 then r.role = "namesfile" end
        return r
    end

    local m, mo = span(node.matrices, cmd)
    if m then
        local mst = sub(node.mtx, m)
        local r = field_result(string.format("matrix %d", m), matrix_field, mo, mst)
        r.matrix = m
        return r
    end

    for mi, mst in pairs(node.mtx) do
        local l, lo = span(mst.levels, cmd)
        if l then
            local lst = sub(node.lvl, mi * 1000 + l)
            lst.matrix, lst.level = mi, l
            local r = field_result(string.format("matrix %d level %d", mi, l),
                level_field, lo, lst)
            r.matrix, r.level = mi, l
            return r
        end
        for _, kind in ipairs({ "srcassoc", "dstassoc" }) do
            local a, ao = span(mst[kind], cmd)
            if a then
                local what = string.format("matrix %d %s association %d", mi,
                    kind == "srcassoc" and "source" or "destination", a)
                if ao >= 3 then
                    -- After the three names comes one entity per level, so the
                    -- offset is the level number.
                    return { what = string.format("%s on level %d", what, ao - 2),
                        scope = what, matrix = mi }
                end
                local r = field_result(what, assoc_field, ao, nil)
                r.matrix = mi
                return r
            end
        end
    end

    for _, lst in pairs(node.lvl) do
        local s, so = span(lst.srcs, cmd)
        if s then
            local r = field_result(string.format("matrix %d level %d source %d",
                lst.matrix, lst.level, s), source_field, so, nil)
            r.matrix, r.level, r.source = lst.matrix, lst.level, s
            return r
        end
        local d, dof = span(lst.dsts, cmd)
        if d then
            local r = field_result(string.format("matrix %d level %d destination %d",
                lst.matrix, lst.level, d), dest_field, dof, nil)
            r.matrix, r.level, r.dest = lst.matrix, lst.level, d
            return r
        end
    end

    local c, co = span(node.categories, cmd)
    if c then
        local cst = sub(node.cat, c)
        local r = field_result(string.format("category %d", c), category_field, co, cst)
        r.category = c
        return r
    end
    for ci, cst in pairs(node.cat) do
        local g, go = span(cst.groups, cmd)
        if g then
            local r = field_result(string.format("category %d group %d", ci, g),
                group_field, go, nil)
            r.category = ci
            return r
        end
    end

    -- Commands this connector's own provider adds above the specification.
    if dhs_panel_command[cmd] then
        return { what = dhs_panel_command[cmd], scope = "dhs provider" }
    end
    if cmd >= DHS_CATEGORY_BASE and cmd < DHS_GROUP_SELECT then
        return { what = string.format("category %d selection (dhs)", cmd - DHS_CATEGORY_BASE),
            scope = "dhs provider" }
    end
    if cmd >= DHS_GROUP_SELECT and cmd < DHS_GROUP_MATCH then
        return { what = string.format("category %d group (dhs)", cmd - DHS_GROUP_SELECT),
            scope = "dhs provider" }
    end
    if cmd >= DHS_GROUP_MATCH and cmd < DHS_GROUP_MATCH + 1000 then
        return { what = string.format("category %d matches (dhs)", cmd - DHS_GROUP_MATCH),
            scope = "dhs provider" }
    end

    if cmd >= 120 then
        return { what = "unresolved — no table published for it yet",
            scope = "routing interface", unresolved = true }
    end
    return nil
end

-- resolve_level names a command on a Router Level, whose set is fixed and
-- needs nothing learned.
local function resolve_level(cmd)
    if level_command[cmd] then
        local r = { what = level_command[cmd], scope = "level" }
        if cmd == 113 then r.role = "checkbox" end
        return r
    end
    for base, name in pairs(level_monitor_base) do
        if cmd > base and cmd <= base + 4 then
            return { what = string.format("%s %d", name, cmd - base), scope = "level" }
        end
    end
    if cmd > LVL_ROUTE_BASE and cmd < LVL_PROTECT_BASE then
        return { what = string.format("routed source of destination %d", cmd - LVL_ROUTE_BASE),
            scope = "level", role = "levelroute", dest = cmd - LVL_ROUTE_BASE }
    end
    if cmd > LVL_PROTECT_BASE and cmd < LVL_REFSRC_BASE then
        return { what = string.format("protect of destination %d", cmd - LVL_PROTECT_BASE),
            scope = "level", role = "protect", dest = cmd - LVL_PROTECT_BASE }
    end
    if cmd > LVL_REFSRC_BASE and cmd < LVL_REFSRC_BASE + 10000 then
        return { what = string.format("reference of source %d", cmd - LVL_REFSRC_BASE),
            scope = "level", source = cmd - LVL_REFSRC_BASE }
    end
    return nil
end

local function resolve_for_node(node, cmd)
    if node.type_id == XY_PANEL then
        return resolve_full_control(node, cmd)
    end
    if node.type_id == ROUTER_LEVEL then
        return resolve_level(cmd)
    end
    if node.type_id == TIELINES then
        if tieline_command[cmd] then
            return { what = tieline_command[cmd], scope = "tielines" }
        end
        return nil
    end
    if node.type_id == ROUTER_MATRIX then
        -- A matrix node controls nothing; it exists so a client walking the
        -- plant finds a matrix and the levels beneath it.
        return nil
    end
    -- The node has not said what it is. The root numbers still mean what the
    -- specification says they mean on whatever node serves them, but on a
    -- level they mean something else entirely, so the ambiguity is stated
    -- rather than resolved away.
    if router_command[cmd] and level_command[cmd] then
        return { what = string.format("%s, or %s on a level", router_command[cmd],
            level_command[cmd]), scope = "unidentified node", ambiguous = true }
    end
    if router_command[cmd] then
        return { what = router_command[cmd], scope = "unidentified node" }
    end
    return nil
end

-- resolve answers once per frame and command and then repeats itself.
--
-- Wireshark dissects a frame more than once, and by the second pass the table
-- above holds everything the whole capture said — including things said after
-- the frame being drawn. Answering from a cache filled on first sight keeps a
-- frame showing what was knowable when it arrived, which is also what a client
-- reading the stream would have known.
local function resolve(ctx, cmd)
    if ctx == nil or ctx.node == nil then
        return nil
    end
    local key = string.format("%d:%s:%d", ctx.frame, ctx.node_key, cmd)
    local hit = resolved_cache[key]
    if hit ~= nil then
        if hit == false then return nil end
        return hit
    end
    local res = resolve_for_node(ctx.node, cmd)
    resolved_cache[key] = res or false
    return res
end

-- learn files a value away when it is one of the numbers the tables below it
-- are addressed by. Only ever called on the first pass over a frame.
local function learn(ctx, res, value)
    if res == nil or res.bucket == nil or res.role == nil or value == nil then
        return
    end
    if value < 0 then
        return
    end
    if res.role == "base" then res.bucket.base = value end
    if res.role == "step" then res.bucket.step = value end
    if res.role == "count" then res.bucket.count = value end
end

-- annotate_command puts a name and, where the tables allow it, a plant address
-- against a command number.
--
-- The name comes from the node the command was asked of: the same number means
-- different things on a level, on the node serving the Full Control tables and
-- on the tielines, and this is the only place that difference is decided.
local function annotate_command(tree, ctx, range, command)
    local res = resolve(ctx, command)
    if res == nil then
        return nil
    end
    local item = tree:add(f.router_cmd, range, res.what)
    -- The scope says which command space named it, which is worth stating
    -- except when the name already begins with it.
    if res.scope and res.what:sub(1, #res.scope) ~= res.scope then
        item:append_text(string.format(" [%s]", res.scope))
    end
    if res.matrix then item:add(f.router_matrix, range, res.matrix) end
    if res.level then item:add(f.router_level, range, res.level) end
    if res.source then item:add(f.router_source, range, res.source) end
    if res.dest then item:add(f.router_dest, range, res.dest) end
    if res.category then item:add(f.router_category, range, res.category) end
    return res
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

-- A string-safe block carries no interior NUL, because the vendor's own code
-- copies string parameters with strcpy. Bit 7 of the count byte says so, and
-- the body has to be put back before any of it can be read: 0xFF 0xFD stands
-- for 0x00 and 0xFF 0xFE for 0xFF, with the first and last bytes left alone.
local STRING_SAFE = 0x80

local function dtp_unescape(tvb)
    local out = ByteArray.new()
    local n = tvb:len()
    out:set_size(0)
    local i = 0
    while i < n do
        local b = tvb(i, 1):uint()
        if b == 0xFF and i < n - 1 then
            local nxt = tvb(i + 1, 1):uint()
            if nxt == 0xFD then
                out:append(ByteArray.new("00"))
                i = i + 2
            elseif nxt == 0xFE then
                out:append(ByteArray.new("ff"))
                i = i + 2
            else
                return nil
            end
        else
            out:append(tvb(i, 1):bytes())
            i = i + 1
        end
    end
    return out
end

-- dtp_meaning says what the parameters of one command are, so that a block of
-- numbers reads as a route rather than as a block of numbers.
--
-- The shapes come from codec/router: assoc.go for a route, salvo.go for a
-- salvo, pin.go for a crosspoint and a protect. Where there is no shape the
-- items are shown as themselves, which is what the format asks of a reader
-- that meets more than it expects.
local function dtp_meaning(res, reply)
    if res == nil or res.role == nil then
        return nil
    end
    local role = res.role
    if role == "makeroute" then
        if reply then
            return { { name = "result", fmt = "result" } }
        end
        return {
            { name = "route", labels = { "destination matrix", "destination association",
                "source matrix", "source association" } },
            { name = "levels" },
        }
    end
    if role == "firesalvo" then
        if reply then
            return { { name = "salvo" }, { name = "routes made" } }
        end
        return { { name = "salvo" } }
    end
    if role == "ahpnode" then
        if reply then
            return { { name = "result", fmt = "result" }, { name = "nodes", quad = true } }
        end
        return {
            { name = "entity", labels = { "matrix", "level", "source or destination" } },
            { name = "is destination" }, { name = "is audio track" },
        }
    end
    if role == "crosspoint" or role == "levelroute" then
        -- A read carries what is routed; a set's reply carries what was routed
        -- before it, plus how the set went. Either way the first item is a
        -- packed source pin and the second is the result.
        return { { name = "source", fmt = "pin" }, { name = "result", fmt = "result" } }
    end
    if role == "protect" then
        return { { name = "protect", fmt = "protect" }, { name = "result", fmt = "result" } }
    end
    if role == "namesfile" then
        return { { name = "filename" }, { name = "checksum" } }
    end
    return nil
end

local function dtp_format(fmt, v)
    if fmt == "pin" then return pin_string(v) end
    if fmt == "result" then return result_string(v) end
    if fmt == "protect" then return protect_string(v) end
    return string.format("%d", v)
end

-- dtp_dissect walks the params and returns a one-line summary for the Info
-- column, which is where they earn their keep.
--
-- `spec`, when given, names each item: a block that reads
-- "destination matrix=1 destination association=4 levels=bitmap(2)" is a route
-- request that can be checked by eye against what the operator asked for.
local function dtp_dissect(tvb, tree, spec)
    if tvb:len() < 1 then
        return nil
    end

    local head = tvb(0, 1):uint()
    local count = bit.band(head, 0x7F)
    local body = tvb
    local safe = bit.band(head, STRING_SAFE) ~= 0

    local subtree = tree:add(p_rc, tvb(), "Data Transfer Params")
    subtree:add(f.dtp_count, tvb(0, 1), count)
    if safe then
        -- The escaping covers everything after the count byte.
        subtree:add(f.dtp_string_safe, tvb(0, 1), true)
        if tvb:len() < 2 then
            return nil
        end
        local plain = dtp_unescape(tvb(1, tvb:len() - 1))
        if plain == nil then
            subtree:add_proto_expert_info(ef_bad_dtp)
            return nil
        end
        body = plain:tvb("Unescaped Data Transfer Params")
        subtree = subtree:add(p_rc, body(), "Unescaped")
    end

    -- With a string-safe block the item stream starts at the beginning of the
    -- unescaped body; otherwise it follows the count byte.
    local off = safe and 0 or 1
    local parts = {}
    local slot = 0

    local function label_of()
        slot = slot + 1
        if spec and spec[slot] then
            return spec[slot]
        end
        return nil
    end

    for _ = 1, count do
        if off >= body:len() then
            subtree:add_proto_expert_info(ef_bad_dtp)
            break
        end
        local itype = body(off, 1):uint()
        local start = off
        off = off + 1
        local want = label_of()

        if itype == 5 then
            local v, used = dtp_varint(body, off)
            if v == nil then
                subtree:add_proto_expert_info(ef_bad_dtp)
                break
            end
            off = off + used
            local shown = want and want.fmt and dtp_format(want.fmt, v) or tostring(v)
            local item = subtree:add(f.dtp_uint, body(start, off - start), v)
            item:set_text(string.format("%s: %s (0x%08X)",
                want and want.name or "uint", shown, v))
            if want and want.fmt == "pin" then
                local _, mx, lv, sr = pin_string(v)
                item:add(f.pin_matrix, body(start, off - start), mx)
                item:add(f.pin_level, body(start, off - start), lv)
                item:add(f.pin_source, body(start, off - start), sr)
            end
            if want and want.fmt == "result" then
                subtree:add(f.route_result, body(start, off - start), v)
                    :append_text(" (" .. result_string(v) .. ")")
            end
            table.insert(parts, want and string.format("%s=%s", want.name, shown) or shown)
        elseif itype == 1 then
            -- uint-array: a count of elements, then that many varints. The
            -- array is how every multi-part address on this interface travels
            -- — a route names four numbers, an AHP node reply four per node.
            local n, used = dtp_varint(body, off)
            if n == nil then
                subtree:add_proto_expert_info(ef_bad_dtp)
                break
            end
            off = off + used
            local vals = {}
            local ok = true
            for _ = 1, n do
                local v, u = dtp_varint(body, off)
                if v == nil then
                    subtree:add_proto_expert_info(ef_bad_dtp)
                    ok = false
                    break
                end
                off = off + u
                table.insert(vals, v)
            end
            if not ok then break end

            local array = subtree:add(p_rc, body(start, off - start),
                string.format("%s: %d values", want and want.name or "uint-array", n))
            local shown = {}
            if want and want.quad then
                -- Four numbers per node: type, unit, port, offset.
                for i = 1, #vals, 4 do
                    if i + 3 <= #vals then
                        local text = string.format("type %d at %02X-%02X+%d",
                            vals[i], vals[i + 1], vals[i + 2], vals[i + 3])
                        array:add(p_rc, body(start, off - start), text)
                        table.insert(shown, text)
                    end
                end
            else
                for i, v in ipairs(vals) do
                    -- A single-element array is how a crosspoint set travels,
                    -- so an array carries the same meanings a lone uint does.
                    local nm = (want and want.labels and want.labels[i])
                        or (want and not want.labels and want.name)
                        or string.format("[%d]", i)
                    local text = want and want.fmt and dtp_format(want.fmt, v) or tostring(v)
                    array:add(f.dtp_uint, body(start, off - start), v):set_text(
                        string.format("%s: %s", nm, text))
                    if want and want.fmt == "pin" then
                        local _, mx, lv, sr = pin_string(v)
                        array:add(f.pin_matrix, body(start, off - start), mx)
                        array:add(f.pin_level, body(start, off - start), lv)
                        array:add(f.pin_source, body(start, off - start), sr)
                    end
                    table.insert(shown, string.format("%s=%s", nm, text))
                end
            end
            table.insert(parts, table.concat(shown, " "))
        elseif itype == 6 then
            -- string: a length then the bytes.
            local n, used = dtp_varint(body, off)
            if n == nil or off + used + n > body:len() then
                subtree:add_proto_expert_info(ef_bad_dtp)
                break
            end
            off = off + used
            local s = body(off, n):string()
            local item = subtree:add(f.dtp_str, body(start, off + n - start), s)
            if want then item:set_text(string.format("%s: %s", want.name, s)) end
            off = off + n
            table.insert(parts, want and string.format("%s=%q", want.name, s)
                or string.format("%q", s))
        elseif itype == 3 or itype == 4 then
            local item = subtree:add(f.dtp_bool, body(start, 1), itype == 4)
            if want then
                item:set_text(string.format("%s: %s", want.name, itype == 4 and "true" or "false"))
            end
            table.insert(parts, want
                and string.format("%s=%s", want.name, itype == 4 and "true" or "false")
                or (itype == 4 and "true" or "false"))
        elseif itype == 2 then
            -- bitmap: a length in bits, then the bytes holding them. On a route
            -- it is which of the source's levels to carry across, so the set
            -- bits are worth naming rather than counting.
            local n, used = dtp_varint(body, off)
            if n == nil then
                subtree:add_proto_expert_info(ef_bad_dtp)
                break
            end
            off = off + used
            local bytes = math.floor((n + 7) / 8)
            if off + bytes > body:len() then
                subtree:add_proto_expert_info(ef_bad_dtp)
                break
            end
            local set = {}
            for i = 0, n - 1 do
                local byte_at = body(off + math.floor(i / 8), 1):uint()
                if bit.band(byte_at, bit.lshift(1, i % 8)) ~= 0 then
                    table.insert(set, tostring(i + 1))
                end
            end
            local text = #set == 0 and "none" or table.concat(set, ",")
            subtree:add(p_rc, body(start, off + bytes - start),
                string.format("%s: %d bits, set: %s", want and want.name or "bitmap", n, text))
            off = off + bytes
            table.insert(parts, string.format("%s=%s", want and want.name or "bitmap", text))
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
--
-- This is where a router node becomes readable. Everything the command space
-- means downstream — whether 100 is an interface version or a selected
-- destination — follows from the type a unit declares here, so it is recorded
-- against the address it came from and used for every command that node
-- serves afterwards.
local function dissect_id(tvb, tree, off, ctx)
    if tvb:len() < off + 28 then
        return nil
    end
    local services = tvb(off, 2):uint()
    tree:add(f.services, tvb(off, 2)):append_text(" (" .. bits_string(services, service_bits) .. ")")

    local type_id = tvb(off + 2, 2):uint()
    local tid = tree:add(f.type_id, tvb(off + 2, 2))
    if router_node_type[type_id] then
        tid:append_text(" (" .. router_node_type[type_id] .. ")")
        tree:add(f.router_node, tvb(off + 2, 2), router_node_type[type_id])
    end
    if ctx and ctx.learning and ctx.node then
        ctx.node.type_id = type_id
    end

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

-- DEVICEINFO_STR: one entry of a device list, which describes a node other
-- than the one that sent it. The address is in the payload rather than in the
-- header, so what this teaches is filed against that address — this is how a
-- client learns a plant's shape before it has spoken to any of it.
local function dissect_devinfo(tvb, tree, ctx)
    if not need(tvb, 10, tree) then return nil end
    tree:add(f.protocol, tvb(0, 2))
    local addr = address_string(tvb, 2)
    tree:add(f.dst, tvb(2, 6), addr):set_text("Address: " .. addr)

    local named = nil
    if ctx and ctx.learning then
        named = { conv = ctx.conv, learning = true,
            node = node_state(ctx, address_fields(tvb, 2)) }
    end
    local name = dissect_id(tvb, tree, 8, named)
    local status = nil
    if tvb:len() >= 40 then
        status = dissect_status(tvb, tree, 36)
    end
    return string.format("%s %q%s", addr, name or "",
        status and (" [" .. status .. "]") or "")
end

-- FUNC_STR (spec 11.4.1): one menu line in the 16-bit generation.
local function dissect_func(tvb, tree, ctx)
    if not need(tvb, 18, tree) then return nil end
    local index = tvb(0, 2):uint()
    local style = tvb(2, 2):uint()
    local command = tvb(4, 2):uint()
    tree:add(f.menu_index, tvb(0, 2), index)
    tree:add(f.style, tvb(2, 2)):append_text(" (" .. style_string(style) .. ")")
    tree:add(f.command, tvb(4, 2), command)
    annotate_command(tree, ctx, tvb(4, 2), command)
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
local function dissect_menu_item(tvb, tree, ctx)
    if not need(tvb, 22, tree) then return nil end
    local index = tvb(0, 4):uint()
    local style = tvb(4, 2):uint()
    local command = tvb(6, 4):uint()
    tree:add(f.menu_index, tvb(0, 4), index)
    tree:add(f.style, tvb(4, 2)):append_text(" (" .. style_string(style) .. ")")
    tree:add(f.command, tvb(6, 4), command)
    -- A menu line names the command behind it, and on a router that command
    -- is a crosspoint. Naming it here is what makes a level's menu readable as
    -- a routing interface rather than as a list of numbers.
    annotate_command(tree, ctx, tvb(6, 4), command)
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

-- value_meaning renders the numeric half of a value when the command says what
-- the number is. A protect word and a crosspoint are both int32 on the wire
-- and neither reads as anything until the command is known.
local function value_meaning(res, value)
    if res == nil or res.role == nil then
        return nil
    end
    if res.role == "protect" then
        return protect_string(value)
    end
    if res.role == "checkbox" then
        -- A checkbox on this interface counts from one: off is 1, on is 2.
        if value == 1 then return "off" end
        if value == 2 then return "on" end
    end
    if res.role == "crosspoint" or res.role == "levelroute" then
        return (pin_string(value))
    end
    return nil
end

local function dissect_value_tail(tvb, tree, off, mode, value, res, reply, info)
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
            local summary = dtp_dissect(data:tvb(), tree, dtp_meaning(res, reply))
            if summary then
                table.insert(info, summary)
            end
        end
    end
    return off
end

-- FUNCSTATUS_STR (spec 11.5.2): a value in the 16-bit generation.
local function dissect_func_status(tvb, tree, ctx, reply)
    if not need(tvb, 8, tree) then return nil end
    local command = tvb(0, 2):uint()
    local mode = tvb(2, 2):uint()
    local value = tvb(4, 4):int()
    tree:add(f.command, tvb(0, 2), command)
    local res = annotate_command(tree, ctx, tvb(0, 2), command)
    tree:add(f.mode, tvb(2, 2)):append_text(" (" .. bits_string(mode, mode_bits) .. ")")
    local vitem = tree:add(f.value, tvb(4, 4), value)

    local info = { string.format("cmd=%d", command) }
    if res then table.insert(info, res.what) end
    if bit.band(mode, 0x0001) ~= 0 then
        local meant = value_meaning(res, value)
        if meant then
            vitem:append_text(" (" .. meant .. ")")
            table.insert(info, meant)
        else
            table.insert(info, tostring(value))
        end
        if ctx and ctx.learning then learn(ctx, res, value) end
    end
    dissect_value_tail(tvb, tree, 8, mode, value, res, reply, info)
    return table.concat(info, " ")
end

-- VALUE_STR: a value in the 32-bit generation.
local function dissect_value(tvb, tree, ctx, reply)
    if not need(tvb, 12, tree) then return nil end
    local command = tvb(0, 4):uint()
    local match = tvb(4, 2):uint()
    local mode = tvb(6, 2):uint()
    local value = tvb(8, 4):int()
    tree:add(f.command, tvb(0, 4), command)
    local res = annotate_command(tree, ctx, tvb(0, 4), command)
    tree:add(f.match_id, tvb(4, 2), match)
    tree:add(f.mode, tvb(6, 2)):append_text(" (" .. bits_string(mode, mode_bits) .. ")")
    local vitem = tree:add(f.value, tvb(8, 4), value)

    local info = { string.format("cmd=%d", command) }
    if res then table.insert(info, res.what) end
    if bit.band(mode, 0x0001) ~= 0 then
        local meant = value_meaning(res, value)
        if meant then
            vitem:append_text(" (" .. meant .. ")")
            table.insert(info, meant)
        else
            table.insert(info, tostring(value))
        end
        -- Only a reply teaches anything: what a client asks for is a request,
        -- and a request for CMD_MATRIX_BASE carries no base.
        if ctx and ctx.learning and reply then learn(ctx, res, value) end
    end
    dissect_value_tail(tvb, tree, 12, mode, value, res, reply, info)
    return table.concat(info, " ")
end

local function dissect_get_fstat(tvb, tree, ctx)
    if not need(tvb, 2, tree) then return nil end
    local command = tvb(0, 2):uint()
    tree:add(f.command, tvb(0, 2), command)
    local res = annotate_command(tree, ctx, tvb(0, 2), command)
    if res then
        return string.format("cmd=%d %s", command, res.what)
    end
    return string.format("cmd=%d", command)
end

local function dissect_get_value(tvb, tree, ctx)
    if not need(tvb, 4, tree) then return nil end
    local command = tvb(0, 4):uint()
    tree:add(f.command, tvb(0, 4), command)
    local res = annotate_command(tree, ctx, tvb(0, 4), command)
    if res then
        return string.format("cmd=%d %s", command, res.what)
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
local function dissect_payload(ptype, tvb, tree, ctx)
    if ptype == 2 then return dissect_call(tvb, tree) end
    if ptype == 3 then return dissect_term(tvb, tree) end
    if ptype == 0 or ptype == 15 then return dissect_nack(tvb, tree) end
    if ptype == 25 then return dissect_wait(tvb, tree) end
    if ptype == 5 then return dissect_status(tvb, tree, 0) end
    if ptype == 7 then local n = dissect_id(tvb, tree, 0, ctx); return n end
    if ptype == 20 or ptype == 33 then return dissect_devinfo(tvb, tree, ctx) end
    if ptype == 9 then return dissect_func(tvb, tree, ctx) end
    if ptype == 68 then return dissect_menu_item(tvb, tree, ctx) end
    if ptype == 65 or ptype == 67 then return dissect_menu_req(tvb, tree) end
    if ptype == 66 then return dissect_menu_size(tvb, tree) end
    if ptype == 11 then return dissect_get_fstat(tvb, tree, ctx) end
    if ptype == 12 then return dissect_func_status(tvb, tree, ctx, true) end
    if ptype == 16 then return dissect_func_status(tvb, tree, ctx, false) end
    if ptype == 69 then return dissect_get_value(tvb, tree, ctx) end
    if ptype == 70 then return dissect_value(tvb, tree, ctx, false) end
    if ptype == 71 then return dissect_value(tvb, tree, ctx, true) end
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

    -- Which end of this message is the node whose command space is in play.
    -- A device answers from its own address and is asked at it, so a reply
    -- names the node in Src and a request names it in Dst.
    local node_addr = node_is_src[ptype] and address_fields(tvb, 10) or address_fields(tvb, 4)
    local ctx = {
        conv = conv_key(pinfo),
        frame = pinfo.number,
        learning = not pinfo.visited,
    }
    ctx.node = node_state(ctx, node_addr)
    ctx.node_key = addr_key(node_addr)
    if ctx.node.type_id and router_node_type[ctx.node.type_id] then
        root:add(f.router_node, tvb(node_is_src[ptype] and 10 or 4, 4),
            router_node_type[ctx.node.type_id]):set_generated(true)
    end

    local payload_len = total - HEADER_SIZE
    local detail = nil
    if payload_len > 0 then
        local payload = tvb(HEADER_SIZE, payload_len)
        local ptree = root:add(f.payload, payload)
        detail = dissect_payload(ptype, payload:tvb(), ptree, ctx)
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
    -- Everything learned belongs to the capture it was learned from. A reload
    -- or a new capture starts over, so a file opened second is never named
    -- with the tables the file before it published.
    nodes = {}
    resolved_cache = {}
end

function p_rc.prefs_changed()
    register_ports()
end
