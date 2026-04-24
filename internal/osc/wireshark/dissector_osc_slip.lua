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
f.start_end    = ProtoField.uint8("osc_slip.start", "SLIP END (start)", base.HEX)
f.body_len     = ProtoField.uint32("osc_slip.body_len", "Unstuffed body size", base.DEC)
f.end_end      = ProtoField.uint8("osc_slip.end", "SLIP END (tail)", base.HEX)
f.stuffed_bytes= ProtoField.uint32("osc_slip.stuffed_bytes", "Stuffed bytes on wire (DLE escaping)", base.DEC)
f.payload_kind = ProtoField.string("osc_slip.payload_kind", "Payload kind")
f.addr_preview = ProtoField.string("osc_slip.address", "OSC Address")
f.tag_preview  = ProtoField.string("osc_slip.type_tag", "Type-tag string")
f.arg_count    = ProtoField.uint32("osc_slip.arg_count", "Arg count", base.DEC)

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

-- peek_osc_header extracts (kind, address, tag_string) from an unstuffed
-- OSC packet body without doing full arg parsing. kind is "bundle",
-- "message", or "unknown". address + tag_string are nil for bundles.
-- OSC strings are NUL-terminated + padded to 4 bytes.
local function peek_osc_header(body)
    if #body == 0 then return "unknown", nil, nil end
    if body[1] == 0x23 then -- '#' → #bundle
        return "bundle", nil, nil
    end
    if body[1] ~= 0x2F then -- '/' start of address
        return "unknown", nil, nil
    end
    -- Scan first NUL to find the end of address.
    local addr_end = nil
    for i = 1, #body do
        if body[i] == 0 then
            addr_end = i
            break
        end
    end
    if addr_end == nil then return "message", nil, nil end
    local addr = {}
    for i = 1, addr_end - 1 do addr[i] = string.char(body[i]) end
    local addr_str = table.concat(addr)
    -- Advance past NUL-pad to 4-byte boundary, then read type-tag string.
    local pad = (4 - (addr_end % 4)) % 4
    local tag_start = addr_end + pad + 1
    if tag_start > #body then
        return "message", addr_str, nil
    end
    local tag_end = nil
    for i = tag_start, #body do
        if body[i] == 0 then
            tag_end = i
            break
        end
    end
    if tag_end == nil then return "message", addr_str, nil end
    local tag = {}
    for i = tag_start, tag_end - 1 do tag[#tag + 1] = string.char(body[i]) end
    return "message", addr_str, table.concat(tag)
end

-- count_stuffed scans the raw (still-stuffed) segment between start and
-- end (exclusive) and returns how many bytes were byte-stuffed (i.e.
-- ESC-pairs), which lets us show the overhead in the subtree.
local function count_stuffed(tvb, start_off, stop_off)
    local n = 0
    local i = start_off
    while i < stop_off do
        if tvb(i, 1):uint() == ESC then
            n = n + 1
            i = i + 2
        else
            i = i + 1
        end
    end
    return n
end

-- nested_message_count walks a bundle body shallowly to report how many
-- nested Messages + Bundles it contains (for the Info column). Returns
-- (messages, sub_bundles, total_elements).
local function nested_message_count(body)
    -- body starts with "#bundle\0" (8 bytes) + timetag (8 bytes) = 16 skip.
    if #body < 16 then return 0, 0, 0 end
    local msgs, subs, total = 0, 0, 0
    local i = 17
    while i <= #body do
        if i + 3 > #body then break end
        -- 4-byte BE element size
        local sz = body[i] * 0x1000000 + body[i + 1] * 0x10000 + body[i + 2] * 0x100 + body[i + 3]
        i = i + 4
        if sz == 0 or i + sz - 1 > #body then break end
        total = total + 1
        if body[i] == 0x2F then
            msgs = msgs + 1
        elseif body[i] == 0x23 then
            subs = subs + 1
        end
        i = i + sz
    end
    return msgs, subs, total
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

    -- Per-packet Info column accumulator. Wireshark overwrites the Info
    -- column on each dissector call; we build one line summarising all
    -- SLIP frames carried in this TCP segment.
    local info_parts = {}

    while offset < total do
        local body_table, consumed = unstuff_slip(tvb, offset)
        if body_table == nil then
            local reason = consumed
            if reason == "truncated" then
                pinfo.desegment_offset = offset
                pinfo.desegment_len = DESEGMENT_ONE_MORE_SEGMENT
                if not any_decoded then
                    tree:add(osc_slip, tvb(offset), "OSC 1.1 SLIP (incomplete — awaiting more bytes)")
                        :add_expert_info(ef_truncated)
                    pinfo.cols.info:set("OSC-SLIP incomplete (desegmenting)")
                end
                return total
            end
            tree:add(osc_slip, tvb(offset), "OSC 1.1 SLIP (malformed escape)")
                :add_expert_info(ef_bad_escape)
            pinfo.cols.info:set("OSC-SLIP malformed")
            return total
        end

        any_decoded = true
        local frame_label
        local kind, addr_str, tag_str = peek_osc_header(body_table)
        if kind == "bundle" then
            local msgs, subs, totalEls = nested_message_count(body_table)
            frame_label = string.format("OSC 1.1 SLIP #bundle (%d msgs%s)",
                msgs, subs > 0 and string.format(", %d sub-bundles", subs) or "")
            info_parts[#info_parts + 1] = string.format("#bundle[%d]", totalEls)
        elseif kind == "message" then
            local args = 0
            if tag_str ~= nil and #tag_str >= 1 and tag_str:sub(1, 1) == "," then
                args = #tag_str - 1
            end
            frame_label = string.format("OSC 1.1 SLIP msg %s (%d arg%s)",
                addr_str or "(?)",
                args,
                args == 1 and "" or "s")
            local tag_suffix = "[?]"
            if tag_str ~= nil then
                tag_suffix = "[" .. tag_str:sub(2) .. "]"
            end
            info_parts[#info_parts + 1] = string.format("%s %s",
                addr_str or "(?)", tag_suffix)
        else
            frame_label = "OSC 1.1 SLIP (unknown payload)"
            info_parts[#info_parts + 1] = "?"
        end

        local subtree = tree:add(osc_slip, tvb(offset, consumed), frame_label)
        subtree:add(f.start_end, tvb(offset, 1))
        subtree:add(f.body_len, #body_table):set_generated()
        local stuffed = count_stuffed(tvb, offset + 1, offset + consumed - 1)
        if stuffed > 0 then
            subtree:add(f.stuffed_bytes, stuffed):set_generated()
        end

        if kind == "message" then
            subtree:add(f.payload_kind, "message"):set_generated()
            if addr_str ~= nil then
                subtree:add(f.addr_preview, addr_str):set_generated()
            end
            if tag_str ~= nil then
                subtree:add(f.tag_preview, tag_str):set_generated()
                if #tag_str >= 1 and tag_str:sub(1, 1) == "," then
                    subtree:add(f.arg_count, #tag_str - 1):set_generated()
                end
            end
        elseif kind == "bundle" then
            subtree:add(f.payload_kind, "bundle"):set_generated()
            local msgs, subs, totalEls = nested_message_count(body_table)
            subtree:add(f.arg_count, totalEls):set_generated()
            subtree:append_text(string.format("  (%d messages, %d sub-bundles)", msgs, subs))
        else
            subtree:add(f.payload_kind, "unknown"):set_generated()
        end

        -- Build a synthetic Tvb from the unstuffed bytes and dispatch
        -- to Wireshark's built-in OSC dissector so the standard Glow-like
        -- tree view still appears under our frame.
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

    if #info_parts > 0 then
        pinfo.cols.info:set(string.format("SLIP×%d: %s",
            #info_parts, table.concat(info_parts, " | ")))
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
