# SNMP consumer — the manager

Polls an agent, and receives what the agent sends unasked. Two faces of
one session: SNMP's own verbs, which speak OIDs, and the neutral
connector verbs, which speak paths and know nothing about SNMP.

---

## The two faces

```
dhs consumer snmp get   --oid sysDescr.0        10.6.255.114   # SNMP's own
dhs consumer snmp get   --path system.Descr     10.6.255.114   # neutral
```

`get`, `walk` and `set` exist in both shapes. Whichever flag the
operator named picks the shape: `--oid` / `--limit` is SNMP's own,
`--path` / `--slot` is the neutral one. The neutral face is what
`info`, `tree`, `export`, `watch` and `alarm` use, and it is registered
in the consumer registry like every other connector — so a device
answers those five with no SNMP in the command line at all.

### SNMP's own verbs

| Verb | What it does | Key flags |
|---|---|---|
| `get` | read named objects (default: the RFC 1213 system group) | `--oid a,b,c` |
| `walk` | discover a subtree — GETBULK under v2c, GETNEXT under v1 | `--oid ROOT`, `--limit N` |
| `set` | write one object | `--oid`, `--type i\|s\|o\|a\|u\|t`, `--value` |
| `trap-listen` | receive notifications, in any version | `--bind :1162`, `--community`, v3 USM flags |
| `validate` | decode a captured `frames.jsonl` offline | — |

Shared: `--version 1|2c`, `--community`, `--timeout`, `--retries`,
`--max-repetitions`, `--mib MODULE`.

### Neutral verbs

| Verb | Notes |
|---|---|
| `info` | agent identity — `sysObjectID` as the DTD version, one slot |
| `walk --slot 0` | the device model: the system group plus the agent's own enterprise branch |
| `walk --slot 0 --path A,B` | reads only those branches (`WalkUnder`), it does not read everything and filter |
| `get\|set --path` | one round trip on a cold connection — no walk is needed to resolve a name |
| `tree`, `export` | the walked model, rendered or written out |
| `watch` | polls (see below) |
| `alarm`, `alarm suggest` | judged against a template keyed by device identity |

---

## What the neutral face decides, and why

**Version is not asked for.** `Connect` tries v3 (when a credential is
configured), then v2c, then v1. An agent that speaks only v1 answers a
v2c request with *silence*, which looks exactly like a device that is
down; guessing wrong costs a diagnosis, so it does not guess — it
tries in order and keeps the one that answered.

v3 goes first because v1 and v2c put their password in clear in every
datagram, and a device that offers both should never be polled the
weaker way by accident. With no `SNMP_V3_USER` there is nothing to
offer: v3 authenticates as a user, and there is no anonymous v3.

The Tandberg IRDs in this lab (TT1260, RX1290) are v1-only; the ATEME
DR5000 answers both v1 and v2c, and the difference there is GETBULK —
a v1 walk of it is one object per round trip. `info` reports the
version the session settled on, not a constant.

**One slot.** An agent is a box. Slot 0 is present; nothing else
exists.

**The model is the system group plus the agent's own enterprise
branch** — the one its `sysObjectID` names. Walking all of MIB-2 on a
device with 26 000 objects to reach the six that matter is not a device
model, it is a denial of service on yourself.

**Paths are the MIB's hierarchy, made readable.** Each ancestor
contributes a segment with the parent's shared head trimmed, the
universal head (`org.dod.internet.private.enterprises`, `…mgmt.mib-2`)
dropped, and a table row's index last:

```
dr5000ChannelConfigurationInputSatInterface
  → ateme.dr5000.Channel.Configuration.Input.Sat.Interface
```

The MIB's own name still works as an alias, so `sysLocation`,
`system.Location` and `1.3.6.1.2.1.1.6.0` all resolve to the same
object.

**Values carry their enumeration's word** — `rf1`, `hdsdi`, `true` —
because that is what the manual prints, what an operator says, and what
an alarm rule is written against. A SET takes the same word:

```
$ dhs consumer snmp get 10.6.255.114 --path ateme.dr5000.Status.Input.Sat.Locked
value = "true"  (enum idx 1)
```

**Writes go out on their own session.** An agent's write community is
rarely its read one, and SNMP carries the community in every PDU.

**A watch polls.** Notifications are the agent's choice, not the
manager's — most devices in this plant have a trap destination pointing
at an address that no longer exists — so a watch that waits for traps
watches nothing. `--interval` is the operator's cadence and `--path`
scopes both the poll plan and the walk that builds it. This is ADR-0030
through `pollwatch`, the same machinery mnset uses.

---

## Communities are environment, never flags

```
export SNMP_COMMUNITY=public            # reads
export SNMP_WRITE_COMMUNITY=private     # writes
```

A community on a command line ends up in shell history and in `ps`, so
the neutral verbs read both from the environment, the same way mnset
takes `MNSET_USER` / `MNSET_PASS`. SNMP's own `--community` stays for
one-off reads against a lab agent.

---

## Timeouts and retries

UDP loses datagrams, so a manager that does not retry reports a live
device as down. The defaults are `snmpcons.DefaultRetries` and
`DefaultTimeout`, and the plugin also raises the CLI's operation
timeout floor (`MinOpTimeout`) so a short global `--timeout` cannot cut
a retry cycle in half — an explicit `--timeout` still wins.

---

## Traps

`trap-listen` receives v1 traps, v2c notifications and v3
notifications on one socket. v1 Trap-PDUs are structurally different
from v2c notifications (enterprise / generic-trap / specific-trap
fields versus a plain varbind list), so they are two decoders, not one
with a flag; the listener prints both in one shape.

```
dhs consumer snmp trap-listen --bind :1162
dhs consumer snmp trap-listen --bind :1162 --community public
dhs consumer snmp trap-listen --bind :1162 --user operator \
    --auth sha256 --auth-pass '…' --priv aes --priv-pass '…'
```

---

## v3

```
export SNMP_V3_USER=operator SNMP_V3_AUTH=sha256 SNMP_V3_PRIV=aes
export SNMP_V3_AUTH_PASS=… SNMP_V3_PRIV_PASS=…

dhs consumer snmp get --version 3 --oid sysDescr.0 10.6.255.114
dhs consumer snmp info 10.6.255.114     # the neutral verbs, same credential
```

v3 authenticates as a **user**, not with a community, and a manager is
authoritative for nothing — so before it can sign a single GET it asks
the agent who it is (RFC 3414 §4 discovery) and localises the user's
keys onto the answer. All three security levels work: noAuthNoPriv,
authNoPriv, authPriv.

It re-discovers by itself. An agent that reboots gets a new boot
count, every message keyed on the old one falls outside its time
window, and the agent says so with a Report — so the session
re-discovers and retries, **without spending a retry**, because that
datagram was not lost, it was answered. A manager that gave up there
would report a live agent as down for as long as it stayed up.

And when a credential is wrong it says which one:

```
error: snmp: the agent refused the message: the authentication password
is wrong, or the protocol is (usmStatsWrongDigests)
```

That line is the whole reason the counters matter — a wrong password
and a dead device are the same silence otherwise.

With a credential configured, the neutral verbs try **v3 first**, then
v2c, then v1.

## Informs

`trap-listen` receives InformRequests as well as traps and
**acknowledges** them — from the socket they arrived on, before the
handler runs, and sealed as the same user when they came in over v3. A
receiver that read informs and stayed silent would leave every sender
retrying the same alarm until it gave up.

An inform shows in the output as `[inform, acknowledged]`. One that
keeps arriving means the sender is not seeing the acknowledgements,
which is a route problem and not a device one.
