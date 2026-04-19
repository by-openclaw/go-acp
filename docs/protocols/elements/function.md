# Function

A **Function** is a callable. It declares a signature (named+typed
arguments, named+typed results) and is invoked via Ember+ `Invocation`;
the provider returns an `InvocationResult` with success/failure and
result values. Functions are used for operations that don't map to a
writable Parameter: "salvo all", "restart card", "query capacity",
server-side calculations.

## Field reference

| Key         | Type   | Wire meaning (codec dev)                                                                                               | UI hint (webui dev)                                                         |
|-------------|--------|------------------------------------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------|
| *common header* |    | See [node.md](node.md). Function has `access:"read"` — it's the *presence* that is read; invocation is a separate op. | Same.                                                                       |
| `arguments` | array  | `[{name, type}]`. Names are hints; positions on the wire matter.                                                       | Render one input per argument, labeled with `name`, typed by `type`.        |
| `result`    | array  | `[{name, type}]`. Same shape as `arguments`. Zero-length (`[]`) means void.                                            | After invoke, render the result tuple; empty → just a "success" indicator. |

### Argument/result types

Same vocabulary as Parameter `type`:

| `type`    | Argument — send as   | Result — expect                                   |
|-----------|----------------------|---------------------------------------------------|
| `integer` | int64                | int64                                             |
| `real`    | float64              | float64                                           |
| `string`  | UTF-8 string         | UTF-8 string                                      |
| `boolean` | bool                 | bool                                              |
| `octets`  | base64 bytes         | base64 bytes                                      |
| `enum`    | int index            | int index (+ optional `enumMap` on the arg spec)  |

Functions carry no `value`, `default`, or streamed concepts — they are
call-only.

## Sample 1 — void trigger

Zero args, zero results. Classic "fire-and-forget" operation.

```json
{
  "number": 0,
  "identifier": "reboot",
  "path": "system.reboot",
  "oid": "9.0.0",
  "description": "Reboot the device",
  "isOnline": true,
  "access": "read",
  "arguments": [],
  "result": [],
  "children": []
}
```

Invocation: `Invoke(oid=9.0.0, args=[])` → `InvocationResult(success=true, result=[])`.

## Sample 2 — unary query

One input, one output. Classic "lookup" function.

```json
{
  "number": 1,
  "identifier": "slotInfo",
  "path": "system.slotInfo",
  "oid": "9.0.1",
  "description": "Get info for a given slot number",
  "isOnline": true,
  "access": "read",
  "arguments": [ { "name": "slot", "type": "integer" } ],
  "result":    [ { "name": "info", "type": "string"  } ],
  "children": []
}
```

Invocation: `Invoke(9.0.1, [3])` → `InvocationResult(true, ["SDI-4K Rev.2"])`.

## Sample 3 — binary arithmetic (canonical demo)

This is the textbook Ember+ `add` function used on TinyEmber+.

```json
{
  "number": 2,
  "identifier": "add",
  "path": "system.add",
  "oid": "9.0.2",
  "description": "Add two integers",
  "isOnline": true,
  "access": "read",
  "arguments": [
    { "name": "a", "type": "integer" },
    { "name": "b", "type": "integer" }
  ],
  "result": [
    { "name": "sum", "type": "integer" }
  ],
  "children": []
}
```

Invocation: `Invoke(9.0.2, [3, 5])` → `InvocationResult(true, [8])`.

## Sample 4 — multi-return capacity query

One input, two outputs. Useful for "how full are you?" queries that want
both used and total in one round-trip.

```json
{
  "number": 3,
  "identifier": "capacity",
  "path": "storage.capacity",
  "oid": "7.0.3",
  "description": "Return used/total bytes for a volume",
  "isOnline": true,
  "access": "read",
  "arguments": [ { "name": "volume", "type": "string" } ],
  "result": [
    { "name": "usedBytes",  "type": "integer" },
    { "name": "totalBytes", "type": "integer" }
  ],
  "children": []
}
```

Invocation: `Invoke(7.0.3, ["vol0"])` →
`InvocationResult(true, [412000000000, 1000000000000])`.

## Sample 5 — matrix salvo command

Multi-arg, zero-result. Used to push a whole take-set atomically.

```json
{
  "number": 4,
  "identifier": "takeSalvo",
  "path": "router.takeSalvo",
  "oid": "3.0.4",
  "description": "Apply a salvo of target->source mappings atomically",
  "isOnline": true,
  "access": "read",
  "arguments": [
    { "name": "targets", "type": "string" },
    { "name": "sources", "type": "string" }
  ],
  "result": [],
  "children": []
}
```

`targets` and `sources` are CSV-encoded int lists (provider convention). The
consumer has no schema beyond `type:"string"` — the calling code must know
the format. Use a more structured parameter set where possible.

## Sample 6 — action returning success flag

Device-level action with explicit success/failure response value (on top of
the protocol-level success flag).

```json
{
  "number": 5,
  "identifier": "applyPreset",
  "path": "audio.applyPreset",
  "oid": "1.5.5",
  "description": "Apply stored preset to all channels",
  "isOnline": true,
  "access": "read",
  "arguments": [
    { "name": "presetId", "type": "integer" }
  ],
  "result": [
    { "name": "ok",      "type": "boolean" },
    { "name": "message", "type": "string"  }
  ],
  "children": []
}
```

## Sample 7 — enum argument

Function that takes one enum input. The argument schema does not carry
`enumMap` natively — the consumer must fetch the backing enum definition
from a Parameter it mirrors, or encode labels as strings. (Most real
providers just use `integer` + documentation here.)

```json
{
  "number": 6,
  "identifier": "setMode",
  "path": "system.setMode",
  "oid": "9.0.6",
  "description": "Set operational mode (0=standby, 1=active, 2=maintenance)",
  "isOnline": true,
  "access": "read",
  "arguments": [ { "name": "mode", "type": "integer" } ],
  "result":    [],
  "children": []
}
```

## Invocation on the wire

| Step                      | Payload                                                                          |
|---------------------------|----------------------------------------------------------------------------------|
| 1. Consumer → Provider    | `Invocation { id: <int32>, arguments: [typed BER values in order] }`             |
| 2. Provider executes      | Synchronous or asynchronous; consumer keyed by invocation `id`.                   |
| 3. Provider → Consumer    | `InvocationResult { id: <same>, success: <bool>, result: [typed BER values] }`   |

Invocation `id` is monotonically incremented per session by the consumer
and stored in a pending map until the result arrives. `success=false` means
the function itself reported failure; `result[]` may still carry diagnostic
values in that case.

Invocation and InvocationResult are **wire-only** — they are not part of
the JSON export. The declared Function element (sample above) is.

## Provider variations

| Pattern                                   | Notes                                                                                           |
|-------------------------------------------|-------------------------------------------------------------------------------------------------|
| Functions with zero description           | Common. Consumer falls back to `identifier`.                                                     |
| Functions declared under dedicated "functions" Node | Clean convention. Alternatively, functions sit as siblings of Parameters under any Node. |
| Arguments with empty `name`               | Legal; UI must render as `arg[0]`, `arg[1]`.                                                     |
| Results typed `octets`                    | Used for vendor-specific binary payloads; consumer surfaces base64.                              |
| Async functions                           | Spec allows provider to delay `InvocationResult`. Consumer must not block the read loop waiting.|
| Non-unique `name` across args/result      | Legal but discouraged; webui disambiguates by position.                                          |

## Consumer handling

- **Discovery**: functions appear in the walk like Nodes/Parameters. No
  separate Subscribe — they don't change state.
- **Invocation**: encode `Invocation` with consumer-assigned `id`; store a
  result channel keyed by `id`; await `InvocationResult` on the read loop;
  deliver or time out.
- **Result mapping**: provider tuple comes back positionally — zip with the
  declared `result[]` metadata to produce a named JSON object for display.
- **Timeouts**: recommend 10s default; consumer exposes per-call timeout.
- **Compliance events**: none specific to Function. Generic
  `field_lossy_down` may fire on overflow during int conversion.

## See also

- [`../schema.md`](../schema.md) — common header.
- [`parameter.md`](parameter.md) — value-type vocabulary.
- [`node.md`](node.md) — functions are leaf children of Nodes.
