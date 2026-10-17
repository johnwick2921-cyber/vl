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
#   -Custom   compile VLCustomBuilder.cs (the C# builder sitting next to this
#             ps1) with NT8's Roslyn, then RUN it. The builder rebuilds
#             NinjaTrader.Custom.dll from its own .csproj, embeds the neutral
#             Resource.resx, and runs the full acceptance (resources / refs /
#             public type signatures / AssemblyName+Version == old, minus the VL
#             AddOn types). This replaces the one-time F5: with NT8 closed the
#             worker rebuilds Custom.dll itself. The builder prints
#             VL_CUSTOM_TYPES_OK (or VL_CUSTOM_TYPES_DIFF / VL_CUSTOM_ERRORS +
#             the diff) and VL_CUSTOM_DLL / VL_CUSTOM_SHA256, and exits non-zero
#             on any mismatch - so a failure falls back to F5, never a silent
#             swap. (FOLD 3: the resource embed must run in compiled C#, because
#             PowerShell 5.1 StackOverflows on Roslyn's Func(Stream) delegate.)
#
# Nothing here deletes or overwrites an existing install file: the output goes
# only to <Out>, which the Go worker owns (a worker-made dir, NEVER the NT8
# folder). The old Custom.dll is only READ here (for the acceptance diff); the
# Go side MOVEs it to a backup and MOVEs the new one in after acceptance.

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

# Build-VLCustomBuilder compiles deploy/addon/VLCustomBuilder.cs (sitting next to
# this ps1) into a console exe with NT8's Roslyn, then returns the exe path. The
# builder does the whole -Custom rebuild - compile from the csproj + embed the
# neutral Resource.resx + all acceptance - because Roslyn's ResourceDescription
# can only be used from COMPILED C#: PowerShell 5.1 StackOverflows on its
# Func(Stream) delegate (verified). The builder exe resolves Roslyn at run time
# from NT8's bin via its own AssemblyResolve.
function Build-VLCustomBuilder([string]$NT8Bin) {
    $src = Join-Path $PSScriptRoot 'VLCustomBuilder.cs'
    if (-not (Test-Path $src)) { Fail 'VLCustomBuilder.cs not found next to build-addon.ps1' }

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

    $bRefs = New-Object 'System.Collections.Generic.List[Microsoft.CodeAnalysis.MetadataReference]'
    if (Test-Path $fw) {
        Get-ChildItem -Path $fw -Filter '*.dll' -File | ForEach-Object {
            # Skip native (non-managed) DLLs - the v4.8 folder also holds a few
            # unmanaged shims (System.EnterpriseServices.*) Roslyn cannot use.
            try { [void][System.Reflection.AssemblyName]::GetAssemblyName($_.FullName) } catch { return }
            $bRefs.Add([Microsoft.CodeAnalysis.MetadataReference]::CreateFromFile($_.FullName))
        }
    }
    # netstandard facade: Roslyn 5.0 is a netstandard2.0 assembly, so its public
    # API (Func(Stream), Stream, Object) forwards through netstandard.
    $ns = Join-Path $fw 'Facades\netstandard.dll'
    if (-not (Test-Path $ns)) { $ns = 'C:\Windows\Microsoft.NET\Framework64\v4.0.30319\netstandard.dll' }
    if (Test-Path $ns) { $bRefs.Add([Microsoft.CodeAnalysis.MetadataReference]::CreateFromFile($ns)) }
    # Roslyn + its metadata deps from NT8's bin.
    foreach ($n in @('Microsoft.CodeAnalysis.dll','Microsoft.CodeAnalysis.CSharp.dll','System.Reflection.Metadata.dll','System.Collections.Immutable.dll','System.Runtime.CompilerServices.Unsafe.dll','System.Memory.dll','System.Buffers.dll')) {
        $p = Join-Path $NT8Bin $n
        if (Test-Path $p) { $bRefs.Add([Microsoft.CodeAnalysis.MetadataReference]::CreateFromFile($p)) }
    }

    $text = [System.IO.File]::ReadAllText($src)
    $bTree = [Microsoft.CodeAnalysis.CSharp.CSharpSyntaxTree]::ParseText($text, $null, $src, $null)
    $bTrees = New-Object 'System.Collections.Generic.List[Microsoft.CodeAnalysis.SyntaxTree]'
    $bTrees.Add($bTree)
    $bOpts = New-Object Microsoft.CodeAnalysis.CSharp.CSharpCompilationOptions([Microsoft.CodeAnalysis.OutputKind]::ConsoleApplication)
    $bComp = [Microsoft.CodeAnalysis.CSharp.CSharpCompilation]::Create('VLCustomBuilder', $bTrees, $bRefs, $bOpts)
    $bErrors = @($bComp.GetDiagnostics() | Where-Object { $_.Severity -eq [Microsoft.CodeAnalysis.DiagnosticSeverity]::Error })
    if ($bErrors.Count -gt 0) {
        foreach ($d in $bErrors) { Write-Output ("ERROR " + $d.Id + ": " + $d.GetMessage()) }
        Fail 'VLCustomBuilder.cs failed to compile'
    }

    $tmpDir = Join-Path $env:LOCALAPPDATA ('Temp\vl-custom-builder\' + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null
    $exe = Join-Path $tmpDir 'VLCustomBuilder.exe'
    $fs = [System.IO.File]::Open($exe, [System.IO.FileMode]::Create)
    try {
        $bResult = $bComp.Emit($fs)
    } finally {
        $fs.Close()
    }
    if (-not $bResult.Success) { Fail 'VLCustomBuilder emit failed' }
    return $exe
}

# -Custom delegates the WHOLE Custom.dll rebuild to the C# builder (FOLD 3). The
# builder prints the same markers the worker greps for (VL_CUSTOM_TYPES_OK /
# VL_CUSTOM_TYPES_DIFF / VL_CUSTOM_ERRORS / VL_CUSTOM_DLL / VL_CUSTOM_SHA256) and
# exits non-zero on any mismatch, so a builder compile/run failure falls back to
# F5 - never a silent swap.
if ($Custom) {
    if (-not $CustomDir) { Fail "-CustomDir is required with -Custom" }
    if (-not $Out) { Fail "-Out is required" }
    if (-not (Test-Path $Out)) { New-Item -ItemType Directory -Path $Out -Force | Out-Null }
    $csproj = Join-Path $CustomDir 'NinjaTrader.Custom.csproj'
    if (-not (Test-Path $csproj)) { Fail "NinjaTrader.Custom.csproj not found in " + $CustomDir }
    $oldDll = Join-Path $CustomDir 'NinjaTrader.Custom.dll'
    if (-not (Test-Path $oldDll)) { Fail "old Custom.dll not found at " + $oldDll }
    $exe = Build-VLCustomBuilder $NT8Bin
    # FOLD 5 F1: self-test the acceptance comparers BEFORE every real rebuild.
    # A broken comparer must refuse the rebuild (-> F5), never accept a bad DLL.
    & $exe --selftest --nt8Bin $NT8Bin
    if ($LASTEXITCODE -ne 0) { Fail 'VLCustomBuilder --selftest did not pass' }
    & $exe --csproj $csproj --customDir $CustomDir --nt8Bin $NT8Bin --outDir $Out --oldDll $oldDll
    exit $LASTEXITCODE
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
    # FOLD 5 F1: the readiness probe also proves the acceptance comparers, so a
    # broken comparer is caught by /api/updates/readiness, not just at install.
    $exe = Build-VLCustomBuilder $NT8Bin
    & $exe --selftest --nt8Bin $NT8Bin
    exit $LASTEXITCODE
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


