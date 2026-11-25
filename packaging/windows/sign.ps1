# Signs Windows files with a code-signing certificate, when one is configured.
# Without one it does nothing and says so: the files ship unsigned (Windows
# then warns "Windows protected your PC" before the first run).
#
#   pwsh packaging/windows/sign.ps1 dist/windows/agentctl.exe [more files]
#
# Configured by two environment variables (repository secrets in CI):
#   WINDOWS_CERT_PFX           the certificate with its private key, .pfx, base64
#   WINDOWS_CERT_PASSWORD      the .pfx password
# Optional: WINDOWS_TIMESTAMP_URL (default http://timestamp.digicert.com).
#
# A certificate kept on a hardware token or a cloud key service, or a service
# such as SignPath, replaces this script; the release workflow calls it at the
# same two places (agentctl.exe and the app before packaging, then the setup).
param([Parameter(Mandatory = $true, ValueFromRemainingArguments = $true)][string[]]$Files)
$ErrorActionPreference = 'Stop'

if (-not $env:WINDOWS_CERT_PFX) {
  Write-Output "::warning::no WINDOWS_CERT_PFX: Windows files are not signed"
  exit 0
}

$signtool = Get-ChildItem "${env:ProgramFiles(x86)}\Windows Kits\10\bin\*\x64\signtool.exe" -ErrorAction SilentlyContinue |
  Sort-Object FullName -Descending | Select-Object -First 1
if (-not $signtool) { throw "signtool.exe not found (Windows SDK)" }

$pfx = Join-Path $env:RUNNER_TEMP 'sign.pfx'
if (-not $env:RUNNER_TEMP) { $pfx = Join-Path ([IO.Path]::GetTempPath()) 'sign.pfx' }
[IO.File]::WriteAllBytes($pfx, [Convert]::FromBase64String($env:WINDOWS_CERT_PFX))
$ts = if ($env:WINDOWS_TIMESTAMP_URL) { $env:WINDOWS_TIMESTAMP_URL } else { 'http://timestamp.digicert.com' }
try {
  foreach ($f in $Files) {
    & $signtool.FullName sign /fd SHA256 /td SHA256 /tr $ts /f $pfx /p $env:WINDOWS_CERT_PASSWORD $f
    if ($LASTEXITCODE -ne 0) { throw "signing $f failed" }
    & $signtool.FullName verify /pa $f
    if ($LASTEXITCODE -ne 0) { throw "$f doesn't verify" }
  }
} finally {
  Remove-Item $pfx -Force -ErrorAction SilentlyContinue
}
