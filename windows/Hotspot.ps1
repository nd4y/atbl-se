param([Parameter(Mandatory=$true)][string]$ConfigPath,[Parameter(Mandatory=$true)][string]$StatePath,[Parameter(Mandatory=$true)][string]$StopPath,[int]$OwnerPid=0,[switch]$Probe)
$ErrorActionPreference='Stop'
Add-Type -AssemblyName System.Runtime.WindowsRuntime
. (Join-Path $PSScriptRoot 'Load-HotspotHost.ps1')
$publisher=$null; $subscription=$null
$connections=[System.Collections.Concurrent.ConcurrentBag[object]]::new()
function Save-State($value) {
 $text=$value | ConvertTo-Json -Depth 5 -Compress
 [IO.File]::WriteAllText($StatePath+'.tmp',$text,[Text.UTF8Encoding]::new($false))
 Move-Item -LiteralPath ($StatePath+'.tmp') -Destination $StatePath -Force
}
try {
 $cfg=Get-Content -LiteralPath $ConfigPath -Raw | ConvertFrom-Json
 if([Text.Encoding]::UTF8.GetByteCount($cfg.ssid) -gt 32 -or $cfg.ssid.Length -lt 1){throw 'SSID must contain 1..32 UTF-8 bytes'}
 if($cfg.password.Length -lt 8 -or $cfg.password.Length -gt 63 -or $cfg.password -notmatch '^[\x20-\x7e]+$'){throw 'Use an ASCII password of 8..63 characters'}
 $active=@(Get-NetAdapter -IncludeHidden | Where-Object {$_.InterfaceDescription -like '*Wi-Fi Direct*' -and $_.Status -eq 'Up'})
 if($active.Count){throw 'Another Wi-Fi Direct connection or Mobile Hotspot is active. Stop it before starting this application.'}
 $publisher=[AutoblowLocal.HotspotHost]::new()
 if($Probe){Save-State @{status='interop-ready'};return}
 $publisher.Start($cfg.ssid,$cfg.password)
 $deadline=[DateTime]::UtcNow.AddSeconds(15);$ip=$null;$adapter=$null
 do {
  if($publisher.Status.ToString() -eq 'Aborted'){throw 'Wi-Fi Direct access point aborted. Check adapter/driver support.'}
  $adapter=Get-NetAdapter -IncludeHidden | Where-Object {$_.InterfaceDescription -like '*Wi-Fi Direct*' -and $_.Status -eq 'Up'} | Select-Object -First 1
  if($adapter){$ip=Get-NetIPAddress -InterfaceIndex $adapter.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue | Where-Object {$_.IPAddress -notlike '169.254.*'} | Select-Object -First 1}
  if(-not $ip){Start-Sleep -Milliseconds 300}
 } while(-not $ip -and [DateTime]::UtcNow -lt $deadline)
 if(-not $ip){throw 'Windows did not assign an IPv4 address to the access point'}
 Save-State @{status='ready';address=$ip.IPAddress;interfaceAlias=$adapter.Name;interfaceIndex=$adapter.ifIndex;ssid=$cfg.ssid}
 while(-not (Test-Path -LiteralPath $StopPath)) {
  if($OwnerPid -and -not (Get-Process -Id $OwnerPid -ErrorAction SilentlyContinue)){break}
  if($publisher.Status.ToString() -ne 'Started'){throw 'Access point stopped unexpectedly'}
  Wait-Event -Timeout 1 | Out-Null
 }
} catch {Save-State @{status='error';error=$_.Exception.Message};exit 1}
finally {
 if($publisher){$publisher.Dispose()}
 if($subscription){Unregister-Event -SubscriptionId $subscription.Id -ErrorAction SilentlyContinue;Remove-Job -Job $subscription -Force -ErrorAction SilentlyContinue}
 foreach($connection in $connections){if($connection -is [IDisposable]){$connection.Dispose()}}
}
