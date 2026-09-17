# Registry-to-registry bridge (lab scripts)

Operational proof of `dhs Node ↔ dhs Registry ↔ [Cerebrum Registry ↔
Cerebrum controller]`, run live 2026-08-29 on VLAN600. Background and
the traps (HTTP 411 heartbeats, parent-validation fill order) are in
[`../../../../internal/amwa/docs/cerebrum-interop.md`](../../../../internal/amwa/docs/cerebrum-interop.md),
"Registry-to-registry bridge".

- `bridge.py` — snapshot the dhs Query API and proxy-register every
  resource into a target Registration API in dependency order
  (node → device → source → flow → sender → receiver). Idempotent —
  registration POSTs are upserts.
- `bridge_hb.sh` — loop: re-read the source node list, proxy one
  `POST /health` per node every 4 s. The `-d ""` is load-bearing:
  Cerebrum's registry 411s bodyless POSTs and its GC then silently
  sweeps everything.

Source/target addresses are hardcoded lab values; edit before reuse.
The productized `dhs registry nmos mirror` verb (Query-WS-driven,
deletion propagation) is designed, awaiting approval post-merge.
