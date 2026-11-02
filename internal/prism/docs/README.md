# prism

Consumer for the Telestream PRISM waveform monitor, over the REST API
described by the page the instrument serves at `/api/help`.

**Never tested on a real PRISM.** Everything below was proven against a
fake built from that page.

| Doc | Purpose |
|---|---|
| [`consumer.md`](consumer.md) | the CLI, verb by verb |
| [`runbook.md`](runbook.md) | what to run at the customer site, and what to bring back |
| [`../CLAUDE.md`](../CLAUDE.md) | wire facts, compliance events, open questions |

## Coverage

| Verb | Unit (httptest fake) | Real binary against a fake | Real PRISM |
|---|---|---|---|
| info | yes | yes | not tested |
| walk | yes | yes | not tested |
| get | yes | yes | not tested |
| set | yes | yes | not tested |
| watch | yes | yes | not tested |
| export | through walk | yes | not tested |

## Not done

- Producer: not built.
- Wireshark dissector: not built, it waits until the connector is in use.
- Integration test, DM + manifest fixture, replay fixtures: they need a
  real instrument.

The connector does not meet ADR-0025 until those exist.
