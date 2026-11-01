# virtuoso

Consumer for the Nevion Virtuoso MI: an Element Manager (slot 0) and up to
8 extension cards (slots 1..8) over the REST/JSON API of the User Guide.

**This connector has never talked to a real Virtuoso.** Everything below was
proven on a fake built from the guide and from the shape of a capture.

| Document | What it holds |
|---|---|
| [`consumer.md`](consumer.md) | every verb, what it sends, an example |
| [`runbook.md`](runbook.md) | what the operator runs at a site, and the open questions |
| [`../CLAUDE.md`](../CLAUDE.md) | wire facts, with the guide's sections |

## Coverage

| Verb | What it does | Unit, fake | CLI, scratch fake | Real device |
|---|---|---|---|---|
| `info` | identity of every module, absent cards named | yes | yes | no |
| `walk` | schema + data of one module, or `--all` | yes | yes | no |
| `get` | one GET by path, no walk | yes | yes | no |
| `set` | PATCH, then read back | yes | yes | no |
| `watch` | polled | yes | yes | no |
| `export` | the walk to a file | no, shared code | yes | no |

Tested on the fake: login, logout, a dead slot (hung up, 404, 502, foreign
page), a status other than ok, warnings and notes, an expired cookie, a
re-login refused, a read-only write refused with nothing sent, a read-back
that differs, one request in flight, no credential in a capture.

The codec also joined the schema and data of a real capture, offline: every
reference resolved, about 3 000 objects on the manager and 3 000 to 4 500 on a
card. Only counts came out of that run, and no capture is in git.

Not tested anywhere: HTTPS against a real certificate, a real session expiry,
a real card, a real write.

## Not done

- Producer: not asked for.
- Wireshark dissector: not asked for.
- Integration against a vendor emulator or a device, and its Ansible play.
- Replay fixtures from a real capture.
- `internal/virtuoso/COMPLIANCE.md` (ADR-0008).
- Alarm log verbs, configuration load and save, software upgrade, RPC calls.

By ADR-0025 the connector is not DONE.
