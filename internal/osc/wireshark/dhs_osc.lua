-------------------------------------------------------------------------------
--
-- Wireshark Lua Dissector: OSC (Open Sound Control) 1.0 + 1.1
--
-- Covers every transport and wire version the dhs OSC plugin implements:
--
--   * UDP                 — OSC 1.0 + 1.1 packets framed by UDP datagrams
--   * TCP length-prefix   — OSC 1.0: int32-BE size + packet
--   * TCP SLIP            — OSC 1.1: RFC 1055 double-END framing
--
-- Decodes every type tag — core (i f s b), extended (h d t S c r m), and
-- 1.1 payload-less tags (T F N I) + array markers ([ ]). Bundles and
-- nested bundles are decoded recursively.
--
-- Info column surfaces the payload in a form that uniquely identifies a
-- frame — address + type-tag-string + arg count for messages, or a
-- bundle summary (timetag + nested-message/bundle counts) for bundles.
--
-- Wireshark 4.x (Lua 5.2/5.3/5.4). Pure-arithmetic bit ops only (no
-- bit32/bit) so it works on stock Wireshark builds.
--
-- Refs:
--   OSC 1.0: https://opensoundcontrol.stanford.edu/spec-1_0.html
--   OSC 1.1: https://opensoundcontrol.stanford.edu/spec-1_1.html
--   RFC 1055 SLIP: https://tools.ietf.org/html/rfc1055
--
-------------------------------------------------------------------------------

local osc = Proto("dhs_osc", "OSC (dhs — Open Sound Control 1.0 + 1.1)")

osc.prefs.udp_port      = Pref.uint("UDP port", 8000, "UDP port carrying OSC packets")
osc.prefs.tcp_len_port  = Pref.uint("TCP port (length-prefix, 1.0)", 8000, "TCP port carrying OSC 1.0 length-prefix frames")
osc.prefs.tcp_slip_port = Pref.uint("TCP port (SLIP, 1.1)", 8001, "TCP port carrying OSC 1.1 SLIP-framed packets")
osc.prefs.heuristic     = Pref.bool("Enable heuristic TCP dispatch", true, "Also try OSC on any TCP stream whose first bytes look like a length-prefix or SLIP frame")

-------------------------------------------------------------------------------
-- Fields
-------------------------------------------------------------------------------

local f = osc.fields

-- Envelope
f.version       = ProtoField.string("dhs_osc.version", "Wire version")
f.transport     = ProtoField.string("dhs_osc.transport", "Transport framing")
f.kind          = ProtoField.string("dhs_osc.kind", "Packet kind")

-- Bundle
f.bundle_head   = ProtoField.string("dhs_osc.bundle.head", "Bundle header")
f.bundle_tt     = ProtoField.uint64("dhs_osc.bundle.timetag", "Timetag (NTP u64)", base.HEX)
f.bundle_tt_sec = ProtoField.uint32("dhs_osc.bundle.timetag.secs", "Timetag seconds since 1900", base.DEC)
f.bundle_tt_fr  = ProtoField.uint32("dhs_osc.bundle.timetag.frac", "Timetag fraction", base.DEC)
f.bundle_tt_imm = ProtoField.bool("dhs_osc.bundle.timetag.immediate", "Immediate (raw 0x0000000000000001)")
f.bundle_count  = ProtoField.uint32("dhs_osc.bundle.element_count", "Element count (direct children)", base.DEC)

-- Element (nested inside bundle)
f.element_size  = ProtoField.uint32("dhs_osc.element.size", "Element size (bytes)", base.DEC)
f.element_kind  = ProtoField.string("dhs_osc.element.kind", "Element kind")

-- Message
f.msg_address   = ProtoField.string("dhs_osc.address", "Address")
f.msg_tagstr    = ProtoField.string("dhs_osc.type_tag", "Type-tag string")
f.msg_argcount  = ProtoField.uint32("dhs_osc.arg_count", "Arg count", base.DEC)

-- Args — per-type
f.arg_i = ProtoField.int32 ("dhs_osc.arg.i", "i  int32",    base.DEC)
f.arg_f = ProtoField.float ("dhs_osc.arg.f", "f  float32")
f.arg_s = ProtoField.string("dhs_osc.arg.s", "s  OSC-string")
f.arg_b = ProtoField.bytes ("dhs_osc.arg.b", "b  OSC-blob", base.NONE)
f.arg_h = ProtoField.int64 ("dhs_osc.arg.h", "h  int64",    base.DEC)
f.arg_d = ProtoField.double("dhs_osc.arg.d", "d  float64")
f.arg_t = ProtoField.uint64("dhs_osc.arg.t", "t  timetag",  base.HEX)
f.arg_S = ProtoField.string("dhs_osc.arg.S", "S  symbol")
f.arg_c = ProtoField.string("dhs_osc.arg.c", "c  char")
f.arg_r = ProtoField.uint32("dhs_osc.arg.r", "r  RGBA",     base.HEX)
f.arg_m = ProtoField.bytes ("dhs_osc.arg.m", "m  MIDI 4 bytes", base.NONE)
f.arg_T = ProtoField.string("dhs_osc.arg.T", "T  true (no payload)")
f.arg_F = ProtoField.string("dhs_osc.arg.F", "F  false (no payload)")
f.arg_N = ProtoField.string("dhs_osc.arg.N", "N  nil (no payload)")
f.arg_I = ProtoField.string("dhs_osc.arg.I", "I  infinitum (no payload)")
f.arg_lb= ProtoField.string("dhs_osc.arg.array_begin", "[  array begin (no payload)")
f.arg_rb= ProtoField.string("dhs_osc.arg.array_end",   "]  array end (no payload)")

-- SLIP framing
f.slip_end_start = ProtoField.uint8 ("dhs_osc.slip.start", "SLIP END (start)", base.HEX)
f.slip_end_tail  = ProtoField.uint8 ("dhs_osc.slip.end",   "SLIP END (tail)",  base.HEX)
f.slip_stuffed   = ProtoField.uint32("dhs_osc.slip.stuffed_bytes", "ESC-stuffed bytes", base.DEC)
f.slip_body_len  = ProtoField.uint32("dhs_osc.slip.body_len", "Unstuffed body size", base.DEC)

-- TCP length-prefix
f.lp_size        = ProtoField.uint32("dhs_osc.len_prefix.size", "Frame size (int32 BE)", base.DEC)

-------------------------------------------------------------------------------
-- Expert info
-------------------------------------------------------------------------------

local ef_alignment   = ProtoExpert.new("dhs_osc.alignment",   "OSC-string / OSC-blob not padded to 4-byte multiple",
                                        expert.group.MALFORMED, expert.severity.WARN)
local ef_comma       = ProtoExpert.new("dhs_osc.comma_missing", "Type-tag string does not begin with ',' — ill-formed",
                                        expert.group.MALFORMED, expert.severity.ERROR)
local ef_truncated   = ProtoExpert.new("dhs_osc.truncated",    "Packet ends before expected argument boundary",
                                        expert.group.MALFORMED, expert.severity.WARN)
local ef_unknown_tag = ProtoExpert.new("dhs_osc.tag_unknown",  "Unknown type tag — decoder does not know how to skip this arg",
                                        expert.group.MALFORMED, expert.severity.ERROR)
local ef_arr_unbal   = ProtoExpert.new("dhs_osc.array_unbalanced", "'[' without matching ']' — unbalanced array markers",
                                        expert.group.PROTOCOL, expert.severity.WARN)
local ef_slip_trunc  = ProtoExpert.new("dhs_osc.slip_truncated", "SLIP frame incomplete — desegmenting",
                                        expert.group.MALFORMED, expert.severity.NOTE)
local ef_slip_bad    = ProtoExpert.new("dhs_osc.slip_bad_escape", "SLIP ESC not followed by ESC_END or ESC_ESC",
                                        expert.group.MALFORMED, expert.severity.ERROR)
local ef_lp_size     = ProtoExpert.new("dhs_osc.lp_size_unreasonable", "TCP length-prefix size looks unreasonable (> 1 MiB)",
                                        expert.group.PROTOCOL, expert.severity.WARN)

osc.experts = {
    ef_alignment, ef_comma, ef_truncated, ef_unknown_tag, ef_arr_unbal,
    ef_slip_trunc, ef_slip_bad, ef_lp_size,
}

-------------------------------------------------------------------------------
-- SLIP constants (RFC 1055)
-------------------------------------------------------------------------------

local SLIP_END     = 0xC0
local SLIP_ESC     = 0xDB
local SLIP_ESC_END = 0xDC
local SLIP_ESC_ESC = 0xDD

local MAX_FRAME = 1024 * 1024  -- 1 MiB — anything larger is almost certainly a decode error

-------------------------------------------------------------------------------
-- Primitive helpers
-------------------------------------------------------------------------------

-- pad4 returns number of pad bytes needed after `n` data bytes of an
-- OSC-string / OSC-blob to reach the next 4-byte boundary.
local function pad4(n)
    local r = n % 4
    if r == 0 then return 4 end
    return 4 - r
end

-- read_osc_string reads a NUL-terminated ASCII string starting at `off`
-- inside tvb, padded to 4 bytes. Returns (value, consumed_bytes) or
-- (nil, reason).
local function read_osc_string(tvb, off, limit)
    if off >= limit then return nil, "truncated" end
    local nul = nil
    for i = off, limit - 1 do
        if tvb(i, 1):uint() == 0 then
            nul = i
            break
        end
    end
    if nul == nil then return nil, "truncated" end
    local data_len = nul - off               -- bytes before NUL
    local pad      = pad4(data_len + 1)      -- pad AFTER the NUL to reach 4-byte boundary
    -- Per OSC spec, the NUL counts toward the unit we align; the pad makes the total length a
    -- multiple of 4. data_len+1 includes the NUL, pad4() computes extra NULs needed.
    local consumed = data_len + 1 + pad - 1  -- -1 because pad4() returns 4 if n%4==0
    if off + consumed > limit then return nil, "truncated" end
    -- Actually compute consumed cleanly: the full field ends at the next 4-byte boundary after nul+1.
    -- Recompute:
    local end_after_nul = nul + 1
    local rem = end_after_nul % 4
    local true_end
    if rem == 0 then
        true_end = end_after_nul          -- no pad needed
    else
        true_end = end_after_nul + (4 - rem)
    end
    if true_end > limit then return nil, "truncated" end
    local value = tvb(off, data_len):string()
    -- alignment sanity check (should already be 4-byte aligned by construction above)
    local aligned_ok = ((true_end - off) % 4 == 0)
    return value, (true_end - off), aligned_ok
end

-- read_osc_blob: int32 BE size + data + pad.
local function read_osc_blob(tvb, off, limit)
    if off + 4 > limit then return nil, "truncated" end
    local sz = tvb(off, 4):uint()
    local data_start = off + 4
    if data_start + sz > limit then return nil, "truncated" end
    local pad_after = (4 - (sz % 4)) % 4
    local total = 4 + sz + pad_after
    if off + total > limit then return nil, "truncated" end
    return tvb(data_start, sz), total, (sz % 4 == 0)
end

-------------------------------------------------------------------------------
-- Arg decoders — each returns (consumed_bytes, uses_1_1_feature) or (nil, reason)
-------------------------------------------------------------------------------

-- Forward-declared because read_args recurses through bundle decoder below.
local dissect_packet

local function add_arg(tree, tvb, off, limit, tag, pinfo)
    if tag == "i" then
        if off + 4 > limit then return nil, "truncated" end
        tree:add(f.arg_i, tvb(off, 4))
        return 4, false
    elseif tag == "f" then
        if off + 4 > limit then return nil, "truncated" end
        tree:add(f.arg_f, tvb(off, 4))
        return 4, false
    elseif tag == "s" or tag == "S" then
        local v, n, aligned = read_osc_string(tvb, off, limit)
        if v == nil then return nil, n end
        local ti = tree:add(tag == "s" and f.arg_s or f.arg_S, tvb(off, n), v)
        if not aligned then ti:add_proto_expert_info(ef_alignment) end
        return n, false
    elseif tag == "b" then
        local bytes_tvb, n, aligned = read_osc_blob(tvb, off, limit)
        if bytes_tvb == nil then return nil, n end
        local ti = tree:add(f.arg_b, tvb(off, n))
        ti:append_text(string.format("  (%d bytes)", bytes_tvb:len()))
        if not aligned then ti:add_proto_expert_info(ef_alignment) end
        return n, false
    elseif tag == "h" then
        if off + 8 > limit then return nil, "truncated" end
        tree:add(f.arg_h, tvb(off, 8))
        return 8, false
    elseif tag == "d" then
        if off + 8 > limit then return nil, "truncated" end
        tree:add(f.arg_d, tvb(off, 8))
        return 8, false
    elseif tag == "t" then
        if off + 8 > limit then return nil, "truncated" end
        tree:add(f.arg_t, tvb(off, 8))
        return 8, false
    elseif tag == "c" then
        if off + 4 > limit then return nil, "truncated" end
        local lo = tvb(off + 3, 1):uint()
        local ch = (lo >= 32 and lo < 127) and string.format("'%s' (0x%02x)", string.char(lo), lo)
                                             or string.format("0x%02x", lo)
        tree:add(f.arg_c, tvb(off, 4), ch)
        return 4, false
    elseif tag == "r" then
        if off + 4 > limit then return nil, "truncated" end
        tree:add(f.arg_r, tvb(off, 4))
        return 4, false
    elseif tag == "m" then
        if off + 4 > limit then return nil, "truncated" end
        tree:add(f.arg_m, tvb(off, 4))
        return 4, false
    elseif tag == "T" then
        tree:add(f.arg_T, tvb(off, 0), "true"):set_generated()
        return 0, true
    elseif tag == "F" then
        tree:add(f.arg_F, tvb(off, 0), "false"):set_generated()
        return 0, true
    elseif tag == "N" then
        tree:add(f.arg_N, tvb(off, 0), "nil"):set_generated()
        return 0, true
    elseif tag == "I" then
        tree:add(f.arg_I, tvb(off, 0), "infinitum"):set_generated()
        return 0, true
    elseif tag == "[" then
        tree:add(f.arg_lb, tvb(off, 0), "array begin"):set_generated()
        return 0, true
    elseif tag == "]" then
        tree:add(f.arg_rb, tvb(off, 0), "array end"):set_generated()
        return 0, true
    else
        return nil, "unknown"
    end
end

-------------------------------------------------------------------------------
-- Message + bundle decoders
-------------------------------------------------------------------------------

local function dissect_message(tvb, off, limit, tree, pinfo)
    local start = off
    local addr, addr_n, addr_aligned = read_osc_string(tvb, off, limit)
    if addr == nil then return nil, addr_n end
    local sub = tree:add(f.kind, tvb(off, 0), "message"):set_generated()
    local ti_addr = tree:add(f.msg_address, tvb(off, addr_n), addr)
    if not addr_aligned then ti_addr:add_proto_expert_info(ef_alignment) end
    off = off + addr_n

    local tag, tag_n, tag_aligned = read_osc_string(tvb, off, limit)
    if tag == nil then return nil, tag_n end
    local ti_tag = tree:add(f.msg_tagstr, tvb(off, tag_n), tag)
    if not tag_aligned then ti_tag:add_proto_expert_info(ef_alignment) end
    if #tag == 0 or tag:sub(1, 1) ~= "," then
        ti_tag:add_proto_expert_info(ef_comma)
    end
    off = off + tag_n

    local tags_only = #tag >= 1 and tag:sub(1, 1) == "," and tag:sub(2) or tag
    local arg_count = 0
    for i = 1, #tags_only do
        local c = tags_only:sub(i, i)
        if c ~= "[" and c ~= "]" then arg_count = arg_count + 1 end
    end
    tree:add(f.msg_argcount, arg_count):set_generated()

    local uses_1_1 = false
    local open_brackets = 0
    for i = 1, #tags_only do
        local t = tags_only:sub(i, i)
        if t == "[" then open_brackets = open_brackets + 1 end
        if t == "]" then open_brackets = open_brackets - 1 end
        local n, err_or_is_1_1 = add_arg(tree, tvb, off, limit, t, pinfo)
        if n == nil then
            tree:add_proto_expert_info(ef_truncated)
            return nil, err_or_is_1_1
        end
        if t == "T" or t == "F" or t == "N" or t == "I" or t == "[" or t == "]" then
            uses_1_1 = true
        end
        if err_or_is_1_1 == "unknown" then
            tree:add_proto_expert_info(ef_unknown_tag)
            return nil, "unknown"
        end
        off = off + n
    end
    if open_brackets ~= 0 then tree:add_proto_expert_info(ef_arr_unbal) end

    return off - start, uses_1_1, addr, tag, arg_count
end

-- dissect_bundle returns (consumed, uses_1_1, summary_string)
local function dissect_bundle(tvb, off, limit, tree, pinfo, depth)
    local start = off
    local head, head_n, head_aligned = read_osc_string(tvb, off, limit)
    if head == nil then return nil, head_n end
    local sub_kind = tree:add(f.kind, tvb(off, 0), "bundle"):set_generated()
    tree:add(f.bundle_head, tvb(off, head_n), head)
    if not head_aligned then tree:add_proto_expert_info(ef_alignment) end
    off = off + head_n

    if off + 8 > limit then return nil, "truncated" end
    local tt_hi = tvb(off, 4):uint()
    local tt_lo = tvb(off + 4, 4):uint()
    local is_imm = (tt_hi == 0 and tt_lo == 1)
    local tt_tree = tree:add(f.bundle_tt, tvb(off, 8))
    tt_tree:add(f.bundle_tt_sec, tvb(off, 4))
    tt_tree:add(f.bundle_tt_fr,  tvb(off + 4, 4))
    tt_tree:add(f.bundle_tt_imm, tvb(off, 8), is_imm):set_generated()
    off = off + 8

    local msgs, subs = 0, 0
    local uses_1_1 = false

    while off < limit do
        if off + 4 > limit then tree:add_proto_expert_info(ef_truncated); return nil, "truncated" end
        local sz = tvb(off, 4):uint()
        local el_start = off + 4
        if el_start + sz > limit then tree:add_proto_expert_info(ef_truncated); return nil, "truncated" end

        local el_kind = "unknown"
        if sz >= 1 then
            local first = tvb(el_start, 1):uint()
            if first == 0x2F then el_kind = "message"
            elseif first == 0x23 then el_kind = "bundle"
            end
        end

        local el_tree = tree:add(osc, tvb(off, 4 + sz),
            string.format("Element #%d  kind=%s  size=%d", msgs + subs + 1, el_kind, sz))
        el_tree:add(f.element_size, tvb(off, 4))
        el_tree:add(f.element_kind, el_kind):set_generated()

        if el_kind == "message" then
            local n, u11 = dissect_message(tvb, el_start, el_start + sz, el_tree, pinfo)
            if n == nil then
                el_tree:add_proto_expert_info(ef_truncated)
                return nil, "truncated"
            end
            if u11 then uses_1_1 = true end
            msgs = msgs + 1
        elseif el_kind == "bundle" then
            local n, u11 = dissect_bundle(tvb, el_start, el_start + sz, el_tree, pinfo, depth + 1)
            if n == nil then
                el_tree:add_proto_expert_info(ef_truncated)
                return nil, "truncated"
            end
            if u11 then uses_1_1 = true end
            subs = subs + 1
        else
            el_tree:add_proto_expert_info(ef_truncated)
        end

        off = el_start + sz
    end

    tree:add(f.bundle_count, msgs + subs):set_generated()
    sub_kind:append_text(string.format("  (%d msg%s, %d sub-bundle%s)",
        msgs, msgs == 1 and "" or "s",
        subs, subs == 1 and "" or "s"))

    local summary = string.format("#bundle tt=%s msgs=%d subs=%d",
        is_imm and "immediate" or string.format("0x%08x.%08x", tt_hi, tt_lo),
        msgs, subs)
    return off - start, uses_1_1, summary
end

-- dissect_packet picks message or bundle based on first byte.
-- Returns (consumed, uses_1_1, info_summary).
dissect_packet = function(tvb, off, limit, tree, pinfo, depth)
    if off >= limit then return nil, "truncated" end
    local first = tvb(off, 1):uint()
    if first == 0x23 then  -- '#'
        return dissect_bundle(tvb, off, limit, tree, pinfo, depth or 0)
    elseif first == 0x2F then  -- '/'
        local n, u11, addr, tag, argc = dissect_message(tvb, off, limit, tree, pinfo)
        if n == nil then return nil, u11 end
        local tag_suffix = tag and ("[" .. tag:sub(2) .. "]") or "[?]"
        local summary = string.format("%s %s (%d arg%s)",
            addr or "(?)", tag_suffix, argc or 0, (argc or 0) == 1 and "" or "s")
        return n, u11, summary
    else
        return nil, "unknown"
    end
end

-- Annotates the top subtree with version + transport for every decode path.
local function annotate_envelope(tvb, tree, pinfo, transport, uses_1_1)
    tree:add(f.transport, transport):set_generated()
    local ver = uses_1_1 and "OSC 1.1" or "OSC 1.0"
    tree:add(f.version, ver):set_generated()
    pinfo.cols.protocol = uses_1_1 and "OSC/1.1" or "OSC/1.0"
end

-------------------------------------------------------------------------------
-- UDP dissector — one packet per datagram.
-------------------------------------------------------------------------------

local function dissect_udp(tvb, pinfo, tree)
    local limit = tvb:len()
    if limit < 4 then return 0 end
    local subtree = tree:add(osc, tvb(0, limit), "OSC (UDP datagram)")
    local n, uses_1_1, summary = dissect_packet(tvb, 0, limit, subtree, pinfo, 0)
    if n == nil then
        pinfo.cols.info:set("OSC malformed (" .. tostring(uses_1_1) .. ")")
        return limit
    end
    annotate_envelope(tvb, subtree, pinfo, "UDP", uses_1_1)
    pinfo.cols.info:set(summary)
    return n
end

-------------------------------------------------------------------------------
-- TCP length-prefix dissector (OSC 1.0).
-- Multiple frames may share a single segment; a single frame may span
-- segments. Desegmentation is handled via pinfo.desegment_*.
-------------------------------------------------------------------------------

local function dissect_tcp_lenprefix(tvb, pinfo, tree)
    local total = tvb:len()
    local off = 0
    local summaries = {}
    local combined_1_1 = false
    while off < total do
        if off + 4 > total then
            pinfo.desegment_offset = off
            pinfo.desegment_len = DESEGMENT_ONE_MORE_SEGMENT
            return total
        end
        local sz = tvb(off, 4):uint()
        if sz == 0 or sz > MAX_FRAME then
            local bad = tree:add(osc, tvb(off), "OSC 1.0 length-prefix unreasonable size")
            bad:add(f.lp_size, tvb(off, 4))
            bad:add_proto_expert_info(ef_lp_size)
            pinfo.cols.info:set(string.format("OSC-LP bad size=%d", sz))
            return total
        end
        if off + 4 + sz > total then
            pinfo.desegment_offset = off
            pinfo.desegment_len = (off + 4 + sz) - total
            return total
        end

        local subtree = tree:add(osc, tvb(off, 4 + sz), string.format("OSC 1.0 length-prefix  size=%d", sz))
        subtree:add(f.lp_size, tvb(off, 4))

        local n, uses_1_1, summary = dissect_packet(tvb, off + 4, off + 4 + sz, subtree, pinfo, 0)
        if n == nil then
            subtree:add_proto_expert_info(ef_truncated)
            summaries[#summaries + 1] = "malformed"
        else
            annotate_envelope(tvb, subtree, pinfo, "TCP/length-prefix", uses_1_1)
            if uses_1_1 then combined_1_1 = true end
            summaries[#summaries + 1] = summary
        end
        off = off + 4 + sz
    end
    pinfo.cols.protocol = combined_1_1 and "OSC/1.1" or "OSC/1.0"
    if #summaries == 1 then
        pinfo.cols.info:set(summaries[1])
    elseif #summaries > 1 then
        pinfo.cols.info:set(string.format("LP×%d: %s", #summaries, table.concat(summaries, " | ")))
    end
    return off
end

-------------------------------------------------------------------------------
-- TCP SLIP dissector (OSC 1.1).
-------------------------------------------------------------------------------

-- unstuff_slip walks tvb from offset, decoding up to a trailing END.
-- Returns (unstuffed_bytes_table, bytes_consumed) on success, or
-- (nil, reason) where reason is "truncated" or "bad_escape".
local function unstuff_slip(tvb, offset)
    local len = tvb:len()
    if offset >= len then return nil, "truncated" end
    if tvb(offset, 1):uint() ~= SLIP_END then return nil, "bad_escape" end
    local i = offset + 1
    while i < len and tvb(i, 1):uint() == SLIP_END do
        i = i + 1  -- tolerate multiple leading ENDs (1.1 double-END between frames)
    end
    if i >= len then return nil, "truncated" end

    local out = {}
    while i < len do
        local b = tvb(i, 1):uint()
        i = i + 1
        if b == SLIP_END then
            return out, (i - offset)
        elseif b == SLIP_ESC then
            if i >= len then return nil, "truncated" end
            local nxt = tvb(i, 1):uint()
            i = i + 1
            if     nxt == SLIP_ESC_END then out[#out + 1] = SLIP_END
            elseif nxt == SLIP_ESC_ESC then out[#out + 1] = SLIP_ESC
            else   return nil, "bad_escape" end
        else
            out[#out + 1] = b
        end
    end
    return nil, "truncated"
end

local function count_stuffed(tvb, start_off, stop_off)
    local n, i = 0, start_off
    while i < stop_off do
        if tvb(i, 1):uint() == SLIP_ESC then
            n = n + 1
            i = i + 2
        else
            i = i + 1
        end
    end
    return n
end

local function dissect_tcp_slip(tvb, pinfo, tree)
    local total = tvb:len()
    if total < 2 then
        pinfo.desegment_offset = 0
        pinfo.desegment_len = DESEGMENT_ONE_MORE_SEGMENT
        return 0
    end
    local offset = 0
    local summaries = {}
    local combined_1_1 = true  -- SLIP is 1.1 by definition

    while offset < total do
        local body_table, consumed = unstuff_slip(tvb, offset)
        if body_table == nil then
            local reason = consumed
            if reason == "truncated" then
                pinfo.desegment_offset = offset
                pinfo.desegment_len = DESEGMENT_ONE_MORE_SEGMENT
                if #summaries == 0 then
                    local t = tree:add(osc, tvb(offset), "OSC 1.1 SLIP (incomplete — desegmenting)")
                    t:add_proto_expert_info(ef_slip_trunc)
                    pinfo.cols.info:set("OSC-SLIP incomplete (desegmenting)")
                end
                return total
            end
            local t = tree:add(osc, tvb(offset), "OSC 1.1 SLIP (malformed escape)")
            t:add_proto_expert_info(ef_slip_bad)
            pinfo.cols.info:set("OSC-SLIP malformed")
            return total
        end

        -- Build a synthetic Tvb from the unstuffed bytes and dispatch our own decoder.
        local chars = {}
        for idx, byte in ipairs(body_table) do chars[idx] = string.char(byte) end
        local inner = ByteArray.new(table.concat(chars), true):tvb("unstuffed OSC")
        local body_len = inner:len()

        local subtree = tree:add(osc, tvb(offset, consumed), "OSC 1.1 SLIP")
        subtree:add(f.slip_end_start, tvb(offset, 1))
        subtree:add(f.slip_body_len, body_len):set_generated()
        local stuffed = count_stuffed(tvb, offset + 1, offset + consumed - 1)
        if stuffed > 0 then subtree:add(f.slip_stuffed, stuffed):set_generated() end
        subtree:add(f.slip_end_tail, tvb(offset + consumed - 1, 1))

        local n, uses_1_1, summary = dissect_packet(inner, 0, body_len, subtree, pinfo, 0)
        if n == nil then
            subtree:add_proto_expert_info(ef_truncated)
            summaries[#summaries + 1] = "malformed"
        else
            annotate_envelope(tvb, subtree, pinfo, "TCP/SLIP", uses_1_1 or true)
            summaries[#summaries + 1] = summary
        end

        offset = offset + consumed
    end

    pinfo.cols.protocol = "OSC/1.1"
    if #summaries == 1 then
        pinfo.cols.info:set("SLIP: " .. summaries[1])
    elseif #summaries > 1 then
        pinfo.cols.info:set(string.format("SLIP×%d: %s", #summaries, table.concat(summaries, " | ")))
    end
    return offset
end

-------------------------------------------------------------------------------
-- Proto.dissector — heuristic entry used when the port tables don't match.
-- Wireshark will call this if we register it as the port dissector; when
-- the heuristic pref is on we probe both length-prefix and SLIP shapes.
-------------------------------------------------------------------------------

function osc.dissector(tvb, pinfo, tree)
    local total = tvb:len()
    if total == 0 then return 0 end
    local first = tvb(0, 1):uint()
    local which = pinfo.match_uint
    -- UDP?
    if pinfo.port_type == 3 then  -- PT_UDP
        return dissect_udp(tvb, pinfo, tree)
    end
    -- TCP — decide by first byte.
    if first == SLIP_END then
        return dissect_tcp_slip(tvb, pinfo, tree)
    else
        return dissect_tcp_lenprefix(tvb, pinfo, tree)
    end
end

-------------------------------------------------------------------------------
-- Registration
-------------------------------------------------------------------------------

local slip_dissector = Proto("dhs_osc_slip", "OSC 1.1 SLIP framing (dhs)")
function slip_dissector.dissector(tvb, pinfo, tree)
    return dissect_tcp_slip(tvb, pinfo, tree)
end

local lp_dissector = Proto("dhs_osc_lp", "OSC 1.0 length-prefix (dhs)")
function lp_dissector.dissector(tvb, pinfo, tree)
    return dissect_tcp_lenprefix(tvb, pinfo, tree)
end

local udp_dissector = Proto("dhs_osc_udp", "OSC UDP (dhs)")
function udp_dissector.dissector(tvb, pinfo, tree)
    return dissect_udp(tvb, pinfo, tree)
end

local function register_ports()
    local udp = DissectorTable.get("udp.port")
    udp:add(osc.prefs.udp_port, udp_dissector)

    local tcp = DissectorTable.get("tcp.port")
    tcp:add(osc.prefs.tcp_len_port,  lp_dissector)
    tcp:add(osc.prefs.tcp_slip_port, slip_dissector)
end

register_ports()

function osc.prefs_changed()
    register_ports()
end
