-------------------------------------------------------------------------------
--
-- Wireshark Lua Dissector: EVS Cerebrum Northbound API (Neuron Bridge)
--
-- Decodes XML-over-WebSocket on TCP port 40007 (configurable).
-- Never delegates to Wireshark's built-in WebSocket dissector.
--
-- Coverage:
--   * HTTP/1.1 handshake (GET / + 101 Switching Protocols)
--   * RFC 6455 frames: FIN/RSV/opcode/MASK/payload-len(7/16/64)/key/payload
--   * Text frames carrying one Cerebrum NB XML document — root +
--     MTID/TYPE/ERROR/ERROR_CODE/DEVICE_TYPE/RESULT-VALUE surfaced in
--     the Info column
--   * Every command + event root from EVS Cerebrum NB 0v13:
--       LOGIN/LOGIN_REPLY, POLL/POLL_REPLY, ACTION, SUBSCRIBE, OBTAIN,
--       UNSUBSCRIBE, UNSUBSCRIBE_ALL, ACK, NACK, BUSY,
--       ROUTING, CATEGORY, SALVO, DEVICE,
--       ROUTING_CHANGE, CATEGORY_CHANGE, SALVO_CHANGE,
--       DEVICE_CHANGE, DATASTORE_CHANGE
--   * The 0v16 additions over that baseline:
--       DEVICE_CONFIGURATION (§4.5, the ADD/MODIFY/REMOVE CRUD command +
--         its RX <RESULT VALUE=…> reply envelope; DEVICE_TYPE surfaced),
--       CONTINUE (§1.4 flow-control after BUSY),
--       WILDCARD_COMPLETE (§1.6 end-of-snapshot),
--       DATASTORES (§2.1 LOGIN_REPLY body).
--     The RESULT VALUE (ACCEPTED/FAILED) and DEVICE_TYPE attrs are pulled
--     into the Info column so a device-config round-trip is readable at a
--     glance.
--   * Live-wire facts (2026-08 production captures — docs/keys.md
--     "wire-actual" notes):
--       LOCK / LOCK_STATE / LOCKED_BY on routing actions + events —
--         incl. LOCK_STATE="RELEASED", the undocumented sixth value that
--         is the wire-actual clearing state;
--       top-level VALUE attr on DEVICE_CHANGE value events (change
--         events carry VALUE on the root, NOT an OBJECT_VALUE child);
--       MIN/MAX/STEP descriptor attrs on VALUE rows;
--       positional <DEVICE_N …> sub-device children in DETAILS replies
--         (surfaced as a sub-device count);
--       MTID-less WILDCARD_COMPLETE — the live NOC emits a spurious one
--         after every event (only MTID-carrying ones end a wildcard
--         request) — flagged with an expert NOTE, not an error.
--   * Per-message Info column with direction arrow + verb + key attrs
--     (DEST_ID/SRCE_ID/LEVEL_ID, DEVICE_NAME/SUB_DEVICE/OBJECT,
--     CATEGORY/GROUP/INSTANCE, MNEMONIC) so each frame is uniquely
--     identifiable at a glance
--   * Any XML root NOT in the known-root catalogue fires a Wireshark
--     expert WARNING (never a silent fallthrough), per the root CLAUDE.md
--     dissector rule.
--
-- 0v16 is the authoritative spec version; 0v13 remains the historical
-- baseline and every 0v13 root above still decodes unchanged.
--
-- Refs:
--   RFC 6455 (WebSocket):  https://www.rfc-editor.org/rfc/rfc6455
--   EVS Cerebrum NB 0v16:  internal/cerebrum-nb/assets/Cerebrum Northbound API 0v16.pdf
--                          (catalogue: internal/cerebrum-nb/docs/keys.md)
--
-------------------------------------------------------------------------------

local p_cnb = Proto("dhs_cerebrum_nb", "EVS Cerebrum NB (dhs — XML over WebSocket)")

p_cnb.prefs.tcp_port = Pref.uint("TCP port", 40007, "TCP port carrying Cerebrum NB WebSocket")

-------------------------------------------------------------------------------
-- Fields
-------------------------------------------------------------------------------

local f = p_cnb.fields

-- Stream phase
f.phase           = ProtoField.string("dhs_cerebrum_nb.phase", "Phase")

-- Handshake
f.hs_request      = ProtoField.string("dhs_cerebrum_nb.handshake.request",  "HTTP request")
f.hs_response     = ProtoField.string("dhs_cerebrum_nb.handshake.response", "HTTP response")

-- WebSocket frame fields
f.ws_fin          = ProtoField.bool  ("dhs_cerebrum_nb.ws.fin",          "FIN")
f.ws_rsv          = ProtoField.uint8 ("dhs_cerebrum_nb.ws.rsv",          "RSV1..3", base.HEX, nil, 0x70)
f.ws_opcode       = ProtoField.uint8 ("dhs_cerebrum_nb.ws.opcode",       "Opcode",  base.HEX)
f.ws_opcode_name  = ProtoField.string("dhs_cerebrum_nb.ws.opcode_name",  "Opcode name")
f.ws_masked       = ProtoField.bool  ("dhs_cerebrum_nb.ws.masked",       "MASK")
f.ws_len7         = ProtoField.uint8 ("dhs_cerebrum_nb.ws.len7",         "Payload len (7-bit field)", base.DEC)
f.ws_len_ext16    = ProtoField.uint16("dhs_cerebrum_nb.ws.len16",        "Payload len (16-bit ext)",  base.DEC)
f.ws_len_ext64    = ProtoField.uint64("dhs_cerebrum_nb.ws.len64",        "Payload len (64-bit ext)",  base.DEC)
f.ws_mask_key     = ProtoField.bytes ("dhs_cerebrum_nb.ws.mask_key",     "Masking key", base.NONE)

-- XML payload
f.xml_root        = ProtoField.string("dhs_cerebrum_nb.xml.root",         "XML root")
f.xml_mtid        = ProtoField.string("dhs_cerebrum_nb.xml.mtid",         "MTID")
f.xml_type        = ProtoField.string("dhs_cerebrum_nb.xml.type",         "TYPE")
f.xml_error       = ProtoField.string("dhs_cerebrum_nb.xml.error",        "ERROR")
f.xml_error_code  = ProtoField.string("dhs_cerebrum_nb.xml.error_code",   "ERROR_CODE")
-- 0v16 additions
f.xml_device_type = ProtoField.string("dhs_cerebrum_nb.xml.device_type",  "DEVICE_TYPE")
f.xml_result      = ProtoField.string("dhs_cerebrum_nb.xml.result",       "RESULT VALUE")
-- Routing identity attrs (per-command Info detail)
f.xml_dest_id     = ProtoField.string("dhs_cerebrum_nb.xml.dest_id",      "DEST_ID")
f.xml_srce_id     = ProtoField.string("dhs_cerebrum_nb.xml.srce_id",      "SRCE_ID")
f.xml_level_id    = ProtoField.string("dhs_cerebrum_nb.xml.level_id",     "LEVEL_ID")
f.xml_mnemonic    = ProtoField.string("dhs_cerebrum_nb.xml.mnemonic",     "MNEMONIC")
-- Lock attrs (wire-actual: LOCK on actions, LOCK_STATE + LOCKED_BY on
-- events; RELEASED is the undocumented clearing value — live 2026-08-16)
f.xml_lock        = ProtoField.string("dhs_cerebrum_nb.xml.lock",         "LOCK")
f.xml_lock_state  = ProtoField.string("dhs_cerebrum_nb.xml.lock_state",   "LOCK_STATE")
f.xml_locked_by   = ProtoField.string("dhs_cerebrum_nb.xml.locked_by",    "LOCKED_BY")
-- Device-object attrs (§5.4 VALUE rows; top-level VALUE is wire-actual on
-- change events — no OBJECT_VALUE child)
f.xml_device_name = ProtoField.string("dhs_cerebrum_nb.xml.device_name",  "DEVICE_NAME")
f.xml_sub_device  = ProtoField.string("dhs_cerebrum_nb.xml.sub_device",   "SUB_DEVICE")
f.xml_object      = ProtoField.string("dhs_cerebrum_nb.xml.object",       "OBJECT")
f.xml_value       = ProtoField.string("dhs_cerebrum_nb.xml.value",        "VALUE (top-level)")
f.xml_min         = ProtoField.string("dhs_cerebrum_nb.xml.min",          "MIN")
f.xml_max         = ProtoField.string("dhs_cerebrum_nb.xml.max",          "MAX")
f.xml_step        = ProtoField.string("dhs_cerebrum_nb.xml.step",         "STEP")
-- Category / salvo identity attrs
f.xml_category    = ProtoField.string("dhs_cerebrum_nb.xml.category",     "CATEGORY")
f.xml_group       = ProtoField.string("dhs_cerebrum_nb.xml.group",        "GROUP")
f.xml_instance    = ProtoField.string("dhs_cerebrum_nb.xml.instance",     "INSTANCE")
-- §5.4.2 DETAILS: positional <DEVICE_N …> sub-device children (wire-actual)
f.xml_sub_count   = ProtoField.uint32("dhs_cerebrum_nb.xml.sub_devices",  "Sub-device children (DEVICE_N)", base.DEC)
f.xml_text        = ProtoField.string("dhs_cerebrum_nb.xml.text",         "XML payload")

-- Close
f.close_code      = ProtoField.uint16("dhs_cerebrum_nb.close.code",      "Close code", base.DEC)
f.close_reason    = ProtoField.string("dhs_cerebrum_nb.close.reason",    "Close reason")

-- Expert
local ef = {
  bad_opcode   = ProtoExpert.new("dhs_cerebrum_nb.bad_opcode",   "Unknown WS opcode",         expert.group.MALFORMED,  expert.severity.ERROR),
  rsv_set      = ProtoExpert.new("dhs_cerebrum_nb.rsv_set",      "Reserved bits set",         expert.group.MALFORMED,  expert.severity.WARN),
  big_control  = ProtoExpert.new("dhs_cerebrum_nb.big_control",  "Control frame >125 bytes",  expert.group.MALFORMED,  expert.severity.ERROR),
  unknown_root = ProtoExpert.new("dhs_cerebrum_nb.unknown_root", "Unknown Cerebrum NB XML root (not in 0v16 catalogue)", expert.group.UNDECODED, expert.severity.WARN),
  spurious_wc  = ProtoExpert.new("dhs_cerebrum_nb.spurious_wc",  "MTID-less WILDCARD_COMPLETE (live NOC deviation — only MTID-carrying ones end a wildcard request)", expert.group.PROTOCOL, expert.severity.NOTE),
}
p_cnb.experts = ef

-- Known XML roots — the full 0v16 catalogue (0v13 baseline + 0v16
-- additions). A decoded root NOT in this set fires ef.unknown_root rather
-- than passing silently. Keys are UPPERCASE (extract_xml_attrs upper-cases
-- the root before lookup); the wire is UPPERCASE canonical, but the lookup
-- is case-folded regardless.
local known_roots = {
  -- §2 top-level commands + replies
  LOGIN = true, LOGIN_REPLY = true, POLL = true, POLL_REPLY = true,
  ACTION = true, SUBSCRIBE = true, OBTAIN = true,
  UNSUBSCRIBE = true, UNSUBSCRIBE_ALL = true,
  -- §1.4 / §6 control + status replies
  ACK = true, NACK = true, BUSY = true,
  CONTINUE = true,          -- 0v16 §1.4 flow-control after BUSY
  WILDCARD_COMPLETE = true, -- 0v16 §1.6 end-of-snapshot
  -- §4 action bodies (when seen as a root in fragments / logs)
  ROUTING = true, CATEGORY = true, SALVO = true, DEVICE = true,
  -- §4.5 device CRUD command + RX RESULT envelope (0v16)
  DEVICE_CONFIGURATION = true,
  -- §5 async change events
  ROUTING_CHANGE = true, CATEGORY_CHANGE = true, SALVO_CHANGE = true,
  DEVICE_CHANGE = true, DATASTORE_CHANGE = true,
}

-------------------------------------------------------------------------------
-- Helpers
-------------------------------------------------------------------------------

local opcode_names = {
  [0x0] = "CONTINUATION",
  [0x1] = "TEXT",
  [0x2] = "BINARY",
  [0x8] = "CLOSE",
  [0x9] = "PING",
  [0xA] = "PONG",
}

-- Pure-arithmetic bit ops so we don't depend on bit32 / bit.
local function bit_and(a, m)
  local r, v = 0, 1
  while a > 0 and m > 0 do
    if (a % 2 == 1) and (m % 2 == 1) then r = r + v end
    a = (a - a % 2) / 2
    m = (m - m % 2) / 2
    v = v * 2
  end
  return r
end

local function bit_xor(a, b)
  local r, v = 0, 1
  while a > 0 or b > 0 do
    if (a % 2) ~= (b % 2) then r = r + v end
    a = (a - a % 2) / 2
    b = (b - b % 2) / 2
    v = v * 2
  end
  return r
end

-- Direction arrow. Client uses an ephemeral high port; server listens
-- on a fixed lower port. So if src_port > dst_port we're going
-- client→server. (Robust without depending on the configured pref
-- matching the actual server port — useful when the user runs against
-- a non-default port like 40008.)
local function direction_arrow(pinfo)
  if pinfo.src_port > pinfo.dst_port then return "→" end
  return "←"
end

-- Case-insensitive attribute matcher over the whole document string. The
-- first occurrence wins, which for a root-level attr is the root element's
-- value (children come later in the byte stream).
local function xml_attr(s, name)
  local ic = ""
  for c in name:gmatch(".") do
    ic = ic .. "[" .. c:lower() .. c:upper() .. "]"
  end
  local v = s:match(ic .. "%s*=%s*\"([^\"]*)\"")
  if not v then v = s:match(ic .. "%s*=%s*'([^']*)'") end
  return v
end

-- XML lightweight extraction. Returns a table of the attrs the Info column
-- cares about, including the 0v16 DEVICE_TYPE and the nested <RESULT
-- VALUE=…> verdict. Case-insensitive on attribute keys; values preserved.
local function extract_xml_attrs(s)
  local root = s:match("<%s*([%w_]+)")
  if not root then return nil end
  root = root:upper()

  local r = {
    root        = root,
    mtid        = xml_attr(s, "MTID"),
    typ         = xml_attr(s, "TYPE"),
    err         = xml_attr(s, "ERROR"),
    err_code    = xml_attr(s, "ERROR_CODE"),
    device_type = xml_attr(s, "DEVICE_TYPE"), -- 0v16 §4.5 DEVICE_CONFIGURATION
    -- Routing identity (first occurrence = root/first row attrs; ROUTE
    -- children's SOURCE_ID is a different attribute name, so no clash)
    dest_id     = xml_attr(s, "DEST_ID"),
    srce_id     = xml_attr(s, "SRCE_ID"),
    level_id    = xml_attr(s, "LEVEL_ID"),
    mnemonic    = xml_attr(s, "MNEMONIC"),
    -- Lock (wire-actual): LOCK on actions; LOCK_STATE + LOCKED_BY on
    -- events. LOCK_STATE must be read before LOCK — xml_attr matches the
    -- substring "LOCK" inside "LOCK_STATE" otherwise.
    lock_state  = xml_attr(s, "LOCK_STATE"),
    locked_by   = xml_attr(s, "LOCKED_BY"),
    -- Device-object rows (§5.4)
    device_name = xml_attr(s, "DEVICE_NAME"),
    sub_device  = xml_attr(s, "SUB_DEVICE"),
    object      = xml_attr(s, "OBJECT"),
    min         = xml_attr(s, "MIN"),
    max         = xml_attr(s, "MAX"),
    step        = xml_attr(s, "STEP"),
    -- Category / salvo identity
    category    = xml_attr(s, "CATEGORY"),
    group       = xml_attr(s, "GROUP"),
    instance    = xml_attr(s, "INSTANCE"),
  }

  -- LOCK: whitespace-anchored so only the attribute NAME "LOCK" matches —
  -- LOCK_STATE= / LOCKED_BY= continue with '_' (not '='), and enum text
  -- like TYPE="DEST_LOCK" continues with '"'. TX actions nest the row
  -- under <ACTION>, so the match runs over the whole document.
  r.lock = s:match("[%s][Ll][Oo][Cc][Kk]%s*=%s*\"([^\"]*)\"")
      or s:match("[%s][Ll][Oo][Cc][Kk]%s*=%s*'([^']*)'")

  -- VALUE: whitespace-anchored, document-wide — covers the wire-actual
  -- top-level VALUE on DEVICE_CHANGE value events (no OBJECT_VALUE child
  -- there), the OBJECT_VALUE child's VALUE in obtain replies, and the
  -- SET_VALUE action's VALUE under <ACTION>. DEVICE_CONFIGURATION is
  -- excluded — its <RESULT VALUE=…> verdict is surfaced as RESULT instead.
  if root ~= "DEVICE_CONFIGURATION" then
    r.value = s:match("[%s][Vv][Aa][Ll][Uu][Ee]%s*=%s*\"([^\"]*)\"")
        or s:match("[%s][Vv][Aa][Ll][Uu][Ee]%s*=%s*'([^']*)'")
  end

  -- §5.4.2 DETAILS wire-actual: positional <DEVICE_1 …> <DEVICE_2 …>
  -- sub-device children. Count them for the Info column.
  local n = 0
  for _ in s:gmatch("<%s*[Dd][Ee][Vv][Ii][Cc][Ee]_%d+[%s/>]") do
    n = n + 1
  end
  if n > 0 then r.sub_count = n end

  -- 0v16 §4.5 RX: <DEVICE_CONFIGURATION …><RESULT VALUE="ACCEPTED|FAILED"/>.
  -- Pull the RESULT child's VALUE so the verdict shows in Info. Scope the
  -- match to a <RESULT …> element so a top-level VALUE attr is not picked
  -- up by mistake.
  local result_el = s:match("[<]%s*[Rr][Ee][Ss][Uu][Ll][Tt][^>]*>?")
  if result_el then
    r.result = xml_attr(result_el, "VALUE")
  end

  return r
end

-------------------------------------------------------------------------------
-- WebSocket frame dissector
-------------------------------------------------------------------------------

-- Returns (consumed_bytes) on success, (0, true) when needs more bytes.
-- We do not call desegment APIs — relying on Wireshark's default TCP
-- reassembly preference (Edit → Preferences → Protocols → TCP → "Allow
-- subdissector to reassemble TCP streams"). When that's on, Wireshark
-- delivers the reassembled buffer to us; when off, we just decode
-- whatever fits in one segment and label partial frames.
local function dissect_ws_frame(buffer, pinfo, tree, offset)
  local available = buffer:len() - offset
  if available < 2 then return 0, true end

  local b0 = buffer(offset, 1):uint()
  local b1 = buffer(offset + 1, 1):uint()

  local fin    = bit_and(b0, 0x80) ~= 0
  local rsv    = bit_and(b0, 0x70)
  local opcode = bit_and(b0, 0x0f)
  local masked = bit_and(b1, 0x80) ~= 0
  local len7   = bit_and(b1, 0x7f)

  local hdr_len = 2
  local plen = len7
  if len7 == 126 then
    if available < hdr_len + 2 then return 0, true end
    plen = buffer(offset + hdr_len, 2):uint()
    hdr_len = hdr_len + 2
  elseif len7 == 127 then
    if available < hdr_len + 8 then return 0, true end
    plen = buffer(offset + hdr_len, 8):uint64():tonumber()
    hdr_len = hdr_len + 8
  end

  local mask_offset = offset + hdr_len
  if masked then hdr_len = hdr_len + 4 end

  local total_needed = hdr_len + plen
  if available < total_needed then return 0, true end

  local pos = offset + hdr_len
  local subtree = tree:add(p_cnb, buffer(offset, total_needed), "WebSocket Frame")

  subtree:add(f.ws_fin,    buffer(offset, 1), fin)
  if rsv ~= 0 then
    subtree:add(f.ws_rsv, buffer(offset, 1), rsv):add_proto_expert_info(ef.rsv_set)
  end
  subtree:add(f.ws_opcode,      buffer(offset, 1), opcode)
  subtree:add(f.ws_opcode_name, buffer(offset, 1), opcode_names[opcode] or "UNKNOWN")
  subtree:add(f.ws_masked,      buffer(offset + 1, 1), masked)
  subtree:add(f.ws_len7,        buffer(offset + 1, 1), len7)
  if len7 == 126 then
    subtree:add(f.ws_len_ext16, buffer(offset + 2, 2), plen)
  elseif len7 == 127 then
    subtree:add(f.ws_len_ext64, buffer(offset + 2, 8), buffer(offset + 2, 8):uint64())
  end
  if not opcode_names[opcode] then
    subtree:add_proto_expert_info(ef.bad_opcode)
  end
  if opcode >= 0x8 and plen > 125 then
    subtree:add_proto_expert_info(ef.big_control)
  end
  if masked then
    subtree:add(f.ws_mask_key, buffer(mask_offset, 4))
  end

  -- Payload into a Lua string for display + parsing. Server frames are
  -- UNMASKED (RFC 6455 §5.1) and carry the big snapshot documents — take
  -- them in ONE TvbRange:raw() call; a per-byte Lua loop here made live
  -- decoding of production event streams visibly lag. Only client frames
  -- are masked, and those are small (commands), so the per-byte unmask
  -- loop stays for them alone.
  local payload_str = ""
  if plen > 0 then
    if masked then
      local raw = buffer(pos, plen):bytes()
      local mk = {
        buffer(mask_offset,     1):uint(),
        buffer(mask_offset + 1, 1):uint(),
        buffer(mask_offset + 2, 1):uint(),
        buffer(mask_offset + 3, 1):uint(),
      }
      local payload_chars = {}
      for i = 0, plen - 1 do
        payload_chars[i + 1] = string.char(bit_xor(raw:get_index(i), mk[(i % 4) + 1]))
      end
      payload_str = table.concat(payload_chars)
    else
      payload_str = buffer(pos, plen):raw()
    end
  end

  local arrow = direction_arrow(pinfo)
  local op_name = opcode_names[opcode] or string.format("op=0x%x", opcode)

  if opcode == 0x1 and plen > 0 then
    -- Text frame — XML extraction.
    local x = extract_xml_attrs(payload_str)
    subtree:add(f.xml_text, payload_str)
    if x then
      local root_item = subtree:add(f.xml_root, x.root)
      local info = string.format("%s %s", arrow, x.root)
      if x.mtid        then subtree:add(f.xml_mtid,        x.mtid);        info = info .. " mtid=" .. x.mtid end
      if x.typ         then subtree:add(f.xml_type,        x.typ);         info = info .. " TYPE=" .. x.typ end
      if x.device_type then subtree:add(f.xml_device_type, x.device_type); info = info .. " DEVICE_TYPE=" .. x.device_type end
      -- Routing identity (the "which cell" half of every routing frame)
      if x.dest_id     then subtree:add(f.xml_dest_id,     x.dest_id);     info = info .. " DEST=" .. x.dest_id end
      if x.srce_id     then subtree:add(f.xml_srce_id,     x.srce_id);     info = info .. " SRCE=" .. x.srce_id end
      if x.level_id    then subtree:add(f.xml_level_id,    x.level_id);    info = info .. " LVL=" .. x.level_id end
      if x.mnemonic    then subtree:add(f.xml_mnemonic,    x.mnemonic);    info = info .. " MNE=" .. string.format("%q", x.mnemonic) end
      -- Lock lifecycle (LOCK on actions; LOCK_STATE/LOCKED_BY on events —
      -- RELEASED is the wire-actual clearing value, keys.md wire-actual)
      if x.lock        then subtree:add(f.xml_lock,        x.lock);        info = info .. " LOCK=" .. x.lock end
      if x.lock_state  then subtree:add(f.xml_lock_state,  x.lock_state);  info = info .. " LOCK_STATE=" .. x.lock_state end
      if x.locked_by   then subtree:add(f.xml_locked_by,   x.locked_by);   info = info .. " BY=" .. string.format("%q", x.locked_by) end
      -- Device-object rows (§5.4): identity + top-level VALUE + descriptor
      if x.device_name then subtree:add(f.xml_device_name, x.device_name); info = info .. " DEV=" .. string.format("%q", x.device_name) end
      if x.sub_device  then subtree:add(f.xml_sub_device,  x.sub_device);  info = info .. " SUB=" .. x.sub_device end
      if x.object      then subtree:add(f.xml_object,      x.object);      info = info .. " OBJ=" .. string.format("%q", x.object) end
      if x.value       then subtree:add(f.xml_value,       x.value);       info = info .. " VALUE=" .. string.format("%q", x.value) end
      if x.min         then subtree:add(f.xml_min,  x.min)  end
      if x.max         then subtree:add(f.xml_max,  x.max)  end
      if x.step        then subtree:add(f.xml_step, x.step) end
      if x.min and x.max then info = info .. string.format(" range=%s..%s", x.min, x.max) end
      if x.sub_count   then subtree:add(f.xml_sub_count,   x.sub_count);   info = info .. " sub_devices=" .. x.sub_count end
      -- Category / salvo identity
      if x.category    then subtree:add(f.xml_category,    x.category);    info = info .. " CAT=" .. string.format("%q", x.category) end
      if x.group       then subtree:add(f.xml_group,       x.group);       info = info .. " GRP=" .. string.format("%q", x.group) end
      if x.instance    then subtree:add(f.xml_instance,    x.instance);    info = info .. " INST=" .. string.format("%q", x.instance) end
      if x.result      then subtree:add(f.xml_result,      x.result);      info = info .. " RESULT=" .. x.result end
      if x.err         then subtree:add(f.xml_error,       x.err);         info = info .. " ERROR=" .. x.err end
      if x.err_code    then subtree:add(f.xml_error_code,  x.err_code);    info = info .. " CODE=" .. x.err_code end
      -- Live NOC deviation: a spurious MTID-less WILDCARD_COMPLETE follows
      -- every event; only MTID-carrying ones end a wildcard request.
      if x.root == "WILDCARD_COMPLETE" and not x.mtid then
        root_item:add_proto_expert_info(ef.spurious_wc)
        info = info .. " [spurious — no mtid]"
      end
      -- Unknown root → expert WARNING (never a silent fallthrough).
      if not known_roots[x.root] then
        root_item:add_proto_expert_info(ef.unknown_root)
        info = info .. " [unknown root]"
      end
      pinfo.cols.info:set(info)
    else
      pinfo.cols.info:set(string.format("%s TEXT (no XML root) %d bytes", arrow, plen))
    end
  elseif opcode == 0x8 then
if plen >= 2 then
      local code = string.byte(payload_str, 1) * 256 + string.byte(payload_str, 2)
      subtree:add(f.close_code, buffer(pos, 2), code)
      local reason = ""
      if plen > 2 then
        reason = payload_str:sub(3)
        subtree:add(f.close_reason, buffer(pos + 2, plen - 2), reason)
        pinfo.cols.info:set(string.format("%s CLOSE code=%d reason=%q", arrow, code, reason))
      else
        pinfo.cols.info:set(string.format("%s CLOSE code=%d", arrow, code))
      end
    else
      pinfo.cols.info:set(string.format("%s CLOSE", arrow))
    end
  elseif opcode == 0x9 then
pinfo.cols.info:set(arrow .. " PING")
  elseif opcode == 0xA then
pinfo.cols.info:set(arrow .. " PONG")
  else
pinfo.cols.info:set(string.format("%s %s len=%d", arrow, op_name, plen))
  end

  return total_needed, false
end

-------------------------------------------------------------------------------
-- Handshake dissector
-------------------------------------------------------------------------------

local function looks_like_handshake(prefix)
  return prefix:sub(1, 4) == "GET " or prefix:sub(1, 9) == "HTTP/1.1 "
end

local function dissect_handshake(buffer, pinfo, tree)
  local s = buffer():string()
  local hdr_end = s:find("\r\n\r\n", 1, true)
  if not hdr_end then return 0 end

  local first_line = s:match("^([^\r\n]+)") or "?"
  local subtree = tree:add(p_cnb, buffer(0, hdr_end + 3), "WebSocket Handshake")
  subtree:add(f.phase, "handshake")

  local arrow = direction_arrow(pinfo)
  if first_line:sub(1, 4) == "GET " then
    subtree:add(f.hs_request, first_line)
    pinfo.cols.info:set(string.format("%s WS upgrade request — %s", arrow, first_line))
  else
    subtree:add(f.hs_response, first_line)
    pinfo.cols.info:set(string.format("%s WS upgrade reply — %s", arrow, first_line))
  end
  return hdr_end + 3
end

-------------------------------------------------------------------------------
-- Top-level dissect
-------------------------------------------------------------------------------

function p_cnb.dissector(buffer, pinfo, tree)
  local len = buffer:len()
  if len < 2 then
    pinfo.desegment_offset = 0
    pinfo.desegment_len = 2 - len
    return
  end

  -- Always claim the protocol column up-front, regardless of phase
  -- (Probel / ACP convention — phase / verb belongs in Info, not Protocol).
  pinfo.cols.protocol:set("Cerebrum-NB")

  local prefix = buffer(0, math.min(9, len)):string()
  if looks_like_handshake(prefix) then
    return dissect_handshake(buffer, pinfo, tree)
  end

  -- WS frame mode. Decode as many full frames as fit in this buffer.
  -- If the last one is partial, request reassembly via desegment_*.
  local offset = 0
  while offset < len do
    local consumed, need_more = dissect_ws_frame(buffer, pinfo, tree, offset)
    if need_more then
      pinfo.desegment_offset = offset
      pinfo.desegment_len = 1 -- "give me more bytes; I'll figure out exact need next call"
      return offset > 0 and offset or nil
    end
    if consumed == 0 then break end
    offset = offset + consumed
  end
  return offset
end

-------------------------------------------------------------------------------
-- Registration
-------------------------------------------------------------------------------

local function register_ports()
  local tcp = DissectorTable.get("tcp.port")
  tcp:add(p_cnb.prefs.tcp_port, p_cnb)
end

register_ports()

function p_cnb.prefs_changed()
  local tcp = DissectorTable.get("tcp.port")
  tcp:add(p_cnb.prefs.tcp_port, p_cnb)
end
