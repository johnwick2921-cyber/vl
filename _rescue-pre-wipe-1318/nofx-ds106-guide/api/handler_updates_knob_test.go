package api

// W-ONE-BUTTON M4 3b-B U5b (a) — the NOFX_UPDATER knob, at the PRODUCTION
// router (canon 53: NewServer → setupRoutes, the real gate).
//
// OFF (unset, or anything but exactly "1") is M3, byte for byte: the four
// bodies below were captured from the base 8d189a1e (M3 #200 + M3-LOWS #202,
// before any U5b line) with the knob unset, and are pinned here as literals.
// ON prints ONE read boot line naming the verifier the server actually holds
// and whether the worker socket dialled; OFF prints no line at all.

import (
	"net/http"
	"strings"
	"testing"

	"vl/manager"
)

// wantGlueVerifier is the verifier the knob-ON server must hold and name.
const wantGlueVerifier = "verifier=verdict-file"

// M3's bytes at 8d189a1e (golden literals; captured, not re-derived), with
// the status body as the CTO ruled it for #206: worker_listening is always
// present and measured at request time (false with no worker listening) —
// the knob still toggles install_enabled, byte for byte.
const (
	m3StatusBody   = `{"enrolled":true,"install_enabled":false,"manifest_verifier":"stub","worker_listening":false}`
	m3Install422   = `{"error":"release not verified"}`
	m3NotFoundBody = `{"error":"not found"}`
	m3JSONType     = "application/json; charset=utf-8"
)

func TestUpdatesKnobOffIsByteIdentical(t *testing.T) {
	for _, v := range []string{"", "0", "true", "on", "yes", " 1", "1 ", "01", "TRUE"} {
		t.Run("NOFX_UPDATER="+v, func(t *testing.T) {
			t.Setenv(updaterKnobEnv, v)
			logs := captureLogs(t)
			e := newUpdEnv(t)
			// a job file the worker WOULD have written must stay invisible OFF
			writeTestJob(t, e.dataDir, "0123456789abcdef", updRelease)
			for _, c := range []struct {
				method, path, body string
				code               int
				want               string
			}{
				{"GET", "/api/updates", "", http.StatusOK, m3StatusBody},
				{"POST", "/api/updates/install", grantBody(e.grant(updRelease)), http.StatusUnprocessableEntity, m3Install422},
				{"GET", "/api/updates/jobs/0123456789abcdef", "", http.StatusNotFound, m3NotFoundBody},
				{"GET", "/api/updates/jobs/0123456789abcdef/receipt", "", http.StatusNotFound, m3NotFoundBody},
			} {
				w := e.do(c.method, c.path, c.body)
				if w.Code != c.code || w.Body.String() != c.want || w.Header().Get("Content-Type") != m3JSONType {
					t.Errorf("%s %s = %d %q (%s), want M3's %d %q (%s)", c.method, c.path, w.Code, w.Body.String(), w.Header().Get("Content-Type"), c.code, c.want, m3JSONType)
				}
			}
			if strings.Contains(logs(), "updater glue") {
				t.Fatalf("knob OFF (%q) printed an updater glue line:\n%s", v, logs())
			}
		})
	}
}

func TestUpdaterGlueBootLineIsReadAndOnlyWhenOn(t *testing.T) {
	t.Run("off", func(t *testing.T) {
		t.Setenv(updaterKnobEnv, "")
		logs := captureLogs(t)
		newUpdEnv(t)
		if strings.Contains(logs(), "updater glue") {
			t.Fatalf("knob OFF printed an updater glue line:\n%s", logs())
		}
	})
	t.Run("on, no worker", func(t *testing.T) {
		t.Setenv(updaterKnobEnv, "1")
		logs := captureLogs(t)
		e := newUpdEnv(t)
		if n := strings.Count(logs(), "updater glue:"); n != 1 {
			t.Fatalf("knob ON printed %d updater glue lines, want exactly 1:\n%s", n, logs())
		}
		want := "updater glue: on · verifier=" + updateVerifierName(e.s.updateVerifier) + " · worker=n/a"
		if !strings.Contains(logs(), want) {
			t.Fatalf("knob ON with no worker: want %q, got:\n%s", want, logs())
		}
		if !strings.Contains(logs(), wantGlueVerifier) {
			t.Fatalf("knob ON: the line must name the verifier the server holds (%s):\n%s", wantGlueVerifier, logs())
		}
	})
	t.Run("on, worker listening", func(t *testing.T) {
		t.Setenv(updaterKnobEnv, "1")
		logs := captureLogs(t)
		e := newUpdEnv(t)
		startTestWorker(t, e.dataDir, func(string, string) (bool, string) { return true, "requested" })
		mark := len(logs())
		e.s = NewServer(manager.NewTraderManager(), e.st, nil, "127.0.0.1", 0)
		tail := logs()[mark:]
		if n := strings.Count(tail, "updater glue:"); n != 1 || !strings.Contains(tail, wantGlueVerifier+" · worker=dial ok") {
			t.Fatalf("knob ON with a listening worker: want one line ending `%s · worker=dial ok`, got:\n%s", wantGlueVerifier, tail)
		}
	})
}
