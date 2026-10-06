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
| `streams` | AES67 receivers and senders: mode, multicast and port of both legs, source, format |
| `sources`, `dests`, `xp` | The two axes of the crosspoint matrix; the crosspoints active now |
| `conferences`, `groups`, `ifbs`, `logic` | Those objects, with members or state |
| `users`, `patches`, `logicdests` | Only with `--from` a walk |

Filters: `--node N`, `--type TEXT`, `--match TEXT`.

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
configuration. About 3,500 requests on a 500-port system; the same requests
other control systems send all day. `--skip` leaves out `properties`,
`commands` or `values`.

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

One row per value of every port and client card, in the CSV layout of the
other dhs connectors: `path`, `oid` (object ID), `label`, `kind`, `access`,
`value`, `value_name`, `unit`, `min`, `max`, `default`, `enum_items`.
`access` is `RW-` for the values `import` can write, `R--` for the rest.
`--format json` or a `.json` name gives the same rows as JSON.

| Collect | Why |
|---|---|
| The CSV | To check that every value is what Director shows; later, to edit and import on a test system |
| The line `exported N values (M writable)` | A quick size check |

## 3. Listen

### watch — what happens, as it happens

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

`--spy all` adds key events for every port that has keys; `--spy NODE.PORT`
for chosen panels. `--events raw` prints method and parameters as received.
`--alive show` also prints the keep-alive pings.

| Collect | Why |
|---|---|
| `watch.txt` | The decoded events |
| The capture `.jsonl` | The raw events, and the tool's own notes (registered, dropped, unregistered, counts) |
| A note of what was done on the intercom and when | To match an action with its lines |
| Any line starting with `event` | An event the tool does not decode yet |

Only port on line, the connect message and the keep-alive have been seen
from a real RRCS so far; the other lines are decoded from the specification.

## 4. Change — test system only

Every command of this part needs `--write-to TESTHOST`.

### xp — one crosspoint

```
.\dhs.exe consumer rrcs xp HOST --src net.1.node.61.port.1026 --dst net.1.node.63.port.1043
.\dhs.exe consumer rrcs xp TESTHOST --src SRC --dst DST --state on  --write-to TESTHOST --capture auto
.\dhs.exe consumer rrcs xp TESTHOST --src SRC --dst DST --state off --write-to TESTHOST --capture auto
```

Without `--state` it only reads. With it, it sets or removes the
crosspoint and reads the state back.

### set — properties of a port or a client card

```
.\dhs.exe consumer rrcs set TESTHOST --path net.1.node.61.port.7.out --prop PortAes67Output.Multicast=239.250.1.1 --prop PortAes67Output.MulticastPort=5004
.\dhs.exe consumer rrcs set TESTHOST --path net.1.node.60.card.1 --prop Ptp.PTP=100 --prop Ptp.PtpPriority=128
```

As written above it sends nothing: it shows the current values beside the
wanted ones, and the request. Add `--apply yes --write-to TESTHOST` to send.
The names are those `get --path` prints.

### import — a whole file

```
.\dhs.exe consumer rrcs import HOST --file rrcs.csv --dry-run
.\dhs.exe consumer rrcs import TESTHOST --file rrcs.csv --write-to TESTHOST --capture auto
```

`--dry-run` compares the file with the live system and sends nothing: safe
anywhere. Without it, every writable value that differs is written, then
everything is read back.

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

## 5. When something goes wrong

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
