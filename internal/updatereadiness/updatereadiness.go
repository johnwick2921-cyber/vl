// Package updatereadiness answers, BEFORE the owner presses Update, whether
// this box can do the one-click walk-away update hands-free (W-ADDON-ONE-CLICK,
// owner order 2026-10-17: "click Update, then nothing else"). Check returns one
// row per precondition with ok and a plain-words fix the Update page can show.
//
// Every probe is read-only. The tests use fake directories only; nothing here
// ever touches the real NinjaTrader install or a live process.
package updatereadiness

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"vl/internal/addonbuild"
)

// Item is one precondition: ok, and a plain-words fix when it is not.
type Item struct {
	Item string `json:"item"`
	OK   bool   `json:"ok"`
	Fix  string `json:"fix"`
	// Detail is optional extra evidence (a version, a connection list).
	Detail string `json:"detail,omitempty"`
}

// Runner runs one external command and returns its combined output. Tests pass
// a fake; production passes RunViaExec.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Config carries the NinjaTrader Documents folder and the seams.
type Config struct {
	// DocsDir is the NinjaTrader 8 Documents folder (the WSL path, e.g.
	// /mnt/c/Users/<u>/Documents/NinjaTrader 8). Empty → Detect is used.
	DocsDir string
	// Detect finds the single NT8 Documents folder. nil → the default scan of
	// /mnt/c/Users/*/Documents/NinjaTrader 8 (addonbuild.Detect once it lands).
	Detect func() (string, error)
	// Run executes a command; nil → the powershell probe fails closed.
	Run Runner
}

// RunViaExec is the production Runner.
func RunViaExec(timeout time.Duration) Runner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		cctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return exec.CommandContext(cctx, name, args...).CombinedOutput()
	}
}

// DefaultDetect finds the single /mnt/c/Users/*/Documents/NinjaTrader 8 whose
// bin/Custom exists; 0 or >1 is an error naming what it saw.
func DefaultDetect() (string, error) {
	users, err := filepath.Glob("/mnt/c/Users/*")
	if err != nil {
		return "", err
	}
	var hits []string
	for _, u := range users {
		docs := filepath.Join(u, "Documents", "NinjaTrader 8")
		if fi, err := os.Stat(filepath.Join(docs, "bin", "Custom")); err == nil && fi.IsDir() {
			hits = append(hits, docs)
		}
	}
	switch len(hits) {
	case 0:
		return "", fmt.Errorf("no NinjaTrader 8 Documents folder under /mnt/c/Users/*")
	case 1:
		return hits[0], nil
	default:
		sort.Strings(hits)
		return "", fmt.Errorf("more than one NinjaTrader 8 Documents folder: %s", strings.Join(hits, ", "))
	}
}

func (c Config) docsDir() (string, error) {
	if c.DocsDir != "" {
		return c.DocsDir, nil
	}
	detect := c.Detect
	if detect == nil {
		detect = DefaultDetect
	}
	return detect()
}

// windowsPath renders a WSL /mnt/<drive>/… path as C:\… for powershell.exe.
func windowsPath(wsl string) string {
	p := filepath.ToSlash(wsl)
	if len(p) >= 6 && strings.HasPrefix(p, "/mnt/") {
		drive := strings.ToUpper(p[5:6])
		rest := strings.TrimPrefix(p[6:], "/")
		return drive + `:\` + strings.ReplaceAll(rest, "/", `\`)
	}
	return strings.ReplaceAll(p, "/", `\`)
}

// Check runs every precondition and returns them in a stable order:
// docs dir, compiler, powershell, connect-on-startup, migration.
func Check(ctx context.Context, c Config) []Item {
	out := make([]Item, 0, 5)

	docs, err := c.docsDir()
	if err != nil {
		out = append(out, Item{Item: "NT8 Documents folder", OK: false, Fix: "NinjaTrader 8 is not installed (or not found). Install NinjaTrader 8, then press Update again.", Detail: err.Error()})
		// The rest depends on the docs dir; report them as not-checkable.
		for _, row := range [][2]string{
			{"NT8 Roslyn compiler", "The NinjaTrader folder is missing — see the first item."},
			{"Connect on startup", "The NinjaTrader folder is missing — see the first item."},
			{"One-time AddOn migration", "The NinjaTrader folder is missing — see the first item."},
		} {
			out = append(out, Item{Item: row[0], OK: false, Fix: row[1]})
		}
		// powershell is independent of the docs dir.
		out = append(out, checkPowershell(ctx, c))
		return out
	}

	out = append(out, Item{Item: "NT8 Documents folder", OK: true, Fix: ""})
	out = append(out, checkCompiler(ctx, c, docs))
	out = append(out, checkPowershell(ctx, c))
	out = append(out, checkConnectOnStartup(docs))
	out = append(out, checkMigration(docs))
	return out
}

func checkCompiler(ctx context.Context, c Config, docs string) Item {
	it := Item{Item: "NT8 Roslyn compiler"}
	// GAP 2: the "compiler" item reads true ONLY when the no-SDK ps1 path
	// actually works — i.e. the embedded build-addon.ps1 loads Roslyn from
	// NT8's bin, resolves its dependencies, and compiles a trivial source
	// against the full reference set. File presence alone is not enough: a
	// box with Roslyn present but broken (or missing its dependency DLLs)
	// must read not-ok before the owner presses Update.
	ok, detail := addonbuild.CompilerProbe(ctx, addonbuild.Config{
		DocsDir:       docs,
		PowerShellExe: "powershell.exe",
		Run:           c.Run,
	})
	it.OK = ok
	it.Detail = detail
	if !ok {
		it.Fix = "NinjaTrader's built-in Roslyn compiler could not compile through the no-SDK build script — reinstall or repair NinjaTrader 8 (Control Panel → NinjaTrader → Repair), then press Update again."
	}
	return it
}

var (
	openTagRe = regexp.MustCompile(`<([A-Za-z0-9]+Options)>`)
	csuRe     = regexp.MustCompile(`<ConnectOnStartup>(true|false)</ConnectOnStartup>`)
	nameRe    = regexp.MustCompile(`<Name>([^<]*)</Name>`)
)

type connection struct {
	block, name, csu string
}

// parseConnections reads the <ConnectOptions> blocks of Config.xml: one
// connection per <ConnectOnStartup> element, associated with its enclosing
// <XxxOptions> block and the connection's <Name>.
func parseConnections(xml string) []connection {
	opens := openTagRe.FindAllStringSubmatchIndex(xml, -1)
	csus := csuRe.FindAllStringSubmatchIndex(xml, -1)
	names := nameRe.FindAllStringSubmatchIndex(xml, -1)

	var conns []connection
	for _, csu := range csus {
		pos := csu[0]
		// The enclosing block is the nearest preceding <XxxOptions> open that
		// is not the <ConnectOptions> wrapper itself.
		block := ""
		blockStart := -1
		for i := len(opens) - 1; i >= 0; i-- {
			tag := xml[opens[i][2]:opens[i][3]]
			if opens[i][0] < pos && tag != "ConnectOptions" {
				block = tag
				blockStart = opens[i][1]
				break
			}
		}
		// The connection <Name> is the last <Name> between the block open and
		// this ConnectOnStartup element.
		name := ""
		for i := range names {
			if names[i][0] > blockStart && names[i][0] < pos {
				name = xml[names[i][2]:names[i][3]]
			}
		}
		conns = append(conns, connection{block: block, name: strings.TrimSpace(name), csu: xml[csu[2]:csu[3]]})
	}
	return conns
}

func checkConnectOnStartup(docs string) Item {
	it := Item{Item: "Connect on startup"}
	xml, err := os.ReadFile(filepath.Join(docs, "Config.xml"))
	if err != nil {
		it.OK = false
		it.Fix = "Could not read NinjaTrader's Config.xml — open NinjaTrader → Connections and tick 'Connect on startup' on the Tradovate connection, then save."
		it.Detail = err.Error()
		return it
	}
	var tradovate []string
	anyTrue := false
	for _, conn := range parseConnections(string(xml)) {
		if strings.Contains(conn.block, "Tradovate") {
			state := conn.csu == "true"
			tradovate = append(tradovate, fmt.Sprintf("%s=%s", conn.name, onOff(state)))
			if state {
				anyTrue = true
			}
		}
	}
	it.Detail = strings.Join(tradovate, ", ")
	if anyTrue {
		it.OK = true
		return it
	}
	it.OK = false
	it.Fix = "No Tradovate connection has 'Connect on startup' ticked — open NinjaTrader → Connections → configure the Tradovate connection → tick 'Connect on startup' → save."
	return it
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func checkPowershell(ctx context.Context, c Config) Item {
	it := Item{Item: "PowerShell reachable"}
	if c.Run == nil {
		it.OK = false
		it.Fix = "PowerShell could not be probed (no command runner)."
		return it
	}
	if _, err := c.Run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "exit 0"); err != nil {
		it.OK = false
		it.Fix = "PowerShell is not reachable from WSL — the update needs it to build and restart NinjaTrader. Enable WSL→Windows interop (or install PowerShell), then press Update again."
		return it
	}
	it.OK = true
	return it
}

func checkMigration(docs string) Item {
	it := Item{Item: "One-time AddOn migration"}
	addons := filepath.Join(docs, "bin", "Custom", "AddOns")
	matches, _ := filepath.Glob(filepath.Join(addons, "VL*.cs"))
	if len(matches) == 0 {
		it.OK = true
		return it
	}
	it.OK = false
	it.Fix = "The old VL*.cs files are still in NinjaTrader's AddOns folder — the very first update needs ONE F5 in NinjaTrader after they are moved out, and only that once."
	it.Detail = fmt.Sprintf("%d file(s): %s", len(matches), strings.Join(matches, ", "))
	return it
}
