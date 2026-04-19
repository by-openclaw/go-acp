# Canonical Element Reference

One file per element type, each carrying:

- **Field reference** — every key, wire meaning for the codec dev, UI hint for the webui dev.
- **Realistic samples** — one per documented variation, in canonical shape.
- **Provider variations / edge cases** — how real-world Ember+ providers diverge from the textbook.
- **Consumer handling** — what resolvers do, which compliance events fire.

The samples are the same style as
[`assets/smh/emulator/ember-server/src/data-model-new.ts`](../../../assets/smh/emulator/ember-server/src/data-model-new.ts) —
literal JSON tree fragments you can read top-to-bottom — but split per type
and expanded to cover every case listed below.

| File                             | Type       | Status   | Variations covered                                                          |
|----------------------------------|------------|----------|-----------------------------------------------------------------------------|
| [node.md](node.md)               | Node       | shipped  | Root / identity / container / template-ref / offline.                       |
| [parameter.md](parameter.md)     | Parameter  | shipped  | integer, real, string, boolean, enum (+ masked), octets, trigger, streamed. |
| [matrix.md](matrix.md)           | Matrix     | shipped  | oneToN, oneToOne, nToN, linear vs nonLinear, dynamic, label patterns, gain. Probel extensions (matrixId, level, protect, valid, supportedLabelVariants) planned. |
| [function.md](function.md)       | Function   | shipped  | trigger-only, unary, binary, multi-return, void.                            |
| [template.md](template.md)       | Template   | shipped  | Inline / separate / both; templateReference resolution.                     |
| [stream.md](stream.md)           | Stream     | shipped  | Parameter with streamIdentifier; StreamCollection merge flow.               |
| [salvo.md](salvo.md)             | Salvo      | planned  | Staged members, commit tally, cross-matrix. Lands with Probel SW-P-08+.     |

Cross-cutting rules (common header, 3 mode flags, compliance events, capture
pipeline) live in [`../schema.md`](../schema.md).
