# dhs_mnset.lua — the emSFP / MN SET REST dissector

The module's control wire is HTTP/1.1 with JSON bodies (and SDP as
text). This dissector decodes it **itself** — it does not layer on
Wireshark's `http`. That is the repo rule (root `CLAUDE.md`,
"Wireshark dissectors"), and there is a practical reason behind it: a
post-dissector's `Info` text is *appended* to what `http` already
wrote, so every frame reads twice:

```
HTTP/1.1 200 OK , JavaScript Object Notation (application/json)200 port/2  link=up
```

Owning the dissection gives the one-glance column every other dhs
connector has:

```
GET  port/2                    200 port/2  link=up sfp=GSS-MPO250-SRC
GET  flows/6baf596c            200 flows/6baf596c  dst=239.6.1.9:20000
GET  telemetry/node            200 telemetry/node  temp=62 fan=4459
PUT  self/ipconfig             400 self/ipconfig REFUSED "invalid netmask"
```

## What it claims

Registered on TCP **80** (the module), **8080** (MN SET) and **9080**
(its NBAPI). A stream is ours when its first request is
`/emsfp/node/v1/…`, `/rest/<array>/<i>/emSFP/node/v1/…` or `/api/…`;
anything else on those ports is handed straight back to Wireshark's
`http`, so a mixed capture still reads normally. **We take our
protocol, not the port.**

## Verify it

```bash
tshark -X lua_script:dhs_mnset.lua -r testdata/fusion6-walk-sample.pcapng -Y dhs_mnset
tshark -X lua_script:dhs_mnset.lua -r testdata/fusion6-walk-sample.pcapng \
       -Y 'http && !dhs_mnset' | wc -l        # must be 0: nothing of ours is left to http
```

`testdata/fusion6-walk-sample.pcapng` is the first 2 000 frames of a
real `dhs consumer mnset walk 10.6.40.53`, captured 2026-09-24. It
covers the root listing, self/{information,ipconfig,interfaces,system},
port, flows, refclk, telemetry, sdi_output and clean_switch.

## Install

| OS | Personal Lua plugin dir |
|---|---|
| Windows | `%APPDATA%\Wireshark\plugins\` |
| macOS / Linux | `~/.local/lib/wireshark/plugins/` |

Or deploy to the fleet with `ansible/playbooks/deploy-dissector.yml`.
