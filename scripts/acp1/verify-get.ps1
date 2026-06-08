# scripts/acp1/verify-get.ps1 - acp1 `get` integration test.
# Reads a known enum object and asserts value + kind + enum items.
. "$PSScriptRoot\_lib.ps1"
$t = Resolve-Acp1Target
$dhs = Resolve-Dhs

$txt = (& $dhs consumer acp1 get $t --slot 0 --group control --label Broadcasts 2>$null) -join "`n"
Check 'returns a value'      ($txt -match 'value\s*=') $txt
Check 'reports enum kind'    ($txt -match 'kind\s*=\s*enum') $txt
Check 'lists enum items'     ($txt -match 'items\s*=\s*\[Off, On\]') $txt
Complete-Checks 'get'
