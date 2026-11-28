package telegram

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vl/internal/ownerpush"
)

// fakeBotAPI answers getMe always and sendMessage per `sendStatus` (200 = ok).
type fakeBotAPI struct {
	srv        *httptest.Server
	sendStatus atomic.Int32
	mu         sync.Mutex
	sent       []string
}

func newFakeBotAPI(t *testing.T) *fakeBotAPI {
	t.Helper()
	f := &fakeBotAPI{}
	f.sendStatus.Store(200)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"vl","username":"vl_bot"}}`))
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			if st := int(f.sendStatus.Load()); st != 200 {
				w.WriteHeader(st)
				_, _ = w.Write([]byte(`{"ok":false,"error_code":502,"description":"Bad Gateway"}`))
				return
			}
			_ = r.ParseForm()
			f.mu.Lock()
			f.sent = append(f.sent, r.Form.Get("text"))
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":7,"date":1,"chat":{"id":4242,"type":"private"},"text":"x"}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.srv.Close)
	old := ownerBotEndpoint
	ownerBotEndpoint = f.srv.URL + "/bot%s/%s"
	t.Cleanup(func() { ownerBotEndpoint = old })
	return f
}
func (f *fakeBotAPI) messages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sent...)
}

// N4 through the PRODUCTION chain: ownerpush -> telegram.NotifyOwner (the exact
// sender main.go wires) -> the Bot API over HTTP. A 502 on sendMessage used to
// vanish (void sender, key marked seen, budget spent). MUTANT (shown RED): make
// SendOwnerText return nil after a failed bot.Send -> the failure counts as a
// delivery, nothing is held, the message is lost -> RED.
func TestOwnerPushRetriesAFailedTelegramSendEndToEnd(t *testing.T) {
	st := m438Store(t)
	api := newFakeBotAPI(t)
	api.sendStatus.Store(502)
	buf := captureLog(t)

	now := time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	p := ownerpush.NewE(func(text string) error { return NotifyOwner(st, text) })
	p.SetClockForTest(func() time.Time { mu.Lock(); defer mu.Unlock(); return now })

	if !p.Push("feed-down|ep1", "FEED DOWN: no bars") {
		t.Fatal("push refused")
	}
	p.Flush()
	s := p.Stats()
	if s.Failed != 1 || s.Sent != 0 || s.Held != 1 {
		t.Fatalf("a 502 from Telegram must be a held failure, got %+v", s)
	}
	if strings.Contains(s.LastError, "SECRETTOKEN") || strings.Contains(buf.String(), "SECRETTOKEN") {
		t.Fatalf("bot token leaked: stats=%q log=%q", s.LastError, buf.String())
	}

	api.sendStatus.Store(200) // Telegram recovers
	mu.Lock()
	now = now.Add(time.Minute)
	mu.Unlock()
	if n := p.Retry(); n != 1 {
		t.Fatalf("Retry accepted %d, want the held FEED DOWN", n)
	}
	p.Flush()
	if got := api.messages(); len(got) != 1 || got[0] != "FEED DOWN: no bars" {
		t.Fatalf("the owner never received the message after Telegram recovered: %q", got)
	}
	if s := p.Stats(); s.Sent != 1 || s.Held != 0 {
		t.Fatalf("after recovery want sent=1 held=0, got %+v", s)
	}
}

// Unbound chat / no token stays a quiet no-op: a SKIP, never retried forever.
func TestNotifyOwnerUnboundIsASkip(t *testing.T) {
	st := m438Store(t)
	if err := st.TelegramConfig().Save("", ""); err != nil {
		t.Fatal(err)
	}
	if err := NotifyOwner(st, "hello"); err == nil || err.Error() != ownerpush.ErrNotBound.Error() {
		t.Fatalf("no token must return ErrNotBound, got %v", err)
	}
}
