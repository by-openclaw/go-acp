# Ember+

Consumer + provider documentation for the Ember+ protocol
(Lawo Glow DTD over S101/TCP).

| Role | Doc | Status |
|---|---|---|
| **Verbs & config reference** | [verbs.md](verbs.md) | every verb + transport/redundancy/logging/get-set/matrix/invoke/stream/export/import/tree/ensure/wireshark/ansible, with real samples |
| Consumer | [consumer.md](consumer.md) | ✓ shipping — spec v2.50 rev.15 compliant, wire-tested on TinyEmberPlus 9000 + TinyEmberPlusRouter 9092 |
| Provider | [provider.md](provider.md) | ✓ shipping — strict-spec Glow encoder over S101/TCP; matrix + parameter + node + function + stream + template (R23 close pending APP 24 wire capture against Lawo mc² / Powercore / DHD) |

## Spec documents

| Document | Path | Description |
|---|---|---|
| Ember+ Protocol Specification | [Ember+ Documentation.pdf](../../../internal/emberplus/assets/Ember+%20Documentation.pdf) | v2.50 rev.15 (2017-11-09), Lawo GmbH. Authoritative |
| Ember+ Formulas | [Ember+ Formulas.pdf](../../../internal/emberplus/assets/Ember+%20Formulas.pdf) | Parameter formula syntax reference |

## Quick links

- [Consumer CLI reference](consumer.md#cli-commands-reference)
- [Glow element types](consumer.md#glow-element-types)
- [Compliance & tolerance profile](consumer.md#compliance--tolerance)
- [Test devices](consumer.md#test-devices)
