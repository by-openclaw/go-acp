# RRCS connector — test plan for the oracle

For the person at the test RRCS ("the oracle"). Run the steps in order, on
the oracle only. Each step says what to run, what to expect and what to
keep. Stop at the first step that does not behave as written and send what
it printed, the capture file and the RRCS log of that minute.

Nothing in parts C to G has ever worked on a real RRCS. One write did
reach a real RRCS, on 2026-10-06, and stopped it: see part D.

In the commands, `ORACLE` is the address of the test RRCS.

## 0. Before starting

| # | Do | Why |
|---|---|---|
| 0.1 | Use the newest `dhs.exe` | `.\dhs.exe consumer rrcs call --help` prints a help text only on a recent one |
| 0.2 | Know where the RRCS log file is and how to restart RRCS | Part D may stop RRCS |
| 0.3 | In the RRCS window, check that the network is enabled | Otherwise every request answers "network connection to artist is not enabled" |
| 0.4 | Run every command with `--capture auto` | The capture file is what gets analysed |
| 0.5 | Never replace `ORACLE` by the production address in a command that has `--write-to` | `--write-to` is what lets a command write |

## A. Reads

| # | Command | Expect |
|---|---|---|
| A1 | `.\dhs.exe consumer rrcs info ORACLE` | Four lines `ok`; `IsConnected` true |
| A2 | `.\dhs.exe consumer rrcs walk ORACLE --capture auto` | `failed requests 0`, or only failures on things the oracle does not have |
| A3 | `.\dhs.exe consumer rrcs tree ORACLE` | Nodes, ports, keys |
| A4 | `.\dhs.exe consumer rrcs list ports ORACLE` | One row per port, with alias and gains for online ports |
| A5 | `.\dhs.exe consumer rrcs export ORACLE --out oracle.csv` | A CSV, one row per value |

Keep: the walk `.json` and `.jsonl`, and `oracle.csv`.

From A4, choose and write down three things for the later parts:

| Name | What | Example |
|---|---|---|
| `PANEL` | A panel you can press keys on | `net.1.node.2.port.5` |
| `SPARE` | An output port nobody uses, with an AES67 sender if the oracle has AES67 | `net.1.node.2.port.40.out` |
| `SRC`, `DST` | A source and a destination you can listen to | from `list sources`, `list dests` |

## B. Events

```
.\dhs.exe consumer rrcs watch ORACLE --spy all --capture auto > watch.txt
```

While it runs, do each of these once, then stop with one Ctrl+C and wait
for the prompt.

| # | Action on the intercom | Line expected in `watch.txt` |
|---|---|---|
| B1 | Press and release a talk key on `PANEL` | `…keyevent… KeyAction = pressed`, then `released` |
| B2 | Same key | `xp.<source>><destination> State = on`, then `off` |
| B3 | Unplug and replug a panel or a 4-wire | `… Online = false`, then `true` |
| B4 | Turn a volume knob on a panel while talking | A `PanelSpyRotateEvent` line |
| B5 | Switch a logic source, if the oracle has one | `logic.<id> State = on` |
| B6 | Send a configuration from Director | `gateway Configuration = changed` |

A line that starts with `event` and shows raw parameters means the event
arrived in a shape the tool does not know yet: that is a finding, not a
failure. Keep `watch.txt` and the capture.

## C. Crosspoint: the first write

The simplest write of the protocol, with nothing stored in the
configuration.

| # | Command | Expect |
|---|---|---|
| C1 | `.\dhs.exe consumer rrcs xp ORACLE --src SRC --dst DST` | `State = off` |
| C2 | `.\dhs.exe consumer rrcs xp ORACLE --src SRC --dst DST --state on --write-to ORACLE --capture auto` | `State = on`; audio from SRC is heard at DST |
| C3 | `.\dhs.exe consumer rrcs list xp ORACLE` | One row: SRC, DST |
| C4 | `.\dhs.exe consumer rrcs xp ORACLE --src SRC --dst DST --state off --write-to ORACLE --capture auto` | `State = off` |

## D. Configuration edit: find what stops RRCS

On 2026-10-06 this request stopped a production RRCS 9.0: an edit of the
multicast address of an AES67 sender that was in NMOS mode. The RRCS log
ended on `Applying change ('1'/'1'): Edit PortEx`. The steps below change
one thing at a time, from the most harmless to that request.

After **each** step: run `.\dhs.exe consumer rrcs info ORACLE`. If it does
not answer, RRCS has stopped: note the step, save the RRCS log, restart
RRCS, and do not go further.

Every command of this part ends with
`--apply yes --write-to ORACLE --capture auto`; it is left out of the table.

| # | What changes | Command (`.\dhs.exe consumer rrcs set ORACLE` …) |
|---|---|---|
| D1 | The alias of an unused port | `--path SPARE --prop Alias=TEST1` |
| D2 | Put it back | `--path SPARE --prop Alias=""` |
| D3 | The mode of the sender, to Manual, nothing else | `--path SPARE --prop PortAes67Output.Protocol=2` |
| D4 | The multicast port, the sender being Manual | `--path SPARE --prop PortAes67Output.MulticastPort=5006` |
| D5 | The multicast address, the sender being Manual | `--path SPARE --prop PortAes67Output.Multicast=239.250.1.1` |
| D6 | Mode back to NMOS | `--path SPARE --prop PortAes67Output.Protocol=5` |
| D7 | The multicast address, the sender being NMOS — the request of 2026-10-06 | `--path SPARE --prop PortAes67Output.Multicast=239.250.1.2` |

Reading the result:

| Outcome | Meaning |
|---|---|
| D1 stops RRCS | The way the tool addresses a port in an edit is wrong. Try D8 |
| D1 works, D3 to D5 work, D7 stops RRCS | RRCS cannot edit the address of a stream NMOS owns: the tool must set Manual first, or refuse |
| D3 or D4 stops RRCS | The stream block itself is the problem; send the log |
| `refused:` with a text | A normal refusal. The text says what RRCS wants |
| `accepted the request but these still differ` | RRCS said yes and kept the old value. Note which step |

D8, only if D1 stops RRCS: the same alias edit with the address written as
the specification prints it, without `IsInput`. Save this as `d8.json`
(replace the two numbers by the node and port of `SPARE`):

```json
[[{"ChangeType":"edit","ObjectType":"portex","SpecificParams":{"PortAddress":{"Node":2,"Port":40},"Alias":"TEST8"}}]]
```

```
.\dhs.exe consumer rrcs call ORACLE ConfigurationChangeEx --args-file d8.json --write-to ORACLE --capture auto
```

## E. Client card: PTP and NMOS registry

Only after part D has shown which edits are safe. From
`.\dhs.exe consumer rrcs list cards ORACLE` take the path of one card
(`CARD`), and note its current values first.

| # | What changes | Command (`… set ORACLE` … `--apply yes --write-to ORACLE --capture auto`) |
|---|---|---|
| E1 | PTP priority 2 | `--path CARD --prop Ptp.PtpPriority2=129` |
| E2 | Put it back | `--path CARD --prop Ptp.PtpPriority2=128` |
| E3 | NMOS registry port | `--path CARD --prop Nmos.RegistrationPort=8081` |
| E4 | Put it back | `--path CARD --prop Nmos.RegistrationPort=8080` |

Watch the card while doing this: note whether it restarts or drops audio.
The tool sends `Ptp` inside an `Aes67` group, as the specification writes
it, although the lists show it at the top of the card. If E1 is refused or
ignored, that placement is the first suspect.

## F. Export, edit, import

| # | Do | Expect |
|---|---|---|
| F1 | `.\dhs.exe consumer rrcs export ORACLE --out oracle.csv --path PortAes67,Ptp,Nmos` | A CSV of the streams and card settings |
| F2 | In the file, change the `value` of one sender's `Multicast` row that part D proved safe | |
| F3 | `.\dhs.exe consumer rrcs import ORACLE --file oracle.csv --dry-run` | `would apply 1`, with the old and the new value |
| F4 | `.\dhs.exe consumer rrcs import ORACLE --file oracle.csv --write-to ORACLE --capture auto` | `applied 1, … failed 0` |
| F5 | F3 again | `would apply 0` |

## G. Other operational writes, through `call`

Only reads and the crosspoint have their own verb so far. Everything else
of the specification can be sent with `call`: put the parameters, in the
order of the specification, in a JSON array in a file.

| # | Method | `args.json` | Read back with |
|---|---|---|---|
| G1 | `SetPortAlias` | `[1, NODE, PORT, "TEST", false]` | `call ORACLE GetPortAlias` with `[1, NODE, PORT, false]` |
| G2 | `SetInputGain` (half dB steps, -36 to 36) | `[1, NODE, PORT, -12]` | `call ORACLE GetInputGain` with `[1, NODE, PORT]` |
| G3 | `SetOutputGain` | `[1, NODE, PORT, -12]` | `call ORACLE GetOutputGain` |
| G4 | `SetLogicSourceState` | `[OBJECTID, true]` | `list logic ORACLE` |

```
.\dhs.exe consumer rrcs call ORACLE SetPortAlias --args-file args.json --write-to ORACLE --capture auto
```

A method whose name starts with `Get` or `Is` needs no `--write-to`.

## What to send back

| # | Item |
|---|---|
| 1 | For each part: what the screen showed |
| 2 | The capture files (`captures\rrcs\ORACLE\`) and the walk snapshot |
| 3 | `watch.txt` |
| 4 | The RRCS log, at least the minutes around each step of part D |
| 5 | The names of the steps that did not behave as written |
