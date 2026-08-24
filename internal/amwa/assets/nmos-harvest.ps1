<#
  nmos-harvest.ps1 - collect the full x-nmos tree from NMOS devices into assets.

  READ-ONLY: every request is a GET. Nothing is written to any device, no
  staged/active change, no activation. Safe on production gear.

  Usage:
    .\nmos-harvest.ps1 -Target 10.44.72.18:3000
    .\nmos-harvest.ps1 -Target 10.44.72.18:3000,10.44.72.24:3000
    .\nmos-harvest.ps1 -Target C:\yob\nodes.txt        # one host:port per line

  Output (beside this script unless -Out is given), one folder per device:
    <host>_<timestamp>\
       tree.json    - everything in one document (the asset to send back)
       raw\*.json   - each endpoint verbatim
       sdp\*.sdp    - sender + connected-receiver SDPs
       report.txt   - what answered, what 404'd, what failed

  Works against a NODE or a REGISTRY - it reads /x-nmos first and adapts.
  Pointed at a registry it also follows each registered node (-MaxNodes caps
  this; a registry lists senders but holds no SDPs, those live on the nodes).
#>
[CmdletBinding()]
param(
  [Parameter(Mandatory)][string[]]$Target,
  [string]$Out = "",
  [int]$TimeoutSec = 8,
  [int]$MaxNodes = 250,
  [switch]$Https,
  [switch]$NoFollow,
  [switch]$AllVersions,   # walk every API version (default: highest only)
  [switch]$Deep,          # IS-05: staged+constraints+transporttype too (default: active only)
  [switch]$NoSdp          # skip SDP fetching entirely (fast inventory pass)
)

$HarvesterVersion = '2026.08.24.16'
$ErrorActionPreference = 'Continue'
$scheme = if ($Https) { 'https' } else { 'http' }
if ($Https) {
  # self-signed certs are the norm on broadcast gear
  try { [Net.ServicePointManager]::ServerCertificateValidationCallback = { $true } } catch { }
  try { [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12 } catch { }
}

if (-not $Out) {
  $Out = if ($PSScriptRoot) { $PSScriptRoot } else { (Get-Location).Path }
}
# -Out may name a folder that does not exist yet; create it rather than fail.
if (-not (Test-Path -LiteralPath $Out)) {
  $null = New-Item -ItemType Directory -Force -Path $Out
}
$Out = [IO.Path]::GetFullPath((Join-Path $Out '.'))

# Expand a file argument into its lines.
# Split on commas too: powershell -File script.ps1 -Target a,b hands the
# whole thing over as ONE string, unlike an in-session call.
$targets = @()
foreach ($t in ($Target | ForEach-Object { $_ -split ',' })) {
  if ((Test-Path -LiteralPath $t -PathType Leaf) -and ($t -notmatch ':\d+$')) {
    $targets += @(Get-Content -LiteralPath $t |
      ForEach-Object { $_.Trim() } |
      Where-Object { $_ -and -not $_.StartsWith('#') })
  } else {
    $targets += $t.Trim()
  }
}
$targets = @($targets | Select-Object -Unique)
Write-Host "nmos-harvest $HarvesterVersion  ->  $Out"

# ---------------------------------------------------------------- helpers --
function Write-Body {
  # Invoke-WebRequest -UseBasicParsing returns .Content as a BYTE ARRAY for
  # content types it does not treat as text (application/sdp is one).
  # WriteAllText on a byte[] stringifies it to "118 61 48 ..." - the data
  # survives but the file is junk. Branch on the type.
  param($Content, [string]$File)
  $parent = Split-Path $File -Parent
  if ($parent -and -not (Test-Path $parent)) { $null = New-Item -ItemType Directory -Force -Path $parent }
  if ($Content -is [byte[]]) { [IO.File]::WriteAllBytes($File, $Content) }
  else { [IO.File]::WriteAllText($File, [string]$Content) }
}

function Invoke-Harvest {
  param([string]$Device, [string]$Root, [switch]$SkipFollow, [switch]$NoStamp)

  $stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
  $safe  = $Device -replace '[^0-9A-Za-z]', '_'
  $dir   = if ($NoStamp) { Join-Path $Root $safe } else { Join-Path $Root "$safe`_$stamp" }
  # A harvest folder must never be the output root, or raw\ and sdp\ end up
  # loose at the top with nothing to identify them. Compare NORMALISED paths:
  # Resolve-Path returns $null for a path that does not exist yet, and two
  # $nulls compare equal - which made this guard fire on a fresh -Out folder.
  $rootFull = [IO.Path]::GetFullPath((Join-Path $Root '.'))
  $dirFull  = [IO.Path]::GetFullPath((Join-Path $dir  '.'))
  if ([string]::IsNullOrWhiteSpace($safe) -or ($rootFull -eq $dirFull)) {
    throw "refusing to harvest into the output root (device='$Device', dir='$dir')"
  }
  $null  = New-Item -ItemType Directory -Force -Path $dir, (Join-Path $dir 'raw'), (Join-Path $dir 'sdp')

  # Identity FIRST, so an interrupted run still leaves an attributable folder.
  # (Written again at the end with the label, api list and counts filled in.)
  [IO.File]::WriteAllText((Join-Path $dir 'device.json'),
    ([pscustomobject][ordered]@{
      target     = $Device
      role       = 'in-progress'
      started_at = (Get-Date).ToString('o')
      harvester  = 'nmos-harvest.ps1'
      version    = $HarvesterVersion
    } | ConvertTo-Json -Depth 4))

  $report  = [System.Collections.Generic.List[string]]::new()
  $ident   = [ordered]@{ role = 'unknown'; label = ''; id = '' }
  $nodesLt = [System.Collections.Generic.List[object]]::new()
  $sdpSeen = @{}
  $tree = [ordered]@{
    harvested_at = (Get-Date).ToString('o')
    target       = $Device
    harvester    = 'nmos-harvest.ps1'
    version      = $HarvesterVersion
    apis         = [ordered]@{}
  }

  function Get-Json {
    param([string]$Path)
    try {
      $r = Invoke-WebRequest -Uri ($scheme + "://" + $Device + $Path) -TimeoutSec $TimeoutSec -UseBasicParsing
      $report.Add(("{0,-5} {1}" -f $r.StatusCode, $Path))
      $name = ($Path -replace '[^0-9A-Za-z]', '_').Trim('_')
      Write-Body $r.Content (Join-Path $dir "raw\$name.json")
      $txt = if ($r.Content -is [byte[]]) { [Text.Encoding]::UTF8.GetString($r.Content) } else { [string]$r.Content }
      return ($txt | ConvertFrom-Json)
    } catch {
      $code = $_.Exception.Response.StatusCode.value__
      $report.Add(("{0,-5} {1}" -f $(if ($code) { $code } else { 'ERR' }), $Path))
      return $null
    }
  }

  # IS-04 Query API PAGES its collections: it returns up to paging.limit items
  # plus a Link header with rel="next". Taking only the first page silently
  # truncates a plant (seen live: 11 of 68 nodes). Follow rel="next" to the end.
  function Get-JsonPaged {
    param([string]$Path)
    $all = @(); $url = $scheme + "://" + $Device + $Path + "?paging.limit=100"; $page = 0
    while ($url -and $page -lt 500) {
      $page++
      try {
        $r = Invoke-WebRequest -Uri $url -TimeoutSec $TimeoutSec -UseBasicParsing
      } catch {
        $code = $_.Exception.Response.StatusCode.value__
        $report.Add(("{0,-5} {1}" -f $(if ($code) { $code } else { 'ERR' }), $url))
        break
      }
      $txt = if ($r.Content -is [byte[]]) { [Text.Encoding]::UTF8.GetString($r.Content) } else { [string]$r.Content }
      # "[]" -> ConvertFrom-Json yields $null, and @($null) counts as ONE item.
      # Strip nulls or an empty page looks like a page of one.
      $items = @($txt | ConvertFrom-Json) | Where-Object { $null -ne $_ }
      $items = @($items)
      $all += $items
      $report.Add(("{0,-5} {1}  (page {2}, +{3}, total {4})" -f $r.StatusCode, $Path, $page, $items.Count, $all.Count))
      if ($items.Count -eq 0) { break }
      # Link: <url>; rel="next", <url>; rel="prev"
      $link = $r.Headers['Link']
      if ($link -is [array]) { $link = $link -join ',' }
      $prev = $url
      $url = $null
      if ($link -and ($link -match '<([^>]+)>\s*;\s*rel="next"')) { $url = $Matches[1] }
      # DEFENSIVE: a registry whose paging cursor does not advance would loop
      # forever returning the same page (seen live on the dhs registry).
      if ($url -and $url -eq $prev) {
        $report.Add("WARN  paging cursor did not advance for $Path - stopping (registry paging defect)")
        break
      }
    }
    # de-duplicate: a non-advancing or overlapping cursor repeats resources
    $seen = @{}; $uniq = @()
    foreach ($it in $all) {
      $k = if ($it.id) { $it.id } else { ($it | ConvertTo-Json -Compress -Depth 6) }
      if (-not $seen.ContainsKey($k)) { $seen[$k] = $true; $uniq += $it }
    }
    if ($uniq.Count -ne $all.Count) {
      $report.Add("NOTE  $Path returned $($all.Count) rows across pages, $($uniq.Count) unique")
    }
    # NEVER let de-duplication lose data: if it produced nothing (ids absent or
    # an unexpected shape), keep every row as collected.
    if ($uniq.Count -gt 0) { $all = $uniq }
    # One raw file per ENDPOINT, holding every page combined - a per-page dump
    # buries the folder (4 versions x 7 collections x N pages) and the pages are
    # an artefact of transport, not of the data. Page counts stay in report.txt.
    $rawName = ($Path -replace '[^0-9A-Za-z]', '_').Trim('_')
    [IO.File]::WriteAllText((Join-Path $dir "raw\$rawName.json"), (ConvertTo-Json -InputObject @($all) -Depth 30))
    if ($false) {
    }
    if ($page -ge 500) { $report.Add("WARN  paging stopped at 500 pages for $Path") }
    return $all
  }

  function Get-Sdp {
    param([string]$Url, [string]$File)
    if ($sdpSeen.ContainsKey($Url)) { return }      # same SDP is listed under every API version
    $sdpSeen[$Url] = $true
    try {
      $r = Invoke-WebRequest -Uri $Url -TimeoutSec $TimeoutSec -UseBasicParsing
      Write-Body $r.Content $File
      $report.Add(("{0,-5} {1}" -f $r.StatusCode, $Url))
    } catch {
      $code = $_.Exception.Response.StatusCode.value__
      $report.Add(("{0,-5} {1}  [{2}]" -f $(if ($code) { $code } else { 'ERR' }), $Url, $_.Exception.Message))
    }
  }

  Write-Host "Harvesting $Device -> $dir"

  # ---- which APIs and versions does it expose? ----
  $apis = Get-Json '/x-nmos'
  $tree.apis['_root'] = @($apis)
  $apiNames = @()
  if ($apis) { $apiNames = @($apis | ForEach-Object { "$_".TrimEnd('/') }) }
  if (-not $apiNames) {
    $apiNames = @('node','connection','channelmapping','system','query','registration','events')
    $report.Add('WARN  no /x-nmos root - probing the standard API names')
  }

  foreach ($api in $apiNames) {
    $vers = Get-Json "/x-nmos/$api/"
    if (-not $vers) { continue }
    $verList = @($vers | ForEach-Object { "$_".TrimEnd('/') })
    $tree.apis[$api] = [ordered]@{ versions = @($verList); data = [ordered]@{} }
    # Walking every minor re-fetches the SAME resources N times: a 176-sender
    # node over IS-05 v1.0+v1.1 is ~2800 GETs, and 4 node minors quadruple the
    # IS-04 pass. Default to the HIGHEST version; -AllVersions restores the lot.
    $walkVers = $verList
    # The query API MUST be walked at every minor. IS-04 version isolation
    # hides resources registered under a lower minor unless query.downgrade is
    # asked for - seen live on a 45-node registry: v1.1 returned 45, v1.3
    # returned 39. Collapsing to the highest version loses six nodes.
    # A node's own API repeats the SAME resources at every minor, so there
    # collapsing is safe and saves 4x the requests.
    if (-not $AllVersions -and $verList.Count -gt 1 -and $api -ne 'query') {
      $walkVers = @($verList | Sort-Object {
        $m = [regex]::Match($_, 'v(\d+)\.(\d+)')
        if ($m.Success) { [int]$m.Groups[1].Value * 1000 + [int]$m.Groups[2].Value } else { 0 }
      } | Select-Object -Last 1)
      $report.Add("NOTE  $api : versions $($verList -join ',') present, walking $($walkVers -join ',') only (-AllVersions for all)")
    }

    foreach ($v in $walkVers) {
      $base   = "/x-nmos/$api/$v"
      $bucket = [ordered]@{}

      switch ($api) {
        'node' {
          foreach ($res in 'self','devices','sources','flows','senders','receivers') {
            $got = Get-Json "$base/$res"
            # @(...) is LOAD-BEARING. A PowerShell function returning a
            # one-element array unrolls it to the bare element, so a node with
            # exactly one Device recorded "devices": {..} instead of [{..}].
            # Read strictly, that node has no devices and every Sender, Flow and
            # Receiver on it dangles - 18104 false findings on one real 7-node
            # capture. /self is a single object and must NOT be wrapped.
            # Direct assignment inside each branch, never $x = if (..) {..}:
            # a statement block's output unrolls a one-element array exactly
            # like 'return' does, which is the very bug this guards against.
            if ($res -eq 'self') { $bucket[$res] = $got } else { $bucket[$res] = @($got) }
          }
          # remember who this is, so the folder can name itself
          if ($bucket['self'] -and $bucket['self'].label) {
            $ident.role  = 'node'
            $ident.label = [string]$bucket['self'].label
            $ident.id    = [string]$bucket['self'].id
          }
          # SDP: IS-04 defines manifest_href ON THE SENDER RESOURCE. There is no
          # /senders/{id}/transportfile in IS-04 (that is IS-05). Follow the
          # field verbatim - devices place it where they like, e.g.
          # /x-nmos/node/v1.3/sdp/{id} on an EVS Neuron.
          foreach ($s in @($bucket['senders'])) {
            if ($NoSdp) { break }
            if (-not $s.id) { continue }
            if ($s.manifest_href) {
              Get-Sdp $s.manifest_href (Join-Path $dir "sdp\sender_$($s.id).sdp")
            } else {
              $report.Add("NOSDP sender $($s.id) has no manifest_href (legal for an inactive sender)")
            }
          }
        }
        'connection' {
          foreach ($side in 'senders','receivers') {
            $ids = Get-Json "$base/single/$side"
            $bucket["single_$side"] = @($ids)
            $total = @($ids).Count; $n = 0
            if ($total -gt 40) { Write-Host ("    IS-05 {0} {1}: {2} entries x4 endpoints ..." -f $v, $side, $total) }
            foreach ($id in @($ids)) {
              $n++
              if ($total -gt 40 -and ($n % 25) -eq 0) { Write-Host ("      {0}/{1}" -f $n, $total) }
              $id = "$id".TrimEnd('/')
              $subs = if ($Deep) { @('staged','active','constraints','transporttype') } else { @('active') }
              # transporttype arrived in IS-05 v1.1 - requesting it on a v1.0
              # connection API is a guaranteed 404 (5,951 of them in one live run).
              if ($v -eq 'v1.0') { $subs = @($subs | Where-Object { $_ -ne 'transporttype' }) }
              foreach ($sub in $subs) {
                $r = Get-Json "$base/single/$side/$id/$sub"
                $bucket["$side/$id/$sub"] = $r
                # A RECEIVER's SDP is pushed into it by a controller and lives
                # in transport_file.data. Empty means nothing is connected.
                if ($r -and $r.transport_file -and $r.transport_file.data) {
                  $data = [string]$r.transport_file.data
                  if ($data.Trim().Length -gt 10) {
                    [IO.File]::WriteAllText((Join-Path $dir "sdp\$($side)_$($id)_$sub.sdp"), $data)
                    $report.Add("SDP   $side $id $sub (embedded)")
                  }
                }
              }
            }
            # IS-05 does serve the sender SDP at its own endpoint
            if ($side -eq 'senders' -and -not $NoSdp) {
              foreach ($id in @($ids)) {
                $id = "$id".TrimEnd('/')
                Get-Sdp ($scheme + "://" + $Device + $base + "/single/senders/$id/transportfile") (Join-Path $dir "sdp\is05_sender_$id.sdp")
              }
            }
          }
          $bucket['bulk'] = Get-Json "$base/bulk"
        }
        'query' {
          $ident.role = 'registry'
          # A REGISTRY: this captures the whole plant.
          foreach ($res in 'nodes','devices','sources','flows','senders','receivers','subscriptions') {
            $bucket[$res] = @(Get-JsonPaged "$base/$res")
          }
          $before = $nodesLt.Count
          foreach ($n in @($bucket['nodes'])) {
            if ($n.id -and -not ($nodesLt | Where-Object { $_.id -eq $n.id })) { $nodesLt.Add($n) }
          }
          if ($bucket['nodes']) {
            $report.Add(("VERSION {0} nodes: {1} listed, {2} new (union so far {3})" -f $v, @($bucket['nodes']).Count, ($nodesLt.Count - $before), $nodesLt.Count))
          }
        }
        'channelmapping' {
          $bucket['io']         = Get-Json "$base/io"
          $bucket['map_active'] = Get-Json "$base/map/active"
          $bucket['map_staged'] = Get-Json "$base/map/staged"
        }
        'system'       { $bucket['global'] = Get-Json "$base/global" }
        'registration' { $bucket['root']   = Get-Json "$base/" }
        default        { $bucket['root']   = Get-Json "$base/" }
      }
      $tree.apis[$api].data[$v] = $bucket
    }
  }

  # ---- registry: follow each registered node so SDPs are captured too ----
  $followed = 0; $capped = 0
  if (-not $SkipFollow -and $nodesLt.Count) {
    Write-Host "  registry holds $($nodesLt.Count) node(s); following up to $MaxNodes"
    $tree['followed_nodes'] = [ordered]@{}
    foreach ($n in $nodesLt) {
      if ($followed -ge $MaxNodes) { $capped++; continue }
      # Prefer api.endpoints (host+port); href is often wrong on real devices.
      $cands = @()
      foreach ($e in @($n.api.endpoints)) { if ($e.host) { $cands += "$($e.host):$($e.port)" } }
      if ($n.href) { try { $u = [uri]$n.href; $cands += "$($u.Host):$($u.Port)" } catch { } }
      $hit = $null
      foreach ($c in @($cands | Select-Object -Unique)) {
        try { if ((Invoke-WebRequest -Uri ($scheme + "://" + $c + "/x-nmos") -TimeoutSec 4 -UseBasicParsing).StatusCode -eq 200) { $hit = $c; break } } catch { }
      }
      if (-not $hit) { $report.Add("SKIP  node $($n.id) '$($n.label)' unreachable at: $($cands -join ', ')"); continue }
      $report.Add("FOLLOW node $($n.id) '$($n.label)' via $hit")
      $sub = Join-Path $dir "nodes"
      $null = New-Item -ItemType Directory -Force -Path $sub
      Invoke-Harvest -Device $hit -Root $sub -SkipFollow -NoStamp | Out-Null
      $tree['followed_nodes'][$n.id] = @{ label = $n.label; reached_at = $hit }
      $followed++
    }
  }
  if ($capped) { $report.Add("CAPPED $capped node(s) not followed (-MaxNodes $MaxNodes)") }
  if ($nodesLt.Count) {
    $skipped = @($report | Where-Object { $_ -match '^SKIP ' }).Count
    Write-Host ("  registry: {0} node(s) listed, {1} followed, {2} unreachable, {3} capped" -f $nodesLt.Count, $followed, $skipped, $capped)
    # NOTE the extra parentheses: inside a method call the commas would split
    # Add()'s argument list, leaving -f with a single value and four holes.
    $report.Add(("SUMMARY {0} listed / {1} followed / {2} unreachable / {3} capped" -f $nodesLt.Count, $followed, $skipped, $capped))
  }

  # ---- write the asset ----
  [IO.File]::WriteAllText((Join-Path $dir 'tree.json'), ($tree | ConvertTo-Json -Depth 40))
  [IO.File]::WriteAllText((Join-Path $dir 'report.txt'), ($report -join "`r`n"))
  $ok0  = @($report | Where-Object { $_ -match '^200' }).Count
  $err0 = @($report | Where-Object { $_ -match '^(ERR|4\d\d|5\d\d)' }).Count

  # Every folder must SAY WHAT IT IS. Without this a tree of harvested folders
  # is a list of ip_port names and nothing tells you which node you are looking
  # at once they are moved, zipped or nested under a registry.
  $sdpCount = @(Get-ChildItem (Join-Path $dir 'sdp') -Filter *.sdp -ErrorAction SilentlyContinue).Count
  # NOTE: not $device - that collides with the [string]$Device parameter
  # (PowerShell is case-insensitive) and silently ToString()s the dictionary.
  $devInfo = [ordered]@{
    target       = $Device
    role         = $ident.role
    label        = $ident.label
    id           = $ident.id
    harvested_at = (Get-Date).ToString('o')
    harvester    = 'nmos-harvest.ps1'
    version      = $HarvesterVersion
    apis         = @($tree.apis.Keys | Where-Object { $_ -ne '_root' })
    counts       = [ordered]@{ ok = $ok0; failed = $err0; sdp = $sdpCount }
  }
  [IO.File]::WriteAllText((Join-Path $dir 'device.json'), ([pscustomobject]$devInfo | ConvertTo-Json -Depth 6))

  # Drop folders that would otherwise be empty and ambiguous: a registry holds
  # no SDPs of its own (they live on its nodes).
  foreach ($sub2 in 'sdp','raw') {
    $q = Join-Path $dir $sub2
    if ((Test-Path $q) -and -not (Get-ChildItem $q -Force -ErrorAction SilentlyContinue)) {
      # [IO.Directory]::Delete is deterministic; the cmdlet form can fail
      # silently under -ErrorAction SilentlyContinue and leave the folder behind.
      try { [IO.Directory]::Delete($q) }
      catch { $report.Add("WARN  empty $sub2\ left in place: $($_.Exception.Message)") }
    }
  }

  # Name the folder after the device once we know who it is: <target>__<label>.
  if ($ident.label) {
    $lab = ($ident.label -replace '[^0-9A-Za-z._-]', '_')
    $newDir = Join-Path (Split-Path $dir -Parent) ((Split-Path $dir -Leaf) + '__' + $lab)
    if (-not (Test-Path $newDir)) {
      try { Rename-Item -LiteralPath $dir -NewName (Split-Path $newDir -Leaf) -ErrorAction Stop; $dir = $newDir } catch { }
    }
  }

  $ok  = $ok0
  $err = $err0
  $sdp = $sdpCount
  Write-Host ("  {0} ok, {1} failed, {2} SDP file(s)" -f $ok, $err, $sdp)
  return $dir
}

# -------------------------------------------------------------------- run --
$done = @()
foreach ($dev in $targets) {
  $d = Invoke-Harvest -Device $dev -Root $Out -SkipFollow:$NoFollow
  $done += $d
}

# Guarantee the caller asked for: no raw\ or sdp\ loose at the output root.
foreach ($stray in 'raw','sdp') {
  $q = Join-Path $Out $stray
  if (Test-Path $q) {
    $n = @(Get-ChildItem $q -Recurse -File -ErrorAction SilentlyContinue).Count
    Write-Host ""
    Write-Host "WARNING: $q exists ($n file(s)) - not written by this run's device folders."
    Write-Host "         It is left in place, not deleted. Remove it if it is old debris."
  }
}

Write-Host ""
Write-Host "=== harvested $($done.Count) device(s) ==="
$done | ForEach-Object { Write-Host "  $_" }
Write-Host ""
Write-Host "Send these folders back (tree.json + sdp\) - they become committed fixtures."
