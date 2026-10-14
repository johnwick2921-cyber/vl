package trader

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"vl/kernel"
)

// P2 — stopUntil PRODUCER (ledger-close dispatch 2026-08-19).
//
// The legacy at.stopUntil field (auto_trader.go) had a consumer at the top of
// runCycle but NO producer anywhere — a pause switch the owner believed existed
// but didn't. That consumer also blocks the WHOLE cycle, which contradicts the
// owner contract ("block NEW entries only; stops, targets, EOD-flat, position
// management continue"). Per additive-only, the legacy field/gate stay dormant
// and untouched; the REAL pause lives here:
//
//   - state: pauseUntilMs (atomic — set from API goroutines, read from the
//     trading loop; the legacy field had no mutex, this one needs no lock)
//   - persistence: system_config key "trader_pause_until:<id>" (unix ms),
//     restored in Run() → survives systemd restart
//   - semantics: NEW entries refused while now < pauseUntil; closes, EOD-flat,
//     the 60s monitorTick guards, and NT8 brackets are untouched (the gate
//     lives in executeDecisionWithRecord and matches open_* only)
//   - expiry: first read at/after the deadline auto-resumes (clears state +
//     store) and logs — no timer needed
//   - master-INDEPENDENT (owner control, like size caps)

func pauseConfigKey(traderID string) string { return "trader_pause_until:" + traderID }

// PauseEntriesUntil arms the pause. source names the actor for the log ("owner").
func (at *AutoTrader) PauseEntriesUntil(until time.Time, source string) error {
	if !until.After(time.Now()) {
		return fmt.Errorf("pause deadline %s is not in the future", until.Format(time.RFC3339))
	}
	at.pauseStoreMu.Lock()
	at.pauseUntilMs.Store(until.UnixMilli())
	var persistErr error
	if at.store != nil {
		persistErr = at.store.SetSystemConfig(pauseConfigKey(at.id), strconv.FormatInt(until.UnixMilli(), 10))
	}
	at.pauseStoreMu.Unlock()
	if persistErr != nil {
		return fmt.Errorf("pause persisted in memory but store write failed: %w", persistErr)
	}
	at.logWarnf("⏸ stop_until ARMED (%s): NEW entries paused until %s CT. Stops, targets, EOD-flat and position management continue.",
		source, kernel.FormatCT(until))
	return nil
}

// ResumeEntries clears the pause immediately.
func (at *AutoTrader) ResumeEntries(source string) {
	at.pauseStoreMu.Lock()
	at.pauseUntilMs.Store(0)
	if at.store != nil {
		_ = at.store.SetSystemConfig(pauseConfigKey(at.id), "0")
	}
	at.pauseStoreMu.Unlock()
	// W-ONE-BUTTON M2 site 6: the maintenance hold is installation-wide and is
	// NOT a pause — resume never lifts it, and must not claim it did.
	if reason, held := MaintenanceHeld(); held {
		at.logWarnf("▶️ stop_until CLEARED (%s) — but NEW entries stay refused: maintenance hold (%s). They resume when the update completes.", source, reason)
		return
	}
	at.logInfof("▶️ stop_until CLEARED (%s): entries resume.", source)
}

// loadPersistedPause restores a pause across restart (called from Run()).
func (at *AutoTrader) loadPersistedPause() {
	if at.store == nil {
		return
	}
	raw, err := at.store.GetSystemConfig(pauseConfigKey(at.id))
	if err != nil || raw == "" || raw == "0" {
		return
	}
	ms, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || ms <= time.Now().UnixMilli() {
		return // malformed or already expired → boot unpaused (fail-open)
	}
	at.pauseUntilMs.Store(ms)
	at.logWarnf("⏸ stop_until RESTORED after restart: NEW entries paused until %s CT.",
		kernel.FormatCT(time.UnixMilli(ms)))
}

// PauseState reports (until, active) — the API/status surface.
func (at *AutoTrader) PauseState() (time.Time, bool) {
	ms := at.pauseUntilMs.Load()
	if ms == 0 {
		return time.Time{}, false
	}
	return time.UnixMilli(ms), time.Now().UnixMilli() < ms
}

// entryPaused is the gate predicate: reason + true while the pause holds.
// The first read at/after the deadline AUTO-RESUMES (clears state + store).
//
// W-EXEC-TRUTH W0: it takes the caller's clock — the admission gate runs
// beneath the armed pass's clock seam, and every caller now passes its own now
// (the wall-clock wrapper had no production caller left and was removed).
func (at *AutoTrader) entryPausedAt(now time.Time) (string, bool) {
	ms := at.pauseUntilMs.Load()
	if ms == 0 {
		return "", false
	}
	if now.UnixMilli() >= ms {
		// Expiry: only the winner of the CAS clears + logs (loop vs API race).
		if at.pauseUntilMs.CompareAndSwap(ms, 0) {
			// E7-v2 fix: a concurrent RE-PAUSE may land between the CAS and the
			// store write — clearing the store then would strand the new
			// deadline in memory only (lost on restart). Re-check under the
			// same lock the producers persist under.
			at.pauseStoreMu.Lock()
			if at.store != nil && at.pauseUntilMs.Load() == 0 {
				_ = at.store.SetSystemConfig(pauseConfigKey(at.id), "0")
			}
			at.pauseStoreMu.Unlock()
			at.logInfof("▶️ stop_until EXPIRED: pause until %s CT elapsed — entries auto-resume.",
				kernel.FormatCT(time.UnixMilli(ms)))
		}
		return "", false
	}
	return fmt.Sprintf("paused until %s CT (owner)", kernel.FormatCT(time.UnixMilli(ms))), true
}

// pauseStatusString renders the status-API "stop_until" value: the pause
// deadline while armed, else the zero time — byte-identical to the legacy
// (never-assigned) field's rendering when unpaused.
func pauseStatusString(at *AutoTrader) string {
	if until, active := at.PauseState(); active {
		return until.Format(time.RFC3339)
	}
	return time.Time{}.Format(time.RFC3339)
}

// logLedgerBootBlock (E1, ledger-close 2026-08-19) — the per-trader half of
// the boot integrity block: session windows + resolved cutoffs, stop_until
// state, cadence, roll status, balance-alert arming. Called once from Run();
// the process half (build hash, clock, half-days, log-shipping) prints in
// main.go.
func (at *AutoTrader) logLedgerBootBlock(now time.Time) {
	if at.config.Exchange != "ninjatrader" {
		return
	}
	reg := at.sessionRegistry(now)
	sessions := make([]string, 0, len(reg.Sessions))
	for i := range reg.Sessions {
		s := reg.Sessions[i]
		le, fl := "", ""
		if at.config.StrategyConfig != nil && at.config.StrategyConfig.DayPlan != nil {
			if m, hh, ok := sessionCutoffCT(&s, at.config.StrategyConfig.DayPlan.LastEntryOffsetFor(s.Name)); ok {
				_ = m
				le = hh
			}
			if m, hh, ok := sessionCutoffCT(&s, at.config.StrategyConfig.DayPlan.EODFlatOffsetFor(s.Name)); ok {
				_ = m
				fl = hh
			}
		}
		sessions = append(sessions, fmt.Sprintf("%s %s→%s CT (last-entry %s, flat %s)", s.Name, s.WindowStartCT, s.WindowEndCT, le, fl))
	}
	pause := "none"
	if until, active := at.PauseState(); active {
		pause = "until " + kernel.FormatCT(until)
	}
	balanceAlert := "off"
	if th, on := aiBalanceWarnThreshold(); on {
		balanceAlert = fmt.Sprintf("armed (<%.2f)", th)
	}
	roll := "pending AddOn ACK"
	if rs := at.RollStatus(now); rs["resolved"] == true {
		roll = fmt.Sprintf("%v expires %v (window from %v, %v days left)",
			rs["resolved_contract"], rs["contract_expiry"], rs["roll_window_start"], rs["roll_days_left"])
	}
	// Phase 3/2 (final-bundle): every new mode/knob prints value + source so the
	// boot block stays the one-glance truth (E1 contract).
	pmSource := "db"
	if at.config.PositionMode == "" {
		pmSource = "default"
	}
	dodge := "on"
	if !staleDodgeEnabled() {
		dodge = "off"
	}
	postExit := "on"
	if !postExitRescanEnabled() {
		postExit = "off"
	}
	// 6.5 — the silent $500 env fallback becomes visible: when the Studio
	// daily-loss value is 0/unset, RISK_MAX_DAILY_LOSS_USD (default 500) is the
	// enforced limit (only while the guardrails master is ON).
	guardrails := "master=OFF (soft-audit only)"
	if at.config.StrategyConfig != nil {
		rc := at.config.StrategyConfig.RiskControl
		if hlBool(rc.GuardrailsEnabled, false) { // r22: absent = OFF (kernel.ResolveStrategyGuardrails)
			if rc.DailyLossLimitUSD > 0 {
				guardrails = fmt.Sprintf("master=ON daily_loss=$%.0f (studio)", rc.DailyLossLimitUSD)
			} else {
				guardrails = fmt.Sprintf("master=ON daily_loss=$%.0f (env RISK_MAX_DAILY_LOSS_USD fallback — Studio value unset)", kernel.LoadRiskLimitsFromConfig().MaxDailyLossUSD)
			}
		}
	}
	trailing := "OFF"
	if at.config.StrategyConfig != nil {
		if en, mult, period, arm, _ := trailingConfig(at.config.StrategyConfig.RiskControl); en {
			trailing = fmt.Sprintf("%.1f×ATR%d arm=%s (source: studio)", mult, period, arm)
		}
	}
	at.logInfof("🧾 ledger boot: sessions[%s] · stop_until=%s · cadence=%s %v · position_mode=%s (source: %s) · watcher[min_conf=%d hold=%d warn_consec=%d] · trailing=%s · stale_dodge=%s reeval_drift=%.2f×ATR%d · post_exit_rescan=%s delay=%dms · guardrails=%s · roll=%s · balance-alert=%s",
		strings.Join(sessions, " | "), pause, at.cadenceMode(), at.config.ScanInterval,
		at.positionMode(), pmSource,
		watchInvalidateMinConf(), watchMinHoldCycles(), watchWarnConsecutive(),
		trailing, dodge, reevalDriftATRMult(), reevalATRPeriod, postExit, postExitDelayMs(), guardrails, roll, balanceAlert)
}

// SessionEndTime resolves the ACTIVE session's window end as a wall-clock
// instant (wrap-aware: an end HH:MM at/before now belongs to tomorrow CT).
// ok=false when no session is active (overnight/interim — nothing to pause to).
func (at *AutoTrader) SessionEndTime(now time.Time) (time.Time, bool) {
	sess, ok := at.sessionRegistry(now).ActiveSession(now)
	if !ok {
		return time.Time{}, false
	}
	endMin, okMin := hhmmToMin(sess.WindowEndCT)
	if !okMin {
		return time.Time{}, false
	}
	ct := now.In(kernel.CTLocation())
	end := time.Date(ct.Year(), ct.Month(), ct.Day(), endMin/60, endMin%60, 0, 0, kernel.CTLocation())
	if !end.After(now) {
		end = end.Add(24 * time.Hour) // wrapped session (e.g. ASIA → 02:00 next day)
	}
	return end, true
}
