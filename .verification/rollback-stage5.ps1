$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
if ((git -C $root status --porcelain).Count -ne 0) { throw "Refusing rollback: working tree is not clean." }
git -C $root merge-base --is-ancestor d7b9084adf5027d353e7efc3e4d96a8fa3e36c96 HEAD
if ($LASTEXITCODE -ne 0) { throw "Refusing rollback: stage 5 baseline is not in HEAD." }
git -C $root reset --hard d7b9084adf5027d353e7efc3e4d96a8fa3e36c96
if ($LASTEXITCODE -ne 0) { throw "git reset failed with exit $LASTEXITCODE" }
Write-Host "Rolled back stage 5 to d7b9084adf5027d353e7efc3e4d96a8fa3e36c96."
