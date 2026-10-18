package updaterworker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// ── the real friend surface (W-FRIEND-HOSTED Segment 5) ────────────────────
//
// The owner's updater reaches each friend ONLY over its own HTTP API
// (http://<TUNNEL_IP>:<web_port>) and through its user unit
// (systemctl --user / journalctl --user). It never reads a friend's .env or
// database, and never touches the owner's live bot.

// friendHealth is the subset of /api/health the flat probe reads (the same
// fields DS-106's Friends page parses).
type friendHealth struct {
	Status   string `json:"status"`
	Revision string `json:"revision"`
}

func (h OSHost) friendGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return body, nil
}

// FriendFlat reports whether the friend bot at baseURL is flat: its health
// answers 200 AND its positions endpoint reports no open positions. A probe
// error is NOT flat (the caller fail-closes: a friend whose flatness cannot
// be proven is never restarted).
func (h OSHost) FriendFlat(ctx context.Context, baseURL string) (bool, error) {
	body, err := h.friendGet(ctx, baseURL+"/api/health")
	if err != nil {
		return false, fmt.Errorf("friend health: %w", err)
	}
	var hh friendHealth
	if err := json.Unmarshal(body, &hh); err != nil {
		return false, fmt.Errorf("friend health body: %w", err)
	}
	if hh.Status != "ok" && hh.Status != "degraded" {
		return false, fmt.Errorf("friend health status %q", hh.Status)
	}
	// The positions view is JWT-protected on the owner bot; on a friend bot
	// the same endpoint reports the friend's own positions. A 401 here means
	// flatness cannot be proven → not flat (fail-closed), never an error.
	body, err = h.friendGet(ctx, baseURL+"/api/positions")
	if err != nil {
		return false, nil
	}
	var pos []json.RawMessage
	if err := json.Unmarshal(body, &pos); err != nil {
		return false, fmt.Errorf("friend positions body: %w", err)
	}
	return len(pos) == 0, nil
}

// FriendRestart restarts a friend's user unit.
func (OSHost) FriendRestart(ctx context.Context, unit string) error {
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "restart", unit)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl restart %s: %w: %s", unit, err, firstLine(string(out)))
	}
	return nil
}

// FriendBootOK scans the friend unit's journal since `since` for the friend's
// own BOOT INTEGRITY OK line naming expectedSHA, and fails on any REFUSED
// line after it. The friend runs the SAME binary as the owner, so the line's
// shape is identical; the pid check is omitted (the friend's MainPID is not
// known before its boot and is not needed to name the revision).
func (OSHost) FriendBootOK(ctx context.Context, unit string, since time.Time, expectedSHA string) (bool, string, error) {
	if len(expectedSHA) < bootLineShort {
		return false, "", fmt.Errorf("friend boot check: %q is not a release sha", expectedSHA)
	}
	want := bootOKPrefix + expectedSHA[:bootLineShort] + " ·"
	// --since <epoch> keeps the scan bounded to this restart; -o cat drops the
	// journal's own prefix so the boot line starts at column 0.
	args := []string{"--user", "-u", unit, "--since", since.UTC().Format("2006-01-02 15:04:05"), "--no-pager", "-o", "cat"}
	cmd := exec.CommandContext(ctx, "journalctl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// journalctl exits 1 when there are no entries — not an error for us.
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return false, "", nil
		}
		return false, "", fmt.Errorf("journalctl %s: %w: %s", unit, err, firstLine(string(out)))
	}
	rev := ""
	ok := false
	for _, ln := range strings.Split(string(out), "\n") {
		if strings.Contains(ln, bootRefused) {
			return false, "", nil
		}
		if strings.Contains(ln, want) {
			ok = true
			rev = expectedSHA[:bootLineShort]
		}
	}
	return ok, rev, nil
}
