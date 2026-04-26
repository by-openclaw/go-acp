-------------------------------------------------------------------------------
--
-- Wireshark Lua Dissector: dhs NMOS (AMWA) — Phase 1 step #1 scope
--
-- Covers the wire layers shipped in PR #148:
--
--   * mDNS / DNS-SD (UDP 5353)         — RFC 6762 + RFC 6763 + AMWA IS-04
--   * Unicast DNS-SD (UDP 53)          — RFC 6763 §10 — engaged when payload
--                                         carries _nmos-*._tcp
--
-- Decodes every record type the NMOS DNS-SD subset uses (PTR / SRV / TXT /
-- A / AAAA) byte-exactly; NMOS-specific Info column surfaces the service
-- type, instance name, host:port, and the IS-04 TXT keys (api_proto,
-- api_ver, api_auth, pri).
--
-- IS-04 / IS-05 / IS-07 / IS-12 HTTP+WS layers will be added to this same
-- dissector in later phases (per top-level CLAUDE.md "Wireshark
-- dissectors" rule — one file per protocol, every transport + every wire
-- version covered).
--
-- Wireshark 4.x (Lua 5.2/5.3/5.4). Pure-arithmetic bit ops only.
--
-- Refs:
--   RFC 1035   DNS message format
--   RFC 6762   Multicast DNS
--   RFC 6763   DNS-Based Service Discovery
--   AMWA IS-04 https://specs.amwa.tv/is-04/ (NMOS Discovery + Registration)
--
-------------------------------------------------------------------------------

local nmos = Proto("dhs_nmos", "AMWA NMOS (dhs — discovery layer)")

nmos.prefs.mdns_port    = Pref.uint("mDNS UDP port", 5353, "Multicast DNS port (RFC 6762)")
nmos.prefs.unicast_port = Pref.uint("Unicast DNS port", 53, "Unicast DNS-SD port (RFC 6763 §10)")
nmos.prefs.heuristic    = Pref.bool("Heuristic NMOS detect", true, "Mark unicast DNS as NMOS when payload references _nmos-*._tcp")

-------------------------------------------------------------------------------
-- Fields
-------------------------------------------------------------------------------

local f = nmos.fields

-- Envelope
f.transport     = ProtoField.string("dhs_nmos.transport",   "Transport")
f.kind          = ProtoField.string("dhs_nmos.kind",        "Message kind")
f.service       = ProtoField.string("dhs_nmos.service",     "NMOS service type")

-- Header (RFC 1035 §4.1.1)
f.h_id          = ProtoField.uint16("dhs_nmos.id",          "Transaction ID", base.HEX)
f.h_flags       = ProtoField.uint16("dhs_nmos.flags",       "Flags", base.HEX)
f.h_qr          = ProtoField.bool  ("dhs_nmos.flags.qr",    "QR (response bit)")
f.h_aa          = ProtoField.bool  ("dhs_nmos.flags.aa",    "AA (authoritative)")
f.h_tc          = ProtoField.bool  ("dhs_nmos.flags.tc",    "TC (truncated)")
f.h_qdcount     = ProtoField.uint16("dhs_nmos.qdcount",     "Question count")
f.h_ancount     = ProtoField.uint16("dhs_nmos.ancount",     "Answer count")
f.h_nscount     = ProtoField.uint16("dhs_nmos.nscount",     "Authority count")
f.h_arcount     = ProtoField.uint16("dhs_nmos.arcount",     "Additional count")

-- Question
f.q_name        = ProtoField.string("dhs_nmos.q.name",      "QNAME")
f.q_type        = ProtoField.uint16("dhs_nmos.q.type",      "QTYPE", base.DEC)
f.q_class       = ProtoField.uint16("dhs_nmos.q.class",     "QCLASS", base.HEX)
f.q_qu          = ProtoField.bool  ("dhs_nmos.q.qu",        "QU (unicast reply requested)")

-- RR (common)
f.rr_name       = ProtoField.string("dhs_nmos.rr.name",     "Name")
f.rr_type       = ProtoField.uint16("dhs_nmos.rr.type",     "Type", base.DEC)
f.rr_class      = ProtoField.uint16("dhs_nmos.rr.class",    "Class", base.HEX)
f.rr_flush      = ProtoField.bool  ("dhs_nmos.rr.flush",    "Cache-flush bit (mDNS)")
f.rr_ttl        = ProtoField.uint32("dhs_nmos.rr.ttl",      "TTL", base.DEC)
f.rr_rdlen      = ProtoField.uint16("dhs_nmos.rr.rdlen",    "RDLENGTH", base.DEC)

-- RR (per type)
f.a_addr        = ProtoField.ipv4  ("dhs_nmos.a.addr",      "A address")
f.aaaa_addr     = ProtoField.ipv6  ("dhs_nmos.aaaa.addr",   "AAAA address")
f.ptr_target    = ProtoField.string("dhs_nmos.ptr.target",  "PTR target")
f.srv_priority  = ProtoField.uint16("dhs_nmos.srv.priority","SRV priority")
f.srv_weight    = ProtoField.uint16("dhs_nmos.srv.weight",  "SRV weight")
f.srv_port      = ProtoField.uint16("dhs_nmos.srv.port",    "SRV port")
f.srv_target    = ProtoField.string("dhs_nmos.srv.target",  "SRV target")
f.txt_segment   = ProtoField.string("dhs_nmos.txt.segment", "TXT segment")

-- NMOS-aware highlights
f.nmos_api_proto = ProtoField.string("dhs_nmos.txt.api_proto","TXT api_proto")
f.nmos_api_ver   = ProtoField.string("dhs_nmos.txt.api_ver",  "TXT api_ver")
f.nmos_api_auth  = ProtoField.string("dhs_nmos.txt.api_auth", "TXT api_auth")
f.nmos_pri       = ProtoField.uint16("dhs_nmos.txt.pri",      "TXT pri")

-------------------------------------------------------------------------------
-- Expert info
-------------------------------------------------------------------------------

local e = nmos.experts
e.unknown_type   = ProtoExpert.new("dhs_nmos.unknown_type",   "Unknown record type",       expert.group.UNDECODED, expert.severity.WARN)
e.truncated      = ProtoExpert.new("dhs_nmos.truncated",      "Truncated DNS message",     expert.group.MALFORMED, expert.severity.ERROR)
e.bad_pointer    = ProtoExpert.new("dhs_nmos.bad_pointer",    "Bad name compression pointer", expert.group.MALFORMED, expert.severity.ERROR)
e.dev_priority   = ProtoExpert.new("dhs_nmos.dev_priority",   "TXT pri >= 100 (dev/lab)",  expert.group.PROTOCOL,  expert.severity.NOTE)

-------------------------------------------------------------------------------
-- Helpers
-------------------------------------------------------------------------------

-- Read a possibly-compressed DNS name starting at offset. Returns
-- (decoded, advance) where advance is the number of bytes the cursor
-- should move past the in-stream encoding (NOT counting any pointer
-- chase).
local function read_name(buf, offset)
    local labels = {}
    local pos = offset
    local advance_pos = nil
    local jumps = 0
    while true do
        if pos >= buf:len() then return nil, nil end
        local b = buf(pos, 1):uint()
        if b == 0 then
            if not advance_pos then advance_pos = pos + 1 end
            return table.concat(labels, "."), advance_pos - offset
        end
        local kind = math.floor(b / 64)  -- top 2 bits as 0..3
        if kind == 3 then
            -- pointer
            if pos + 1 >= buf:len() then return nil, nil end
            local ptr = (buf(pos, 1):uint() % 64) * 256 + buf(pos + 1, 1):uint()
            if ptr >= pos then return nil, nil end -- forward pointer / loop guard
            if not advance_pos then advance_pos = pos + 2 end
            pos = ptr
            jumps = jumps + 1
            if jumps > 32 then return nil, nil end
        elseif kind == 0 then
            local n = b
            if pos + 1 + n > buf:len() then return nil, nil end
            table.insert(labels, buf(pos + 1, n):string())
            pos = pos + 1 + n
        else
            -- Reserved label flags (0x40 / 0x80) — bail.
            return nil, nil
        end
    end
end

local rrtype_names = {
    [1]  = "A",
    [12] = "PTR",
    [16] = "TXT",
    [28] = "AAAA",
    [33] = "SRV",
    [255]= "ANY",
}

local function rrtype_name(t)
    return rrtype_names[t] or string.format("TYPE%d", t)
end

local function service_from_qname(name)
    -- Match _nmos-{...}._tcp inside name.
    if not name then return nil end
    for svc in name:gmatch("(_nmos%-[^.]+%._tcp)") do
        return svc
    end
    return nil
end

-------------------------------------------------------------------------------
-- Section dissectors
-------------------------------------------------------------------------------

local function dissect_question(buf, off, tree)
    local name, adv = read_name(buf, off)
    if not name then return nil end
    local p = off + adv
    if p + 4 > buf:len() then return nil end
    local qtype  = buf(p, 2):uint()
    local qclass = buf(p + 2, 2):uint()
    local q = tree:add(buf(off, p + 4 - off), string.format("Question: %s %s", name, rrtype_name(qtype)))
    q:add(f.q_name,  buf(off, adv), name)
    q:add(f.q_type,  buf(p, 2))
    q:add(f.q_class, buf(p + 2, 2))
    if qclass >= 0x8000 then
        q:add(f.q_qu, buf(p + 2, 2), true)
    end
    return p + 4, name, qtype
end

local function dissect_rr(buf, off, tree, label)
    local name, adv = read_name(buf, off)
    if not name then return nil end
    local p = off + adv
    if p + 10 > buf:len() then return nil end
    local rrtype  = buf(p, 2):uint()
    local rrclass = buf(p + 2, 2):uint()
    local ttl     = buf(p + 4, 4):uint()
    local rdlen   = buf(p + 8, 2):uint()
    if p + 10 + rdlen > buf:len() then return nil end

    local rdata_off = p + 10

    local sub_label = string.format("%s: %s %s ttl=%d", label or "Answer", name, rrtype_name(rrtype), ttl)
    local rr = tree:add(buf(off, rdata_off + rdlen - off), sub_label)
    rr:add(f.rr_name,  buf(off, adv), name)
    rr:add(f.rr_type,  buf(p, 2))
    rr:add(f.rr_class, buf(p + 2, 2))
    if rrclass >= 0x8000 then
        rr:add(f.rr_flush, buf(p + 2, 2), true)
    end
    rr:add(f.rr_ttl,   buf(p + 4, 4))
    rr:add(f.rr_rdlen, buf(p + 8, 2))

    -- Per-type rdata decoding.
    if rrtype == 1 and rdlen == 4 then
        rr:add(f.a_addr, buf(rdata_off, 4))
    elseif rrtype == 28 and rdlen == 16 then
        rr:add(f.aaaa_addr, buf(rdata_off, 16))
    elseif rrtype == 12 then
        local target, _ = read_name(buf, rdata_off)
        if target then rr:add(f.ptr_target, buf(rdata_off, rdlen), target) end
    elseif rrtype == 33 and rdlen >= 7 then
        rr:add(f.srv_priority, buf(rdata_off,     2))
        rr:add(f.srv_weight,   buf(rdata_off + 2, 2))
        rr:add(f.srv_port,     buf(rdata_off + 4, 2))
        local tgt, _ = read_name(buf, rdata_off + 6)
        if tgt then rr:add(f.srv_target, buf(rdata_off + 6, rdlen - 6), tgt) end
    elseif rrtype == 16 then
        local cur = rdata_off
        local txt_end = rdata_off + rdlen
        while cur < txt_end do
            local seglen = buf(cur, 1):uint()
            if cur + 1 + seglen > txt_end then break end
            local seg = buf(cur + 1, seglen):string()
            rr:add(f.txt_segment, buf(cur, 1 + seglen), seg)
            -- NMOS-aware TXT key callouts.
            local key, val = seg:match("^([^=]+)=(.*)$")
            if key then
                local k = key:lower()
                if k == "api_proto" then
                    rr:add(f.nmos_api_proto, buf(cur, 1 + seglen), val)
                elseif k == "api_ver" then
                    rr:add(f.nmos_api_ver,   buf(cur, 1 + seglen), val)
                elseif k == "api_auth" then
                    rr:add(f.nmos_api_auth,  buf(cur, 1 + seglen), val)
                elseif k == "pri" then
                    local n = tonumber(val)
                    if n then
                        rr:add(f.nmos_pri, buf(cur, 1 + seglen), n)
                        if n >= 100 then
                            rr:add_proto_expert_info(e.dev_priority)
                        end
                    end
                end
            end
            cur = cur + 1 + seglen
        end
    else
        if rrtype ~= 0 and not rrtype_names[rrtype] then
            rr:add_proto_expert_info(e.unknown_type)
        end
    end
    return rdata_off + rdlen, name, rrtype
end

-------------------------------------------------------------------------------
-- Top-level dissector
-------------------------------------------------------------------------------

local function looks_nmos(buf)
    -- Heuristic: payload contains the literal "_nmos-".
    local s = buf:bytes():tohex()
    -- "_nmos-" in ASCII = 5F 6E 6D 6F 73 2D
    return s:find("5f6e6d6f732d", 1, true) ~= nil
end

function nmos.dissector(buf, pinfo, tree)
    if buf:len() < 12 then return 0 end
    if not looks_nmos(buf) then
        -- No NMOS-related labels — let the regular DNS dissector handle it.
        return 0
    end

    local id      = buf(0, 2):uint()
    local flags   = buf(2, 2):uint()
    local qd, an, ns, ar = buf(4,2):uint(), buf(6,2):uint(), buf(8,2):uint(), buf(10,2):uint()
    local is_resp = math.floor(flags / 32768) == 1
    local is_aa   = math.floor((flags / 1024) % 2) == 1

    pinfo.cols.protocol:set("NMOS-DNSSD")
    local proto_tree = tree:add(nmos, buf(0, buf:len()))
    proto_tree:add(f.transport, buf(0, 0), pinfo.dst_port == 5353 and "mDNS" or "Unicast DNS-SD")
    proto_tree:add(f.kind, buf(0, 0), is_resp and "Response" or "Query")

    local hdr = proto_tree:add(buf(0, 12), string.format("Header  id=0x%04X qd=%d an=%d ns=%d ar=%d", id, qd, an, ns, ar))
    hdr:add(f.h_id, buf(0, 2))
    hdr:add(f.h_flags, buf(2, 2))
    hdr:add(f.h_qr, buf(2, 2), is_resp)
    hdr:add(f.h_aa, buf(2, 2), is_aa)
    hdr:add(f.h_tc, buf(2, 2), math.floor((flags / 512) % 2) == 1)
    hdr:add(f.h_qdcount, buf(4, 2))
    hdr:add(f.h_ancount, buf(6, 2))
    hdr:add(f.h_nscount, buf(8, 2))
    hdr:add(f.h_arcount, buf(10, 2))

    local off = 12
    local first_service = nil
    local first_qname = nil

    if qd > 0 then
        local qtree = proto_tree:add(buf(off, 0), string.format("Questions (%d)", qd))
        for i = 1, qd do
            local newoff, qname, _ = dissect_question(buf, off, qtree)
            if not newoff then
                proto_tree:add_proto_expert_info(e.truncated)
                break
            end
            if not first_qname then first_qname = qname end
            if not first_service then first_service = service_from_qname(qname) end
            off = newoff
        end
    end

    local dissect_section = function(count, label)
        if count == 0 then return end
        local stree = proto_tree:add(buf(off, 0), string.format("%s (%d)", label, count))
        for i = 1, count do
            local newoff, rname, _ = dissect_rr(buf, off, stree, label:sub(1, -2))
            if not newoff then
                proto_tree:add_proto_expert_info(e.truncated)
                return
            end
            if not first_service then first_service = service_from_qname(rname) end
            off = newoff
        end
    end
    dissect_section(an, "Answers")
    dissect_section(ns, "Authority")
    dissect_section(ar, "Additional")

    if first_service then
        proto_tree:add(f.service, buf(0, 0), first_service)
        pinfo.cols.info:set(string.format("%s %s qd=%d an=%d ns=%d ar=%d",
            is_resp and "RSP" or "QRY", first_service, qd, an, ns, ar))
    else
        local tag = first_qname or "(no qname)"
        pinfo.cols.info:set(string.format("%s %s qd=%d an=%d ns=%d ar=%d",
            is_resp and "RSP" or "QRY", tag, qd, an, ns, ar))
    end

    return buf:len()
end

-------------------------------------------------------------------------------
-- Registration
-------------------------------------------------------------------------------

DissectorTable.get("udp.port"):add(5353, nmos)

-- Heuristic dispatch on unicast DNS too — DissectorTable.set semantics
-- on UDP/53 would conflict with the built-in DNS dissector, so we use
-- a heuristic on the standard "udp" heuristic table instead.
local function nmos_heur(buf, pinfo, tree)
    if not nmos.prefs.heuristic then return false end
    if buf:len() < 12 then return false end
    if pinfo.src_port ~= 53 and pinfo.dst_port ~= 53 then return false end
    if not looks_nmos(buf) then return false end
    nmos.dissector(buf, pinfo, tree)
    return true
end
nmos:register_heuristic("udp", nmos_heur)
