package addonbuild

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// csprojTemplate is the net48 project template Build renders into WorkDir.
// deploy/addon/VL.AddOn.csproj.tmpl is a byte-identical copy for the worker;
// TestTemplateParityWithDeploy keeps the two from drifting.
//
//go:embed csproj.tmpl
var csprojTemplate string

// wslToWindows converts a WSL path to the Windows form dotnet.exe sees.
// /mnt/c/... → C:\... ; anything else (e.g. /home/<u>/...) → the UNC
// \\wsl.localhost\<distro>\... form (distro from WSL_DISTRO_NAME, default
// Ubuntu).
func wslToWindows(p string) string {
	if p == "" {
		return p
	}
	clean := filepath.Clean(p)
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
