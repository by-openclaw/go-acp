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
--     MTID/TYPE/ERROR/ERROR_CODE attributes surfaced in the Info column
--   * Every command + event root from EVS Cerebrum NB v0.13:
--       LOGIN/LOGIN_REPLY, POLL/POLL_REPLY, ACTION, SUBSCRIBE, OBTAIN,
--       UNSUBSCRIBE, UNSUBSCRIBE_ALL, ACK, NACK, BUSY,
--       ROUTING, CATEGORY, SALVO, DEVICE,
--       ROUTING_CHANGE, CATEGORY_CHANGE, SALVO_CHANGE,
--       DEVICE_CHANGE, DATASTORE_CHANGE
--   * Per-message Info column with direction arrow + verb + key attrs
--     so each frame is uniquely identifiable at a glance
--
-- Refs:
--   RFC 6455 (WebSocket):  https://www.rfc-editor.org/rfc/rfc6455
--   EVS Cerebrum NB 0.13:  internal/cerebrum-nb/docs/keys.md
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
f.xml_root        = ProtoField.string("dhs_cerebrum_nb.xml.root",        "XML root")
f.xml_mtid        = ProtoField.string("dhs_cerebrum_nb.xml.mtid",        "MTID")
f.xml_type        = ProtoField.string("dhs_cerebrum_nb.xml.type",        "TYPE")
f.xml_error       = ProtoField.string("dhs_cerebrum_nb.xml.error",       "ERROR")
f.xml_error_code  = ProtoField.string("dhs_cerebrum_nb.xml.error_code",  "ERROR_CODE")
f.xml_text        = ProtoField.string("dhs_cerebrum_nb.xml.text",        "XML payload")

-- Close
f.close_code      = ProtoField.uint16("dhs_cerebrum_nb.close.code",      "Close code", base.DEC)
f.close_reason    = ProtoField.string("dhs_cerebrum_nb.close.reason",    "Close reason")

-- Expert
local ef = {
  bad_opcode  = ProtoExpert.new("dhs_cerebrum_nb.bad_opcode",  "Unknown WS opcode",         expert.group.MALFORMED, expert.severity.ERROR),
  rsv_set     = ProtoExpert.new("dhs_cerebrum_nb.rsv_set",     "Reserved bits set",         expert.group.MALFORMED, expert.severity.WARN),
  big_control = ProtoExpert.new("dhs_cerebrum_nb.big_control", "Control frame >125 bytes",  expert.group.MALFORMED, expert.severity.ERROR),
}
p_cnb.experts = ef

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

-- XML lightweight extraction. Returns root, mtid, type, error, error_code.
-- Case-insensitive on attribute keys; values preserved.
local function extract_xml_attrs(s)
  local root = s:match("<%s*([%w_]+)")
  if not root then return nil end
  root = root:upper()

  local function attr(name)
    -- Build a case-insensitive matcher by char-class.
    local ic = ""
    for c in name:gmatch(".") do
      ic = ic .. "[" .. c:lower() .. c:upper() .. "]"
    end
    local v = s:match(ic .. "%s*=%s*\"([^\"]*)\"")
    if not v then v = s:match(ic .. "%s*=%s*'([^']*)'") end
    return v
  end

  return root, attr("MTID"), attr("TYPE"), attr("ERROR"), attr("ERROR_CODE")
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

  -- Unmask payload into a Lua string for display + parsing.
  local payload_chars = {}
  if plen > 0 then
    local raw = buffer(pos, plen):bytes()
    if masked then
      local mk = {
        buffer(mask_offset,     1):uint(),
        buffer(mask_offset + 1, 1):uint(),
        buffer(mask_offset + 2, 1):uint(),
        buffer(mask_offset + 3, 1):uint(),
      }
      for i = 0, plen - 1 do
        payload_chars[i + 1] = string.char(bit_xor(raw:get_index(i), mk[(i % 4) + 1]))
      end
    else
      for i = 0, plen - 1 do
        payload_chars[i + 1] = string.char(raw:get_index(i))
      end
    end
  end
  local payload_str = table.concat(payload_chars)

  local arrow = direction_arrow(pinfo)
  local op_name = opcode_names[opcode] or string.format("op=0x%x", opcode)

  if opcode == 0x1 and plen > 0 then
    -- Text frame — XML extraction.
    local root, mtid, typ, err, err_code = extract_xml_attrs(payload_str)
    subtree:add(f.xml_text, payload_str)
    if root then
      subtree:add(f.xml_root, root)
      pinfo.cols.protocol:set("Cerebrum-NB")
      local info = string.format("%s %s", arrow, root)
      if mtid     then subtree:add(f.xml_mtid,       mtid);     info = info .. " mtid=" .. mtid end
      if typ      then subtree:add(f.xml_type,       typ);      info = info .. " TYPE=" .. typ end
      if err      then subtree:add(f.xml_error,      err);      info = info .. " ERROR=" .. err end
      if err_code then subtree:add(f.xml_error_code, err_code); info = info .. " CODE=" .. err_code end
      pinfo.cols.info:set(info)
    else
      pinfo.cols.protocol:set("Cerebrum-NB")
      pinfo.cols.info:set(string.format("%s TEXT (no XML root) %d bytes", arrow, plen))
    end
  elseif opcode == 0x8 then
    pinfo.cols.protocol:set("Cerebrum-NB")
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
    pinfo.cols.protocol:set("Cerebrum-NB")
    pinfo.cols.info:set(arrow .. " PING")
  elseif opcode == 0xA then
    pinfo.cols.protocol:set("Cerebrum-NB")
    pinfo.cols.info:set(arrow .. " PONG")
  else
    pinfo.cols.protocol:set("Cerebrum-NB")
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

  pinfo.cols.protocol:set("Cerebrum-NB Upgrade")
  local arrow = direction_arrow(pinfo)
  if first_line:sub(1, 4) == "GET " then
    subtree:add(f.hs_request, first_line)
    pinfo.cols.info:set(arrow .. " " .. first_line)
  else
    subtree:add(f.hs_response, first_line)
    pinfo.cols.info:set(arrow .. " " .. first_line)
  end
  return hdr_end + 3
end

-------------------------------------------------------------------------------
-- Top-level dissect
-------------------------------------------------------------------------------

function p_cnb.dissector(buffer, pinfo, tree)
  local len = buffer:len()
  if len < 2 then return 0 end

  local prefix = buffer(0, math.min(9, len)):string()
  if looks_like_handshake(prefix) then
    return dissect_handshake(buffer, pinfo, tree)
  end

  -- WS frame mode. Loop through any number of full frames in this segment.
  local offset = 0
  while offset < len do
    local consumed, need_more = dissect_ws_frame(buffer, pinfo, tree, offset)
    if need_more then break end
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
