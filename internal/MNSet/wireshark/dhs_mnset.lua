-- dhs_mnset.lua — Riedel MuoN eMSFP / FusioN node REST (emsfp/node/v1)
--
-- The wire is plain HTTP/JSON, so this is a post-dissector layered on
-- Wireshark's http decoder: it recognises requests to /emsfp/node/v1/…
-- (and MN SET's /api/… + NBAPI /rest/<array>/<i>/emSFP/node/v1/…), names
-- the resource, and rewrites Protocol | Info so one filter
-- (dhs_mnset) shows the whole control conversation the same way the
-- other dhs dissectors do. Responses are tied to their request by
-- http.response_for.uri.
--
-- Filters:
--   dhs_mnset                              every module / MN SET exchange
--   dhs_mnset.resource == "flows"          one resource
--   dhs_mnset.method == "PUT"              writes only
--   dhs_mnset.api == "nbapi"               through MN SET's NBAPI

local p = Proto("dhs_mnset", "Riedel MuoN/FusioN REST (dhs)")

local f_api      = ProtoField.string("dhs_mnset.api",      "API",       base.ASCII)
local f_method   = ProtoField.string("dhs_mnset.method",   "Method",    base.ASCII)
local f_resource = ProtoField.string("dhs_mnset.resource", "Resource",  base.ASCII)
local f_array    = ProtoField.string("dhs_mnset.array",    "NBAPI array/index", base.ASCII)
local f_status   = ProtoField.uint16("dhs_mnset.status",   "HTTP status", base.DEC)
local f_uri      = ProtoField.string("dhs_mnset.uri",      "URI",       base.ASCII)
local f_dir      = ProtoField.string("dhs_mnset.direction","Direction", base.ASCII)
p.fields = { f_api, f_method, f_resource, f_array, f_status, f_uri, f_dir }

local ef_unknown = ProtoExpert.new("dhs_mnset.unknown_resource", "Resource not in the MN SET Rest catalogue", expert.group.PROTOCOL, expert.severity.WARN)
local ef_refused = ProtoExpert.new("dhs_mnset.refused", "Module refused the request (non-2xx)", expert.group.RESPONSE_CODE, expert.severity.WARN)
p.experts = { ef_unknown, ef_refused }

local h_method   = Field.new("http.request.method")
local h_uri      = Field.new("http.request.uri")
local h_code     = Field.new("http.response.code")
local h_resp_uri = Field.new("http.response_for.uri")

-- The node catalogue as MN SET's Rest page lists it (verified on a
-- FusioN6, fw 0x68cd783f). Anything else on the node path is flagged,
-- not dropped — a new firmware adds resources and the dissector must
-- show them.
local known = {}
for _, r in ipairs({
  "self", "port", "flows", "sources", "receivers", "senders", "route",
  "devices", "sdi", "sdi_output", "sdi_input", "sdi_audio", "sdp",
  "receivers_sdp", "senders_sdp", "clean_switch", "refclk", "lldp", "telemetry",
  "self/information", "self/diag", "self/firmware", "self/phy", "self/interfaces",
  "self/ipconfig", "self/static_route", "self/license", "self/system",
  "self/syslog", "self/protocols",
  "diag", "diag/common", "diag/firmware", "diag/dns", "diag/flow",
  "diag/packet_interval_time", "diag/devices", "diag/2110-7_engine",
  "diag/refclk", "diag/nmos",
}) do known[r] = true end

-- classify returns api, resource, array for a request URI, or nil when
-- the URI is not one of ours.
local function classify(uri)
  if not uri then return nil end
  local path = uri:match("^([^?]*)")
  -- module direct
  local rest = path:match("^/emsfp/node/v1/?(.*)$")
  if rest then return "node", rest, nil end
  -- MN SET NBAPI: /rest/<array>/<index>/emSFP/node/v1/<resource>
  local arr, idx, r2 = path:match("^/rest/([^/]+)/([^/]+)/emSFP/node/v1/?(.*)$")
  if arr then return "nbapi", r2, arr .. "/" .. idx end
  -- MN SET app API
  local app = path:match("^/api/(.*)$")
  if app then return "mnset", app, nil end
  return nil
end

-- resourceKey trims a resource to its catalogue form: "flows/abc"→"flows",
-- "self/ipconfig"→"self/ipconfig", "" (the root listing)→"/".
local function resourceKey(res)
  if res == "" then return "/" end
  local a, b = res:match("^([^/]+)/([^/]+)")
  if a and (a == "self" or a == "diag") and b then return a .. "/" .. b end
  return res:match("^([^/]+)") or res
end

function p.dissector(tvb, pinfo, tree)
  local method, uri, code, resp_uri = h_method(), h_uri(), h_code(), h_resp_uri()
  local dir
  if method and uri then
    dir = "request"
  elseif code and resp_uri then
    dir = "response"
    uri = resp_uri
  else
    return
  end
  local api, res, arr = classify(tostring(uri))
  if not api then return end

  local st = tree:add(p, tvb(), "Riedel MuoN/FusioN REST (dhs)")
  st:add(f_api, api)
  st:add(f_dir, dir)
  st:add(f_uri, tostring(uri))
  local key = resourceKey(res)
  st:add(f_resource, key)
  if arr then st:add(f_array, arr) end

  pinfo.cols.protocol = (api == "mnset") and "MNSET/REST" or "EMSFP/REST"
  if dir == "request" then
    local m = tostring(method)
    st:add(f_method, m)
    pinfo.cols.info = string.format("%s %s%s", m, key, arr and (" [" .. arr .. "]") or "")
    if api ~= "mnset" and not known[key] then st:add_proto_expert_info(ef_unknown) end
  else
    local c = tonumber(tostring(code))
    st:add(f_status, c)
    pinfo.cols.info = string.format("%d %s%s", c, key, arr and (" [" .. arr .. "]") or "")
    if c < 200 or c >= 300 then st:add_proto_expert_info(ef_refused) end
  end
end

register_postdissector(p)

-- Ports the http dissector must already own so the fields above exist:
-- modules answer on 80, MN SET on 8080, NBAPI on 9080. 80 is built in;
-- the other two are added here.
local ok, http_port = pcall(function() return DissectorTable.get("tcp.port") end)
if ok and http_port then
  local http = Dissector.get("http")
  if http then
    http_port:add(8080, http)
    http_port:add(9080, http)
  end
end
