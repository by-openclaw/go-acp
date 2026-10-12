# Test on a federation: move one resource between local and federated

A self-contained test for a Cerebrum that has a federation. It needs one
binary and one small file; nothing from our platform. It moves **one**
source from local to federated and back, and writes down what Cerebrum
really does.

Time: about 15 minutes. Risk: one source of your choice changes its link
twice; nothing is created and nothing is deleted.

## 1. What you need

| # | Item | Where |
|---|---|---|
| 1 | The `dhs` binary, version **v0.45.0** | <https://github.com/by-openclaw/go-acp/releases/tag/v0.45.0> — `dhs_windows_amd64.zip` or `dhs_linux_amd64.tar.gz` |
| 2 | The address of the Cerebrum | for example `10.1.2.3` |
| 3 | Its RCP HTTP port | the "HTTP" port of *Enable RCP* in Cerebrum (8080 unless changed) |
| 4 | A Cerebrum account allowed to edit the RouteMaster | user and password |
| 5 | One **local source** to move, and one **federation source** to link it to | choose a source nobody is using |

## 2. Two rules for this test

| # | Rule | Why |
|---|---|---|
| 1 | Never add `--prune` | with it, everything the file does not name is removed from the RouteMaster |
| 2 | Never add `--state absent` | it deletes the resources the file names |

## 3. Prepare

Windows (PowerShell), in the folder where you unpacked the zip:

```powershell
$env:DHS_CEREBRUM_USER = "your-user"
$env:DHS_CEREBRUM_PASS = "your-password"
$H = "10.1.2.3"; $P = 8080
```

Linux:

```sh
export DHS_CEREBRUM_USER=your-user
export DHS_CEREBRUM_PASS=your-password
H=10.1.2.3; P=8080
```

In the commands below, on Windows write `.\dhs.exe` and `$H`, `$P`; on
Linux `./dhs` and `$H`, `$P`.

## 4. Steps

| # | Step | Command | What you should see |
|---|---|---|---|
| 1 | The server answers, and has a federation | `dhs consumer rcp info $H --port $P` | `api_version`, your user, and a number above 0 on the line `federation-sources` |
| 2 | Keep a copy of the RouteMaster | `dhs consumer rcp export $H --port $P --plan > before.json` | a file `before.json`; keep it |
| 3 | Find the two names | `dhs consumer rcp list $H --port $P --kind sources` then the same with `--kind federation-sources` | the name of your local source and of the federation source, exactly as written |
| 4 | Write the file `move.json` | see 5.1 | — |
| 5 | Dry run | `dhs consumer rcp ensure $H --port $P --plan move.json --check` | one line saying **link** for your source; last line `would_change=1 pending=0` |
| 6 | Move local → federated | `dhs consumer rcp ensure $H --port $P --plan move.json` | the same line; last line `changed=1 pending=0` |
| 7 | Look at the source | `dhs consumer rcp list $H --port $P --kind sources`, and the RouteMaster in Cerebrum | the source now shows `federation=N`; in Cerebrum, its "Federation" column is filled |
| 8 | Run step 6 again | same command | last line `changed=0 pending=0`, and **no new source** in the list |
| 9 | Write the file `back.json` | see 5.2 | — |
| 10 | Dry run, then move federated → local | step 5 then step 6 with `back.json` | a line saying **unlink**; `changed=1` |
| 11 | Compare with the copy | `dhs consumer rcp export $H --port $P --plan > after.json` and compare with `before.json` | this answers question 3 of section 6 |

## 5. The two files

Replace the two names by yours, exactly as the lists of step 3 show them.

### 5.1 `move.json` — local to federated

```json
{
  "sources": [
    { "mnemonic": "NAME OF YOUR LOCAL SOURCE", "federation": "NAME OF THE FEDERATION SOURCE" }
  ]
}
```

### 5.2 `back.json` — federated to local

After the move, Cerebrum may show your source under the name of the
federation source. Use the name the list of step 7 shows.

```json
{
  "sources": [
    { "mnemonic": "NAME THE SOURCE HAS NOW", "federation": "" }
  ]
}
```

For a destination, write `"destinations"` in place of `"sources"`, and
use `--kind destinations` and `--kind federation-destinations` in the
lists.

## 6. What to write down

These are the points we could not learn without a federation.

| # | Question | Where to look | Answer |
|---|---|---|---|
| 1 | Does the move work with one update, with no delete and no create? | steps 6 and 8 | |
| 2 | After the link, does the source keep its own name, or take the name of the federation source? | step 7 | |
| 3 | After the unlink, does the source get back its own levels, tags and tie line settings? | step 11 | |
| 4 | How long does Cerebrum take to apply a link? | step 6 | |
| 5 | Which RCP version does this Cerebrum answer? | step 1, `api_version` | |

## 7. If a command stops

| Message | Meaning | What to do |
|---|---|---|
| `the server accepted federation N and the resource still reads 0` | Cerebrum answered "accepted" and did not apply the link — what a Cerebrum without federation does | check step 1; note it as the answer to question 1 |
| `federation sources "…" does not exist` | the federation name in the file is not in the list | copy the name again from step 3 |
| `no such local resource to link` | the local name in the file is not in the list | copy the name again from step 3 |
| `is linked to federation … and states levels` | the file gives levels to a linked source | remove everything but `mnemonic` and `federation` |
| `HTTP 401` or a login error | wrong account, or not allowed | check the two environment variables |
| no answer | wrong address or port, or the RCP HTTP port is closed by the Windows firewall of the Cerebrum server | check *Enable RCP* and the firewall rule for that port |
