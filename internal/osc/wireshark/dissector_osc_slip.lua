-------------------------------------------------------------------------------
--
-- Wireshark Lua Dissector: OSC 1.1 SLIP framing over TCP
--
-- Wireshark ships a built-in OSC dissector that handles UDP and the
-- OSC 1.0 length-prefix TCP framing. It does NOT handle OSC 1.1 SLIP
-- framing (RFC 1055 with double-END per spec §1.1), which is what the
-- dhs OSC plugin uses for `osc-v11` TCP.
--
-- This supplementary dissector:
--   1. Registers for a configurable TCP port (default 8000 — same as
--      the UDP default; adjust per deployment).
--   2. Detects SLIP by looking for the END byte (0xC0) at the start of
--      the TCP segment.
--   3. Walks the SLIP stream, un-stuffs the body per RFC 1055 / OSC 1.1,
--      and hands the unframed OSC packet to Wireshark's built-in OSC
--      dissector via Dissector.get("osc").
--   4. Desegments across TCP segments when a frame is incomplete.
--
-- Compatible with Wireshark 4.x (Lua 5.2 / 5.3 / 5.4).
-- Spec: https://opensoundcontrol.stanford.edu/spec-1_1.html
--
-------------------------------------------------------------------------------

local osc_slip = Proto("osc_slip", "OSC 1.1 SLIP Framing (RFC 1055 double-END)")

osc_slip.prefs.tcp_port = Pref.uint("TCP port", 8000, "TCP port carrying SLIP-framed OSC 1.1")

-- Display fields
local f = osc_slip.fields
f.start_end  = ProtoField.uint8("osc_slip.start", "SLIP END (start)", base.HEX)
f.body_len   = ProtoField.uint32("osc_slip.body_len", "Unstuffed body size", base.DEC)
f.end_end    = ProtoField.uint8("osc_slip.end", "SLIP END (tail)", base.HEX)

local ef_truncated = ProtoExpert.new("osc_slip.truncated", "SLIP frame incomplete — desegmenting",
    expert.group.MALFORMED, expert.severity.NOTE)
local ef_bad_escape = ProtoExpert.new("osc_slip.bad_escape", "SLIP ESC not followed by ESC_END or ESC_ESC",
    expert.group.MALFORMED, expert.severity.WARN)

osc_slip.experts = { ef_truncated, ef_bad_escape }

-- SLIP constants per RFC 1055
local END     = 0xC0
local ESC     = 0xDB
local ESC_END = 0xDC
local ESC_ESC = 0xDD

-- unstuff_slip walks tvb from offset, decoding up to a trailing END.
-- Returns (unstuffed_bytes_table, bytes_consumed) on success, or
-- (nil, reason) where reason is one of "truncated" / "bad_escape".
local function unstuff_slip(tvb, offset)
    local len = tvb:len()
    if offset >= len then return nil, "truncated" end

    -- Require leading END.
    if tvb(offset, 1):uint() ~= END then return nil, "bad_escape" end
    local i = offset + 1
    -- Tolerate multiple leading END bytes (1.1 double-END between frames).
    while i < len and tvb(i, 1):uint() == END do
        i = i + 1
    end
    if i >= len then return nil, "truncated" end

    local out = {}
    while i < len do
        local b = tvb(i, 1):uint()
        i = i + 1
        if b == END then
            return out, (i - offset)
        elseif b == ESC then
            if i >= len then return nil, "truncated" end
            local nxt = tvb(i, 1):uint()
            i = i + 1
            if nxt == ESC_END then
                out[#out + 1] = END
            elseif nxt == ESC_ESC then
                out[#out + 1] = ESC
            else
                return nil, "bad_escape"
            end
        else
            out[#out + 1] = b
        end
    end
    return nil, "truncated"
end

-- table_to_bytestring turns a byte table into a lua string suitable for
-- ByteArray.new.
local function table_to_bytestring(t)
    local chars = {}
    for i, b in ipairs(t) do
        chars[i] = string.char(b)
    end
    return table.concat(chars)
end

function osc_slip.dissector(tvb, pinfo, tree)
    local total = tvb:len()
    if total < 2 then
        -- Need at least END + one body byte.
        pinfo.desegment_offset = 0
        pinfo.desegment_len = DESEGMENT_ONE_MORE_SEGMENT
        return 0
    end

    pinfo.cols.protocol = "OSC-SLIP"
    local offset = 0
    local any_decoded = false

    while offset < total do
        local body_table, consumed = unstuff_slip(tvb, offset)
        if body_table == nil then
            -- consumed carries the reason when body_table is nil.
            local reason = consumed
            if reason == "truncated" then
                -- Ask Wireshark to reassemble more bytes and retry.
                pinfo.desegment_offset = offset
                pinfo.desegment_len = DESEGMENT_ONE_MORE_SEGMENT
                if not any_decoded then
                    tree:add(osc_slip, tvb(offset), "OSC 1.1 SLIP (incomplete)")
                        :add_expert_info(ef_truncated)
                end
                return total
            end
            -- Framing error — report + abort this segment.
            tree:add(osc_slip, tvb(offset), "OSC 1.1 SLIP (malformed)")
                :add_expert_info(ef_bad_escape)
            return total
        end

        any_decoded = true
        local subtree = tree:add(osc_slip, tvb(offset, consumed), "OSC 1.1 SLIP frame")
        subtree:add(f.start_end, tvb(offset, 1))
        subtree:add(f.body_len, #body_table):set_generated()

        -- Build a synthetic Tvb from the unstuffed bytes and dispatch
        -- to Wireshark's built-in OSC dissector.
        local raw = table_to_bytestring(body_table)
        local inner = ByteArray.new(raw, true):tvb("unstuffed OSC")
        local osc = Dissector.get("osc")
        if osc ~= nil then
            osc:call(inner, pinfo, subtree)
        else
            subtree:add("(Wireshark built-in 'osc' dissector not available — load or upgrade Wireshark)")
        end
        offset = offset + consumed
    end
    return offset
end

-- Register on the configured TCP port. Wireshark's built-in OSC
-- dissector stays on UDP + length-prefix-TCP via its own prefs.
local function register_ports()
    local tcp = DissectorTable.get("tcp.port")
    tcp:add(osc_slip.prefs.tcp_port, osc_slip)
end

register_ports()

function osc_slip.prefs_changed()
    register_ports()
end
