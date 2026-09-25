-- dhs_mnset.lua — Riedel MuoN eMSFP / FusioN node REST (emsfp/node/v1)
--
-- The wire is HTTP/1.1 carrying JSON (and SDP as text), and this
-- dissector decodes it ITSELF rather than layering on Wireshark's http
-- decoder. That is the repo rule (root CLAUDE.md, "Wireshark
-- dissectors"): a protocol dhs ships a dissector for is decoded by
-- ours, so the Protocol | Info shape matches every other dhs
-- connector. A post-dissector cannot do it — what it writes to the
-- Info column is APPENDED to what http already wrote, so every frame
-- reads twice; that was measured against a real capture, not assumed.
--
-- It claims a TCP stream only when the first request is one of ours:
--
--   /emsfp/node/v1/…                     the module, direct
--   /rest/<array>/<i>/emSFP/node/v1/…    through MN SET's NBAPI
--   /api/…                               MN SET's own app API
--
-- Anything else on 80 stays with Wireshark's http, which is what an
-- operator wants in a mixed capture.
--
-- What the Info column carries (ADR-0025 #5 — the discriminating
-- arguments, not just the verb):
--
--   GET port/1                    → 200 port/1  link=up sfp=GSS-MPO250-SRC
--   GET refclk                    → 200 refclk  status=3 locked=e1
--   GET flows/6baf596c            → 200 flows/6baf596c  dst=239.6.1.9:20000
--   PUT flows/6baf596c/network ← dst=239.1.1.9:20000  (what is being written)
--   GET self/information          → 200 self/information  FusioN6 fw=0x68cd783f sn=125061600012
--   PUT self/ipconfig             → 400 self/ipconfig REFUSED "invalid netmask"
--
-- Filters:
--   dhs_mnset                        every module / MN SET exchange
--   dhs_mnset.resource == "flows"    one resource
--   dhs_mnset.method == "PUT"        writes only
--   dhs_mnset.api == "nbapi"         through MN SET's NBAPI
--   dhs_mnset.refused == 1           everything the module said no to

local p = Proto("dhs_mnset", "Riedel MuoN/FusioN REST (dhs)")

local f_api      = ProtoField.string("dhs_mnset.api", "API", base.ASCII)
local f_dir      = ProtoField.string("dhs_mnset.direction", "Direction", base.ASCII)
local f_method   = ProtoField.string("dhs_mnset.method", "Method", base.ASCII)
local f_uri      = ProtoField.string("dhs_mnset.uri", "URI", base.ASCII)
local f_resource = ProtoField.string("dhs_mnset.resource", "Resource", base.ASCII)
local f_instance = ProtoField.string("dhs_mnset.instance", "Instance", base.ASCII)
local f_sub      = ProtoField.string("dhs_mnset.sub", "Sub-resource", base.ASCII)
local f_array    = ProtoField.string("dhs_mnset.array", "NBAPI array/index", base.ASCII)
local f_version  = ProtoField.string("dhs_mnset.version", "HTTP version", base.ASCII)
local f_status   = ProtoField.uint16("dhs_mnset.status", "Status", base.DEC)
local f_reason   = ProtoField.string("dhs_mnset.reason", "Reason", base.ASCII)
local f_len      = ProtoField.uint32("dhs_mnset.content_length", "Content-Length", base.DEC)
local f_ctype    = ProtoField.string("dhs_mnset.content_type", "Content-Type", base.ASCII)
local f_header   = ProtoField.string("dhs_mnset.header", "Header", base.ASCII)
local f_body     = ProtoField.string("dhs_mnset.body", "Body", base.ASCII)
local f_summary  = ProtoField.string("dhs_mnset.summary", "What it says", base.ASCII)
local f_refused  = ProtoField.bool("dhs_mnset.refused", "Refused")
local f_devmsg   = ProtoField.string("dhs_mnset.device_message", "Module message", base.ASCII)
p.fields = {
  f_api, f_dir, f_method, f_uri, f_resource, f_instance, f_sub, f_array,
  f_version, f_status, f_reason, f_len, f_ctype, f_header, f_body,
  f_summary, f_refused, f_devmsg,
}

local ef_unknown = ProtoExpert.new("dhs_mnset.unknown_resource",
  "Resource not in the MN SET Rest catalogue", expert.group.PROTOCOL, expert.severity.WARN)
local ef_refused = ProtoExpert.new("dhs_mnset.refused_expert",
  "Module refused the request (non-2xx)", expert.group.RESPONSE_CODE, expert.severity.WARN)
local ef_trunc = ProtoExpert.new("dhs_mnset.truncated",
  "Body shorter than Content-Length — capture cut or stream reset",
  expert.group.MALFORMED, expert.severity.NOTE)
p.experts = { ef_unknown, ef_refused, ef_trunc }

local tcp_stream = Field.new("tcp.stream")

-- The node catalogue as MN SET's Rest page lists it (verified on a
-- FusioN6, fw 0x68cd783f). Anything else on the node path is flagged,
-- not dropped — a new firmware adds resources and the dissector must
-- show them.
local known = {}
for _, r in ipairs({
  "/", "self", "port", "flows", "sources", "receivers", "senders", "route",
  "devices", "sdi", "sdi_output", "sdi_input", "sdi_audio", "sdp",
  "receivers_sdp", "senders_sdp", "clean_switch", "refclk", "lldp", "telemetry",
  "self/information", "self/diag", "self/firmware", "self/phy", "self/interfaces",
  "self/ipconfig", "self/static_route", "self/license", "self/system",
  "self/syslog", "self/protocols",
  "diag", "diag/common", "diag/firmware", "diag/dns", "diag/flow",
  "diag/packet_interval_time", "diag/devices", "diag/2110-7_engine",
  "diag/refclk", "diag/nmos",
}) do known[r] = true end

-- classify returns api, resource-path, array for a URI, or nil when the
-- URI is not one of ours.
local function classify(uri)
  if not uri then return nil end
  local full = uri:match("^https?://[^/]+(/.*)$")
  if full then uri = full end
  local path = uri:match("^([^?]*)")
  local rest = path:match("^/emsfp/node/v1/?(.*)$")
  if rest then return "node", rest, nil end
  local arr, idx, r2 = path:match("^/rest/([^/]+)/([^/]+)/emSFP/node/v1/?(.*)$")
  if arr then return "nbapi", r2, arr .. "/" .. idx end
  local app = path:match("^/api/(.*)$")
  if app then return "mnset", app, nil end
  return nil
end

-- split takes "flows/6baf596c-…/network" apart into the catalogue key,
-- the instance and the sub-resource: WHICH flow, and which part of it.
local function split(res)
  if res == "" then return "/", nil, nil end
  local head, tail = res:match("^([^/]+)/?(.*)$")
  if (head == "self" or head == "diag") and tail ~= "" then
    local second, rest2 = tail:match("^([^/]+)/?(.*)$")
    local key = head .. "/" .. second
    if rest2 ~= "" then return key, nil, rest2 end
    return key, nil, nil
  end
  if tail == "" then return head, nil, nil end
  local inst, sub = tail:match("^([^/]+)/?(.*)$")
  if sub == "" then sub = nil end
  return head, inst, sub
end

-- shortID keeps a uuid readable in a column; the full value is in the tree.
local function shortID(id)
  if not id then return nil end
  return id:match("^(%x%x%x%x%x%x%x%x)%-") or id
end

-- str/num pull one value out of a JSON document by key. A dissector is
-- not a parser: these read the module's own flat documents, and a key
-- that is not there simply does not appear in the column.
local function str(doc, key)
  if not doc then return nil end
  return doc:match('"' .. key .. '"%s*:%s*"([^"]*)"')
end
local function num(doc, key)
  if not doc then return nil end
  return doc:match('"' .. key .. '"%s*:%s*(-?%d+%.?%d*)')
end

-- describe reads the discriminating values for one resource kind. Two
-- frames that differ only in which port, which flow or which state
-- must not look alike.
local function describe(key, doc)
  if not doc or doc == "" then return "" end
  local t = {}
  local function put(s) if s and s ~= "" then t[#t + 1] = s end end

  if key == "port" then
    local link = str(doc, "link") or num(doc, "link")
    local sfp = str(doc, "part_number") or str(doc, "vendor_part_number")
    put(link and ("link=" .. link))
    put(sfp and ("sfp=" .. sfp))
  elseif key == "refclk" or key == "diag/refclk" then
    local st = num(doc, "status") or str(doc, "status")
    local li = str(doc, "locked_interface")
    put(st and ("status=" .. st))
    put(li and ("locked=" .. li))
  elseif key == "flows" or key == "sources" or key == "receivers" or key == "senders" then
    local ip, port = str(doc, "dst_ip_addr"), num(doc, "dst_udp_port")
    local fmt = str(doc, "format_type") or str(doc, "vid_format")
    if ip then
      put("dst=" .. ip .. (port and (":" .. port) or ""))
    else
      put(fmt and ("format=" .. fmt))
    end
  elseif key == "telemetry" then
    local temp, fan = num(doc, "core_temp"), num(doc, "fan_speed")
    put(temp and ("temp=" .. temp))
    put(fan and ("fan=" .. fan))
  elseif key == "self/information" then
    put(str(doc, "base_type"))
    put(str(doc, "current_version") and ("fw=" .. str(doc, "current_version")))
    put(str(doc, "serial_number") and ("sn=" .. str(doc, "serial_number")))
  elseif key == "self/ipconfig" then
    put(str(doc, "hostname"))
    put(str(doc, "address") or str(doc, "ip"))
  elseif key == "sdi_output" or key == "sdi_input" then
    local f = str(doc, "vid_format") or str(doc, "format")
    put(f and ("format=" .. f))
  elseif key == "sdp" or key == "receivers_sdp" or key == "senders_sdp" then
    local s, c = doc:match("s=([^\r\n]*)"), doc:match("c=IN IP4 ([^\r\n/]*)")
    put(s and ("s=" .. s))
    put(c and ("c=" .. c))
  end
  return table.concat(t, " ")
end

-- streams remembers what each TCP stream is carrying, because a reply
-- says only "200 OK" — which resource it is about is in the request
-- that went the other way. frames[] pins the answer to a frame number
-- so a second pass says the same thing as the first.
local streams = {}

local function streamState(pinfo)
  local s = tcp_stream()
  local key = s and tostring(s.value) or (tostring(pinfo.src) .. ">" .. tostring(pinfo.dst))
  streams[key] = streams[key] or { pending = {}, frames = {} }
  return streams[key]
end

-- headerBlock splits the message: a table of headers, where the body
-- starts, and the raw block for the tree. nil when it has not all
-- arrived.
local function headerBlock(text)
  local sep = text:find("\r\n\r\n", 1, true)
  if not sep then return nil end
  local block = text:sub(1, sep - 1)
  local h = {}
  for line in block:gmatch("[^\r\n]+") do
    local k, v = line:match("^([%w%-]+):%s*(.*)$")
    if k then h[k:lower()] = v end
  end
  return h, sep + 4, block
end

-- dissectMessage decodes one request or one response. Returns the
-- bytes consumed, or 0 when more of the stream is needed.
local function dissectMessage(tvb, pinfo, tree)
  local text = tvb:raw()
  local h, bodyAt, block = headerBlock(text)
  if not h then
    -- More of the header block is coming. Claim what is here and ask
    -- for the next segment: returning 0 would mean "not mine", and TCP
    -- would hand the rest of OUR message to somebody else's dissector
    -- — which is exactly how half the replies ended up decoded by the
    -- built-in http the first time this was measured.
    pinfo.desegment_offset = 0
    pinfo.desegment_len = DESEGMENT_ONE_MORE_SEGMENT
    return tvb:len()
  end
  local want = tonumber(h["content-length"] or "0") or 0
  local have = #text - (bodyAt - 1)
  local truncated = false
  if want > 0 and have < want then
    if pinfo.can_desegment > 0 then
      pinfo.desegment_offset = 0
      pinfo.desegment_len = want - have
      return tvb:len()
    end
    truncated = true
  end

  local firstLine = block:match("^([^\r\n]*)")
  local st = streamState(pinfo)
  local body = text:sub(bodyAt, bodyAt + (want > 0 and want or have) - 1)

  local method, uri, version = firstLine:match("^(%u+) (%S+) (HTTP/[%d%.]+)$")
  local respVer, code, reason = firstLine:match("^(HTTP/[%d%.]+) (%d%d%d) ?(.*)$")

  local api, res, arr, dir
  if method then
    api, res, arr = classify(uri)
    if not api then return 0 end
    dir = "request"
    local pend = { uri = uri, api = api, res = res, arr = arr }
    st.frames[pinfo.number] = pend
    st.pending[#st.pending + 1] = pend
  elseif code then
    dir = "response"
    local pend = st.frames[pinfo.number]
    if not pend then
      pend = table.remove(st.pending, 1)
      st.frames[pinfo.number] = pend
    end
    if not pend then return 0 end
    api, res, arr, uri, version = pend.api, pend.res, pend.arr, pend.uri, respVer
  else
    return 0
  end

  local key, inst, sub = split(res)
  local t = tree:add(p, tvb(), "Riedel MuoN/FusioN REST (dhs)")
  t:add(f_api, api)
  t:add(f_dir, dir)
  t:add(f_uri, uri)
  t:add(f_resource, key)
  if inst then t:add(f_instance, inst) end
  if sub then t:add(f_sub, sub) end
  if arr then t:add(f_array, arr) end
  if version then t:add(f_version, version) end
  if h["content-type"] then t:add(f_ctype, h["content-type"]) end
  if want > 0 then t:add(f_len, want) end
  local ht = t:add(p, tvb(), "Headers")
  for line in block:gmatch("[^\r\n]+") do ht:add(f_header, line) end
  if #body > 0 then t:add(f_body, body) end
  if truncated then t:add_proto_expert_info(ef_trunc) end

  local what = key
  if inst then what = what .. "/" .. (shortID(inst) or inst) end
  if sub then what = what .. "/" .. sub end
  if arr then what = what .. " [" .. arr .. "]" end

  local summary = describe(key, body)
  if summary ~= "" then t:add(f_summary, summary) end

  pinfo.cols.protocol = (api == "mnset") and "MNSET/REST" or "EMSFP/REST"
  if dir == "request" then
    t:add(f_method, method)
    local tail = (method ~= "GET" and summary ~= "") and ("  <- " .. summary) or ""
    pinfo.cols.info = string.format("%s %s%s", method, what, tail)
    if api ~= "mnset" and not known[key] then t:add_proto_expert_info(ef_unknown) end
    return #text
  end

  local c = tonumber(code)
  t:add(f_status, c)
  if reason and reason ~= "" then t:add(f_reason, reason) end
  local refused = (c < 200 or c >= 300)
  t:add(f_refused, refused)
  if refused then
    local msg = str(body, "message")
    if msg then t:add(f_devmsg, msg) end
    t:add_proto_expert_info(ef_refused)
    pinfo.cols.info = string.format("%d %s REFUSED%s", c, what, msg and (' "' .. msg .. '"') or "")
  else
    pinfo.cols.info = string.format("%d %s%s", c, what, (summary ~= "") and ("  " .. summary) or "")
  end
  return #text
end

-- ours reports whether this frame belongs to the emSFP / MN SET
-- conversation: a request to one of our APIs, or any frame on a stream
-- where we already saw one.
local function ours(tvb, pinfo)
  if tvb:len() < 16 then return false end
  local text = tvb:raw(0, math.min(tvb:len(), 512))
  local uri = text:match("^%u+ (%S+) HTTP/1%.[01]\r\n")
  if uri and classify(uri) then return true end
  local s = tcp_stream()
  if s and streams[tostring(s.value)] then return true end
  return false
end

-- The module IS an HTTP server on 80, so owning the port is the only
-- way this dissector — rather than Wireshark's http — decodes our
-- wire, which is what root CLAUDE.md requires. Traffic on the same
-- port that is NOT ours is handed straight back to http, so a mixed
-- capture still reads normally: we take our protocol, not the port.
local http_fallback

function p.dissector(tvb, pinfo, tree)
  if not ours(tvb, pinfo) then
    if http_fallback then return http_fallback:call(tvb, pinfo, tree) end
    return 0
  end
  return dissectMessage(tvb, pinfo, tree)
end

local tcp_port = DissectorTable.get("tcp.port")
if tcp_port then
  http_fallback = tcp_port:get_dissector(80) or Dissector.get("http")
  -- 80 the module, 8080 MN SET, 9080 its NBAPI.
  for _, port in ipairs({ 80, 8080, 9080 }) do tcp_port:add(port, p) end
end
