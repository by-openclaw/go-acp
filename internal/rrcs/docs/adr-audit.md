# RRCS connector — audit against the ADRs

State on 2026-10-07, branch `feat/rrcs-consumer`. One row per requirement:
what the ADR asks, what the connector does, what is open. "Real" means
seen working against a real RRCS; "stand-in" means unit-tested against a
fake gateway only.

## 1. Summary

| # | Finding | Weight |
|---|---|---|
| 1 | The rrcs verbs live in a private dispatcher, not behind a registered `consumer.Protocol` plugin | Structural — see §3 |
| 2 | No write has passed on a real RRCS; one stopped a production RRCS | Blocks every write deliverable until the test system |
| 3 | Deliverables 2 to 6 of ADR-0025 are missing | The connector is not done |
| 4 | Logging, `ensure`, alarm template, command guide: closed in this pass | — |
| 5 | Two RRCS generations are in the plant: 9.0 ("PortEX" API) and 8.4 ("Port" API) | `set` / `import` / `ensure` only speak the 9.0 form |

## 2. Requirement by requirement

### ADR-0002 — canonical verbs and flags

| Canonical verb | rrcs | Note |
|---|---|---|
| `discover` | Present, **wrong meaning** | Here it lists the gateway's objects; the canonical verb finds devices on the network. To rename (it is now a subset of `walk`) or to drop |
| `connect` / `disconnect` | Missing | HTTP per request: no session to hold. A `not_supported` stub is what the ADR asks |
| `info` | Real | |
| `walk` | Real | Writes a snapshot file; the canonical walk prints the tree. Same intent |
| `tree` | Stand-in live, real on a snapshot | |
| `get <path>` | Real | Takes `--path` / `--id` flags, not a positional path |
| `set <path> <value>` | Stand-in; **stopped a real RRCS** | Takes `--path` and `--prop NAME=VALUE` |
| `watch` | Real for three event types, stand-in for the rest | |
| `export` / `import` | Real export on a snapshot; import stand-in | acp contract: `--format/--out`, `--file/--dry-run` |
| `ensure` | Stand-in | ADR-0007 output; file-driven, no `--state` |
| `status` / `health` | Missing | `info` covers the content; `health` needs the plugin (§3) |
| `extract` / `validate` / `replay` | Missing | Need the plugin and committed fixtures |
| Extensions | `list`, `xp`, `call` | Allowed as additions |

| Canonical flag | rrcs |
|---|---|
| `--output text\|json` | Yes (no `yaml`) |
| `--timeout`, `--capture` | Yes |
| `--log-format` and the log flags | Yes, since this pass |
| `--check` | `ensure` yes. `import` uses `--dry-run` (the acp form), `set` uses `--apply yes` |
| `--metrics-addr` | No — comes with the plugin |
| `--host` / `--port` | Host is positional, `host:port` |

### ADR-0006 — codec standard library only

Met. `internal/rrcs/codec` imports nothing outside the standard library;
100 % statement coverage.

### ADR-0007 — ensure

| Requirement | State |
|---|---|
| `--check` mutates nothing | Met, tested |
| `{changed \| would_change, previous \| target, current, diff[]}` | Met; `diff` is `[]` when empty |
| Run twice = no change | Met against the stand-in; the Ansible role asserts it |
| Exit code = outcome, not change | Met: 0, or 1 when a field could not reach its target |
| Scope | Values of ports and client cards, crosspoints. **Not** conferences, groups, IFBs, key assignment, port creation |

### ADR-0013, 0014, 0027 — workflow

| Requirement | State |
|---|---|
| One unit = one commit, `Refs #N` | Followed; issues #1399 – #1432 |
| No PR while a deliverable is missing | No PR opened |
| Every new verb approved as text first | Followed for the first verbs; the later ones were built on the codeowner's direct requests during the 2026-10-06 session |

### ADR-0020, 0021, 0028 — captures and artefacts

| Requirement | State |
|---|---|
| `--capture` writes the JSONL wire trace with a meta line | Met. The verb's own notes are written as extra meta records (`{"note": …}`), which is outside the letter of ADR-0021 |
| Captures under `captures/<proto>/<host>/`, snapshots under `snapshots/` | Met |
| Committed fixtures under `internal/rrcs/testdata/` | **Missing** — the real captures hold the plant's configuration; committing them waits for the codeowner's go |

### ADR-0025 — definition of done

| # | Deliverable | State |
|---|---|---|
| 1 | Consumer, every command of the specification | Partial. Reads: most. Events: decoded, three seen. Writes: `xp`, `set`, `import`, `ensure` unproven; everything else only through `call` |
| 2 | Producer | **Missing**, not decided (scope: consumer first) |
| 3 | Integration test driving the binary, Ansible plays | **Missing.** `playbooks/rrcs-ensure.yml` exists and has never run |
| 4 | DM + manifest fixture | **Missing** — needs the plugin's object model |
| 5 | Wireshark dissector | **Missing** |
| 6 | Replay fixture set | **Missing** |
| — | README with coverage, runbook | `internal/rrcs/README.md` (command guide), `docs/oracle-test-plan.md` |
| — | CI coverage floors for the rrcs packages | **Missing** — a workflow change needs the codeowner's go |

### ADR-0031, 0033 — events and alarms

| Requirement | State |
|---|---|
| Events as values with a path | Met in `watch` (`--events values`); they are not yet `consumer.Event` on a plugin |
| Alarm template per model, every row with a source | `internal/rrcs/alarm/RRCS@9.0.json`: two rules, both sourced from the RRCS log. `watch --alarm FILE` evaluates it |
| Node and client card alarms | Left as info: neither the specification nor RRCS gives them a severity. The plant has to |
| `alarm` verb, cached template under `.cache/alarm/rrcs/` | Not wired — comes with the plugin |

### ADR-0034 — oracle testing

Nothing counts as verified until it has run against a vendor emulator or a
real device. The oracle arrives at the datacentre; the steps are in
`docs/oracle-test-plan.md`.

### docs/logging.md — uniform logging

Met since this pass: `--log` (default a daily file under
`.cache/logs/rrcs/<host>/<verb>.log`), `--log-format`, `--log-level`,
`--syslog-addr`, `--log-retention` on every verb; `watch` writes
`msg=value_change` and `msg=alarm` records. Verbs that read a snapshot
file (`--from`) open no gateway and write no log.

## 3. The structural gap: no plugin

Every other connector registers a `consumer.Protocol` (Connect, Walk,
GetValue, SetValue, Subscribe). The generic verbs, the alarm evaluator,
the health check, the metrics endpoint, the neutral monitor, `validate`
and the Ansible `dhs_verb` role all work on that interface.

rrcs has a private dispatcher (`cmd/dhs/cmd_rrcs*.go`) and its model lives
in package `main`. It was the fast way to a first contact, and it is why
logging, `ensure` and alarms had to be wired by hand here.

Closing it means:

| Step | Content |
|---|---|
| 1 | Move the model, the collector and the event decoder from `cmd/dhs` into `internal/rrcs/consumer` |
| 2 | Implement `consumer.Protocol` on them: the tree as objects with path, kind, access, unit, range; events as `consumer.Event` |
| 3 | Register the plugin; keep `list`, `xp`, `call`, `tree` as rrcs extensions; retire the private `walk` / `get` / `set` / `watch` / `export` / `import` / `ensure` in favour of the generic ones |
| 4 | The write guard has to survive the move: the generic `set` has no `--write-to` |

It is a refactor of working code and belongs after the test system has
confirmed the write shapes, not before.

## 4. Two RRCS generations in the plant

From the plant's network drawing (v0.3, 2025-04-25):

| RRCS | Version | Configuration API | Artist |
|---|---|---|---|
| Médiasquare | 9.0 | "Configuration Change PortEX" | Matrix 1 (Artist-1024, 9.0) |
| Reyers | 8.4 | "Configuration Change Port" | Reyers ring (Artist-32/64/128, 8.4) |

`set`, `import` and `ensure` send the object type `portex` only. The 8.4
gateway needs the older `port` object type (specification §8.10.4.5,
"deprecated"), whose members differ. The choice has to follow the version
`GetVersion` reports. Not started.

## 5. What the plant still has to decide

| # | Question |
|---|---|
| 1 | Severity of each node and client card alarm (§9.7), so the template can stop reporting them as info |
| 2 | Whether the real captures and the specification PDFs go into the repository once it is private |
| 3 | Whether streams stay owned by NMOS or are set through RRCS in Manual mode |
| 4 | Go for the plugin refactor of §3, and when |
