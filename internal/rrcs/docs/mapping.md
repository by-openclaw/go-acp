# RRCS on the shared model — mapping

**Status: proposed, 2026-10-11. Nothing of it is built.** It says how the
rrcs connector is to be rebuilt like the other connectors, before any code
moves. It records decisions taken with the codeowner and marks what is
still open.

## 1. Why

Every dhs connector keeps its logic in `internal/<proto>/consumer` and
plugs into one shared contract (`consumer.Protocol`). The standard verbs,
the alarm evaluator, the health check, the metrics and any server (REST,
WebSocket) work on that contract.

rrcs does not: about 8,000 lines of its logic sit in the command
(`cmd/dhs/cmd_rrcs*.go`), with private copies of `walk`, `get`, `set`,
`watch`, `export`, `import`, `ensure`, and 52 verbs that are one RRCS
method each. A server cannot use any of it. This document is the target
that removes the difference.

## 2. Frame and slots

As acp1, acp2 and RollCall: a device is a frame, a card position is a slot.

| Shared notion | RRCS |
|---|---|
| Device | one Artist node |
| Slot N | bay N of the node |
| Slot state | `present` when a client card is configured in the bay, `no_card` otherwise |
| Slot 0 | the node itself and what belongs to no card: conferences, groups, IFBs, logic sources, the crosspoint matrix |
| Objects of a slot | the ports of the card, with their properties and keys |

A port is addressed by RRCS with a node number of its own (61 to 66 for the
six cards of node 60 on an Artist-1024). RRCS gives no field that links a
port to its card; the link is by position and is made only when the counts
agree (`cardOfNode`).

## 3. Elements

The element types are those of `docs/protocols/elements/`.

| RRCS | Element | Notes |
|---|---|---|
| Node, client card, port, panel, expansion panel, key | Node | the tree Director shows |
| A property of any of them | Parameter | kind, unit, minimum, maximum, default, named values and access come from the property tables of the specification (`rrcsSpecMeta`) |
| The crosspoints | one Matrix: `nToN`, `nonLinear`, dynamic | sources are the ports with an input, targets the ports with an output; on slot 0 |
| Level of a crosspoint | the gain of a connection (`connectionParams`) | single and conference level; read by registration, set with `SetXpVolume` |
| Conference, group, IFB, logic source | Node with Parameters; members as a Parameter | no dedicated element exists |
| Dial, hang up, line status, press a key, send a string, port cloning | Function | arguments in, an answer out, no value left behind |

A property that has a value which stays and can be read back is a
Parameter, even when RRCS sets it with a method of its own: key label, key
marker, key lock, logic source state, gateway state (working / standby).

## 4. Paths

The paths used today stay, so exports, desired-state files and captures
remain valid.

| Thing | Path |
|---|---|
| Node | `net.N.node.N` |
| Client card | `net.N.node.N.card.B` |
| Port | `net.N.node.N.port.P`, with `.in` / `.out` where an input and an output share the number |
| Key | `<port>.key.E.G.K` (expansion, page, key) |
| Virtual function | `<port>.vfunc.TYPE.K` |
| Conference, group, IFB, logic source | `conference.ID`, `group.ID`, `ifb.ID`, `logic.ID` |
| The matrix | `xp` on slot 0; a connection is `xp.<source>><target>` |

## 5. The nine functions of the contract

| Function | For RRCS | Requests |
|---|---|---|
| Connect | open the connection, read version and state | 4 |
| Disconnect | remove the registrations, close | 0 to 2 |
| GetDeviceInfo | node, version, state, number of bays | 1 |
| GetSlotInfo | one bay: its card or none | 1 |
| Walk (slot) | the objects of one card; slot 0: the objects of the node | 1, plus 1 per panel of the card |
| GetValue | one property of one object (`GetPort`, `GetClientCard`, or the list of its kind) | 1 |
| SetValue | one property, read before and after | 3 |
| Subscribe | every event (`RegisterForAllEvents`) and the levels (`RegisterForEventsEx`) | 1 to 3, once |
| Unsubscribe | remove them | 1 to 2 |

The request counts are a requirement, not an estimate: a verb sends what
its answer is made of and no more. `TestRRCSAtomicReads` pins them today
and must keep passing.

## 6. Verbs

| Verb | Kind | What of RRCS goes through it |
|---|---|---|
| `info`, `status`, `health` | standard | node, version, state, Artist connection, bays; registration alive |
| `walk`, `tree` | standard | one card or the node; the tree as Director shows it |
| `get`, `set` | standard | any property |
| `ensure` | standard | one object to a value or a state, with `--check` |
| `watch` | standard | every event; a card inserted or pulled is a slot change |
| `export`, `import`, `extract`, `diff`, `convert`, `validate` | standard | the system as CSV, JSON, YAML; offline tools |
| `alarm` | standard | the alarm template |
| `matrix` | extension, as Ember+ | make, remove, read a crosspoint; its level |
| `invoke` | extension, as Ember+ | the Functions of §3 only |
| raw access | standard, by rule of 2026-10-11 | one RRCS method, raw answer; every connector is to have it |

What goes: the private copies of the standard verbs, the 52 one-method
verbs, `xp` and `set-xp-volume` (into `matrix`), `list` (filters on `walk`
and `tree`), `coverage` (a table in the documentation), and the present
`discover`, which only lists things: the standard `discover` answers
`not_supported`, RRCS has no discovery.

## 7. What must survive

| Thing | Why |
|---|---|
| The write guard: a write names its target a second time | a write stopped a production RRCS twice |
| The refusal of an edit that gives stream fields to a stream in NMOS mode | ADR-0035 |
| The dry run that prints the exact request | it is how each write was tested |
| The request counts of §5 | the footprint requirement |
| The log records: `value_change`, `config_change`, `config_failed`, `xmlrpc` at debug and trace | docs/logging.md |
| Every result seen on a real RRCS | checked again after the move, same input, same output |

## 8. What is not decided

| # | Point | Who |
|---|---|---|
| 1 | The name of the raw-access verb (`call` today; `raw` proposed) | codeowner |
| 2 | Whether the shared `matrix` and `invoke`, marked Ember+ only today, are opened to rrcs as they are or need work in the common part | to check, then codeowner |
| 3 | Where the write guard lives in the shared `set`, which has no such flag | codeowner |
| 4 | `ensure` brings one object to one value in the shared form; the desired-state file with sections (keys, conferences, groups, IFBs, levels, ports, streams) is an rrcs invention. Keep it as an rrcs addition, or loop over objects in the playbook | codeowner |
| 5 | A matrix above the slots (slot 0) has no precedent: Ember+ has no slots | to confirm |
| 6 | Several Artist nodes behind one RRCS: one device per node, or slots numbered across nodes. Needed before the second site | codeowner |

## 9. Order of the move

| Step | Content | Guard against regression |
|---|---|---|
| 1 | `plugin.go` on the shared contract, added beside the existing code | new files only |
| 2 | The logic from the command into `internal/rrcs/consumer`, part by part | the tests of the whole repository before and after each part |
| 3 | The standard verbs in place of the private ones, one verb at a time | the CLI contract test shows every change of the surface |
| 4 | The private copies removed | only when the standard verb gives the same result on the same input |
