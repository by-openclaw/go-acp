# kratos consumer — CLI

Kratos NGC2 IDU antenna controller. State over REST/JSON, changes over
MQTT. Wire context: [`../CLAUDE.md`](../CLAUDE.md).

## Account and target

```bash
export DHS_KRATOS_USER='<operator>'     # omit both on a low-security (no login) unit
export DHS_KRATOS_PASS='<password>'
H=<idu-ip>            # port 3000 (HTTP); <idu-ip>:4000 = HTTPS + MQTT/TLS
# export DHS_KRATOS_MQTT_PORT=1822      # only if the broker is not on 1883 (the document names both)
# export DHS_KRATOS_INSECURE=1          # self-signed certificate on 4000
```

## Verbs

| Verb | What it sends | Example |
|---|---|---|
| `info` | login, `GET consumer/boundaries` | `dhs consumer kratos info $H` |
| `walk` | `svtList` + `cvtList` + `enumerations` + `dependencies` | `dhs consumer kratos walk $H` |
| `get` | `GET consumer/cvtItem/<path>` | `dhs consumer kratos get $H --path antenna/motor/status/targetPlatAz` |
| `set` | `POST consumer/postMessage {"MCII":1,"NumArgs":2,"Message":"<UID>,<v>"}`, then `cvtItem` | `dhs consumer kratos set $H --path move/command/targetAz --value 120000` |
| `watch` | MQTT SUBSCRIBE `<UID>` or `<UID>/#` | `dhs consumer kratos watch $H --path antenna/motor` |
| `export` | the walk, to a file | `dhs consumer kratos export $H --format json --out ngc2.json` |
| `command` | `POST consumer/postMessage {"MCII":2,"NumArgs":1,"Message":"VERB args"}` | `dhs consumer kratos command $H --verb STOW` |
| `table` | `GET consumer/<table>[/args]` | `dhs consumer kratos table $H --query positionLog` |

A path is a UID with or without its root, slashed or dotted:
`antenna/motor/status/targetPlatAz`, `cpiasd/antenna/motor/status/targetPlatAz`
and `antenna.motor.status.targetPlatAz` are the same value. `watch` with
no path subscribes the whole tree (`cpiasd/#`); a node subscribes
everything beneath it.

`command --list` prints the §9 verbs; `command --check` prints the body
without sending. A command has no acknowledge: read the values it
changes, or `table --query noteLog`.

Useful tables: `positionLog[/FROM[/TO]]`, `positionByTime/<from>/<to>`,
`noteLog`, `systemLog`, `alarmPointsList[/subtree]`, `satelliteSettings`,
`almanac`, `serverStatus`, `systemConnections`, `snmptrapsinks`,
`security/clientTbl`.

## Output seen against the test fake

```text
$ dhs consumer kratos walk $H
[cpiasd.antenna.motor.status]
            0  targetPlatAz          string  RW-  "180000"
[cpiasd.tracking.general.status]
            2  trackState            enum    RW-  "2"     [uninitialized, not present, not tracking]

$ dhs consumer kratos set $H --path move.command.targetAz --value 120000
confirmed value = "120000"

$ dhs consumer kratos watch $H --path antenna/motor
00:50:35  …  cpiasd/antenna/motor/status/targetPlatAz  targetPlatAz  RW-  live  info  "180100"
```

These outputs come from an in-process fake built from the document's
examples, not from a real NGC2: no unit is in the lab yet.
