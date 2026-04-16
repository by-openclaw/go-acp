# acp-ui

React 19 browser interface for `acp-srv`.

Connects to `acp-srv` via REST and WebSocket.
Supports all protocols registered in `acp` (ACP1, ACP2, and future protocols).

---

## Quick Start

```bash
# Prerequisites: Node 20+, acp-srv running

cp .env.example .env          # set VITE_API_URL and VITE_WS_URL
npm install
npm run dev
# open http://localhost:5173
```

---

## Environment

```bash
# .env
VITE_API_URL=http://localhost:8080
VITE_WS_URL=ws://localhost:8080
```

---

## Scripts

```bash
npm run dev              # Vite dev server
npm run build            # production build → dist/
npm run preview          # preview production build
npm run generate:types   # regenerate src/types/api.ts from acp-srv OpenAPI spec
npm run test             # vitest unit tests
npm run lint             # eslint
npm run typecheck        # tsc --noEmit
```

---

## Type Generation

TypeScript types are generated from the `acp-srv` OpenAPI spec.
Run after any API change:

```bash
# acp-srv must be running
npm run generate:types
```

**Never manually edit `src/types/api.ts`.**

---

## Docker

```bash
docker build \
  --build-arg VITE_API_URL=http://192.168.1.100:8080 \
  --build-arg VITE_WS_URL=ws://192.168.1.100:8080 \
  -t acp-ui .

docker run -p 3000:80 acp-ui
```

---

## Features

- **Multi-protocol**: ACP1 and ACP2 in the same UI — add device with protocol selector
- **Device dashboard**: connection status, slot status dots, announce rate sparkline
- **Object tree**: collapsible, filterable, with live value badges on watched properties
- **Property detail**: type-specific editors (number+slider, enum, preset, ipv4, string)
- **Live updates**: WebSocket announces update values in real-time without polling
- **Export**: full device / slot / group / object family — JSON, CSV, YAML
- **Import**: 3-phase (upload → dry-run with per-line status → apply with progress)
- **Logger**: virtual-scroll protocol exchange log, filter by level/device/object
