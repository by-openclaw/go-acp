# Request (MTYPE=1)

Consumer-to-provider message. Always MTYPE=1. Carries an MCODE
(method ID 0-5) plus the target object (group, id).

## Spec

AXON-ACP_v1_4.pdf, p. 11.

```
ACP1 header (7 bytes, UDP mode A):
  u32  MTID               -- transaction id (nonzero, incremented per new request)
  u8   PVER       = 1     -- ACP v1
  u8   MTYPE      = 1     -- Request
  u8   MADDR              -- target slot (0 = rack controller)

MDATA (1..134 bytes):
  u8   MCODE              -- method ID (0=getValue, 1=setValue, 2=setIncValue,
                          --            3=setDecValue, 4=setDefValue, 5=getObject)
  u8   ObjGrp            -- 0=root, 1=identity, 2=control, 3=status,
                          -- 4=alarm, 5=file, 6=frame
  u8   ObjId             -- object index within group
  Value up to 131 bytes   -- method args (setValue only)
```

Total ≤ 141 bytes per request.

The fixture shows the first request sent by a walker: `getValue` on
`slot=0 frame[0]` (rack controller's Frame Status object).

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 428 bytes.
- Extracted from: `bin/acp1_walk_slot0_slot1.pcapng` frame 1.
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

```bash
# Every CLI operation emits at least one request
./bin/acp get 10.6.239.113 --protocol acp1 --slot 0 --group frame --id 0
```
