# Reply (MTYPE=2)

Provider-to-consumer response. Always MTYPE=2. Echoes the consumer's
MTID so the client can correlate the reply to its outstanding request.
Body carries the method's return bytes.

## Spec

AXON-ACP_v1_4.pdf, p. 11.

```
ACP1 header (7 bytes):
  u32  MTID                -- echoed from request
  u8   PVER       = 1
  u8   MTYPE      = 2      -- Reply
  u8   MADDR               -- same slot as request

MDATA (1..134 bytes):
  u8   MCODE               -- same method ID as request
  u8   ObjGrp
  u8   ObjId
  Value bytes              -- method return value
```

For `getValue`: Value = current value bytes.
For `getObject`: Value = every property byte in sequence (type + n_props + ...).
For `setValue`: Value = confirmed value bytes (provider may clamp).
For `setIncValue / setDecValue / setDefValue`: Value = resulting value bytes.

The fixture is the Frame Status `getValue` reply — pairs with the
[`request/`](../request/) fixture. Same MTID, inverted direction.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 460 bytes.
- Extracted from: `bin/acp1_walk_slot0_slot1.pcapng` frame 2.
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

Reply arrives as a result of any request:

```bash
./bin/acp get 10.6.239.113 --protocol acp1 --slot 0 --group frame --id 0
# → prints whatever the provider's Reply body decodes to
```
