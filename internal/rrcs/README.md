# dhs consumer rrcs — command guide

Riedel RRCS gateway (XML-RPC over HTTP, port 8193). One section per verb:
what it does, the command to copy, what to collect.

- `HOST` is the RRCS to read. Every verb in parts 1 to 3 only reads.
- `TESTHOST` is a test RRCS. The verbs of part 4 write; they refuse to run
  without `--write-to`, and **no write has passed on a real RRCS yet** — one
  stopped a production RRCS on 2026-10-06 (see `CLAUDE.md`). Use them on a
  test system only, with `docs/oracle-test-plan.md`.
- `--capture auto` records every request and answer to
  `captures\rrcs\<host>\<verb>-<time>.jsonl`. It is the file to send for any
  question. It holds the names of the configuration in clear.
- `--output json` gives the same result for a program.
- Paths are what `tree` and `list` print:
  `net.1.node.61.port.1026`, `…port.7.out`, `…port.1026.key.0.1.5`,
  `net.1.node.60.card.1`, `group.665663144`.

## 0. What each verb costs RRCS

Every verb except `watch` opens one connection, sends its requests, reads
the answers and closes. `watch` keeps one connection and one listening
port until it is stopped, then removes its registrations.

A verb sends the requests its answer is made of, and no other:

| Verb | Requests |
|---|---|
| `info` | 4 |
| `get --path` of a port or a client card | 1 |
| `get --path` of a conference, group, IFB | 1 |
| `list conferences`, `groups`, `ifbs`, `cards`, `logic` | 1 |
| `list ports`, `panels`, `streams`, `sources`, `dests` | 1 (the port list: one request, a large answer) |
| `list xp` | 2 |
| `list keys` | 2, plus 1 per panel (`--keys all`: 1 per port) |
| `tree` | 16, plus 1 per panel (`--keys no`: 16) |
| `xp` | 1 (`--level yes`: 6) |
| `set` | 3: read, change, read back |
| `ensure`, per value, port or stream link | 1 to 3 |
| `ensure`, keys of one panel | about 4 to read, 1 per change, 4 to read back |
| `ensure`, a conference, group or IFB | 1 to read, 1 per change, 1 to read back |
| `watch`, at start | 9 (the version and the eight lists extract reads; `--changes keys` or a panel spy: plus 1 per panel), then 1 every 30 s to check its registration |
| `watch`, on a configuration change | 8 (`--changes keys`: plus 1 per panel; `--match` naming one port or card: 1) |
| `extract` | 9 (version and the eight lists), plus the keys and the commands of one port per type |
| `export`, one slot or all | 4 lists, plus 1 per panel for its keys (`--slot N`: the same reads, the rows of that slot) |
| `walk` from a live gateway | thousands: the full copy, for once in a while |

## 1. Look

### info — is the gateway there

```
.\dhs.exe consumer rrcs info HOST
```

Version, state (Working / Standby), connected to Artist, configuration ID.

| Collect | Why |
|---|---|
| The four lines | `IsConnected` false means RRCS runs but its network is off; the configuration ID changes when the configuration does |

### tree — the whole system at a glance

```
.\dhs.exe consumer rrcs tree HOST
.\dhs.exe consumer rrcs tree HOST --node 61 --type RSP
.\dhs.exe consumer rrcs tree HOST --keys no
```

Nodes, client cards, ports, what is on each key, then conferences, groups,
IFBs with members, logic sources.

| Collect | Why |
|---|---|
| The output, redirected to a file (`> tree.txt`) | A readable picture of the configuration |

### list — one table

```
.\dhs.exe consumer rrcs list ports HOST
.\dhs.exe consumer rrcs list panels HOST --node 61
.\dhs.exe consumer rrcs list keys HOST --match NOC
.\dhs.exe consumer rrcs list streams HOST --type Output
.\dhs.exe consumer rrcs list cards HOST
.\dhs.exe consumer rrcs list sources HOST
.\dhs.exe consumer rrcs list dests HOST
.\dhs.exe consumer rrcs list xp HOST
.\dhs.exe consumer rrcs list conferences HOST
```

| Kind | Rows |
|---|---|
| `nodes`, `cards` | Nodes; client cards with media networks, NMOS registry, PTP |
| `ports`, `panels` | All ports; those with keys. Alias and gains appear only with `--from` a walk |
| `keys` | What each key does and its target |
| `streams` | AES67 receivers and senders, one row per port and direction: in or out, the number Director shows (-7.41), object ID, mode, channels, channel used (`SEL`), multicast and port of both legs, source, format |
| `sources`, `dests`, `xp` | The two axes of the crosspoint matrix; the crosspoints active now |
| `conferences`, `groups`, `ifbs`, `logic` | Those objects, with members or state |
| `users`, `patches`, `logicdests` | Only with `--from` a walk |

Filters: `--node N`, `--type TEXT`, `--match TEXT`. `--output csv` prints
any of these tables as CSV, `--output json` as JSON:

```
.\dhs.exe consumer rrcs list streams HOST --node 63 --output csv > card4.csv
```

A port that uses a channel above 1 (`SEL`) takes it from the stream of
another port. RRCS does not report which one: the link is write-only.

### get — one thing

```
.\dhs.exe consumer rrcs get HOST --path net.1.node.61.port.1026
.\dhs.exe consumer rrcs get HOST --path net.1.node.60.card.1 --prop Ptp
.\dhs.exe consumer rrcs get HOST --id 665663144
.\dhs.exe consumer rrcs get HOST --id 665663144 --names yes
```

`--path` reads what the lists hold; `--id` asks RRCS for the properties of
one object.

## 2. Keep

### walk — everything, into one file

```
.\dhs.exe consumer rrcs walk HOST --capture auto
.\dhs.exe consumer rrcs walk HOST --skip values,commands
```

Status, every list, the licence, the properties of every object, the key
assignments of every port, and per port label, alias, gains and key
configuration. About 3,500 requests on a 500-port system. `--skip` leaves
out `properties`, `commands`, `values` or `singles`.

A walk sends nothing RRCS has to refuse. It first learns which ports are on
line, by a registration of a second that is removed at once (`--online yes`,
the default; it listens on port 8196, not the port of `watch`), and does
not ask the others for gain and level; smart panels, which have no gain,
are not asked either. `--online no` asks every port.

| Collect | Why |
|---|---|
| The summary on screen | `failed requests` should be 0; `port not online` counts unplugged ports and is normal |
| `snapshots\rrcs\<host>\walk-<time>.json` | The snapshot. `tree`, `list`, `get --path` and `export` read it with `--from FILE`, without touching RRCS |
| The capture `.jsonl` | The raw answers, to check a doubtful value |

### export — a table to edit

```
.\dhs.exe consumer rrcs export HOST --out rrcs.csv
.\dhs.exe consumer rrcs export HOST --out streams.csv --path PortAes67,Ptp,Nmos
.\dhs.exe consumer rrcs export --from walk.json --out rrcs.csv
```

One row per value of the whole system — the node, every client card,
every port with its keys, and the conferences, groups, IFBs and logic
sources with one row per member — in the CSV layout of the other dhs
connectors: `path`, `oid` (object ID), `label`, `kind`, `access`,
`value`, `value_name`, `unit`, `min`, `max`, `default`, `enum_items`.
The path is the reference, as Director shows it; the `oid` is the
ObjectID RRCS itself gives the object. The crosspoints are not rows:
`list xp` and `xp` carry them.
`access` is `RW-` for the values `import` can write, `R--` for the rest.
`--format json` or a `.json` name gives the same rows as JSON.

| Collect | Why |
|---|---|
| The CSV | To check that every value is what Director shows; later, to edit and import on a test system |
| The line `exported N values (M writable)` | A quick size check |

### extract — the device models and the inventory

```
dhs consumer rrcs extract 10.12.0.12
dhs consumer rrcs extract --from rrcs-walk-20261013.json
```

The models of the system (ADR-0022), one file for the node type and one
per client card type, under `.cache/dm/rrcs/<Type@version>.json`:
`ARTIST_1024@9.0.RR1-11.json` holds the node's own properties and, as
templates, the conference, group, IFB, logic source, key and the 26
command types of the specification; `AES67@9.0.RR1-11.json` holds the
card's own properties and one template per port type met (FOUR_WIRE_SPLIT
with its `in` and `out` halves, FOUR_WIRE, RSP_1232HL with its keys,
BOLERO_BP, ARTIST_64 — the names RRCS gives in `GetPortExTypeList`, 49
on a 9.0 where the 9.0.1 tables print 30). A template is the shape of a
type: every property with its kind, bounds, named values and default as
the specification prints them — never a value of the plant. The
specification gives the shape in advance; the walk says which types this
version has, which blocks each carries, what it exposes beyond the
printed tables (`observed`) and what it does not expose of them
(`not-exposed`), both counted in the account.

The inventory goes to `.cache/manifest/rrcs/<host>.json` (ADR-0028): one
entry for the node, one per card, one per port with its address, its
object IDs (one per half of a split port), its type, its template and
its role — `stream-block` with its channel count, `stream-linked` with
its channel, `panel` with its key and page counts, `port`. The main port
of a linked one is never reported by RRCS and is not guessed.

Nine requests (the version and eight lists), plus the keys and the
commands of one port per type. A model that exists is kept (`--refresh`
rewrites it); the inventory is rewritten each time. `--output json`
prints the account as data.

## 3. Listen

### watch — what happens, as it happens

Three filters keep the stream to what is being looked at: `--match TEXT`
(the lines whose path contains it; the full path of one port or client
card also makes a configuration change re-read that object alone, one
request), `--oid N` (one object ID, the oid column of export), `--kind`
(node, card, stream, panel, port, key, conference, group, ifb, logic,
xp). What the watch says about itself (`gateway …`) always passes. On a
configuration change — RRCS says that something changed, never what —
the eight lists are read again and what differs is printed; the keys of
the panels only with `--changes keys`; the actual inventory manifest
(`.cache/manifest/rrcs/<host>.json`) is rewritten when something
differed, so it stays what `extract` would write now.

```
.\dhs.exe consumer rrcs watch HOST --capture auto > watch.txt
.\dhs.exe consumer rrcs watch HOST --spy all --capture auto > watch.txt
.\dhs.exe consumer rrcs watch HOST --spy 61.1026 --events raw
```

Registers for every event and prints one line per value:
`time  oid=<object ID> <path> <member> = <value> [unit] [range]  (label)`.
RRCS sends the events to this machine on port 8195 (`--listen`). Stop with
**one** Ctrl+C and wait for the prompt: the tool then unregisters.

| Event | Line |
|---|---|
| Port goes on or off line | `…port.1026 Online = true` |
| Crosspoint made or removed | `xp.<source>><destination> State = on` |
| Crosspoint volume | `… SingleVolume = -6.0 dB [-114.5..12.5]` |
| Logic source | `logic.<id> State = on` |
| GP input or output | `…port.P.gpi.N State = on` |
| Configuration sent | `gateway Configuration = changed` |
| Artist connection | `gateway ArtistConnection = connected` |
| Node or client card alarm | `net.1.node.2 UpstreamFailed = true` |
| Key pressed or released (`--spy`) | `…port.1026.keyevent.0.5 KeyAction = pressed` |
| Function key, rotary, numeric key (`--spy`) | One line per member |
| Anything not known yet | `event <Method> = <parameters>` |

`--spy` (Panel Spy, §8.12 and §9.9) needs the Panel Spy licence on the
RRCS. Without it RRCS answers "No client card acknowledge received" and
the rest of the watch runs as usual.

When RRCS says the configuration changed, it does not say what. `watch`
then reads the system again and prints what differs — a key, a conference,
a group, an IFB, a port, a client card — one line per value, with what it
was (`--changes yes`, the default; one request per panel at start and at
each change). `--changes no` prints only `gateway Configuration = changed`.

Crosspoint levels are followed by default (`--volume yes`): each crosspoint
that is made while watching is registered in both directions, because the
level a panel sets on a key is how loud it hears that key's port, which is
the crosspoint the other way round. `--volume no` turns it off.

`--output csv` prints one record per value in the columns of `export`
(`kind`, `value`, `unit`, `min`, `max`, `enum_items`, …), with `ts` and
`event` in front and `description` at the end:

```
.\dhs.exe consumer rrcs watch HOST --output csv > watch.csv
```

`--spy all` adds key and rotary events for every panel on line; `--spy NODE.PORT`
for chosen panels. On an Artist-1024 with AES67 client cards (RRCS 9.0) every
panel answered "No client card acknowledge received" and RRCS retried every
5 s, so it is off by default. `--events raw` prints method and parameters as
received. `--alive show` also prints the keep-alive pings.

| Collect | Why |
|---|---|
| `watch.txt` | The decoded events |
| The capture `.jsonl` | The raw events, and the tool's own notes (registered, dropped, unregistered, counts) |
| A note of what was done on the intercom and when | To match an action with its lines |
| Any line starting with `event` | An event the tool does not decode yet |

Only port on line, the connect message and the keep-alive have been seen
from a real RRCS so far; the other lines are decoded from the specification.

## 4. Change

Every command of this part that writes needs `--write-to HOST`: the host
again, as a second look at the target. Each has a dry run; use it first.

What has passed on a real RRCS 9.0 with an Artist-1024 (2026-10-08):

| Change | Result |
|---|---|
| Crosspoint level | applied, read back |
| Key: function, label, mode; emptied | applied, seen on the panel |
| Conference: created, deleted | applied |
| Stream put in manual mode with its address | applied |
| Stream back to NMOS, the mode alone | applied (the one NMOS edit that is safe, ADR-0035) |
| Address fields given to a stream in NMOS mode | **stops RRCS**; the verbs refuse it (ADR-0035) |
| Main port's channel count (manual with the count, then NMOS) | applied, 19 mains of card 4 |
| Port linked to the stream of another (`Mode` + `Selection`) | applied, 77 ports of card 4, both directions |
| Port created as 4-Wire Split, named, deleted | applied |

Groups, IFBs, port labels, aliases, gains and client card settings have
not met a real RRCS yet.

### xp — one crosspoint, and its level

```
.\dhs.exe consumer rrcs xp HOST --src net.1.node.61.port.1026 --dst net.1.node.63.port.1043
.\dhs.exe consumer rrcs xp HOST --src SRC --dst DST --level yes
.\dhs.exe consumer rrcs xp HOST --src SRC --dst DST --state on  --write-to HOST --capture auto
.\dhs.exe consumer rrcs set-xp-volume HOST --src SRC --dst DST --single yes --conference no --volume 201 --write-to HOST
```

Without `--state` it only reads. `--level yes` also reads the level: on an
Artist-1024 RRCS refuses the direct read (`GetXpVolume`), so the level is
read the way `watch` gets it. The level a panel sets on one of its keys is
the crosspoint from that key's port **to the panel**. A volume is 0 for
mute, else `(dB × 2) + 230`: 201 is −14.5 dB.

### set — properties of a port or a client card

```
.\dhs.exe consumer rrcs set TESTHOST --path net.1.node.61.port.7.out --prop PortAes67Output.Multicast=239.250.1.1 --prop PortAes67Output.MulticastPort=5004
.\dhs.exe consumer rrcs set TESTHOST --path net.1.node.60.card.1 --prop Ptp.PTP=100 --prop Ptp.PtpPriority=128
```

As written above it sends nothing: it shows the current values beside the
wanted ones, and the request. Add `--apply yes --write-to HOST` to send.
The names are those `get --path` prints. A set costs three requests: the
object is read, changed, read again.

An edit that gives address fields to a stream in NMOS mode is refused before
anything is sent; the mode alone passes (ADR-0035). A link names the main
port and the channel, nothing else — an input takes them in two sets, an
output in one:

```
.\dhs.exe consumer rrcs set HOST --path net.1.node.63.port.1073.in  --prop PortAes67Input.Mode=1072
.\dhs.exe consumer rrcs set HOST --path net.1.node.63.port.1073.in  --prop PortAes67Input.Selection=2
.\dhs.exe consumer rrcs set HOST --path net.1.node.63.port.1073.out --prop PortAes67Output.Mode=1072 --prop PortAes67Output.Selection=2
```

For more than a port or two, `import` does all of it from one file.

### import — a whole file

```
.\dhs.exe consumer rrcs import HOST --file rrcs.csv --dry-run
.\dhs.exe consumer rrcs import TESTHOST --file rrcs.csv --write-to TESTHOST --capture auto
```

The stream rows of a port go through it too, with the hardware's own
rules applied (ADR-0035): a `…Channels` row on an NMOS main is done by the
round trip through manual; `…Mode` (the main port — never reported by
RRCS, so you add the row) and `…Selection` link a port, an input in two
requests, an output in one; a `…PortType` row on a port that does not
exist creates it (3 = 4-Wire Split) and names both directions the way
Director does. Every value is typed and bounded by the specification's
tables before anything is sent. A whole card in one file:

```csv
path,value
net.1.node.63.port.1080.in.PortAes67Input.Channels,8
net.1.node.63.port.1081.in.PortAes67Input.Mode,1080
net.1.node.63.port.1081.in.PortAes67Input.Selection,2
net.1.node.63.port.1056.PortType,3
```

`--dry-run` compares the file with the live system and sends nothing: safe
anywhere. Without it, every writable value that differs is written, then
everything is read back.

The key rows of a panel go through import too, by the key path ensure
proved on the plant: `key.E.P.K.cmd.CommandType` (the function as the
export prints it — `call-to-port-cmd`, `call-to-conference`,
`call-to-group`, `call-to-ifb`, `reply-cmd`; empty = the key is emptied)
with `key.E.P.K.cmd.Target` (a port path, `conference.ID`, `group.ID`,
`ifb.ID`), `key.E.P.K.LabelValue` and `key.E.P.K.KeyMode` (as read: 1
Auto, 2 Momentary, 3 Latching, or the word). The other key properties
stay read only. So the rows `ensure --manifest` saves before a retype,
and a slot's export, bring a panel's keys back with one import.

### ensure — a desired state, for Ansible

```
.\dhs.exe consumer rrcs ensure HOST --file desired.json --check --output json
.\dhs.exe consumer rrcs ensure TESTHOST --file desired.json --write-to TESTHOST --output json
```

`desired.json` names values by the path `export` prints, and crosspoints by
their two ports. Only what the file names is touched.

```json
{
  "values": {
    "net.1.node.60.card.1.Ptp.PTP": 100,
    "net.1.node.60.card.1.Nmos.RegistrationIp": "10.0.0.15",
    "net.1.node.63.port.1045.out.PortAes67Output.Protocol": "Manual",
    "net.1.node.63.port.1045.out.PortAes67Output.Multicast": "239.5.63.45"
  },
  "crosspoints": [
    {"source": "net.1.node.63.port.1045.in", "destination": "net.1.node.61.port.1026", "state": "present"}
  ]
}
```

The file has more sections; each is optional:

```json
{
  "keys": [
    {"panel": "net.1.node.61.port.1024", "key": "0.1.14", "function": "call-to-port",
     "target": "net.1.node.61.port.1026", "label": "NOC2", "mode": "momentary"},
    {"panel": "net.1.node.61.port.1024", "key": "0.1.15", "state": "absent"}
  ],
  "conferences": [{"name": "Conference 040", "label": "CONF 40"}],
  "groups": [{"name": "Group NOC", "label": "NOC", "members": ["net.1.node.61.port.1024"]}],
  "ifbs": [{"number": 7, "label": "SPORT", "input": "net.1.node.61.port.1040", "mix_minus": ""}],
  "levels": [{"source": "net.1.node.61.port.1026", "destination": "net.1.node.61.port.1024", "single": "-14.5"}],
  "ports": [{"port": "net.1.node.61.port.1040", "label": "CODIP01A", "input_gain": "0", "output_gain": "mute"}]
}
```

| Section | Named by | Can do |
|---|---|---|
| `keys` | panel and `EXPANSION.PAGE.KEY` (0.1.14), as `list keys` prints | function (call-to-port, call-to-conference, call-to-group, call-to-ifb, reply), target, label, mode (auto, momentary, latching); `"state": "absent"` empties the key |
| `conferences` | `id`, or `name` to create | label, name; create; delete |
| `groups` | `id`, or `name` to create | label, name, the whole member list; create; delete |
| `ifbs` | `number` | label, name, input, output, mix minus (`""` = none), dim level; never created |
| `levels` | source and destination | single level in dB, or `mute` |
| `ports` | port path | label, alias, input and output gain in dB, or `mute` |

`--check` reports `would_change` and the `diff`, prints each request it
would send, and sends nothing: safe anywhere. An apply reports `changed`;
run again, it reports `changed: false`. The Ansible role `dhs_rrcs` wraps
it with one variable per section (`rrcs_values`, `rrcs_crosspoints`,
`rrcs_keys`, `rrcs_conferences`, `rrcs_groups`, `rrcs_ifbs`, `rrcs_levels`,
`rrcs_ports`), a dry-run by default; `playbooks/rrcs-ensure.yml` and
`playbooks/rrcs-panel.yml` are the examples.

The streams of an AES67 card are a section too:

```json
{"streams": [{"main": "net.1.node.63.port.1072", "block": 8}]}
```

`"block": 8` links the seven ports after the main one to it, channel 2 to
8, inputs and outputs (`"linked"` names the ports instead, `"directions":
["in"]` keeps one side, `"channels"` forces the main's count). The proven
order is used (ADR-0035): the main's channel count by the round trip
through manual, inputs linked in two requests, outputs in one. RRCS does
not report a link, so a port counts as done when it uses the wanted
channel. Creating the ports first is `import`'s `PortType` row.

### ensure --manifest — the inventory as the desired state

```
dhs consumer rrcs ensure <host> --manifest desired.json --check
dhs consumer rrcs ensure <host> --manifest desired.json --write-to <host>
```

The manifest `extract` writes is the actual inventory; a copy of it,
edited, is the desired one (keep it under `snapshots/rrcs/<host>/`, or in
the Ansible inventory). `ensure --manifest` reads the system as `extract`
does (nine requests), compares every port entry — address, `type` or
`template`, `role` — and sends the difference in the proven order,
through the same convergence as `import`:

| Entry | What is sent |
|---|---|
| a port the system does not have | one `create` with its type, then its two names by the Director pattern; its role rows follow in a second pass, once it exists |
| `role.in` or `role.out` = `{"kind": "stream-block", "channels": 8}` | the channel count of that half: on an NMOS stream the round trip (manual with the count, then NMOS alone) |
| `role.in` or `role.out` = `{"kind": "stream-linked", "main": 1064, "channel": 2}` | the link: `Mode` then `Selection` on an input, both in one request on an output |
| `panel`, `port`, `stream` | nothing: values stay with `import` |
| a port present with another type | a retype — RRCS has no edit of a type: refused, with what points at the port, unless `--allow-retype`; then the port's rows and those that point at it are saved to `snapshots/rrcs/<host>/retype-<node>-<port>-<time>.csv`, the port is deleted and created, and its role applied |
| a port the manifest does not name | left alone, counted as "not named" |

The type of a port to create is checked against the gateway's own type
list (`GetPortExTypeList`, 49 names on a 9.0), as `import` now does too.
After a write the actual manifest is rewritten; `--check` prints the
ADR-0007 shape (`would_change`, `diff[]`) and sends nothing. Per block of
eight both directions: 49 requests.

### call — any method of the specification

```
.\dhs.exe consumer rrcs call HOST GetPortAlias --args-file args.json
.\dhs.exe consumer rrcs call TESTHOST SetPortAlias --args-file args.json --write-to TESTHOST
```

`args.json` holds the parameters in the order of the specification, as one
JSON array: `[1, 61, 1026, false]`. The transaction key is added in front.
A method named `Get…` or `Is…` only reads and needs no `--write-to`.

| Collect, for every command of this part | Why |
|---|---|
| The screen output | `done`, `refused: …` or `still differ` |
| The capture | The exact request |
| The RRCS log of that minute | How RRCS understood the request, and its own error text |

## 5. Logs and alarms

Every verb that talks to a gateway logs, like the other dhs connectors:

| Flag | Default | Meaning |
|---|---|---|
| `--log` | `auto` | Local file, one per day: `.cache\logs\rrcs\<host>\<verb>.log`. A path replaces it; `off` disables it |
| `--log-format` | `syslog` | `syslog` (RFC 5424), `json` or `text` |
| `--syslog-addr` | none | Also send every record to a syslog server, `host:port`, UDP |
| `--log-level` | `info` | `debug`, `info`, `warn`, `error` |

`watch` writes one `value_change` record per decoded event (`proto`,
`event`, `oid`, `path`, `label`, `value`, `unit`, `name`). `set` and
`ensure` write one `config_change` record per value changed (`verb`,
`target`, `path`, `from`, `to`, and `mode`: `applied` or `dry_run`) and one
`config_failed` record, at warning level, per value that could not be
brought to its target, with the `reason`.

```
.\dhs.exe consumer rrcs watch HOST --syslog-addr 10.0.0.5:514 --alarm RRCS@9.0.json
```

`--alarm FILE` judges the values with an alarm template
(`internal/rrcs/alarm/RRCS@9.0.json`) and prints, and logs with its
severity, each change of verdict:

| Condition | Severity | Where the severity comes from |
|---|---|---|
| RRCS loses the Artist system | critical | Seen: nothing can be read or controlled in that state |
| RRCS drops the registration of the watch | major | Seen: RRCS then sends nothing more; every other value is stale until it is registered again |
| A port goes off line for 30 s | minor | RRCS itself rates "Panels offline" Minor |
| Node and client card alarms | none yet | Neither the specification nor RRCS rates them; the plant has to |

## 6. When something goes wrong

| Symptom | Meaning |
|---|---|
| `actively refused` | Nothing listens on that address and port: wrong address, or RRCS is not running |
| `context deadline exceeded` | No route to the address, or RRCS is not answering |
| `network connection to artist is not enabled (code 11)` | RRCS runs; its network is switched off in the RRCS window |
| `Port is not online (code 24)` | The port is unplugged: normal for gains |
| `XML-RPC fault …` | RRCS refused the request and says why |
| `forcibly closed by the remote host` on a write | RRCS may have stopped. Run `info`; keep the RRCS log |
| `this would WRITE to …` | The write guard: add `--write-to` only for a test system |

The RRCS log file records every request with the name of the client
(`User-Agent='dhs-rrcs'`), how RRCS read its parameters, and the result. It
is the best evidence for any doubt: send it with the capture.
