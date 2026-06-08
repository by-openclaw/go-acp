# scripts/acp1/verify-diff.ps1 - acp1 `diff` integration test (offline compare).
# Captures a canonical tree (via extract), then: identical -> "no changes";
# a mutated copy -> changes reported. `diff` is offline; reached via the
# consumer dispatcher (accepts the injected --protocol, which it ignores).
. "$PSScriptRoot\_lib.ps1"
$t = Resolve-Acp1Target
$dhs = Resolve-Dhs
$dir = Join-Path $env:TEMP "acp1-diff-$PID"

& $dhs consumer acp1 extract $t --manufacturer axon --product rrs18 `
    --direction consumer --version 1601 --out $dir --slot 0 2>$null | Out-Null
$tree = Join-Path $dir 'tree.json'
Check 'tree captured for diff' (Test-Path $tree) $tree

# identical inputs -> no changes
$same = (& $dhs consumer acp1 diff $tree $tree 2>$null) -join "`n"
Check 'identical trees report no changes' ($same -match 'no changes') $same

# Mutated copy: diff is a SCHEMA diff (access/type/unit/enum/min-max/add-remove),
# not a value diff, so flip a read-only object to readWrite. The trailing comma
# avoids also matching "readWrite". Write WITHOUT a BOM ([IO.File]::WriteAllText)
# — Set-Content -Encoding utf8 on PS 5.1 emits a BOM that breaks the JSON parse.
$mod = Join-Path $dir 'tree-mod.json'
$mutated = (Get-Content $tree -Raw) -replace '"access": "read",', '"access": "readWrite",'
[System.IO.File]::WriteAllText($mod, $mutated)
$chg = (& $dhs consumer acp1 diff $tree $mod 2>$null) -join "`n"
Check 'mutated tree reports changes' (($chg -notmatch 'no changes') -and ($chg.Trim().Length -gt 0)) $chg

Remove-Item -Recurse -Force $dir -ErrorAction SilentlyContinue
Complete-Checks 'diff'
