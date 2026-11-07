# Collecting a device's NMOS description on a customer site

For a device we cannot reach from the lab (first case: a Grass Valley
LDX C135 camera behind a bastion). Everything here only reads; nothing
is sent to the device. Run it from a machine on the site that reaches
the device, bring the folders back.

## 1. Get dhs

Release page: <https://github.com/by-openclaw/go-acp/releases/latest>

| Machine | Asset | Then |
|---|---|---|
| Windows VM | `dhs_windows_amd64.zip` | unzip; `dhs.exe` in the folder; `certutil -hashfile dhs_windows_amd64.zip SHA256` against `SHA256SUMS.txt` |
| Linux VM | `dhs_linux_amd64.tar.gz` | `tar xzf`, `sha256sum -c SHA256SUMS.txt --ignore-missing` |

## 2. Capture the device (the camera's NMOS node)

```
# everything the node says about itself, every API minor, every IS-05
# endpoint with staged / active / constraints / transporttype, raw bodies kept
dhs consumer nmos export --target 10.44.79.132:8601 --deep --raw --all-versions --out ldx-c135-4

# a one-screen summary, to check on the spot that it answered
dhs consumer nmos walk --node http://10.44.79.132:8601 -l

# conformance of what it serves (optional, 30 s)
dhs consumer nmos probe --target 10.44.79.132:8601 --deep --format json --out ldx-c135-4-probe.json
```

What to look for, if you want to read on the spot: in the export, each
`Tally-*` Receiver's IS-05 `constraints` and `active` — that is where the
MQTT transport parameters the camera expects appear (`broker_host`,
`broker_port`, `broker_topic`, `broker_protocol`,
`connection_status_broker_topic`), and whether something already feeds
them (`sender_id`, `master_enable`).

## 3. Capture the plant's registry (optional, tells who feeds the tally today)

```
dhs consumer nmos export --target <registry host>:<port> --deep --out plant-registry
```

The registry is the one the camera registers into (on this site the
Cerebrum NMOS FCR shows it at 10.44.55.15; its Query API port is in
Cerebrum's NMOS settings). The export follows every registered node, so
it also holds any existing IS-07 tally Senders.

## 4. Bring back

Zip the folders `ldx-c135-4/`, `plant-registry/` and the probe file. They
contain no credentials. From them we build the tally sender side
(`dhs producer nmos serve` publishing IS-07 over MQTT) and the site
runbook.
