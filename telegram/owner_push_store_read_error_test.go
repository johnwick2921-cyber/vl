package telegram

import (
	"errors"
	"path/filepath"
	"testing"

	"vl/internal/ownerpush"
	"vl/store"
)

// m471 L3-1 — a store READ ERROR is not "no chat bound". SendOwnerText reads the
// token and the bound chat from SQLite on every send; any read error used to be
// returned as ownerpush.ErrNotBound, which the pusher counts as skipped_unbound
// and never retries - so a P0 sent while the read failed (a closed handle, disk
// I/O, a moved or corrupt DB) was lost for good and reported as "nothing to do".
//
// MUTANT (shown RED): return ownerpush.ErrNotBound on any Get() error again ->
// "a store READ ERROR is reported as ErrNotBound" + "P0 feed-down lost:
// skipped_unbound=1 failed=0 held=0" -> RED.
func TestOwnerSendStoreReadErrorIsRetriedNotSkipped(t *testing.T) {
	st := m438Store(t)
	_ = newFakeBotAPI(t)
	_ = st.Close() // every read now fails ("sql: database is closed")

	sendErr := NotifyOwner(st, "FEED DOWN: no bars")
	if sendErr == nil {
		t.Fatal("a failing store read must not report a delivery")
	}
	if errors.Is(sendErr, ownerpush.ErrNotBound) {
		t.Fatalf("a store READ ERROR is reported as ErrNotBound (\"no chat bound\"): %v", sendErr)
	}

	p := ownerpush.NewE(func(text string) error { return NotifyOwner(st, text) })
	p.SetEnabledForTest(func() bool { return true })
	if !p.Push("feed-down|e1", "FEED DOWN: no bars") {
		t.Fatal("push refused")
	}
	p.Flush()
	s := p.Stats()
	if s.Skipped != 0 || s.Failed != 1 || p.Held() != 1 {
		t.Fatalf("P0 feed-down lost: skipped_unbound=%d failed=%d held=%d (want skipped=0 failed=1 held=1)", s.Skipped, s.Failed, p.Held())
	}
}

// The real "not configured" states stay a quiet skip: no config row at all,
// an empty token, and a token with no bound chat.
func TestOwnerSendNotConfiguredIsStillASkip(t *testing.T) {
	_ = newFakeBotAPI(t)

	noRow, err := store.New(filepath.Join(t.TempDir(), "norow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = noRow.Close() })
	if err := NotifyOwner(noRow, "hello"); !errors.Is(err, ownerpush.ErrNotBound) {
		t.Fatalf("no telegram config row must return ErrNotBound, got %v", err)
	}

	unbound, err := store.New(filepath.Join(t.TempDir(), "unbound.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unbound.Close() })
	if err := unbound.TelegramConfig().Save(m438Token, ""); err != nil {
		t.Fatal(err)
	}
	if err := NotifyOwner(unbound, "hello"); !errors.Is(err, ownerpush.ErrNotBound) {
		t.Fatalf("a token with no bound chat must return ErrNotBound, got %v", err)
	}
}
