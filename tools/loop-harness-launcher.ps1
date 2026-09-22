# Installed next to platform binaries as .claude/bin/loop-harness.ps1.
$ErrorActionPreference = 'Stop'
$platformArch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
if ($platformArch -ne 'X64') { throw "Unsupported Windows architecture: $platformArch. Install a native build." }
$binaryPath = Join-Path $PSScriptRoot 'loop-harness-windows-amd64.exe'
if (-not (Test-Path -LiteralPath $binaryPath -PathType Leaf)) { throw "Missing native Harness: $binaryPath" }
& $binaryPath @args
exit $LASTEXITCODE
