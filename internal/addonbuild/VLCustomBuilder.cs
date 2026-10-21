// VLCustomBuilder.cs - rebuilds NinjaTrader.Custom.dll FROM ITS OWN .csproj,
// including the embedded neutral Resource.resx, with NO dotnet SDK: it is
// compiled on the box by build-addon.ps1 using NT8's bundled Roslyn, then run
// as a small .NET Framework 4.8 console program. (release #35 FOLD 3, owner
// order "one click, no F5".)
//
// WHY a separate program: Roslyn's Microsoft.CodeAnalysis.ResourceDescription
// has exactly one public factory in Roslyn 5.0 - the (string, Func<Stream>,
// bool) constructor - and every use of it FROM POWERSHELL throws
// StackOverflowException (PowerShell 5.1 cannot marshal the Func<Stream>
// delegate). Compiled C# does not have that problem, so the resource-embedding
// Emit lives here.
//
// THE ONE RULE: this program only WRITES into the output dir (a worker-made
// temp dir) and only READS the old Custom.dll and the Custom source tree. It
// never deletes, moves, or overwrites any install file. On any mismatch it
// exits non-zero so the caller falls back to F5 - never a silent swap.

using System;
using System.Collections;
using System.Collections.Generic;
using System.Globalization;
using System.IO;
using System.Linq;
using System.Reflection;
using System.Reflection.Metadata;
using System.Reflection.PortableExecutable;
using System.Resources;
using System.Security.Cryptography;
using System.Text;
using System.Xml;
using Microsoft.CodeAnalysis;
using Microsoft.CodeAnalysis.CSharp;

internal static class VLCustomBuilder
{
    private sealed class Opts
    {
        public string Csproj;
        public string CustomDir;
        public string Nt8Bin;
        public string OutDir;
        public string OldDll;
        public bool SelfTest;
    }

    public static int Main(string[] args)
    {
        try
        {
            var o = ParseArgs(args);
            // Resolve Roslyn + its deps from NT8's bin dir at RUN time (the exe
            // sits in a temp dir, not next to those DLLs). netstandard.dll is a
            // GAC facade on .NET Framework 4.7.2+, but point at it explicitly as
            // a belt-and-braces fallback.
            AppDomain.CurrentDomain.AssemblyResolve += (s, e) =>
            {
                var n = new AssemblyName(e.Name).Name;
                var p = Path.Combine(o.Nt8Bin, n + ".dll");
                if (File.Exists(p)) return Assembly.LoadFrom(p);
                if (string.Equals(n, "netstandard", StringComparison.OrdinalIgnoreCase))
                {
                    var g = Path.Combine(
                        Environment.GetFolderPath(Environment.SpecialFolder.Windows),
                        @"Microsoft.NET\Framework64\v4.0.30319\netstandard.dll");
                    if (File.Exists(g)) return Assembly.LoadFrom(g);
                }
                return null;
            };
            if (o.SelfTest) return SelfTest(o.Nt8Bin);
            return Run(o);
        }
        catch (Exception ex)
        {
            Console.WriteLine("ERROR " + ex.Message);
            return 1;
        }
    }

    private static Opts ParseArgs(string[] args)
    {
        var o = new Opts();
        for (int i = 0; i < args.Length; i++)
        {
            switch (args[i])
            {
                case "--csproj": o.Csproj = args[++i]; break;
                case "--customDir": o.CustomDir = args[++i]; break;
                case "--nt8Bin": o.Nt8Bin = args[++i]; break;
                case "--outDir": o.OutDir = args[++i]; break;
                case "--oldDll": o.OldDll = args[++i]; break;
                case "--selftest": o.SelfTest = true; break;
                default: throw new ArgumentException("unknown arg: " + args[i]);
            }
        }
        if (o.SelfTest)
        {
            if (string.IsNullOrEmpty(o.Nt8Bin))
                throw new ArgumentException("--selftest requires --nt8Bin");
            return o;
        }
        if (string.IsNullOrEmpty(o.Csproj) || string.IsNullOrEmpty(o.CustomDir) ||
            string.IsNullOrEmpty(o.Nt8Bin) || string.IsNullOrEmpty(o.OutDir))
            throw new ArgumentException("missing required args");
        if (string.IsNullOrEmpty(o.OldDll))
            o.OldDll = Path.Combine(o.CustomDir, "NinjaTrader.Custom.dll");
        return o;
    }

    private static int Run(Opts o)
    {
        if (!File.Exists(o.Csproj)) throw new Exception("csproj not found: " + o.Csproj);
        if (!File.Exists(o.OldDll)) throw new Exception("old Custom.dll not found: " + o.OldDll);

        var proj = new ProjectInfo(o.Csproj);
        Directory.CreateDirectory(o.OutDir);

        // -- references -----------------------------------------------------
        var refPaths = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);
        Action<string> addRef = p =>
        {
            if (string.IsNullOrEmpty(p) || !File.Exists(p)) return;
            string full;
            try { full = Path.GetFullPath(p); }
            catch { return; }
            try { AssemblyName.GetAssemblyName(full); } // skip native/non-managed
            catch { return; }
            refPaths[Path.GetFileName(full)] = full;
        };

        var fw = FrameworkRefDir();
        if (Directory.Exists(fw))
            foreach (var f in Directory.GetFiles(fw, "*.dll")) addRef(f);

        // netstandard facade: Roslyn 5.0 is a netstandard2.0 assembly, so its
        // public API (Func<>, Stream, ...) forward through netstandard.
        var ns = Path.Combine(fw, "Facades", "netstandard.dll");
        if (!File.Exists(ns))
            ns = Path.Combine(
                Environment.GetFolderPath(Environment.SpecialFolder.Windows),
                @"Microsoft.NET\Framework64\v4.0.30319\netstandard.dll");
        addRef(ns);

        foreach (var hp in proj.HintPaths) addRef(hp);
        addRef(Path.Combine(o.CustomDir, "NinjaTrader.Vendor.dll"));
        addRef(Path.Combine(o.Nt8Bin, "Newtonsoft.Json.dll"));

        if (refPaths.Count == 0) throw new Exception("no reference assemblies resolved");

        var refs = new List<MetadataReference>();
        foreach (var kv in refPaths.OrderBy(k => k.Key, StringComparer.OrdinalIgnoreCase))
            refs.Add(MetadataReference.CreateFromFile(kv.Value));

        // -- compile set: the csproj's <Compile Include> items EXACTLY --------
        // (URL-decoded %40 -> @) MINUS the release's AddOns/VL*.cs (FOLD 1).
        // No glob: a stray .cs not listed in the csproj must not sneak in.
        var parseOpts = CSharpParseOptions.Default.WithPreprocessorSymbols(proj.DefineSymbols);
        var trees = new List<SyntaxTree>();
        foreach (var rel in proj.CompileItems)
        {
            var dir = Path.GetDirectoryName(rel);
            var file = Path.GetFileName(rel);
            if (string.Equals(dir, "AddOns", StringComparison.OrdinalIgnoreCase) &&
                file.StartsWith("VL", StringComparison.OrdinalIgnoreCase)) continue;
            var full = Path.Combine(o.CustomDir, rel);
            if (!File.Exists(full)) continue;
            var text = File.ReadAllText(full);
            trees.Add(CSharpSyntaxTree.ParseText(text, parseOpts, full));
        }
        if (trees.Count == 0) throw new Exception("the csproj yielded no .cs files");

        var options = new CSharpCompilationOptions(OutputKind.DynamicallyLinkedLibrary);
        var compilation = CSharpCompilation.Create(proj.AssemblyName, trees, refs, options);

        // (a) 0 errors - check BEFORE writing anything.
        var errors = compilation.GetDiagnostics()
            .Where(d => d.Severity == DiagnosticSeverity.Error)
            .ToList();
        if (errors.Count > 0)
        {
            Console.WriteLine("VL_CUSTOM_ERRORS=" + errors.Count);
            foreach (var d in errors)
                Console.WriteLine("ERROR " + d.Id + ": " + d.GetMessage());
            return 1;
        }

        // -- manifest resources, from the OLD DLL's manifest names -----------
        var resources = BuildResources(o, proj.RootNamespace);

        var dllPath = Path.Combine(o.OutDir, proj.AssemblyName + ".dll");
        var xmlPath = Path.Combine(o.OutDir, proj.AssemblyName + ".XML");

        using (var pe = File.Create(dllPath))
        using (var xml = File.Create(xmlPath))
        {
            var result = compilation.Emit(pe, null, xml, null, resources, null);
            if (!result.Success)
            {
                var errs = result.Diagnostics
                    .Where(d => d.Severity == DiagnosticSeverity.Error)
                    .ToList();
                Console.WriteLine("VL_CUSTOM_ERRORS=" + errs.Count);
                foreach (var d in errs)
                    Console.WriteLine("ERROR " + d.Id + ": " + d.GetMessage());
                return 1;
            }
        }

        // -- acceptance: (b) resources, (c) refs, (d) types, (e) name/version -
        if (!Accept(o.OldDll, dllPath, proj)) return 1;

        Console.WriteLine("VL_CUSTOM_TYPES_OK");
        Console.WriteLine("VL_CUSTOM_DLL=" + dllPath);
        Console.WriteLine("VL_CUSTOM_SHA256=" + Sha256(dllPath));
        return 0;
    }

    private static string FrameworkRefDir()
    {
        var root = @"C:\Program Files (x86)\Reference Assemblies\Microsoft\Framework\.NETFramework";
        if (Directory.Exists(Path.Combine(root, "v4.8"))) return Path.Combine(root, "v4.8");
        var best = new DirectoryInfo(root).Exists
            ? new DirectoryInfo(root).GetDirectories("v4.*").OrderByDescending(d => d.Name).FirstOrDefault()
            : null;
        if (best != null) return best.FullName;
        var fx64 = Path.Combine(
            Environment.GetFolderPath(Environment.SpecialFolder.Windows),
            @"Microsoft.NET\Framework64\v4.0.30319");
        return Directory.Exists(fx64) ? fx64 : root;
    }

    private static List<ResourceDescription> BuildResources(Opts o, string rootNs)
    {
        var list = new List<ResourceDescription>();
        foreach (var name in ReadManifestResourceNames(o.OldDll))
        {
            var resx = MapNeutralResx(name, rootNs, o.CustomDir);
            if (resx == null)
                throw new Exception("cannot reproduce manifest resource '" + name +
                    "' from a neutral .resx (satellite resources are not embedded)");
            list.Add(Embed(name, resx));
        }
        return list;
    }

    // Maps "NinjaTrader.Custom.Resource.resources" -> <customDir>\Resource.resx.
    // A culture-suffixed name (a satellite) returns null: those live in
    // satellite DLLs the rebuild leaves alone, never in the main DLL.
    private static string MapNeutralResx(string manifestName, string rootNs, string customDir)
    {
        if (string.IsNullOrEmpty(manifestName) || !manifestName.EndsWith(".resources", StringComparison.Ordinal))
            return null;
        if (!manifestName.StartsWith(rootNs + ".", StringComparison.Ordinal))
            return null;
        var rel = manifestName.Substring(rootNs.Length + 1,
            manifestName.Length - rootNs.Length - 1 - ".resources".Length);
        var direct = Path.Combine(customDir, rel.Replace('.', '\\') + ".resx");
        if (File.Exists(direct)) return direct;
        return null;
    }

    private static ResourceDescription Embed(string name, string resx)
    {
        byte[] bytes;
        using (var ms = new MemoryStream())
        {
            using (var w = new ResourceWriter(ms))
            {
                using (var r = new ResXResourceReader(resx))
                {
                    foreach (DictionaryEntry e in r)
                    {
                        var key = e.Key as string;
                        if (key == null) throw new Exception("non-string key in " + resx);
                        w.AddResource(key, (e.Value as string) ?? e.Value.ToString());
                    }
                }
                w.Generate();
            }
            bytes = ms.ToArray();
        }
        return new ResourceDescription(name, () => new MemoryStream(bytes), true);
    }

    // -- acceptance ---------------------------------------------------------
    // Each Check* returns true on pass and false on a mismatch, carrying the
    // diff detail in `detail` WITHOUT printing - so the -Custom run can print
    // the markers once, and --selftest can drive them silently.

    private static bool Accept(string oldDll, string newDll, ProjectInfo proj)
    {
        if (!CheckResources(oldDll, newDll, out var d)) { Console.WriteLine("VL_CUSTOM_TYPES_DIFF"); Console.WriteLine(d); return false; }
        if (!CheckReferences(oldDll, newDll, out d)) { Console.WriteLine("VL_CUSTOM_TYPES_DIFF"); Console.WriteLine(d); return false; }
        if (!CheckTypes(oldDll, newDll, out d)) { Console.WriteLine("VL_CUSTOM_TYPES_DIFF"); Console.WriteLine(d); return false; }
        if (!CheckIdentity(oldDll, newDll, out d)) { Console.WriteLine("VL_CUSTOM_TYPES_DIFF"); Console.WriteLine(d); return false; }
        return true;
    }

    // (b) manifest resource names == old
    private static bool CheckResources(string oldDll, string newDll, out string detail)
    {
        var oldRes = ReadManifestResourceNames(oldDll).OrderBy(x => x, StringComparer.Ordinal).ToArray();
        var newRes = ReadManifestResourceNames(newDll).OrderBy(x => x, StringComparer.Ordinal).ToArray();
        if (!oldRes.SequenceEqual(newRes, StringComparer.Ordinal))
        {
            detail = "RESOURCE_DIFF old=[" + string.Join(",", oldRes) + "] new=[" + string.Join(",", newRes) + "]";
            return false;
        }
        detail = null;
        return true;
    }

    // (c) referenced-assembly names == old
    private static bool CheckReferences(string oldDll, string newDll, out string detail)
    {
        var oldRefs = ReadReferencedAssemblyNames(oldDll).OrderBy(x => x, StringComparer.Ordinal).ToArray();
        var newRefs = ReadReferencedAssemblyNames(newDll).OrderBy(x => x, StringComparer.Ordinal).ToArray();
        if (!oldRefs.SequenceEqual(newRefs, StringComparer.Ordinal))
        {
            detail = "REFERENCE_DIFF old=[" + string.Join(",", oldRefs) + "] new=[" + string.Join(",", newRefs) + "]";
            return false;
        }
        detail = null;
        return true;
    }

    // (d) full public type signatures == old minus the VL AddOn types
    private static bool CheckTypes(string oldDll, string newDll, out string detail)
    {
        var vlPrefix = "NinjaTrader.NinjaScript.AddOns.VL";
        var oldTypes = ReadPublicTypeSignatures(oldDll)
            .Where(s => !s.StartsWith(vlPrefix, StringComparison.Ordinal))
            .OrderBy(s => s, StringComparer.Ordinal)
            .ToArray();
        var newTypes = ReadPublicTypeSignatures(newDll)
            .OrderBy(s => s, StringComparer.Ordinal)
            .ToArray();
        if (!oldTypes.SequenceEqual(newTypes, StringComparer.Ordinal))
        {
            var onlyOld = oldTypes.Except(newTypes, StringComparer.Ordinal).Take(20).ToArray();
            var onlyNew = newTypes.Except(oldTypes, StringComparer.Ordinal).Take(20).ToArray();
            var sb = new StringBuilder();
            foreach (var t in onlyOld) sb.AppendLine("TYPE => " + t);
            foreach (var t in onlyNew) sb.AppendLine("TYPE <= " + t);
            sb.Append("TYPE_DIFF old_only=" + oldTypes.Except(newTypes, StringComparer.Ordinal).Count() +
                      " new_only=" + newTypes.Except(oldTypes, StringComparer.Ordinal).Count());
            detail = sb.ToString();
            return false;
        }
        detail = null;
        return true;
    }

    // (e) AssemblyName/Version == old (FOLD 4: also culture neutral and
    // unsigned - the full main identity).
    private static bool CheckIdentity(string oldDll, string newDll, out string detail)
    {
        var oldName = AssemblyName.GetAssemblyName(oldDll);
        var newName = AssemblyName.GetAssemblyName(newDll);
        if (!string.Equals(oldName.Name, newName.Name, StringComparison.Ordinal) ||
            oldName.Version != newName.Version ||
            !string.Equals(oldName.CultureName ?? "", newName.CultureName ?? "", StringComparison.Ordinal) ||
            !PublicKeyTokenEqual(oldName, newName))
        {
            detail = "ASSEMBLY_DIFF old=" + oldName.Name + "/" + oldName.Version +
                     "/" + (oldName.CultureName ?? "") + "/" + PublicKeyTokenHex(oldName) +
                     " new=" + newName.Name + "/" + newName.Version +
                     "/" + (newName.CultureName ?? "") + "/" + PublicKeyTokenHex(newName);
            return false;
        }
        detail = null;
        return true;
    }

    private static bool PublicKeyTokenEqual(AssemblyName a, AssemblyName b)
    {
        var ta = a.GetPublicKeyToken();
        var tb = b.GetPublicKeyToken();
        if (ta == null || tb == null) return ta == tb;
        if (ta.Length != tb.Length) return false;
        for (var i = 0; i < ta.Length; i++) if (ta[i] != tb[i]) return false;
        return true;
    }

    private static string PublicKeyTokenHex(AssemblyName a)
    {
        var t = a.GetPublicKeyToken();
        if (t == null || t.Length == 0) return "unsigned";
        var sb = new StringBuilder(t.Length * 2);
        foreach (var b in t) sb.Append(b.ToString("x2"));
        return sb.ToString();
    }

    // -- self-test (FOLD 5 F1) ----------------------------------------------
    // Compiles tiny in-memory fixture assemblies with Roslyn and drives each
    // comparer with deliberately-different pairs. Prints VL_SELFTEST_OK only
    // when every rejecting pair is rejected and one identical pair is accepted.

    private static int SelfTest(string nt8Bin)
    {
        var failures = new List<string>();
        var dir = Path.Combine(Path.GetTempPath(), "vl-builder-selftest-" + Guid.NewGuid().ToString("N"));
        Directory.CreateDirectory(dir);

        var refsBase = FixtureRefs(false);
        var refsXml = FixtureRefs(true);

        // (a) a compile error must be detected
        var badComp = CompileInMemory("Bad", "public class Bad { this is not valid C# }", refsBase);
        if (!HasCompileErrors(badComp))
            failures.Add("(a) a compile error was not detected");

        // (b) a missing manifest resource must be rejected
        var bOld = BuildFixture(dir, "b-old.dll", FixtureSource("1.0.0.0"), refsBase,
            new List<ResourceDescription> { FixtureResource("Fixture.resources") }, failures, "(b)");
        var bNew = BuildFixture(dir, "b-new.dll", FixtureSource("1.0.0.0"), refsBase, null, failures, "(b)");
        if (bOld != null && bNew != null && CheckResources(bOld, bNew, out _))
            failures.Add("(b) a missing manifest resource was accepted");

        // (c) a different reference must be rejected
        var cOld = BuildFixture(dir, "c-old.dll", FixtureSource("1.0.0.0"), refsBase, null, failures, "(c)");
        var cNew = BuildFixture(dir, "c-new.dll", XmlSource("1.0.0.0"), refsXml, null, failures, "(c)");
        if (cOld != null && cNew != null && CheckReferences(cOld, cNew, out _))
            failures.Add("(c) a different reference was accepted");

        // (d) an extra public type must be rejected
        var dOld = BuildFixture(dir, "d-old.dll", TwoClassSource("1.0.0.0"), refsBase, null, failures, "(d)");
        var dNew = BuildFixture(dir, "d-new.dll", FixtureSource("1.0.0.0"), refsBase, null, failures, "(d)");
        if (dOld != null && dNew != null && CheckTypes(dOld, dNew, out _))
            failures.Add("(d) an extra public type was accepted");

        // (e) a different version must be rejected
        var eOld = BuildFixture(dir, "e-old.dll", FixtureSource("1.0.0.0"), refsBase, null, failures, "(e)");
        var eNew = BuildFixture(dir, "e-new.dll", FixtureSource("2.0.0.0"), refsBase, null, failures, "(e)");
        if (eOld != null && eNew != null && CheckIdentity(eOld, eNew, out _))
            failures.Add("(e) a different version was accepted");

        // one identical pair must be accepted on every comparer
        var okOld = BuildFixture(dir, "ok-old.dll", FixtureSource("1.0.0.0"), refsBase, null, failures, "(ok)");
        var okNew = BuildFixture(dir, "ok-new.dll", FixtureSource("1.0.0.0"), refsBase, null, failures, "(ok)");
        if (okOld != null && okNew != null)
        {
            if (!CheckResources(okOld, okNew, out _)) failures.Add("(ok) identical pair rejected on resources");
            if (!CheckReferences(okOld, okNew, out _)) failures.Add("(ok) identical pair rejected on references");
            if (!CheckTypes(okOld, okNew, out _)) failures.Add("(ok) identical pair rejected on types");
            if (!CheckIdentity(okOld, okNew, out _)) failures.Add("(ok) identical pair rejected on identity");
        }

        if (failures.Count > 0)
        {
            Console.WriteLine("VL_SELFTEST_FAIL");
            foreach (var f in failures) Console.WriteLine("SELFTEST " + f);
            return 1;
        }
        Console.WriteLine("VL_SELFTEST_OK");
        return 0;
    }

    private static bool HasCompileErrors(Compilation c) =>
        c.GetDiagnostics().Any(d => d.Severity == DiagnosticSeverity.Error);

    private static CSharpCompilation CompileInMemory(string asmName, string source, List<MetadataReference> refs)
    {
        var tree = CSharpSyntaxTree.ParseText(source);
        return CSharpCompilation.Create(asmName, new[] { tree }, refs,
            new CSharpCompilationOptions(OutputKind.DynamicallyLinkedLibrary));
    }

    private static byte[] CompileAssembly(string asmName, string source, List<MetadataReference> refs,
        List<ResourceDescription> resources, out List<Diagnostic> errors)
    {
        var comp = CompileInMemory(asmName, source, refs);
        errors = comp.GetDiagnostics().Where(d => d.Severity == DiagnosticSeverity.Error).ToList();
        if (errors.Count > 0) return null;
        using (var ms = new MemoryStream())
        {
            var result = comp.Emit(ms, null, null, null, resources, null);
            if (!result.Success)
            {
                errors = result.Diagnostics.Where(d => d.Severity == DiagnosticSeverity.Error).ToList();
                return null;
            }
            return ms.ToArray();
        }
    }

    private static string BuildFixture(string dir, string name, string source, List<MetadataReference> refs,
        List<ResourceDescription> resources, List<string> failures, string label)
    {
        // The assembly NAME is fixed ("Fixture") so a pair compares as the same
        // assembly; only the on-disk filename differs.
        var bytes = CompileAssembly("Fixture", source, refs, resources, out var errors);
        if (bytes == null)
        {
            failures.Add(label + ": fixture failed to compile: " + string.Join("; ", errors.Select(e => e.Id)));
            return null;
        }
        var p = Path.Combine(dir, name);
        File.WriteAllBytes(p, bytes);
        return p;
    }

    private static List<MetadataReference> FixtureRefs(bool withXml)
    {
        var fw = FrameworkRefDir();
        var refs = new List<MetadataReference>();
        foreach (var n in new[] { "mscorlib.dll", "System.dll", "System.Core.dll" })
            refs.Add(MetadataReference.CreateFromFile(Path.Combine(fw, n)));
        if (withXml)
            refs.Add(MetadataReference.CreateFromFile(Path.Combine(fw, "System.Xml.dll")));
        return refs;
    }

    private static ResourceDescription FixtureResource(string name)
    {
        byte[] bytes;
        using (var ms = new MemoryStream())
        {
            using (var w = new ResourceWriter(ms)) { w.AddResource("k", "v"); w.Generate(); }
            bytes = ms.ToArray();
        }
        return new ResourceDescription(name, () => new MemoryStream(bytes), true);
    }

    private static string FixtureSource(string version) =>
        "using System.Reflection;\n[assembly: AssemblyVersion(\"" + version + "\")]\n" +
        "public class Fixture { public static int One() { return 1; } }\n";

    private static string XmlSource(string version) =>
        "using System.Reflection;\n[assembly: AssemblyVersion(\"" + version + "\")]\n" +
        "public class Fixture { public System.Xml.XmlDocument Doc; }\n";

    private static string TwoClassSource(string version) =>
        "using System.Reflection;\n[assembly: AssemblyVersion(\"" + version + "\")]\n" +
        "public class Fixture { }\npublic class Extra { }\n";

    // -- metadata readers ---------------------------------------------------

    private static List<string> ReadManifestResourceNames(string dll)
    {
        using (var fs = File.OpenRead(dll))
        using (var pe = new PEReader(fs))
        {
            var md = pe.GetMetadataReader();
            var names = new List<string>();
            foreach (var h in md.ManifestResources)
                names.Add(md.GetString(md.GetManifestResource(h).Name));
            return names;
        }
    }

    private static List<string> ReadReferencedAssemblyNames(string dll)
    {
        using (var fs = File.OpenRead(dll))
        using (var pe = new PEReader(fs))
        {
            var md = pe.GetMetadataReader();
            var names = new List<string>();
            foreach (var h in md.AssemblyReferences)
                names.Add(md.GetString(md.GetAssemblyReference(h).Name));
            return names;
        }
    }

    private static List<string> ReadPublicTypeSignatures(string dll)
    {
        using (var fs = File.OpenRead(dll))
        using (var pe = new PEReader(fs))
        {
            var md = pe.GetMetadataReader();
            var provider = new TypeNameProvider(md);
            var sigs = new List<string>();
            foreach (var h in md.TypeDefinitions)
            {
                var t = md.GetTypeDefinition(h);
                var vis = (int)t.Attributes & 0x7;
                if (vis != 0x1 && vis != 0x2) continue; // only Public / NestedPublic

                var name = TypeName(md, h);
                var baseName = BaseTypeName(md, t.BaseType, provider);
                var attrs = new List<string>();
                foreach (var ah in t.GetCustomAttributes())
                    attrs.Add(AttributeTypeName(md, md.GetCustomAttribute(ah).Constructor));
                attrs.Sort(StringComparer.Ordinal);
                sigs.Add(name + " : " + baseName + " [" + string.Join(";", attrs) + "]");
            }
            return sigs;
        }
    }

    private static string TypeName(MetadataReader md, TypeDefinitionHandle h)
    {
        var t = md.GetTypeDefinition(h);
        var ns = md.GetString(t.Namespace);
        var name = md.GetString(t.Name);
        var decl = t.GetDeclaringType();
        if (!decl.IsNil)
            return TypeName(md, decl) + "+" + name;
        return string.IsNullOrEmpty(ns) ? name : ns + "." + name;
    }

    private static string BaseTypeName(MetadataReader md, EntityHandle baseType, TypeNameProvider provider)
    {
        if (baseType.IsNil) return "<none>";
        switch (baseType.Kind)
        {
            case HandleKind.TypeDefinition:
                return TypeName(md, (TypeDefinitionHandle)baseType);
            case HandleKind.TypeReference:
                return TypeRefName(md, (TypeReferenceHandle)baseType);
            case HandleKind.TypeSpecification:
                return md.GetTypeSpecification((TypeSpecificationHandle)baseType)
                    .DecodeSignature(provider, null);
            default:
                return "<" + baseType.Kind + ">";
        }
    }

    private static string TypeRefName(MetadataReader md, TypeReferenceHandle h)
    {
        var t = md.GetTypeReference(h);
        var ns = md.GetString(t.Namespace);
        var name = md.GetString(t.Name);
        var full = string.IsNullOrEmpty(ns) ? name : ns + "." + name;
        var scope = t.ResolutionScope;
        if (!scope.IsNil && scope.Kind == HandleKind.AssemblyReference)
        {
            var an = md.GetAssemblyReference((AssemblyReferenceHandle)scope);
            full = "[" + md.GetString(an.Name) + "]" + full;
        }
        return full;
    }

    private static string AttributeTypeName(MetadataReader md, EntityHandle ctor)
    {
        switch (ctor.Kind)
        {
            case HandleKind.MethodDefinition:
                var m = md.GetMethodDefinition((MethodDefinitionHandle)ctor);
                var dt = m.GetDeclaringType();
                return dt.IsNil ? "<attr>" : TypeName(md, dt);
            case HandleKind.MemberReference:
                var mr = md.GetMemberReference((MemberReferenceHandle)ctor);
                if (mr.Parent.Kind == HandleKind.TypeReference)
                    return TypeRefName(md, (TypeReferenceHandle)mr.Parent);
                if (mr.Parent.Kind == HandleKind.TypeDefinition)
                    return TypeName(md, (TypeDefinitionHandle)mr.Parent);
                return "<attr:" + mr.Parent.Kind + ">";
            default:
                return "<attr:" + ctor.Kind + ">";
        }
    }

    private static string Sha256(string path)
    {
        using (var s = File.OpenRead(path))
        using (var sha = SHA256.Create())
        {
            var b = sha.ComputeHash(s);
            var sb = new StringBuilder(b.Length * 2);
            foreach (var x in b) sb.Append(x.ToString("x2"));
            return sb.ToString();
        }
    }

    // -- csproj model -------------------------------------------------------

    private sealed class ProjectInfo
    {
        public string AssemblyName = "NinjaTrader.Custom";
        public string RootNamespace;
        public List<string> CompileItems = new List<string>();
        public List<string> HintPaths = new List<string>();
        public List<string> DefineSymbols = new List<string>();

        public ProjectInfo(string csprojPath)
        {
            var xml = new XmlDocument();
            xml.Load(csprojPath);

            var nsm = new XmlNamespaceManager(xml.NameTable);
            nsm.AddNamespace("ms", "http://schemas.microsoft.com/developer/msbuild/2003");

            var asm = Select(xml, nsm, "//ms:AssemblyName");
            if (asm != null && !string.IsNullOrWhiteSpace(asm.InnerText))
                AssemblyName = asm.InnerText.Trim();
            RootNamespace = AssemblyName;
            var rns = Select(xml, nsm, "//ms:RootNamespace");
            if (rns != null && !string.IsNullOrWhiteSpace(rns.InnerText))
                RootNamespace = rns.InnerText.Trim();

            foreach (XmlNode n in xml.SelectNodes("//*[local-name()='Compile']"))
            {
                var inc = n.Attributes != null ? n.Attributes["Include"] : null;
                if (inc != null && !string.IsNullOrEmpty(inc.Value))
                    CompileItems.Add(inc.Value.Replace("%40", "@"));
            }
            foreach (XmlNode n in xml.SelectNodes("//*[local-name()='Reference']"))
            {
                var hp = n.SelectSingleNode("*[local-name()='HintPath']");
                if (hp != null && !string.IsNullOrWhiteSpace(hp.InnerText))
                    HintPaths.Add(hp.InnerText.Trim());
            }

            // DefineConstants = "$(DefineConstants);$(CiConstants)". The NT8 F5
            // build is Release|x64, where the SDK's $(DefineConstants) is TRACE;
            // $(CiConstants) is not defined anywhere on the box (only referenced
            // here), so it resolves empty. Any other $(...) token is unknowable.
            var defs = Select(xml, nsm, "//ms:DefineConstants");
            if (defs != null)
            {
                foreach (var tok in defs.InnerText.Split(';'))
                {
                    var t = tok.Trim();
                    if (t.Length == 0) continue;
                    if (t == "$(DefineConstants)") { DefineSymbols.Add("TRACE"); continue; }
                    if (t == "$(CiConstants)") continue;
                    if (t.Contains("$("))
                        throw new Exception("unknowable DefineConstants token: " + t);
                    DefineSymbols.Add(t);
                }
            }

            // FOLD 4: the rebuild must NOT define PRODUCTION. AssemblyInfo.cs
            // stamps 8.1.8.3 under PRODUCTION and 8.1.9.0 otherwise; today's
            // main is 8.1.9.0. Refuse rather than silently stamp the wrong
            // identity.
            if (DefineSymbols.Contains("PRODUCTION"))
                throw new Exception("DefineConstants resolves PRODUCTION; refusing (would stamp 8.1.8.3, today's main is 8.1.9.0)");
        }

        private static XmlNode Select(XmlDocument xml, XmlNamespaceManager nsm, string xpath)
        {
            var node = xml.SelectSingleNode(xpath, nsm);
            if (node == null)
            {
                // fall back to a namespace-agnostic search for hand-edited files
                var local = xpath.Substring(xpath.IndexOf(':') + 1);
                node = xml.SelectSingleNode("//*[local-name()='" + local + "']");
            }
            return node;
        }
    }

    // Decodes TypeSpecification signatures (generic instantiations, arrays,
    // pointers) to a deterministic string, so old-vs-new base types compare.
    private sealed class TypeNameProvider : ISignatureTypeProvider<string, object>
    {
        private readonly MetadataReader _md;
        public TypeNameProvider(MetadataReader md) { _md = md; }

        public string GetArrayType(string elementType, ArrayShape shape)
            => elementType + "[" + new string(',', shape.Rank - 1) + "]";
        public string GetByReferenceType(string elementType) => elementType + "&";
        public string GetPointerType(string elementType) => elementType + "*";
        public string GetSZArrayType(string elementType) => elementType + "[]";
        public string GetPinnedType(string elementType) => elementType + " pinned";
        public string GetModifiedType(string modifier, string unmodifiedType, bool isRequired) => unmodifiedType;
        public string GetGenericInstantiation(string genericType, System.Collections.Immutable.ImmutableArray<string> typeArguments)
            => genericType + "<" + string.Join(",", typeArguments) + ">";
        public string GetGenericMethodParameter(object genericContext, int index) => "!!" + index;
        public string GetGenericTypeParameter(object genericContext, int index) => "!" + index;
        public string GetFunctionPointerType(MethodSignature<string> signature) => "fnptr";
        public string GetTypeFromDefinition(MetadataReader reader, TypeDefinitionHandle handle, byte rawTypeKind) => TypeName(reader, handle);
        public string GetTypeFromReference(MetadataReader reader, TypeReferenceHandle handle, byte rawTypeKind) => TypeRefName(reader, handle);
        public string GetTypeFromSpecification(MetadataReader reader, object genericContext, TypeSpecificationHandle handle, byte rawTypeKind)
            => reader.GetTypeSpecification(handle).DecodeSignature(this, genericContext);
        public string GetPrimitiveType(PrimitiveTypeCode typeCode)
        {
            switch (typeCode)
            {
                case PrimitiveTypeCode.Void: return "void";
                case PrimitiveTypeCode.Boolean: return "bool";
                case PrimitiveTypeCode.Char: return "char";
                case PrimitiveTypeCode.SByte: return "sbyte";
                case PrimitiveTypeCode.Byte: return "byte";
                case PrimitiveTypeCode.Int16: return "short";
                case PrimitiveTypeCode.UInt16: return "ushort";
                case PrimitiveTypeCode.Int32: return "int";
                case PrimitiveTypeCode.UInt32: return "uint";
                case PrimitiveTypeCode.Int64: return "long";
                case PrimitiveTypeCode.UInt64: return "ulong";
                case PrimitiveTypeCode.Single: return "float";
                case PrimitiveTypeCode.Double: return "double";
                case PrimitiveTypeCode.String: return "string";
                case PrimitiveTypeCode.IntPtr: return "nint";
                case PrimitiveTypeCode.UIntPtr: return "nuint";
                case PrimitiveTypeCode.Object: return "object";
                case PrimitiveTypeCode.TypedReference: return "typedref";
                default: return "prim:" + typeCode;
            }
        }
    }
}
