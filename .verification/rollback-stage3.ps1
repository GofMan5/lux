$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
if ((git -C $root status --porcelain).Count -ne 0) { throw "Refusing rollback: working tree is not clean." }
git -C $root merge-base --is-ancestor 724640bab000466b5d679810981b888971ecf8ed HEAD
if ($LASTEXITCODE -ne 0) { throw "Refusing rollback: stage 3 baseline is not in HEAD." }
git -C $root reset --hard 724640bab000466b5d679810981b888971ecf8ed
if ($LASTEXITCODE -ne 0) { throw "git reset failed with exit $LASTEXITCODE" }
Write-Host "Rolled back stage 3 to 724640bab000466b5d679810981b888971ecf8ed."