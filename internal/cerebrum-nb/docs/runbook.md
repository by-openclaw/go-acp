# Cerebrum NB — operational runbook

Quick-reference card for operators. For wire-format detail see
[../CLAUDE.md](../CLAUDE.md); for in-depth CLI behaviour see
[consumer.md](consumer.md); for the full verb + sample reference see
[verbs.md](verbs.md); for why there is no provider see
[provider.md](provider.md).

Authoritative spec: **EVS Cerebrum Northbound API 0v16**
([../assets/Cerebrum Northbound API 0v16.pdf](../assets/Cerebrum%20Northbound%20API%200v16.pdf)).
0v13 is the historical baseline.

---

## Transport matrix

| Mode | Scheme | Port | Notes |
|---|---|---:|---|
| WebSocket (plain) | `ws://host:port` | 40007 | One XML document per WS text message (UTF-8). No URL path. |
| WebSocket (TLS) | `wss://host:port` | 40007 | `--tls`; `--insecure-skip-verify` to skip cert validation. |

- **One northbound licence per active WebSocket session.** If the licence
  is exhausted (or missing entirely, as on the current lab Cerebrum) the
  server refuses or drops the session — this is the single most common
  field failure.
- Port is configurable in the Cerebrum app; `--port` overrides the 40007
  default.

## Credentials

Pass `--user` / `--pass`, or set the environment (preferred — keeps secrets
out of shell history and logs):

```powershell
$env:DHS_CEREBRUM_USER = 'admin'
$env:DHS_CEREBRUM_PASS = 's3cr3t'
```

LOGIN is sent automatically when `--user`/`--pass` (or the env vars) are
set. Write verbs (`route`, `lock`, `device-config`, …) require an
authenticated session.

## Verb reference (consumer-only)

There is **no provider** — Cerebrum is a northbound API we consume, not a
device we serve (see [provider.md](provider.md)).

### Read / monitor

| Verb | What it does | Common flags |
|---|---|---|
| `dhs consumer cerebrum-nb connect <host>` | LOGIN + one POLL, then exit (sanity + redundancy probe) | `--user --pass --port --timeout` |
| `dhs consumer cerebrum-nb listen <host>` | Subscribe to routing / category / salvo / device events; one line per frame; Ctrl-C to stop | `--user --pass` |
| `dhs consumer cerebrum-nb list-devices <host>` | Snapshot of every device | `--device-type Router\|SNMP\|Device` |
| `dhs consumer cerebrum-nb device-details <host>` | One device's metadata | `--device IP --device-type T` |
| `dhs consumer cerebrum-nb device-value <host>` | One device object value | `--device NAME --by-name --sub-device X --object Y` |
| `dhs consumer cerebrum-nb list-categories <host>` | Category catalogue | — |
| `dhs consumer cerebrum-nb category-details <host>` | Items in one category | `--category NAME` |
| `dhs consumer cerebrum-nb list-salvo-groups <host>` | Salvo groups | — |
| `dhs consumer cerebrum-nb list-salvo-instances <host>` | Instances in a group | `--group NAME` |
| `dhs consumer cerebrum-nb salvo-instance-details <host>` | One instance's detail | `--group NAME --instance NAME` |
| `dhs consumer cerebrum-nb obtain-datastore <host>` | Fetch a data store by path | `--name PATH` |
| `dhs consumer cerebrum-nb keepalive-probe <host>` | Diagnostic — hold WS open, watch keep-alives | `--idle DUR --send-login` |

### Write / control (auth required)

| Verb | What it does | Common flags |
|---|---|---|
| `route` | Apply one or more routes | `--dest --srce --level`, `--route dst:src:lvl`, `--csv FILE` |
| `lock` / `unlock` | Lock / unlock a source or dest | `--kind SRCE_LOCK\|DEST_LOCK`, `--mode` (5-value enum), `--level`, `--duration` |
| `device-config` | Add / modify / remove a device in the Cerebrum tree (0v16 §4.5) | `add\|modify\|remove --device-type generic\|panel\|router\|snmp --ip IP …` |
| `set-mnemonic` | Set a level / source / dest mnemonic | `--kind LEVEL_MNE\|SRCE_MNE\|DEST_MNE --mnemonic TXT --level [--alt SLOT]` |
| `set-tags` | Set Routemaster source / dest tags | `--kind RM_SRCE_TAGS\|RM_DEST_TAGS --tags a,b,c` |
| `salvo` | Run / save / rename / set description / delete a salvo — ENSURE (ADR-0007): description/rename/delete read live state first (already converged = `changed:false`, nothing sent); run/save are events (always fire) | `--op run\|save\|rename\|description\|delete --group [--instance --new-name --description] [--check] [--output json]` |
| `category` | Create / modify / delete a category | `--op create\|modify\|delete --category` |
| `set-value` | Write a device object value | `--device --sub-device --object --value` |

## Logging

- **Logs go to stderr, data to stdout** — `2>run.log 1>out.txt` separates them.
- `--debug` turns on verbose RX/TX XML logging.

## Lock modes (0v16 §3.2 — five-value enum)

`--mode` accepts the canonical 0v16 LOCK_STATE values:
`unlocked` · `locked` · `protected` · `locked_path` · `protected_path`.
The pre-0v13 `PROTECT` / `RELEASE` verbs survive only as deprecated action
arguments (see [../CLAUDE.md](../CLAUDE.md) and `codec/actions.go`).

## Data store PATH vs NAME (confirm vs live capture)

§5.5.1 of the spec is ambiguous: the worked TX example uses `PATH=…` but
the attribute table names the field `NAME`. The codec **emits `PATH`**
(matching the only concrete example) and **accepts both** on decode. When
an NB licence is available, confirm which the live server expects and
tighten if needed. See [verbs.md](verbs.md) §5 and `codec/events.go`
(`DatastoreChange`).

## Common failures

| Symptom | Likely cause | Fix |
|---|---|---|
| Session refused / dropped immediately | NB licence missing or exhausted | Enable / free a northbound licence on the Cerebrum |
| `NACK` with `MTID_ERROR` on a write | lowercase keys sent (some impls reject) | the encoder emits UPPERCASE by default — do not hand-craft lowercase |
| `connect` hangs | wrong `--port` (configurable in Cerebrum) | confirm the app's NB port |
| TLS handshake fails | self-signed cert | `--tls --insecure-skip-verify` (lab only) |

## See also

- [README.md](README.md) · [consumer.md](consumer.md) · [verbs.md](verbs.md) · [provider.md](provider.md) · [keys.md](keys.md)
- [../CLAUDE.md](../CLAUDE.md) — wire layer, mtid, quirks
- [../testdata/fixtures/README.md](../testdata/fixtures/README.md) — codec-generated wire samples
- [../../../ansible/playbooks/cerebrum-nb-integration.yml](../../../ansible/playbooks/cerebrum-nb-integration.yml) — live-peer verb play (gated on licence)
