package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
)

// installcode.go — the one-time install code the owner sends the friend (B2
// contract). It is base64url of a JSON object; the app decodes and validates it
// on first run. The ts_authkey is used once and never stored.

var (
	friendNameRe = regexp.MustCompile(`^[a-z0-9]{2,20}$`)
	sha256HexRe  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// hostRe accepts an IPv4 literal or a DNS hostname (S18): no scheme, no port,
	// no spaces.
	hostRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9.-]{0,252}[a-zA-Z0-9])?$`)
)

// InstallCode is the decoded install code.
type InstallCode struct {
	V          int    `json:"v"`
	Name       string `json:"name"`
	Host       string `json:"host"`
	NTPort     int    `json:"nt_port"`
	WebPort    int    `json:"web_port"`
	ConnectKey string `json:"connect_key"`
	TSAuthKey  string `json:"ts_authkey"`
}

// ParseInstallCode decodes and validates an install code. Every field the app
// will act on is checked: a malformed code refuses rather than half-installing.
func ParseInstallCode(code string) (InstallCode, error) {
	raw, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil {
		return InstallCode{}, fmt.Errorf("install code: not base64url: %w", err)
	}
	var ic InstallCode
	if err := json.Unmarshal(raw, &ic); err != nil {
		return InstallCode{}, fmt.Errorf("install code: bad JSON: %w", err)
	}
	if ic.V != 1 {
		return InstallCode{}, fmt.Errorf("install code: unsupported version %d", ic.V)
	}
	if !friendNameRe.MatchString(ic.Name) {
		return InstallCode{}, fmt.Errorf("install code: invalid friend name %q", ic.Name)
	}
	if ic.Host == "" || !hostRe.MatchString(ic.Host) {
		return InstallCode{}, fmt.Errorf("install code: invalid host %q", ic.Host)
	}
	if ic.NTPort < 1 || ic.NTPort > 65535 || ic.WebPort < 1 || ic.WebPort > 65535 {
		return InstallCode{}, fmt.Errorf("install code: port out of range")
	}
	if !sha256HexRe.MatchString(ic.ConnectKey) {
		return InstallCode{}, fmt.Errorf("install code: connect key is not 64 hex")
	}
	// TSAuthKey may be empty (the device already joined the tailnet). We never
	// validate its shape beyond non-empty, and never log it.
	return ic, nil
}

// BaseURL is the friend bot's web base URL ("http://host:web_port").
func (ic InstallCode) BaseURL() string {
	return fmt.Sprintf("http://%s:%d", ic.Host, ic.WebPort)
}
