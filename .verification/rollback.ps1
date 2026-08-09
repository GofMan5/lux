$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
if ((git -C $root status --porcelain).Count -ne 0) { throw "Refusing rollback: working tree is not clean." }
if ((git -C $root rev-parse HEAD).Trim() -ne "1bf4a632e9d185b5e8e4d48ff6d96fa6b380a2ba") { throw "Refusing rollback: HEAD is not the verified revision." }
git -C $root reset --hard 3f33aae
if ($LASTEXITCODE -ne 0) { throw "git reset failed with exit $LASTEXITCODE" }
Write-Host "Rolled back to empty baseline 3f33aae."