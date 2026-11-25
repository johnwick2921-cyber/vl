# deploy/friend/load-harness/run-load-harness.ps1
# m391 gate: compile LoadHarness.cs with the .NET Framework 4.8 csc and run it
# against the obfuscated add-on DLL. The harness LOADS the DLL on CLR4, walks
# every type's methods/ctors with RuntimeHelpers.PrepareMethod, and counts
# System.Private.CoreLib references — a DLL that cannot actually load on NT8's
# CLR fails here even when `strings`-based checks pass. Returns the harness exit
# code (0 = 0 fail AND 0 corelib).
param(
  [Parameter(Mandatory=$true)][string]$Dll,
  [Parameter(Mandatory=$true)][string]$OutExe,
  [Parameter(Mandatory=$true)][string]$Cs,
  [Parameter(Mandatory=$true)][string]$ProbeDir
)

$ErrorActionPreference = 'Stop'
$csc = Join-Path $env:WINDIR 'Microsoft.NET\Framework64\v4.0.30319\csc.exe'
if (-not (Test-Path -LiteralPath $csc)) {
  Write-Error "csc.exe not found: $csc (need .NET Framework 4.x 64-bit)"
  exit 2
}

& $csc /nologo /out:$OutExe $Cs
if ($LASTEXITCODE -ne 0) {
  Write-Error "LoadHarness compile failed (csc exit $LASTEXITCODE)"
  exit 2
}

& $OutExe $Dll $ProbeDir
exit $LASTEXITCODE
