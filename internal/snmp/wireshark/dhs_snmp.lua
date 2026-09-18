-------------------------------------------------------------------------------
--
-- Wireshark Lua Dissector: SNMP v1 / v2c / v3
--
-- Handles:
--   - v1  (RFC 1157): SEQUENCE { version(0), community, PDU }
--   - v2c (RFC 3416): SEQUENCE { version(1), community, PDU }
--   - v3  (RFC 3412/3414): SEQUENCE { version(3), msgGlobalData,
--            msgSecurityParameters (USM), msgData (scoped PDU | encrypted) }
--   - every PDU tag: GetRequest(A0), GetNextRequest(A1), Response(A2),
--            SetRequest(A3), Trap-PDU/v1(A4), GetBulkRequest(A5),
--            InformRequest(A6), SNMPv2-Trap(A7), Report(A8)
--   - every value tag we encode: INTEGER, OCTET STRING, NULL, OID,
--            IpAddress, Counter32, Gauge32/Unsigned32, TimeTicks, Opaque,
--            Counter64, and the RFC 3416 exceptions noSuchObject(80),
--            noSuchInstance(81), endOfMibView(82)
--
-- Default ports (configurable via the preferences below):
--   agent            : UDP 161
--   trap / notify    : UDP 162
--
-- The dissector decodes BER itself rather than delegating to Wireshark's
-- built-in `snmp`, so the `Protocol | Info` shape matches every other dhs
-- connector. Field abbrevs and the proto name carry the `dhs_` prefix to
-- avoid clashing with the built-in `snmp.*` fields.
--
-- Compatible with Wireshark 4.x (Lua 5.2 / 5.3).
-- Spec authority: internal/snmp/CLAUDE.md + RFC 1157 / 3412 / 3414 / 3416.
-- Wire authority: internal/snmp/codec/{message,v3,usmparams,pdu,value,tlv}.go
--
-------------------------------------------------------------------------------

local snmp = Proto("dhs_snmp", "SNMP (v1 / v2c / v3)")

-- ===== User preferences =====
snmp.prefs.agent_port = Pref.uint("UDP port — agent",      161, "Default 161 (GET/GETNEXT/GETBULK/SET/Response)")
snmp.prefs.trap_port  = Pref.uint("UDP port — trap/notify", 162, "Default 162 (Trap/SNMPv2-Trap/Inform)")

-- ===== Constants =====
local VERSION = { [0] = "v1", [1] = "v2c", [3] = "v3" }

-- PDU tags (context-specific constructed, RFC 3416 §3 / RFC 1157).
local PDU = {
  [0xA0] = "GetRequest",
  [0xA1] = "GetNextRequest",
  [0xA2] = "Response",
  [0xA3] = "SetRequest",
  [0xA4] = "Trap",          -- v1 only, different shape
  [0xA5] = "GetBulkRequest",
  [0xA6] = "InformRequest",
  [0xA7] = "SNMPv2-Trap",
  [0xA8] = "Report",
}

-- ASN.1 / SNMP value tags (internal/snmp/codec/tlv.go).
local TAG_INTEGER      = 0x02
local TAG_OCTETSTRING  = 0x04
local TAG_NULL         = 0x05
local TAG_OID          = 0x06
local TAG_SEQUENCE     = 0x30
local TAG_IPADDRESS    = 0x40
local TAG_COUNTER32    = 0x41
local TAG_GAUGE32      = 0x42
local TAG_TIMETICKS    = 0x43
local TAG_OPAQUE       = 0x44
local TAG_COUNTER64    = 0x46
local TAG_NOSUCHOBJECT = 0x80
local TAG_NOSUCHINST   = 0x81
local TAG_ENDOFMIB     = 0x82

local VALUE_TYPE = {
  [TAG_INTEGER]      = "INTEGER",
  [TAG_OCTETSTRING]  = "OCTET STRING",
  [TAG_NULL]         = "NULL",
  [TAG_OID]          = "OID",
  [TAG_IPADDRESS]    = "IpAddress",
  [TAG_COUNTER32]    = "Counter32",
  [TAG_GAUGE32]      = "Gauge32",
  [TAG_TIMETICKS]    = "TimeTicks",
  [TAG_OPAQUE]       = "Opaque",
  [TAG_COUNTER64]    = "Counter64",
  [TAG_NOSUCHOBJECT] = "noSuchObject",
  [TAG_NOSUCHINST]   = "noSuchInstance",
  [TAG_ENDOFMIB]     = "endOfMibView",
}

-- error-status (RFC 1157 §4.1.1 / RFC 3416 §3).
local ERRSTAT = {
  [0]  = "noError",       [1]  = "tooBig",         [2]  = "noSuchName",
  [3]  = "badValue",      [4]  = "readOnly",       [5]  = "genErr",
  [6]  = "noAccess",      [7]  = "wrongType",      [8]  = "wrongLength",
  [9]  = "wrongEncoding", [10] = "wrongValue",     [11] = "noCreation",
  [12] = "inconsistentValue", [13] = "resourceUnavailable",
  [14] = "commitFailed",  [15] = "undoFailed",     [16] = "authorizationError",
  [17] = "notWritable",   [18] = "inconsistentName",
}

-- generic-trap (RFC 1157 §4.1.6), v1 Trap-PDU only.
local GENTRAP = {
  [0] = "coldStart", [1] = "warmStart", [2] = "linkDown",
  [3] = "linkUp",    [4] = "authenticationFailure",
  [5] = "egpNeighborLoss", [6] = "enterpriseSpecific",
}

-- ===== Field definitions =====
local f = snmp.fields
f.version    = ProtoField.string("dhs_snmp.version",        "Version")
f.community  = ProtoField.string("dhs_snmp.community",      "Community")

-- v3 message header (RFC 3412 §6).
f.msg_id     = ProtoField.uint32("dhs_snmp.msgID",          "msgID")
f.msg_max    = ProtoField.uint32("dhs_snmp.msgMaxSize",     "msgMaxSize")
f.msg_flags  = ProtoField.uint8 ("dhs_snmp.msgFlags",       "msgFlags", base.HEX)
f.flag_auth  = ProtoField.bool  ("dhs_snmp.flags.auth",     "Auth")
f.flag_priv  = ProtoField.bool  ("dhs_snmp.flags.priv",     "Priv")
f.flag_rep   = ProtoField.bool  ("dhs_snmp.flags.reportable","Reportable")
f.sec_level  = ProtoField.string("dhs_snmp.securityLevel",  "Security level")
f.sec_model  = ProtoField.uint32("dhs_snmp.securityModel",  "msgSecurityModel")

-- USM security parameters (RFC 3414 §2.4).
f.usm_engine = ProtoField.bytes ("dhs_snmp.usm.engineID",   "msgAuthoritativeEngineID")
f.usm_boots  = ProtoField.uint32("dhs_snmp.usm.engineBoots","msgAuthoritativeEngineBoots")
f.usm_time   = ProtoField.uint32("dhs_snmp.usm.engineTime", "msgAuthoritativeEngineTime")
f.usm_user   = ProtoField.string("dhs_snmp.usm.userName",   "msgUserName")
f.usm_auth   = ProtoField.bytes ("dhs_snmp.usm.authParams", "msgAuthenticationParameters")
f.usm_priv   = ProtoField.bytes ("dhs_snmp.usm.privParams", "msgPrivacyParameters")

-- scoped PDU (RFC 3412 §6.3).
f.ctx_engine = ProtoField.bytes ("dhs_snmp.contextEngineID","contextEngineID")
f.ctx_name   = ProtoField.string("dhs_snmp.contextName",    "contextName")
f.encrypted  = ProtoField.bytes ("dhs_snmp.encryptedPDU",   "encryptedPDU (authPriv)")

-- PDU (RFC 3416 §3).
f.pdu_type   = ProtoField.string("dhs_snmp.pdu",            "PDU")
f.req_id     = ProtoField.int32 ("dhs_snmp.request_id",     "request-id")
f.err_stat   = ProtoField.string("dhs_snmp.error_status",   "error-status")
f.err_index  = ProtoField.int32 ("dhs_snmp.error_index",    "error-index")
f.non_reps   = ProtoField.int32 ("dhs_snmp.non_repeaters",  "non-repeaters")
f.max_reps   = ProtoField.int32 ("dhs_snmp.max_repetitions","max-repetitions")

-- v1 Trap-PDU (RFC 1157 §4.1.6).
f.enterprise = ProtoField.string("dhs_snmp.enterprise",     "enterprise")
f.agent_addr = ProtoField.ipv4  ("dhs_snmp.agent_addr",     "agent-addr")
f.gen_trap   = ProtoField.string("dhs_snmp.generic_trap",   "generic-trap")
f.spec_trap  = ProtoField.int32 ("dhs_snmp.specific_trap",  "specific-trap")
f.timestamp  = ProtoField.uint32("dhs_snmp.timestamp",      "time-stamp")

-- varbinds.
f.varbind    = ProtoField.string("dhs_snmp.varbind",        "VarBind")
f.vb_name    = ProtoField.string("dhs_snmp.varbind.name",   "name (OID)")
f.vb_type    = ProtoField.string("dhs_snmp.varbind.type",   "type")
f.vb_value   = ProtoField.string("dhs_snmp.varbind.value",  "value")

f.ber_tag    = ProtoField.uint8 ("dhs_snmp.ber.tag",        "BER tag", base.HEX)
f.ber_len    = ProtoField.uint32("dhs_snmp.ber.length",     "BER length")

-- ===== Expert info =====
local ef = {}
ef.badber   = ProtoExpert.new("dhs_snmp.ef.badber",  "malformed BER", expert.group.MALFORMED, expert.severity.ERROR)
ef.unknown  = ProtoExpert.new("dhs_snmp.ef.unknown", "unknown tag",   expert.group.UNDECODED, expert.severity.WARN)
ef.trunc    = ProtoExpert.new("dhs_snmp.ef.trunc",   "truncated",     expert.group.MALFORMED, expert.severity.ERROR)
snmp.experts = { ef.badber, ef.unknown, ef.trunc }

-------------------------------------------------------------------------------
-- BER helpers
--
-- Each returns the decoded value plus the offset of the next element, so a
-- caller reads a sequence by threading the offset. `read_tl` returns the tag,
-- the content offset and the content length; content is read from there.
-------------------------------------------------------------------------------

-- read_tl reads one tag + length header at off. Returns tag, content_off,
-- content_len, next_off (the byte after the whole element), or nil on a
-- header that runs off the end.
local function read_tl(tvb, off, limit)
  if off + 2 > limit then return nil end
  local tag = tvb(off, 1):uint()
  local lb  = tvb(off + 1, 1):uint()
  local coff, clen
  if lb < 0x80 then
    coff = off + 2
    clen = lb
  else
    local n = lb - 0x80
    if n == 0 or n > 4 or off + 2 + n > limit then return nil end
    clen = 0
    for i = 0, n - 1 do
      clen = clen * 256 + tvb(off + 2 + i, 1):uint()
    end
    coff = off + 2 + n
  end
  if coff + clen > limit then return nil end
  return tag, coff, clen, coff + clen
end

-- ber_int reads a signed base-256 integer of clen bytes (<= 8).
local function ber_int(tvb, coff, clen)
  if clen == 0 then return 0 end
  local n = clen
  if n > 8 then n = 8 end
  local v = tvb(coff, 1):uint()
  local neg = v >= 0x80
  local acc = neg and (v - 256) or v
  for i = 1, n - 1 do
    acc = acc * 256 + tvb(coff + i, 1):uint()
  end
  return acc
end

-- ber_uint reads an unsigned base-256 integer (Counter/Gauge/TimeTicks may
-- carry a leading zero byte to stay positive).
local function ber_uint(tvb, coff, clen)
  local acc = 0
  local n = clen
  if n > 8 then n = 8 end
  for i = 0, n - 1 do
    acc = acc * 256 + tvb(coff + i, 1):uint()
  end
  return acc
end

-- oid_string decodes an OID's content into dotted form (RFC BER 8.19).
local function oid_string(tvb, coff, clen)
  if clen == 0 then return "" end
  local first = tvb(coff, 1):uint()
  local parts = { math.floor(first / 40), first % 40 }
  local val, i = 0, 1
  while i < clen do
    local b = tvb(coff + i, 1):uint()
    val = val * 128 + (b % 128)
    if b < 0x80 then
      parts[#parts + 1] = val
      val = 0
    end
    i = i + 1
  end
  return table.concat(parts, ".")
end

-- printable renders an OCTET STRING as text when it is all printable, else as
-- hex, which is how an operator tells a sysName from an engine ID by eye.
local function printable(tvb, coff, clen)
  if clen == 0 then return "" end
  local raw = tvb(coff, clen):bytes()
  local ok = true
  for i = 0, clen - 1 do
    local c = raw:get_index(i)
    if c < 0x20 or c > 0x7e then ok = false break end
  end
  if ok then return tvb(coff, clen):string() end
  return "0x" .. tostring(tvb(coff, clen):bytes():tohex())
end

-------------------------------------------------------------------------------
-- Value + varbind decode
-------------------------------------------------------------------------------

-- decode_value renders one varbind value from its tag, returning a short
-- string for the tree and the Info column.
local function decode_value(tvb, tag, coff, clen)
  local name = VALUE_TYPE[tag]
  if tag == TAG_INTEGER then
    return string.format("%d", ber_int(tvb, coff, clen))
  elseif tag == TAG_OCTETSTRING then
    return string.format("%q", printable(tvb, coff, clen))
  elseif tag == TAG_NULL then
    return "NULL"
  elseif tag == TAG_OID then
    return oid_string(tvb, coff, clen)
  elseif tag == TAG_IPADDRESS then
    if clen == 4 then
      return string.format("%d.%d.%d.%d",
        tvb(coff,1):uint(), tvb(coff+1,1):uint(), tvb(coff+2,1):uint(), tvb(coff+3,1):uint())
    end
    return "0x" .. tostring(tvb(coff, clen):bytes():tohex())
  elseif tag == TAG_COUNTER32 or tag == TAG_GAUGE32 or tag == TAG_TIMETICKS then
    return string.format("%d", ber_uint(tvb, coff, clen))
  elseif tag == TAG_COUNTER64 then
    return string.format("%d", ber_uint(tvb, coff, clen))
  elseif tag == TAG_OPAQUE then
    return "0x" .. tostring(tvb(coff, clen):bytes():tohex())
  elseif tag == TAG_NOSUCHOBJECT or tag == TAG_NOSUCHINST or tag == TAG_ENDOFMIB then
    return name
  end
  return nil -- unknown
end

-- decode_varbind reads one VarBind SEQUENCE { name OID, value } at off and
-- adds it to the tree. Returns "oid=value" for the Info column and next_off.
local function decode_varbind(tvb, tree, off, limit)
  local tag, coff, clen, noff = read_tl(tvb, off, limit)
  if not tag then
    tree:add_proto_expert_info(ef.trunc, "varbind ran off the end")
    return nil, limit
  end
  local vbt = tree:add(f.varbind, tvb(off, noff - off))
  if tag ~= TAG_SEQUENCE then
    vbt:add_proto_expert_info(ef.badber, string.format("varbind tag 0x%02x, want SEQUENCE", tag))
    return nil, noff
  end

  -- name
  local ntag, ncoff, nclen, nnoff = read_tl(tvb, coff, coff + clen)
  if not ntag or ntag ~= TAG_OID then
    vbt:add_proto_expert_info(ef.badber, "varbind name is not an OID")
    return nil, noff
  end
  local oid = oid_string(tvb, ncoff, nclen)
  vbt:add(f.vb_name, tvb(ncoff, nclen), oid)

  -- value
  local vtag, vcoff, vclen = read_tl(tvb, nnoff, coff + clen)
  if not vtag then
    vbt:add_proto_expert_info(ef.trunc, "varbind value ran off the end")
    return oid .. "=?", noff
  end
  local tname = VALUE_TYPE[vtag] or string.format("tag 0x%02x", vtag)
  vbt:add(f.vb_type, tvb(nnoff, 1), tname)
  local rendered = decode_value(tvb, vtag, vcoff, vclen)
  if rendered == nil then
    vbt:add_proto_expert_info(ef.unknown, string.format("unknown value tag 0x%02x", vtag))
    rendered = "0x" .. tostring(tvb(vcoff, vclen):bytes():tohex())
  end
  vbt:add(f.vb_value, tvb(vcoff, vclen), rendered)
  vbt:append_text(string.format(": %s = %s", oid, rendered))
  return oid .. "=" .. rendered, noff
end

-- decode_varbinds walks the varbind-list SEQUENCE and returns a short summary
-- (first OID + count) for the Info column.
local function decode_varbinds(tvb, tree, off, limit)
  local tag, coff, clen, noff = read_tl(tvb, off, limit)
  if not tag or tag ~= TAG_SEQUENCE then
    tree:add_proto_expert_info(ef.badber, "varbind-list is not a SEQUENCE")
    return "", noff or limit
  end
  local vbl = tree:add(tvb(off, noff - off), "variable-bindings")
  local first, count = nil, 0
  local p = coff
  while p < coff + clen do
    local summary, np = decode_varbind(tvb, vbl, p, coff + clen)
    if summary and not first then first = summary end
    if summary then count = count + 1 end
    if np <= p then break end
    p = np
  end
  vbl:append_text(string.format(" (%d)", count))
  if not first then return "", noff end
  if count == 1 then return first, noff end
  return string.format("%s (+%d more)", first, count - 1), noff
end

-------------------------------------------------------------------------------
-- PDU decode
-------------------------------------------------------------------------------

-- decode_std_pdu handles the shared shape of Get/GetNext/Response/Set/GetBulk/
-- Inform/SNMPv2-Trap/Report. GetBulk renames the two middle INTEGERs.
local function decode_std_pdu(tvb, tree, pdutag, coff, clen)
  local pname = PDU[pdutag]
  local info = {}
  local p = coff

  local rtag, rcoff, rclen, rnoff = read_tl(tvb, p, coff + clen)
  if not rtag then tree:add_proto_expert_info(ef.trunc, "PDU truncated"); return pname end
  tree:add(f.req_id, tvb(rcoff, rclen), ber_int(tvb, rcoff, rclen))
  info[#info+1] = string.format("req=%d", ber_int(tvb, rcoff, rclen))
  p = rnoff

  local a1tag, a1coff, a1clen, a1noff = read_tl(tvb, p, coff + clen)
  local a2tag, a2coff, a2clen, a2noff = read_tl(tvb, a1noff or (coff + clen), coff + clen)
  if a1tag and a2tag then
    if pdutag == 0xA5 then -- GetBulk: non-repeaters, max-repetitions
      tree:add(f.non_reps, tvb(a1coff, a1clen), ber_int(tvb, a1coff, a1clen))
      tree:add(f.max_reps, tvb(a2coff, a2clen), ber_int(tvb, a2coff, a2clen))
      info[#info+1] = string.format("nonrep=%d maxrep=%d",
        ber_int(tvb, a1coff, a1clen), ber_int(tvb, a2coff, a2clen))
    else -- error-status, error-index
      local es = ber_int(tvb, a1coff, a1clen)
      tree:add(f.err_stat, tvb(a1coff, a1clen), ERRSTAT[es] or tostring(es))
      tree:add(f.err_index, tvb(a2coff, a2clen), ber_int(tvb, a2coff, a2clen))
      if es ~= 0 then
        info[#info+1] = string.format("err=%s@%d", ERRSTAT[es] or es, ber_int(tvb, a2coff, a2clen))
      end
    end
    p = a2noff
  end

  local vbsummary = decode_varbinds(tvb, tree, p, coff + clen)
  if vbsummary ~= "" then info[#info+1] = vbsummary end
  return pname .. " " .. table.concat(info, " ")
end

-- decode_trap_v1 handles the v1 Trap-PDU (RFC 1157 §4.1.6), a different shape.
local function decode_trap_v1(tvb, tree, coff, clen)
  local info = {}
  local p = coff

  local etag, ecoff, eclen, enoff = read_tl(tvb, p, coff + clen)
  if etag == TAG_OID then
    local oid = oid_string(tvb, ecoff, eclen)
    tree:add(f.enterprise, tvb(ecoff, eclen), oid)
    info[#info+1] = "ent=" .. oid
    p = enoff
  end

  local atag, acoff, aclen, anoff = read_tl(tvb, p, coff + clen)
  if atag == TAG_IPADDRESS and aclen == 4 then
    tree:add(f.agent_addr, tvb(acoff, aclen))
    p = anoff
  elseif atag then
    p = anoff
  end

  local gtag, gcoff, gclen, gnoff = read_tl(tvb, p, coff + clen)
  if gtag == TAG_INTEGER then
    local g = ber_int(tvb, gcoff, gclen)
    tree:add(f.gen_trap, tvb(gcoff, gclen), GENTRAP[g] or tostring(g))
    info[#info+1] = "generic=" .. (GENTRAP[g] or g)
    p = gnoff
  end

  local stag, scoff, sclen, snoff = read_tl(tvb, p, coff + clen)
  if stag == TAG_INTEGER then
    tree:add(f.spec_trap, tvb(scoff, sclen), ber_int(tvb, scoff, sclen))
    p = snoff
  end

  local ttag, tcoff, tclen, tnoff = read_tl(tvb, p, coff + clen)
  if ttag == TAG_TIMETICKS or ttag == TAG_INTEGER then
    tree:add(f.timestamp, tvb(tcoff, tclen), ber_uint(tvb, tcoff, tclen))
    p = tnoff
  end

  local vbsummary = decode_varbinds(tvb, tree, p, coff + clen)
  if vbsummary ~= "" then info[#info+1] = vbsummary end
  return "Trap " .. table.concat(info, " ")
end

-- decode_pdu dispatches on the PDU tag. Returns the Info-column string.
local function decode_pdu(tvb, tree, off, limit)
  local tag, coff, clen, noff = read_tl(tvb, off, limit)
  if not tag then
    tree:add_proto_expert_info(ef.trunc, "PDU header ran off the end")
    return "?"
  end
  local pname = PDU[tag]
  local pt = tree:add(tvb(off, noff - off), "PDU")
  if not pname then
    pt:add_proto_expert_info(ef.unknown, string.format("unknown PDU tag 0x%02x", tag))
    return string.format("PDU 0x%02x", tag)
  end
  pt:add(f.pdu_type, tvb(off, 1), pname)
  pt:append_text(": " .. pname)
  if tag == 0xA4 then
    return decode_trap_v1(tvb, pt, coff, clen)
  end
  return decode_std_pdu(tvb, pt, tag, coff, clen)
end

-------------------------------------------------------------------------------
-- USM security parameters (the OCTET STRING wraps a SEQUENCE)
-------------------------------------------------------------------------------

local function decode_usm(tvb, tree, coff, clen, info)
  local seqtag, scoff, sclen = read_tl(tvb, coff, coff + clen)
  if not seqtag or seqtag ~= TAG_SEQUENCE then
    tree:add_proto_expert_info(ef.badber, "USM parameters are not a SEQUENCE")
    return
  end
  local u = tree:add(tvb(coff, clen), "USM Security Parameters")
  local p = scoff
  local function field()
    local t, c, l, n = read_tl(tvb, p, scoff + sclen)
    if not t then return nil end
    p = n
    return c, l, t
  end
  local c, l = field(); if c then u:add(f.usm_engine, tvb(c, l)) end
  c, l = field(); if c then u:add(f.usm_boots, tvb(c, l), ber_int(tvb, c, l)) end
  c, l = field(); if c then u:add(f.usm_time,  tvb(c, l), ber_int(tvb, c, l)) end
  c, l = field(); if c then
    local user = tvb(c, l):string()
    u:add(f.usm_user, tvb(c, l), user)
    if user ~= "" then info[#info+1] = "user=" .. user end
  end
  c, l = field(); if c and l > 0 then u:add(f.usm_auth, tvb(c, l)) end
  c, l = field(); if c and l > 0 then u:add(f.usm_priv, tvb(c, l)) end
end

-------------------------------------------------------------------------------
-- Message decode
-------------------------------------------------------------------------------

-- decode_v3 reads the v3 global data, USM parameters and scoped PDU.
local function decode_v3(tvb, tree, off, limit, info)
  -- msgGlobalData SEQUENCE { msgID, msgMaxSize, msgFlags, msgSecurityModel }
  local gtag, gcoff, gclen, gnoff = read_tl(tvb, off, limit)
  if not gtag or gtag ~= TAG_SEQUENCE then
    tree:add_proto_expert_info(ef.badber, "msgGlobalData is not a SEQUENCE")
    return
  end
  local g = tree:add(tvb(off, gnoff - off), "msgGlobalData")
  local p = gcoff
  do local a,b,d,n = read_tl(tvb, p, gcoff+gclen); if a then g:add(f.msg_id, tvb(b,d), ber_int(tvb,b,d)); p = n end end
  do local a,b,d,n = read_tl(tvb, p, gcoff+gclen); if a then g:add(f.msg_max, tvb(b,d), ber_int(tvb,b,d)); p = n end end
  do local a,b,d,n = read_tl(tvb, p, gcoff+gclen)
     if a then
       local fl = d > 0 and tvb(b,1):uint() or 0
       local ft = g:add(f.msg_flags, tvb(b, d))
       ft:add(f.flag_auth, tvb(b, d), (fl % 2))
       ft:add(f.flag_priv, tvb(b, d), (math.floor(fl/2) % 2))
       ft:add(f.flag_rep,  tvb(b, d), (math.floor(fl/4) % 2))
       local lvl = "noAuthNoPriv"
       if (math.floor(fl/2) % 2) == 1 then lvl = "authPriv"
       elseif (fl % 2) == 1 then lvl = "authNoPriv" end
       g:add(f.sec_level, tvb(b, d), lvl)
       info[#info+1] = lvl
       p = n
     end
  end
  do local a,b,d,n = read_tl(tvb, p, gcoff+gclen); if a then g:add(f.sec_model, tvb(b,d), ber_int(tvb,b,d)); p = n end end

  -- msgSecurityParameters OCTET STRING (wraps the USM SEQUENCE)
  local stag, scoff, sclen, snoff = read_tl(tvb, gnoff, limit)
  if stag == TAG_OCTETSTRING and sclen > 0 then
    decode_usm(tvb, tree, scoff, sclen, info)
  end
  local after_sec = snoff or gnoff

  -- msgData: scoped PDU SEQUENCE (plaintext) OR OCTET STRING (encrypted)
  local dtag, dcoff, dclen = read_tl(tvb, after_sec, limit)
  if not dtag then return end
  if dtag == TAG_OCTETSTRING then
    tree:add(f.encrypted, tvb(dcoff, dclen))
    info[#info+1] = "encryptedPDU"
    return
  end
  if dtag ~= TAG_SEQUENCE then
    tree:add_proto_expert_info(ef.badber, "msgData is neither a scoped-PDU SEQUENCE nor encryptedPDU")
    return
  end
  -- scoped PDU: contextEngineID, contextName, PDU
  local sp = tree:add(tvb(after_sec, (dcoff + dclen) - after_sec), "Scoped PDU")
  local q = dcoff
  do local a,b,d,n = read_tl(tvb, q, dcoff+dclen); if a then sp:add(f.ctx_engine, tvb(b, d)); q = n end end
  do local a,b,d,n = read_tl(tvb, q, dcoff+dclen); if a then sp:add(f.ctx_name, tvb(b, d), tvb(b,d):string()); q = n end end
  local pinfo_str = decode_pdu(tvb, sp, q, dcoff + dclen)
  info[#info+1] = pinfo_str
end

-- decode_message reads the top-level SEQUENCE and dispatches by version.
local function decode_message(tvb, pinfo, tree)
  local limit = tvb:len()
  local seqtag, coff, clen = read_tl(tvb, 0, limit)
  if not seqtag or seqtag ~= TAG_SEQUENCE then
    tree:add_proto_expert_info(ef.badber, "message is not a SEQUENCE")
    return
  end

  -- version INTEGER
  local vtag, vcoff, vclen, vnoff = read_tl(tvb, coff, coff + clen)
  if not vtag or vtag ~= TAG_INTEGER then
    tree:add_proto_expert_info(ef.badber, "version is not an INTEGER")
    return
  end
  local ver = ber_int(tvb, vcoff, vclen)
  local vname = VERSION[ver] or ("v?" .. ver)
  tree:add(f.version, tvb(vcoff, vclen), vname)

  local info = {}

  if ver == 3 then
    pinfo.cols.protocol = "SNMPv3"
    decode_v3(tvb, tree, vnoff, coff + clen, info)
  else
    pinfo.cols.protocol = (ver == 0) and "SNMPv1" or "SNMPv2c"
    -- community OCTET STRING
    local ctag, ccoff, cclen, cnoff = read_tl(tvb, vnoff, coff + clen)
    if ctag == TAG_OCTETSTRING then
      local comm = tvb(ccoff, cclen):string()
      tree:add(f.community, tvb(ccoff, cclen), comm)
      info[#info+1] = 'community="' .. comm .. '"'
      -- PDU
      local pinfo_str = decode_pdu(tvb, tree, cnoff, coff + clen)
      info[#info+1] = pinfo_str
    else
      tree:add_proto_expert_info(ef.badber, "community is not an OCTET STRING")
    end
  end

  pinfo.cols.info = table.concat(info, "  ")
end

-------------------------------------------------------------------------------
-- Dissector entry + registration
-------------------------------------------------------------------------------

function snmp.dissector(tvb, pinfo, root)
  if tvb:len() < 2 then return 0 end
  -- Every SNMP message is a BER SEQUENCE; a datagram that does not start 0x30
  -- is not ours, so decline it rather than draw a malformed tree.
  if tvb(0, 1):uint() ~= TAG_SEQUENCE then return 0 end
  local tree = root:add(snmp, tvb(), "SNMP")
  decode_message(tvb, pinfo, tree)
  return tvb:len()
end

local function register()
  local udp = DissectorTable.get("udp.port")
  udp:add(snmp.prefs.agent_port, snmp)
  udp:add(snmp.prefs.trap_port, snmp)
end

register()

-- Re-bind if the operator changes the port preferences.
local last_agent, last_trap = snmp.prefs.agent_port, snmp.prefs.trap_port
function snmp.prefs_changed()
  local udp = DissectorTable.get("udp.port")
  if last_agent ~= snmp.prefs.agent_port then
    udp:remove(last_agent, snmp); udp:add(snmp.prefs.agent_port, snmp)
    last_agent = snmp.prefs.agent_port
  end
  if last_trap ~= snmp.prefs.trap_port then
    udp:remove(last_trap, snmp); udp:add(snmp.prefs.trap_port, snmp)
    last_trap = snmp.prefs.trap_port
  end
end
