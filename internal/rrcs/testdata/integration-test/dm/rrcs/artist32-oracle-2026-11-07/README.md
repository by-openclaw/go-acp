# Artist 32 oracle models — 2026-11-07

The device models `dhs consumer rrcs extract` built from the Riedel oracle
(Artist 32 "M90", RRCS 9.0.RR1-11): the node, and one model per client
card (AES67 bay 1, CAT5 bays 2 and 4, MADI bay 3).

To have a template for every port type, one port of each type was
created on the free CAT5 positions, extracted and deleted again, in three
rounds; the rounds are merged here, one template per type per card model.
33 port types: 31 on CAT5 (panels, beltpacks, 4-wire, 2-wire in/out,
telephone codec), FOUR_WIRE_SPLIT on AES67 and MADI. The types that need
an IP card (SIP, Stage, NSA, PQ, VCP, beltpacks over IP, trunk line,
codec connection, unicast 4-wire) were refused on CAT5 and the oracle's
AES67 card is out of order (ClientFailed).
