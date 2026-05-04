# tx 003 TALLY (SW-P-02 §3.2.5)

| Direction | Cmd byte | Spec |
| --- | ---: | --- |
| matrix → controller | 3 | §3.2.5 |

Spontaneous broadcast: "this dst is now connected to src." Emitted to
every connected session whenever the matrix applies a route — single-
shot connect (cmd 2), salvo commit (cmd 6), or local panel.

## Wire shape

```
SOM 0x03 <multiplier> <dst> <src> <chk>
```

## Fixtures (when captured)

- `frames.jsonl` — a representative tally storm following a salvo
  commit, captured against Lawo VSM or Commie.
- `capture.pcapng` + `tshark.tree` — dissector artefacts.
