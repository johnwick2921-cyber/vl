package addonbuild

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// What a plain Linux test can say about build-addon.ps1 without PowerShell, NT8 or Roslyn:
// the brackets and quotes balance, the lines that decide what NinjaTrader.Core/Gui the
// build sees are exactly the known ones, and the known ones run in EVERY mode (top level,
// no -Probe branch). It does NOT run the script: that it really compiles is said only by a
// Windows run, and ps1ProvenSha256 is the note of which code lines had one.

// ps1ProvenSha256 is the sha256 of ps1Code(build-addon.ps1).
const ps1ProvenSha256 = "bed229720209d14bda3ea9f8d364f4cf497ea541744c2a5d20ff64f4846f127f"

const ps1ProofsOwed = `a changed code line has not been run anywhere. No Linux test can replace that run. Before this constant is renewed, run on Windows with the real NT8 (and say so in the PR, with the logs):
  1. Go Build() on the LIVE and on the new AddOn source: built_by roslyn-ps1 for both;
  2. a planted C# error is refused;
  3. CompilerProbe on the real folder and on an old-shape csproj (Core and Gui by HintPath): ok;
  4. NinjaTrader.Core.dll missing, and NinjaTrader.Gui.dll missing: each stops with the line that names it;
  5. the built DLL loads and JITs, and the DLL readers agree on its build id;
  6. the NT8 folder is unchanged.
Opt-in helpers: ADDONBUILD_REAL_PROOF=1 (TestRealBuildProof, TestRealPs1ProjectReferenceOnlyCsproj); they cover a part of the list only.`

// ps1Code is the script without its comment-only and blank lines (a #requires line acts,
// so it stays).
func ps1Code(script string) []string {
	var out []string
	for _, l := range strings.Split(script, "\n") {
		l = strings.TrimRight(l, " \t\r")
		s := strings.TrimLeft(l, " \t")
		if s == "" || (strings.HasPrefix(s, "#") && !strings.HasPrefix(strings.ToLower(s), "#requires")) {
			continue
		}
		out = append(out, l)
	}
	return out
}

type ps1Line struct {
	text  string // trimmed
	depth int    // open ( [ { before it
}

// ps1Lex reads the script as PowerShell's brackets, quotes and comments. It knows only
// what this script uses and refuses the rest (backtick, here-string, block comment, a
// quote over a line end), so a new construct is red instead of misread.
func ps1Lex(script string) ([]ps1Line, error) {
	closes := map[byte]byte{')': '(', ']': '[', '}': '{'}
	var open []byte
	var out []ps1Line
	for n, line := range strings.Split(script, "\n") {
		n++
		for i := 0; i < len(line); i++ {
			if line[i] > 126 || (line[i] < 32 && line[i] != '\t') {
				return nil, fmt.Errorf("line %d: byte 0x%02x (PowerShell 5.1 reads this file as ANSI, and a lone CR ends a line): plain ASCII only", n, line[i])
			}
		}
		depth, code := len(open), ""
	scan:
		for i := 0; i < len(line); i++ {
			c, next := line[i], byte(0)
			if i+1 < len(line) {
				next = line[i+1]
			}
			switch {
			case c == '`', c == '<' && next == '#', c == '@' && (next == '\'' || next == '"'):
				return nil, fmt.Errorf("line %d: %q: backtick, block comment or here-string, not known to this guard", n, line)
			case c == '#' && (i == 0 || strings.ContainsRune(" \t({[;,|", rune(line[i-1]))):
				break scan
			case c == '\'' || c == '"':
				j := i + 1
				for ; j < len(line); j++ {
					if line[j] == '`' {
						return nil, fmt.Errorf("line %d: %q: backtick in a string, not known to this guard", n, line)
					}
					if line[j] != c {
						continue
					}
					if j+1 < len(line) && line[j+1] == c {
						j++
						continue
					}
					break
				}
				if j >= len(line) {
					return nil, fmt.Errorf("line %d: %q: the quote %c is not closed", n, line, c)
				}
				code += line[i : j+1]
				i = j
				continue
			case strings.IndexByte("([{", c) >= 0:
				open = append(open, c)
			case closes[c] != 0:
				if len(open) == 0 || open[len(open)-1] != closes[c] {
					return nil, fmt.Errorf("line %d: %q: %c closes nothing", n, line, c)
				}
				open = open[:len(open)-1]
			}
			code += string(c)
		}
		if code = strings.TrimSpace(code); code != "" {
			out = append(out, ps1Line{code, depth})
		}
	}
	if len(open) > 0 {
		return nil, fmt.Errorf("%d brackets stay open at the end: %q", len(open), open)
	}
	return out, nil
}

var (
	ps1FailBody = `function Fail([string]$msg) {
Write-Output ("ERROR " + $msg)
exit 1
}`
	ps1AddRef = `$refPaths = @{}
function Add-Ref([string]$path) {
if (-not $path) { return }
if (-not (Test-Path $path)) { return }
$full = [System.IO.Path]::GetFullPath($path)
try {
[void][System.Reflection.AssemblyName]::GetAssemblyName($full)
} catch {
return
}
$key = [System.IO.Path]::GetFileName($full).ToLowerInvariant()
if (-not $refPaths.ContainsKey($key)) {
$refPaths[$key] = $full
}
}`
	// The csproj mirror, then Core and Gui from the installed NT8, then the rest: with
	// nothing between (no exit, no other $NT8Bin, no -Probe branch).
	ps1MirrorToRefs = `if ($CustomDir) {
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
foreach ($n in @('NinjaTrader.Core.dll','NinjaTrader.Gui.dll')) {
Add-Ref (Join-Path $NT8Bin $n)
if (-not $refPaths.ContainsKey($n.ToLowerInvariant())) {
Fail ($n + ' not found: not in NinjaTrader.Custom.csproj and not in ' + $NT8Bin + ' - is NinjaTrader 8 installed there?')
}
}
Add-Ref (Join-Path $NT8Bin 'Newtonsoft.Json.dll')
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
$trees = New-Object 'System.Collections.Generic.List[Microsoft.CodeAnalysis.SyntaxTree]'`
	ps1Verdict = `$errors = @($result.Diagnostics | Where-Object { $_.Severity -eq [Microsoft.CodeAnalysis.DiagnosticSeverity]::Error })
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
Write-Output 'VL_ADDON_PROBE=ok'`
	// Every code line that names one of these, in order: nothing else may touch them.
	ps1Mentions = []struct {
		name string
		re   *regexp.Regexp
		want []string
	}{
		{"$refPaths", regexp.MustCompile(`(?i)refPaths`), []string{
			"$refPaths = @{}", "if (-not $refPaths.ContainsKey($key)) {", "$refPaths[$key] = $full",
			"if (-not $refPaths.ContainsKey($n.ToLowerInvariant())) {", "if ($refPaths.Count -eq 0) {",
			"foreach ($key in ($refPaths.Keys | Sort-Object)) {",
			"$refs.Add([Microsoft.CodeAnalysis.MetadataReference]::CreateFromFile($refPaths[$key]))"}},
		{"$refs", regexp.MustCompile(`(?i)\brefs\b`), []string{
			"$refs = New-Object 'System.Collections.Generic.List[Microsoft.CodeAnalysis.MetadataReference]'",
			"$refs.Add([Microsoft.CodeAnalysis.MetadataReference]::CreateFromFile($refPaths[$key]))",
			"$compilation = [Microsoft.CodeAnalysis.CSharp.CSharpCompilation]::Create($asmName, $trees, $refs, $options)"}},
		{"$srcFiles", regexp.MustCompile(`(?i)\bsrcFiles\b`), []string{
			"$srcFiles = @(Get-ChildItem -Path $Src -Filter '*.cs' -File)",
			`if ($srcFiles.Count -eq 0) { Fail "no .cs files in " + $Src }`, "foreach ($f in $srcFiles) {"}},
		{"$trees", regexp.MustCompile(`(?i)\btrees\b`), []string{
			"$trees = New-Object 'System.Collections.Generic.List[Microsoft.CodeAnalysis.SyntaxTree]'",
			"$trees.Add([Microsoft.CodeAnalysis.CSharp.CSharpSyntaxTree]::ParseText('public class __VlAddonProbe : NinjaTrader.NinjaScript.AddOnBase { public NinjaTrader.Cbi.Account A; }'))",
			"$trees.Add([Microsoft.CodeAnalysis.CSharp.CSharpSyntaxTree]::ParseText($text, $null, $f.FullName, $null))",
			"$compilation = [Microsoft.CodeAnalysis.CSharp.CSharpCompilation]::Create($asmName, $trees, $refs, $options)"}},
		{"$result", regexp.MustCompile(`(?i)\bresult\b`), []string{
			"$result = $compilation.Emit($ms)", "$result = $compilation.Emit($fs)",
			"$errors = @($result.Diagnostics | Where-Object { $_.Severity -eq [Microsoft.CodeAnalysis.DiagnosticSeverity]::Error })",
			"if (-not $result.Success) {"}},
		{"$Probe", regexp.MustCompile(`(?i)\$probe\b`), []string{
			"[switch]$Probe,", "if (-not $Probe -and -not $Custom -and -not $CustomDir) {",
			"if ($Probe) {", "if ($Probe) {", "if ($Probe) {"}},
		{"an assignment of $NT8Bin", regexp.MustCompile(`(?i)(\$(\w+:)?nt8bin\s*[-+*/%]?=|variable\b.*nt8bin)`), []string{
			`if (-not $NT8Bin) { $NT8Bin = 'C:\Program Files\NinjaTrader 8\bin' }`}},
		{"function", regexp.MustCompile(`(?i)\bfunction\b`), []string{
			"function Fail([string]$msg) {", "function Build-VLCustomBuilder([string]$NT8Bin) {", "function Add-Ref([string]$path) {"}},
		{"alias", regexp.MustCompile(`(?i)\b(set|new)-alias\b|\bInvoke-Expression\b|\biex\b`), nil},
		{"exit, return, throw, break or continue", regexp.MustCompile(`(?i)\b(exit|return|throw|break|continue)\b`), []string{
			"exit 1", "return [System.Reflection.Assembly]::LoadFrom($candidate)", "return $null",
			"try { [void][System.Reflection.AssemblyName]::GetAssemblyName($_.FullName) } catch { return }",
			"return $exe", "exit $LASTEXITCODE", "if (-not $path) { return }", "if (-not (Test-Path $path)) { return }", "return",
			"exit $LASTEXITCODE", "exit 0"}},
		{"trap", regexp.MustCompile(`(?i)^trap\b|ErrorActionPreference`), []string{"$ErrorActionPreference = 'Stop'"}},
	}
)

// ps1Guard says what is wrong with the script's reference set, without the sha256 pin.
func ps1Guard(script string) error {
	lines, err := ps1Lex(script)
	if err != nil {
		return err
	}
	all := "\n"
	for _, l := range lines {
		all += l.text + "\n"
	}
	for _, p := range []struct{ name, block string }{
		{"Fail", ps1FailBody}, {"Add-Ref", ps1AddRef},
		{"csproj mirror + NinjaTrader.Core/Gui + reference list", ps1MirrorToRefs},
		{"compile verdict", ps1Verdict},
	} {
		at := strings.Index(all, "\n"+p.block+"\n")
		if at < 0 || strings.Count(all, "\n"+p.block+"\n") != 1 {
			return fmt.Errorf("the %s block is not there exactly once, line for line", p.name)
		}
		if l := lines[strings.Count(all[:at], "\n")]; l.depth != 0 {
			return fmt.Errorf("the %s block starts inside %d open brackets (%q), want the top level: it must run in every mode", p.name, l.depth, l.text)
		}
	}
	order := -1
	for _, b := range []string{ps1AddRef, ps1MirrorToRefs, "$compilation = ", ps1Verdict} {
		at := strings.Index(all, "\n"+b)
		if at <= order {
			return fmt.Errorf("order: Add-Ref, the reference list, the compile, the verdict (%q)", strings.SplitN(b, "\n", 2)[0])
		}
		order = at
	}
	for _, m := range ps1Mentions {
		var got []string
		for _, l := range lines {
			if m.re.MatchString(l.text) {
				got = append(got, l.text)
			}
		}
		if !reflect.DeepEqual(got, m.want) {
			return fmt.Errorf("the lines that name %s are\n  %s\nwant\n  %s", m.name, strings.Join(got, "\n  "), strings.Join(m.want, "\n  "))
		}
	}
	return nil
}

func TestPs1GuardRealScript(t *testing.T) {
	if err := ps1Guard(BuildPs1()); err != nil {
		t.Fatalf("build-addon.ps1: %v", err)
	}
}

// The guard must see the breaks that keep every text pin of ps1_refs_test.go green (the
// auditor's S1-S8, and more of the same kind). The sha256 pin is not used here.
func TestPs1GuardSeesBreaksThatKeepThePinnedText(t *testing.T) {
	const (
		blk = "foreach ($n in @('NinjaTrader.Core.dll','NinjaTrader.Gui.dll')) {\n"
		end = "is NinjaTrader 8 installed there?')\n    }\n}\n"
		nj  = "Add-Ref (Join-Path $NT8Bin 'Newtonsoft.Json.dll')"
		cmp = "$options = New-Object"
		top = "$codeAnalysis = Join-Path"
		mir = "if ($CustomDir) {\n    $csproj"
		ver = "$errors = @($result.Diagnostics"
		src = "@(Get-ChildItem -Path $Src -Filter '*.cs' -File)"
		fwd = "-File | ForEach-Object { Add-Ref $_.FullName }\n}\n"
	)
	if err := ps1Guard(BuildPs1()); err != nil {
		t.Fatalf("the unbroken script is red, so a red mutant shows nothing: %v", err)
	}
	for name, rs := range map[string][]string{
		"S1 block only with -Probe":           {blk, "if ($Probe) {\n" + blk, end, end + "}\n"},
		"S1b all of it one level down":        {mir, "& {\n" + mir, "[Microsoft.CodeAnalysis.SyntaxTree]'\n\nif ($Probe) {", "[Microsoft.CodeAnalysis.SyntaxTree]'\n}\nif ($Probe) {"},
		"S1c block in an uncalled function":   {blk, "function Later {\n" + blk, end, end + "}\n"},
		"S1d block in a scriptblock":          {blk, "$later = {\n" + blk, end, end + "}\n"},
		"S2 $NT8Bin emptied before the block": {blk, "$NT8Bin = 'C:\\nowhere'\n" + blk},
		"S2b $NT8Bin set early":               {top, "$script:nt8bin = 'C:\\nowhere'\n" + top},
		"S2c Set-Variable":                    {top, "Set-Variable NT8Bin 'C:\\nowhere'\n" + top},
		"S3 Add-Ref last-wins":                {"    if (-not $refPaths.ContainsKey($key)) {\n", "    if ($true) {\n"},
		"S3b Add-Ref does nothing":            {"    if (-not $path) { return }\n", "    return\n"},
		"S3c a second Add-Ref":                {nj, "function Add-Ref([string]$p) { }\n" + nj},
		"S4 Core removed afterwards":          {nj, "$refPaths.Remove('ninjatrader.core.dll')\n" + nj},
		"S4b list emptied before the compile": {cmp, "$refPaths.Clear()\n" + cmp},
		"S4c refs emptied before the compile": {cmp, "$refs.Clear()\n" + cmp},
		"S5 compile errors ignored":           {"if ($errors.Count -gt 0) {", "if ($false) {"},
		"S6 Emit failure ignored":             {"if (-not $result.Success) {", "if ($false) {"},
		"S7 the missing test leaves early":    {"    if (-not $refPaths.ContainsKey($n.ToLowerInvariant())) {\n", "    if (($refPaths.Count -eq 0) -and (-not $x)) { } ; if (-not $refPaths.ContainsKey($n.ToLowerInvariant())) { return\n"},
		"S7b the missing test is inverted":    {"if (-not $refPaths.ContainsKey($n.ToLowerInvariant())) {", "if ($refPaths.ContainsKey($n.ToLowerInvariant())) {"},
		"S8 a bracket is missing":             {"is NinjaTrader 8 installed there?')\n", "is NinjaTrader 8 installed there?'\n"},
		"S8b a quote is missing":              {"is NinjaTrader 8 installed there?')\n", "is NinjaTrader 8 installed there?)\n"},
		"S8c a brace is missing at the end":   {end, "is NinjaTrader 8 installed there?')\n    }\n"},
		"S8d block comment around the block":  {blk, "<#\n" + blk, end, end + "#>\n"},
		"S10 the script leaves early":         {mir, "if ($PSVersionTable.PSVersion.Major -lt 9) { exit 0 }\n" + mir},
		"S10b a top-level return":             {top, "return\n" + top},
		"X1 a success object as the result":   {ver, "$result = New-Object PSObject -Property @{ Success = $true; Diagnostics = @() }\n" + ver},
		"X1b the result by Set-Variable":      {ver, "Set-Variable result ([pscustomobject]@{ Success = $true; Diagnostics = @() })\n" + ver},
		"X1c another Emit result":             {"$result = $compilation.Emit($fs)", "[void]$compilation.Emit($fs); $result = $empty"},
		"X2 a top-level continue":             {fwd, fwd + "continue\n"},
		"X2b continue on a condition":         {top, "if ($true) { Continue }\n" + top},
		"X3 trees emptied before the compile": {cmp, "$trees.Clear()\n" + cmp},
		"X3b trees replaced":                  {cmp, "$Trees = New-Object 'System.Collections.Generic.List[Microsoft.CodeAnalysis.SyntaxTree]'\n" + cmp},
		"X6 only the first source file":       {src, src + " | Select-Object -First 1"},
		"X6b the first file in the loop":      {"foreach ($f in $srcFiles) {", "foreach ($f in $srcFiles[0]) {"},
		"X6c the loop ends after one file":    {"$f.FullName, $null))\n", "$f.FullName, $null))\n        break\n"},
		"X6d the loop skips files":            {"foreach ($f in $srcFiles) {\n", "foreach ($f in $srcFiles) {\n        if ($f.Name -ne 'x.cs') { continue }\n"},
		"Fail does not stop":                  {"    exit 1\n}\n", "}\n"},
		"errors do not stop":                  {"$ErrorActionPreference = 'Stop'", "$ErrorActionPreference = 'SilentlyContinue'"},
		"a lone CR hides code in a comment":   {cmp, "# note\r$x = 1\n" + cmp},
	} {
		t.Run(name, func(t *testing.T) {
			s := BuildPs1()
			for i := 0; i < len(rs); i += 2 {
				if strings.Count(s, rs[i]) != 1 {
					t.Fatalf("the mutant's anchor is there %d times: %q", strings.Count(s, rs[i]), rs[i])
				}
				s = strings.Replace(s, rs[i], rs[i+1], 1)
			}
			if err := ps1Guard(s); err == nil {
				t.Error("a broken script passes the guard")
			} else {
				t.Logf("seen: %v", err)
			}
		})
	}
}

// Not a proof by itself: it only stops a code line from changing without a Windows run.
// Comments and blank lines may change (TestPs1GuardRealScript reads the file's bytes).
func TestPs1CodeLinesAreTheOnesRunOnWindows(t *testing.T) {
	sum := sha256.Sum256([]byte(strings.Join(ps1Code(BuildPs1()), "\n") + "\n"))
	if got := hex.EncodeToString(sum[:]); got != ps1ProvenSha256 {
		t.Fatalf("the code lines of build-addon.ps1 changed: sha256 %s, want %s.\n%s", got, ps1ProvenSha256, ps1ProofsOwed)
	}
}

func TestPs1CodeStripsOnlyCommentsAndBlankLines(t *testing.T) {
	got := ps1Code("# a\r\n\n  # b\n#Requires -Version 7\n$x = 1 # kept \n\t\n  $y\n")
	want := []string{"#Requires -Version 7", "$x = 1 # kept", "  $y"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ps1Code = %q, want %q", got, want)
	}
}
