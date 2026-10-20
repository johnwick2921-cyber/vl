# DS-104 — offline compile evidence (executed) vs runtime block (executed)

Branch: `fix/ds104-connstatus-handler-repro` @ `ed651fb79`. All commands run
read-only via WSL interop; nothing installed, NT8 not launched.

## 1. What was ACTUALLY EXECUTED

### 1a. Compiler availability (executed)

```
$ "/mnt/c/Windows/Microsoft.NET/Framework64/v4.0.30319/csc.exe" /?
Microsoft (R) Visual C# Compiler version 4.8.9221.0
for C# 5
...
$ "/mnt/c/Program Files/dotnet/dotnet.exe" --version
9.0.307
```

`.NET Framework 4.8 targeting pack` present at:
`C:\Program Files (x86)\Reference Assemblies\Microsoft\Framework\.NETFramework\v4.8\`
(verified: `Accessibility.dll`, `Facades\`, …).

### 1b. Assembly type ownership (executed)

`strings` scan of `C:\Program Files\NinjaTrader 8\bin\*.dll`:

- `NinjaTrader.Core.dll` contains `ConnectionStatusEventArgs`, `ConnectionStatus`,
  `AddOnBase`, and the `NinjaTrader.Cbi` namespace.
- There is **no** `NinjaTrader.Cbi.dll` in the NT8 bin. `NinjaTrader.Cbi` is a
  namespace inside `NinjaTrader.Core.dll`.

### 1c. Offline compile probe (executed — SUCCESS)

Source (`probe2.cs`) statically references the Cbi types so the compiler must
resolve them from the `-r:` reference:

```csharp
NinjaTrader.Cbi.ConnectionStatusEventArgs e = null;
NinjaTrader.Cbi.ConnectionStatus st = NinjaTrader.Cbi.ConnectionStatus.Connected;
NinjaTrader.NinjaScript.AddOnBase addon = null;
```

Exact command:

```
"/mnt/c/Windows/Microsoft.NET/Framework64/v4.0.30319/csc.exe" -nologo -target:exe \
  -out:"C:\\Users\\hoang\\AppData\\Local\\Temp\\ds104probe\\probe2.exe" \
  -r:"C:\\Program Files\\NinjaTrader 8\\bin\\NinjaTrader.Core.dll" \
  "C:\\Users\\hoang\\AppData\\Local\\Temp\\ds104probe\\probe2.cs"
```

Output (no errors — compile success):

```
c:\Users\hoang\AppData\Local\Temp\ds104probe\probe2.cs(12,39): warning CS0219: The variable 'addon' is assigned but its value is never used
```

Conclusion: compiling against the NT8 assemblies offline **works** with the
Windows .NET Framework compiler via WSL interop.

### 1d. Standalone execution probe (executed — BLOCKED by AgileDotNet)

With `NinjaTrader.Core.dll`, `NinjaTrader.Gui.dll`, `NinjaTrader.Client.dll`
co-located next to the exe, running `probe2.exe` fails BEFORE `Main`:

```
Unhandled Exception: System.TypeInitializationException: The type initializer for '<Module>' threw an exception. ---> System.ArgumentNullException: Value cannot be null.
Parameter name: ptr
   at System.Runtime.InteropServices.Marshal.GetDelegateForFunctionPointer(IntPtr ptr, Type t)
   at <AgileDotNetRT>.InitializeThroughDelegate64(IntPtr )
   at <AgileDotNetRT>.Initialize()
   at .cctor()
   --- End of inner exception stack trace ---
```

`NinjaTrader.Core.dll` is AgileDotNet-protected and only initializes inside
NT8's own process. A standalone CLR cannot host it.

## 2. Remaining dependency for RUNTIME reproduction

The reflection driver (`ReproProgram.cs`) targets the real
`OnVLConnectionStatusUpdate`, but it cannot execute in a standalone process
because of the AgileDotNet protection above. It must run **inside NT8's
AppDomain**, i.e. in a later owner-attended NT8 session. Not authorized for
DS-104 now.

## 3. Full-production compile note

The production `.cs` files use C# ≥ 6, so Framework `csc.exe` (C# 5) cannot build
them. The bounded path is `dotnet.exe` 9.0.307 (Roslyn) with a `net48` target,
`TargetFrameworkRootPath` pointed at the local v4.8 targeting pack (offline),
plus HintPaths to the NinjaTrader assemblies (see `ConnectionStatusRepro.csproj`).

## 4. Withdrawn (superseded)

- "No C# toolchain" — superseded: `csc.exe` and `dotnet.exe` are available via
  WSL interop.
- "Separate `NinjaTrader.Cbi.dll` required" — wrong: Cbi is a namespace in
  `NinjaTrader.Core.dll`.
- "75-minute watchdog outcome" and "timestamp proxy proves recreate" — not
  demonstrated at runtime; withdrawn.
