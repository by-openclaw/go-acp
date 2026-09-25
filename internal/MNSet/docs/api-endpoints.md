# MN SET REST API — discovered endpoints (2026-09-20)

Reverse-engineered from the running MN SET server at 10.6.250.105:8080 and
the app bundle. No vendor OpenAPI exists. Base: `http://<host>:8080`.

## App API (port 8080)

| method | path | auth | returns |
|---|---|---|---|
| GET | `/api/device` | none (here) | array of managed devices, full model (44 keys each) |
| GET | `/api/array` | none | arrays for SNMP/NBAPI; `[]` = none created → SNMP off |
| GET | `/api/appsetting` | **token** (403 without) | REST/SNMP/trap port settings |
| POST | `/api/authentication/login/` | — | login → token |
| GET | `/api/authentication/checkToken` | token | validate |
| GET | `/api/authentication/logout` | token | end session |
| GET | `/api/user`, `/api/user/{id}` | token | users |
| GET | `/api/syslog`, `/api/syslog/{id}` | token | syslog config |
| GET | `/api/file/icon`, `/api/file/logo` | none | UI assets |

## Device emSFP node REST (the "Rest" page — GET/PUT per Device IP)

The MN SET **Rest** tab (screenshot `localhost:8080/#/raw`) targets a
**Device IP** (e.g. 10.6.40.53) and exposes the emSFP node API as tabs.
These are the per-device resources — this is the "more endpoints" set:

**Page 1 (streaming/IO):** `self · port · flows · sources · receivers ·
senders · route · devices · sdi · sdi_output · sdi_input · sdi_audio ·
sdp · receivers_sdp · senders_sdp · clean_switch · refclk · lldp ·
telemetry`

**Page 2 (system):** `information · diag · firmware · phy · interfaces ·
ipconfig · static_route · license · system · syslog · protocols`

GET reads a resource, PUT writes it, "PUT Preset" applies a stored JSON.
`self/information` gives type (`22 - ST2110 UHD Transceiver`), base_type
FusioN6, sw/asic versions, 25G links. Reachable directly at
`http://<device-ip>/…` only from a host on the device's segment (the LXC
`dhs-debian` can reach 10.6.40.53; the desk cannot). MN SET proxies them
for everyone else via the backend endpoints below.

## MN SET backend operations (port 8080) — full set

112 endpoints under `/api/…` (from the app bundle). The load-bearing ones:

| group | endpoints |
|---|---|
| device | `/api/device`, `/deleteDevice/{id}`, `/deleteMultiDevices`, `/diagDevice/{id}`, `/upgradeDevices`, `/refresh/{id}`, `/refreshPorts/{id}`, `/reboot`, `/refresh/2022_7/{id}` |
| flows | `/flow/all`, `/flow/{id}`, `/flowsByDevice`, `/flowsByDevices`, `/flowsByID`, `/flow/reset`, `/flow/resetByDevice`, `/flow/refresh/{id}`, `/flowBufferControl/{id}`, `/flowTags/{id}`, `/getDestMac/{id}` |
| SDI/audio | `/sdi`, `/sdiOutput/{id}`, `/sdiOutputs`, `/sdiOutputsByDevice`, `/mapSDIAudioEncap/{id}`, `/mapSDIAudioDecap/{id}`, `/mapSDIAudioMultipleDevices`, `/audioMapping/*`, `/audioFlows/{id}`, `/madiOutput/{id}` |
| SDP | `/sdp/{id}/{leg}`, `/receiverSDP/{id}/{leg}`, `/senderSDP/{id}/{leg}`, `/SDPForVirtualDevice/{id}/{leg}` |
| network | `/ports`, `/phy/{id}`, `/sfp/{id}`, `/ipconfig`(via device), `/staticRoutes`, `/dns`, `/reference`, `/ptp`, `/lldp`, `/smpteNetworkClass/{id}` |
| streams | `/stream/program/{type}/{id}`, `/stream/buffer/{id}/{leg}`, `/stream/refresh/{id}`, `/packetIntervalTime/{id}` |
| CSV / network sheet | `/exportCSV`, `/applyCSV`, `/importCSV`, `/exportNetworkSpreadSheet`, `/applyNetworkConfigurationSpreadSheet`, `/importNetworkSpreadSheet`, `/exportMAC` |
| NMOS | `/diagNmos/{id}` |
| backup/restore | `/backup/*`, `/restore`, `/depack`, `/logs` |
| auth/admin | `/api/authentication/*`, `/api/user`, `/createUser`, `/api/appsetting`, `/brand` |

Full list: `testdata/endpoints-raw.txt` (112 lines) + `device-rest-tabs.txt`.

## NBAPI (port 9080) — per-array device REST

Only after an Array is created in MN SET:
```
http://<host>:9080/rest/<array-name>/<device-index|0>/emSFP/node/v1
```
`0` = all devices in the array. This is the MuoN eMSFP node API (GET/PUT).

## SNMP (after enabling on an array)

- agent poll port **1610/UDP** (not 1620)
- 1620/UDP = MN SET receives device SNMP (inbound, not for polling)
- enable: create Array → Enable SNMP → wait ~5 min → export MIB zip
- v1 (no community) or v2c (`public`); **monitoring only, no SET**

## Device model highlights (`/api/device[0]`)

- `info`: type `2110 Encap/Decap - F6`, base_type FusioN6, sn 125061600012,
  media in/out st2110+sdi, skuType `2R + 6T`, processingMode Embox 6
- `devices[8]`: channels `Device CH1..CH8`
- `interfaces`: e1 media (static 192.168.39.230 / current 10.6.40.53),
  e2 172.16.16.2, oob 192.168.40.230; each SFP module also has its own web (:80)
- `sfps[6]`: HDMI = `MN-Z-SFP-1T-HDMI-1.4` in slots 0,1,3,5 (the 4 HDMI modules)
- `senders[12]` / `receivers[36]`: thin `{id, device_id, flow_id}` pointers
- `flows[96]`: the subscription records (`network[]` = ST2110 legs)
- `sdiOutputs[8]`: SDI/HDMI output routing + `sdi_aud_chans_cfg`
- `nmos` / `diagNmos`: IS-04/05 state

## Access note

The Fusion/SFP module web pages (192.168.39.230, 10.6.40.53, 192.168.40.230)
sit on media/OOB VLANs that are not routed to the mgmt/desk network, so from
here only MN SET (10.6.250.105:8080) reaches them. On-site, browsing a
module IP gives its own web UI.
