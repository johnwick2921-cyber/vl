# build-addon.ps1 - compile ninjascript/*.cs into VL.AddOn.dll with the
# NinjaTrader 8 built-in Roslyn compiler (Microsoft.CodeAnalysis.CSharp.dll).
# NO dotnet SDK required: the partner box has no SDK, only NT8.
#
# W-ADDON-ONE-CLICK (owner order 2026-10-17) + GAP 2 (DS-103,
# feat/addon-build-nt8-roslyn): this is the PRIMARY build path. The Go side
# falls back to dotnet.exe ONLY when this script fails.
#
# Two modes:
#   -Probe   compile a trivial source against the full reference set and print
#            "VL_ADDON_PROBE=ok" - used by /api/updates/readiness to prove the
#            no-SDK path actually works on this box.
#   normal   compile every <Src>\*.cs into <Out>\VL.AddOn.dll and print
#            "VL_ADDON_DLL=<path>" and "VL_ADDON_SHA256=<hex>".
#
# Nothing here deletes or overwrites an existing install file: the output goes
# only to <Out>, which the Go worker owns (a worker-made dir, NEVER the NT8
# folder).

param(
    [string]$Src,       # Windows path to the ninjascript source dir (normal mode)
    [string]$Out,       # Windows path to the output dir (normal mode)
    [string]$CustomDir, # Windows path to Documents\NinjaTrader 8\bin\Custom
    [string]$NT8Bin,    # Windows path to C:\Program Files\NinjaTrader 8\bin
    [switch]$Probe
)

$ErrorActionPreference = 'Stop'

function Fail([string]$msg) {
    Write-Output ("ERROR " + $msg)
    exit 1
}

if (-not $NT8Bin) { $NT8Bin = 'C:\Program Files\NinjaTrader 8\bin' }

# -- Load Roslyn from NT8's bin (the no-SDK compiler) ------------------------
$codeAnalysis = Join-Path $NT8Bin 'Microsoft.CodeAnalysis.dll'
$codeAnalysisCSharp = Join-Path $NT8Bin 'Microsoft.CodeAnalysis.CSharp.dll'
if (-not (Test-Path $codeAnalysisCSharp)) {
    Fail ("Microsoft.CodeAnalysis.CSharp.dll not found in " + $NT8Bin + " - reinstall or repair NinjaTrader 8")
}

# Roslyn's dependencies (System.Collections.Immutable, System.Reflection.Metadata,
# System.Runtime.CompilerServices.Unsafe, ...) live in the same NT8 bin dir but
# are NOT on PowerShell's probe path. Register a resolver that loads any
# requested assembly name from the NT8 bin dir, BEFORE the first Roslyn load.
$resolver = [System.ResolveEventHandler]{
    param($sender, $e)
    $short = ($e.Name -split ',')[0].Trim()
    $candidate = Join-Path $NT8Bin ($short + '.dll')
    if (Test-Path $candidate) {
        return [System.Reflection.Assembly]::LoadFrom($candidate)
    }
    return $null
}
[System.AppDomain]::CurrentDomain.add_AssemblyResolve($resolver)

try {
    [void][System.Reflection.Assembly]::LoadFrom($codeAnalysis)
    [void][System.Reflection.Assembly]::LoadFrom($codeAnalysisCSharp)
} catch {
    Fail ("failed to load Roslyn from " + $NT8Bin + ": " + $_.Exception.Message)
}

# -- Reference set: framework reference assemblies + the csproj mirror ------
# Add-Ref registers a metadata reference, deduped by file name (first wins), so
# the framework reference assemblies (compile-time facades) win over any
# runtime copy the csproj points at, while the NT8-specific DLLs the csproj
# lists get added on top.
$refPaths = @{}
function Add-Ref([string]$path) {
    if (-not $path) { return }
    if (-not (Test-Path $path)) { return }
    $full = [System.IO.Path]::GetFullPath($path)
    # Skip native (non-managed) DLLs ? the v4.8 reference-assemblies folder also
    # holds a few unmanaged shims (System.EnterpriseServices.*) that Roslyn
    # cannot consume as metadata references.
    try {
        [void][System.Reflection.AssemblyName]::GetAssemblyName($full)
    } catch {
        return
    }
    $key = [System.IO.Path]::GetFileName($full).ToLowerInvariant()
    if (-not $refPaths.ContainsKey($key)) {
        $refPaths[$key] = $full
    }
}

# 1. Framework reference assemblies (v4.8 preferred; any v4.* fallback; the
#    installed .NET Framework64 runtime as the last resort). These supply
#    mscorlib, System, System.Core, System.Xml, WPF and the other base types.
$fwRoot = 'C:\Program Files (x86)\Reference Assemblies\Microsoft\Framework\.NETFramework'
$fw = $null
if (Test-Path (Join-Path $fwRoot 'v4.8')) {
    $fw = Join-Path $fwRoot 'v4.8'
} else {
    $best = Get-ChildItem -Path $fwRoot -Directory -Filter 'v4.*' -ErrorAction SilentlyContinue |
        Sort-Object Name -Descending | Select-Object -First 1
    if ($best) { $fw = $best.FullName }
}
if (-not $fw) { $fw = 'C:\Windows\Microsoft.NET\Framework64\v4.0.30319' }
if (Test-Path $fw) {
    Get-ChildItem -Path $fw -Filter '*.dll' -File | ForEach-Object { Add-Ref $_.FullName }
}

# 2. Mirror the references in NinjaTrader.Custom.csproj (NT8 bin DLLs, WPF,
#    mscorlib, and NinjaTrader.Vendor under the per-user Documents folder).
if ($CustomDir) {
    $csproj = Join-Path $CustomDir 'NinjaTrader.Custom.csproj'
    if (Test-Path $csproj) {
        $xml = [System.IO.File]::ReadAllText($csproj)
        $rx = New-Object System.Text.RegularExpressions.Regex(
            '<Reference Include="[^"]+">.*?<HintPath>([^<]+)</HintPath>',
            [System.Text.RegularExpressions.RegexOptions]::Singleline)
        foreach ($m in $rx.Matches($xml)) {
            Add-Ref $m.Groups[1].Value
        }
    }
    # 3. NinjaTrader.Custom.dll - the assembly whose base types our AddOn
    #    extends (the csproj above BUILDS it, so it cannot list itself).
    Add-Ref (Join-Path $CustomDir 'NinjaTrader.Custom.dll')
    Add-Ref (Join-Path $CustomDir 'NinjaTrader.Vendor.dll')
}

# 4. Newtonsoft.Json (the dotnet recipe references it; harmless if unused).
Add-Ref (Join-Path $NT8Bin 'Newtonsoft.Json.dll')

if ($refPaths.Count -eq 0) {
    Fail "no reference assemblies could be resolved"
}

$refs = New-Object 'System.Collections.Generic.List[Microsoft.CodeAnalysis.MetadataReference]'
foreach ($key in ($refPaths.Keys | Sort-Object)) {
    $refs.Add([Microsoft.CodeAnalysis.MetadataReference]::CreateFromFile($refPaths[$key]))
}

# -- Compile ----------------------------------------------------------------
$trees = New-Object 'System.Collections.Generic.List[Microsoft.CodeAnalysis.SyntaxTree]'

if ($Probe) {
    $trees.Add([Microsoft.CodeAnalysis.CSharp.CSharpSyntaxTree]::ParseText('public static class __VlAddonProbe { }'))
    $asmName = '__VlAddonProbe'
} else {
    if (-not $Src) { Fail "-Src is required (unless -Probe)" }
    if (-not $Out) { Fail "-Out is required (unless -Probe)" }
    if (-not (Test-Path $Src)) { Fail "source dir not found: " + $Src }
    $srcFiles = @(Get-ChildItem -Path $Src -Filter '*.cs' -File)
    if ($srcFiles.Count -eq 0) { Fail "no .cs files in " + $Src }
    foreach ($f in $srcFiles) {
        $text = [System.IO.File]::ReadAllText($f.FullName)
        $trees.Add([Microsoft.CodeAnalysis.CSharp.CSharpSyntaxTree]::ParseText($text, $null, $f.FullName, $null))
    }
    $asmName = 'VL.AddOn'
}

$options = New-Object Microsoft.CodeAnalysis.CSharp.CSharpCompilationOptions([Microsoft.CodeAnalysis.OutputKind]::DynamicallyLinkedLibrary)
$compilation = [Microsoft.CodeAnalysis.CSharp.CSharpCompilation]::Create($asmName, $trees, $refs, $options)

if ($Probe) {
    $ms = New-Object System.IO.MemoryStream
    try {
        $result = $compilation.Emit($ms)
    } finally {
        $ms.Dispose()
    }
} else {
    if (-not (Test-Path $Out)) {
        New-Item -ItemType Directory -Path $Out -Force | Out-Null
    }
    $outFile = Join-Path $Out 'VL.AddOn.dll'
    $fs = [System.IO.File]::Open($outFile, [System.IO.FileMode]::Create)
    try {
        $result = $compilation.Emit($fs)
    } finally {
        $fs.Close()
    }
}

$errors = @($result.Diagnostics | Where-Object { $_.Severity -eq [Microsoft.CodeAnalysis.DiagnosticSeverity]::Error })
if ($errors.Count -gt 0) {
    foreach ($d in $errors) {
        Write-Output ("ERROR " + $d.Id + ": " + $d.GetMessage())
    }
    Fail ("compilation failed with " + $errors.Count + " error(s)")
}
if (-not $result.Success) {
    Fail "Emit returned failure with no error-level diagnostics"
}

if ($Probe) {
    Write-Output 'VL_ADDON_PROBE=ok'
    exit 0
}

# -- sha256 + machine-readable result ---------------------------------------
$sha = [System.Security.Cryptography.SHA256]::Create()
try {
    $bytes = [System.IO.File]::ReadAllBytes($outFile)
    $hash = $sha.ComputeHash($bytes)
    $hex = -join ($hash | ForEach-Object { $_.ToString('x2') })
} finally {
    $sha.Dispose()
}

Write-Output ("VL_ADDON_DLL=" + $outFile)
Write-Output ("VL_ADDON_SHA256=" + $hex)
exit 0
