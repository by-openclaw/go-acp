# OSC — operational runbook

Quick-reference card for operators. For wire-format detail see
[../CLAUDE.md](../CLAUDE.md); for in-depth behaviour see [consumer.md](consumer.md)
and [provider.md](provider.md). OSC is a **message/push** protocol — there is no
device to walk, get, or set; you listen (`watch`) or push (`send`/`fader`/`serve`).

Spec: <https://opensoundcontrol.org/spec-1_0.html> ·
<https://opensoundcontrol.org/spec-1_1.html> (not `osc.org`).

---

## Transport matrix

| Kind | `--listen` / `--transport` | Default port | Version | Notes |
|---|---|---:|---|---|
| UDP | `udp` | 8000 | both | one datagram = one packet; SO_REUSEADDR multi-listener; broadcast-friendly |
| TCP length-prefix | `tcp-len` | 8000 | osc-v10 only | int32 BE size + packet |
| TCP SLIP | `tcp-slip` | 8001 | osc-v11 only | RFC 1055 double-END |

The CLI enforces version↔framing: `tcp-slip` ⇒ `osc-v11`, `tcp-len` ⇒ `osc-v10`.

## Verb reference

### Consumer

| Verb | What it does | Common flags |
|---|---|---|
| `dhs consumer osc-vNN watch --listen <kind>:<port>` | bind a port; print every inbound packet | `--pattern PAT` |

> No `info` / `walk` / `get` / `set` / `export` — **N/A** for a push protocol
> (no device tree / object catalogue).

### Producer

| Verb | What it does | Required / common flags |
|---|---|---|
| `dhs producer osc-vNN send --to <host:port> --transport <kind> --address /A --types TAGS [args…]` | push one message, exit | `--to`, `--transport`, `--address`, `--types` |
| `dhs producer osc-vNN fader --to <host:port> --transport <kind>` | high-rate float push (perf) | `--rate`, `--duration`, `--min/--max`, `--pattern ramp\|sine\|random` |
| `dhs producer osc-vNN serve --bind <kind>:<port>` | bind + log inbound (no echo) | `--pattern` |

## Common flows

### Monitor a control surface

```
dhs consumer osc-v10 watch --listen udp:8000
dhs consumer osc-v10 watch --listen udp:8000 --pattern "/mixer/*/gain"
```

Live-captured output (loopback):

```
[udp      ] /mixer/fader1  ,fsi  0.75 "PGM" 42
```

### Push a value to a device (X32 / QLab / Companion)

```
dhs producer osc-v10 send --to 192.0.2.50:8000 --transport udp --address /ch/01/mix/fader --types f 0.5
dhs producer osc-v11 send --to 192.0.2.50:8001 --transport tcp-slip --address /q/go --types T
```

### Measure throughput / latency

```
dhs producer osc-v10 fader --to 127.0.0.1:8000 --rate 1000 --duration 5s --pattern sine
```

Real measured run (UDP, 1000 Hz, 2 s on this host): `989 frames/s`,
`mean=3µs max=1.029ms`, `errors: 0`.

### Act as a passive OSC sink

```
dhs producer osc-v10 serve --bind udp:8000
```

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `transport=tcp-slip requires --protocol osc-v11` | SLIP requested on osc-v10 | use `osc-v11` (SLIP is 1.1 only) |
| `transport=tcp-len requires --protocol osc-v10` | length-prefix on osc-v11 | use `osc-v10` (length-prefix is 1.0 only) |
| `flag provided but not defined: -3.5` | negative value directly after `--types` | place negatives after a preceding positional (`--types if 1 -6.0`) |
| `watch` shows nothing | wrong port/transport, or pattern too narrow | confirm `--listen` matches sender; `*` does not cross `/` (use `/a/*/c` not `/a/*`) |
| Two listeners share a UDP port but only one receives | SO_REUSEPORT inactive (e.g. Windows) | expected; integration suite accepts partial reception |
| 1.1 `T/F/N/I` not appearing as args | they are payload-less by design | they print as `T F N I` with no bytes — correct (captured) |

## Idempotency

OSC has no converge target (no read-back), so `ensure`-style idempotency does
not apply. The Ansible play's run-twice = same-result holds at the **socket**
level: each integration test binds an ephemeral socket and tears it down
(`changed_when: false`). Pushing the same `send` twice emits two identical
packets — there is no de-duplication.

## Integration (Ansible — no .ps1)

```
# loopback only (CI / agent, no node):
ansible-playbook -i inventory/hosts.ini playbooks/osc-integration.yml

# + osc.js cross-impl oracle (install node + osc.js in ../assets/test-harness):
ansible-playbook -i inventory/hosts.ini playbooks/osc-integration.yml -e osc_run_oscjs=true
```

Oracle-per-tier: the cross-implementation **osc.js** peer is the external oracle
(not a lab host); loopback (our provider ↔ our consumer over real localhost
sockets) is regression only. `OSC_TEST_HOST` is reserved for a future live-peer
test and not used to gate today.

## Pointers

- Wire format: [../CLAUDE.md](../CLAUDE.md)
- Consumer / Provider deep-dives: [consumer.md](consumer.md) · [provider.md](provider.md)
- Verbs + captured samples: [verbs.md](verbs.md)
- Wireshark dissector: [../wireshark/dhs_osc.lua](../wireshark/dhs_osc.lua)
- Fixtures: [../testdata/fixtures/README.md](../testdata/fixtures/README.md)
- ADR-0025 (definition of done): [../../../docs/adr/0025-per-connector-definition-of-done.md](../../../docs/adr/0025-per-connector-definition-of-done.md)
</content>
