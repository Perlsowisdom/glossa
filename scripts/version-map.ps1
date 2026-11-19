# Prints a release version in the form a packager needs: nsis or msix give the four-part number
# (0.4.0-alpha -> 0.4.0.0); validate checks it is MAJOR.MINOR.PATCH with an optional -suffix.
# The rules and their tests are in tools/relver.
param([Parameter(Mandatory)][ValidateSet('validate', 'nsis', 'msix')][string]$Form, [Parameter(Mandatory)][string]$Version)
$ErrorActionPreference = 'Stop'
Push-Location (Join-Path (Split-Path $PSScriptRoot) 'tools')
try {
  $out = go run ./versionmap $Form $Version
  if ($LASTEXITCODE -ne 0) { throw "bad version '$Version'" }
  $out
} finally { Pop-Location }
