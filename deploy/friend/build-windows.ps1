# build-windows.ps1 - compile the friend add-on DLL and obfuscate it (B10.1).
# Runs on Windows via `powershell.exe -File deploy/friend/build-windows.ps1`.
# It delegates the compile to deploy/addon/build-addon.ps1 (NT8's own Roslyn
# compiler, no dotnet SDK) and then runs Obfuscar (rename symbols + encrypt
# strings). The OUTPUT goes only to a fresh temp dir — NEVER the NinjaTrader
# folder (the NT8 DLLs are read as references only).
#
# Prints (stdout):
#   VL_ADDON_OBFUSCATED_DLL=<path>
#   VL_ADDON_OBFUSCATED_SHA256=<hex>
# and exits non-zero on any failure.
param(
    [string]$Src,          # Windows path to the ninjascript source dir
    [string]$Out,          # Windows path to the output dir (fresh temp; NEVER NT8)
    [string]$CustomDir,    # Windows path to Documents\NinjaTrader 8\bin\Custom
    [string]$NT8Bin = 'C:\Program Files\NinjaTrader 8\bin',
    [string]$Obfuscar = 'obfuscar.console',   # Obfuscar.Console.exe or the dotnet tool
    [string]$ObfuscarCfg = ''                 # defaults to the Obfuscar.xml beside this script
)

$ErrorActionPreference = 'Stop'

function Fail([string]$msg) {
    Write-Output ("ERROR " + $msg)
    exit 1
}

if (-not $Src) { Fail "-Src is required" }
if (-not $Out) { Fail "-Out is required" }
if (-not $CustomDir) { Fail "-CustomDir is required" }
if (-not $ObfuscarCfg) {
    $ObfuscarCfg = Join-Path $PSScriptRoot 'Obfuscar.xml'
}

# 1. Compile with NT8's own compiler into a plain (unobfuscated) temp dir.
$plain = Join-Path $Out 'plain'
New-Item -ItemType Directory -Force -Path $plain | Out-Null

$addonScript = Join-Path $PSScriptRoot '..\addon\build-addon.ps1'
& powershell.exe -NoProfile -ExecutionPolicy Bypass -File $addonScript `
    -Src $Src -Out $plain -CustomDir $CustomDir -NT8Bin $NT8Bin
if ($LASTEXITCODE -ne 0) { Fail "build-addon.ps1 failed (exit $LASTEXITCODE)" }

$plainDll = Join-Path $plain 'VL.AddOn.dll'
if (-not (Test-Path $plainDll)) { Fail "expected $plainDll after compile" }

# 2. Obfuscate (rename symbols + encrypt strings) into <Out>\obfuscated.
$obf = Join-Path $Out 'obfuscated'
New-Item -ItemType Directory -Force -Path $obf | Out-Null

# Write a config with the real InPath/OutPath so the checked-in XML stays a
# documented default.
$cfg = [xml](Get-Content -Raw $ObfuscarCfg)
foreach ($v in $cfg.Obfuscator.Var) {
    if ($v.name -eq 'InPath') { $v.SetAttribute('value', $plain) }
    elseif ($v.name -eq 'OutPath') { $v.SetAttribute('value', $obf) }
    elseif ($v.name -eq 'ExtraFrameworkFolders') { $v.SetAttribute('value', "$NT8Bin;$CustomDir") }
}
$cfgPath = Join-Path $Out 'Obfuscar.generated.xml'
$cfg.Save($cfgPath)

& $Obfuscar $cfgPath
if ($LASTEXITCODE -ne 0) { Fail "Obfuscar failed (exit $LASTEXITCODE)" }

$obfDll = Join-Path $obf 'VL.AddOn.dll'
if (-not (Test-Path $obfDll)) { Fail "expected $obfDll after obfuscation" }

# 3. SHA-256 of the obfuscated DLL.
$sha = (Get-FileHash -Algorithm SHA256 $obfDll).Hash.ToLowerInvariant()

Write-Output ("VL_ADDON_OBFUSCATED_DLL=" + $obfDll)
Write-Output ("VL_ADDON_OBFUSCATED_SHA256=" + $sha)
