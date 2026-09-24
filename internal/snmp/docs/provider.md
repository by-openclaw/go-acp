# SNMP provider — the agent

Be an agent: serve *our* MIB, answer someone else's manager, emit our
own notifications. Same idea as the ACP1 and Ember+ providers serving
their trees — a device we present, not a mock.

```
dhs producer snmp serve   answer polls against a served MIB
dhs producer snmp trap    send one notification to one or more receivers
dhs producer snmp mib     write DHS-MIB, the module that names all of it
```

`status` (`--url`), `stop` (`--pidfile`) and `ensure`
(`--state present|absent`) are the canonical lifecycle verbs, per
ADR-0002 and ADR-0007.

---

## serve

```
# an agent on a high port, which needs no privilege
dhs producer snmp serve --bind 0.0.0.0:1161 --location "TEC RACK 23"

# writable, deliberately
dhs producer snmp serve --bind 0.0.0.0:1161 --write-community private
```

| Flag | Meaning |
|---|---|
| `--bind` | default `0.0.0.0:161` |
| `--read-community` | admits GET, GETNEXT and GETBULK (default `public`) |
| `--write-community` | **empty refuses every SET** — writability is opt-in, not a default |
| `--descr` `--name` `--contact` `--location` | the system group's four writable-by-configuration strings |
| `--metrics-addr` | serves `/snmp.json` and `/snapshot.json` |
| `--pidfile` | so `stop` / `ensure` can manage this instance |
| `--v3-user` `--v3-auth` `--v3-auth-pass` `--v3-priv` `--v3-priv-pass` | USM: md5/sha/sha224/sha256/sha384/sha512, des/aes |
| `--engine-id` `--engine-boots` | RFC 3411 identity. **Boots must be persisted and incremented across restarts**, or the agent accepts messages recorded before its last reboot |

One socket, one read loop, each datagram dispatched inline: SNMP
requests are small and every answer comes out of an in-process tree, so
a serial handler is well inside budget — and it keeps responses in the
order the requests arrived, which a manager correlating by request-id
does not need but an operator reading a capture very much wants.

`SO_REUSEADDR` is set, because an agent and a manager frequently share
a host in this lab and something else may already hold 161.

---

## The MIB we serve

**DHS-MIB**, under BY-SYSTEMS SPRL's IANA Private Enterprise Number
**54981**. The module is written by `dhs producer snmp mib` from the
same tree the agent serves, so the module a manager loads cannot drift
from what the agent answers — `TestDHSMIBIsCurrent` fails the build if
someone changes one without the other.

| Arc | Name | What |
|---|---|---|
| `54981.1` | dhsProducts | |
| `54981.1.1` | dhsAgent | this agent's `sysObjectID`, and its v1 trap enterprise |
| `54981.1.1.0.n` | dhsAgentNotifications | so the RFC 3584 v1→v2c mapping lands on a defined name |
| `54981.2` | dhsMIB | objects at `.2.1`, conformance at `.2.2` |

A published arc is never reused. A changed definition is a new
REVISION in `provider/dhsmib.go`.

```
dhs producer snmp mib --out DHS-MIB.mib
```

---

## trap

```
dhs producer snmp trap --to 10.6.250.5/2c/public
dhs producer snmp trap --to 10.6.255.9:162/1/public,10.6.250.5/2c/public
dhs producer snmp trap --to 10.6.250.7/3/operator \
    --user operator --auth sha256 --auth-pass '…' --priv aes --priv-pass '…'
```

A destination is `ADDR[:PORT][/VERSION[/COMMUNITY-OR-USER]]`, so one
command proves receivers that speak different versions.

v1 traps carry enterprise / generic-trap / specific-trap /
agent-address; v2c and v3 carry a varbind list with
`sysUpTime.0` and `snmpTrapOID.0` first. Two encoders, not one with a
flag. `--generic 6` means "look at `--specific`"; the default
`--specific 1` with the default `--enterprise` is `dhsTestNotification`.

`--agent-addr` is the v1 agent-address field and defaults to this
host's outbound address. v2c and v3 have no such field.

> Every trap destination configured on the devices in
> [`docs/testbed.md`](../../../docs/testbed.md) currently points at an
> address that no longer exists, so they emit to nobody. `trap` is how
> a receiver is proven before anything depends on it — and it is why
> the consumer's `watch` polls rather than waiting.

---

## v3

USM is ours (`internal/snmp/usm`): RFC 3414 key derivation asserted
against the published test vectors, RFC 7860 for the SHA-2 family, RFC
3826 for AES. Both directions of notification work. v3 *polling* is
still open — a manager is authoritative for nothing, so it must
discover the agent's engine first.
