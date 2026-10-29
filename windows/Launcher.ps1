param([switch]$Smoke)
$ErrorActionPreference='Stop'
$startupLog=Join-Path $env:LOCALAPPDATA 'AutoblowLocal\launcher-startup.log'
[IO.Directory]::CreateDirectory((Split-Path -Parent $startupLog)) | Out-Null
function Startup-Stage($stage){[IO.File]::AppendAllText($startupLog,([DateTime]::UtcNow.ToString('o')+' '+$stage+[Environment]::NewLine))}
trap {Startup-Stage ('error: '+$_.Exception.Message);throw $_}
Startup-Stage 'loading forms'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
[Windows.Forms.Application]::EnableVisualStyles()
$mutex=[Threading.Mutex]::new($false,'Local\AutoblowLocalLauncher')
if(-not $mutex.WaitOne(0)){[Windows.Forms.MessageBox]::Show('Приложение уже запущено.','Autoblow Local') | Out-Null;exit 1}
Startup-Stage 'launcher lock acquired'
$script:server=$null;$script:hotspot=$null;$script:adapter=$null
$script:address='';$script:headers=@{};$script:stopping=$false
$root=Split-Path -Parent $PSScriptRoot
$serverPath=Join-Path $root 'autoblow-server.exe'
$data=Join-Path $env:LOCALAPPDATA 'AutoblowLocal'
[IO.Directory]::CreateDirectory($data) | Out-Null
$hotspotConfig=Join-Path $data 'hotspot-config.json'
$hotspotState=Join-Path $data 'hotspot-state.json'
$hotspotStop=Join-Path $data 'hotspot-stop'
$serverConfig=Join-Path $data 'server-config.json'
$serverData=Join-Path $data 'server'
. (Join-Path $PSScriptRoot 'ServerLifecycle.ps1')
$script:cancelStart=$false
function Write-Json($path,$value){[IO.File]::WriteAllText($path,($value | ConvertTo-Json -Depth 5),[Text.UTF8Encoding]::new($false))}
function Wait-While($condition,[int]$seconds){$end=[DateTime]::UtcNow.AddSeconds($seconds);while((& $condition) -and [DateTime]::UtcNow -lt $end){[Windows.Forms.Application]::DoEvents();Start-Sleep -Milliseconds 100}}
function Stop-App {
 $script:cancelStart=$true
 if($script:stopping){return};$script:stopping=$true
 try {
  if($script:server -and -not $script:server.HasExited){
   Stop-LocalServer $script:server $serverData { [Windows.Forms.Application]::DoEvents(); Start-Sleep -Milliseconds 100 }
   if($script:server.ExitCode -ne 0){[Windows.Forms.MessageBox]::Show('Сервер завершился с ошибкой. STOP мог быть не подтверждён: проверьте устройство.','Autoblow Local') | Out-Null}
  }
  if($script:hotspot -and -not $script:hotspot.HasExited){[IO.File]::WriteAllText($hotspotStop,'stop');Wait-While {-not $script:hotspot.HasExited} 6;if(-not $script:hotspot.HasExited){throw 'Точка доступа ещё завершает работу.'}}
  $script:server=$null;$script:hotspot=$null;$status.Text='Сервер остановлен';$start.Enabled=$true;$stop.Enabled=$false;$open.Enabled=$false
 } finally {$script:stopping=$false}
}
function Start-App {
 $script:cancelStart=$false
 $start.Enabled=$false;$status.Text='Запуск...';[Windows.Forms.Application]::DoEvents()
 try {
  if($script:cancelStart){throw 'Запуск отменён.'}
  if(-not (Test-Path -LiteralPath $serverPath)){throw 'Не найден autoblow-server.exe рядом с приложением.'}
  $control=@{maxSpeed=15;compatibilityControl=$false;experimentalSyncScripts=$false;experimentalLocalScripts=$false;scriptURLOrigins=@()}
  $controlFile=Join-Path $data 'control-options.json'
  if(Test-Path -LiteralPath $controlFile){
   $options=Get-Content -LiteralPath $controlFile -Raw | ConvertFrom-Json
   if($null -eq $options -or $options -isnot [pscustomobject]){throw 'control-options.json должен содержать JSON-объект.'}
   foreach($property in $options.PSObject.Properties){if(-not $control.ContainsKey($property.Name)){throw ('Неизвестный параметр управления: '+$property.Name)};$control[$property.Name]=$property.Value}
  }
  $useDNS=$dns.Checked
  if($mode.SelectedIndex -eq 1){
   $useDNS=$true
   Write-Json $hotspotConfig @{ssid=$ssid.Text;password=$password.Text}
   foreach($path in @($hotspotStop,$hotspotState)){if(Test-Path -LiteralPath $path){Remove-Item -LiteralPath $path}}
   $arguments=@('-NoProfile','-NonInteractive','-ExecutionPolicy','Bypass','-File',('"'+(Join-Path $PSScriptRoot 'Hotspot.ps1')+'"'),'-ConfigPath',('"'+$hotspotConfig+'"'),'-StatePath',('"'+$hotspotState+'"'),'-StopPath',('"'+$hotspotStop+'"'),'-OwnerPid',$PID)
   $script:hotspot=Start-Process -FilePath powershell.exe -ArgumentList $arguments -WindowStyle Hidden -PassThru -RedirectStandardError (Join-Path $data 'hotspot-error.log')
   Wait-While {-not $script:cancelStart -and -not (Test-Path -LiteralPath $hotspotState) -and -not $script:hotspot.HasExited} 20
   if($script:cancelStart){throw 'Запуск отменён.'}
   if(-not (Test-Path -LiteralPath $hotspotState)){throw 'Точка доступа не запустилась. См. hotspot-error.log в папке данных.'}
   $state=Get-Content -LiteralPath $hotspotState -Raw | ConvertFrom-Json
   if($state.status -ne 'ready'){throw $state.error}
   $script:address=$state.address;$script:adapter=$state.interfaceAlias
  } else {
   if($network.SelectedIndex -lt 0){throw 'Выберите сетевой интерфейс.'}
   $selected=$script:networks[$network.SelectedIndex];$script:address=$selected.IPAddress;$script:adapter=$selected.InterfaceAlias
  }
  $config=@{deviceListen=($script:address+':443');apiListen='127.0.0.1:20880';dnsListen=$(if($useDNS){$script:address+':53'}else{''});dnsAnswer=$script:address;dataDir=$serverData;allowShutdown=$true}
  foreach($key in $control.Keys){$config[$key]=$control[$key]}
  Write-Json $serverConfig $config
  $script:server=Start-Process -FilePath $serverPath -ArgumentList @('--config',('"'+$serverConfig+'"')) -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $data 'server.log') -RedirectStandardError (Join-Path $data 'server-error.log')
  $ready=$false;$end=[DateTime]::UtcNow.AddSeconds(10)
  while(-not $ready -and -not $script:cancelStart -and -not $script:server.HasExited -and [DateTime]::UtcNow -lt $end){try{$health=Invoke-RestMethod 'http://127.0.0.1:20880/health' -TimeoutSec 1;$ready=$health.version -eq '0.2.0-rc.2'}catch{};if(-not $ready){Start-Sleep -Milliseconds 100};[Windows.Forms.Application]::DoEvents()}
  if($script:cancelStart){throw 'Запуск отменён.'}
  if(-not $ready){$detail=Get-Content -LiteralPath (Join-Path $data 'server-error.log') -Raw -ErrorAction SilentlyContinue;throw ('Не удалось запустить сервер. '+$detail)}
  $token=(Get-Content -LiteralPath (Join-Path $serverData 'control-token') -Raw).Trim();$script:headers=@{Authorization='Bearer '+$token}
  Assert-LocalServerListener $script:server
  # Verify that the listener belongs to this application's token, not another service.
  Invoke-RestMethod 'http://127.0.0.1:20880/api/status' -Headers $script:headers -TimeoutSec 2 | Out-Null
  $status.Text='Сервер: '+$script:address+':443. '+$(if($mode.SelectedIndex -eq 1){'Wi-Fi: '+$ssid.Text}else{'Настройте DNS на роутере по инструкции.'})
  $stop.Enabled=$true;$open.Enabled=$true;$firewall.Enabled=$true
  Start-Process ('http://127.0.0.1:20880/#token='+$token)
 } catch {
  $message=$_.Exception.Message
  try {Stop-App} catch {$status.Text=$message+' '+$_.Exception.Message;$start.Enabled=$false;$stop.Enabled=$true;return}
  $status.Text=$message
 }
}

$form=[Windows.Forms.Form]::new();$form.Text='Autoblow Local';$form.Size=[Drawing.Size]::new(640,650);$form.StartPosition='CenterScreen';$form.Font=[Drawing.Font]::new('Segoe UI',10);$form.FormBorderStyle='FixedDialog';$form.MaximizeBox=$false
function Label($text,$y){$c=[Windows.Forms.Label]::new();$c.Text=$text;$c.Location=[Drawing.Point]::new(24,$y);$c.Size=[Drawing.Size]::new(575,28);$form.Controls.Add($c);return $c}
$title=Label 'Autoblow Local - запуск сервера' 20;$title.Font=[Drawing.Font]::new('Segoe UI',16)
$null=Label 'Способ подключения' 68
$mode=[Windows.Forms.ComboBox]::new();$mode.Location=[Drawing.Point]::new(24,98);$mode.Width=575;$mode.DropDownStyle='DropDownList';$mode.Items.AddRange(@('Домашняя сеть - роутер настраивается пользователем','Автономная точка доступа Windows (Wi-Fi Direct)'));$mode.SelectedIndex=0;$form.Controls.Add($mode)
$null=Label 'Сетевой интерфейс для домашней сети' 138
$network=[Windows.Forms.ComboBox]::new();$network.Location=[Drawing.Point]::new(24,168);$network.Width=575;$network.DropDownStyle='DropDownList';$form.Controls.Add($network)
$script:networks=@(Get-NetIPAddress -AddressFamily IPv4 | Where-Object {$_.IPAddress -notlike '127.*' -and $_.IPAddress -notlike '169.254.*' -and $_.AddressState -eq 'Preferred'})
Startup-Stage 'network list ready'
foreach($n in $script:networks){$network.Items.Add($n.InterfaceAlias+' - '+$n.IPAddress) | Out-Null};if($network.Items.Count){$network.SelectedIndex=0}
$dns=[Windows.Forms.CheckBox]::new();$dns.Text='Включить DNS приложения для VacuGlide';$dns.Location=[Drawing.Point]::new(24,208);$dns.Width=575;$form.Controls.Add($dns)
$null=Label 'Название Wi-Fi для автономного режима' 246
$ssid=[Windows.Forms.TextBox]::new();$ssid.Location=[Drawing.Point]::new(24,276);$ssid.Width=575;$ssid.Text='Autoblow-Local';$form.Controls.Add($ssid)
$null=Label 'Пароль Wi-Fi (сохранится только на этом компьютере)' 312
$password=[Windows.Forms.TextBox]::new();$password.Location=[Drawing.Point]::new(24,342);$password.Width=430;$password.UseSystemPasswordChar=$true;$password.Text=[guid]::NewGuid().ToString('N').Substring(0,16);$form.Controls.Add($password)
if(Test-Path -LiteralPath $hotspotConfig){try{$saved=Get-Content -LiteralPath $hotspotConfig -Raw | ConvertFrom-Json;$ssid.Text=$saved.ssid;$password.Text=$saved.password}catch{}}
function Button($text,$x,$y,$width){$b=[Windows.Forms.Button]::new();$b.Text=$text;$b.Location=[Drawing.Point]::new($x,$y);$b.Size=[Drawing.Size]::new($width,38);$form.Controls.Add($b);return $b}
$start=Button 'Запустить' 24 398 175;$open=Button 'Управление' 218 398 175;$stop=Button 'Остановить' 412 398 187;$open.Enabled=$false;$stop.Enabled=$false
$firewall=Button 'Разрешить в брандмауэре...' 24 450 300;$firewall.Enabled=$false
$docs=Button 'Инструкция' 344 450 255
$showPassword=Button 'Показать' 470 337 129;$showPassword.Add_Click({$password.UseSystemPasswordChar=-not $password.UseSystemPasswordChar});
$status=Label 'Сервер остановлен. Автоматического запуска нет.' 512;$status.Height=70
$start.Add_Click({Start-App})
$open.Add_Click({$token=(Get-Content -LiteralPath (Join-Path $serverData 'control-token') -Raw).Trim();Start-Process ('http://127.0.0.1:20880/#token='+$token)})
$stop.Add_Click({try{Stop-App}catch{$status.Text=$_.Exception.Message}})
$firewall.Add_Click({try{
 $firewallArgs=@('-NoProfile','-ExecutionPolicy','Bypass','-File',('"'+(Join-Path $PSScriptRoot 'Firewall.ps1')+'"'),'-ServerPath',('"'+$serverPath+'"'),'-InterfaceAlias',('"'+$script:adapter+'"'))
 $p=Start-Process -FilePath powershell.exe -ArgumentList $firewallArgs -Verb RunAs -WindowStyle Hidden -PassThru;$p.WaitForExit()
 if($p.ExitCode -ne 0){throw 'Настройка брандмауэра не выполнена.'};$status.Text='Разрешены только DNS и TLS приложения на выбранном интерфейсе для локальной сети.'
}catch{$status.Text=$_.Exception.Message}})
$docs.Add_Click({Start-Process (Join-Path $root 'docs\WINDOWS.md')})
$form.Add_FormClosing({param($sender,$closingEvent) try{Stop-App}catch{$closingEvent.Cancel=$true;$status.Text=$_.Exception.Message}})
if($Smoke){$timer=[Windows.Forms.Timer]::new();$timer.Interval=1000;$timer.Add_Tick({$timer.Stop();$form.Close()});$timer.Start()}
Startup-Stage 'showing form'
try{[Windows.Forms.Application]::Run($form)}finally{Startup-Stage 'form closed';$mutex.ReleaseMutex();$mutex.Dispose()}
