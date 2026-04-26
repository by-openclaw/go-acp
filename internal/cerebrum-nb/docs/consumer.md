# Cerebrum NB consumer — `dhs consumer cerebrum-nb`

Drives the **EVS Cerebrum Northbound API v0.13** (a.k.a. **Neuron Bridge**)
over XML-on-WebSocket. One licence per WebSocket connection;
default port **40007**.

The full element / attribute / enum catalogue is at
[keys.md](keys.md). Wire format + quirks live in
[../CLAUDE.md](../CLAUDE.md). This page is the user-facing CLI reference.

---

## Verbs

| Verb | Purpose |
|---|---|
| `connect` | Login + one `<poll/>` and exit (sanity check + redundancy probe) |
| `listen` | Subscribe to all routing / category / salvo / device events; print one line per dispatched frame; Ctrl-C to stop |
| `list-devices` | One-shot `<obtain><device_change type='LIST'/></obtain>` — table of every device |
| `list-routers` | Same as `list-devices`, filter `device_type='Router'` |
| `walk` | One-shot obtain across DEVICE_LIST + CATEGORY_LIST + GROUP_LIST — counts + entries |

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
dhs consumer cerebrum-nb list-routers 10.6.239.50

# Devices + categories + salvos in one shot
dhs consumer cerebrum-nb walk 10.6.239.50 --timeout 60s

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
