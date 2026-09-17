# Probel SW-P-08 / SW-P-88

Consumer + provider documentation for the Probel SW-P-08 General Remote
Control Protocol (level-scoped matrix routing over DLE/STX-framed TCP,
default port 2008).

| Role | Doc | Status |
|---|---|---|
| **Verbs & config reference** | [verbs.md](verbs.md) | every consumer verb + transport / controllers / logging / connect / interrogate / tally-dump / protect / names / salvo / discover / bench / wireshark / ansible, with real captured frames |
| Consumer | [consumer.md](consumer.md) | ✓ shipping — SW-P-08 Issue 30 compliant; level-scoped `<matrix, level, dst, src>`; general + extended wire forms; wire-tested against the loopback emulator on 127.0.0.1:12008 |
| Provider | [provider.md](provider.md) | ✓ shipping — strict-spec matrix emulator over DLE/STX/TCP; crosspoint + protect + names + salvo + tally-dump; serves a canonical `tree.json` |
| Operational runbook | [runbook.md](runbook.md) | verb-by-verb happy-path + error tables, captured against the loopback emulator |

## Spec documents

| Document | Path | Description |
|---|---|---|
| SW-P-08 General Remote Control Protocol | [SW-P-08 Issue 30.doc](../assets/probel-sw08p/SW-P-08%20Issue%2030.doc) | Issue 30. Authoritative. Read via antiword (the sibling PDF is corrupted) |
| SW-P-08 Issue 30 (text) | [SW-P-08_issue_30.txt](../assets/probel-sw08p/SW-P-08_issue_30.txt) | antiword extraction for grep / Ctrl-F |
| SW-P-88 Issue 3 | [SW-P-88 Issue 3.pdf](../assets/probel-sw08p/SW-P-88%20Issue%203.pdf) | command catalogue cross-reference |

## Spec section bookmarks

| Topic | Section |
|---|---|
| Transmission protocol (ACK / NAK, retry, 10 ms, 128-byte DATA) | §2 (**not** §3.5) |
| Narrow matrix/level packing + multiplier (4-bit + 3-bit DIV-128) | §3.1.2 |
| Multiplier semantics for protect / tally | §3.1.6 |
| RX general / TX general / RX extended / TX extended | §3.2 / §3.3 / §3.4 / §3.5 |

## Quick links

- [Consumer CLI reference](consumer.md#cli-commands-reference)
- [Command catalogue](consumer.md#command-catalogue)
- [Protect states](consumer.md#protect-states)
- [Known deviations](consumer.md#compliance--known-deviations)
- [Wire format & quirks](../CLAUDE.md) — §2 framing, DLE-stuffing, level-scoping, salvo-emits-cmd-04 deviation

## Capture provenance

Every wire sample in these docs is a **real captured frame** — the
`{ts, proto, dir, hex, len}` JSONL emitted by `--capture` against the
loopback provider, not hand-written hex. The capture procedure is
documented in [runbook.md](runbook.md#capture-procedure-how-the-samples-in-these-docs-were-made).
All verbs — including `salvo-connect` — are driven from the CLI; the
`salvo-connect` trace is a real `--capture` of the build → go-set →
go-clear flow (see [consumer.md](consumer.md#salvo-connect--controller-side-batch-route)).
