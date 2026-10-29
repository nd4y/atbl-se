$ErrorActionPreference='Stop'
. (Join-Path (Split-Path -Parent $PSScriptRoot) 'windows\ServerLifecycle.ps1')
$temp=Join-Path ([IO.Path]::GetTempPath()) ('autoblow-lifecycle-test-'+[guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($temp)
function Get-NetTCPConnection {param($State,$LocalPort,$ErrorAction) [pscustomobject]@{OwningProcess=$script:listenerOwner;LocalAddress='127.0.0.1'}}
function Invoke-RestMethod {param($Uri,$Method,$Headers,$ContentType,$Body,$TimeoutSec) if($Uri -ne 'http://127.0.0.1:20880/api/shutdown'){throw 'Unexpected endpoint'};$script:shutdownCalls++}
function Check($condition,$message){if(-not $condition){throw $message}}
function Must-Fail([scriptblock]$Action){$failed=$false;try{& $Action}catch{$failed=$true};Check $failed 'Expected rejection'}
try {
 [IO.File]::WriteAllText((Join-Path $temp 'control-token'),('a'*64))
 $script:proc=[pscustomobject]@{Id=12345;HasExited=$false;ExitCode=0};$script:shutdownCalls=0
 $script:listenerOwner=54321
 Must-Fail {Stop-LocalServer $script:proc $temp {} 0};Check ($script:shutdownCalls -eq 0) 'Foreign process received shutdown'
 $script:listenerOwner=12345
 Stop-LocalServer $script:proc $temp {$script:proc.HasExited=$true} 1
 Check ($script:shutdownCalls -eq 1 -and $script:proc.HasExited) 'Owned process not stopped'
 Stop-LocalServer $script:proc $temp {} 0;Check ($script:shutdownCalls -eq 1) 'Exited process caused another request'
 $script:proc.HasExited=$false
 Must-Fail {Stop-LocalServer $script:proc $temp {} 0};Check (-not $script:proc.HasExited) 'Timeout killed process'
 [IO.File]::WriteAllText((Join-Path $temp 'control-token'),'invalid');$count=$script:shutdownCalls
 Must-Fail {Stop-LocalServer $script:proc $temp {} 0};Check ($script:shutdownCalls -eq $count) 'Invalid key sent request'
 Write-Output 'PASS: 5 mocked lifecycle checks; no real network, process or hotspot operation'
} finally {
 Remove-Item -LiteralPath (Join-Path $temp 'control-token') -ErrorAction SilentlyContinue
 [IO.Directory]::Delete($temp)
}
