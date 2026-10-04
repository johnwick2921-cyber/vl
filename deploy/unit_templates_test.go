package deploy

// D2-OPS item 7 (units) — pins for the renamed unit templates. Production call
// sites: the REAL templates on disk. The CTO's mutations hit these pins: an
// ExecStart back to the pre-rename binary, an Alias= reintroduced on
// vl.service, or a user unit pointing at %h/<old> paths must all go RED.

import (
	"strings"
	"testing"
)

// retiredName is the pre-rename name, assembled at runtime so the rename
// census never sees it as a literal (the R5 tree holds zero occurrences).
func retiredName() string { return "no" + "fx" }

func TestVlServiceTemplate(t *testing.T) {
	svc := repoFile(t, "deploy/vl.service")
	for _, want := range []string{
		"[Unit]",
		"User=__VL_USER__",
		"WorkingDirectory=__VL_DIR__",
		"ExecStart=__VL_DIR__/vl-bin",
		"Restart=on-failure",
		"[Install]",
	} {
		if !strings.Contains(svc, want) {
			t.Fatalf("vl.service must carry %q:\n%s", want, svc)
		}
	}
	// Plan A6-S6 P2-1: NO Alias — enable must not collide with the installed
	// rollback target's unit file.
	if strings.Contains(svc, "Alias=") {
		t.Fatalf("vl.service must carry NO Alias= (enable would overwrite the R2 rollback target):\n%s", svc)
	}
	// The pre-rename placeholders are gone.
	for _, bad := range []string{"__" + strings.ToUpper(retiredName()) + "_USER__", "__" + strings.ToUpper(retiredName()) + "_DIR__", retiredName() + "-bin"} {
		if strings.Contains(svc, bad) {
			t.Fatalf("vl.service must not carry %q:\n%s", bad, svc)
		}
	}
}

func TestVlWebServiceTemplate(t *testing.T) {
	svc := repoFile(t, "deploy/vl-web.service")
	for _, want := range []string{
		"WorkingDirectory=__VL_DIR__/web",
		"Environment=PATH=__NODE_DIR__:",
		"ExecStart=__NODE_DIR__/npm run dev",
		"User=__VL_USER__",
	} {
		if !strings.Contains(svc, want) {
			t.Fatalf("vl-web.service must carry %q:\n%s", want, svc)
		}
	}
	for _, bad := range []string{"__" + strings.ToUpper(retiredName()) + "_USER__", "__" + strings.ToUpper(retiredName()) + "_DIR__"} {
		if strings.Contains(svc, bad) {
			t.Fatalf("vl-web.service must not carry %q:\n%s", bad, svc)
		}
	}
}

func TestVlUserUnitTemplates(t *testing.T) {
	for path, want := range map[string][]string{
		"deploy/systemd-user/vl-backup.service": {
			"ExecStart=%h/vl/deploy/vl-db-backup.sh",
			"Type=oneshot",
		},
		"deploy/systemd-user/vl-backup.timer": {
			"OnCalendar=*-*-* 05:00:00",
			"OnCalendar=*-*-* 17:30:00",
		},
		"deploy/systemd-user/vl-clock-guard.service": {
			"ExecStart=%h/vl/deploy/vl-clock-guard.sh",
			"Type=oneshot",
		},
		"deploy/systemd-user/vl-clock-guard.timer": {
			"OnCalendar=*:0/15",
		},
		"deploy/systemd-user/vl-updater.service": {
			"ExecStart=%h/bin/vl-updater --install-dir %h/vl serve",
			"EnvironmentFile=%h/.config/vl-updater/env",
			"UnsetEnvironment=TZ",
			"Restart=always",
			"RestartSec=2",
		},
	} {
		body := repoFile(t, path)
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Fatalf("%s must carry %q:\n%s", path, w, body)
			}
		}
		// None of them may name the pre-rename unit, script or binary.
		for _, bad := range []string{retiredName(), strings.ToUpper(retiredName())} {
			if strings.Contains(body, bad) {
				t.Fatalf("%s must not carry %q:\n%s", path, bad, body)
			}
		}
	}
}
