# agents.md — acp-ui

See `acp/agents.md` for the full shared agent instructions.

This file contains acp-ui–specific additions only.

---

## This Repo's Boundary

`acp-ui` only talks to `acp-srv`. It has zero protocol knowledge.
If you find yourself writing ACP1 or ACP2 logic here, stop — it belongs in `acp/`.

---

## Protocol Display Rules

```
Protocol names come from GET /api/protocols — never hardcoded.
Protocol badge: render device.protocol as a coloured chip.
Port defaults: fetched from ProtocolMeta.default_port, not hardcoded.

ACP1 tree shape:   flat groups → render as labelled sections
ACP2 tree shape:   nested nodes → render as recursive collapsible tree
Shape determined by API response structure, not by checking protocol name.
```

---

## When to Regenerate Types

Run `npm run generate:types` (acp-srv must be running) after:
- Any change to `acp/api/openapi.yaml`
- Any new endpoint added to `acp/api/handlers/`
- Any new field added to a response struct in `acp/`

Never commit stale types. The CI check runs `npm run typecheck`.
