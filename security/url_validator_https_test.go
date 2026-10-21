package security

import (
	"strings"
	"testing"
)

// U19 — ValidateHTTPSURL is ValidateURL plus a hard https-scheme requirement.

func TestValidateHTTPSURL(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"https public ok", "https://api.deepseek.com", false},
		{"http refused", "http://api.deepseek.com", true},
		{"ftp refused", "ftp://api.deepseek.com", true},
		{"https loopback refused", "https://127.0.0.1:9999", true},
		{"https localhost refused", "https://localhost", true},
		{"https link-local refused", "https://169.254.169.254", true},
		{"empty refused", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateHTTPSURL(c.url)
			if (err != nil) != c.wantErr {
				t.Fatalf("ValidateHTTPSURL(%q) err=%v, wantErr=%v", c.url, err, c.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), c.url) {
				t.Fatalf("error %q should carry the offending URL", err.Error())
			}
		})
	}
}
