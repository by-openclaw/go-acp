# Cerebrum NB consumer — `dhs consumer cerebrum-nb`

Drives the **EVS Cerebrum Northbound API 0v16** (a.k.a. **Neuron Bridge**)
over XML-on-WebSocket. One licence per WebSocket connection;
default port **40007**. (0v13 is the historical baseline; 0v16 is a
superset and is what this connector targets.)

The full element / attribute / enum catalogue is at
[keys.md](keys.md). The full verb + wire-sample reference (including the
0v16 `device-config` verb, the 5-mode `lock`, and `obtain-datastore`) is at
[verbs.md](verbs.md); the operator quick-ref is [runbook.md](runbook.md).
Wire format + quirks live in [../CLAUDE.md](../CLAUDE.md). This page is the
user-facing CLI reference.

---

## Verbs

| Verb | Purpose |
|---|---|
| `connect` | Login + one `<poll/>` and exit (sanity check + redundancy probe) |
| `listen` | Subscribe to all routing / category / salvo / device events; print one line per dispatched frame; Ctrl-C to stop |
| `route` | Issue one or more `<action><routing TYPE='ROUTE'/></action>` — single (`--dest --srce --level`), batch (`--route dst:src:lvl` repeated) or CSV (`--csv FILE`) |
| `list-devices` | One-shot `<obtain><device_change type='LIST'/></obtain>` — table of every device, with `--device-type CLASS` filter and a route-master sentinel synth row when unfiltered or filtered to ROUTER |
| `device-details` / `device-value` | Detail + property snapshots for one device |
| `list-categories` / `category-details` | Category catalogue + per-category items |
| `list-salvo-groups` / `list-salvo-instances` / `salvo-instance-details` | Salvo catalogue snapshots |
| `obtain-datastore` | One-shot `<obtain><datastore_change path='…'/></obtain>` — fetch a Cerebrum data store by relative path (§5.5.1) |
| `keepalive-probe` | Diagnostic — hold the WS open, count keep-alive frames, optional periodic LOGIN |

### Write verbs (§4 ACTION / §4.5 — auto-LOGIN with `--user`/`--pass`)

| Verb | Purpose |
|---|---|
| `lock` / `unlock` | `<action><routing LOCK='…'/></action>` — `--kind SRCE_LOCK\|DEST_LOCK`, `--mode unlocked\|locked\|protected\|locked_path\|protected_path` (the 0v16 §3.2 five-value LOCK enum) |
| `device-config` | `<DEVICE_CONFIGURATION TYPE='ADD\|MODIFY\|REMOVE'/>` — the 0v16 §4.5 device-tree CRUD command, `--device-type generic\|panel\|router\|snmp` |
| `set-mnemonic` | `<action><routing TYPE='*_MNE'/></action>` — set a level / source / dest mnemonic |
| `set-tags` | `<action><routing TYPE='RM_*_TAGS'/></action>` — Routemaster source / dest tags |
| `salvo` | `<action><salvo TYPE='…'/></action>` — `--op run\|save\|rename\|delete` |
| `category` | `<action><category TYPE='…'/></action>` — `--op create\|modify\|delete` |
| `set-value` | `<action><device TYPE='SET_VALUE'/></action>` — write a device object value |

See [verbs.md](verbs.md) for per-verb flags and codec-generated wire
samples.

## Common flags

| Flag | Default | Notes |
|---|---|---|
| `--port N` | `40007` | WebSocket port (configurable in the Cerebrum app) |
| `--user U` | `$DHS_CEREBRUM_USER` | NB username |
| `--pass P` | `$DHS_CEREBRUM_PASS` | NB password |
| `--tls` | off | Use `wss://` instead of `ws://` |
| `--insecure-skip-verify` | off | With `--tls`, skip cert validation |
| `--debug` | off | Verbose RX/TX XML logging |
| `--timeout DUR` | `30s` | Per-request timeout |

Credentials default to environment variables so they don't appear in
shell history or logs. On Windows:

```powershell
$env:DHS_CEREBRUM_USER = 'admin'
$env:DHS_CEREBRUM_PASS = 's3cr3t'
```

## Examples

```bash
# Sanity check + redundancy probe
dhs consumer cerebrum-nb connect 10.6.239.50

# Live event stream (Ctrl-C to stop)
dhs consumer cerebrum-nb listen 10.6.239.50

# Snapshot of every device known to Cerebrum
dhs consumer cerebrum-nb list-devices 10.6.239.50

# Routers only
dhs consumer cerebrum-nb list-devices --device-type Router 10.6.239.50

# Apply a route at the route-master (level 1, dest 60 ← srce 60)
dhs consumer cerebrum-nb route --dest 60 --srce 60 --level 1 10.6.239.50

# Batch routes
dhs consumer cerebrum-nb route --route 60:60:1 --route 61:61:1 10.6.239.50

# Over TLS
dhs consumer cerebrum-nb listen cerebrum.local --tls
```

---

## Install on a Cerebrum host (portable Windows layout)

Cerebrum runs on Windows Server. `dhs.exe` keeps **all** state
(logs, config, captures) in the same directory as the binary — no
`%APPDATA%\dhs\` writes, no UAC drama, no leftovers when you remove it.

### One-time setup

```powershell
# 1. Build the binary on your dev box (from the repo root)
pwsh ./scripts/build-windows.ps1

# 2. Copy bin\ contents to the Cerebrum host
Copy-Item -Recurse bin\* \\cerebrum-host\C$\dhs\

# 3. On the Cerebrum host, set credentials in the user environment
[Environment]::SetEnvironmentVariable('DHS_CEREBRUM_USER', 'admin', 'User')
[Environment]::SetEnvironmentVariable('DHS_CEREBRUM_PASS', 's3cr3t', 'User')

# 4. Run
C:\dhs\dhs.exe consumer cerebrum-nb listen 127.0.0.1
```

### Portable layout

```
C:\dhs\
├── dhs.exe
├── config.yaml          (optional; read at startup)
├── logs\
│   └── dhs.log          (rotated daily; 7 kept)
└── captures\
    ├── pcap\            (only when --capture is set; future feature)
    └── xml\             (only when --debug-xml is set; future feature)
```

The rule: **`--data-dir` defaults to the directory containing
`dhs.exe`** on Windows when no override is given. To opt out, pass
`--data-dir C:\Users\<u>\AppData\Roaming\dhs` explicitly.

> **Do not install under `C:\Program Files`** — UAC blocks writes to
> `Program Files` for non-admin processes; portable layout requires
> write access to the .exe directory. Use `C:\dhs\` or similar.

> **Do not commit `config.yaml` to source control** — it may carry NB
> credentials. Prefer environment variables.

### Logs

`logs\dhs.log` is plain text by default. Pass `--log-format json` to
emit JSON lines for Loki / Promtail (see the root `CLAUDE.md`
"Metrics surface on the producer" section).

---

## Compliance events

Every spec deviation surfaces as a named event. Sample names:

| Event | When |
|---|---|
| `cerebrum_case_normalized` | Peer sent a non-lowercase element / attribute name |
| `cerebrum_busy_received` | Server returned `<busy>` |
| `cerebrum_unknown_notification` | RX root or TYPE not in `keys.md` |
| `cerebrum_mtid_reused` | Same mtid on two in-flight requests |
| `cerebrum_server_inactive` | `poll_reply` reported `CONNECTED_SERVER_ACTIVE='0'` |
| `cerebrum_response_too_large` | RX frame exceeded the 16 MiB cap |
| `cerebrum_nack_<code>` | One per §6 NACK code (0..13) |

Counts available via `Plugin.Compliance().Counts()` — surfaced in
`--debug` mode and via the future metrics endpoint.

---

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| `nack code='NO_LICENCE_AVAILABLE'` | All NB licences in use on the Cerebrum server. Contact EVS. |
| `nack code='SERVER_INACTIVE'` | We're connected to the standby in a redundant pair; reconnect to the active server. |
| Connection refused | Wrong port (default 40007 may be re-mapped) or NB API disabled in the Cerebrum app. |
| `cerebrum_case_normalized` count > 0 | Peer is sending non-UPPERCASE element / attribute names. Wire-actual canonical form is UPPERCASE; decoder accepts either. |

---

## Limitations

- Provider plugin not yet implemented — there is no `dhs producer cerebrum-nb` today.
- TLS root-CA pinning not yet wired; only `--insecure-skip-verify`
  toggles validation.
- `dhs metrics` does not yet surface cerebrum-nb session counters
  (planned).
