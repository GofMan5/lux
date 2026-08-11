$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
if ((git -C $root status --porcelain).Count -ne 0) { throw "Refusing rollback: working tree is not clean." }
git -C $root merge-base --is-ancestor 3d362f31a43f79cbbca06ad2454b7af5795139dc HEAD
if ($LASTEXITCODE -ne 0) { throw "Refusing rollback: stage 4 baseline is not in HEAD." }
git -C $root reset --hard 3d362f31a43f79cbbca06ad2454b7af5795139dc
if ($LASTEXITCODE -ne 0) { throw "git reset failed with exit $LASTEXITCODE" }
Write-Host "Rolled back stage 4 to 3d362f31a43f79cbbca06ad2454b7af5795139dc."
