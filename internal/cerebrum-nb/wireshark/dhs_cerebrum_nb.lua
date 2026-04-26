-------------------------------------------------------------------------------
--
-- Wireshark Lua Dissector: EVS Cerebrum Northbound API (Neuron Bridge)
--
-- Decodes XML-over-WebSocket on TCP port 40007 (configurable).
--
-- Coverage:
--   * HTTP/1.1 handshake (GET / + 101 Switching Protocols)
--   * RFC 6455 frames: FIN/RSV/opcode/MASK/payload-len(7/16/64)/key/payload
--   * Text frames carrying one Cerebrum NB XML document — root element
--     name, mtid, type/TYPE attribute surfaced in the Info column
--   * Every command + event root from EVS Cerebrum NB v0.13:
--       login, login_reply, poll, poll_reply, action, subscribe,
--       obtain, unsubscribe, unsubscribe_all, ack, nack, busy,
--       routing, category, salvo, device, routing_change,
--       category_change, salvo_change, device_change, datastore_change
--   * Per-message Info column: "<root> mtid=N TYPE=X" so frames are
--     uniquely identifiable at a glance
--
-- Pure-arithmetic bit ops; no bit32 / bit dependency. Never delegates
-- to Wireshark's built-in WebSocket dissector.
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

-- Stream classification
f.stream_kind     = ProtoField.string("dhs_cerebrum_nb.stream", "Stream phase")

-- Handshake
f.hs_request      = ProtoField.string("dhs_cerebrum_nb.handshake.request", "HTTP request")
f.hs_response     = ProtoField.string("dhs_cerebrum_nb.handshake.response", "HTTP response")

-- WebSocket frame
f.ws_fin          = ProtoField.bool  ("dhs_cerebrum_nb.ws.fin",     "FIN")
f.ws_rsv          = ProtoField.uint8 ("dhs_cerebrum_nb.ws.rsv",     "RSV1..3", base.HEX, nil, 0x70)
f.ws_opcode       = ProtoField.uint8 ("dhs_cerebrum_nb.ws.opcode",  "Opcode",  base.HEX)
f.ws_opcode_name  = ProtoField.string("dhs_cerebrum_nb.ws.opcode_name", "Opcode name")
f.ws_masked       = ProtoField.bool  ("dhs_cerebrum_nb.ws.masked",  "MASK")
f.ws_len7         = ProtoField.uint8 ("dhs_cerebrum_nb.ws.len7",    "Payload len (7-bit field)", base.DEC)
f.ws_len_ext16    = ProtoField.uint16("dhs_cerebrum_nb.ws.len16",   "Payload len (16-bit ext)",  base.DEC)
f.ws_len_ext64    = ProtoField.uint64("dhs_cerebrum_nb.ws.len64",   "Payload len (64-bit ext)",  base.DEC)
f.ws_mask_key     = ProtoField.bytes ("dhs_cerebrum_nb.ws.mask_key", "Masking key", base.NONE)
f.ws_payload      = ProtoField.bytes ("dhs_cerebrum_nb.ws.payload",  "Payload (post-unmask)", base.NONE)

-- XML
f.xml_root        = ProtoField.string("dhs_cerebrum_nb.xml.root",   "XML root element")
f.xml_mtid        = ProtoField.string("dhs_cerebrum_nb.xml.mtid",   "mtid")
f.xml_type        = ProtoField.string("dhs_cerebrum_nb.xml.type",   "type / TYPE")
f.xml_text        = ProtoField.string("dhs_cerebrum_nb.xml.text",   "XML payload")

-- Expert
local ef = {
  bad_opcode  = ProtoExpert.new("dhs_cerebrum_nb.bad_opcode",  "Unknown WS opcode",       expert.group.MALFORMED, expert.severity.ERROR),
  rsv_set     = ProtoExpert.new("dhs_cerebrum_nb.rsv_set",     "Reserved bits set",       expert.group.MALFORMED, expert.severity.WARN),
  big_control = ProtoExpert.new("dhs_cerebrum_nb.big_control", "Control frame >125 bytes", expert.group.MALFORMED, expert.severity.ERROR),
}
p_cnb.experts = ef

-------------------------------------------------------------------------------
-- Helpers
-------------------------------------------------------------------------------

local opcodes = {
  [0x0] = "continuation",
  [0x1] = "text",
  [0x2] = "binary",
  [0x8] = "close",
  [0x9] = "ping",
  [0xA] = "pong",
}

local function band(a, b) return (a - a % b) / b * b end -- not used; kept for clarity

local function bit_and(a, m)
  -- arithmetic AND for byte-wide masks. m must be a power of two combination.
  local r = 0
  local v = 1
  while a > 0 and m > 0 do
    if (a % 2 == 1) and (m % 2 == 1) then
      r = r + v
    end
    a = (a - a % 2) / 2
    m = (m - m % 2) / 2
    v = v * 2
  end
  return r
end

local function bit_xor(a, b)
  local r = 0
  local v = 1
  while a > 0 or b > 0 do
    if (a % 2) ~= (b % 2) then
      r = r + v
    end
    a = (a - a % 2) / 2
    b = (b - b % 2) / 2
    v = v * 2
  end
  return r
end

-- Per-conversation state: are we past the handshake?
local conv_state = {}

local function conv_key(pinfo)
  return tostring(pinfo.src) .. ":" .. tostring(pinfo.src_port) .. "->" ..
         tostring(pinfo.dst) .. ":" .. tostring(pinfo.dst_port)
end

local function rev_key(pinfo)
  return tostring(pinfo.dst) .. ":" .. tostring(pinfo.dst_port) .. "->" ..
         tostring(pinfo.src) .. ":" .. tostring(pinfo.src_port)
end

-------------------------------------------------------------------------------
-- XML lightweight extraction
-------------------------------------------------------------------------------

local function extract_xml_attrs(s)
  -- Returns root, mtid, type. Case-folded comparisons, original values
  -- preserved.
  local root = s:match("<%s*([%w_]+)")
  if not root then return nil, nil, nil end
  local mtid = s:match("[Mm][Tt][Ii][Dd]%s*=%s*\"([^\"]*)\"")
  if not mtid then mtid = s:match("[Mm][Tt][Ii][Dd]%s*=%s*'([^']*)'") end
  local typ = s:match("[Tt][Yy][Pp][Ee]%s*=%s*\"([^\"]*)\"")
  if not typ then typ = s:match("[Tt][Yy][Pp][Ee]%s*=%s*'([^']*)'") end
  return root:lower(), mtid, typ
end

-------------------------------------------------------------------------------
-- WebSocket frame dissector
-------------------------------------------------------------------------------

local function dissect_ws_frame(buffer, pinfo, tree, offset)
  local available = buffer:len() - offset
  if available < 2 then return nil end

  local b0 = buffer(offset, 1):uint()
  local b1 = buffer(offset + 1, 1):uint()

  local fin    = bit_and(b0, 0x80) ~= 0
  local rsv    = bit_and(b0, 0x70)
  local opcode = bit_and(b0, 0x0f)
  local masked = bit_and(b1, 0x80) ~= 0
  local len7   = bit_and(b1, 0x7f)

  local pos = offset + 2
  local plen = len7
  if len7 == 126 then
    if buffer:len() - pos < 2 then return nil end
    plen = buffer(pos, 2):uint()
    pos = pos + 2
  elseif len7 == 127 then
    if buffer:len() - pos < 8 then return nil end
    plen = buffer(pos, 8):uint64():tonumber()
    pos = pos + 8
  end

  local mask_offset = pos
  if masked then
    if buffer:len() - pos < 4 then return nil end
    pos = pos + 4
  end

  if buffer:len() - pos < plen then
    -- need more bytes; ask Wireshark to reassemble
    pinfo.desegment_offset = offset
    pinfo.desegment_len = plen - (buffer:len() - pos)
    return nil
  end

  local frame_total = pos + plen - offset
  local subtree = tree:add(p_cnb, buffer(offset, frame_total), "WebSocket Frame")

  subtree:add(f.ws_fin,    buffer(offset, 1), fin)
  if rsv ~= 0 then
    subtree:add(f.ws_rsv, buffer(offset, 1), rsv):add_proto_expert_info(ef.rsv_set)
  else
    subtree:add(f.ws_rsv, buffer(offset, 1), rsv)
  end
  subtree:add(f.ws_opcode, buffer(offset, 1), opcode)
  subtree:add(f.ws_opcode_name, buffer(offset, 1), opcodes[opcode] or "unknown")
  subtree:add(f.ws_masked, buffer(offset + 1, 1), masked)
  subtree:add(f.ws_len7,   buffer(offset + 1, 1), len7)
  if len7 == 126 then
    subtree:add(f.ws_len_ext16, buffer(offset + 2, 2), plen)
  elseif len7 == 127 then
    subtree:add(f.ws_len_ext64, buffer(offset + 2, 8), buffer(offset + 2, 8):uint64())
  end

  if not opcodes[opcode] then
    subtree:add_proto_expert_info(ef.bad_opcode)
  end
  if opcode >= 0x8 and plen > 125 then
    subtree:add_proto_expert_info(ef.big_control)
  end

  if masked then
    subtree:add(f.ws_mask_key, buffer(mask_offset, 4))
  end

  -- Unmask payload into a Lua string (for display + XML extraction).
  local payload_bytes = {}
  if plen > 0 then
    local raw = buffer(pos, plen):bytes()
    if masked then
      local mk = { buffer(mask_offset, 1):uint(),
                   buffer(mask_offset + 1, 1):uint(),
                   buffer(mask_offset + 2, 1):uint(),
                   buffer(mask_offset + 3, 1):uint() }
      for i = 0, plen - 1 do
        payload_bytes[#payload_bytes + 1] = string.char(bit_xor(raw:get_index(i), mk[(i % 4) + 1]))
      end
    else
      for i = 0, plen - 1 do
        payload_bytes[#payload_bytes + 1] = string.char(raw:get_index(i))
      end
    end
  end
  local payload_str = table.concat(payload_bytes)
  if plen > 0 then
    subtree:add(f.ws_payload, buffer(pos, plen)):set_text(string.format("Payload — %d bytes", plen))
  end

  -- Info column construction.
  local info_parts = { opcodes[opcode] or string.format("op=0x%x", opcode) }

  if opcode == 0x1 and plen > 0 then
    -- Text frame — XML extraction
    local root, mtid, typ = extract_xml_attrs(payload_str)
    if root then
      subtree:add(f.xml_root, root)
      info_parts[#info_parts + 1] = "<" .. root .. ">"
      if mtid then
        subtree:add(f.xml_mtid, mtid)
        info_parts[#info_parts + 1] = "mtid=" .. mtid
      end
      if typ then
        subtree:add(f.xml_type, typ)
        info_parts[#info_parts + 1] = "TYPE=" .. typ
      end
    end
    subtree:add(f.xml_text, payload_str)
  elseif opcode == 0x8 and plen >= 2 then
    local code = string.byte(payload_str, 1) * 256 + string.byte(payload_str, 2)
    info_parts[#info_parts + 1] = string.format("code=%d", code)
  end

  pinfo.cols.info:append(" | " .. table.concat(info_parts, " "))

  return frame_total
end

-------------------------------------------------------------------------------
-- Handshake dissector
-------------------------------------------------------------------------------

local function looks_like_handshake(s)
  return s:sub(1, 4) == "GET " or s:sub(1, 9) == "HTTP/1.1 "
end

local function dissect_handshake(buffer, pinfo, tree)
  local s = buffer():string()
  -- Find end of headers (\r\n\r\n)
  local hdr_end = s:find("\r\n\r\n", 1, true)
  if not hdr_end then
    -- Need more bytes
    pinfo.desegment_offset = 0
    pinfo.desegment_len = -1 -- DESEGMENT_ONE_MORE_SEGMENT
    return nil
  end
  local hdr = s:sub(1, hdr_end + 1)
  local subtree = tree:add(p_cnb, buffer(0, hdr_end + 3), "WebSocket Handshake")

  local first_line = hdr:match("^([^\r\n]+)")
  if first_line and first_line:sub(1, 4) == "GET " then
    subtree:add(f.hs_request, first_line)
    subtree:add(f.stream_kind, "handshake (client→server)")
    pinfo.cols.info:append(" | WS upgrade request")
  elseif first_line and first_line:sub(1, 9) == "HTTP/1.1 " then
    subtree:add(f.hs_response, first_line)
    subtree:add(f.stream_kind, "handshake (server→client)")
    pinfo.cols.info:append(" | " .. first_line)
    if first_line:find("101", 10, true) then
      conv_state[conv_key(pinfo)]   = "frames"
      conv_state[rev_key(pinfo)]    = "frames"
    end
  end
  return hdr_end + 3
end

-------------------------------------------------------------------------------
-- Top-level dissect
-------------------------------------------------------------------------------

function p_cnb.dissector(buffer, pinfo, tree)
  local len = buffer:len()
  if len < 2 then return 0 end

  pinfo.cols.protocol = "Cerebrum-NB"
  pinfo.cols.info = "" -- start clean; dissectors append

  -- Handshake detection: look at first few bytes.
  local first4 = buffer(0, math.min(4, len)):string()
  if looks_like_handshake(buffer():string()) then
    -- Mark state if we don't have it yet.
    local k = conv_key(pinfo)
    if not conv_state[k] then conv_state[k] = "handshake" end
    local consumed = dissect_handshake(buffer, pinfo, tree)
    return consumed or len
  end

  -- WS frame mode.
  local offset = 0
  while offset < len do
    local consumed = dissect_ws_frame(buffer, pinfo, tree, offset)
    if not consumed then break end
    offset = offset + consumed
  end
  return offset
end

-------------------------------------------------------------------------------
-- Registration
-------------------------------------------------------------------------------

local function register_ports()
  local tcp_port_table = DissectorTable.get("tcp.port")
  tcp_port_table:add(p_cnb.prefs.tcp_port, p_cnb)
end

register_ports()

-- Re-register on prefs change.
function p_cnb.prefs_changed()
  local tcp_port_table = DissectorTable.get("tcp.port")
  -- Wireshark Lua API doesn't expose remove(); the user reload picks up new port.
  tcp_port_table:add(p_cnb.prefs.tcp_port, p_cnb)
end
