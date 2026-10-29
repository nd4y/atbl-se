$framework=[Runtime.InteropServices.RuntimeEnvironment]::GetRuntimeDirectory()
$refs=@(
 (Join-Path $env:WINDIR 'System32\WinMetadata\Windows.Devices.winmd'),
 (Join-Path $env:WINDIR 'System32\WinMetadata\Windows.Foundation.winmd'),
 (Join-Path $env:WINDIR 'System32\WinMetadata\Windows.Security.winmd'),
 (Join-Path $framework 'System.Runtime.WindowsRuntime.dll')
)
foreach($assembly in @('System.Runtime','System.Threading.Tasks','System.Runtime.InteropServices.WindowsRuntime')) {
 $refs+=[Reflection.Assembly]::Load($assembly+', Version=4.0.0.0, Culture=neutral, PublicKeyToken=b03f5f7f11d50a3a').Location
}
$source=Join-Path $PSScriptRoot 'HotspotHost.cs'
$hash=(Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash.Substring(0,16)
$cache=Join-Path $env:LOCALAPPDATA 'AutoblowLocal\interop'
[IO.Directory]::CreateDirectory($cache) | Out-Null
$dll=Join-Path $cache ('hotspot-'+$hash+'.dll')
if(-not (Test-Path -LiteralPath $dll)) {
 $arguments=@('/nologo','/target:library',('/out:'+$dll))
 $arguments+=@($refs | ForEach-Object {'/reference:'+$_})
 $arguments+=$source
 $result=& (Join-Path $framework 'csc.exe') @arguments 2>&1
 if($LASTEXITCODE -ne 0){throw ('Windows interop compiler: '+($result -join [Environment]::NewLine))}
}
Add-Type -Path $dll
