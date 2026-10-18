package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func goodCode() InstallCode {
	return InstallCode{
		V: 1, Name: "tom", Host: "100.64.0.2", NTPort: 36981, WebPort: 18081,
		ConnectKey: strings.Repeat("ab", 32), TSAuthKey: "tskey-123",
	}
}

func TestParseInstallCodeRoundTrip(t *testing.T) {
	ic := goodCode()
	code := encodeInstallCode(ic)
	got, err := ParseInstallCode(code)
	if err != nil {
		t.Fatalf("round-trip refused: %v", err)
	}
	if got.Name != "tom" || got.Host != "100.64.0.2" || got.NTPort != 36981 ||
		got.WebPort != 18081 || got.ConnectKey != ic.ConnectKey || got.TSAuthKey != "tskey-123" {
		t.Fatalf("round-trip lost fields: %+v", got)
	}
	if got.BaseURL() != "http://100.64.0.2:18081" {
		t.Fatalf("BaseURL = %q", got.BaseURL())
	}
}

func TestParseInstallCodeRefuses(t *testing.T) {
	cases := []struct {
		name string
		mut  func(InstallCode) InstallCode
	}{
		{"wrong version", func(ic InstallCode) InstallCode { ic.V = 2; return ic }},
		{"bad name (empty)", func(ic InstallCode) InstallCode { ic.Name = ""; return ic }},
		{"bad name (caps)", func(ic InstallCode) InstallCode { ic.Name = "Tom"; return ic }},
		{"empty host", func(ic InstallCode) InstallCode { ic.Host = ""; return ic }},
		{"bad nt port", func(ic InstallCode) InstallCode { ic.NTPort = 0; return ic }},
		{"bad web port", func(ic InstallCode) InstallCode { ic.WebPort = 70000; return ic }},
		{"bad connect key", func(ic InstallCode) InstallCode { ic.ConnectKey = "xyz"; return ic }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code := encodeInstallCode(c.mut(goodCode()))
			if _, err := ParseInstallCode(code); err == nil {
				t.Fatalf("mutant %q was accepted", c.name)
			}
		})
	}
	if _, err := ParseInstallCode("not-base64!!"); err == nil {
		t.Fatal("garbage install code accepted")
	}
	if _, err := ParseInstallCode(encodeInstallCode(goodCode()) + "="); err != nil {
		// base64.RawURLEncoding has no padding; a padded code may still decode
		// with a different decoder. Accept either decode or refuse, but never a
		// silent half-install: the real gate is the JSON validation above.
		t.Logf("padded code: %v (refused, acceptable)", err)
	}
}

func TestInstallCodeJSONShape(t *testing.T) {
	// The B2 wire shape must stay stable: {"v","name","host","nt_port","web_port",
	// "connect_key","ts_authkey"}.
	raw, _ := json.Marshal(goodCode())
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"v", "name", "host", "nt_port", "web_port", "connect_key", "ts_authkey"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("install code JSON missing %q", k)
		}
	}
}
