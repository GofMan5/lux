# Idle-CPU probe (Windows): start an example in its own console window,
# let it settle, then read process CPU time across a quiet window.
# Usage: powershell -File .verification\slice57\idle.ps1 <exe> <seconds> <label>
param([string]$Exe, [int]$Seconds = 20, [string]$Label = 'run')
$p = Start-Process -FilePath $Exe -PassThru -WindowStyle Minimized
Start-Sleep -Seconds 3
$p.Refresh(); $t0 = $p.TotalProcessorTime; $w0 = Get-Date
Start-Sleep -Seconds $Seconds
$p.Refresh(); $t1 = $p.TotalProcessorTime; $w1 = Get-Date
Stop-Process -Id $p.Id -Force
$cpu = ($t1 - $t0).TotalMilliseconds
$wall = ($w1 - $w0).TotalMilliseconds
"{0}: cpu={1:N1} ms over {2:N0} ms wall = {3:N3}% of one core" -f $Label, $cpu, $wall, (100 * $cpu / $wall)
