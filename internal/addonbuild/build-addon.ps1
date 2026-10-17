# build-addon.ps1 - compile NinjaTrader 8 NinjaScript with NT8's built-in
# Roslyn compiler (Microsoft.CodeAnalysis.CSharp.dll).
# NO dotnet SDK required: the partner box has no SDK, only NT8.
#
# W-ADDON-ONE-CLICK (owner order 2026-10-17) + GAP 2 (DS-103) + release #35
# (DS-103, feat/no-f5-custom-rebuild).
#
# Modes:
#   normal    compile every <Src>\*.cs into <Out>\VL.AddOn.dll and print
#             "VL_ADDON_DLL=<path>" and "VL_ADDON_SHA256=<hex>".
#   -Probe    compile a trivial source against the full reference set and print
#             "VL_ADDON_PROBE=ok" - used by /api/updates/readiness to prove the
#             no-SDK path actually works on this box.
#   -Custom   compile every <CustomDir>\**\*.cs EXCEPT VL*.cs into
#             <Out>\NinjaTrader.Custom.dll, then verify the new DLL's PUBLIC
#             type list equals the old Custom.dll's minus the VL AddOn types.
#             This replaces the one-time F5: with NT8 closed the worker rebuilds
#             Custom.dll itself, ACCEPTING it only when compile errors = 0 AND
#             the public types are unchanged apart from the VL types. It prints
#             VL_CUSTOM_TYPES_OK (or VL_CUSTOM_TYPES_DIFF + the diff) and
#             VL_CUSTOM_DLL / VL_CUSTOM_SHA256.
#
# Nothing here deletes or overwrites an existing install file: the output goes
# only to <Out>, which the Go worker owns (a worker-made dir, NEVER the NT8
# folder). The old Custom.dll is only READ here (for the type-list diff); the Go
# side MOVEs it to a backup and MOVEs the new one in after acceptance.

param(
    [string]$Src,       # Windows path to the ninjascript source dir (normal mode)
    [string]$Out,       # Windows path to the output dir
    [string]$CustomDir, # Windows path to Documents\NinjaTrader 8\bin\Custom
    [string]$NT8Bin,    # Windows path to C:\Program Files\NinjaTrader 8\bin
    [switch]$Probe,
    [switch]$Custom
)

$ErrorActionPreference = 'Stop'

function Fail([string]$msg) {
    Write-Output ("ERROR " + $msg)
    exit 1
}

# Get-PublicTypes lists the PUBLIC type definitions (namespace.name) of a .NET
# assembly, read straight from metadata (no loading, no dependency resolution).
function Get-PublicTypes([string]$dll) {
    $fs = [System.IO.File]::OpenRead($dll)
    try {
        $pe = [System.Reflection.PortableExecutable.PEReader]::new($fs)
        $md = [System.Reflection.Metadata.PEReaderExtensions]::GetMetadataReader($pe)
        $out = @()
        $en = $md.TypeDefinitions.GetEnumerator()
        while ($en.MoveNext()) {
            $t = $md.GetTypeDefinition($en.Current)
            $vis = $t.Attributes -band 0x7
            if ($vis -eq 0x1 -or $vis -eq 0x2) {  # TypeAttributes.Public / NestedPublic
                $ns = $md.GetString($t.Namespace)
                $name = $md.GetString($t.Name)
                if ($ns) { $out += ($ns + '.' + $name) } else { $out += $name }
            }
        }
        return $out
    } finally {
        $fs.Close()
    }
}

# Get-ManifestResourceNames lists the embedded manifest resource names of a DLL.
function Get-ManifestResourceNames([string]$dll) {
    $fs = [System.IO.File]::OpenRead($dll)
    try {
        $pe = [System.Reflection.PortableExecutable.PEReader]::new($fs)
        $md = [System.Reflection.Metadata.PEReaderExtensions]::GetMetadataReader($pe)
        $out = @()
        foreach ($h in $md.ManifestResources) {
            $r = $md.GetManifestResource($h)
            $out += $md.GetString($r.Name)
        }
        return $out
    } finally {
        $fs.Close()
    }
}

# Get-ReferencedAssemblyNames lists the simple names of the assemblies a DLL
# references (its AssemblyRef table).
function Get-ReferencedAssemblyNames([string]$dll) {
    $fs = [System.IO.File]::OpenRead($dll)
    try {
        $pe = [System.Reflection.PortableExecutable.PEReader]::new($fs)
        $md = [System.Reflection.Metadata.PEReaderExtensions]::GetMetadataReader($pe)
        $out = @()
        foreach ($h in $md.AssemblyReferences) {
            $a = $md.GetAssemblyReference($h)
            $out += $md.GetString($a.Name)
        }
        return $out
    } finally {
        $fs.Close()
    }
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
    Add-Ref (Join-Path $CustomDir 'NinjaTrader.Vendor.dll')
}

# 3. Newtonsoft.Json (the dotnet recipe references it; harmless if unused).
Add-Ref (Join-Path $NT8Bin 'Newtonsoft.Json.dll')

# In the normal VL.AddOn mode the AddOn's base types live in Custom.dll, so it
# is a reference. In -Custom mode Custom.dll is the OUTPUT - never reference it
# (that would be the old DLL we are replacing).
if (-not $Custom) {
    Add-Ref (Join-Path $CustomDir 'NinjaTrader.Custom.dll')
}

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
} elseif ($Custom) {
    if (-not $CustomDir) { Fail "-CustomDir is required with -Custom" }
    if (-not (Test-Path $CustomDir)) { Fail "custom dir not found: " + $CustomDir }
    # Every .cs under bin/Custom EXCEPT the release's AddOns/VL*.cs (the files
    # being migrated out). Other VL*.cs elsewhere (e.g. Strategies/vltrader.cs,
    # the legacy CSV strategy) is KEPT - it is not the release's file.
    $srcFiles = @(Get-ChildItem -Path $CustomDir -Recurse -Filter '*.cs' -File |
        Where-Object { -not ($_.Directory.Name -eq 'AddOns' -and $_.Name -like 'VL*.cs') })
    if ($srcFiles.Count -eq 0) { Fail "no .cs files in " + $CustomDir }
    foreach ($f in $srcFiles) {
        $text = [System.IO.File]::ReadAllText($f.FullName)
        $trees.Add([Microsoft.CodeAnalysis.CSharp.CSharpSyntaxTree]::ParseText($text, $null, $f.FullName, $null))
    }
    $asmName = 'NinjaTrader.Custom'
} else {
    if (-not $Src) { Fail "-Src is required" }
    if (-not $Out) { Fail "-Out is required" }
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
    if (-not $Out) { Fail "-Out is required" }
    if (-not (Test-Path $Out)) {
        New-Item -ItemType Directory -Path $Out -Force | Out-Null
    }
    $outName = if ($Custom) { 'NinjaTrader.Custom.dll' } else { 'VL.AddOn.dll' }
    $outFile = Join-Path $Out $outName
    $fs = [System.IO.File]::Open($outFile, [System.IO.FileMode]::Create)
    try {
        # NOTE (DS-103, release #35): embedding the neutral Resource.resx
        # requires Roslyn's ResourceDescription, whose static factories
        # (Create/FromFile/FromStream) all throw StackOverflowException under
        # PowerShell 5.1 + Roslyn 5.0 (.NET Framework reflection). The emit
        # therefore does NOT embed resources; the -Custom acceptance below
        # REJECTS the result when the embedded-resource set differs from the old
        # Custom.dll, so a rebuild without resources falls back to F5 instead of
        # silently swapping in a deficient DLL.
        $result = $compilation.Emit($fs)
    } finally {
        $fs.Close()
    }
}

$errors = @($result.Diagnostics | Where-Object { $_.Severity -eq [Microsoft.CodeAnalysis.DiagnosticSeverity]::Error })
if ($errors.Count -gt 0) {
    if ($Custom) { Write-Output ("VL_CUSTOM_ERRORS=" + $errors.Count) }
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

# -Custom: accept only if the public type list is unchanged minus the VL AddOn
# types (indicators, strategies, Apex untouched).
if ($Custom) {
    $oldDll = Join-Path $CustomDir 'NinjaTrader.Custom.dll'
    if (-not (Test-Path $oldDll)) { Fail "old Custom.dll not found at " + $oldDll }
    $oldTypes = @(Get-PublicTypes $oldDll)
    $newTypes = @(Get-PublicTypes $outFile)
    # The release's AddOn sources all live in the AddOns namespace, so subtract
    # exactly those public types (VLBarsSubscriptionManager, VLContractResolver,
    # VLHistoryPullEntry, VLHistoryPullManager, VLInstrumentLookup,
    # VLTraderTCPClient) and NOTHING else - a VL type elsewhere (e.g.
    # NinjaTrader.NinjaScript.Strategies.VLTrader) is KEPT.
    $vlPrefix = 'NinjaTrader.NinjaScript.AddOns.VL'
    $expected = @($oldTypes | Where-Object { -not $_.StartsWith($vlPrefix) })
    $diff = @(Compare-Object $expected $newTypes)
    if ($diff) {
        Write-Output 'VL_CUSTOM_TYPES_DIFF'
        foreach ($d in $diff) {
            Write-Output ("TYPE " + $d.SideIndicator + " " + $d.InputObject)
        }
        Fail "the rebuilt Custom.dll public type list differs from the old one minus the release's AddOn types"
    }
    # DS-102 review: the type list is necessary but not sufficient. The rebuilt
    # DLL must also carry the SAME embedded resources and reference the SAME
    # assemblies as the old one, or it is REJECTED (never a silent swap).
    $oldRes = @(Get-ManifestResourceNames $oldDll)
    $newRes = @(Get-ManifestResourceNames $outFile)
    if (@(Compare-Object $oldRes $newRes).Count -gt 0) {
        Write-Output 'VL_CUSTOM_TYPES_DIFF'
        Write-Output ("RESOURCE_DIFF old=[" + ($oldRes -join ',') + "] new=[" + ($newRes -join ',') + "]")
        Fail "the rebuilt Custom.dll embeds different resources than the old one"
    }
    $oldRefs = @(Get-ReferencedAssemblyNames $oldDll)
    $newRefs = @(Get-ReferencedAssemblyNames $outFile)
    if (@(Compare-Object $oldRefs $newRefs).Count -gt 0) {
        Write-Output 'VL_CUSTOM_TYPES_DIFF'
        Write-Output ("REFERENCE_DIFF old=[" + ($oldRefs -join ',') + "] new=[" + ($newRefs -join ',') + "]")
        Fail "the rebuilt Custom.dll references different assemblies than the old one"
    }
    Write-Output 'VL_CUSTOM_TYPES_OK'
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

if ($Custom) {
    Write-Output ("VL_CUSTOM_DLL=" + $outFile)
    Write-Output ("VL_CUSTOM_SHA256=" + $hex)
} else {
    Write-Output ("VL_ADDON_DLL=" + $outFile)
    Write-Output ("VL_ADDON_SHA256=" + $hex)
}
exit 0


