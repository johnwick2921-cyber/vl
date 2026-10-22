using System;
using System.IO;
using System.Reflection;
using System.Runtime.CompilerServices;

class LoadHarness {
    static string[] probeDirs = new string[0];

    static Assembly Resolve(object s, ResolveEventArgs e) {
        var name = new AssemblyName(e.Name).Name;
        foreach (var d in probeDirs) {
            var p = Path.Combine(d, name + ".dll");
            if (File.Exists(p)) return Assembly.LoadFrom(p);
        }
        return null;
    }

    static int Main(string[] args) {
        if (args.Length < 1) { Console.WriteLine("usage: LoadHarness <dll> [probeDirs...]"); return 2; }
        string dll = args[0];
        probeDirs = new string[args.Length - 1];
        Array.Copy(args, 1, probeDirs, 0, probeDirs.Length);
        AppDomain.CurrentDomain.AssemblyResolve += Resolve;
        int fail = 0, corelib = 0, types = 0, methods = 0;
        try {
            var asm = Assembly.LoadFrom(dll);
            foreach (var r in asm.GetReferencedAssemblies())
                if (r.Name.IndexOf("System.Private.CoreLib", StringComparison.OrdinalIgnoreCase) >= 0) corelib++;
            foreach (var t in asm.GetTypes()) {
                types++;
                foreach (var m in t.GetMethods(BindingFlags.Public|BindingFlags.NonPublic|BindingFlags.Instance|BindingFlags.Static|BindingFlags.DeclaredOnly)) {
                    methods++;
                    try { RuntimeHelpers.PrepareMethod(m.MethodHandle); }
                    catch (Exception e) { fail++; Console.WriteLine("FAIL " + t.FullName + "::" + m.Name + ": " + e.GetType().Name + " " + e.Message); }
                }
                foreach (var c in t.GetConstructors(BindingFlags.Public|BindingFlags.NonPublic|BindingFlags.Instance|BindingFlags.Static)) {
                    methods++;
                    try { RuntimeHelpers.PrepareMethod(c.MethodHandle); }
                    catch (Exception e) { fail++; Console.WriteLine("FAIL ctor " + t.FullName + ": " + e.GetType().Name + " " + e.Message); }
                }
            }
        } catch (Exception e) {
            Console.WriteLine("LOAD_FAIL: " + e.GetType().Name + ": " + e.Message);
            return 1;
        }
        Console.WriteLine("RESULT fail=" + fail + " corelib=" + corelib + " types=" + types + " methods=" + methods);
        return (fail == 0 && corelib == 0) ? 0 : 1;
    }
}
