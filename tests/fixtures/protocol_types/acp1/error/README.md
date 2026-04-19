# Error (MTYPE=3)

Provider rejection of a request. MCODE becomes the **error code**, not a
method ID. Error codes split into two ranges:

- **< 16** — transport-level problem (bus timeout, out of resources)
- **≥ 16** — object-level problem (wrong group, bad type, no access)

## Spec

AXON-ACP_v1_4.pdf, p. 11.

```
ACP1 header (7 bytes):
  u32  MTID                -- echoed from offending request
  u8   PVER       = 1
  u8   MTYPE      = 3      -- Error
  u8   MADDR

MDATA (at least 1 byte, may omit ObjGrp/ObjId):
  u8   MCODE               -- error code
       <16: transport error
         0 undefined
         1 internal bus communication error
         2 internal bus timeout
         3 transaction timeout
         4 out of resources
       >=16: AxonNet object error
        16 object group does not exist
        17 object instance does not exist
        18 object property does not exist
        19 no write access
        20 no read access
        21 no setDefault access
        22 object type does not exist
        23 illegal method
        24 illegal method for this object type
        32 file error
        39 SPF file constraint violation
        40 SPF buffer full — retry fragment later
  (optional) u8 ObjGrp + u8 ObjId  -- echoed from offending request
```

The fixture shows error code 17 (`Object instance does not exist`) for a
request to `alarm[99]` — a non-existent alarm ID. Tree echoes the group
and id so the client knows which request the error pertains to.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 428 bytes.
- Extracted from: `bin/acp1_walk_slot0_slot1.pcapng` frame 1274.
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

```bash
./bin/acp get 10.6.239.113 --protocol acp1 --slot 0 --group alarm --id 99
# → error: acp1 get: object instance does not exist (code=17)
```
