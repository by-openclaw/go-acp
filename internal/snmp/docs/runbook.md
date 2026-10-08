# SNMP — operational runbook

Quick-reference card. Wire format and scope are in
[../CLAUDE.md](../CLAUDE.md); behaviour in [consumer.md](consumer.md)
and [provider.md](provider.md). Device addresses, communities and
identities are in [`docs/testbed.md`](../../../docs/testbed.md) — not
restated here, per ADR-0015.

---

## Before anything

```
export SNMP_COMMUNITY=public            # v1/v2c reads
export SNMP_WRITE_COMMUNITY=private     # v1/v2c writes, only when you mean it

export SNMP_V3_USER=operator            # v3, which has no community
export SNMP_V3_AUTH=sha256 SNMP_V3_AUTH_PASS=…
export SNMP_V3_PRIV=aes    SNMP_V3_PRIV_PASS=…
```

All of them come from the environment so they stay out of shell
history and out of `ps`.

**Setting `SNMP_V3_USER` changes what gets tried first.** The neutral
verbs then attempt v3 before v2c and v1, because those two put their
password in clear in every datagram. Unset it and nothing v3 happens:
there is no anonymous v3.

## Is it there?

```
dhs consumer snmp info 10.6.255.114
```

```
device       10.6.255.114:161
protocol     snmp v1
dtd_version  1.3.6.1.4.1.27338.5.2.2
slots        1
```

`dtd_version` is the agent's `sysObjectID` — the vendor and product,
which is what tells you whose MIB applies.

## It does not answer

Work down this list before concluding the device is down.

| Symptom | Likely cause | What to do |
|---|---|---|
| Silence on every request | the agent is v1-only and something asked v2c | the neutral verbs already try v2c then v1; for SNMP's own verbs add `--version 1` |
| Silence, intermittently | a lost datagram, on UDP | `--retries` (a manager with no retry reports a live device as down) |
| `context deadline exceeded` | the CLI's operation timeout is shorter than one retry cycle | leave `--timeout` off — the plugin raises the floor itself — or set it high enough for `retries × timeout` |
| Answers `get`, hangs on `walk` | you are walking 26 000 objects one round trip at a time | scope it: `walk --slot 0 --path <branch>` |
| `noAccess` on a SET | the read community was used | `SNMP_WRITE_COMMUNITY`; the agent is behaving correctly |
| `usmStatsUnknownUserNames` | this agent has no such v3 user | check `SNMP_V3_USER` against the agent's own USM table |
| `usmStatsWrongDigests` | wrong auth password, or wrong protocol | `SNMP_V3_AUTH` / `SNMP_V3_AUTH_PASS` |
| `usmStatsDecryptionErrors` | wrong privacy password or cipher | `SNMP_V3_PRIV` / `SNMP_V3_PRIV_PASS` |
| `usmStatsNotInTimeWindows` | the agent rebooted under the session | nothing — the manager re-discovers and retries by itself; frequent means something is resetting |
| Nothing at 161 | the agent is elsewhere | `10.6.250.5:1161` — Cerebrum's own agent is one |

## Read something

```
# SNMP's own shape — OIDs and MIB names
dhs consumer snmp get --version 1 --oid sysDescr.0,sysUpTime.0 10.6.255.114

# the neutral shape — the paths a walk produced, resolved cold
dhs consumer snmp get 10.6.255.114 --path ateme.dr5000.Status.Input.Sat.Locked
```

## Walk, without hurting yourself

```
# the branches you care about — seconds
dhs consumer snmp walk 10.6.255.114 --slot 0 \
    --path system,ateme.dr5000.Status.Input

# the whole device model — minutes on a v1 IRD, and it writes the DM cache
dhs consumer snmp walk 10.6.255.114 --slot 0
```

A scoped walk reads only those branches and deliberately does **not**
write the device model cache: a handful of leaves is not a model.

The protocol verb walks OIDs rather than paths, starts at the whole tree
(`1.3.6.1`) and has no object limit; flags go on either side of the host:

```
dhs consumer snmp walk 10.6.255.114                          # everything the agent has
dhs consumer snmp walk 10.6.255.114 --oid 1.3.6.1.4.1 --limit 500
```

**The DR5000's whole tree is 381 079 objects and takes twelve hours**
(measured 2026-10-08, v0.42.0; net-snmp 5.9.3 walks it at the same
pace). 327 680 of them are one table, the DVB subtitle services
(`dr5000StatusTsDescriptorProgramStreamDvbSubService…`): the agent
returns a row for every possible program x stream x service index
(128 x 32 x 16) whether or not the service exists — one row carried real
data — and answers more slowly the deeper into the table it is asked,
down to ten objects a second. It also answers a GET for an index outside
the table (program 999) with a value instead of noSuchInstance. Everything
else on the device, about 53 000 objects, walks in half an hour.

A request the agent has not answered is repeated, each time waiting twice
as long as before (2 s, 4 s, 8 s): the agent answered late 8 079 times in
that walk and none of them stopped it. With three equal waits the walk
died at object 100 750.

## Write something

```
export SNMP_WRITE_COMMUNITY=private
dhs consumer snmp set 10.6.255.114 \
    --path ateme.dr5000.Channel.Configuration.Output.Mapping.Connector1 \
    --value hdsdi
```

On live kit, write back the value the object already holds unless you
intend to change air. The integration suite's only write does exactly
that, and asserts it came back unchanged.

## Alarms

An alarm template is keyed by device identity (`DR5000@1.3.1.1`), and
every object in the model is in the view — the ones no rule covers read
`info` rather than being absent.

```
# what the template covers
dhs consumer snmp alarm list --template internal/snmp/alarm/DR5000@1.3.1.1.json

# judge one value against it, offline
dhs consumer snmp alarm test --template internal/snmp/alarm/DR5000@1.3.1.1.json \
    --path ateme.dr5000.Status.Input.Sat.Locked --value false

# draft rules from what the device itself declares, then read them
dhs consumer snmp alarm suggest 10.6.255.114 \
    --path ateme.dr5000.Status.Input --include-writable --out draft.json

# judge the live device, continuously, with Prometheus on the side
dhs consumer snmp watch 10.6.255.114 --path ateme.dr5000.Status.Input \
    --interval 30s --metrics-addr :9112 \
    --alarm internal/snmp/alarm/DR5000@1.3.1.1.json
```

Without `--alarm`, `watch` resolves the template from the DM identity
under `.cache/alarm/snmp/`, and falls back to the built-in
everything-is-info template so no object is ever invisible.

`--include-writable` is needed on the DR5000 because its MIB marks
*status* objects `read-write`; a tool that reads "writable" as "a
setting" would skip the whole Status branch.

Deployment is Ansible, never by hand — add the device to
`dhs_alarm_devices` in
[`ansible/inventory/group_vars/all.yml`](../../../ansible/inventory/group_vars/all.yml)
and run:

```
ansible-playbook -i inventory/hosts.ini playbooks/alarm.yml         # deploy
ansible-playbook -i inventory/hosts.ini playbooks/alarm.yml         # changed=0
ansible-playbook -i inventory/hosts.ini playbooks/alarm-verify.yml  # prove it
```

Navigating what comes out — Prometheus, Loki and Grafana all keyed by
the same `device` label — is
[`docs/deployment/grafana/navigation.md`](../../../docs/deployment/grafana/navigation.md).

## Be an agent

```
dhs producer snmp serve --bind 0.0.0.0:1161 --location "TEC RACK 23"
dhs producer snmp mib --out DHS-MIB.mib          # load this into the manager
dhs producer snmp trap --to 10.6.250.5/2c/public # prove the receiver first
dhs producer snmp inform --to 10.6.250.5/2c/public  # ...and be told it arrived
```

That agent answers **v3 out of the box**, as user `dhs`, alongside
v1/v2c — the startup log says which security level the user ended up
at, and with no passphrase it is `noAuthNoPriv`:

```
dhs producer snmp serve --bind 0.0.0.0:1161 \
    --v3-user operator --v3-auth sha256 --v3-priv aes
    # passwords from SNMP_V3_AUTH_PASS / SNMP_V3_PRIV_PASS
```

`--v3-user=""` turns v3 off and leaves v1/v2c, which means a community
in clear on every datagram.

SET is refused until `--write-community` is set. That is deliberate.

## Integration tests

Tier 2/3, against a real agent — never against our own provider.

```
cd ansible
ansible-playbook -i inventory/hosts.ini playbooks/snmp-integration.yml
ansible-playbook -i inventory/hosts.ini playbooks/snmp-integration.yml  # changed=0
```

The play cross-compiles where there is a Go toolchain and ships the
binaries (`DHS_BIN`), because the control node has none.

The verbs themselves, with the released binary, against the DR5000 —
info, health, get, walk, a captured walk and its `validate`, and a `set`
of the unit name with the write community, read back and put back:

```
ansible-playbook -i inventory/hosts.ini playbooks/snmp-dr5000-verify.yml
ansible-playbook -i inventory/hosts.ini playbooks/snmp-dr5000-verify.yml -e snmp_full_walk=true   # the twelve hours
```

The Go suite, direct:

```
SNMP_TEST_HOST=10.6.255.114 SNMP_WRITE_COMMUNITY=private \
  go test -tags integration ./internal/snmp/integration/ -v
```

| Variable | What |
|---|---|
| `SNMP_TEST_HOST` | the agent; unset skips the whole suite |
| `SNMP_WRITE_COMMUNITY` | unset skips the write checks only |
| `SNMP_WRITABLE_PATH` | the object written back to itself |
| `SNMP_WATCH_PATH` | the branch `watch` polls |
| `SNMP_ALARM_TEMPLATE` | the template the alarm check judges against |
| `DHS_BIN` | a prebuilt CLI, for hosts with no toolchain |

## Look at the wire

Every datagram of a session, as the manager sent and received it, and
the same file read back offline (what the agent refused, and any answer
no request in the file asked for):

```
dhs consumer snmp walk 10.6.255.114 --oid system --capture frames.jsonl
dhs consumer snmp validate frames.jsonl
```

Or from outside, with a packet capture:

```
tshark -i any -Y snmp -O snmp
```

Capture **unfiltered** and select afterwards: a BPF `udp port 161` on a
cooked `-i any` capture matches nothing. Our dissector and how it was
verified against this device's own frames are in
[../wireshark/README.md](../wireshark/README.md); replayable captures
are under [../testdata/](../testdata).

## Recompile the MIB tables

Offline, reviewed in a PR — never at runtime.

```
go run ./tools/mibc -out internal/snmp/mib/tables.tsv.gz \
    <by-protocol/mib>/ird <by-protocol/mib>/standard \
    internal/snell-rollcall/assets/Protocol/SNMP/SNMP_MIBs \
    internal/snmp/mib
```

`-v` lists duplicate modules and which copy won, and names that did not
resolve. Those are findings, not failures. A device that serves its own
MIB (the DR5000 does, over HTTP) is the copy to trust.
