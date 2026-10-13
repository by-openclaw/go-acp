# ADR-0036 — Site limits may narrow factory limits, never widen them

Status: proposed (direction given by the codeowner, 2026-10-14)

## Context

Every connector validates a value before sending it, against the factory
limits of the device family:

- acp2 and Ember+: the device describes each object live (type, min,
  max, enum) and the shared validator asks it;
- ccm: the device's own api.yml is the write contract;
- rrcs: RRCS describes nothing over the wire, so the table generated
  from the Riedel specification (§8.10.4) is compiled into the binary.

A plant often needs stricter operational limits than the factory ones: a
gain the factory allows from −18 to 18 dB that this site caps at 6; a
channel count the hardware takes to 16 that this plant fixes at 8. Today
nothing holds such site rules, and an operator or an import can set any
factory-legal value.

## Decision

1. A connector's validation accepts an optional set of SITE LIMITS per
   property: min, max, allowed values.
2. A site limit may only NARROW the factory limit, never widen it:
   `site_min >= factory_min`, `site_max <= factory_max`, allowed values
   a subset of the factory's. A site limit outside the factory limit is
   itself refused when loaded, naming both ranges.
3. Factory limits always apply; site limits apply on top. A value is
   refused when it breaks either, and the refusal names which: `refused
   by the site limit (site 2..8, factory 0..16)`.
4. The checks run where all validation runs: at the dry run, before
   anything is sent — the same for import, ensure and set.
5. The mechanism lives in the shared validation layer, so every
   connector gets it the same way — acp2's device-described limits
   included: the device stays the factory truth, the site file narrows
   it.

## Not decided

| # | Question |
| --- | --- |
| 1 | Where the site limits live: a file beside the plant's desired state, or the platform inventory (NetBox) once Q9/Q10 land |
| 2 | Their format (the export's metadata columns are the natural shape: path or property, min, max, enum) |
| 3 | Who may change them (ties to the access and audit scope) |

## Consequences

- A site can enforce plant policy without touching the binaries.
- The factory table stays the only truth about what the hardware takes;
  drift between the two is impossible by construction (rule 2).
- One more input to version with the plant's configuration.
