# virtuoso runbook — at a customer site

**This connector has never run on a real Virtuoso.** The first run is this
one. The chassis is the customer's production. Read before typing.

Rules for the whole visit:

- Reads first, and only reads, until the output is understood.
- A write only with the customer's explicit agreement, on a field they name,
  by the procedure below.
- If anything is not what this page says, stop and bring the output back.

## 1. Before you go

- Build from a released version; put the binary on the laptop or jump host
  that reaches the chassis's management network.
- Get from the customer: the address, an account, and the level of that
  account. A read-only account is enough for sections 3 and 4.
- Store the account in Vault. Nothing else holds it: no file, no note, no
  shell history.

## 2. Credentials at run time

Fetch from Vault into the environment of the shell that runs dhs, and only
there. The path is whatever your site entry in Vault is:

```bash
export DHS_VIRTUOSO_USER=$(vault kv get -field=user <path>)
export DHS_VIRTUOSO_PASS=$(vault kv get -field=pass <path>)
H=<chassis-ip>
```

When HTTP redirects to HTTPS, `info` says so. Then:

```bash
export DHS_VIRTUOSO_TLS=1
# export DHS_VIRTUOSO_INSECURE=1   # only for a self-signed certificate, with the customer's agreement
```

Unset both variables when you are done.

## 3. First contact: reads, from small to large

Each step depends on the one before. Stop at the first surprise.

| # | Command | Traffic | What to look at |
|---|---|---|---|
| 1 | `dhs consumer virtuoso info $H` | one small GET per slot | model, software on every module; which cards are absent, and the reason |
| 2 | `dhs consumer virtuoso get $H --path dev/product_info` | one small GET | the same as `info` for slot 0 |
| 3 | `dhs consumer virtuoso get $H --path dev/product_info/swver` | one small GET | a single field: open question 1 |
| 4 | `time dhs consumer virtuoso walk $H --slot 0` | schema + data, about 1.2 MB | object count, duration, warnings on stderr |
| 5 | `time dhs consumer virtuoso walk $H --slot 1` | schema + data, about 1.9 MB | the same on one card |

Expect about 3 000 objects on the manager and 3 000 to 4 500 on a card: the
counts the codec gave on a capture, read offline.

`info` waits up to 20 s for each card that does not answer. Up to about three
minutes on a chassis full of dead slots is not a hang.

No step here changes the device. Each run opens a session and closes it at the
end. A run that was killed leaves its session to expire in 3 minutes.

Only then, and outside a critical moment:

```bash
dhs consumer virtuoso walk $H --all
dhs consumer virtuoso export $H --format json --out chassis.json
```

## 4. Watch

A watch re-reads each field with one GET, at most 5 a second, so name the
branch you want:

```bash
dhs consumer virtuoso watch $H --slot 1 --path alarms --interval 60s
```

A watch with no `--path` reads every field of the module, thousands, and takes
over ten minutes per round. Ctrl-C ends it and logs out.

## 5. A write

Only with the customer's explicit agreement, on the field they named, for the
time they named. Pick a harmless field for the first one, such as
`dev/product_info/location`.

Write, read, restore, and restore means the exact previous value:

```bash
P=dev/product_info/location
dhs consumer virtuoso get $H --path $P           # 1. read it, write the value down
dhs consumer virtuoso set $H --path $P --value "new value" --write-to $H   # 2. write: prints the value read back
dhs consumer virtuoso get $H --path $P           # 3. read it again
dhs consumer virtuoso set $H --path $P --value "<the value from step 1>" --write-to $H   # 4. restore
dhs consumer virtuoso get $H --path $P           # 5. prove it is back
```

Show step 5 to the customer. A field that is empty in step 1 is restored with
`--value ""`: open question 9 says that is unproven, so pick a field that is
not empty.

`set` refuses what the schema says it cannot do, with nothing sent. A
`write-rejected` means the device said no: bring back its text, do not retry
in another way. A `read-back failed` means the write may have landed: read the
field by hand.

Never, by any tool, during this visit without a change window: `dev/system/do_reset`,
a configuration load, a software activation, an RPC of any kind. This connector
invokes none of them.

## 6. What to bring back

Bring the outputs below. Anything else stays at the site.

| Output | How |
|---|---|
| `info` | `dhs consumer virtuoso info $H > info.txt 2>&1` |
| `walk` of slot 0 and one card | `> walk-0.txt 2>&1`, with the `time` line |
| stderr of every run | warnings and the `virtuoso compliance` line |
| one `--capture auto` of `info` | only if a question below needs it |

Scrub before it leaves the site. Replace device names, addresses, serials, UUIDs,
user names, contact and location strings. Check with a search for the site's
address prefix, its device-name prefix and each user name:

```bash
grep -rnE '<site-prefix>|<devname-prefix>|<user>' info.txt walk-*.txt
```

Note what dhs itself wrote on disk: `.cache/` and `snapshots/` and `captures/`
next to where it ran hold the device's values. Delete them from a borrowed
machine. A capture holds no password and no login exchange, but it does hold
every value that was read. Never commit any of this.

## 7. When it goes wrong

| Symptom | Meaning | Do |
|---|---|---|
| `login refused` | wrong account | check Vault, do not retry in a loop |
| `401 unauthorized` | no credentials in the environment | section 2 |
| `HTTP 301` or `302` at connect | HTTP redirects to HTTPS | `DHS_VIRTUOSO_TLS=1` |
| `no_card` with reason `no answer` | timeout or connection drop | compare with the GUI: open question 3 |
| `device status "failed": …` | the device refused; its text follows | bring it back |
| a slow `walk` | 20 s without an answer ends it | do not raise anything: bring the `time` line back |
| `virtuoso_*` in the compliance line | a deviation from the guide | bring the line back |

## Open questions, settle at the customer site

Each is what the guide leaves open and the connector has an answer to on a
fake only.

| # | Question | What the connector does today | Settled by |
|---|---|---|---|
| 1 | Does GET on a single field answer? The guide says any level and shows objects; only `dev/product_info.json` was measured. | GET the field. `get` on the object is the fallback. | section 3, step 3 |
| 2 | What level does a PATCH need? The guide adds `&level=2` to a PUT of network settings and says nothing for PATCH. | Sends no `level`. A refusal shows the device's text. | the first agreed write, and the account's level |
| 3 | What does an empty slot answer: 404, 502, a timeout, a page? | All of them are "absent". | `info` on a chassis with an empty slot |
| 4 | Does an expired session answer 401 on `/slot/N` too? | Re-logs in on 401 only. | `get --path` on a card, after 3 idle minutes in one process, needs a long `watch --interval 4m` |
| 5 | Is a PATCH applied when its answer returns? | Reads back at once. A difference is reported as `write-coerced`. | section 5, step 2 |
| 6 | Is 20 s enough for a card's schema on a loaded chassis? It is the script's value, not a measurement. | 20 s per answer. | the `time` lines of section 3 |
| 7 | What poll rate is safe? 5 reads a second is a choice, not a limit from the guide. | One read per 200 ms. | the customer's view, and the device's CPU in `dev` during a watch |
| 8 | How many sessions does a chassis take, and do ours compete with the GUI or VideoIPath? | One session per dhs run, logged out at the end. | the customer, or the GUI's session list if it has one |
| 9 | Does `field=` with an empty value clear a string? | Sends it. | an agreed write on a field that is safe to clear |
| 10 | Is HTTPS on, and with which certificate? Is the redirect on? | `DHS_VIRTUOSO_TLS` and `DHS_VIRTUOSO_INSECURE`. | section 2 |
| 11 | With two Element Managers, A and B, does one address reach the active one? | One endpoint. | the customer's cabling and the GUI |
| 12 | `Model@SwRev` is `prodname@swver`. Is that enough, or does a media function change the schema under one name? | That key names the DM cache. | compare the `walk` of two cards with the same name |
| 13 | A capture held two fields the schema does not declare (`severity`, `subid3` in a `subid3_coll`). Is that general? | Listed read-only, `virtuoso_data_not_in_schema`. | the compliance line |
