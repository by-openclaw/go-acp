# Cerebrum NB plugin

EVS **Cerebrum Northbound API 0v16** (authoritative) — also branded
**Neuron Bridge**. XML over WebSocket, default port **40007**.
0v13 is the historical baseline; 0v16 is a superset.

## Quick links

| Doc | What it covers |
|---|---|
| [keys.md](keys.md) | Authoritative element / attribute / enum catalogue (the wire facts) |
| [verbs.md](verbs.md) | 12-section verb + sample reference (device-config, 5-mode lock, datastore) |
| [consumer.md](consumer.md) | CLI walkthrough + portable Windows install recipe |
| [runbook.md](runbook.md) | Operator quick-reference card |
| [provider.md](provider.md) | Provider rationale — consumer-only by design (N/A) |
| [../CLAUDE.md](../CLAUDE.md) | Atomic per-protocol context — wire layer, mtid, quirks, "what NOT to do" |

## Status

- Consumer plugin: **complete at 0v16** — codec + WS framing +
  Login/Poll/Action/Subscribe/Obtain/Unsubscribe/UnsubscribeAll +
  DeviceConfiguration; CLI verbs (`connect` / `listen` / `route` /
  `lock` / `unlock` / `set-mnemonic` / `set-tags` / `salvo` / `category`
  / `set-value` / `device-config` / `list-devices` / `device-details` /
  `device-value` / `list-categories` / `category-details` /
  `list-salvo-groups` / `list-salvo-instances` / `salvo-instance-details`
  / `obtain-datastore` / `keepalive-probe`); Wireshark dissector;
  unit tests; codec-generated fixtures + drift-guard.
- **Provider plugin: N/A by design** — Cerebrum is a northbound API we
  consume; there is no "serve Cerebrum" role. See [provider.md](provider.md).
- Real-peer interop validation: pending the NB northbound **licence**
  (currently missing on the lab Cerebrum) — the Ansible verb play
  ([`../../../ansible/playbooks/cerebrum-nb-integration.yml`](../../../ansible/playbooks/cerebrum-nb-integration.yml))
  is written + ready and skips until host + creds + licence exist.

## Wire samples

Every wire sample in these docs comes from the committed
[codec-generated fixtures](../testdata/fixtures/README.md) — **not a live
capture** (a live capture is pending the NB licence). The fixtures are
produced by the codec and pinned byte-for-byte by a drift-guard.

## Spec sources

- Authoritative: `assets/Cerebrum Northbound API 0v16.pdf`
- Historical baseline: `assets/Cerebrum Northbound API 0v13.docx` +
  `assets/cerebrum_northbound_api_full_v0_13.docx`
- Live wire captures from production Cerebrum servers, when an NB licence
  is available, override spec text where the two disagree.
