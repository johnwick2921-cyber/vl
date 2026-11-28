package telegram

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/logger"
	"vl/store"
)

// UpdateResultMessage must render the plan's exact plain words per terminal
// state, with an unknown state empty (nothing fabricated, nothing sent).
func TestUpdateResultMessage(t *testing.T) {
	cases := []struct {
		name                       string
		state, newTag, oldTag, why string
		want                       string
	}{
		{"done", "complete", "v2", "v1", "", "Update done — running v2"},
		{"rolled back", "rolled_back", "v2", "v1", "AddOn reconnect timed out",
			"Update could not finish — nothing changed, still running v1, trading normal. Reason: AddOn reconnect timed out"},
		{"reverted clean", "reverted_clean", "v2", "v1", "build failed",
			"Update could not finish — nothing changed, still running v1, trading normal. Reason: build failed"},
		{"refused", "refused", "v2", "v1", "not passed within 10m0s: sim_accounts",
			"Update could not finish — nothing changed, still running v1, trading normal. Reason: not passed within 10m0s: sim_accounts"},
		{"recovery needed", "recovery_needed", "v2", "v1", "rollback failed",
			"⚠️ Update needs your attention — the automatic recovery failed. Reason: rollback failed"},
		{"unknown state", "requested", "v2", "v1", "", ""},
	}
	for _, c := range cases {
		if got := UpdateResultMessage(c.state, c.newTag, c.oldTag, c.why); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// ── m438 (release #53): the owner/update sender must neither leak the bot token
// into the journal nor hang the single owner-push worker. PRODUCTION PATH: the
// real NotifyUpdateResult (the function NotifyOwner delegates to) reads a real
// store, builds the real client and talks to a real local listener.

const m438Token = "123456:SECRETTOKENabcDEF-ghi_JKL"

func m438Store(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "tg.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.TelegramConfig().Save(m438Token, ""); err != nil {
		t.Fatal(err)
	}
	if err := st.TelegramConfig().BindUser(4242, "owner"); err != nil {
		t.Fatal(err)
	}
	return st
}

// captureLog swaps the logger's output for a buffer (and restores it).
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := logger.Log.Out
	logger.Log.SetOutput(&buf)
	t.Cleanup(func() { logger.Log.SetOutput(prev) })
	return &buf
}

// MUTANT (shown RED): log the raw error (`%v`, err) instead of
// redactTelegramErr(err, token) -> the connection-refused text carries
// ".../bot123456:SECRETTOKEN.../getMe" into the log -> RED.
func TestNotifyOwnerNeverLogsTheBotToken(t *testing.T) {
	st := m438Store(t)
	old := ownerBotEndpoint
	ownerBotEndpoint = "http://127.0.0.1:1/bot%s/%s" // nothing listens on port 1: a network error that quotes the URL
	t.Cleanup(func() { ownerBotEndpoint = old })
	buf := captureLog(t)

	NotifyOwner(st, "hello")

	out := buf.String()
	if !strings.Contains(out, "telegram update result: bot api") {
		t.Fatalf("expected the failure to be logged, got %q", out)
	}
	if strings.Contains(out, m438Token) || strings.Contains(out, "SECRETTOKEN") {
		t.Fatalf("the bot token leaked into the log: %q", out)
	}
	if !strings.Contains(out, "bot[redacted]") {
		t.Fatalf("the log should show the redacted URL segment: %q", out)
	}
}

func TestRedactTelegramErrCoversTheSendPathShape(t *testing.T) {
	err := errors.New(`Post "https://api.telegram.org/bot` + m438Token + `/sendMessage": dial tcp: i/o timeout`)
	got := redactTelegramErr(err, m438Token)
	if strings.Contains(got, "SECRETTOKEN") || !strings.Contains(got, "i/o timeout") {
		t.Fatalf("redacted = %q", got)
	}
	// a token unknown to the caller is still caught by the URL-segment pattern
	if got := redactTelegramErr(err, ""); strings.Contains(got, "SECRETTOKEN") {
		t.Fatalf("pattern fallback failed: %q", got)
	}
}

// MUTANT (shown RED): build the bot with a client that has no Timeout (what
// tgbotapi.NewBotAPI does) -> the black-holed getMe never returns -> RED.
func TestNotifyOwnerCannotHangOnABlackHole(t *testing.T) {
	st := m438Store(t)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // accepts the connection and never answers
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	oldEP, oldTO := ownerBotEndpoint, ownerSendTimeout
	ownerBotEndpoint = srv.URL + "/bot%s/%s"
	ownerSendTimeout = 300 * time.Millisecond
	t.Cleanup(func() { ownerBotEndpoint, ownerSendTimeout = oldEP, oldTO })
	buf := captureLog(t)

	done := make(chan struct{})
	go func() { NotifyOwner(st, "hello"); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("NotifyOwner hung on a peer that never answers: no HTTP timeout")
	}
	if strings.Contains(buf.String(), "SECRETTOKEN") {
		t.Fatalf("token leaked on the timeout path: %q", buf.String())
	}
}
