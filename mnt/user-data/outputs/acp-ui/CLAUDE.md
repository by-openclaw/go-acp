# CLAUDE.md — acp-ui

Read this file completely before touching any code.

---

## What This Is

A React 19 single-page application that provides a browser interface
to the `acp-srv` API (from the `acp` repository).

**This repository has no protocol knowledge.** It never speaks ACP1 or ACP2.
It only calls `acp-srv` via REST and WebSocket.

---

## One Critical Rule

TypeScript types in `src/types/api.ts` are **generated** from the OpenAPI spec.
**Never edit that file by hand.**

```bash
# acp-srv must be running
npm run generate:types
```

---

## Repository Structure

```
acp-ui/
├── src/
│   ├── main.tsx
│   ├── app.tsx                     router, layout shell, WS providers
│   │
│   ├── components/
│   │   ├── layout/
│   │   │   ├── Sidebar.tsx         nav links, responsive collapse
│   │   │   ├── TopBar.tsx          mobile top bar
│   │   │   └── PanelLayout.tsx     3-col / 2-col / 1-col responsive shell
│   │   │
│   │   ├── dashboard/
│   │   │   ├── StatCard.tsx        devices / slots / objects / announces/min
│   │   │   ├── DeviceSummaryList.tsx
│   │   │   └── SystemStatusPanel.tsx
│   │   │
│   │   ├── devices/
│   │   │   ├── DeviceList.tsx      left panel
│   │   │   ├── DeviceCard.tsx      IP, MAC, protocol badge, slot dots
│   │   │   ├── SlotDots.tsx        coloured per slot status
│   │   │   └── AddDeviceForm.tsx   IP + port + protocol selector
│   │   │
│   │   ├── tree/
│   │   │   ├── ObjectTree.tsx      center panel, collapsible
│   │   │   ├── TreeNode.tsx        recursive
│   │   │   ├── NodeRow.tsx         container / group row
│   │   │   ├── ValueRow.tsx        leaf row + inline value + ● live badge
│   │   │   └── LiveBadge.tsx       pulsing dot when watched
│   │   │
│   │   ├── property/
│   │   │   ├── PropertyDetail.tsx  right panel
│   │   │   ├── PropertyMeta.tsx    type, access, unit, step, min, max
│   │   │   ├── ValueDisplay.tsx    formatted read-only value
│   │   │   ├── ValueEditor.tsx     dispatches to type-specific editor
│   │   │   ├── editors/
│   │   │   │   ├── NumberEditor.tsx   input + slider
│   │   │   │   ├── EnumEditor.tsx     select
│   │   │   │   ├── PresetEditor.tsx   idx selector + value per idx
│   │   │   │   ├── Ipv4Editor.tsx     4-octet inputs
│   │   │   │   └── StringEditor.tsx   text + char counter
│   │   │   ├── PresetIndexSelector.tsx
│   │   │   └── ActionBar.tsx       GET SET WATCH buttons
│   │   │
│   │   ├── export/
│   │   │   ├── ExportPage.tsx
│   │   │   └── ExportTreePreview.tsx
│   │   │
│   │   ├── import/
│   │   │   ├── ImportPage.tsx      phase controller (upload→dryrun→apply)
│   │   │   ├── UploadPhase.tsx
│   │   │   ├── DryRunPhase.tsx     per-line status table
│   │   │   └── ApplyPhase.tsx      progress + per-line result
│   │   │
│   │   ├── logger/
│   │   │   ├── LoggerPage.tsx
│   │   │   ├── LogRow.tsx
│   │   │   ├── LogFilter.tsx
│   │   │   └── LogToolbar.tsx
│   │   │
│   │   └── shared/
│   │       ├── Badge.tsx
│   │       ├── StatusDot.tsx
│   │       ├── ProtocolBadge.tsx   "ACP1" / "ACP2" coloured chip
│   │       ├── Spinner.tsx
│   │       ├── ErrorBanner.tsx
│   │       └── ConfirmDialog.tsx
│   │
│   ├── hooks/
│   │   ├── useWebSocket.ts         one WS per device, shared via context
│   │   ├── useDeviceEvents.ts      slot_status, device_lost, device_found
│   │   ├── usePropertyWatch.ts     watch toggle + live value from WS
│   │   └── useObjectTree.ts        tree fetch + filter
│   │
│   ├── store/
│   │   ├── deviceStore.ts          devices, slots, selected mac/slot
│   │   ├── treeStore.ts            object tree per (mac, slot)
│   │   ├── propertyStore.ts        live values + watched set
│   │   └── logStore.ts             circular buffer 1000 entries
│   │
│   ├── api/
│   │   ├── client.ts               base fetch + error handling
│   │   ├── protocols.ts            GET /api/protocols
│   │   ├── devices.ts
│   │   ├── slots.ts
│   │   ├── objects.ts
│   │   ├── properties.ts
│   │   └── exportImport.ts
│   │
│   ├── types/
│   │   └── api.ts                  GENERATED — never edit manually
│   │
│   └── lib/
│       ├── formatValue.ts          Value → display string per type
│       ├── validateValue.ts        client-side check before SET
│       └── wsProtocol.ts           WS message type guards
│
├── .env.example
├── package.json
├── vite.config.ts
├── tsconfig.json
├── tailwind.config.ts
├── Dockerfile
├── nginx.conf
├── CLAUDE.md                       ← this file
├── agents.md
└── README.md
```

---

## Tech Stack

```
React 19            UI framework
TypeScript 5        strict mode
Vite 5              build + dev server
Tailwind CSS 3      utility classes only
Zustand 5           global state
TanStack Query 5    REST data fetching + cache
TanStack Virtual 3  virtual scroll (logger, large trees)
Lucide React        icons
Recharts            announce rate sparkline on dashboard
openapi-typescript  type generation from spec
```

---

## Routing

```
/                             Dashboard
/devices                      Device list + add device
/devices/:mac/:slot/tree      Object tree
/devices/:mac/:slot/:objId    Property detail
/export                       Export
/import                       Import (3-phase)
/logs                         Protocol exchange log
```

---

## Protocol Awareness

The UI treats protocols as opaque strings from `GET /api/protocols`.
The only protocol-specific rendering is:

```
Add device form:
  - Protocol dropdown populated from GET /api/protocols
  - Port auto-fills on protocol change (acp1→2071, acp2→2072)
  - User can override port

Device card:
  - ProtocolBadge shows "ACP1" or "ACP2" (or future names)

Object tree:
  - ACP1: groups displayed as flat sections (control / status / alarm)
  - ACP2: recursive node tree
  - Distinction driven by GET /api/devices/:mac/slots/:slot/objects response shape

Property detail:
  - ACP2 only: Preset Index Selector (idx field)
  - ACP2 only: announce_delay field (labeled "Announce Delay")
  - ACP1: no idx, no preset selector
```

**Never hardcode "acp1" or "acp2" strings in component logic.**
Use the `protocol` field from the device object.

---

## State Architecture

### Zustand stores

```typescript
deviceStore
  devices: Record<mac, Device>   // Device includes: ip, mac, protocol, slots
  selectedMac: string | null
  selectedSlot: number | null

treeStore
  trees: Record<`${mac}:${slot}`, Object[]>
  expanded: Set<string>

propertyStore
  liveValues: Record<`${mac}:${slot}:${objKey}`, Value>
  watched: Set<string>

logStore
  entries: LogEntry[]    // max 1000, FIFO circular
  filter: LogFilter
```

### TanStack Query

```typescript
// Object tree — stale 5 min, no refetch on window focus
queryKey: ['tree', mac, slot]

// System status — refetch every 30s
queryKey: ['system']

// Available protocols — cached, rarely changes
queryKey: ['protocols']
```

### WebSocket

One WS per device: `ws://host:8080/ws/{mac}`

```typescript
// Server → Client
type WsMessage =
  | { type: 'property_changed'; mac; slot; obj_key; value; timestamp }
  | { type: 'slot_status';      mac; slot; status }
  | { type: 'device_found';     mac; ip; protocol }
  | { type: 'device_lost';      mac }
  | { type: 'log';              ts; level; msg; attrs }

// Client → Server
type WsCommand =
  | { type: 'watch';   slot; obj_key }
  | { type: 'unwatch'; slot; obj_key }
```

Dispatch: all WS messages → Zustand stores → components re-render.
Never dispatch directly to component state from WS.

---

## Coding Conventions

### TypeScript

- `strict: true` — no exceptions, no `any`
- Explicit return types on all exported functions
- `unknown` + type guards at WS/API boundaries
- Never edit `src/types/api.ts`

### React 19

- Functional components only
- `useOptimistic` for all SET operations (immediate UI update + rollback on error)
- `useTransition` around tree navigation
- `use()` for promise unwrapping where applicable

### SET Flow (always follow this exactly)

```
1. user edits value in editor
2. validateValue(obj, value) — client-side, instant
   invalid → red border + message, SET button disabled
3. user clicks SET
4. useOptimistic → update store immediately (optimistic)
5. PUT /api/.../properties/{pid}?idx={idx}   (ACP2)
   PUT /api/.../objects/{group}/{id}/value   (ACP1)
6. success → green flash, log entry
7. error   → rollback + ErrorBanner
```

### WATCH Flow (always follow this exactly)

```
1. user clicks WATCH
2. WS send: { type: "watch", slot, obj_key }
3. button → ● WATCHING, tree node → LiveBadge
4. incoming WS property_changed → propertyStore.setLiveValue → re-render
5. user clicks WATCHING → WS send: { type: "unwatch", slot, obj_key }
```

### Styling

- Tailwind utility classes only — no custom CSS except `globals.css`
- Dark theme: bg=`#0d1117`, sidebar=`#111827`, card=`#1a2234`
- Never add `!important` — structure your selectors correctly
- Responsive via Tailwind breakpoints `xl:` `md:` (mobile-first, no prefix)

---

## Responsive Layout

```
≥ 1280px (xl)
  sidebar(w-64) | devices(w-72) | tree(w-80) | detail(flex-1)
  logger: pinned bottom h-48, resizable

768–1279px (md)
  sidebar(icons,w-14) | [devices|tree](tabs,w-80) | detail(flex-1)
  logger: bottom drawer

< 768px (sm)
  topbar + full-screen active panel
  bottom tab nav: [Devices][Tree][Detail][Logs]
  push navigation: tap device→tree, tap object→detail
```

---

## Configuration

```bash
# .env
VITE_API_URL=http://localhost:8080
VITE_WS_URL=ws://localhost:8080
```

Baked at Vite build time. Docker: pass as build args.

---

## Generate Types

```bash
# acp-srv must be running on localhost:8080
npm run generate:types
# runs: openapi-typescript http://localhost:8080/openapi.json -o src/types/api.ts
```

Run this after every API change in `acp/api/openapi.yaml`.

---

## What NOT to Do

- Never call ACP1/ACP2 devices directly
- Never write ACP or AN2 protocol logic
- Never hardcode "acp1" or "acp2" in component logic (use device.protocol)
- Never manually edit `src/types/api.ts`
- Never store property values in localStorage / sessionStorage
- Never poll REST for live values — use WebSocket
- Never add a backend, proxy, or server to this repo
- Never create a Zustand slice for data that belongs in TanStack Query
