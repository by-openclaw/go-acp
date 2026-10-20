# RRCS — DM + manifest fixture (ADR-0025 deliverable 4)

```
dm/rrcs/ARTIST_1024@9.0.RR1-11.json   the node type: its properties, and the system's types as templates
dm/rrcs/AES67@9.0.RR1-11.json         the client card type: its properties, one template per port type
manifest/rrcs/192.0.2.10.json         a small inventory that refers to them
```

The two models were written by `dhs consumer rrcs extract --from` on the
walk of a production RRCS 9.0.RR1-11 (2026-10-13): the shape of every
type the system had — six port types, the key, the 26 command types,
conference, group, IFB, logic source — with kinds, bounds, named values
and defaults from the specification's tables, and the properties the
walk showed beyond them marked `observed`. A model carries no value of
the plant (every `value` is null; the `value` keys inside `enumMap` are
the numbers of named values).

The manifest is generated from the synthetic system of
`consumer/dm_test.go` (`TestFixtureManifest` rewrites it with
`RRCS_UPDATE_FIXTURE=1` and otherwise checks it is in sync): a node, two
cards, a split port with its linked neighbour, a panel, a beltpack. It
refers to the two models above by identity.
