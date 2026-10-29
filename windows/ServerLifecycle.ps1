# Pure process/API ownership helpers; importing this file starts nothing.
function Assert-LocalServerListener($ServerProcess) {
 if($null -eq $ServerProcess -or $ServerProcess.HasExited){throw 'Сервер приложения уже завершён.'}
 $owned=@(Get-NetTCPConnection -State Listen -LocalPort 20880 -ErrorAction SilentlyContinue | Where-Object {$_.OwningProcess -eq $ServerProcess.Id -and $_.LocalAddress -eq '127.0.0.1'})
 if(-not $owned.Count){throw 'Порт API пока не принадлежит запущенному процессу. Сервер и сеть сохранены; повторите остановку.'}
}
function Stop-LocalServer($ServerProcess,[string]$DataDirectory,[scriptblock]$Pump={Start-Sleep -Milliseconds 100},[int]$TimeoutSeconds=12) {
 if($null -eq $ServerProcess -or $ServerProcess.HasExited){return}
 Assert-LocalServerListener $ServerProcess
 $key=(Get-Content -LiteralPath (Join-Path $DataDirectory 'control-token') -Raw).Trim()
 if($key -cnotmatch '^[0-9a-f]{64}$'){throw 'Не удалось прочитать ключ собственного сервера.'}
 Invoke-RestMethod -Uri 'http://127.0.0.1:20880/api/shutdown' -Method Post -Headers @{Authorization='Bearer '+$key} -ContentType 'application/json' -Body '{}' -TimeoutSec 3 | Out-Null
 $deadline=[DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
 while(-not $ServerProcess.HasExited -and [DateTime]::UtcNow -lt $deadline){& $Pump}
 if(-not $ServerProcess.HasExited){throw 'Сервер ещё завершает работу. Точка доступа оставлена включённой.'}
}
