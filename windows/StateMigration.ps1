# Importing this file performs no migration. The launcher owns both name locks.
function Initialize-AtblseData([string]$BaseDirectory) {
 $ErrorActionPreference='Stop'
 $base=[IO.Path]::GetFullPath($BaseDirectory)
 $current=Join-Path $base 'atblse'
 $legacy=Join-Path $base 'AutoblowLocal'
 if(Test-Path -LiteralPath $current){
  $item=Get-Item -LiteralPath $current -Force
  if(-not $item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)){throw 'Invalid atblse data directory.'}
  if(Test-Path -LiteralPath $legacy){
   $marker=Join-Path $current 'migration-v1.json'
   if(-not (Test-Path -LiteralPath $marker) -or (Get-Item -LiteralPath $marker).Length -gt 1MB){throw 'Both atblse and legacy data exist without a valid migration marker. Reconcile private backups first; nothing was overwritten.'}
   $saved=Get-Content -LiteralPath $marker -Raw | ConvertFrom-Json
   if($saved.version -ne 1 -or $saved.source -ne 'AutoblowLocal' -or $null -eq $saved.files){throw 'Invalid migration marker. Preserve both private data copies before recovery.'}
  }
  return $current
 }
 if(-not (Test-Path -LiteralPath $legacy)){
  [IO.Directory]::CreateDirectory($current) | Out-Null
  return $current
 }
 # Do not move live state. The legacy launcher lock is acquired by our caller;
 # the API port also catches an independently running legacy server.
 if(@(Get-NetTCPConnection -State Listen -LocalPort 20880 -ErrorAction SilentlyContinue).Count){throw 'Stop the previous server before migrating its data.'}
 $source=Get-Item -LiteralPath $legacy -Force
 if(-not $source.PSIsContainer -or ($source.Attributes -band [IO.FileAttributes]::ReparsePoint)){throw 'Legacy data must be a normal directory.'}
 $entries=[Collections.Generic.List[object]]::new()
 $pending=[Collections.Generic.Queue[string]]::new();$pending.Enqueue($legacy)
 $total=[long]0
 while($pending.Count){
  foreach($item in Get-ChildItem -LiteralPath $pending.Dequeue() -Force){
   if($item.Attributes -band [IO.FileAttributes]::ReparsePoint){throw 'Migration refuses links and junctions. Preserve the backup and resolve them manually.'}
   $entries.Add($item)
   if($entries.Count -gt 4096){throw 'Migration exceeds the supported file count.'}
   if($item.PSIsContainer){$pending.Enqueue($item.FullName)}else{$total+=$item.Length}
   if($total -gt 1GB){throw 'Migration exceeds the supported data size.'}
  }
 }
 $stage=Join-Path $base ('atblse-migration-'+[guid]::NewGuid().ToString('N'))
 [IO.Directory]::CreateDirectory($stage) | Out-Null
 # Incomplete staging is deliberately retained for diagnosis; never erase a backup.
 $hashes=@{}
 foreach($item in $entries){
  $relative=$item.FullName.Substring($legacy.Length+1)
  $destination=Join-Path $stage $relative
  if($item.PSIsContainer){[IO.Directory]::CreateDirectory($destination) | Out-Null;continue}
  $before=(Get-FileHash -LiteralPath $item.FullName -Algorithm SHA256).Hash
  [IO.Directory]::CreateDirectory((Split-Path -Parent $destination)) | Out-Null
  [IO.File]::Copy($item.FullName,$destination,$false)
  $stream=[IO.File]::Open($destination,[IO.FileMode]::Open,[IO.FileAccess]::Write,[IO.FileShare]::None)
  try{$stream.Flush($true)}finally{$stream.Dispose()}
  if((Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash -ne $before){throw 'Migration copy verification failed; original data preserved.'}
  $hashes[$relative]=$before
 }
 foreach($relative in $hashes.Keys){
  if((Get-FileHash -LiteralPath (Join-Path $legacy $relative) -Algorithm SHA256).Hash -ne $hashes[$relative]){throw 'Legacy data changed during migration; stop its writer and retry.'}
 }
 $marker=Join-Path $stage 'migration-v1.json'
 [IO.File]::WriteAllText($marker,(@{version=1;source='AutoblowLocal';files=$hashes;createdAt=[DateTime]::UtcNow.ToString('o')} | ConvertTo-Json -Depth 4),[Text.UTF8Encoding]::new($false))
 $stream=[IO.File]::Open($marker,[IO.FileMode]::Open,[IO.FileAccess]::Write,[IO.FileShare]::None)
 try{$stream.Flush($true)}finally{$stream.Dispose()}
 # Same-parent directory rename publishes the complete copy in one step.
 [IO.Directory]::Move($stage,$current)
 return $current
}
