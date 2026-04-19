# Salvo

> **Status:** planned extension. Schema documented here; Go struct
> (`canonical.Salvo`) not yet added to `internal/export/canonical/`.
> Lands with the Probel SW-P-08+ plugin work. See
> `memory/project_probel_extensions.md`.

A **Salvo** is a named, stateful group of staged crosspoint
connections that can be committed (fired) atomically. Salvos are
first-class elements — not callables — because they carry persistent
observable state (member list, status, commit history) that consumers
subscribe to via the normal announce mechanism.

Salvos were first introduced by Probel SW-P-08+ (§3.1.29–31, §3.2.24–26)
but the concept is reusable for any protocol that needs atomic
multi-crosspoint application. Ember+ has no native salvo; a Probel
plugin surfaces them through this canonical element.

## Why not a Function?

| Aspect              | Function     | Salvo                          |
|---------------------|--------------|--------------------------------|
| Has persistent state| No           | Yes (members, status)          |
| Observable via announce | No (one-shot call) | Yes (tally broadcast on commit/stage) |
| Readable by interrogation | No (void after return) | Yes (read members at any time) |
| Subscribable        | No           | Yes (via normal Subscribe)     |

Functions are call-and-return. Salvos are living objects with a
commit lifecycle: **stage → fire → clear**. Modelling them as
Functions loses the staged-members read-back and the tally stream.

## Field reference

| Key             | Type                              | Wire meaning (codec dev)                                                                                       | UI hint (webui dev)                                                         |
|-----------------|-----------------------------------|----------------------------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------|
| *common header* |                                   | See [node.md](node.md).                                                                                        | Same.                                                                       |
| `capacity`      | integer                           | Maximum number of staged member connections the provider accepts. Probel SW-P-08+: 128 per group (§3.1.29).    | "X / capacity" counter.                                                     |
| `members`       | array                             | `[{target, sources[], matrixId?, level?}]`. Staged connections awaiting commit.                                | Preview grid of what will be applied on "Go".                               |
| `status`        | string                            | `"staged"` \| `"firing"` \| `"committed"` \| `"cleared"` \| `"invalid"`.                                       | Status badge. `"firing"` highlighted for feedback.                          |
| `lastCommitAt`  | string \| null                    | ISO-8601 UTC timestamp of last successful GO. `null` if never fired.                                           | Tooltip "last fired HH:MM:SS".                                              |
| `matrixRef`     | string \| null                    | OID of the Matrix this salvo applies to. `null` if the salvo spans matrices (Probel cross-matrix salvos).      | Link to matrix element.                                                     |

Companion Functions (siblings of the Salvo in `children[]` of its
parent Node) provide the verbs:

| Function identifier | Arguments                                           | Result                | Maps to Probel command               |
|---------------------|-----------------------------------------------------|-----------------------|--------------------------------------|
| `stageConnection`   | `(target:int, source:int)`                          | `success:bool`        | CONNECT ON GO GROUP SALVO (§3.1.29)  |
| `go`                | `()`                                                | `status:enum`         | GO GROUP SALVO (§3.1.30)             |
| `clearPending`      | `()`                                                | `success:bool`        | (SW-P-08 + implementation)           |
| `interrogate`       | `(index:int)`                                       | `{target, sources[]}` | SALVO GROUP INTERROGATE (§3.1.31)    |

The Salvo element holds the observable state; the Functions carry
the operations.

## Sample 1 — staged salvo, pre-commit

Eight staged connections; not yet fired. Grouped under the router's
salvo container Node.

```json
{
  "number": 0,
  "identifier": "evening-take",
  "path": "router.salvos.evening-take",
  "oid": "3.99.0",
  "description": "Evening news take",
  "isOnline": true,
  "access": "readWrite",

  "capacity": 128,
  "status": "staged",
  "lastCommitAt": null,
  "matrixRef": "3.0.0",

  "members": [
    { "target": 0,  "sources": [12] },
    { "target": 1,  "sources": [12] },
    { "target": 2,  "sources": [14] },
    { "target": 3,  "sources": [14] },
    { "target": 8,  "sources": [5]  },
    { "target": 9,  "sources": [5]  },
    { "target": 10, "sources": [3]  },
    { "target": 11, "sources": [3]  }
  ],

  "children": [
    { "number": 0, "identifier": "stageConnection", "path": "router.salvos.evening-take.stageConnection",
      "oid": "3.99.0.0", "description": "Stage a crosspoint",
      "isOnline": true, "access": "read",
      "arguments": [ {"name": "target", "type": "integer"}, {"name": "source", "type": "integer"} ],
      "result": [ {"name": "success", "type": "boolean"} ],
      "children": [] },

    { "number": 1, "identifier": "go", "path": "router.salvos.evening-take.go",
      "oid": "3.99.0.1", "description": "Commit all staged connections",
      "isOnline": true, "access": "read",
      "arguments": [],
      "result": [ {"name": "status", "type": "string"} ],
      "children": [] },

    { "number": 2, "identifier": "clearPending", "path": "router.salvos.evening-take.clearPending",
      "oid": "3.99.0.2", "description": "Drop all staged connections",
      "isOnline": true, "access": "read",
      "arguments": [],
      "result": [ {"name": "success", "type": "boolean"} ],
      "children": [] },

    { "number": 3, "identifier": "interrogate", "path": "router.salvos.evening-take.interrogate",
      "oid": "3.99.0.3", "description": "Read a staged member by index",
      "isOnline": true, "access": "read",
      "arguments": [ {"name": "index", "type": "integer"} ],
      "result": [
        {"name": "target", "type": "integer"},
        {"name": "sources", "type": "string"}
      ],
      "children": [] }
  ]
}
```

## Sample 2 — committed salvo, post-fire

Same salvo after GO returned status=00 (set). `members` cleared,
`status=committed`, `lastCommitAt` populated. Further stage/go cycles
reset `lastCommitAt`.

```json
{
  "number": 0,
  "identifier": "evening-take",
  "path": "router.salvos.evening-take",
  "oid": "3.99.0",
  "description": "Evening news take",
  "isOnline": true,
  "access": "readWrite",

  "capacity": 128,
  "status": "committed",
  "lastCommitAt": "2026-04-18T19:00:00Z",
  "matrixRef": "3.0.0",

  "members": [],

  "children": []
}
```

## Sample 3 — invalid / cleared-no-data

GO was sent on an empty salvo — provider returned status=02 (cleared).
Consumer fires `probel_salvo_cleared_no_data` compliance event.

```json
{
  "number": 1,
  "identifier": "standby",
  "path": "router.salvos.standby",
  "oid": "3.99.1",
  "description": "Standby salvo (never staged)",
  "isOnline": true,
  "access": "readWrite",

  "capacity": 128,
  "status": "invalid",
  "lastCommitAt": null,
  "matrixRef": "3.0.0",

  "members": [],

  "children": []
}
```

## Sample 4 — cross-matrix salvo

Stages members across TWO matrices (video + audio). `matrixRef` is
`null` because the salvo spans multiple matrices; each member's
`matrixId` + `level` disambiguates.

```json
{
  "number": 2,
  "identifier": "cross-bus",
  "path": "router.salvos.cross-bus",
  "oid": "3.99.2",
  "description": "Cross-matrix bus take",
  "isOnline": true,
  "access": "readWrite",

  "capacity": 128,
  "status": "staged",
  "lastCommitAt": null,
  "matrixRef": null,

  "members": [
    { "target": 5, "sources": [10], "matrixId": 1, "level": 0 },
    { "target": 5, "sources": [10], "matrixId": 1, "level": 1 },
    { "target": 2, "sources": [ 7], "matrixId": 2, "level": 0 }
  ],

  "children": []
}
```

Cross-matrix salvos only work within a single device instance (one
TCP session). Per `memory/project_deployment_strategy.md`, when a
device is sharded across multiple ports/pods, cross-matrix salvos
must live in the same pod.

## Provider variations

| Pattern                                | Notes                                                                               |
|----------------------------------------|-------------------------------------------------------------------------------------|
| Probel SW-P-08+ textbook               | Group salvo, 0..127 groups, 128 members each. Fits Sample 1 directly.               |
| Probel MIXER-aware salvo               | Members carry gain via `connectionParams` pattern (see matrix.md).                  |
| Cross-matrix salvo                     | `matrixRef: null`, each member carries its own `matrixId` + `level`.                |
| Ember+ "fake salvo" via Function chain | Not a salvo. Document as Function cluster under a Node, no Salvo element emitted.   |

## Consumer handling

- **Discovery**: salvos appear in the walk as peers of Matrix elements
  under the router's salvo container Node.
- **Subscribe**: normal Subscribe on the Salvo element delivers
  member and status updates via announce.
- **Fire flow**: client calls `stageConnection` N times, then `go`;
  provider returns GO status (00/01/02). Consumer updates
  `status` on receipt and fires a compliance event if status=02.
- **Compliance events**: `probel_salvo_capacity_exceeded`,
  `probel_salvo_cleared_no_data`. Generic:
  `field_lossy_down` on gain conversion inside a salvo member.

## See also

- [`../schema.md`](../schema.md) — common header, `--templates` flag (salvos support templateReference in principle).
- [`matrix.md`](matrix.md) — what salvos operate on.
- [`function.md`](function.md) — the companion verbs.
- `memory/project_probel_extensions.md` — when this lands in Go.
