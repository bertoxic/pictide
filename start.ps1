$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot
& go run -p 1 ./cmd/pictide
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
