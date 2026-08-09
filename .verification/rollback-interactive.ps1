$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
if ((git -C $root status --porcelain).Count -ne 0) { throw "Refusing rollback: working tree is not clean." }
git -C $root merge-base --is-ancestor b2caec1751f6f23157260f39d24db7ee6a5eb5dd HEAD
if ($LASTEXITCODE -ne 0) { throw "Refusing rollback: interaction baseline is not in HEAD." }
git -C $root reset --hard b2caec1751f6f23157260f39d24db7ee6a5eb5dd
if ($LASTEXITCODE -ne 0) { throw "git reset failed with exit $LASTEXITCODE" }
Write-Host "Rolled back interactive extension to b2caec1751f6f23157260f39d24db7ee6a5eb5dd."