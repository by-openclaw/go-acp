# virtuoso consumer — CLI

Nevion Virtuoso MI. Wire context: [`../CLAUDE.md`](../CLAUDE.md). Before
touching a chassis in service: [`runbook.md`](runbook.md).

## Account and target

```bash
export DHS_VIRTUOSO_USER='<user>'
export DHS_VIRTUOSO_PASS='<password>'
H=<chassis-ip>             # port 80 (HTTP); <chassis-ip>:443 = HTTPS
# export DHS_VIRTUOSO_TLS=1        # HTTPS on any port
# export DHS_VIRTUOSO_INSECURE=1   # self-signed certificate
```

Credentials come from the environment only. There is no flag for them, and
they reach neither the log nor a `--capture` file.

## Verbs

| Verb | What it sends | Example |
|---|---|---|
| `info` | login, then `GET .../data/dev/product_info.json` per module, one after the other | `dhs consumer virtuoso info $H` |
| `walk` | `GET .../schema.json`, then `GET .../data` | `dhs consumer virtuoso walk $H --slot 1` |
| `walk --all` | the same for every module that answers | `dhs consumer virtuoso walk $H --all` |
| `get` | `GET .../data/<path>` | `dhs consumer virtuoso get $H --path dev/product_info/swver` |
| `set` | `GET .../schema.json` once, `PATCH .../data/<object>?<field>=<value>`, `GET` the field | `dhs consumer virtuoso set $H --path dev/product_info/location --value "rack 4"` |
| `watch` | `GET` per field, at most 5 a second | `dhs consumer virtuoso watch $H --slot 1 --path alarms --interval 60s` |
| `export` | the walk of every present slot, to a file | `dhs consumer virtuoso export $H --format json --out chassis.json` |

Slot 0 is the Element Manager, slots 1..8 the cards. `--slot` defaults to 0.
`watch` with no `--slot` watches the manager.

## Paths

A path is the URL path under `/data`: `dev/product_info/swver`. A path with no
`/` is split on dots, `dev.product_info.swver`, like every other connector. A
collection key may hold dots, so such a path must be slashed:
`network/routev4_static_coll/0.0.0.0:0:10/gw`.

`get` on an object answers its JSON text. `walk --path` is a display filter on
dotted segments, applied after the whole module was read.

## What walk shows

The object list comes from the module's own schema, with collections expanded
by the keys in the data:

| Object | Meaning |
|---|---|
| kind `bool`, `int`, `uint`, `float`, `enum`, `string` | from the schema; `R--` = `readOnly`, `RW-` writable |
| `meta.noValue` | the schema declares it, the data does not hold it |
| `meta.undeclared` | the data holds it, the schema does not: compliance event |
| `meta.key` | a collection key the data repeats: read-only |
| `meta.rpc`, access `---` | a method. Listed, never invoked |

## What set refuses, with nothing sent

From the module's schema: an unknown path, a read-only field, a collection key,
an object, an RPC, a value out of `minimum`/`maximum`, a value not in the enum,
a value that does not fit the type, `format` or `pattern`. An enum takes its
label or its number and sends the number.

| Outcome | Error |
|---|---|
| refused by the device | `session:write-rejected`, with the device's text |
| accepted, reads back different | `session:write-coerced`, naming both values |
| accepted, read-back failed | an error saying exactly that: **the write may have landed** |

## Errors and absent cards

- A card that does not answer (no connection, timeout, a status other than 200,
  a page that is not the API) is `no_card` in `info`, with the reason, and is
  skipped by `walk --all` and `export`. A `walk --slot N` on it says the slot
  is absent.
- A module that answers with `info.status` other than `ok` is an error,
  and `info.errors` is its text.
- `info.warnings` and `info.errors` on an `ok` answer go to the log as warnings,
  `info.notes` as info.
- The session is re-established once after a 401. A second 401 is an error.
- Deviations from the guide are counted as compliance events, listed in
  [`compliance_events.go`](../consumer/compliance_events.go), and summed in a
  log line at the end of the run.

## Output against the scratch fake

```text
$ dhs consumer virtuoso get 127.0.0.1:18080 --path dev/product_info/swver
value = "0.0.1"

$ dhs consumer virtuoso set 127.0.0.1:18080 --path dev/product_info/location --value "rack 4"
confirmed value = "rack 4"
```

Synthetic data from a fake, not from a Virtuoso.
