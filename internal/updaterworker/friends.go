package updaterworker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"vl/internal/updaterjob"
)

// ── W-FRIEND-HOSTED Segment 5 (DS-105): the owner's Update also updates
// friends ────────────────────────────────────────────────────────────────────
//
// After the owner's install reaches COMPLETE (the release is proven, the hold
// and the main-tree lock are released), the worker sweeps the friend registry
// (`friends.json`, the B2 contract) and, for each ACTIVE friend, proves it is
// flat before restarting its user unit so it boots the new shared vl-bin.
//
// This sweep is ADVISORY: a friend that cannot be restarted never rolls back
// the owner's proven install. Every outcome is recorded per friend in the job
// file (`friends:[{name,state,rev,note}]`) and in a receipt; the owner is
// notified (the Notify seam) when a friend is deferred or failed.
//
// Friend outcome states (the test matrix):
//
//	restarted — flat → restarted → its BOOT INTEGRITY line named the new rev
//	deferred  — open (never flat) → retried every flat-poll → gave up at the budget
//	restored-restart — flat → restarted → boot FAILED → one extra restart recovered it
//	failed    — boot failed and the restore did not recover, or the probe could not
//	            prove flat before the budget (fail-closed)
//	skipped   — a paused friend (the updater never restarts a paused unit)
//
// The unit and the binary are SHARED with the owner (B2: the friend's
// ExecStart is the owner's vl-bin; the friend folder holds only its .env and
// data). A friend therefore cannot run a DIFFERENT previous release than the
// owner: "restore" is a second restart of the friend's unit, never a revert
// of the shared binary.

// FriendResultState is the closed set of friend outcomes (the test matrix).
type FriendResultState string

const (
	FriendRestarted FriendResultState = "restarted"
	FriendDeferred  FriendResultState = "deferred"
	// FriendRestored is "restored-restart" (CTO ruling 2026-10-18): the
	// binary is SHARED with the owner, so a friend boot failure is recovered
	// by ONE extra restart of the friend unit — never a per-friend binary
	// revert. Recorded honestly as a restart, not a rollback.
	FriendRestored FriendResultState = "restored-restart"
	FriendFailed   FriendResultState = "failed"
	FriendSkipped  FriendResultState = "skipped"
)

// friendRecord is one B2 registry row (friends.json). Only the SHA-256 of the
// connect key lives there; the key itself is never read by the worker.
type friendRecord struct {
	Name              string `json:"name"`
	WebPort           int    `json:"web_port"`
	NTPort            int    `json:"nt_port"`
	KeySHA256         string `json:"key_sha256"`
	State             string `json:"state"`
	CreatedAt         string `json:"created_at"`
	TailscaleDeviceID string `json:"tailscale_device_id"`
}

// readFriends reads the registry. A missing file is an empty list, never an
// error (the owner has no friends yet); a corrupt file is an error (fail
// closed — the sweep records it and notifies, never fabricates).
func readFriends(registryPath string) ([]friendRecord, error) {
	b, err := os.ReadFile(registryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []friendRecord{}, nil
		}
		return nil, err
	}
	var out []friendRecord
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("friend registry %s: %w", registryPath, err)
	}
	if out == nil {
		out = []friendRecord{}
	}
	return out, nil
}

// friendUnit is the B2 user-unit name for a friend.
func friendUnit(name string) string { return "vl-friend@" + name + ".service" }

// friendBaseURL is the friend bot's HTTP base (the box's tunnel IP + the
// friend's web port — the owner bot never reaches into the friend's process).
func (w *Worker) friendBaseURL(port int) string {
	return fmt.Sprintf("http://%s:%d", strings.TrimSpace(w.cfg.FriendHost), port)
}

// updateFriends sweeps the registry and restarts every active, flat friend.
// It never fails the owner's install: every outcome is recorded, and the
// returned receipts + results are persisted with the step's DONE transition.
func (w *Worker) updateFriends(ctx context.Context, expectedSHA string) ([]Receipt, []updaterjob.FriendResult) {
	cfg := w.cfg
	if strings.TrimSpace(cfg.FriendRegistry) == "" || strings.TrimSpace(cfg.FriendHost) == "" {
		return nil, nil // no friend surface configured: no sweep, friends stays absent
	}
	start := w.host.Now()
	rows, err := readFriends(cfg.FriendRegistry)
	if err != nil {
		rc := w.receipt("friends_sweep", start, map[string]string{"registry": cfg.FriendRegistry}, err)
		w.notify("⚠️ Friend update skipped — could not read the friend registry: " + err.Error())
		return []Receipt{rc}, nil
	}
	var results []updaterjob.FriendResult
	var receipts []Receipt
	ev := map[string]string{
		"registry": cfg.FriendRegistry,
		"host":     cfg.FriendHost,
	}
	active := 0
	for _, fr := range rows {
		if fr.State == "active" {
			active++
		}
	}
	ev["active_friends"] = fmt.Sprintf("%d", active)
	receipts = append(receipts, w.receipt("friends_sweep", start, ev, nil))
	for _, fr := range rows {
		if fr.State != "active" {
			// Paused / revoked friends are never restarted.
			if fr.State == "paused" {
				results = append(results, updaterjob.FriendResult{Name: fr.Name, State: string(FriendSkipped), Note: "paused"})
			}
			continue
		}
		res := w.updateOneFriend(ctx, fr, expectedSHA)
		results = append(results, res)
		receipts = append(receipts, w.receipt("friend_"+res.State, w.host.Now(), map[string]string{
			"name": fr.Name, "state": res.State, "rev": res.Rev, "note": res.Note,
		}, nil))
		if res.State != string(FriendRestarted) && res.State != string(FriendSkipped) {
			w.notify(fmt.Sprintf("⚠️ Friend %q could not be updated — %s. %s", fr.Name, res.State, res.Note))
		}
	}
	return receipts, results
}

// updateOneFriend runs one friend through flat → restart → boot, with the
// flat retry loop bounded by the FriendFlat budget.
func (w *Worker) updateOneFriend(ctx context.Context, fr friendRecord, expectedSHA string) updaterjob.FriendResult {
	base := w.friendBaseURL(fr.WebPort)
	view, err := w.host.FriendFlat(ctx, base)
	if err != nil {
		// A flat probe that errors is NOT safe: fail-closed, never restart a
		// friend whose positions we could not read.
		view = FriendFlatView{}
	}
	if !friendRestartSafe(view) {
		deadline := w.host.Now().Add(w.cfg.Budgets.FriendFlat)
		for !friendRestartSafe(view) {
			if !w.host.Now().Before(deadline) {
				return updaterjob.FriendResult{Name: fr.Name, State: string(FriendDeferred), Note: "not flat at the friend-update cutoff"}
			}
			if err := w.host.Sleep(ctx, w.cfg.Budgets.FriendPoll); err != nil {
				return updaterjob.FriendResult{Name: fr.Name, State: string(FriendDeferred), Note: "friend update interrupted: " + err.Error()}
			}
			view, err = w.host.FriendFlat(ctx, base)
			if err != nil {
				view = FriendFlatView{}
			}
		}
	}
	if err := w.host.FriendRestart(ctx, friendUnit(fr.Name)); err != nil {
		return updaterjob.FriendResult{Name: fr.Name, State: string(FriendFailed), Note: "restart: " + err.Error()}
	}
	since := w.host.Now().Truncate(time.Second)
	rev, ok, err := w.friendBootWait(ctx, fr, since, expectedSHA)
	if err != nil {
		return updaterjob.FriendResult{Name: fr.Name, State: string(FriendFailed), Note: "boot check: " + err.Error()}
	}
	if ok {
		return updaterjob.FriendResult{Name: fr.Name, State: string(FriendRestarted), Rev: rev}
	}
	// Boot failed: restore is a second restart of the shared binary (the
	// friend cannot run a different release than the owner — see the file
	// comment). One retry, then fail closed.
	if err := w.host.FriendRestart(ctx, friendUnit(fr.Name)); err != nil {
		return updaterjob.FriendResult{Name: fr.Name, State: string(FriendFailed), Note: "boot failed and the restore restart refused: " + err.Error()}
	}
	since2 := w.host.Now().Truncate(time.Second)
	rev2, ok2, err := w.friendBootWait(ctx, fr, since2, expectedSHA)
	if err != nil {
		return updaterjob.FriendResult{Name: fr.Name, State: string(FriendFailed), Note: "restore boot check: " + err.Error()}
	}
	if ok2 {
		return updaterjob.FriendResult{Name: fr.Name, State: string(FriendRestored), Rev: rev2}
	}
	return updaterjob.FriendResult{Name: fr.Name, State: string(FriendFailed), Note: "boot failed and the restore did not recover"}
}

// friendRestartSafe is the R11 restart rule: restart when the friend reports
// flat==true, OR when it reports a traders count of ZERO (a zero-trader
// instance manages no position, so restarting it is safe even though F1 makes
// its flat read false forever). An OLDER bot that omits "traders" (HasTraders
// false) falls back to flat alone; a failed probe (zero view) is never safe.
func friendRestartSafe(v FriendFlatView) bool {
	if v.HasTraders && v.Traders == 0 {
		return true
	}
	return v.Flat
}

// friendBootWait waits (within the Watch budget, re-read every Poll) for the
// friend's journal to name the expected revision.
func (w *Worker) friendBootWait(ctx context.Context, fr friendRecord, since time.Time, expectedSHA string) (string, bool, error) {
	deadline := w.host.Now().Add(w.cfg.Budgets.Watch)
	for {
		ok, rev, err := w.host.FriendBootOK(ctx, friendUnit(fr.Name), since, expectedSHA)
		if err != nil {
			return "", false, err
		}
		if ok {
			return rev, true, nil
		}
		if !w.host.Now().Before(deadline) {
			return "", false, nil
		}
		if err := w.host.Sleep(ctx, w.cfg.Budgets.Poll); err != nil {
			return "", false, err
		}
	}
}

// notify sends one advisory owner message through the Notify seam (a no-op
// when the seam is nil). It never blocks the worker and never fails the job.
func (w *Worker) notify(text string) {
	if w.notifier == nil {
		w.logf("updater: %s", text)
		return
	}
	w.notifier(text)
}
