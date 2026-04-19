# Wireshark Dissectors for ACP1, ACP2, and Ember+

This repository ships three Lua dissectors so you can inspect live or captured
traffic from the CLI (`acp walk`, `acp extract`, `acp watch`, ...) in Wireshark
without having to decode frames by hand.

| Protocol | Transport          | Default ports | Lua file                                  |
|----------|--------------------|---------------|-------------------------------------------|
| ACP1     | UDP / TCP direct   | 2071          | [`assets/acp1/dissector_acpv1.lua`](../assets/acp1/dissector_acpv1.lua) |
| ACP2     | AN2 over TCP       | 2072          | [`assets/acp2/dissector_acp2.lua`](../assets/acp2/dissector_acp2.lua)   |
| Ember+   | S101 over TCP      | 9000 / 9090 / 9092 | [`assets/emberplus/dissector_emberplus.lua`](../assets/emberplus/dissector_emberplus.lua) |

All three target **Wireshark 4.x** (Lua 5.2+). They install the same way.

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
cp assets/acp1/dissector_acpv1.lua        "$HOME/.local/lib/wireshark/plugins/"
cp assets/acp2/dissector_acp2.lua         "$HOME/.local/lib/wireshark/plugins/"
cp assets/emberplus/dissector_emberplus.lua "$HOME/.local/lib/wireshark/plugins/"
```

```powershell
# Windows PowerShell
Copy-Item assets/acp1/dissector_acpv1.lua        $env:APPDATA\Wireshark\plugins\
Copy-Item assets/acp2/dissector_acp2.lua         $env:APPDATA\Wireshark\plugins\
Copy-Item assets/emberplus/dissector_emberplus.lua $env:APPDATA\Wireshark\plugins\
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
**Analyze → Enabled Protocols** and verify the three protocols are listed:

- `emberplus` — Ember+ / S101 (with child `emberplus_glow` for the BER tree)
- `acp2_msg` / `an2_acp2` — ACP2 + AN2 transport
- `acp1` — ACP1

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
./bin/acp walk 10.6.239.113 --protocol acp1 --slot 0 --capture out/acp1/

# ACP2 (Convert Hybrid VM)
./bin/acp walk 10.41.40.195 --protocol acp2 --slot 0 --capture out/acp2/

# Ember+ (TinyEmberPlus, port 9092)
./bin/acp walk localhost --protocol emberplus --port 9092 --capture out/emberplus/
```

### Display filters

| Want to see                  | Filter                        |
|------------------------------|-------------------------------|
| All ACP1 traffic             | `acp1`                        |
| ACP1 errors only             | `acp1.mtype == 3`             |
| ACP1 set method calls        | `acp1.mcode == 1`             |
| All ACP2 traffic             | `acp2 or an2`                 |
| ACP2 announces               | `acp2.type == 2`              |
| ACP2 errors                  | `acp2.type == 3`              |
| All Ember+ traffic           | `emberplus`                   |
| Ember+ keep-alive only       | `emberplus.s101.command == 0x01 or emberplus.s101.command == 0x02` |
| Ember+ Glow elements         | `emberplus_glow`              |

### Decode As… (non-default ports)

If your provider runs on a port we don't register by default, right-click a
packet → **Decode As…** → pick the appropriate protocol (`emberplus`,
`acp2_msg`, `acp1`) → OK. The rule is saved for the session; tick **Save**
to make it persistent.

---

## 6. Read a capture file already recorded by `acp extract`

`acp extract` emits `wire.jsonl` per run (one JSONL record per frame). That
format is for offline replay in Go tests, **not** a pcap file. To inspect raw
frames in Wireshark:

1. Re-run the same CLI command with `tcpdump`/`dumpcap` open in parallel, or
2. Use the captures stored under `bin/devices/captures/<proto>/<ip>/<slot>/`
   when `acp walk --capture` was invoked against the device. These are pcap
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
| Ember+ frames show only raw bytes       | `emberplus` heuristic disabled under **Analyze → Enabled Protocols**. |
| CRC mismatch on every Ember+ frame      | Your provider uses non-standard escaping — open an issue with a pcap.      |
| Tree recursion cut off at depth 20      | Malformed Glow tree. Grab the pcap and file a spec-deviation bug.          |

---

## 8. Updating dissectors

Pull the latest repo, re-copy the `.lua` files, then **View → Reload Lua
Plugins** (Ctrl-Shift-L). No Wireshark restart needed.

---

## References

- ACP1 spec: [`assets/acp1/AXON-ACP_v1_4.pdf`](../assets/acp1/AXON-ACP_v1_4.pdf)
- ACP2 + AN2 spec: [`assets/acp2/acp2_protocol.pdf`](../assets/acp2/acp2_protocol.pdf) · [`assets/acp2/an2_protocol.pdf`](../assets/acp2/an2_protocol.pdf)
- Ember+ spec v2.50: [`assets/emberplus/Ember+ Documentation.pdf`](../assets/emberplus/Ember+ Documentation.pdf)
- Wire format summary: [`CLAUDE.md`](../CLAUDE.md)
