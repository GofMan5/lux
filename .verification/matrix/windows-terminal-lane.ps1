# Windows Terminal lane (host): launch the showcase in a Windows Terminal
# window, drive it through real Win32 key events (WScript SendKeys to the
# activated window), capture window screenshots, and require a clean exit
# (exit code 0 recorded by the launcher batch file).
#
# Run from the repo root: powershell -File .verification\matrix\windows-terminal-lane.ps1
$ErrorActionPreference = 'Stop'
Set-Location (Join-Path $PSScriptRoot '..\..')
$wt = "$env:LOCALAPPDATA\Microsoft\WindowsApps\wt.exe"
if (-not (Test-Path $wt)) { throw "Windows Terminal not found" }
$wtVersion = (Get-AppxPackage Microsoft.WindowsTerminal).Version
Write-Output "Windows Terminal: $wtVersion"

Remove-Item .probe\win-exit.txt -ErrorAction SilentlyContinue
# A leftover keyprobe tab would keep the Windows Terminal host alive after
# the showcase tab closes, so the window teardown below waits forever.
Get-Process keyprobe -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
go build -o .probe\showcase.exe ./examples/06-showcase
if ($LASTEXITCODE -ne 0) { throw "build failed" }

# Self-contained launcher batch: runs the showcase, then records the exit
# code for the lane to read. The trailing space before `>` matters: cmd's
# `echo EXIT=%ERRORLEVEL%>file` without it strips the `=0` into a redirect
# handle and writes a truncated title.
$root = (Get-Location).Path
@"
@echo off
cd /d $root
title showcase
.probe\showcase.exe
echo EXIT=%ERRORLEVEL% > .probe\win-exit.txt
"@ | Set-Content -Path .probe\wt-lane.cmd -Encoding ASCII

Start-Process $wt -ArgumentList '-d', $root, 'cmd', '/c', '.probe\wt-lane.cmd'
# The GUI window belongs to the WindowsTerminal process, not to the
# console-subsystem showcase process — find it by its title.
$deadline = (Get-Date).AddSeconds(10)
do {
    Start-Sleep -Milliseconds 500
    $app = Get-Process WindowsTerminal -ErrorAction SilentlyContinue |
        Where-Object { $_.MainWindowTitle -match 'showcase' } | Select-Object -First 1
} while (-not $app -and (Get-Date) -lt $deadline)
if (-not $app) { throw "showcase window did not appear" }
Write-Output ("window title: " + $app.MainWindowTitle)

Add-Type @"
using System;
using System.Runtime.InteropServices;
public static class WinRect {
    [DllImport("user32.dll")]
    public static extern bool GetWindowRect(IntPtr hWnd, out RECT rect);
    [DllImport("user32.dll")]
    public static extern bool SetForegroundWindow(IntPtr hWnd);
    public struct RECT { public int Left, Top, Right, Bottom; }
}
"@
Add-Type -AssemblyName System.Drawing
$ws = New-Object -ComObject WScript.Shell

function Shot($name) {
    $r = New-Object WinRect+RECT
    [WinRect]::GetWindowRect($app.MainWindowHandle, [ref]$r) | Out-Null
    $w = $r.Right - $r.Left; $h = $r.Bottom - $r.Top
    $bmp = New-Object System.Drawing.Bitmap($w, $h)
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.CopyFromScreen($r.Left, $r.Top, 0, 0, $bmp.Size)
    $path = Join-Path $PSScriptRoot $name
    $bmp.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
    $g.Dispose(); $bmp.Dispose()
    Write-Output ("screenshot: " + $path)
}

function Key($keys) {
    [WinRect]::SetForegroundWindow($app.MainWindowHandle) | Out-Null
    Start-Sleep -Milliseconds 300
    if (-not $ws.AppActivate($app.Id)) { throw "AppActivate failed" }
    Start-Sleep -Milliseconds 200
    $ws.SendKeys($keys)
}

Shot 'windows-terminal-main.png'
Key '{F1}'          # open the help dialog
Start-Sleep -Seconds 1
Shot 'windows-terminal-help.png'
Key '{ESC}'         # close the dialog
Key '{TAB}'         # move focus
Start-Sleep -Milliseconds 500
Key 'z'             # a rune into the focused region (edit path; 'q' would
                    # quit the showcase via its plain-q fallback)
Start-Sleep -Seconds 1
Key '^q'           # ctrl+q — quit
$deadline = (Get-Date).AddSeconds(10)
while (-not (Test-Path .probe\win-exit.txt) -and (Get-Date) -lt $deadline) {
    Start-Sleep -Milliseconds 250
}
if (-not (Test-Path .probe\win-exit.txt)) { throw "exit code file missing: app did not exit" }
$exit = Get-Content .probe\win-exit.txt -Raw
Write-Output "showcase exit: $exit"
if ($exit.Trim() -ne 'EXIT=0') { throw "nonzero exit: $exit" }
# Best-effort window teardown: whether the last tab closes on process exit
# is a Windows Terminal profile setting (closeOnExit), not a lux-tui
# contract, so the lane only tries to close the host and never fails on a
# lingering window — zombie tabs from earlier runs share that host too.
try {
    $null = $app.CloseMainWindow()
    Start-Sleep -Milliseconds 500
    Get-Process -Id $app.Id -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
} catch { }
Write-Output "windows terminal lane OK"
