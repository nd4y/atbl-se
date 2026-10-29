$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot '../windows/StateMigration.ps1')
function Get-NetTCPConnection { if($script:activeServer){[pscustomobject]@{LocalPort=20880}} }
function Assert($ok,$message){if(-not $ok){throw $message}}
$root=Join-Path ([IO.Path]::GetTempPath()) ('atblse-migration-test-'+[guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($root) | Out-Null
$checks=0
try {
 $fresh=Join-Path $root 'fresh';[IO.Directory]::CreateDirectory($fresh) | Out-Null
 $result=Initialize-AtblseData $fresh;Assert ($result -eq (Join-Path $fresh 'atblse')) 'fresh location';$checks++
 $base=Join-Path $root 'space and unicode-данные';$legacy=Join-Path $base 'AutoblowLocal';$nested=Join-Path $legacy 'server'
 [IO.Directory]::CreateDirectory($nested) | Out-Null
 [IO.File]::WriteAllText((Join-Path $nested 'control-token'),('a'*64))
 [IO.File]::WriteAllText((Join-Path $legacy 'hotspot-config.json'),'synthetic-test-configuration')
 $result=Initialize-AtblseData $base
 Assert ((Get-FileHash -LiteralPath (Join-Path $nested 'control-token')).Hash -eq (Get-FileHash -LiteralPath (Join-Path $result 'server/control-token')).Hash) 'key changed'
 Assert (Test-Path -LiteralPath $legacy) 'legacy source deleted';$checks++
 [IO.File]::WriteAllText((Join-Path $result 'new-state'),'keep')
 Assert ((Initialize-AtblseData $base) -eq $result) 'repeat location'
 Assert (Test-Path -LiteralPath (Join-Path $result 'new-state')) 'repeat erased current state';$checks++
 $collision=Join-Path $root 'collision'
 [IO.Directory]::CreateDirectory((Join-Path $collision 'atblse')) | Out-Null
 [IO.Directory]::CreateDirectory((Join-Path $collision 'AutoblowLocal')) | Out-Null
 $failed=$false;try{Initialize-AtblseData $collision | Out-Null}catch{$failed=$true};Assert $failed 'collision accepted';$checks++
 $busy=Join-Path $root 'busy';[IO.Directory]::CreateDirectory((Join-Path $busy 'AutoblowLocal')) | Out-Null
 $script:activeServer=$true;$failed=$false;try{Initialize-AtblseData $busy | Out-Null}catch{$failed=$true};$script:activeServer=$false
 Assert $failed 'live server migration accepted';Assert (-not (Test-Path -LiteralPath (Join-Path $busy 'atblse'))) 'live source published';$checks++
 $fault=Join-Path $root 'fault';[IO.Directory]::CreateDirectory((Join-Path $fault 'AutoblowLocal')) | Out-Null
 [IO.File]::WriteAllText((Join-Path $fault 'AutoblowLocal/input'),'preserve')
 function Get-FileHash {
  param([string]$LiteralPath,[string]$Algorithm='SHA256')
  if($LiteralPath -like '*atblse-migration-*/input' -or $LiteralPath -like '*atblse-migration-*\input'){throw 'synthetic interrupted copy'}
  Microsoft.PowerShell.Utility\Get-FileHash -LiteralPath $LiteralPath -Algorithm $Algorithm
 }
 $failed=$false;try{Initialize-AtblseData $fault | Out-Null}catch{$failed=$true}
 Remove-Item Function:/Get-FileHash
 Assert $failed 'copy failure accepted';Assert (-not (Test-Path -LiteralPath (Join-Path $fault 'atblse'))) 'partial migration published'
 Assert ([IO.File]::ReadAllText((Join-Path $fault 'AutoblowLocal/input')) -eq 'preserve') 'source changed';$checks++
 $result=Initialize-AtblseData $fault;Assert (Test-Path -LiteralPath (Join-Path $result 'input')) 'retry failed';$checks++
 [IO.File]::WriteAllText((Join-Path $result 'migration-v1.json'),'{}')
 $failed=$false;try{Initialize-AtblseData $fault | Out-Null}catch{$failed=$true};Assert $failed 'damaged migration marker accepted';$checks++
 Write-Output "PASS: $checks isolated Windows migration checks; no device/network operations"
} finally {
 # Delete only this test's freshly created, resolved subtree.
 $resolved=[IO.Path]::GetFullPath($root);$parent=[IO.Path]::GetFullPath([IO.Path]::GetTempPath())
 if(-not $resolved.StartsWith($parent,[StringComparison]::OrdinalIgnoreCase) -or -not ([IO.Path]::GetFileName($resolved)).StartsWith('atblse-migration-test-')){throw 'unsafe test cleanup path'}
 Remove-Item -LiteralPath $resolved -Recurse -Force
}
