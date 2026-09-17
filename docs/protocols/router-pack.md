# Router pack — the uniform export file-set (#738)

One `export` invocation per router-capable connector writes one
self-describing folder — the input for the per-device acceptance
report and the cross-protocol correlated report. Default location:
the ADR-0028 snapshot home, `snapshots/<proto>/<target>/`.

## Layout

| File | Content | Written by |
|---|---|---|
| `meta.json` | pack_version, protocol, target, generated_at, tool version/commit | every connector, always |
| `<p>-matrix.csv` | ADR-0023 descriptor: `matrix,behavior,targets,sources,max_connects_per_target,max_total_connects,label` | cerebrum-nb*, probel-sw08p, probel-sw02p, emberplus |
| `<p>-xpoint.csv` | crosspoints, canonical grammar `dest,srce,levels` (+ protocol extras as appended columns, e.g. sw08p `matrix_id`) | all four |
| `<p>-src.csv` / `<p>-dst.csv` | label tables — columns are the protocol's label axis (alt-mnemonics on cerebrum-nb, widths 4/8/12/16 on sw08p, label groups on ember+) | cerebrum-nb, sw08p, ember+ |
| `<p>-level.csv` | level mnemonics | cerebrum-nb |
| `<p>-cat-src/-dst/-mixed.csv` | categories | cerebrum-nb (route master only) |
| `<p>-lock.csv` / `<p>-protect.csv` | locks (nb) / protects (sw08p) | cerebrum-nb, sw08p |
| `tree.json` | canonical object tree | tree-model protocols via `extract` (ADR-0022) |

\* cerebrum-nb descriptor: pending upstream of its matrix row.

## Rules

1. **Omit, don't fake.** A file is absent iff the protocol has no such
   concept on the wire (SW-P-02 has no label commands → no src/dst
   files; ember+/probel have no categories → no cat files). An absent
   file means "concept does not exist here", an empty file means "the
   router has zero of them" — never conflate the two.
2. **Column-level parity inside shared files.** The `levels` column is
   present in every `-xpoint.csv`; protocols without levels write `0`
   (tree matrices). One grammar → one parser, one differ, one import.
3. **Evolvability.** `meta.json:pack_version` bumps on any format
   change. New concept = new FILE. New attribute = new COLUMN appended
   and keyed by header name (parsers index by header, never by
   position). Readers ignore unknown files/columns and hard-fail on
   missing required ones. Old packs stay readable forever; sw08p's
   legacy `matrix_id,level_id,dst_id,src_id` xpoint files remain
   importable via header sniffing.
4. **Wire evidence rides along, optionally.** `--capture FILE.jsonl`
   records the raw frames of the same invocation (offline decode via
   `validate`, ADR-0021); `--log` keeps the slog trail. Both live in
   their ADR-0028 homes (`captures/<proto>/<target>/`). cerebrum-nb
   captures contain the LOGIN in cleartext — treat as secrets, never
   commit.
5. **Determinism.** Exporting twice against unchanged state yields
   byte-identical facet files (meta.json differs only in
   generated_at). `import --check` against a just-exported pack is
   `would_change=0`.

## Per-connector invocation

```bash
dhs consumer cerebrum-nb  export <host>            # full set incl. cats/locks (RM)
dhs consumer probel-sw08p export <host> --matrix N # descriptor+labels+xpoints+protect
dhs consumer probel-sw02p --dsts N export <host>   # descriptor+xpoints
dhs consumer emberplus    export <host> --path <matrix> --out-dir DIR
dhs consumer <tree-proto> extract <host>           # tree.json (DM triple, ADR-0022)
```
