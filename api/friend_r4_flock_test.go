package api

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// R4 — the Go registry writers take the friends.json.lock flock the scripts
// use, so a held lock makes setFriendState block until release.

func TestR4SetFriendStateBlocksOnHeldFlock(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)
	writeRegistry(t, `[{"name":"tom","web_port":18081,"nt_port":36981,"key_sha256":"x","state":"active","created_at":"2026-10-18T00:00:00Z"}]`)

	lockPath := filepath.Join(os.Getenv("VL_FRIENDS_ROOT"), "friends.json.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("open lock: %v", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		t.Fatalf("flock: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- setFriendState("tom", "paused") }()

	// With the lock held, the write must NOT complete within a short window.
	select {
	case err := <-done:
		t.Fatalf("setFriendState returned while the lock was held (err=%v) — no flock taken", err)
	case <-time.After(300 * time.Millisecond):
	}

	// Release the lock; the writer must then complete.
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	f.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("setFriendState after release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("setFriendState did not complete after the lock was released")
	}
}
