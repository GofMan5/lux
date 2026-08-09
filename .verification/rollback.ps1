$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
if ((git -C $root status --porcelain).Count -ne 0) { throw "Refusing rollback: working tree is not clean." }
if ((git -C $root rev-parse HEAD).Trim() -ne "43a377bd26647a8d58e2ecd50c6360031b87fc0e") { throw "Refusing rollback: HEAD is not the verified revision." }
git -C $root reset --hard 3f33aae
if ($LASTEXITCODE -ne 0) { throw "git reset failed with exit $LASTEXITCODE" }
Write-Host "Rolled back to empty baseline 3f33aae."