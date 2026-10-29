param([Parameter(Mandatory=$true)][string]$ServerPath,[Parameter(Mandatory=$true)][string]$InterfaceAlias,[switch]$Remove)
$ErrorActionPreference='Stop'
$names=@('AutoblowLocal-DeviceTLS','AutoblowLocal-DNS-TCP','AutoblowLocal-DNS-UDP')
foreach($name in $names){Get-NetFirewallRule -Name $name -ErrorAction SilentlyContinue | Remove-NetFirewallRule}
if($Remove){exit 0}
$resolved=(Resolve-Path -LiteralPath $ServerPath).Path
if([IO.Path]::GetFileName($resolved) -ne 'autoblow-server.exe'){throw 'Unexpected server executable'}
$adapter=Get-NetAdapter -IncludeHidden | Where-Object Name -EQ $InterfaceAlias
if(@($adapter).Count -ne 1){throw 'Expected one selected network adapter'}
New-NetFirewallRule -Name $names[0] -DisplayName 'Autoblow Local - device TLS' -Direction Inbound -Action Allow -Program $resolved -Protocol TCP -LocalPort 443 -RemoteAddress LocalSubnet -InterfaceAlias $InterfaceAlias -Profile Any | Out-Null
New-NetFirewallRule -Name $names[1] -DisplayName 'Autoblow Local - local DNS TCP' -Direction Inbound -Action Allow -Program $resolved -Protocol TCP -LocalPort 53 -RemoteAddress LocalSubnet -InterfaceAlias $InterfaceAlias -Profile Any | Out-Null
New-NetFirewallRule -Name $names[2] -DisplayName 'Autoblow Local - local DNS UDP' -Direction Inbound -Action Allow -Program $resolved -Protocol UDP -LocalPort 53 -RemoteAddress LocalSubnet -InterfaceAlias $InterfaceAlias -Profile Any | Out-Null
