# Wireshark Dissectors for ACP1, ACP2, Ember+, OSC, and Probel SW-P-08

This repository ships Lua dissectors so you can inspect live or captured
traffic from the CLI (`dhs consumer <proto> walk`, `dhs producer serve`, ...)
in Wireshark without having to decode frames by hand. Every dhs plugin
carries a from-scratch dissector — we never delegate to Wireshark
built-ins, so `Protocol | Info` shape stays consistent across protocols.

All files follow the `dhs_<proto>` naming convention (file / Proto /
field-abbrev prefix) so display filters and Wireshark namespaces don't
clash with upstream built-ins.

| Protocol       | Transport                               | Default ports                         | Lua file                                                                                              | Display filter      |
|----------------|-----------------------------------------|---------------------------------------|-------------------------------------------------------------------------------------------------------|---------------------|
| ACP1           | UDP / TCP direct                        | 2071                                  | [`internal/acp1/wireshark/dhs_acpv1.lua`](../internal/acp1/wireshark/dhs_acpv1.lua)                   | `dhs_acpv1`         |
| ACP2           | AN2 over TCP                            | 2072                                  | [`internal/acp2/wireshark/dhs_acpv2.lua`](../internal/acp2/wireshark/dhs_acpv2.lua)                   | `dhs_acpv2`         |
| Ember+         | S101 over TCP                           | 9000 / 9090 / 9092                    | [`internal/emberplus/wireshark/dhs_emberplus.lua`](../internal/emberplus/wireshark/dhs_emberplus.lua) | `dhs_emberplus`     |
| OSC 1.0 + 1.1  | UDP + TCP length-prefix + TCP SLIP      | 8000 UDP/TCP-LP, 8001 TCP-SLIP        | [`internal/osc/wireshark/dhs_osc.lua`](../internal/osc/wireshark/dhs_osc.lua)                         | `dhs_osc`           |
| Probel SW-P-08 | TCP                                     | 2008                                  | [`internal/probel-sw08p/wireshark/dhs_probel_sw08p.lua`](../internal/probel-sw08p/wireshark/dhs_probel_sw08p.lua) | `dhs_probel_sw08p`  |

All target **Wireshark 4.x** (Lua 5.2+). They install the same way.

---

## 1. Locate your personal plugins directory

Open Wireshark and go to **Help → About Wireshark → Folders**. Look for the
**Personal Lua Plugins** row. Copy the path. Typical values:

| OS       | Default path                                                      |
|----------|-------------------------------------------------------------------|
| Windows  | `%APPDATA%\Wireshark\plugins`                                     |
| macOS    | `~/.config/wireshark/plugins`                                     |
| Linux    | `~/.local/lib/wireshark/plugins` or `~/.config/wireshark/plugins` |

If the folder does not exist, create it.

---

## 2. Copy the dissectors into that directory

From the repo root:

```bash
# Linux / macOS
cp internal/acp1/wireshark/dhs_acpv1.lua              "$HOME/.local/lib/wireshark/plugins/"
cp internal/acp2/wireshark/dhs_acpv2.lua              "$HOME/.local/lib/wireshark/plugins/"
cp internal/emberplus/wireshark/dhs_emberplus.lua     "$HOME/.local/lib/wireshark/plugins/"
cp internal/osc/wireshark/dhs_osc.lua                 "$HOME/.local/lib/wireshark/plugins/"
cp internal/probel-sw08p/wireshark/dhs_probel_sw08p.lua "$HOME/.local/lib/wireshark/plugins/"
```

```powershell
# Windows PowerShell
Copy-Item internal/acp1/wireshark/dhs_acpv1.lua              $env:APPDATA\Wireshark\plugins\
Copy-Item internal/acp2/wireshark/dhs_acpv2.lua              $env:APPDATA\Wireshark\plugins\
Copy-Item internal/emberplus/wireshark/dhs_emberplus.lua     $env:APPDATA\Wireshark\plugins\
Copy-Item internal/osc/wireshark/dhs_osc.lua                 $env:APPDATA\Wireshark\plugins\
Copy-Item internal/probel-sw08p/wireshark/dhs_probel_sw08p.lua $env:APPDATA\Wireshark\plugins\
```

Any `.lua` file dropped into that folder is auto-loaded on Wireshark start.

---

## 3. Enable Lua plugins (first time only)

Lua is enabled by default in official Wireshark builds. If you installed via a
distro package that ships a `console.lua` stub, verify in **Edit → Preferences
→ Protocols → Lua** that *Disable Lua scripts* is unchecked.

On Windows the installer asks whether to enable Lua during setup. If you
unchecked it, run the installer again and enable it.

---

## 4. Restart Wireshark

Close and re-open Wireshark. On the start screen open
**Analyze → Enabled Protocols** and verify the dissectors are listed:

- `dhs_acpv1` — ACP1
- `dhs_acpv2` / `dhs_acpv2_an2` / `dhs_acpv2_prop` — ACP2 + AN2 transport + property sub-tree
- `dhs_emberplus` / `dhs_emberplus_glow` — Ember+ S101 + Glow BER sub-tree
- `dhs_osc` — OSC 1.0 + 1.1 (all three transports: UDP + TCP length-prefix + TCP SLIP)
- `dhs_probel_sw08p` — Probel SW-P-08/88

If one is missing, check **View → Reload Lua Plugins** (Ctrl-Shift-L) — the
status bar reports Lua errors you can then copy from **View → Lua →
Evaluate…** or the Lua console.

---

## 5. Capture and filter

### Live capture with a CLI walk

Start Wireshark on the network interface that reaches your device, press
**Capture Start**, then run the walk in another terminal. The dissectors pick
up the default ports automatically.

```bash
# ACP1 (Synapse Simulator)
dhs consumer acp1 walk 10.6.239.113 --slot 0 --capture out/acp1/

# ACP2 (Convert Hybrid VM)
dhs consumer acp2 walk 10.41.40.195 --slot 0 --capture out/acp2/

# Ember+ (TinyEmberPlus, port 9092)
dhs consumer emberplus walk localhost:9092 --capture out/emberplus/
```

### Display filters

| Want to see                  | Filter                                                    |
|------------------------------|-----------------------------------------------------------|
| All ACP1 traffic             | `dhs_acpv1`                                               |
| ACP1 errors only             | `dhs_acpv1.mtype == 3`                                    |
| ACP1 set method calls        | `dhs_acpv1.mcode == 1`                                    |
| All ACP2 traffic             | `dhs_acpv2 or dhs_acpv2_an2`                              |
| ACP2 announces               | `dhs_acpv2.type == 2`                                     |
| ACP2 errors                  | `dhs_acpv2.type == 3`                                     |
| All Ember+ traffic           | `dhs_emberplus`                                           |
| Ember+ keep-alive only       | `dhs_emberplus.s101.command == 0x01 or dhs_emberplus.s101.command == 0x02` |
| Ember+ Glow elements         | `dhs_emberplus_glow`                                      |
| All OSC traffic (1.0 + 1.1)  | `dhs_osc`                                                 |
| OSC 1.0 only                 | `dhs_osc.version == "OSC 1.0"`                            |
| OSC 1.1 only                 | `dhs_osc.version == "OSC 1.1"`                            |
| All Probel SW-P-08 traffic   | `dhs_probel_sw08p`                                        |
| Probel salvo fire (cmd 121)  | `dhs_probel_sw08p.cmd == 0x79`                            |

### Non-default ports

Ember+ auto-detects on **any TCP port** via a heuristic that checks BoF +
S101 header shape. No configuration needed — if your provider speaks S101
on, say, port 12345, Wireshark will still pick it up.

For protocols without a heuristic, right-click a packet → **Decode As…** →
pick the appropriate dissector (`dhs_acpv1`, `dhs_acpv2`, `dhs_osc`,
`dhs_probel_sw08p`) → OK. The rule is saved for the session; tick **Save**
to make it persistent.

---

## 6. Read a capture file already recorded by `dhs extract`

`dhs extract` emits `wire.jsonl` per run (one JSONL record per frame). That
format is for offline replay in Go tests, **not** a pcap file. To inspect raw
frames in Wireshark:

1. Re-run the same CLI command with `tcpdump`/`dumpcap` open in parallel, or
2. Use the captures stored under `bin/devices/captures/<proto>/<ip>/<slot>/`
   when `dhs ... --capture` was invoked against the device. These are pcap
   sidecars (not generated automatically today — follow-up work).

For fixture-driven offline inspection, the JSONL replay already gives
round-trip tests the byte-for-byte content; Wireshark is only needed for
live debugging.

---

## 7. Troubleshooting

| Symptom                                 | Fix                                                                        |
|-----------------------------------------|----------------------------------------------------------------------------|
| Protocol column is blank                | Confirm the port matches a registered default, or use **Decode As…**.      |
| "Lua: Error during loading" on startup  | **View → Lua → Evaluate** to see stack trace; check Wireshark version ≥ 4. |
| Ember+ frames show only raw bytes       | `dhs_emberplus` heuristic disabled under **Analyze → Enabled Protocols**.  |
| CRC mismatch on every Ember+ frame      | Your provider uses non-standard escaping — open an issue with a pcap.      |
| Tree recursion cut off at depth 20      | Malformed Glow tree. Grab the pcap and file a spec-deviation bug.          |

---

## 8. Updating dissectors

Pull the latest repo, re-copy the `.lua` files, then **View → Reload Lua
Plugins** (Ctrl-Shift-L). No Wireshark restart needed.

---

## References

- ACP1 spec: [`internal/acp1/assets/AXON-ACP_v1_4.pdf`](../internal/acp1/assets/AXON-ACP_v1_4.pdf)
- ACP2 + AN2 spec: [`internal/acp2/assets/acp2_protocol.pdf`](../internal/acp2/assets/acp2_protocol.pdf) · [`internal/acp2/assets/an2_protocol.pdf`](../internal/acp2/assets/an2_protocol.pdf)
- Ember+ spec v2.50: [`internal/emberplus/assets/Ember+ Documentation.pdf`](../internal/emberplus/assets/Ember+ Documentation.pdf)
- Probel SW-P-08 spec: [`internal/probel-sw08p/assets/probel-sw08p/SW-P-08 Issue 30.doc`](../internal/probel-sw08p/assets/probel-sw08p/SW-P-08%20Issue%2030.doc) (via `antiword`)
- Wire format summary: [`CLAUDE.md`](../CLAUDE.md)
