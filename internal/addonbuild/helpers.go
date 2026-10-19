package addonbuild

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// csprojTemplate is the net48 project template Build renders into WorkDir.
// deploy/addon/VL.AddOn.csproj.tmpl is a byte-identical copy for the worker;
// TestTemplateParityWithDeploy keeps the two from drifting.
//
//go:embed csproj.tmpl
var csprojTemplate string

// buildPs1Script is the no-SDK Roslyn build script (GAP 2, the PRIMARY path).
// deploy/addon/build-addon.ps1 is a byte-identical copy for the worker;
// TestPs1ParityWithDeploy keeps the two from drifting.
//
//go:embed build-addon.ps1
var buildPs1Script string

// builderCs is the C# program build-addon.ps1 compiles and runs in -Custom mode
// to rebuild NinjaTrader.Custom.dll WITH its embedded resources (FOLD 3) - the
// one thing PowerShell cannot do (Roslyn's Func(Stream) delegate StackOverflows
// under PS 5.1). deploy/addon/VLCustomBuilder.cs is a byte-identical copy;
// TestBuilderCsParityWithDeploy keeps the two from drifting.
//
//go:embed VLCustomBuilder.cs
var builderCs string

// wslpathSeam runs `wslpath -w <path>` to ask the system for the Windows form
// of a WSL path. A var so tests can substitute a fake; production runs the real
// wslpath (present on every WSL box).
var wslpathSeam = func(wslPath string) (string, error) {
	out, err := exec.Command("wslpath", "-w", wslPath).Output()
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "", fmt.Errorf("wslpath -w %s: empty output", wslPath)
	}
	return s, nil
}

// wslToWindows converts a WSL path to the Windows form dotnet.exe /
// powershell.exe sees. /mnt/c/... → C:\... ; anything else (e.g.
// /home/<u>/...) → the UNC \\wsl.localhost\<distro>\... form. The distro comes
// from WSL_DISTRO_NAME; when that is empty (the updater user unit does not
// inherit it), the system is asked via `wslpath -w` — only if that fails is
// the old "Ubuntu" guess used. A path that is ALREADY Windows (drive letter or
// UNC) passes through unchanged, because the worker's addonExe returns a
// Windows path.
func wslToWindows(p string) string {
	if p == "" {
		return p
	}
	clean := filepath.Clean(p)
	if len(clean) >= 3 && clean[1] == ':' && (clean[2] == '\\' || clean[2] == '/') {
		return strings.ReplaceAll(clean, "/", "\\")
	}
	if strings.HasPrefix(clean, `\\`) {
		return strings.ReplaceAll(clean, "/", "\\")
	}
	if strings.HasPrefix(clean, "/mnt/") {
		rest := strings.TrimPrefix(clean, "/mnt/")
		if len(rest) == 1 {
			return strings.ToUpper(rest[:1]) + ":\\"
		}
		if len(rest) >= 2 && rest[1] == '/' {
			return strings.ToUpper(rest[:1]) + ":" + strings.ReplaceAll(rest[1:], "/", "\\")
		}
	}
	distro := os.Getenv("WSL_DISTRO_NAME")
	if distro == "" {
		// W-UPDATER-WINPATH: systemd does not pass WSL_DISTRO_NAME, and the
		// box's distro may be "Ubuntu-24.04", not "Ubuntu". Ask the system.
		if win, err := wslpathSeam(clean); err == nil {
			return strings.ReplaceAll(win, "/", "\\")
		}
		distro = "Ubuntu"
	}
	return `\\wsl.localhost\` + distro + strings.ReplaceAll(clean, "/", "\\")
}

// renderTemplate substitutes every key in subs with its value in tpl.
func renderTemplate(tpl string, subs map[string]string) string {
	for k, v := range subs {
		tpl = strings.ReplaceAll(tpl, k, v)
	}
	return tpl
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}
