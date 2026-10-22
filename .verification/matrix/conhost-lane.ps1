# Classic conhost lane (host): run the showcase attached to the OS-provided
# console host (System32\conhost.exe), headless, capturing the raw wire
# transcript, plus a redirected-stdout pipe run that exercises the portable
# monochrome plain-output profile on Windows.
#
# `conhost.exe -- cmd /c script` attaches the child to a fresh console
# hosted by that conhost (the transcript's restored title proves the host
# identity). The hidden console delivers no input, so the showcase exits
# on stdin EOF; what the lane proves is the classic-console lifecycle:
# the win32-input-mode probe (DEC 9001) answers positively in plain
# conhost, enter/leave sequences pair byte for byte, and the process
# exits 0. Interactive conhost windows cannot be spawned without flipping
# the machine's default-terminal delegation, so key driving stays in the
# Windows Terminal lane; the win32-input parser itself is byte-covered by
# unit tests and by the WT lane's real SendKeys events.
#
# Run from the repo root: powershell -File .verification\matrix\conhost-lane.ps1
$ErrorActionPreference = 'Stop'
Set-Location (Join-Path $PSScriptRoot '..\..')
$root = (Get-Location).Path
$conhost = Join-Path $env:WINDIR 'System32\conhost.exe'
if (-not (Test-Path $conhost)) { throw "System32 conhost.exe not found" }
$os = [System.Environment]::OSVersion.VersionString
Write-Output "OS: $os"

go build -o .probe\showcase.exe ./examples/06-showcase
if ($LASTEXITCODE -ne 0) { throw "build failed" }

# Self-contained launcher batch (trailing space before `>` is deliberate,
# see the WT lane).
@"
@echo off
cd /d $root
title conhost-lane
.probe\showcase.exe
echo EXIT=%ERRORLEVEL% > .probe\win-exit.txt
"@ | Set-Content -Path .probe\conhost-lane.cmd -Encoding ASCII

# --- Lane 1: classic conhost console, raw wire transcript ---------------
$bin = Join-Path $PSScriptRoot 'conhost-transcript.bin'
Remove-Item $bin -ErrorAction SilentlyContinue
cmd /c "$conhost -- cmd /c .probe\conhost-lane.cmd > `"$bin`" 2>&1"
$rc = $LASTEXITCODE
Write-Output "conhost run exit code: $rc"
if ($rc -ne 0) { throw "conhost run exited $rc" }
$bytes = [System.IO.File]::ReadAllBytes($bin)
$text = [System.Text.Encoding]::GetEncoding(28591).GetString($bytes)
Write-Output ("transcript: {0} bytes" -f $bytes.Length)
# Lifecycle proof: win32-input-mode enabled then disabled, focus reporting
# on then off, full clear, title restored, cursor shown.
$esc = [char]27
foreach ($needle in @(
    "$esc[?9001h",  # enter: win32 input mode on (classic conhost answered
                    # the DEC 9001 probe positively)
    "$esc[?1004h",  # enter: focus reporting on
    "$esc[?25l",    # enter: cursor hidden
    "$esc[?9001l",  # leave: win32 input mode off
    "$esc[?1004l",  # leave: focus reporting off
    "$esc[2J",      # leave: screen cleared
    "$esc]0;",      # leave: title restored
    "$esc[?25h"     # leave: cursor shown
)) {
    if (-not $text.Contains($needle)) { throw "missing wire sequence: $needle" }
}
Write-Output "conhost wire lifecycle: all enter/leave sequences present"

# --- Lane 2: redirected stdout (pipe) — plain-output profile ------------
$pipe = Join-Path $PSScriptRoot 'windows-pipe-out.txt'
Remove-Item $pipe -ErrorAction SilentlyContinue
Remove-Item .probe\win-exit.txt -ErrorAction SilentlyContinue
cmd /c ".probe\conhost-lane.cmd > `"$pipe`" 2>&1"
$rc = $LASTEXITCODE
if ($rc -ne 0) { throw "pipe run exited $rc" }
if (-not (Test-Path .probe\win-exit.txt)) { throw "exit code file missing" }
$exit = (Get-Content .probe\win-exit.txt -Raw).Trim()
if ($exit -ne 'EXIT=0') { throw "pipe run recorded $exit" }
$snapshot = [System.Text.Encoding]::UTF8.GetString([System.IO.File]::ReadAllBytes($pipe))
if ($snapshot.Contains([char]0xFFFD)) { throw "pipe snapshot is not valid UTF-8" }
foreach ($needle in @('LUX TUI', '[tab] focus', 'q or ctrl+q')) {
    if (-not $snapshot.Contains($needle)) { throw "plain snapshot missing: $needle" }
}
Write-Output "pipe plain-output snapshot: UTF-8 clean, footer present, exit 0"
Write-Output "conhost lane OK"
