$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
if ((git -C $root status --porcelain).Count -ne 0) { throw "Refusing rollback: working tree is not clean." }
git -C $root merge-base --is-ancestor e6a92030520cd0d70e0a64a79bdb98e8835012ee HEAD
if ($LASTEXITCODE -ne 0) { throw "Refusing rollback: verified implementation is not in HEAD." }
git -C $root reset --hard 3f33aae
if ($LASTEXITCODE -ne 0) { throw "git reset failed with exit $LASTEXITCODE" }
Write-Host "Rolled back to empty baseline 3f33aae."
