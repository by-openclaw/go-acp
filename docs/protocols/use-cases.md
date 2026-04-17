# Use Cases — Sequence Diagrams

Protocol-agnostic flows. Applies to ACP1, ACP2, and future Ember+.

---

## 1. Connect + Discover (first time, no library)

```
  CLI/API            Connector           Device            Library (disk)
    |                   |                  |                    |
    |-- connect ------->|                  |                    |
    |                   |-- TCP/UDP ------>|                    |
    |                   |<--- connected ---|                    |
    |                   |                  |                    |
    |                   |-- get_info ----->|                    |
    |                   |<--- slots=2 -----|                    |
    |                   |                  |                    |
    |                   |-- get_identity ->|                    |
    |                   |<-- SHPRM1/5.3.5 -|                    |
    |                   |                  |                    |
    |                   |-- lookup ------->|----------------->  |
    |                   |                  |  NOT FOUND         |
    |                   |                  |<-----------------  |
    |                   |                  |                    |
    |                   |== FULL WALK ====>|                    |
    |                   |  get_object(1)   |                    |
    |                   |  get_object(2)   |                    |
    |                   |  ...             |                    |
    |                   |  get_object(N)   |                    |
    |                   |<== all objects ==|                    |
    |                   |                  |                    |
    |                   |-- save DM ------>|----------------->  |
    |                   |                  |  SHPRM1_5.3.5.json |
    |                   |                  |<-----------------  |
    |                   |                  |                    |
    |<-- ready (N obj) -|                  |                    |
    |                   |                  |                    |
```

---

## 2. Connect + Load from Library (instant)

```
  CLI/API            Connector           Device            Library (disk)
    |                   |                  |                    |
    |-- connect ------->|                  |                    |
    |                   |-- TCP/UDP ------>|                    |
    |                   |<--- connected ---|                    |
    |                   |                  |                    |
    |                   |-- get_identity ->|                    |
    |                   |<-- SHPRM1/5.3.5 -|                    |
    |                   |                  |                    |
    |                   |-- lookup ------->|----------------->  |
    |                   |                  |  FOUND             |
    |                   |                  |  load DM from disk |
    |                   |<----------------|<-----------------  |
    |                   |                  |                    |
    |<-- ready (instant)|                  |                    |
    |                   |                  |                    |
    |   NO WALK NEEDED  |                  |                    |
    |                   |                  |                    |
```

---

## 3. Get Value by ID

```
  CLI/API            Connector           Device
    |                   |                  |
    |-- get(slot=0,     |                  |
    |    id=15239) ---->|                  |
    |                   |                  |
    |                   |-- get_object --->|
    |                   |    (id=15239)    |
    |                   |                  |
    |                   |<-- reply --------|
    |                   |    value="OK"    |
    |                   |    kind=enum     |
    |                   |                  |
    |<-- value="OK" ----|                  |
    |    kind=enum      |                  |
    |    id=15239       |                  |
    |                   |                  |
```

---

## 4. Set Value by ID (ACK + Announcement)

```
  CLI/API            Connector           Device           Other consumers
    |                   |                  |                    |
    |-- set(slot=0,     |                  |                    |
    |    id=21127,      |                  |                    |
    |    "Full Speed")->|                  |                    |
    |                   |                  |                    |
    |                   |-- reverse map -->|                    |
    |                   |  "Full Speed"    |                    |
    |                   |   → wire idx 19  |                    |
    |                   |                  |                    |
    |                   |-- set_property ->|                    |
    |                   |    (id=21127,    |                    |
    |                   |     val=19)      |                    |
    |                   |                  |                    |
    |                   |<-- ACK ---------|                    |
    |                   |    confirmed=19  |                    |
    |                   |                  |                    |
    |<-- confirmed -----|                  |                    |
    |    "Full Speed"   |                  |                    |
    |                   |                  |                    |
    |                   |                  |-- announce ------->|
    |                   |<-- announce -----|    (id=21127,      |
    |                   |    (id=21127,    |     val=19)        |
    |                   |     val=19)      |                    |
    |                   |                  |                    |
    |   (watch sees     |                  |                    |
    |    the change)    |                  |                    |
    |                   |                  |                    |
```

---

## 5. Watch (Subscribe + Announcements)

```
  CLI/API            Connector           Device            Library (disk)
    |                   |                  |                    |
    |-- watch(slot=0)-->|                  |                    |
    |                   |                  |                    |
    |                   |-- load cache --->|----------------->  |
    |                   |                  |  labels + units    |
    |                   |<----------------|<-----------------  |
    |                   |                  |                    |
    |                   |-- subscribe ---->|                    |
    |                   |   (all objects)  |                    |
    |                   |                  |                    |
    |                   |     BACKGROUND:  |                    |
    |                   |== full walk ====>|                    |
    |                   |                  |                    |
    |                   |<-- announce -----|                    |
    |<-- [cache] -------|  id=15212        |                    |
    |    Temperature=20C|  (label from     |                    |
    |                   |   disk cache)    |                    |
    |                   |                  |                    |
    |                   |<-- announce -----|                    |
    |<-- [cache] -------|  id=47397        |                    |
    |    Power Rx=0.99mW|                  |                    |
    |                   |                  |                    |
    |                   |<== walk done ====|                    |
    |                   |  (plugin tree    |                    |
    |                   |   populated)     |                    |
    |                   |                  |                    |
    |                   |-- save DM ------>|----------------->  |
    |                   |                  |                    |
    |                   |<-- announce -----|                    |
    |<-- [live] --------|  id=15212        |                    |
    |    Temperature=21C|  (label + decode |                    |
    |                   |   from walk tree)|                    |
    |                   |                  |                    |
    |   ... continues until Ctrl-C ...     |                    |
    |                   |                  |                    |
```

---

## 6. Export

```
  CLI/API            Connector           Device            File (disk)
    |                   |                  |                    |
    |-- export(slot=0,  |                  |                    |
    |    format=yaml)-->|                  |                    |
    |                   |                  |                    |
    |                   |== full walk ====>|                    |
    |                   |  (DM + values)   |                    |
    |                   |<== all objects ==|                    |
    |                   |                  |                    |
    |                   |-- save DM ------>|                    |
    |                   |  (cache, no val) |                    |
    |                   |                  |                    |
    |-- apply --path -->|                  |                    |
    |   apply --filter  |                  |                    |
    |   (filter objects)|                  |                    |
    |                   |                  |                    |
    |-- write YAML ---->|-----------------|----------------->  |
    |                   |                  |  slot0.yaml        |
    |                   |                  |  (tree + values)   |
    |                   |                  |                    |
    |<-- done (N obj) --|                  |                    |
    |                   |                  |                    |
```

---

## 7. Import (dry-run + apply)

```
  CLI/API            Connector           Device            File (disk)
    |                   |                  |                    |
    |-- import -------->|                  |                    |
    |   (file=slot0.yaml|                  |                    |
    |    dry-run=true)  |                  |                    |
    |                   |                  |                    |
    |                   |<-- read file ----|<-----------------  |
    |                   |   parse objects  |  slot0.yaml        |
    |                   |                  |                    |
    |                   |-- walk slot ---->|                    |
    |                   |  (fresh tree     |                    |
    |                   |   for metadata)  |                    |
    |                   |<== tree =========|                    |
    |                   |                  |                    |
    |   FOR EACH OBJECT:|                  |                    |
    |                   |                  |                    |
    |   R-- (read-only)?|  → skip          |                    |
    |   RW- (writable)? |  → would apply   |                    |
    |                   |                  |                    |
    |<-- dry-run report-|                  |                    |
    |    45 would apply |                  |                    |
    |    154 skipped    |                  |                    |
    |    0 failed       |                  |                    |
    |                   |                  |                    |
    |                   |                  |                    |
    |== APPLY (not dry-run): ==============|                    |
    |                   |                  |                    |
    |   FOR EACH RW-:   |                  |                    |
    |                   |-- set_property ->|                    |
    |                   |    (by obj ID)   |                    |
    |                   |<-- ACK ---------|                    |
    |                   |                  |                    |
    |<-- applied 45 ----|                  |                    |
    |                   |                  |                    |
```

---

## 8. Browse (search DM + live values)

```
  CLI/API            Connector           Device            Library (disk)
    |                   |                  |                    |
    |-- browse -------->|                  |                    |
    |   (--filter Temp  |                  |                    |
    |    --slot 0)      |                  |                    |
    |                   |                  |                    |
    |                   |-- load DM ------>|----------------->  |
    |                   |                  |  SHPRM1_5.3.5.json |
    |                   |<----------------|<-----------------  |
    |                   |                  |                    |
    |                   |-- search DM ---->|                    |
    |                   |  filter="Temp"   |                    |
    |                   |  matches:        |                    |
    |                   |   15212 PSU/1    |                    |
    |                   |   15220 PSU/2    |                    |
    |                   |   15529 PSU/BOARD|                    |
    |                   |                  |                    |
    |                   |-- get(15212) --->|                    |
    |                   |<-- 20 C --------|                    |
    |                   |-- get(15220) --->|                    |
    |                   |<-- 20 C --------|                    |
    |                   |-- get(15529) --->|                    |
    |                   |<-- 0 C ---------|                    |
    |                   |                  |                    |
    |<-- results -------|                  |                    |
    |  15212 PSU/1/Temp  20 C              |                    |
    |  15220 PSU/2/Temp  20 C              |                    |
    |  15529 PSU/B/Temp  0 C               |                    |
    |                   |                  |                    |
```

---

## 9. Startup with Stale Cache

```
  CLI/API            Connector           Device            Library (disk)
    |                   |                  |                    |
    |-- connect ------->|                  |                    |
    |                   |-- TCP/UDP ------>|                    |
    |                   |<--- connected ---|                    |
    |                   |                  |                    |
    |                   |-- load cache --->|----------------->  |
    |                   |                  |  DM + stale values |
    |                   |<----------------|<-----------------  |
    |                   |                  |                    |
    |<-- all objects ----|                  |                    |
    |    [stale]        |                  |                    |
    |                   |                  |                    |
    |                   |-- subscribe ---->|                    |
    |                   | (client view     |                    |
    |                   |  objects first)  |                    |
    |                   |                  |                    |
    |                   |<-- announce -----|                    |
    |<-- id=15212 ------|  [stale] → [live]|                    |
    |    Temperature=20C|                  |                    |
    |                   |                  |                    |
    |                   |<-- announce -----|                    |
    |<-- id=15219 ------|  [stale] → [live]|                    |
    |    Fan Speed=10%  |                  |                    |
    |                   |                  |                    |
    |   SUBSCRIBED OBJECTS NOW [live]       |                    |
    |                   |                  |                    |
    |                   |     BACKGROUND:  |                    |
    |                   |== walk rest ====>|                    |
    |                   |  (non-subscribed |                    |
    |                   |   objects)       |                    |
    |                   |                  |                    |
    |                   |<== walk done ====|                    |
    |<-- all [live] ----|                  |                    |
    |                   |                  |                    |
    |                   |-- save cache --->|----------------->  |
    |                   |                  |  DM + fresh values |
    |                   |                  |                    |
```
