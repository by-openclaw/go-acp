# prism consumer — CLI

Telestream PRISM waveform monitor, REST/JSON. Wire context:
[`../CLAUDE.md`](../CLAUDE.md). Never tested on a real PRISM.

## Target

```bash
H=<prism-ip>                      # port 9000; <prism-ip>:80 if 9000 does not answer
# export DHS_PRISM_PASS='<pw>'    # sent on every call as "Authorization: Basic <pw>", in clear over HTTP
# export DHS_PRISM_SCALAR=bare    # one number as 3 instead of [3]
# export DHS_PRISM_ANSWER_WAIT=2s # one answer's wait in walk, watch and export (default 5s)
```

## Verbs

| Verb | What it sends | Example |
|---|---|---|
| `info` | one GET per identity call | `dhs consumer prism info $H` |
| `walk` | one GET per GET-able call and scope, 1 460 URLs in all | `dhs consumer prism walk $H` |
| `walk --path` | the same, for one call | `dhs consumer prism walk $H --path waveform_gain.tile1` |
| `get` | one GET | `dhs consumer prism get $H --path waveform_mode/tile1` |
| `set` | one POST, one GET | `dhs consumer prism set $H --path waveform_mode/tile1 --value WAVEFORM_MODE_RGB --write-to $H` |
| `watch` | the GETs again, every 2 s or `--interval` | `dhs consumer prism watch $H --path waveform_gain` |
| `export` | the walk, to a file | `dhs consumer prism export $H --format json --out prism.json` |

## Paths

`name`, `name/scope` or `name.scope`, with an optional query:

```text
waveform_mode/tile1
timing_ref_measure_summary/SDI1-4
system_status?field=object.fans.state
inputConfigure?input=3
```

Quote a path with `?`. A call that needs a scope refuses to go without
one. A scope the catalogue does not list is passed on: the page
disagrees with itself on a few.

`walk --path` and `export --path` take the dotted form only: the
display filter splits on dots.

## Values

| Datatype | `--value` | Body |
|---|---|---|
| ints with named options | `WAVEFORM_MODE_RGB`, or an integer | `{"object":["WAVEFORM_MODE_RGB"]}` |
| ints | `4`, or `1,0` for an array | `{"object":[4]}` |
| floats, doubles | `-20.5` | `{"object":[-20.5]}` |
| quantity | `0.4,194.5 mV` | `{"object":["0.4","194.5 mV"]}` |
| string | `L1` | `{"object":"L1"}` |
| object | JSON text | `{"object":<text>}` |
| `activeInput`, `inputConfigure` | JSON text | the text as it stands |

An option outside the enum, a bad number or broken JSON stops at exit 2
and nothing is sent. Single-number forms are open question 6.

## What set prints

`confirmed` is the read-back: what the instrument holds, not what was
sent. If they differ, a warning and `prism_readback_differs` say so. A
POST-only call cannot be read back, and the value printed says
`(sent, no read-back on this call)`.

Guarded calls need `DHS_PRISM_ALLOW_ACTIONS=1`; the list is in
[`../CLAUDE.md`](../CLAUDE.md).

## What walk prints

Value blank means no value: the reason is in the stderr warning for that
call, and in the `error` field of that entry in `export --format json`,
with `skipped` on a call walk does not read. The last stderr lines count
them. CSV and YAML export have no field for these and show the value
absent, so bring JSON back.

A refused call never stops a walk. Five transport failures in a row do.

## What watch prints

The first value of each URL, then its changes. With no `--path` it polls
`system_status`, `input_status`, `ptp_status`, `ip_signal_status/IP1`
and `/IP2`. A read that fails prints nothing: `--log-level debug` shows
it, as the shared monitor logs it there.

## Output seen against the test fake

```text
$ dhs consumer prism get 127.0.0.1:18000 --path waveform_gain/tile1
value = 4

$ dhs consumer prism set 127.0.0.1:18000 --path waveform_gain/tile1 --value 7 --write-to 127.0.0.1:18000
confirmed value = 7
```

These come from a fake built from the page, not from a real PRISM.
