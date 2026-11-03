# prism runbook — first run at the customer site

**This connector has never met a real PRISM.** Each step below settles
open questions from [`../CLAUDE.md`](../CLAUDE.md). It is the customer's
instrument: reads first, one write only, restore what you found.

Keep everything in one folder and bring it back.

```bash
IP=<prism-ip>
mkdir prism-site && cd prism-site
```

## Step 1 — find the port

No dhs yet. Which port answers, and how a scalar comes back:

```bash
curl -i --max-time 5 http://$IP:9000/api/software_version | tee port-9000.txt
curl -i --max-time 5 http://$IP:80/api/software_version   | tee port-80.txt
```

Take the port that answers JSON and keep it in `H` for every step below:

```bash
H=$IP:9000      # or H=$IP:80
```

If neither answers JSON, stop: ask for the port before going further.
Settles questions 1, 3, 5.

## Step 2 — bring back the instrument's own catalogue

```bash
curl -s --max-time 10 http://$H/api/apiHelp -o apiHelp.json
curl -s --max-time 10 http://$H/api/nosuchcall -i | tee error-shape.txt
```

`apiHelp.json` is what the page renders from: the real scope family of
every call, and which ones need the password. `error-shape.txt` shows
what a refusal looks like. Settles questions 4, 12, 14.

## Step 3 — identity

```bash
dhs consumer prism info $H --timeout 3s 2>&1 | tee info.txt
```

Expected: `software_version` and the other identity calls. A line ending
in `.error` is a call that failed, with the reason. A 401/403 means the
password: ask for it, `export DHS_PRISM_PASS='<pw>'`, run again.

## Step 4 — one call of each datatype

```bash
for c in serial_number waveform_mode/tile1 waveform_gain/tile1 audio_program_level \
         waveform_position/tile1 system_status; do
  echo "== $c"; dhs consumer prism get $H --path $c --timeout 3s
done 2>&1 | tee gets.txt
```

Also the raw answers, the only proof of the wire shape:

```bash
for c in waveform_mode/tile1 waveform_gain/tile1 audio_program_level; do
  curl -s --max-time 5 http://$H/api/$c; echo
done | tee raw-gets.txt
```

Settles questions 5, 6, 7.

## Step 5 — the walk

About 1 460 reads, one at a time, nothing written. Say so before you
start. It stops on its own if the instrument stops answering.

```bash
dhs consumer prism export $H --format json --out walk.json --capture capture-walk 2>walk.log
```

Expected last lines in `walk.log`: `prism: walk done read=… failed=…
skipped=…`, then `deviations from the page` if any. A refusal on a call
the model does not have is normal. `capture-walk/` carries
`DHS_PRISM_PASS` in clear if one is set: treat it as secret.

## Step 6 — watch, one minute

```bash
timeout 60 dhs consumer prism watch $H --path waveform_gain --interval 2s 2>&1 | tee watch.txt
```

Change the gain of tile 1 on the front panel once while it runs. The
change must show. On Windows, stop it with Ctrl-C.

## Step 7 — one write, restored

Only with the customer's agreement, and on a call that changes a display,
not a signal path. `waveform_gain/tile1` is the proposal: it changes a
trace on one tile.

Read, and note it:

```bash
dhs consumer prism get $H --path waveform_gain/tile1 --timeout 3s | tee before.txt
curl -s --max-time 5 http://$H/api/waveform_gain/tile1 | tee before-raw.txt
```

Write a different legal value, 1 to 10, here 2 (use 3 if `before` is 2):

```bash
dhs consumer prism set $H --path waveform_gain/tile1 --value 2 --write-to $H --timeout 3s 2>&1 | tee write.txt
```

Expected: `confirmed value = 2`, and the trace changes. If the set fails
or the value did not move, run it again with the other form, once:

```bash
DHS_PRISM_SCALAR=bare dhs consumer prism set $H --path waveform_gain/tile1 --value 2 --write-to $H --timeout 3s 2>&1 | tee write-bare.txt
```

Restore the exact previous value, in the form that worked:

```bash
dhs consumer prism set $H --path waveform_gain/tile1 --value <value from before.txt> --write-to $H --timeout 3s 2>&1 | tee restore.txt
curl -s --max-time 5 http://$H/api/waveform_gain/tile1 | tee after-raw.txt
```

`after-raw.txt` must equal `before-raw.txt` byte for byte. If it does not,
fix it on the instrument before you leave and say so. Settles questions
6, 7, 8, 10, 11.

## What to bring back

The whole `prism-site` folder, and the firmware version read in step 3.

| File | Settles |
|---|---|
| `port-9000.txt`, `port-80.txt` | port, TLS, scalar wrapper |
| `apiHelp.json`, `error-shape.txt` | catalogue, scopes, error shape |
| `info.txt`, `gets.txt`, `raw-gets.txt` | identity, value shapes |
| `walk.json`, `walk.log`, `capture-walk/` | what this instrument answers; future replay fixture and DM |
| `watch.txt` | polling |
| `before*.txt`, `write*.txt`, `restore.txt`, `after-raw.txt` | the POST body and the restore |

## Common failures

| Symptom | Meaning |
|---|---|
| `connection refused`, `try <host>:80` | wrong port, step 1 |
| `not authorized` | set `DHS_PRISM_PASS`; if that fails, the base64 form is open question 2 |
| `the answer is not a value` with `prism_reply_not_json` | an HTML page answered: wrong port, or a login |
| `prism_scalar_form_differs` on every number | read `raw-gets.txt`, then set `DHS_PRISM_SCALAR=bare` |
| `validation: ...: guarded` | a call that stops or rewrites the instrument. Do not open it here |
