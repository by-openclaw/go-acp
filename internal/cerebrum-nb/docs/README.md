# Cerebrum NB plugin

EVS **Cerebrum Northbound API v0.13** — also branded **Neuron Bridge**.
XML over WebSocket, default port **40007**.

## Quick links

| Doc | What it covers |
|---|---|
| [keys.md](keys.md) | Authoritative element / attribute / enum catalogue (the wire facts) |
| [consumer.md](consumer.md) | CLI walkthrough + portable Windows install recipe |
| [../CLAUDE.md](../CLAUDE.md) | Atomic per-protocol context — wire layer, mtid, quirks, "what NOT to do" |

## Status (2026-04-26)

- 🟡 In flight, branch `feat/cerebrum-nb-plugin`, tracking issue
  [#143](https://github.com/by-openclaw/go-acp/issues/143).
- Consumer plugin: codec + WS framing + Login/Poll/Action/Subscribe/
  Obtain/Unsubscribe/UnsubscribeAll, CLI verbs (`connect` / `listen` /
  `list-devices` / `list-routers` / `walk`), Wireshark dissector, unit
  tests, integration test scaffold.
- Provider plugin: not yet (separate follow-up PR).
- Real-peer interop validation: pending Cerebrum server access.

## Spec sources

- Primary: `assets/Cerebrum Northbound API 0v13.pdf` +
  `assets/cerebrum_northbound_api_full_v0_13.docx`
- Third-party vendor reference driver — held under NDA, gitignored;
  cited as a secondary cross-check only.
