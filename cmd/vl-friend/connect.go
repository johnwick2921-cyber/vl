package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

// connect.go — the AddOn settings file (B2 contract). The app writes
// Documents\NinjaTrader 8\VL\connect.json; the AddOn (DS-103) reads it once at
// start. File absent = the AddOn connects to 127.0.0.1:36974 with no key
// (exactly today's behaviour on the owner's box).

// connectConfig is the exact connect.json shape (B2).
type connectConfig struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	ConnectKey string `json:"connect_key"`
}

// ntVLdir returns the AddOn's VL settings dir under MyDocuments:
// <MyDocuments>\NinjaTrader 8\VL. F2: the C# AddOn (VLTraderTCPClient.cs) and
// NinjaTrader both read Environment.GetFolderPath(MyDocuments) — with OneDrive
// Known-Folder redirection that is NOT <USERPROFILE>\Documents, so every NT
// path must sit under MyDocuments.
func ntVLdir(docs string) string {
	return filepath.Join(docs, "NinjaTrader 8", "VL")
}

// connectJSONPath returns the connect.json path.
func connectJSONPath(docs string) string {
	return filepath.Join(ntVLdir(docs), "connect.json")
}

// addonDstPath returns where the compiled (obfuscated) add-on DLL must live:
// <MyDocuments>\NinjaTrader 8\bin\Custom\VL.AddOn.dll (B10.1 — never a .cs file).
func addonDstPath(docs string) string {
	return filepath.Join(docs, "NinjaTrader 8", "bin", "Custom", "VL.AddOn.dll")
}

// oldAddonPath returns where the previous add-on is MOVED before a swap:
// <MyDocuments>\NinjaTrader 8\VL\old\VL.AddOn.dll (moved, never deleted).
func oldAddonPath(docs string) string {
	return filepath.Join(ntVLdir(docs), "old", "VL.AddOn.dll")
}

// installedJSONPath records the add-on build the app last installed, so the
// update check can tell "newer" without reading the DLL.
func installedJSONPath(docs string) string {
	return filepath.Join(ntVLdir(docs), "installed.json")
}

// readConnectConfig reads connect.json (written by the app at install).
func readConnectConfig(d deps, docs string) (connectConfig, error) {
	b, err := d.readFile(connectJSONPath(docs))
	if err != nil {
		return connectConfig{}, fmt.Errorf("connect.json: %w", err)
	}
	var c connectConfig
	if err := json.Unmarshal(b, &c); err != nil {
		return connectConfig{}, fmt.Errorf("connect.json: %w", err)
	}
	return c, nil
}

// connectJSON marshals the install code into the connect.json bytes.
func connectJSON(ic InstallCode) ([]byte, error) {
	return json.MarshalIndent(connectConfig{
		Host:       ic.Host,
		Port:       ic.NTPort,
		ConnectKey: ic.ConnectKey,
	}, "", "  ")
}

// writeConnectJSON writes connect.json (and its parent dir) for the install code.
func writeConnectJSON(d deps, docs string, ic InstallCode) error {
	b, err := connectJSON(ic)
	if err != nil {
		return fmt.Errorf("connect.json: %w", err)
	}
	if err := d.mkdirAll(ntVLdir(docs), 0o755); err != nil {
		return fmt.Errorf("connect.json dir: %w", err)
	}
	if err := d.writeFile(connectJSONPath(docs), b, 0o600); err != nil {
		return fmt.Errorf("connect.json: %w", err)
	}
	return nil
}

// installedState is what installed.json records.
type installedState struct {
	AddonBuild string `json:"addon_build"`
	AppVersion string `json:"app_version"`
}

// readInstalledState returns the recorded state, or a zero value when absent
// (first run). An unreadable file is an error, not a silent zero.
func readInstalledState(d deps, docs string) (installedState, error) {
	b, err := d.readFile(installedJSONPath(docs))
	if err != nil {
		return installedState{}, nil // absent on first run
	}
	var s installedState
	if err := json.Unmarshal(b, &s); err != nil {
		return installedState{}, fmt.Errorf("installed.json: %w", err)
	}
	return s, nil
}

// writeInstalledState records the build the app just installed.
func writeInstalledState(d deps, docs string, s installedState) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := d.mkdirAll(ntVLdir(docs), 0o755); err != nil {
		return err
	}
	return d.writeFile(installedJSONPath(docs), b, 0o600)
}

// appState records the fields the app needs on later starts (web_port; the
// host and connect key are in connect.json, and the install code — which holds
// the one-time ts_authkey — is never stored). Written to VL\vl-app.json.
type appState struct {
	Name    string `json:"name"`
	WebPort int    `json:"web_port"`
}

func appStatePath(docs string) string {
	return filepath.Join(ntVLdir(docs), "vl-app.json")
}

func writeAppState(d deps, docs string, s appState) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := d.mkdirAll(ntVLdir(docs), 0o755); err != nil {
		return err
	}
	return d.writeFile(appStatePath(docs), b, 0o600)
}

func readAppState(d deps, docs string) (appState, error) {
	b, err := d.readFile(appStatePath(docs))
	if err != nil {
		return appState{}, fmt.Errorf("vl-app.json: %w", err)
	}
	var s appState
	if err := json.Unmarshal(b, &s); err != nil {
		return appState{}, fmt.Errorf("vl-app.json: %w", err)
	}
	return s, nil
}
