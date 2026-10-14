package store

import (
	"fmt"
	"time"
)

// ── POST-LOSS RE-ARM COUNTER (2026-09-09, dispatch 104 D3) ──────────────────
//
// COUNTER ONLY. It refuses nothing. The research asks how "new fade permissions
// respond after several level failures"; nobody has measured what this desk
// actually does after a loser, so the first job is to count it — a threshold
// invented before the measurement is a threshold nobody can defend.
//
// THE TRIGGER IS A LOSING CLOSE, NOT A STOP-OUT. The dispatch said "after a
// stop-out"; the record says that would observe almost nothing. Of 47 losing
// closes in the retained era (>= 2026-08-15 CT, pnl_corrected NOT NULL,
// excluding the e7 seam), 42 carry close_reason='sync' and only FOUR carry
// 'stop'. A stop-keyed counter would see 4 events and read as "this never
// happens". P&L sign is the honest trigger and needs no attribution to work.

const PostLossReArmKind = "post_loss_rearm"

// PostLossCounterKey scopes the tally per (trader, session-day, session), so a
// ruling can be made per session rather than on one blended number.
func PostLossCounterKey(traderID, tradeDate, session string) string {
	return "post_loss_rearm:" + traderID + ":" + tradeDate + ":" + session
}

// IncPostLossReArm records ONE re-arm-after-a-loss and returns the new count.
func IncPostLossReArm(st *Store, traderID, tradeDate, session string) (int, error) {
	if st == nil || st.gdb == nil {
		return 0, fmt.Errorf("store required")
	}
	key := PostLossCounterKey(traderID, tradeDate, session)
	if err := st.gdb.Exec(`INSERT INTO system_config (key, value) VALUES (?, '1')
		ON CONFLICT(key) DO UPDATE SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT)`, key).Error; err != nil {
		return 0, err
	}
	return CountFromSystemConfig(st, key), nil
}

// PostLossReArmCount reads one counter (0 when absent — never a fabricated
// figure, class 35).
func PostLossReArmCount(st *Store, traderID, tradeDate, session string) int {
	return CountFromSystemConfig(st, PostLossCounterKey(traderID, tradeDate, session))
}

// LastLosingClose returns the most recent CLOSED position whose corrected P&L
// is a RESOLVED loss, and whether one exists in the window.
//
// UNRESOLVED closes are skipped rather than treated as losses: this counter
// must not label an arm on the strength of a P&L nobody can read (A24).
func (s *PositionStore) LastLosingClose(traderID string, sinceMs int64) (*TraderPosition, bool) {
	var rows []TraderPosition
	// R22 R1: a manual NT8 loser is not the bot's loss (BotOriginSQL).
	if err := s.db.Where(BotOriginSQL).
		Where("trader_id = ? AND status = ? AND close_reason <> ? AND exit_time >= ?",
			traderID, "CLOSED", CloseReasonTestSeam, sinceMs).
		Order("exit_time DESC").Limit(20).Find(&rows).Error; err != nil {
		return nil, false
	}
	for i := range rows {
		pnl, resolved := rows[i].CorrectedPnL()
		if !resolved {
			continue
		}
		if pnl < 0 {
			return &rows[i], true
		}
		return nil, false // the most recent RESOLVED close was a win: not a post-loss window
	}
	return nil, false
}

// PostLossWindow reports whether exitMs falls within the window ending at now.
func PostLossWindow(exitMs int64, now time.Time, windowMin int) bool {
	if windowMin <= 0 || exitMs <= 0 {
		return false
	}
	age := now.Sub(time.UnixMilli(exitMs))
	return age >= 0 && age <= time.Duration(windowMin)*time.Minute
}
