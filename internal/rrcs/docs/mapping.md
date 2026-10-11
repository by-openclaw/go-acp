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

**RRCS is not modelled as an Ember+ tree.** Rule of the codeowner,
2026-10-11. The element types of `docs/protocols/elements/` are used as
words for what a thing is (a value, a matrix, a function); the tree with
numbered positions is not.

| RRCS | What it is | Notes |
|---|---|---|
| Node, client card, port, panel, expansion panel, key | a fixed object | exists as long as the configuration has it; addressed by what RRCS addresses it with |
| Conference, group, IFB | a dynamic object | created, changed and deleted at run time, by us or by anyone else |
| Logic source | a fixed object with a state | |
| A property of any of them | Parameter | kind, unit, minimum, maximum, default, named values and access come from the property tables of the specification (`rrcsSpecMeta`) |
| The crosspoints | one matrix, n:n | any input port to any output port, several at once; addressed by the two ports, not by row and column numbers |
| Level of a crosspoint | the gain of a connection | single and conference level; read by registration, set with `SetXpVolume` |
| Dial, hang up, line status, press a key, send a string, port cloning | Function | arguments in, an answer out, no value left behind |

### 3.1 Identity

| Rule | Why |
|---|---|
| The connector gives no numeric OID. The `OID` field of the contract stays empty; the contract allows it for a protocol that is already addressed by identifiers | a number per position cannot follow objects that come and go |
| A fixed object is identified by the address RRCS uses: net, node, port, direction; key position | it does not change while the configuration stands |
| A dynamic object is identified by its name. The `ObjectID` RRCS gives at creation is reported as a property and used inside one request, never kept as a key | delete a conference and create it again: same name, other metadata, and the `ObjectID` is whatever RRCS gives then |
| A dynamic object that appears or disappears is an event of its own (`created`, `deleted`), not a change of value | a client must not have to compare two walks to learn it |

A gateway from RRCS to Ember+, if one is built, owns the numbering: it
keeps the table from name to Ember+ number, keeps a number reserved when
the object goes, and gives the same number back when the name returns.
That table is the gateway's, not the connector's. Nothing here must make
it harder: no number invented by the connector may leak into a path, an
export or an event.

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
| Conference, group, IFB | by name: `conference.<name>`, `group.<name>`, `ifb.<name>`. The form with the `ObjectID` used today is still read, for the files that exist |
| Logic source | `logic.ID` |
| The matrix | `xp`; a connection is `xp.<source port>><target port>` |

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
| `matrix` | extension | make, remove, read a crosspoint; its level. Same verb name as Ember+, rrcs code of its own |
| `invoke` | extension | the Functions of §3 only. Same verb name as Ember+, rrcs code of its own |
| `raw` | standard, by rule of 2026-10-11 | one RRCS method, raw answer; every connector is to have it |

The shared `matrix` and `invoke` commands are written for the Ember+
plugin alone (`cmd_matrix.go` and `cmd_invoke.go` take the plugin as
`*emberplus.Plugin` and work on tree paths). rrcs does not go through that
code. How the two share one verb name without rrcs depending on the Ember+
tree is a change in the common part and needs a decision of its own.

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

| # | Point | State |
|---|---|---|
| 1 | The name of the raw-access verb | decided 2026-10-11: `raw` |
| 2 | The matrix and the dynamic objects | decided 2026-10-11: n:n matrix; no Ember+ tree, no numeric OID, identity as in §3.1 |
| 3 | The write guard once `set` goes through the shared contract | to test on a real RRCS: the same writes as proven today, the request compared field by field with the one sent now. Until that passes, the present `set` stays |
| 4 | Two ways to ask for a wanted state. One object per command (the shared `ensure`; the playbook loops over objects, one connection each). Or one file for a whole panel (the rrcs `ensure --file` of today; one connection, the lists read once). Proposed: keep both, the file for anything above a few objects because it sends fewer requests | open |
| 5 | The matrix on slot 0 | closed by point 2: the matrix is addressed by ports, not by a place in a tree |
| 6 | A plant with several Artist nodes behind one RRCS. Today: one node (60). The paths already carry the node (`net.N.node.N`), so nothing breaks; what is not defined is how bays of different nodes are numbered as slots. Proposed: decide when such a plant is to be connected, not before | parked |
| 7 | How `matrix` and `invoke` share a verb name with Ember+ (§6) | open, needs a go before the common part is touched |

## 9. Order of the move

| Step | Content | Guard against regression |
|---|---|---|
| 1 | `plugin.go` on the shared contract, added beside the existing code | new files only |
| 2 | The logic from the command into `internal/rrcs/consumer`, part by part | the tests of the whole repository before and after each part |
| 3 | The standard verbs in place of the private ones, one verb at a time | the CLI contract test shows every change of the surface |
| 4 | The private copies removed | only when the standard verb gives the same result on the same input |
